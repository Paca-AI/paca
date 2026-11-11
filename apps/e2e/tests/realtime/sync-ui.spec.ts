// The web app on the sync protocol (the flag is turned on for every test),
// driven in a real browser: another person or an agent changes the project over
// the API and the page follows without a reload and, where the change carries
// what it needs, without refetching the lists it shows.

import {
	type APIRequestContext,
	type Browser,
	expect,
	type Page,
	test,
} from "@playwright/test";
import {
	API_URL,
	authRequest,
	BASE_URL,
	createProject,
	createProjectAgent,
	createUserWithProjectPermissions,
	RESTRICTED_PASSWORD,
	replaceMemberRoles,
	signIn,
} from "../helpers/e2e-api";
import {
	allow,
	asAgent,
	createAgentKey,
	createDoc,
	createSprint,
	createTask,
	projectRoleFrom,
} from "../helpers/iam";
import { changes, useSyncScaffold } from "../helpers/sync";

const { name } = useSyncScaffold("E2E_RTSYNCUI_");

// E2E_SYNC_OFF=1 runs the same specs on the plain protocol: the checks about
// refetching are expected to differ, everything the page shows must not.
const FLAG = process.env.E2E_SYNC_OFF === "1" ? "0" : "1";

// The acks the page has received for its join, per page. A change made between
// a page loading its data and its join being acknowledged is not in the feed
// (the first join starts at the head), as with the plain protocol; the specs
// wait for the join so what they check is delivery, not that window.
const joinAcks = new WeakMap<Page, string[]>();

test.beforeEach(async ({ page }) => {
	await page.addInitScript((flag) => {
		window.localStorage.setItem("paca.realtime.sync", flag);
	}, FLAG);
	const acks: string[] = [];
	joinAcks.set(page, acks);
	page.on("websocket", (ws) => {
		if (!ws.url().includes("/ws/socket.io")) return;
		ws.on("framereceived", (f) => {
			const text = String(f.payload);
			// An ack packet ("43<id>[...]") that lists rooms: a sync join's or the
			// plain join's. Not permissions:changed, which also lists them.
			if (/^43\d*\[/.test(text) && text.includes('"namespaces"')) {
				acks.push(text);
			}
		});
	});
});

/** Resolves once the page's socket has joined the project's rooms. */
async function joined(page: Page) {
	await expect
		.poll(() => joinAcks.get(page)?.length ?? 0, { timeout: 20_000 })
		.toBeGreaterThan(0);
	// And the page's own first requests: a view list, say, still in flight when
	// the change is made would come back without it, whatever the protocol.
	await page.waitForLoadState("networkidle");
	await page.waitForTimeout(300);
}

const WRITER = {
	"tasks:read": true,
	"tasks:write": true,
	"docs:read": true,
	"docs:write": true,
	"views:read": true,
	"views:write": true,
	"sprints:read": true,
	"project.members:read": true,
} as const;

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
	return {
		projectId,
		username: name("bob"),
		...member,
		api: changes(request, projectId),
	};
}

async function openBacklog(page: Page, projectId: string) {
	await page.goto(`${BASE_URL}/projects/${projectId}/interactions/backlog`);
	await expect(
		page.getByRole("heading", { name: "Product Backlog" }),
	).toBeVisible({
		timeout: 15_000,
	});
}

async function openBacklogTable(page: Page, projectId: string) {
	await openBacklog(page, projectId);
	await page.getByRole("button", { name: "Table", exact: true }).click();
}

/** Counts the GETs a page makes that load task lists of `projectId`. */
function countTaskListFetches(page: Page, projectId: string) {
	const seen = { count: 0, urls: [] as string[] };
	page.on("request", (r) => {
		if (r.method() !== "GET") return;
		const url = r.url();
		if (!url.includes(`/projects/${projectId}/tasks`)) return;
		// A single task's own GET (…/tasks/<uuid>) is not a list.
		const rest = url.split(`/projects/${projectId}/tasks`)[1] ?? "";
		if (/^\/[0-9a-f-]{36}(\?|$|\/)/i.test(rest)) return;
		seen.count += 1;
		seen.urls.push(url);
	});
	return seen;
}

function watchFrames(page: Page) {
	const seen = { sent: [] as string[], received: [] as string[], opened: 0 };
	page.on("websocket", (ws) => {
		if (!ws.url().includes("/ws/socket.io")) return;
		seen.opened += 1;
		ws.on("framesent", (f) => seen.sent.push(String(f.payload)));
		ws.on("framereceived", (f) => seen.received.push(String(f.payload)));
	});
	return seen;
}

test.describe("tasks", () => {
	test("a title change patches the open table in place: no task list is fetched again", async ({
		page,
		request,
		playwright,
	}) => {
		const a = await arrange(request, playwright);
		const title = name("before");
		const taskId = await createTask(request, a.projectId, title);
		await signIn(page, a.username, RESTRICTED_PASSWORD);
		await openBacklogTable(page, a.projectId);
		await expect(page.getByText(title).first()).toBeVisible({
			timeout: 15_000,
		});
		await page.waitForTimeout(1_500);

		const fetches = countTaskListFetches(page, a.projectId);
		const renamed = name("after");
		await a.api.patchTask(taskId, { title: renamed });
		await expect(page.getByText(renamed).first()).toBeVisible({
			timeout: 15_000,
		});
		await expect(page.getByText(title, { exact: true })).toHaveCount(0);
		await page.waitForTimeout(1_500);
		expect(fetches.urls).toEqual([]);
	});

	test("a change a list may filter or sort on refetches the list, and the page ends up right", async ({
		page,
		request,
		playwright,
	}) => {
		const a = await arrange(request, playwright);
		const title = name("t");
		const taskId = await createTask(request, a.projectId, title);
		await signIn(page, a.username, RESTRICTED_PASSWORD);
		await openBacklogTable(page, a.projectId);
		await expect(page.getByText(title).first()).toBeVisible({
			timeout: 15_000,
		});

		const fetches = countTaskListFetches(page, a.projectId);
		await a.api.patchTask(taskId, { importance: 3, title: name("t2") });
		await expect(page.getByText(name("t2")).first()).toBeVisible({
			timeout: 15_000,
		});
		await expect
			.poll(() => fetches.count, { timeout: 10_000 })
			.toBeGreaterThan(0);
	});

	test("a task created by someone else appears, and a deleted one goes", async ({
		page,
		request,
		playwright,
	}) => {
		const a = await arrange(request, playwright);
		await signIn(page, a.username, RESTRICTED_PASSWORD);
		await openBacklogTable(page, a.projectId);
		const title = name("new");
		const taskId = await createTask(request, a.projectId, title);
		await expect(page.getByText(title).first()).toBeVisible({
			timeout: 15_000,
		});
		await a.api.deleteTask(taskId);
		await expect(page.getByText(title)).toHaveCount(0, { timeout: 15_000 });
	});

	test("an agent's edit made with its own key shows up", async ({
		page,
		request,
		playwright,
	}) => {
		const a = await arrange(request, playwright);
		const taskId = await createTask(request, a.projectId, name("t"));
		await signIn(page, a.username, RESTRICTED_PASSWORD);
		await openBacklogTable(page, a.projectId);
		await expect(page.getByText(name("t")).first()).toBeVisible({
			timeout: 15_000,
		});

		const agent = await createProjectAgent(
			request,
			a.projectId,
			name("bot"),
			"acp",
		);
		const key = await createAgentKey(request, agent.id, a.projectId);
		await asAgent(playwright, key, async (api) => {
			const response = await api.patch(
				`${API_URL}/projects/${a.projectId}/tasks/${taskId}`,
				{
					data: { title: name("by_agent") },
				},
			);
			expect(response.ok(), await response.text()).toBeTruthy();
		});
		await expect(page.getByText(name("by_agent")).first()).toBeVisible({
			timeout: 15_000,
		});
	});

	test("thirty quick edits end on the last title and do not fetch the list thirty times", async ({
		page,
		request,
		playwright,
	}) => {
		const a = await arrange(request, playwright);
		const taskId = await createTask(request, a.projectId, name("t"));
		await signIn(page, a.username, RESTRICTED_PASSWORD);
		await openBacklogTable(page, a.projectId);
		await expect(page.getByText(name("t")).first()).toBeVisible({
			timeout: 15_000,
		});
		await page.waitForTimeout(1_000);

		const fetches = countTaskListFetches(page, a.projectId);
		for (let i = 0; i < 30; i++)
			await a.api.patchTask(taskId, { title: name(`v${i}`) });
		await expect(page.getByText(name("v29")).first()).toBeVisible({
			timeout: 20_000,
		});
		await page.waitForTimeout(1_000);
		expect(fetches.count).toBeLessThan(5);
	});

	test("a comment by someone else appears in the open task without reloading it", async ({
		page,
		request,
		playwright,
	}) => {
		const a = await arrange(request, playwright);
		const taskId = await createTask(request, a.projectId, name("t"));
		await signIn(page, a.username, RESTRICTED_PASSWORD);
		await page.goto(`${BASE_URL}/projects/${a.projectId}/tasks/${taskId}`);
		await expect(page.getByText(name("t")).first()).toBeVisible({
			timeout: 15_000,
		});

		const fetches = countTaskListFetches(page, a.projectId);
		await a.api.comment(taskId, name("hello_there"));
		await expect(page.getByText(name("hello_there")).first()).toBeVisible({
			timeout: 15_000,
		});
		// A comment touches its own timeline: no task list, and none of the task's
		// links, attachments or subtasks, is fetched again.
		await page.waitForTimeout(1_000);
		expect(fetches.urls.filter((u) => !u.includes("/activities"))).toEqual([]);
	});

	test("a task's title edited elsewhere updates its open detail page", async ({
		page,
		request,
		playwright,
	}) => {
		const a = await arrange(request, playwright);
		const taskId = await createTask(request, a.projectId, name("old"));
		await signIn(page, a.username, RESTRICTED_PASSWORD);
		await page.goto(`${BASE_URL}/projects/${a.projectId}/tasks/${taskId}`);
		await expect(page.getByText(name("old")).first()).toBeVisible({
			timeout: 15_000,
		});
		await a.api.patchTask(taskId, { title: name("fresh") });
		await expect(page.getByText(name("fresh")).first()).toBeVisible({
			timeout: 15_000,
		});
	});
});

test.describe("sprints, views and the board", () => {
	test("a sprint created and then renamed by someone else shows on the backlog", async ({
		page,
		request,
		playwright,
	}) => {
		const a = await arrange(request, playwright);
		await signIn(page, a.username, RESTRICTED_PASSWORD);
		await openBacklog(page, a.projectId);
		const sprintId = await createSprint(request, a.projectId, name("sprint_a"));
		await expect(page.getByText(name("sprint_a")).first()).toBeVisible({
			timeout: 15_000,
		});
		await a.api.patchSprint(sprintId, { name: name("sprint_b") });
		await expect(page.getByText(name("sprint_b")).first()).toBeVisible({
			timeout: 15_000,
		});
		await expect(page.getByText(name("sprint_a"), { exact: true })).toHaveCount(
			0,
		);
	});

	test("a task moved to a status that was just created shows that status' name on its open page", async ({
		page,
		request,
		playwright,
	}) => {
		const a = await arrange(request, playwright);
		const taskId = await createTask(request, a.projectId, name("t"));
		await signIn(page, a.username, RESTRICTED_PASSWORD);
		await page.goto(`${BASE_URL}/projects/${a.projectId}/tasks/${taskId}`);
		await expect(page.getByText(name("t")).first()).toBeVisible({
			timeout: 15_000,
		});
		await joined(page);
		// The page must learn the status' name (the schema event) and that the
		// task is in it (the task event) without being reloaded.
		const status = await a.api.createStatus(name("status_x"));
		await a.api.patchTask(taskId, { status_id: status.id });
		await expect(page.getByText(name("status_x")).first()).toBeVisible({
			timeout: 15_000,
		});
	});

	test("a view created by someone else makes the page fetch its view list again, and the list has it", async ({
		page,
		request,
		playwright,
	}) => {
		const a = await arrange(request, playwright);
		// As the administrator, who sees every view.
		await signIn(page);
		// The page's own first fetch of the list must be in before the change, or
		// the change finds nothing to refresh (and the list it then loads has it).
		const firstFetch = page.waitForResponse(
			(r) => r.url().includes(`/projects/${a.projectId}/views?context=backlog`),
			{ timeout: 20_000 },
		);
		await openBacklog(page, a.projectId);
		await firstFetch;
		const refetched = page.waitForResponse(
			async (r) =>
				r.url().includes(`/projects/${a.projectId}/views?context=backlog`) &&
				(await r.text().catch(() => "")).includes(name("my_view")),
			{ timeout: 15_000 },
		);
		await a.api.createView(name("my_view"));
		expect((await refetched).ok()).toBe(true);
	});
});

test.describe("docs", () => {
	test("a document and a folder created by someone else appear in the doc tree", async ({
		page,
		request,
		playwright,
	}) => {
		const a = await arrange(request, playwright);
		await signIn(page, a.username, RESTRICTED_PASSWORD);
		await page.goto(`${BASE_URL}/projects/${a.projectId}/docs`);
		await page.waitForLoadState("networkidle");
		await joined(page);

		await createDoc(request, a.projectId, name("doc_one"));
		await expect(page.getByText(name("doc_one")).first()).toBeVisible({
			timeout: 15_000,
		});
		const response = await request.post(
			`${API_URL}/projects/${a.projectId}/docs/folders`,
			{
				data: { name: name("folder_one") },
			},
		);
		expect(response.ok(), await response.text()).toBeTruthy();
		await expect(page.getByText(name("folder_one")).first()).toBeVisible({
			timeout: 15_000,
		});
	});

	test("a document renamed by someone else shows the new title", async ({
		page,
		request,
		playwright,
	}) => {
		const a = await arrange(request, playwright);
		const docId = await createDoc(request, a.projectId, name("doc_old"));
		await signIn(page, a.username, RESTRICTED_PASSWORD);
		await page.goto(`${BASE_URL}/projects/${a.projectId}/docs`);
		await expect(page.getByText(name("doc_old")).first()).toBeVisible({
			timeout: 15_000,
		});
		await a.api.patchDoc(docId, { title: name("doc_new") });
		await expect(page.getByText(name("doc_new")).first()).toBeVisible({
			timeout: 15_000,
		});
	});
});

test.describe("team and project", () => {
	test("a member added by someone else shows on the team page, and their new role after a role change", async ({
		page,
		request,
		playwright,
	}) => {
		const a = await arrange(request, playwright);
		await signIn(page, a.username, RESTRICTED_PASSWORD);
		await page.goto(`${BASE_URL}/projects/${a.projectId}/team`);
		await page.waitForLoadState("networkidle");
		await joined(page);

		const newcomer = await createUserWithProjectPermissions(
			request,
			playwright,
			{
				projectId: a.projectId,
				username: name("newcomer"),
				roleName: name("role_one"),
				permissions: { "tasks:read": true },
			},
		);
		await expect(page.getByText(name("newcomer")).first()).toBeVisible({
			timeout: 15_000,
		});
		await expect(page.getByText(name("role_one")).first()).toBeVisible({
			timeout: 15_000,
		});

		const second = await projectRoleFrom(
			request,
			a.projectId,
			name("role_two"),
			allow(["projects:read", "docs:read"], [`project/${a.projectId}/*`]),
		);
		await replaceMemberRoles(request, a.projectId, newcomer.memberId, [second]);
		await expect(page.getByText(name("role_two")).first()).toBeVisible({
			timeout: 15_000,
		});
	});

	test("a project renamed by someone else is renamed on the open page", async ({
		page,
		request,
		playwright,
	}) => {
		const a = await arrange(request, playwright);
		const taskId = await createTask(request, a.projectId, name("t"));
		await signIn(page, a.username, RESTRICTED_PASSWORD);
		// The task page shows the project's name in its breadcrumb.
		await page.goto(`${BASE_URL}/projects/${a.projectId}/tasks/${taskId}`);
		await expect(page.getByText(name("t")).first()).toBeVisible({
			timeout: 15_000,
		});
		await expect(
			page.getByText(name("p"), { exact: true }).first(),
		).toBeVisible();
		await joined(page);
		await a.api.patchProject({ name: name("renamed_project") });
		await expect(page.getByText(name("renamed_project")).first()).toBeVisible({
			timeout: 15_000,
		});
	});
});

test.describe("permissions", () => {
	test("losing tasks:read hides the tasks, and getting it back shows them again", async ({
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

		const put = async (actions: string[]) => {
			const response = await request.put(
				`${API_URL}/projects/${a.projectId}/roles/${a.roleId}`,
				{
					data: {
						name: name("role"),
						description: "",
						policy: {
							version: "2026-10-01",
							statements: [
								{
									effect: "Allow",
									actions,
									resources: [
										`project/${a.projectId}`,
										`project/${a.projectId}/*`,
									],
								},
							],
						},
					},
				},
			);
			expect(response.ok(), await response.text()).toBeTruthy();
		};
		await put(["projects:read", "docs:read", "views:read", "sprints:read"]);
		await expect(page.getByText(title)).toHaveCount(0, { timeout: 20_000 });
		await put([
			"projects:read",
			"tasks:read",
			"docs:read",
			"views:read",
			"sprints:read",
		]);
		await expect(page.getByText(title).first()).toBeVisible({
			timeout: 20_000,
		});
	});
});

test.describe("two people at once", () => {
	test("each sees the other's changes, a member in a second browser included", async ({
		page,
		browser,
		request,
		playwright,
	}) => {
		const a = await arrange(request, playwright);
		await signIn(page, a.username, RESTRICTED_PASSWORD);
		await openBacklogTable(page, a.projectId);

		const second = await secondBrowserAsAdmin(browser, a.projectId);
		try {
			const first = name("from_admin");
			await createTask(request, a.projectId, first);
			await expect(page.getByText(first).first()).toBeVisible({
				timeout: 15_000,
			});
			await expect(second.page.getByText(first).first()).toBeVisible({
				timeout: 15_000,
			});

			const reply = name("from_member");
			const memberApi = await playwright.request.newContext();
			await memberApi.post(`${API_URL}/auth/login`, {
				data: { username: a.username, password: RESTRICTED_PASSWORD },
			});
			const created = await memberApi.post(
				`${API_URL}/projects/${a.projectId}/tasks`,
				{
					data: { title: reply },
				},
			);
			expect(created.ok(), await created.text()).toBeTruthy();
			await memberApi.dispose();
			await expect(second.page.getByText(reply).first()).toBeVisible({
				timeout: 15_000,
			});
			await expect(page.getByText(reply).first()).toBeVisible({
				timeout: 15_000,
			});
		} finally {
			await second.context.close();
		}
	});
});

async function secondBrowserAsAdmin(browser: Browser, projectId: string) {
	const context = await browser.newContext();
	await context.addInitScript(() => {
		window.localStorage.setItem("paca.realtime.sync", "1");
	});
	const page = await context.newPage();
	joinAcks.set(page, []);
	page.on("websocket", (ws) => {
		ws.on("framereceived", (f) => {
			if (String(f.payload).includes('"namespaces"'))
				joinAcks.get(page)?.push("ack");
		});
	});
	await signIn(page);
	await openBacklogTable(page, projectId);
	return { context, page };
}

test.describe("the connection", () => {
	test("joins through sync:join and never through the plain join", async ({
		page,
		request,
		playwright,
	}) => {
		const a = await arrange(request, playwright);
		const frames = watchFrames(page);
		await signIn(page, a.username, RESTRICTED_PASSWORD);
		await openBacklogTable(page, a.projectId);
		await expect
			.poll(() => frames.sent.some((f) => f.includes('"sync:join"')))
			.toBe(true);
		expect(frames.sent.some((f) => /\["join",\{"projectId"/.test(f))).toBe(
			false,
		);
	});

	test("a change made while the page is offline arrives in place when it is back, without refetching the lists", async ({
		page,
		context,
		request,
		playwright,
	}) => {
		test.setTimeout(120_000);
		const a = await arrange(request, playwright);
		const title = name("offline_task");
		const taskId = await createTask(request, a.projectId, title);
		const frames = watchFrames(page);
		await signIn(page, a.username, RESTRICTED_PASSWORD);
		await openBacklogTable(page, a.projectId);
		await expect(page.getByText(title).first()).toBeVisible({
			timeout: 15_000,
		});
		await expect.poll(() => frames.opened).toBeGreaterThan(0);

		await context.setOffline(true);
		await a.api.patchTask(taskId, { title: name("renamed_offline") });
		await page.waitForTimeout(500);
		await expect(page.getByText(name("renamed_offline"))).toHaveCount(0);

		const fetches = countTaskListFetches(page, a.projectId);
		await context.setOffline(false);
		await expect(page.getByText(name("renamed_offline")).first()).toBeVisible({
			timeout: 60_000,
		});
		await page.waitForTimeout(1_500);
		// Whether the browser opened a new socket or the old one delivered what it
		// had been holding back, the change arrives as a change: in place, with no
		// list fetched again. (A real reconnect with a cursor is checked on a bare
		// socket in sync-resilience.spec.ts.)
		expect(fetches.urls).toEqual([]);
	});

	test("a task created while offline appears after reconnect", async ({
		page,
		context,
		request,
		playwright,
	}) => {
		test.setTimeout(120_000);
		const a = await arrange(request, playwright);
		await signIn(page, a.username, RESTRICTED_PASSWORD);
		await openBacklogTable(page, a.projectId);
		await context.setOffline(true);
		const missed = name("created_offline");
		await createTask(request, a.projectId, missed);
		await context.setOffline(false);
		await expect(page.getByText(missed).first()).toBeVisible({
			timeout: 60_000,
		});
	});

	test("a page on the plain protocol and one on sync both follow the same change", async ({
		page,
		browser,
		request,
		playwright,
	}) => {
		const a = await arrange(request, playwright);
		await signIn(page, a.username, RESTRICTED_PASSWORD);
		await openBacklogTable(page, a.projectId);

		const legacyContext = await browser.newContext();
		await legacyContext.addInitScript(() => {
			window.localStorage.setItem("paca.realtime.sync", "0");
		});
		const legacy = await legacyContext.newPage();
		joinAcks.set(legacy, []);
		legacy.on("websocket", (ws) => {
			ws.on("framereceived", (f) => {
				if (String(f.payload).includes('"namespaces"'))
					joinAcks.get(legacy)?.push("ack");
			});
		});
		const frames = watchFrames(legacy);
		await signIn(legacy);
		await openBacklogTable(legacy, a.projectId);
		await expect
			.poll(() => frames.sent.some((f) => /\["join",/.test(f)))
			.toBe(true);

		const title = name("for_both");
		await createTask(request, a.projectId, title);
		await expect(page.getByText(title).first()).toBeVisible({
			timeout: 15_000,
		});
		await expect(legacy.getByText(title).first()).toBeVisible({
			timeout: 15_000,
		});
		await legacyContext.close();
	});
});
