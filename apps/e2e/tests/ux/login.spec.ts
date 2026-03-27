import { expect, test } from "../../fixtures";

/**
 * UX tests — cover the visible interactions on the login page:
 * error message display, password visibility toggle, "Remember me",
 * theme switching, and mobile layout/touch behaviour.
 *
 * Mobile-layout tests set an explicit viewport rather than relying on a
 * mobile browser project so that the same assertions are meaningful on
 * every desktop CI run too.
 */

test.beforeEach(async ({ context }) => {
	await context.clearCookies();
});

/* ─── Error display ────────────────────────────────────────────────── */

test.describe("Error Display", () => {
	test("shows error for invalid credentials, clears on successful login", async ({
		loginPage,
		page,
	}) => {
		await loginPage.login("invaliduser", "invalidpass");
		await expect(loginPage.errorMessage).toBeVisible();

		await loginPage.fill(
			process.env.E2E_USERNAME ?? "admin",
			process.env.E2E_PASSWORD ?? "e2e-admin-password",
		);
		await loginPage.submit();
		await expect(page).toHaveURL(/\/dashboard/);
	});
});

/* ─── Password visibility toggle ──────────────────────────────────── */

test.describe("Password Visibility", () => {
	test("toggles password field visibility via show/hide button", async ({
		loginPage,
	}) => {
		await loginPage.passwordInput.fill("e2e-admin-password");

		await expect(loginPage.showPasswordButton).toBeVisible();
		await loginPage.showPasswordButton.click();
		await expect(loginPage.hidePasswordButton).toBeVisible();

		await loginPage.hidePasswordButton.click();
		await expect(loginPage.showPasswordButton).toBeVisible();
	});
});

/* ─── Remember me ─────────────────────────────────────────────────── */

test.describe("Remember Me", () => {
	test("remember-me switch is checked after clicking and login succeeds", async ({
		loginPage,
		page,
	}) => {
		await loginPage.rememberMeSwitch.click();
		await expect(loginPage.rememberMeSwitch).toBeChecked();

		await loginPage.login(
			process.env.E2E_USERNAME ?? "admin",
			process.env.E2E_PASSWORD ?? "e2e-admin-password",
		);
		await expect(page).toHaveURL(/\/dashboard/);
	});
});

/* ─── Theme switching ──────────────────────────────────────────────── */

test.describe("Theme Switching", () => {
	test("cycles through auto → light → dark themes without breaking the form", async ({
		loginPage,
	}) => {
		await loginPage.page
			.getByRole("button", { name: "Theme mode: auto (system)." })
			.click();
		await expect(
			loginPage.page.getByRole("button", { name: /Theme mode: light/i }),
		).toBeVisible();

		await loginPage.page
			.getByRole("button", { name: /Theme mode: light/i })
			.click();
		await expect(
			loginPage.page.getByRole("button", { name: /Theme mode: dark/i }),
		).toBeVisible();

		// Form must remain functional in every theme.
		await loginPage.expectFormVisible();
	});
});

/* ─── Mobile layout ────────────────────────────────────────────────── */

test.describe("Mobile Layout", () => {
	test("login form is fully visible at iPhone 8 viewport (375×667)", async ({
		loginPage,
	}) => {
		await loginPage.page.setViewportSize({ width: 375, height: 667 });
		await loginPage.page.goto("/");

		await expect(
			loginPage.page.getByRole("heading", { name: "Welcome back" }),
		).toBeVisible();
		await loginPage.expectFormVisible();

		const hasHorizontalScroll = await loginPage.page.evaluate(
			() => document.body.scrollWidth > document.body.clientWidth,
		);
		expect(hasHorizontalScroll).toBeFalsy();
	});

	test("touch interactions log in successfully on mobile viewport", async ({
		loginPage,
		page,
	}) => {
		await loginPage.page.setViewportSize({ width: 375, height: 667 });
		await loginPage.page.goto("/");

		await loginPage.usernameInput.fill(process.env.E2E_USERNAME ?? "admin");
		await loginPage.passwordInput.fill(
			process.env.E2E_PASSWORD ?? "e2e-admin-password",
		);
		await loginPage.rememberMeSwitch.click();
		await loginPage.submit();

		await expect(
			page.getByRole("heading", { name: /dashboard|home|welcome/i }),
		).toBeVisible();
	});
});
