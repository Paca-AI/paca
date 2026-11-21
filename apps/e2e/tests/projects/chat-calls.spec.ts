// spec: features/projects/chat-calls.feature
// seed: tests/seed.spec.ts
//
// Phase 3 of chat: voice channels over LiveKit. Needs the e2e stack started
// (calls are on by default in the e2e Compose file):
//   docker compose -f deploy/docker-compose.e2e.yml up -d --build
// Two real browser contexts join one voice channel with Chromium's fake
// microphone and camera. Skipped when the stack has calls switched off.

import { expect, type Page, test } from "@playwright/test";
import {
	API_URL,
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

const TEST_PREFIX = "E2E_CALLS_";
const RUN_ID = newRunId();

test.use({
	permissions: ["microphone", "camera"],
	launchOptions: {
		args: [
			"--use-fake-device-for-media-stream",
			"--use-fake-ui-for-media-stream",
		],
	},
});

test.describe("Voice channel calls", () => {
	let projectId: string;
	let channelId: string;
	let member: string;

	test.beforeEach(async ({ request, playwright, page }, info) => {
		test.skip(info.project.name !== "chromium", "fake media is Chromium-only");
		await authRequest(request);
		const cfg = await request.get(`${API_URL}/chat/calls/config`);
		test.skip(
			!cfg.ok() || (await cfg.json()).data?.enabled !== true,
			"calls are not enabled on this stack",
		);
		await cleanupProjectsByPrefix(request, TEST_PREFIX);
		await cleanupUsersByPrefix(request, TEST_PREFIX);
		projectId = await createProject(request, `${TEST_PREFIX}PROJECT_${RUN_ID}`);
		const created = await request.post(
			`${API_URL}/projects/${projectId}/channels`,
			{ data: { kind: "voice", name: `lounge${RUN_ID}`.toLowerCase() } },
		);
		expect(created.ok()).toBeTruthy();
		channelId = (await created.json()).data.id as string;
		member = `${TEST_PREFIX}member_${RUN_ID}`;
		await createUserWithProjectPermissions(request, playwright, {
			projectId,
			username: member,
			roleName: `${TEST_PREFIX}role_${RUN_ID}`,
			permissions: {
				"channels:read": true,
				"messages:write": true,
				"calls:join": true,
			},
		});
		await signIn(page);
	});

	test.afterEach(async ({ request }) => {
		await cleanupProjectsByPrefix(request, TEST_PREFIX);
		await cleanupUsersByPrefix(request, TEST_PREFIX);
	});

	const url = () => `${BASE_URL}/projects/${projectId}/chat/${channelId}`;
	const panel = (p: Page) => p.getByRole("region", { name: "Call" });
	const participants = async (p: Page) =>
		(
			await (
				await p.request.get(
					`${API_URL}/projects/${projectId}/channels/${channelId}/call`,
				)
			).json()
		).data.participants as { name: string }[];

	test("two people talk, one moderates the other out, both leave", async ({
		page,
		browser,
	}) => {
		test.setTimeout(120_000);
		const ctx = await browser.newContext({
			permissions: ["microphone", "camera"],
		});
		const other = await ctx.newPage();
		await signIn(other, member, RESTRICTED_PASSWORD);

		await page.goto(url());
		await expect(page.getByText(/Nobody is here yet/)).toBeVisible();
		// The call opens in its own tab.
		const [call] = await Promise.all([
			page.waitForEvent("popup"),
			page.getByRole("button", { name: "Join call" }).first().click(),
		]);
		await expect(panel(call)).toBeVisible();

		// LiveKit's join webhook records the first participant.
		await expect
			.poll(async () => (await participants(page)).length, { timeout: 30_000 })
			.toBe(1);
		await other.goto(url());
		await expect(other.getByText(/1 in call/)).toBeVisible();
		const [otherCall] = await Promise.all([
			other.waitForEvent("popup"),
			other.getByRole("button", { name: "Join call" }).first().click(),
		]);
		await expect(panel(otherCall)).toBeVisible();

		// Each sees the other's tile, with remote media flowing.
		await expect(panel(call).getByText(member)).toBeVisible({
			timeout: 30_000,
		});
		await expect(panel(otherCall).getByText("Admin")).toBeVisible({
			timeout: 30_000,
		});

		// LiveKit's webhooks reached the API: both are recorded in the call, and
		// the main tab shows who is in it and offers a way back.
		await expect
			.poll(async () => (await participants(page)).length, { timeout: 30_000 })
			.toBe(2);
		await expect(
			page.getByRole("button", { name: "Back to call" }).first(),
		).toBeVisible();

		// The admin (calls:moderate through "*") removes the member; that tab closes.
		await panel(call)
			.getByRole("button", { name: `Moderate ${member}` })
			.click();
		await call
			.getByRole("menuitem", { name: `Remove ${member} from the call` })
			.click();
		await expect
			.poll(() => otherCall.isClosed(), { timeout: 30_000 })
			.toBe(true);
		await expect
			.poll(async () => (await participants(page)).length, { timeout: 30_000 })
			.toBe(1);

		await call.getByRole("button", { name: "Leave call" }).click();
		await expect.poll(() => call.isClosed(), { timeout: 30_000 }).toBe(true);
		await expect
			.poll(async () => (await participants(page)).length, { timeout: 30_000 })
			.toBe(0);
		await ctx.close();
	});

	test("a member without calls:join is offered no way in", async ({
		request,
		playwright,
		browser,
	}) => {
		const viewer = `${TEST_PREFIX}viewer_${RUN_ID}`;
		await createUserWithProjectPermissions(request, playwright, {
			projectId,
			username: viewer,
			roleName: `${TEST_PREFIX}viewer_role_${RUN_ID}`,
			permissions: { "channels:read": true },
		});
		const ctx = await browser.newContext();
		const p = await ctx.newPage();
		await signIn(p, viewer, RESTRICTED_PASSWORD);
		await p.goto(url());
		await expect(p.getByRole("heading", { name: /lounge/ })).toBeVisible();
		await expect(p.getByRole("button", { name: "Join call" })).toHaveCount(0);
		await ctx.close();
	});
});
