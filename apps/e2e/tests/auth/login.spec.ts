import { expect, test } from "../../fixtures";

/**
 * Authentication tests — cover the core login flows:
 * valid credentials, invalid credentials, and empty/missing field scenarios.
 *
 * All tests start at `/` via the `loginPage` fixture (pre-navigated, clean state).
 * No stored auth is used so each test runs fully isolated.
 */
test.describe("Authentication", () => {
	test.beforeEach(async ({ context }) => {
		// Clear all browser state to ensure test isolation when running in parallel
		await context.clearCookies();
		await context.clearPermissions();
	});

	test("redirects to home on valid credentials", async ({
		loginPage,
		page,
	}) => {
		await loginPage.login(
			process.env.E2E_USERNAME ?? "admin",
			process.env.E2E_PASSWORD ?? "e2e-admin-password",
		);
		await expect(page).toHaveURL(/\/home/);
	});

	test("shows error on invalid username", async ({ loginPage }) => {
		await loginPage.login("nonexistentuser", "wrongpassword");
		await expect(loginPage.errorMessage).toBeVisible();
		await expect(loginPage.page).toHaveURL("/");
		await expect(loginPage.usernameInput).toHaveValue("nonexistentuser");
		await expect(loginPage.passwordInput).toHaveValue("wrongpassword");
	});

	test("shows error on invalid password", async ({ loginPage }) => {
		await loginPage.login(
			process.env.E2E_USERNAME ?? "admin",
			"wrongpassword123",
		);
		await expect(loginPage.errorMessage).toBeVisible();
		await expect(loginPage.page).toHaveURL("/");
		await expect(loginPage.usernameInput).toHaveValue(
			process.env.E2E_USERNAME ?? "admin",
		);
		await expect(loginPage.passwordInput).toHaveValue("wrongpassword123");
	});

	test("disables sign-in button when both fields are empty", async ({
		loginPage,
	}) => {
		await expect(loginPage.signInButton).toBeDisabled();
	});

	test("disables sign-in button when password is missing", async ({
		loginPage,
	}) => {
		await loginPage.usernameInput.fill(process.env.E2E_USERNAME ?? "admin");
		await expect(loginPage.signInButton).toBeDisabled();
	});
});
