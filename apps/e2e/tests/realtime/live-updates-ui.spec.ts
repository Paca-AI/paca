// spec: features/realtime/live-updates-ui.feature
// seed: tests/seed.spec.ts
//
// The web app keeps one socket per signed-in session and refetches what an
// event says changed. These specs drive the real app in a browser, change data
// as the admin over the API, and check that the page follows without a reload:
// tasks appear and update live, a member's UI follows edits to their role (the
// server's `permissions:changed`), the session survives the server's `reauth`
// (refresh, then reconnect on the new cookie), and a dropped connection catches
// up when it comes back.

import {
	type APIRequestContext,
	expect,
	type Page,
	test,
} from "@playwright/test";
import {
	API_URL,
	allowPolicy,
	authRequest,
	BASE_URL,
	cleanupProjectsByPrefix,
	cleanupUsersByPrefix,
	createProject,
	createUserWithProjectPermissions,
	newRunId,
	RESTRICTED_PASSWORD,
	signIn,
} from "../helpers/e2e-api";
import { createTask } from "../helpers/iam";
import { mintToken } from "../helpers/realtime";

const PREFIX = "E2E_RTUI_";
const RUN_ID = newRunId();
const name = (label: string) => `${PREFIX}${label}_${RUN_ID}`;

async function cleanup(request: APIRequestContext) {
	await cleanupProjectsByPrefix(request, PREFIX);
	await cleanupUsersByPrefix(request, PREFIX);
}

// E2E_SYNC=1 runs every spec of this file with the web app on the sync
// protocol (apps/web/src/lib/sync-engine.ts), which is off by default.
const SYNC = process.env.E2E_SYNC === "1";

test.beforeEach(async ({ request, context, page }) => {
	if (SYNC) {
		await page.addInitScript(() => {
			window.localStorage.setItem("paca.realtime.sync", "1");
		});
	}
	await authRequest(request);
	await cleanup(request);
	await context.clearCookies();
});

test.afterEach(async ({ request }) => {
	await authRequest(request);
	await cleanup(request);
});

// ─── Arrangement ─────────────────────────────────────────────────────────────

const WRITER = {
	"tasks:read": true,
	"tasks:write": true,
	"views:read": true,
	"views:write": true,
	"sprints:read": true,
} as const;

/** A project and a member whose role holds `permissions`. */
async function arrange(
	request: APIRequestContext,
	playwright: Parameters<typeof createUserWithProjectPermissions>[1],
	permissions: Record<string, boolean> = WRITER,
) {
	await authRequest(request);
	const projectId = await createProject(request, name("p"));
	const member = await createUserWithProjectPermissions(request, playwright, {
		projectId,
		username: name("bob"),
		roleName: name("role"),
		permissions,
	});
	return { projectId, username: name("bob"), ...member };
}

/** Replaces what the member's role grants. */
async function editRole(
	request: APIRequestContext,
	projectId: string,
	roleId: string,
	permissions: Record<string, boolean>,
) {
	const actions = [
		...new Set([
			"projects:read",
			...Object.keys(permissions).filter((a) => permissions[a]),
		]),
	];
	const response = await request.put(
		`${API_URL}/projects/${projectId}/roles/${roleId}`,
		{
			data: {
				name: name("role"),
				description: "",
				policy: allowPolicy(actions, { projectId }),
			},
		},
	);
	expect(response.ok(), await response.text()).toBeTruthy();
}

async function openBacklogTable(page: Page, projectId: string) {
	await page.goto(`${BASE_URL}/projects/${projectId}/interactions/backlog`);
	await expect(
		page.getByRole("heading", { name: "Product Backlog" }),
	).toBeVisible({
		timeout: 15_000,
	});
	await page.getByRole("button", { name: "Table", exact: true }).click();
}

const rename = (
	request: APIRequestContext,
	projectId: string,
	taskId: string,
	title: string,
) =>
	request.patch(`${API_URL}/projects/${projectId}/tasks/${taskId}`, {
		data: { title },
	});

// ─── Live data ───────────────────────────────────────────────────────────────

test.describe("The page follows other people's changes", () => {
	test("a task created by someone else appears without a reload", async ({
		page,
		request,
		playwright,
	}) => {
		const a = await arrange(request, playwright);
		await signIn(page, a.username, RESTRICTED_PASSWORD);
		await openBacklogTable(page, a.projectId);

		const title = name("appears");
		await expect(page.getByText(title)).toHaveCount(0);
		await createTask(request, a.projectId, title);
		await expect(page.getByText(title).first()).toBeVisible({
			timeout: 15_000,
		});
	});

	test("a task renamed by someone else updates in place", async ({
		page,
		request,
		playwright,
	}) => {
		const a = await arrange(request, playwright);
		const before = name("before");
		const taskId = await createTask(request, a.projectId, before);
		await signIn(page, a.username, RESTRICTED_PASSWORD);
		await openBacklogTable(page, a.projectId);
		await expect(page.getByText(before).first()).toBeVisible({
			timeout: 15_000,
		});

		const after = name("after");
		expect(
			(await rename(request, a.projectId, taskId, after)).ok(),
		).toBeTruthy();
		await expect(page.getByText(after).first()).toBeVisible({
			timeout: 15_000,
		});
		await expect(page.getByText(before)).toHaveCount(0);
	});

	test("a deleted task disappears", async ({ page, request, playwright }) => {
		const a = await arrange(request, playwright);
		const title = name("doomed");
		const taskId = await createTask(request, a.projectId, title);
		await signIn(page, a.username, RESTRICTED_PASSWORD);
		await openBacklogTable(page, a.projectId);
		await expect(page.getByText(title).first()).toBeVisible({
			timeout: 15_000,
		});

		await request.delete(`${API_URL}/projects/${a.projectId}/tasks/${taskId}`);
		await expect(page.getByText(title)).toHaveCount(0, { timeout: 15_000 });
	});

	test("a task the member is denied never shows up, while the others do", async ({
		page,
		request,
		playwright,
	}) => {
		const a = await arrange(request, playwright);
		const secretTitle = name("secret");
		const secret = await createTask(request, a.projectId, secretTitle);
		// Deny on top of the member's role: a second role attached to them.
		const denyRole = await request.post(
			`${API_URL}/projects/${a.projectId}/roles`,
			{
				data: {
					name: name("deny"),
					description: "",
					policy: {
						version: "2026-10-01",
						statements: [
							{
								effect: "Deny",
								actions: ["tasks:read"],
								resources: [`project/${a.projectId}/task/${secret}`],
							},
						],
					},
				},
			},
		);
		expect(denyRole.ok(), await denyRole.text()).toBeTruthy();
		const denyRoleId = (await denyRole.json()).data.id as string;
		const put = await request.put(
			`${API_URL}/projects/${a.projectId}/members/${a.memberId}/roles`,
			{
				data: { role_ids: [a.roleId, denyRoleId] },
			},
		);
		expect(put.ok(), await put.text()).toBeTruthy();

		await signIn(page, a.username, RESTRICTED_PASSWORD);
		await openBacklogTable(page, a.projectId);

		const openTitle = name("open");
		await createTask(request, a.projectId, openTitle);
		await rename(request, a.projectId, secret, name("secret2"));
		await expect(page.getByText(openTitle).first()).toBeVisible({
			timeout: 15_000,
		});
		await expect(page.getByText(secretTitle)).toHaveCount(0);
		await expect(page.getByText(name("secret2"))).toHaveCount(0);
	});
});

// ─── The UI follows the member's permissions ─────────────────────────────────

test.describe("The page follows edits to the member's role", () => {
	test("losing tasks:write hides 'Add task' and getting it back shows it again", async ({
		page,
		request,
		playwright,
	}) => {
		const a = await arrange(request, playwright);
		await signIn(page, a.username, RESTRICTED_PASSWORD);
		await openBacklogTable(page, a.projectId);
		const addTask = page.getByRole("button", { name: "Add task" }).first();
		await expect(addTask).toBeVisible({ timeout: 15_000 });

		await editRole(request, a.projectId, a.roleId, {
			"tasks:read": true,
			"views:read": true,
			"sprints:read": true,
		});
		await expect(page.getByRole("button", { name: "Add task" })).toHaveCount(
			0,
			{
				timeout: 15_000,
			},
		);

		await editRole(request, a.projectId, a.roleId, WRITER);
		await expect(
			page.getByRole("button", { name: "Add task" }).first(),
		).toBeVisible({
			timeout: 15_000,
		});
	});

	test("losing tasks:read empties the list and getting it back fills it again", async ({
		page,
		request,
		playwright,
	}) => {
		const a = await arrange(request, playwright);
		const title = name("visible");
		await createTask(request, a.projectId, title);
		await signIn(page, a.username, RESTRICTED_PASSWORD);
		await openBacklogTable(page, a.projectId);
		await expect(page.getByText(title).first()).toBeVisible({
			timeout: 15_000,
		});

		await editRole(request, a.projectId, a.roleId, {
			"views:read": true,
			"sprints:read": true,
		});
		await expect(page.getByText(title)).toHaveCount(0, { timeout: 15_000 });

		await editRole(request, a.projectId, a.roleId, WRITER);
		await expect(page.getByText(title).first()).toBeVisible({
			timeout: 15_000,
		});
	});

	test("removing the member leaves them without the project's tasks", async ({
		page,
		request,
		playwright,
	}) => {
		const a = await arrange(request, playwright);
		const title = name("gone");
		await createTask(request, a.projectId, title);
		await signIn(page, a.username, RESTRICTED_PASSWORD);
		await openBacklogTable(page, a.projectId);
		await expect(page.getByText(title).first()).toBeVisible({
			timeout: 15_000,
		});

		const removed = await request.delete(
			`${API_URL}/projects/${a.projectId}/members/${a.memberId}`,
		);
		expect(removed.ok(), await removed.text()).toBeTruthy();
		await expect(page.getByText(title)).toHaveCount(0, { timeout: 15_000 });
	});
});

// ─── The session and the socket ──────────────────────────────────────────────

/** Records the socket.io frames and websocket opens of a page. */
function watchSocket(page: Page) {
	const seen = {
		opened: 0,
		reauth: 0,
		refreshes: 0,
		frames: [] as string[],
		sent: [] as string[],
	};
	page.on("websocket", (ws) => {
		if (!ws.url().includes("/ws/socket.io")) return;
		seen.opened += 1;
		ws.on("framesent", (frame) => seen.sent.push(String(frame.payload)));
		ws.on("framereceived", (frame) => {
			const text = String(frame.payload);
			seen.frames.push(text);
			if (text.includes('"reauth"')) seen.reauth += 1;
		});
	});
	page.on("request", (request) => {
		if (
			request.method() === "POST" &&
			request.url().endsWith("/auth/refresh")
		) {
			seen.refreshes += 1;
		}
	});
	return seen;
}

test.describe("The session and the socket", () => {
	test("the page opens one realtime connection and keeps it", async ({
		page,
		request,
		playwright,
	}) => {
		const a = await arrange(request, playwright);
		const seen = watchSocket(page);
		await signIn(page, a.username, RESTRICTED_PASSWORD);
		await openBacklogTable(page, a.projectId);
		await expect.poll(() => seen.opened).toBeGreaterThan(0);
		const opened = seen.opened;
		await page.waitForTimeout(3_000);
		expect(seen.opened).toBe(opened);
		expect(seen.reauth).toBe(0);
	});

	test("joins through sync:join and receives the change as sync:changes, not as an event", async ({
		page,
		request,
		playwright,
	}) => {
		test.skip(!SYNC, "runs with E2E_SYNC=1");
		const a = await arrange(request, playwright);
		const seen = watchSocket(page);
		await signIn(page, a.username, RESTRICTED_PASSWORD);
		await openBacklogTable(page, a.projectId);
		await expect
			.poll(() => seen.sent.some((f) => f.includes('"sync:join"')))
			.toBe(true);
		expect(seen.sent.some((f) => /"join",\{"projectId"/.test(f))).toBe(false);

		const title = name("viasync");
		const mark = seen.frames.length;
		await createTask(request, a.projectId, title);
		await expect(page.getByText(title).first()).toBeVisible({
			timeout: 15_000,
		});
		const after = seen.frames.slice(mark).join("\n");
		expect(after).toContain("sync:changes");
		expect(after).not.toMatch(/\["event",\{"type":"task\.created"/);
	});

	test("answers the server's reauth by refreshing the session and reconnecting, and stays live", async ({
		page,
		context,
		request,
		playwright,
	}) => {
		test.setTimeout(120_000);
		const a = await arrange(request, playwright);
		const seen = watchSocket(page);
		await signIn(page, a.username, RESTRICTED_PASSWORD);

		// Swap the access token for a copy that expires in 75s: the server asks
		// the page to reauthenticate about 15s after it connects.
		const cookies = await context.cookies();
		const access = cookies.find((c) => c.name === "access_token");
		expect(access, "the browser holds an access_token cookie").toBeTruthy();
		await context.addCookies([
			{
				...(access as (typeof cookies)[number]),
				value: mintToken((access as { value: string }).value, 75),
			},
		]);
		await openBacklogTable(page, a.projectId);
		await expect
			.poll(() => seen.opened, { timeout: 10_000 })
			.toBeGreaterThan(0);
		const openedBefore = seen.opened;
		const refreshesBefore = seen.refreshes;

		await test.step("the server asks the page to reauthenticate", async () => {
			await expect
				.poll(() => seen.reauth, { timeout: 40_000 })
				.toBeGreaterThan(0);
		});
		await test.step("the page refreshes its session and opens a new connection", async () => {
			await expect
				.poll(() => seen.refreshes, { timeout: 15_000 })
				.toBeGreaterThan(refreshesBefore);
			await expect
				.poll(() => seen.opened, { timeout: 15_000 })
				.toBeGreaterThan(openedBefore);
		});
		await test.step("it still receives live changes afterwards", async () => {
			const title = name("after_reauth");
			await createTask(request, a.projectId, title);
			await expect(page.getByText(title).first()).toBeVisible({
				timeout: 15_000,
			});
		});
		await test.step("and keeps them past the old token's expiry", async () => {
			// The minted token is long expired by now; the page is on the real,
			// refreshed one, so the server must not have cut it off.
			await page.waitForTimeout(45_000);
			const title = name("past_expiry");
			await createTask(request, a.projectId, title);
			await expect(page.getByText(title).first()).toBeVisible({
				timeout: 15_000,
			});
		});
	});

	test("catches up on what it missed once the connection is back", async ({
		page,
		context,
		request,
		playwright,
	}) => {
		test.setTimeout(120_000);
		const a = await arrange(request, playwright);
		const seen = watchSocket(page);
		await signIn(page, a.username, RESTRICTED_PASSWORD);
		await openBacklogTable(page, a.projectId);
		await expect.poll(() => seen.opened).toBeGreaterThan(0);

		await context.setOffline(true);
		const missed = name("missed");
		await createTask(request, a.projectId, missed);
		await expect(page.getByText(missed)).toHaveCount(0);
		await context.setOffline(false);

		// The socket reconnects on its own, rejoins the project and, once the
		// join is acknowledged, the project's queries are refetched.
		await expect(page.getByText(missed).first()).toBeVisible({
			timeout: 60_000,
		});
		expect(seen.opened).toBeGreaterThan(1);
	});
});
