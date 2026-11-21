// spec: features/auth/sessions.feature
// seed: tests/seed.spec.ts
//
// Signed-in devices on the security page. Each "device" is its own browser
// context (its own cookie jar and User-Agent), so signing one out really is
// invisible to the others until the server says otherwise. Every scenario
// works on throwaway users.

import {
	type Browser,
	type BrowserContext,
	expect,
	type Page,
	test,
} from "@playwright/test";
import { dialogPanel } from "../helpers/dialog";
import {
	API_URL,
	authRequest,
	BASE_URL,
	cleanupUsersByPrefix,
	createUserWithPassword,
	newRunId,
	RESTRICTED_PASSWORD,
	signIn,
} from "../helpers/e2e-api";
import { rawLogin } from "../helpers/mfa";

const PREFIX = "E2E_SESSIONS_";
const RUN_ID = newRunId();
let counter = 0;

// Edge: no Playwright browser project reports as Edge, so this device never
// shares a label with the "This device" row, whatever project runs.
const EDGE_WINDOWS =
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36 Edg/126.0.0.0";
const EDGE_IOS =
	"Mozilla/5.0 (iPhone; CPU iPhone OS 17_5 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 EdgiOS/126.0.0.0 Mobile/15E148 Safari/605.1.15";

function uniqueName(label: string): string {
	counter += 1;
	return `${PREFIX}${label}_${RUN_ID}_${counter}`;
}

test.beforeEach(async ({ request }) => {
	await cleanupUsersByPrefix(request, PREFIX);
});

test.afterEach(async ({ request }) => {
	await cleanupUsersByPrefix(request, PREFIX);
});

async function newUser(
	request: Parameters<typeof authRequest>[0],
	playwright: Parameters<typeof createUserWithPassword>[1],
	label: string,
) {
	const username = uniqueName(label);
	await authRequest(request);
	await createUserWithPassword(request, playwright, {
		username,
		fullName: username,
	});
	return username;
}

/** The "Signed-in devices" card on the security page. */
function devicesCard(page: Page) {
	return page
		.locator('[data-slot="card"]')
		.filter({ has: page.getByText("Signed-in devices", { exact: true }) });
}

async function openSecurity(page: Page) {
	await page.goto(`${BASE_URL}/account/security`);
	await expect(page.getByRole("heading", { name: "Security" })).toBeVisible();
	await expect(devicesCard(page)).toBeVisible();
}

/** Another device: a separate browser context signed in as `username`. */
async function signInOnNewDevice(
	browser: Browser,
	username: string,
	userAgent: string,
): Promise<{ context: BrowserContext; page: Page }> {
	const context = await browser.newContext({ userAgent });
	const page = await context.newPage();
	await signIn(page, username, RESTRICTED_PASSWORD);
	return { context, page };
}

async function confirmSignOut(page: Page) {
	const dialog = dialogPanel(page);
	await dialog.getByRole("button", { name: "Sign out", exact: true }).click();
	await expect(dialog).toBeHidden();
}

/** Whether `context` is still authenticated, judged by a real API call. */
async function whoAmIStatus(context: BrowserContext): Promise<number> {
	return (await context.request.get(`${API_URL}/users/me`)).status();
}

test.describe("Seeing devices", () => {
	test("the current device is listed and cannot be signed out from the list", async ({
		page,
		request,
		playwright,
	}) => {
		const username = await newUser(request, playwright, "SELF");
		await signIn(page, username, RESTRICTED_PASSWORD);
		await openSecurity(page);

		const card = devicesCard(page);
		await expect(card.getByText("This device")).toBeVisible();
		await expect(card.getByRole("listitem")).toHaveCount(1);
		await expect(card.getByRole("button")).toHaveCount(0);
	});

	test("another device appears with its browser and system", async ({
		page,
		browser,
		request,
		playwright,
	}) => {
		const username = await newUser(request, playwright, "SEE");
		await signIn(page, username, RESTRICTED_PASSWORD);
		const other = await signInOnNewDevice(browser, username, EDGE_WINDOWS);
		try {
			await openSecurity(page);
			const card = devicesCard(page);
			await expect(card.getByRole("listitem")).toHaveCount(2);
			await expect(card.getByText("Edge on Windows")).toBeVisible();

			// Seen from the other device, roles swap.
			await openSecurity(other.page);
			const row = devicesCard(other.page)
				.getByRole("listitem")
				.filter({ hasText: "Edge on Windows" });
			await expect(row.getByText("This device")).toBeVisible();
		} finally {
			await other.context.close();
		}
	});

	test("a device that signs out disappears from the list", async ({
		page,
		browser,
		request,
		playwright,
	}) => {
		const username = await newUser(request, playwright, "GONE");
		await signIn(page, username, RESTRICTED_PASSWORD);
		const other = await signInOnNewDevice(browser, username, EDGE_WINDOWS);
		try {
			await openSecurity(page);
			await expect(devicesCard(page).getByRole("listitem")).toHaveCount(2);

			const out = await other.context.request.post(`${API_URL}/auth/logout`);
			expect(out.ok()).toBeTruthy();

			await openSecurity(page);
			await expect(devicesCard(page).getByRole("listitem")).toHaveCount(1);
			await expect(
				devicesCard(page).getByText("Edge on Windows"),
			).toHaveCount(0);
		} finally {
			await other.context.close();
		}
	});
});

test.describe("Signing a device out", () => {
	test("signing out another device ends it immediately", async ({
		page,
		browser,
		request,
		playwright,
	}) => {
		const username = await newUser(request, playwright, "REVOKE");
		await signIn(page, username, RESTRICTED_PASSWORD);
		const other = await signInOnNewDevice(browser, username, EDGE_WINDOWS);
		try {
			expect(await whoAmIStatus(other.context)).toBe(200);

			await openSecurity(page);
			await page
				.getByRole("button", { name: "Sign out Edge on Windows" })
				.click();
			await confirmSignOut(page);

			const card = devicesCard(page);
			await expect(card.getByText("Edge on Windows")).toHaveCount(0);

			// Its access token is nowhere near expiry, yet it no longer works.
			expect(await whoAmIStatus(other.context)).toBe(401);
			await other.page.reload();
			await expect(
				other.page.getByRole("textbox", { name: "Username" }),
			).toBeVisible();

			// This device is unaffected.
			expect(await whoAmIStatus(page.context())).toBe(200);
			await expect(card.getByText("This device")).toBeVisible();
		} finally {
			await other.context.close();
		}
	});

	test("cancelling the confirmation leaves the device signed in", async ({
		page,
		browser,
		request,
		playwright,
	}) => {
		const username = await newUser(request, playwright, "CANCEL");
		await signIn(page, username, RESTRICTED_PASSWORD);
		const other = await signInOnNewDevice(browser, username, EDGE_WINDOWS);
		try {
			await openSecurity(page);
			await page
				.getByRole("button", { name: "Sign out Edge on Windows" })
				.click();
			const dialog = dialogPanel(page);
			await dialog.getByRole("button", { name: "Cancel" }).click();
			await expect(dialog).toBeHidden();

			await expect(
				devicesCard(page).getByText("Edge on Windows"),
			).toBeVisible();
			expect(await whoAmIStatus(other.context)).toBe(200);
		} finally {
			await other.context.close();
		}
	});

	test("signing out all other devices keeps only the current one", async ({
		page,
		browser,
		request,
		playwright,
	}) => {
		const username = await newUser(request, playwright, "ALL");
		await signIn(page, username, RESTRICTED_PASSWORD);
		const desktop = await signInOnNewDevice(browser, username, EDGE_WINDOWS);
		const phone = await signInOnNewDevice(browser, username, EDGE_IOS);
		try {
			await openSecurity(page);
			const card = devicesCard(page);
			await expect(card.getByRole("listitem")).toHaveCount(3);
			await expect(card.getByText("Edge on iOS")).toBeVisible();

			await page
				.getByRole("button", { name: "Sign out all other devices" })
				.click();
			await confirmSignOut(page);

			await expect(card.getByRole("listitem")).toHaveCount(1);
			await expect(card.getByText("This device")).toBeVisible();
			await expect(
				page.getByRole("button", { name: "Sign out all other devices" }),
			).toHaveCount(0);

			expect(await whoAmIStatus(desktop.context)).toBe(401);
			expect(await whoAmIStatus(phone.context)).toBe(401);
			expect(await whoAmIStatus(page.context())).toBe(200);
		} finally {
			await desktop.context.close();
			await phone.context.close();
		}
	});
});

test.describe("Sessions are private to their account", () => {
	test("another account cannot end a session it does not own", async ({
		request,
		playwright,
	}) => {
		const alice = await newUser(request, playwright, "ALICE");
		const bob = await newUser(request, playwright, "BOB");
		const { ctx: aliceCtx } = await rawLogin(playwright, alice);
		const { ctx: bobCtx } = await rawLogin(playwright, bob);
		try {
			const aliceSessions = (
				await (await aliceCtx.get(`${API_URL}/users/me/sessions`)).json()
			).data as { id: string }[];
			expect(aliceSessions).toHaveLength(1);
			const aliceSessionId = aliceSessions[0].id;

			const attempt = await bobCtx.delete(
				`${API_URL}/users/me/sessions/${aliceSessionId}`,
			);
			expect(attempt.status()).toBe(404);
			expect((await attempt.json()).error_code).toBe("SESSION_NOT_FOUND");

			// Exactly what an id that never existed looks like.
			const unknown = await bobCtx.delete(
				`${API_URL}/users/me/sessions/00000000-0000-4000-8000-000000000000`,
			);
			expect(unknown.status()).toBe(404);
			expect((await unknown.json()).error_code).toBe("SESSION_NOT_FOUND");

			expect((await aliceCtx.get(`${API_URL}/users/me`)).status()).toBe(200);
			const bobSessions = (
				await (await bobCtx.get(`${API_URL}/users/me/sessions`)).json()
			).data as { id: string }[];
			expect(bobSessions.map((s) => s.id)).not.toContain(aliceSessionId);
		} finally {
			await aliceCtx.dispose();
			await bobCtx.dispose();
		}
	});

	test("a malformed session id is simply not found", async ({
		request,
		playwright,
	}) => {
		const username = await newUser(request, playwright, "BADID");
		const { ctx } = await rawLogin(playwright, username);
		try {
			for (const id of ["not-a-session", "user:anything"]) {
				const res = await ctx.delete(`${API_URL}/users/me/sessions/${id}`);
				expect(res.status(), id).toBe(404);
				expect((await res.json()).error_code).toBe("SESSION_NOT_FOUND");
			}
			expect((await ctx.get(`${API_URL}/users/me`)).status()).toBe(200);
		} finally {
			await ctx.dispose();
		}
	});
});
