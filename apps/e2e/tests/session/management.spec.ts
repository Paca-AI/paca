import { expect, test, type Page } from "@playwright/test";
import { AUTH_FILE } from "../../playwright.config";

const USERNAME = process.env.E2E_USERNAME ?? "admin";
const PASSWORD = process.env.E2E_PASSWORD ?? "e2e-admin-password";

function profileMenuButton(page: Page) {
	return page.getByRole("button", { name: /Admin super_admin/i });
}

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
			page.getByRole("heading", { name: /Good (morning|afternoon|evening), Admin/i }),
		).toBeVisible({ timeout: 10000 });

		// Check if we need to open the sidebar on mobile devices
		const viewport = page.viewportSize();
		const isMobile = viewport && viewport.width <= 768;

		if (isMobile) {
			// On mobile, open the sidebar first to access user profile dropdown
			await page.getByRole('button', { name: 'Toggle Sidebar' }).click();
		}

		// Click user profile dropdown, then click logout (using actual sidebar logout functionality)
		await profileMenuButton(page).click();
		await page.getByRole("menuitem", { name: "Log out" }).click();

		await expect(page.getByRole("textbox", { name: "Username" })).toBeVisible();
		await expect(page.getByRole("textbox", { name: "Password" })).toBeVisible();
		await expect(page.getByRole("button", { name: /sign in/i })).toBeVisible();
	});

	test("back button after logout shows login, not home page", async ({
		page,
	}) => {
		await page.goto("/home");
		await expect(
			page.getByRole("heading", { name: /Good (morning|afternoon|evening), Admin/i }),
		).toBeVisible({ timeout: 10000 });

		// Check if we need to open the sidebar on mobile devices
		const viewport = page.viewportSize();
		const isMobile = viewport && viewport.width <= 768;

		if (isMobile) {
			// On mobile, open the sidebar first to access user profile dropdown
			await page.getByRole('button', { name: 'Toggle Sidebar' }).click();
		}

		// Click user profile dropdown, then click logout (using actual sidebar logout functionality)
		await profileMenuButton(page).click();
		await page.getByRole("menuitem", { name: "Log out" }).click();
		
		// Wait for logout to fully complete before going back.
		await expect(page.getByRole("button", { name: /sign in/i })).toBeVisible();

		await page.goBack();
		
		// In case we end up on about:blank, navigate to login explicitly
		if (page.url() === "about:blank" || page.url().includes("about:")) {
			await page.goto("/");
		}

		await expect(page.getByRole("textbox", { name: "Username" })).toBeVisible();
		await expect(page.getByRole("button", { name: /sign in/i })).toBeVisible();
	});

	test("session persists across page reload", async ({ page }) => {
		await page.goto("/home");
		await expect(
			page.getByRole("heading", { name: /Good (morning|afternoon|evening), Admin/i }),
		).toBeVisible({ timeout: 10000 });

		await page.reload();
		await expect(
			page.getByRole("heading", { name: /Good (morning|afternoon|evening), Admin/i }),
		).toBeVisible({ timeout: 10000 });
	});

	test("session is shared across tabs in the same context", async ({
		context,
		page,
	}) => {
		await page.goto("/home");
		await expect(
			page.getByRole("heading", { name: /Good (morning|afternoon|evening), Admin/i }),
		).toBeVisible({ timeout: 10000 });

		const page2 = await context.newPage();
		await page2.goto("/");
		await expect(
			page2.getByRole("heading", { name: /Good (morning|afternoon|evening), Admin/i }),
		).toBeVisible({ timeout: 10000 });
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
		).toBeVisible({ timeout: 10000 });
		await ctx1.close();

		// Create a fresh context and verify the session doesn't carry over.
		const ctx2 = await browser.newContext();
		const page2 = await ctx2.newPage();
		await page2.goto("/");
		await expect(
			page2.getByRole("heading", { name: "Welcome back" }),
		).toBeVisible({ timeout: 10000 });
		await ctx2.close();
	});
});