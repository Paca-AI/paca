// spec: features/projects/task-presence.feature
// seed: tests/seed.spec.ts
//
// Two browser contexts (the admin and a member) open the same task, so the
// specs check what a second person actually sees. Presence is pushed over the
// realtime socket and the typing lease is a few seconds long, so assertions
// rely on Playwright's auto-retry; "not shown" assertions use a short quiet
// window.

import { type Browser, expect, type Page, test } from "@playwright/test";
import {
	API_URL,
	BASE_URL,
	cleanupProjectsByPrefix,
	cleanupUsersByPrefix,
	createProject,
	createUserWithProjectPermissions,
	newRunId,
	RESTRICTED_PASSWORD,
	signIn,
} from "../helpers/e2e-api";
import { loginContext } from "../helpers/profile-users";
import {
	accessTokenOf,
	closeAll,
	Probe,
	sleep,
	track,
} from "../helpers/realtime";

const TEST_PREFIX = "E2E_PRESENCE_";
const RUN_ID = newRunId();
const QUIET_MS = 2_000;

let counter = 0;
const uniqueName = (label: string) =>
	`${TEST_PREFIX}${label}_${RUN_ID}${++counter}`;

const composer = (page: Page) =>
	page.locator('fieldset [contenteditable="true"]').first();
const viewers = (page: Page) => page.getByTestId("task-viewers");
const typing = (page: Page) => page.getByTestId("typing-indicator");

async function openTask(page: Page, projectId: string, taskId: string) {
	await page.goto(`${BASE_URL}/projects/${projectId}/tasks/${taskId}`);
	await expect(composer(page)).toBeVisible({ timeout: 15_000 });
}

async function newSignedInPage(
	browser: Browser,
	username: string,
	password: string,
	init?: () => void,
) {
	const context = await browser.newContext();
	if (init) await context.addInitScript(init);
	const page = await context.newPage();
	await signIn(page, username, password);
	return { context, page };
}

test.describe("Task presence", () => {
	let projectId: string;
	let taskId: string;
	let editor: { userId: string; username: string };
	let adminName: string;

	test.beforeEach(async ({ request, playwright, isMobile }) => {
		test.skip(isMobile, "Two-user specs run on desktop viewports only");
		await cleanupProjectsByPrefix(request, TEST_PREFIX);
		await cleanupUsersByPrefix(request, TEST_PREFIX);
		projectId = await createProject(request, uniqueName("PROJECT"));
		const created = await request.post(
			`${API_URL}/projects/${projectId}/tasks`,
			{ data: { title: uniqueName("TASK") } },
		);
		expect(created.ok()).toBeTruthy();
		taskId = (await created.json()).data.id as string;

		const username = uniqueName("editor").toLowerCase();
		const { userId } = await createUserWithProjectPermissions(
			request,
			playwright,
			{
				projectId,
				username,
				roleName: uniqueName("ROLE"),
				permissions: { "tasks:read": true, "tasks:write": true },
			},
		);
		editor = { userId, username };
		const me = await request.get(`${API_URL}/users/me`);
		adminName = (await me.json()).data.full_name as string;
	});

	test.afterEach(async ({ request }) => {
		await closeAll();
		await cleanupProjectsByPrefix(request, TEST_PREFIX);
		await cleanupUsersByPrefix(request, TEST_PREFIX);
	});

	test.describe("Viewers and typing", () => {
		test("A second person opening the task appears as a viewer", async ({
			page,
			browser,
		}) => {
			await signIn(page);
			await openTask(page, projectId, taskId);
			// Alone: nobody else to show.
			await expect(viewers(page)).toHaveCount(0);

			const other = await newSignedInPage(
				browser,
				editor.username,
				RESTRICTED_PASSWORD,
			);
			await openTask(other.page, projectId, taskId);

			await expect(viewers(page)).toBeVisible();
			await expect(viewers(page)).toHaveAttribute(
				"title",
				new RegExp(editor.username, "i"),
			);
			// The member sees the admin, and not themselves.
			await expect(viewers(other.page)).toHaveAttribute(
				"title",
				new RegExp(adminName),
			);
			await expect(viewers(other.page)).not.toHaveAttribute(
				"title",
				new RegExp(editor.username, "i"),
			);
			await other.context.close();
		});

		test("Typing a comment shows a typing indicator to the other person", async ({
			page,
			browser,
		}) => {
			await signIn(page);
			await openTask(page, projectId, taskId);
			const other = await newSignedInPage(
				browser,
				editor.username,
				RESTRICTED_PASSWORD,
			);
			await openTask(other.page, projectId, taskId);
			await expect(viewers(page)).toBeVisible();

			await composer(other.page).click();
			await other.page.keyboard.type("working on a reply");
			await expect(typing(page)).toHaveText(
				new RegExp(`${editor.username}.* is typing`, "i"),
			);
			// You never see your own typing.
			await expect(typing(other.page)).toHaveText("");

			// Clearing the composer stops it at once, not after the lease.
			await other.page.keyboard.press("ControlOrMeta+A");
			await other.page.keyboard.press("Backspace");
			await expect(typing(page)).toHaveText("", { timeout: 4_000 });
			await other.context.close();
		});

		test("The typing indicator lapses on its own when typing just stops", async ({
			page,
			browser,
		}) => {
			await signIn(page);
			await openTask(page, projectId, taskId);
			const other = await newSignedInPage(
				browser,
				editor.username,
				RESTRICTED_PASSWORD,
			);
			await openTask(other.page, projectId, taskId);
			await composer(other.page).click();
			await other.page.keyboard.type("x");
			await expect(typing(page)).toHaveText(/is typing/i);
			// No further keystrokes: the 5 s lease expires.
			await expect(typing(page)).toHaveText("", { timeout: 12_000 });
			await other.context.close();
		});

		test("A member who turned typing broadcast off is not announced", async ({
			page,
			browser,
		}) => {
			await signIn(page);
			await openTask(page, projectId, taskId);
			const other = await newSignedInPage(
				browser,
				editor.username,
				RESTRICTED_PASSWORD,
				() => localStorage.setItem("paca:typing-broadcast", "off"),
			);
			await openTask(other.page, projectId, taskId);
			await expect(viewers(page)).toBeVisible();

			await composer(other.page).click();
			await other.page.keyboard.type("nobody needs to know");
			await sleep(QUIET_MS);
			await expect(typing(page)).toHaveText("");
			// Still listed as a viewer.
			await expect(viewers(page)).toBeVisible();
			await other.context.close();
		});

		test("Leaving the task removes the viewer", async ({ page, browser }) => {
			await signIn(page);
			await openTask(page, projectId, taskId);
			const other = await newSignedInPage(
				browser,
				editor.username,
				RESTRICTED_PASSWORD,
			);
			await openTask(other.page, projectId, taskId);
			await expect(viewers(page)).toBeVisible();

			await other.page.goto(`${BASE_URL}/projects/${projectId}`);
			await expect(viewers(page)).toHaveCount(0, { timeout: 10_000 });
			await other.context.close();
		});
	});

	test.describe("Access is checked at join", () => {
		test("A read-only member is a viewer but cannot send typing", async ({
			page,
			browser,
			request,
			playwright,
		}) => {
			const readOnly = uniqueName("reader").toLowerCase();
			await createUserWithProjectPermissions(request, playwright, {
				projectId,
				username: readOnly,
				roleName: uniqueName("ROLE"),
				permissions: { "tasks:read": true },
			});
			await signIn(page);
			await openTask(page, projectId, taskId);

			const other = await newSignedInPage(
				browser,
				readOnly,
				RESTRICTED_PASSWORD,
			);
			await other.page.goto(
				`${BASE_URL}/projects/${projectId}/tasks/${taskId}`,
			);
			await expect(viewers(page)).toHaveAttribute(
				"title",
				new RegExp(readOnly, "i"),
			);

			// The UI offers no composer to a read-only member; force a typing
			// message over their own socket to check the server ignores it too.
			const api = track(await loginContext(playwright, readOnly));
			const probe = track(
				await Probe.connect({ token: await accessTokenOf(api) }),
			);
			const ack = await new Promise<{ ok: boolean; canType?: boolean }>(
				(resolve) =>
					probe.socket
						.timeout(10_000)
						.emit(
							"live:join",
							{ kind: "task", projectId, id: taskId },
							(_e: unknown, res: { ok: boolean; canType?: boolean }) =>
								resolve(res),
						),
			);
			expect(ack.ok).toBe(true);
			expect(ack.canType).toBe(false);
			probe.socket.emit("live:typing", {
				kind: "task",
				id: taskId,
				typing: true,
			});
			await sleep(QUIET_MS);
			await expect(typing(page)).toHaveText("");
			await other.context.close();
		});

		test("A user who cannot read the task cannot join its live room", async ({
			request,
			playwright,
		}) => {
			const outsider = uniqueName("outsider").toLowerCase();
			const other = await createProject(request, uniqueName("OTHER"));
			await createUserWithProjectPermissions(request, playwright, {
				projectId: other,
				username: outsider,
				roleName: uniqueName("ROLE"),
				permissions: { "tasks:read": true },
			});
			const api = track(await loginContext(playwright, outsider));
			const probe = track(
				await Probe.connect({ token: await accessTokenOf(api) }),
			);
			const ack = await new Promise<{ ok: boolean; reason?: string }>(
				(resolve) =>
					probe.socket
						.timeout(10_000)
						.emit(
							"live:join",
							{ kind: "task", projectId, id: taskId },
							(_e: unknown, res: { ok: boolean; reason?: string }) =>
								resolve(res),
						),
			);
			expect(ack).toEqual({ ok: false, reason: "forbidden" });

			// A malformed ref is refused without a lookup.
			const bad = await new Promise<{ ok: boolean; reason?: string }>(
				(resolve) =>
					probe.socket
						.timeout(10_000)
						.emit(
							"live:join",
							{ kind: "task", projectId: "nope", id: taskId },
							(_e: unknown, res: { ok: boolean; reason?: string }) =>
								resolve(res),
						),
			);
			expect(bad).toEqual({ ok: false, reason: "invalid" });
		});
	});
});
