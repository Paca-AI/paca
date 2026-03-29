import { expect, test } from "@playwright/test";
import { AUTH_FILE } from "../../playwright.config";

const USERNAME = process.env.E2E_USERNAME ?? "admin";
const PASSWORD = process.env.E2E_PASSWORD ?? "e2e-admin-password";

/**
 * Session management tests.
 *
 * Two groups:
 *
 * 1. **Pre-authenticated** — use `storageState` (produced by `global-setup.ts`)
 *    to start each test already logged in.  These tests focus on post-login
 *    behaviours: logout, back-button protection, and session persistence.
 *
 * 2. **Fresh-context** — create brand-new browser contexts without stored
 *    auth to verify that closing a browser clears the session.
 */

/* ─── Pre-authenticated tests ─────────────────────────────────────── */

test.describe("Session Management — authenticated", () => {
	test.use({ storageState: AUTH_FILE });

	test("logout redirects to login page", async ({ page }) => {
		await page.goto("/home");
		await expect(
			page.getByRole("heading", { name: /home|welcome/i }),
		).toBeVisible();

		// Update this selector to match the actual logout button in the UI.
		await page.getByRole("button", { name: /temporary logout/i }).click();

		await expect(page.getByRole("textbox", { name: "Username" })).toBeVisible();
		await expect(page.getByRole("textbox", { name: "Password" })).toBeVisible();
		await expect(page.getByRole("button", { name: /sign in/i })).toBeVisible();
	});

	test("back button after logout shows login, not home page", async ({
		page,
	}) => {
		await page.goto("/home");
		await expect(
			page.getByRole("heading", { name: /home|welcome/i }),
		).toBeVisible();

		await page.getByRole("button", { name: /temporary logout/i }).click();
		// Wait for logout to fully complete before going back.
		await expect(page.getByRole("button", { name: /sign in/i })).toBeVisible();

		await page.goBack();
		// The auth guard may redirect asynchronously after loading the cached page;
		// wait for the network to settle before asserting.
		await page.waitForLoadState("networkidle");

		await expect(page.getByRole("textbox", { name: "Username" })).toBeVisible();
		await expect(page.getByRole("button", { name: /sign in/i })).toBeVisible();
	});

	test("session persists across page reload", async ({ page }) => {
		await page.goto("/home");
		await expect(
			page.getByRole("heading", { name: /home|welcome/i }),
		).toBeVisible();

		await page.reload();
		await expect(
			page.getByRole("heading", { name: /home|welcome/i }),
		).toBeVisible();
	});

	test("session is shared across tabs in the same context", async ({
		context,
		page,
	}) => {
		await page.goto("/home");
		await expect(
			page.getByRole("heading", { name: /home|welcome/i }),
		).toBeVisible();

		const page2 = await context.newPage();
		await page2.goto("/");
		await expect(
			page2.getByRole("heading", { name: /home|welcome/i }),
		).toBeVisible();
	});
});

/* ─── Fresh-context tests ─────────────────────────────────────────── */

test.describe("Session Management — fresh context", () => {
	test("session does not persist after browser context is closed", async ({
		browser,
	}) => {
		// Create a context, log in, close it.
		const ctx1 = await browser.newContext();
		const page1 = await ctx1.newPage();
		await page1.goto("/");
		await page1.getByRole("textbox", { name: "Username" }).fill(USERNAME);
		await page1.getByRole("textbox", { name: "Password" }).fill(PASSWORD);
		await page1.getByRole("button", { name: /sign in/i }).click();
		await expect(
			page1.getByRole("heading", { name: /home|welcome/i }),
		).toBeVisible();
		await ctx1.close();

		// Open a fresh context — should require re-login.
		const ctx2 = await browser.newContext();
		const page2 = await ctx2.newPage();
		await page2.goto("/");
		await expect(
			page2.getByRole("textbox", { name: "Username" }),
		).toBeVisible();
		await expect(page2.getByRole("button", { name: /sign in/i })).toBeVisible();
		await ctx2.close();
	});

	test("session is isolated between independent browser contexts", async ({
		browser,
	}) => {
		const ctx = await browser.newContext();
		const page = await ctx.newPage();
		await page.goto("/");

		// A brand-new context should not inherit any auth state.
		await expect(page.getByRole("textbox", { name: "Username" })).toBeVisible();
		await expect(page.getByRole("button", { name: /sign in/i })).toBeVisible();
		await ctx.close();
	});
});
