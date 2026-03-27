import { expect, test } from "../../fixtures";

/**
 * Security tests — verify that the login form handles malicious input
 * safely and returns a generic error without leaking server information.
 *
 * Each injection payload is submitted as the username; the app must respond
 * with the standard "Invalid username or password." message rather than a
 * server error, stack trace, or unexpected output.
 */
test.describe("Login Security", () => {
	test.beforeEach(async ({ context }) => {
		await context.clearCookies();
	});

	const INVALID_CREDS_MSG = "Invalid username or password.";

	test("rejects SQL injection in username", async ({ loginPage }) => {
		await loginPage.login("admin' OR '1'='1", "password");
		await expect(loginPage.page.getByText(INVALID_CREDS_MSG)).toBeVisible();
		await expect(loginPage.page).toHaveURL("/");
	});

	test("rejects XSS payload in username", async ({ loginPage }) => {
		await loginPage.login("<script>alert('xss')</script>", "password");
		await expect(loginPage.page.getByText(INVALID_CREDS_MSG)).toBeVisible();
		await expect(loginPage.page).toHaveURL("/");
	});

	test("rejects LDAP injection in username", async ({ loginPage }) => {
		await loginPage.login("*)(uid=*))(|(uid=*", "password");
		await expect(loginPage.page.getByText(INVALID_CREDS_MSG)).toBeVisible();
		await expect(loginPage.page).toHaveURL("/");
	});

	test("rejects directory traversal in username", async ({ loginPage }) => {
		await loginPage.login("../../../etc/passwd", "password");
		await expect(loginPage.page.getByText(INVALID_CREDS_MSG)).toBeVisible();
		await expect(loginPage.page).toHaveURL("/");
	});
});
