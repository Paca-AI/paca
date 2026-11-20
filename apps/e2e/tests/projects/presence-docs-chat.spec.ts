// spec: features/projects/presence-docs-chat.feature
// seed: tests/seed.spec.ts
//
// The presence layer beyond tasks: document comments, project channels and
// direct messages. Two browser contexts (the admin and a member) open the same
// thing, so the specs check what a second person actually sees. The members
// here deliberately lack project.members:read, so the names on screen are the
// usernames the realtime service sends.

import { type Browser, expect, type Page, test } from "@playwright/test";
import {
	API_URL,
	BASE_URL,
	cleanupProjectsByPrefix,
	cleanupUsersByPrefix,
	createProject,
	createUserWithPassword,
	createUserWithProjectPermissions,
	newRunId,
	RESTRICTED_PASSWORD,
	signIn,
} from "../helpers/e2e-api";
import { createDoc } from "../helpers/iam";
import { loginContext } from "../helpers/profile-users";
import {
	accessTokenOf,
	closeAll,
	Probe,
	sleep,
	track,
} from "../helpers/realtime";

const TEST_PREFIX = "E2E_PRESENCE2_";
const RUN_ID = newRunId();
const QUIET_MS = 2_000;

let counter = 0;
const uniqueName = (label: string) =>
	`${TEST_PREFIX}${label}_${RUN_ID}${++counter}`;

const typing = (page: Page) => page.getByTestId("typing-indicator");

async function newSignedInPage(
	browser: Browser,
	username: string,
): Promise<{ page: Page; close: () => Promise<void> }> {
	const context = await browser.newContext();
	const page = await context.newPage();
	await signIn(page, username, RESTRICTED_PASSWORD);
	return { page, close: () => context.close() };
}

async function liveJoin(
	probe: Probe,
	ref: { kind: string; projectId: string; id: string },
): Promise<{ ok: boolean; reason?: string; canType?: boolean }> {
	return new Promise((resolve) =>
		probe.socket
			.timeout(10_000)
			.emit(
				"live:join",
				ref,
				(
					_e: unknown,
					res: { ok: boolean; reason?: string; canType?: boolean },
				) => resolve(res),
			),
	);
}

test.describe("Presence in docs and chat", () => {
	let projectId: string;
	let member: string;

	test.beforeEach(async ({ request, playwright, isMobile }) => {
		test.skip(isMobile, "Two-user specs run on desktop viewports only");
		await cleanupProjectsByPrefix(request, TEST_PREFIX);
		await cleanupUsersByPrefix(request, TEST_PREFIX);
		projectId = await createProject(request, uniqueName("PROJECT"));
		member = uniqueName("member").toLowerCase();
		await createUserWithProjectPermissions(request, playwright, {
			projectId,
			username: member,
			roleName: uniqueName("ROLE"),
			// No project.members:read: names must come from the server.
			permissions: {
				"tasks:read": true,
				"docs:read": true,
				"docs:write": true,
				"channels:read": true,
				"messages:write": true,
			},
		});
	});

	test.afterEach(async ({ request }) => {
		await closeAll();
		await cleanupProjectsByPrefix(request, TEST_PREFIX);
		await cleanupUsersByPrefix(request, TEST_PREFIX);
	});

	test.describe("Project scoping", () => {
		test("A room cannot be joined by naming a project the entity is not in", async ({
			request,
			playwright,
		}) => {
			// The member may read docs and tasks in project P; the entities are in Q.
			const other = await createProject(request, uniqueName("OTHER"));
			const otherDoc = await createDoc(request, other, uniqueName("DOC"));
			const task = await request.post(`${API_URL}/projects/${other}/tasks`, {
				data: { title: uniqueName("TASK") },
			});
			const otherTask = (await task.json()).data.id as string;

			const api = track(await loginContext(playwright, member));
			const probe = track(
				await Probe.connect({ token: await accessTokenOf(api) }),
			);
			// The chat channel room (chat:join) has the same rule.
			const otherChannel = await request.post(
				`${API_URL}/projects/${other}/channels`,
				{ data: { kind: "public", name: uniqueName("room").toLowerCase() } },
			);
			const otherChannelId = (await otherChannel.json()).data.id as string;
			const chatAck = await new Promise<{ ok: boolean; reason?: string }>(
				(resolve) =>
					probe.socket
						.timeout(10_000)
						.emit(
							"chat:join",
							{ projectId, channelId: otherChannelId },
							(_e: unknown, res: { ok: boolean; reason?: string }) =>
								resolve(res),
						),
			);
			expect(chatAck).toMatchObject({ ok: false, reason: "forbidden" });

			for (const ref of [
				{ kind: "doc", projectId, id: otherDoc },
				{ kind: "task", projectId, id: otherTask },
			]) {
				expect(await liveJoin(probe, ref)).toEqual({
					ok: false,
					reason: "forbidden",
				});
			}
		});
	});

	test.describe("Documents", () => {
		let docId: string;

		test.beforeEach(async ({ request }) => {
			docId = await createDoc(request, projectId, uniqueName("DOC"));
		});

		async function openDoc(page: Page, withComments = false) {
			await page.goto(`${BASE_URL}/projects/${projectId}/docs/${docId}`);
			if (withComments) {
				await page.getByTitle("Comments & activity").click();
				await expect(
					page.locator('fieldset [contenteditable="true"]').first(),
				).toBeVisible({ timeout: 15_000 });
			} else {
				await expect(
					page
						.getByText(/saved|unsaved/i)
						.or(page.locator("main"))
						.first(),
				).toBeVisible();
			}
		}

		test("A second person opening the doc appears in the header", async ({
			page,
			browser,
		}) => {
			await signIn(page);
			await openDoc(page);
			await expect(page.getByTestId("doc-viewers")).toHaveCount(0);

			const other = await newSignedInPage(browser, member);
			await openDoc(other.page);

			await expect(page.getByTestId("doc-viewers")).toHaveAttribute(
				"title",
				new RegExp(member, "i"),
			);
			await other.close();
		});

		test("Typing a doc comment shows an indicator to the other person", async ({
			page,
			browser,
		}) => {
			await signIn(page);
			await openDoc(page, true);
			const other = await newSignedInPage(browser, member);
			await openDoc(other.page, true);
			await expect(page.getByTestId("doc-viewers")).toBeVisible();

			const box = other.page
				.locator('fieldset [contenteditable="true"]')
				.first();
			await box.click();
			await other.page.keyboard.type("a thought about this doc");
			// Named by username: the member cannot list project members.
			await expect(typing(page)).toHaveText(
				new RegExp(`${member}.* is typing`, "i"),
			);
			await expect(typing(other.page)).toHaveText("");

			for (let i = 0; i < 40; i++) await other.page.keyboard.press("Backspace");
			await expect(typing(page)).toHaveText("", { timeout: 4_000 });
			await other.close();
		});
	});

	test.describe("Chat channels", () => {
		let channelId: string;

		test.beforeEach(async ({ request, browser }) => {
			void browser;
			const created = await request.post(
				`${API_URL}/projects/${projectId}/channels`,
				{ data: { kind: "public", name: uniqueName("room").toLowerCase() } },
			);
			expect(created.ok(), await created.text()).toBeTruthy();
			channelId = (await created.json()).data.id as string;
		});

		async function openChannel(page: Page) {
			await page.goto(`${BASE_URL}/projects/${projectId}/chat/${channelId}`);
			await expect(page.locator("textarea").first()).toBeVisible({
				timeout: 15_000,
			});
		}

		async function joinAs(
			username: string,
			playwright: Parameters<typeof loginContext>[0],
		) {
			const api = track(await loginContext(playwright, username));
			const res = await api.post(
				`${API_URL}/projects/${projectId}/channels/${channelId}/join`,
			);
			expect(res.ok(), await res.text()).toBeTruthy();
			return api;
		}

		test("A second person opening the channel appears in the header", async ({
			page,
			browser,
			playwright,
		}) => {
			await joinAs(member, playwright);
			await signIn(page);
			await openChannel(page);
			const other = await newSignedInPage(browser, member);
			await openChannel(other.page);

			await expect(page.getByTestId("channel-viewers")).toHaveAttribute(
				"title",
				new RegExp(member, "i"),
			);
			await other.close();
		});

		test("Typing a message shows an indicator, and sending clears it", async ({
			page,
			browser,
			playwright,
		}) => {
			await joinAs(member, playwright);
			await signIn(page);
			await openChannel(page);
			const other = await newSignedInPage(browser, member);
			await openChannel(other.page);
			await expect(page.getByTestId("channel-viewers")).toBeVisible();

			const box = other.page.locator("textarea").first();
			await box.fill("");
			await box.pressSequentially("hello there");
			await expect(typing(page)).toHaveText(
				new RegExp(`${member}.* is typing`, "i"),
			);

			await box.press("Enter");
			await expect(typing(page)).toHaveText("", { timeout: 4_000 });
			await other.close();
		});

		test("A read-only member is a viewer but is never announced as typing", async ({
			page,
			browser,
			request,
			playwright,
		}) => {
			const reader = uniqueName("reader").toLowerCase();
			await createUserWithProjectPermissions(request, playwright, {
				projectId,
				username: reader,
				roleName: uniqueName("ROLE"),
				permissions: { "channels:read": true },
			});
			await signIn(page);
			await openChannel(page);

			const other = await newSignedInPage(browser, reader);
			await other.page.goto(
				`${BASE_URL}/projects/${projectId}/chat/${channelId}`,
			);
			await expect(page.getByTestId("channel-viewers")).toHaveAttribute(
				"title",
				new RegExp(reader, "i"),
			);

			const api = track(await loginContext(playwright, reader));
			const probe = track(
				await Probe.connect({ token: await accessTokenOf(api) }),
			);
			const ack = await liveJoin(probe, {
				kind: "channel",
				projectId,
				id: channelId,
			});
			expect(ack).toMatchObject({ ok: true, canType: false });
			probe.socket.emit("live:typing", {
				kind: "channel",
				id: channelId,
				typing: true,
			});
			await sleep(QUIET_MS);
			await expect(typing(page)).toHaveText("");
			await other.close();
		});
	});

	test.describe("Direct messages", () => {
		test("A direct message has no project and still shows who is typing", async ({
			page,
			browser,
			request,
			playwright,
		}) => {
			const admin = await (await request.get(`${API_URL}/users/me`)).json();
			const memberApi = track(await loginContext(playwright, member));
			const started = await memberApi.post(`${API_URL}/chat/dms`, {
				data: { account_id: admin.data.id },
			});
			expect(started.ok(), await started.text()).toBeTruthy();
			const channelId = (await started.json()).data.id as string;

			await signIn(page);
			await page.goto(`${BASE_URL}/chat/dm/${channelId}`);
			await expect(page.locator("textarea").first()).toBeVisible({
				timeout: 15_000,
			});
			const other = await newSignedInPage(browser, member);
			await other.page.goto(`${BASE_URL}/chat/dm/${channelId}`);
			const box = other.page.locator("textarea").first();
			await expect(box).toBeVisible({ timeout: 15_000 });
			await expect(page.getByTestId("channel-viewers")).toBeVisible();

			await box.pressSequentially("psst");
			await expect(typing(page)).toHaveText(
				new RegExp(`${member}.* is typing`, "i"),
			);
			await other.close();
		});

		test("A user outside a direct message cannot join its live room", async ({
			request,
			playwright,
		}) => {
			const admin = await (await request.get(`${API_URL}/users/me`)).json();
			const memberApi = track(await loginContext(playwright, member));
			const started = await memberApi.post(`${API_URL}/chat/dms`, {
				data: { account_id: admin.data.id },
			});
			expect(started.ok(), await started.text()).toBeTruthy();
			const channelId = (await started.json()).data.id as string;

			const stranger = uniqueName("stranger").toLowerCase();
			await createUserWithPassword(request, playwright, {
				username: stranger,
				fullName: stranger,
			});
			const api = track(await loginContext(playwright, stranger));
			const probe = track(
				await Probe.connect({ token: await accessTokenOf(api) }),
			);
			const ack = await liveJoin(probe, {
				kind: "channel",
				projectId: "",
				id: channelId,
			});
			expect(ack).toEqual({ ok: false, reason: "forbidden" });
		});
	});
});
