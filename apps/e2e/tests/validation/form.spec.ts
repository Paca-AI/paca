import { expect, test } from "../../fixtures";

/**
 * Form validation tests — verify client-side validation behaviour:
 * required-field errors, minimum-length checks, and button enable/disable state.
 */
test.describe("Form Validation", () => {
	test.beforeEach(async ({ context }) => {
		await context.clearCookies();
	});

	test('shows "Username is required" after tabbing out of empty username', async ({
		loginPage,
	}) => {
		await loginPage.usernameInput.click();
		await loginPage.passwordInput.click(); // trigger blur on username
		await expect(
			loginPage.page.getByText("Username is required"),
		).toBeVisible();
	});

	test('shows "Username is required" after clearing a non-empty username', async ({
		loginPage,
	}) => {
		await loginPage.usernameInput.fill("someuser");
		await loginPage.usernameInput.fill("");
		await loginPage.passwordInput.click(); // trigger blur on username
		await expect(
			loginPage.page.getByText("Username is required"),
		).toBeVisible();
	});

	test("shows minimum-length error when username is too short", async ({
		loginPage,
	}) => {
		await loginPage.usernameInput.fill("a");
		await loginPage.passwordInput.fill("b");
		await loginPage.usernameInput.fill(""); // clear to trigger validation
		await expect(
			loginPage.page.getByText("Username must be at least 3 characters"),
		).toBeVisible();
		await expect(loginPage.signInButton).toBeDisabled();
	});

	test("enables sign-in button only when both fields are non-empty", async ({
		loginPage,
	}) => {
		await loginPage.usernameInput.fill(process.env.E2E_USERNAME ?? "admin");
		await expect(loginPage.signInButton).toBeDisabled();

		await loginPage.passwordInput.fill(
			process.env.E2E_PASSWORD ?? "e2e-admin-password",
		);
		await expect(loginPage.signInButton).toBeEnabled();

		await loginPage.passwordInput.fill("");
		await expect(loginPage.signInButton).toBeDisabled();
	});
});
