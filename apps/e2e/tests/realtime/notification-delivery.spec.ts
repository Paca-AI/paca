// spec: features/realtime/notification-delivery.feature
// seed: tests/seed.spec.ts
//
// A notification is stored, then announced over a socket that keeps nothing for
// a client that is away. These specs make a member miss the announcement three
// ways and check the bell still shows the notification: the browser goes
// offline, the realtime service is down, and the announcement is lost while the
// socket stays up (found by the once-a-minute re-read).

import { execFileSync } from "node:child_process";
import {
	type APIRequestContext,
	expect,
	type Page,
	test,
} from "@playwright/test";
import {
	API_URL,
	authRequest,
	cleanupProjectsByPrefix,
	cleanupUsersByPrefix,
	createProject,
	createUserWithProjectPermissions,
	newRunId,
	RESTRICTED_PASSWORD,
	signIn,
} from "../helpers/e2e-api";
import { createTask } from "../helpers/iam";

const PREFIX = "E2E_NOTIFY_";
const RUN_ID = newRunId();
const name = (label: string) => `${PREFIX}${label}_${RUN_ID}`;

async function cleanup(request: APIRequestContext) {
	await cleanupProjectsByPrefix(request, PREFIX);
	await cleanupUsersByPrefix(request, PREFIX);
}

test.beforeEach(async ({ request, context }) => {
	await authRequest(request);
	await cleanup(request);
	await context.clearCookies();
});

test.afterEach(async ({ request }) => {
	await authRequest(request);
	await cleanup(request);
});

/** A project, a member who will be notified, and a task to assign to them. */
async function arrange(
	request: APIRequestContext,
	playwright: Parameters<typeof createUserWithProjectPermissions>[1],
) {
	await authRequest(request);
	const projectId = await createProject(request, name("p"));
	const member = await createUserWithProjectPermissions(request, playwright, {
		projectId,
		username: name("bob"),
		roleName: name("role"),
		permissions: { "tasks:read": true },
	});
	const taskId = await createTask(request, projectId, name("task"));
	return { projectId, taskId, username: name("bob"), ...member };
}

/** The admin (who is not the member) assigns the task to the member. */
async function assign(
	request: APIRequestContext,
	a: { projectId: string; taskId: string; memberId: string },
) {
	const response = await request.patch(
		`${API_URL}/projects/${a.projectId}/tasks/${a.taskId}`,
		{ data: { assignee_ids: [a.memberId] } },
	);
	expect(response.ok(), await response.text()).toBeTruthy();
}

const bell = (page: Page) =>
	page.getByRole("button", { name: "Notifications" });

/** Counts the app's realtime socket connections and the frames they receive. */
function watchSocket(page: Page) {
	const seen = { opened: 0, frames: 0 };
	page.on("websocket", (ws) => {
		if (!ws.url().includes("/ws/socket.io")) return;
		seen.opened++;
		ws.on("framereceived", () => {
			seen.frames++;
		});
	});
	return seen;
}

/** Signs in and waits until the socket is up, so the next change is a real miss. */
async function openApp(page: Page, username: string) {
	const socket = watchSocket(page);
	await signIn(page, username, RESTRICTED_PASSWORD);
	await expect
		.poll(() => socket.frames, { timeout: 20_000 })
		.toBeGreaterThan(0);
	await expect(bell(page)).toBeVisible();
	// Nothing yet: the badge is the only text in the bell.
	await expect(bell(page)).not.toContainText(/\d/);
	return socket;
}

test.describe("a notification the browser was not there to hear", () => {
	test("assigned while the browser is offline, it is in the bell when it is back", async ({
		page,
		context,
		request,
		playwright,
	}) => {
		test.setTimeout(120_000);
		const a = await arrange(request, playwright);
		await openApp(page, a.username);

		await context.setOffline(true);
		await assign(request, a);
		await context.setOffline(false);

		await expect(bell(page)).toContainText("1", { timeout: 30_000 });
	});

	test("the announcement is lost with the socket still up: the next re-read finds it", async ({
		page,
		request,
		playwright,
	}) => {
		// The poll runs once a minute while the tab is visible.
		test.setTimeout(150_000);
		const a = await arrange(request, playwright);

		// Swallow the one announcement; everything else passes through.
		let swallowed = 0;
		let received = 0;
		await page.routeWebSocket(/\/ws\/socket\.io/, (ws) => {
			const server = ws.connectToServer();
			ws.onMessage((message) => server.send(message));
			server.onMessage((message) => {
				received++;
				if (
					typeof message === "string" &&
					message.includes("notification.created")
				) {
					swallowed++;
					return;
				}
				ws.send(message);
			});
		});

		await signIn(page, a.username, RESTRICTED_PASSWORD);
		await expect(bell(page)).toBeVisible();
		// The socket is up once the server has spoken to it (its handshake).
		await expect.poll(() => received, { timeout: 20_000 }).toBeGreaterThan(0);
		await assign(request, a);

		// The announcement arrives, is dropped, and the bell stays empty.
		await expect.poll(() => swallowed, { timeout: 20_000 }).toBeGreaterThan(0);
		await expect(bell(page)).not.toContainText(/\d/);

		// Then the periodic re-read shows it, with no socket event.
		await expect(bell(page)).toContainText("1", { timeout: 90_000 });
	});
});

// ─── The realtime service itself is down ─────────────────────────────────────

const REALTIME = process.env.E2E_REALTIME_CONTAINER ?? "paca-e2e-realtime-1";

function docker(...args: string[]): boolean {
	try {
		execFileSync("docker", args, { encoding: "utf8", timeout: 60_000 });
		return true;
	} catch {
		return false;
	}
}

test.describe("with the realtime service down", () => {
	// This stops a service every other spec running against the stack relies on,
	// so it is opt-in and meant for a stack nothing else is using.
	test.skip(
		process.env.E2E_ALLOW_STACK_RESTART !== "1",
		"stops the shared stack's realtime service; set E2E_ALLOW_STACK_RESTART=1 on an otherwise idle stack",
	);
	test.skip(
		!docker("inspect", REALTIME),
		`needs docker access to the ${REALTIME} container`,
	);

	test("assigned while it is down, it is in the bell once the app has reconnected", async ({
		page,
		request,
		playwright,
	}) => {
		test.setTimeout(180_000);
		const a = await arrange(request, playwright);
		await openApp(page, a.username);

		try {
			expect(docker("stop", REALTIME)).toBe(true);
			await assign(request, a);
		} finally {
			docker("start", REALTIME);
		}

		// socket.io reconnects with backoff; the catch-up runs on "connect".
		await expect(bell(page)).toContainText("1", { timeout: 90_000 });
	});
});
