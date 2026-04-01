// spec: features/auth/change-password.feature
// seed: tests/seed.spec.ts

import {
	expect,
	test,
	type APIRequestContext,
	type Locator,
	type Page,
} from "@playwright/test";

const AUTH_URL = `${process.env.E2E_BASE_URL ?? "http://localhost"}/api/v1/auth/login`;
const USERS_URL = `${process.env.E2E_BASE_URL ?? "http://localhost"}/api/v1/admin/users`;
const ADMIN_USERNAME = process.env.E2E_USERNAME ?? "admin";
const ADMIN_PASSWORD = process.env.E2E_PASSWORD ?? "e2e-admin-password";
const MOBILE_BREAKPOINT = 768;
const TEST_USER_PREFIX = "PWD_CHANGE_AUTH_";
const TEST_RUN_ID = Date.now().toString(36).slice(-6).toUpperCase();

type UserRole = "ADMIN" | "SUPER_ADMIN" | "USER";

type AdminUser = {
	id: string;
	username: string;
	full_name: string;
	role: UserRole;
	must_change_password: boolean;
	created_at: string;
};

function uniqueUsername(label: string) {
	return `${TEST_USER_PREFIX}${label}_${TEST_RUN_ID}`;
}

async function authenticateAdmin(request: APIRequestContext) {
	const response = await request.post(AUTH_URL, {
		data: {
			username: ADMIN_USERNAME,
			password: ADMIN_PASSWORD,
			rememberMe: false,
		},
	});

	expect(response.ok()).toBeTruthy();
}

async function listUsers(request: APIRequestContext): Promise<AdminUser[]> {
	const response = await request.get(USERS_URL);
	expect(response.ok()).toBeTruthy();

	const body = await response.json();
	return body.data.items ?? [];
}

async function cleanupPasswordChangeUsers(request: APIRequestContext) {
	const users = await listUsers(request);

	await Promise.all(
		users
			.filter((user) => user.username.startsWith(TEST_USER_PREFIX))
			.map(async (user) => {
				const response = await request.delete(`${USERS_URL}/${user.id}`);
				expect(response.ok()).toBeTruthy();
			}),
	);
}

async function expectLoginPage(page: Page) {
	await expect(page.getByRole("textbox", { name: "Username" })).toBeVisible();
	await expect(page.getByRole("textbox", { name: "Password" })).toBeVisible();
	await expect(page.getByRole("button", { name: "Sign in" })).toBeVisible();
}

async function login(page: Page, username: string, password: string) {
	await page.goto("/");
	await expectLoginPage(page);
	await page.getByRole("textbox", { name: "Username" }).fill(username);
	await page.getByRole("textbox", { name: "Password" }).fill(password);
	await page.getByRole("button", { name: "Sign in" }).click();
}

async function loginAsAdmin(page: Page) {
	await login(page, ADMIN_USERNAME, ADMIN_PASSWORD);
	await expectHomePage(page, "Admin");
}

async function openUsersPage(page: Page) {
	await page.goto("/admin/users");
	await expect(page.getByRole("heading", { name: "User Management" })).toBeVisible();
}

function userRow(page: Page, username: string): Locator {
	return page.getByRole("row").filter({
		has: page.getByText(username, { exact: true }),
	});
}

function showsForcedPasswordGuidance(page: Page) {
	const viewport = page.viewportSize();
	return !viewport || viewport.width > MOBILE_BREAKPOINT;
	}

function profileMenuButton(page: Page): Locator {
	return page.getByRole("button", { name: /Admin super_admin/i });
}

async function openAdminProfileMenu(page: Page) {
	const viewport = page.viewportSize();

	if (viewport && viewport.width <= MOBILE_BREAKPOINT) {
		const menuButton = profileMenuButton(page);
		const isVisible = await menuButton.isVisible().catch(() => false);

		if (!isVisible) {
			await page.getByRole("button", { name: "Toggle Sidebar" }).first().click();
		}
	}

	await profileMenuButton(page).click();
}

async function signOutAdmin(page: Page) {
	await openAdminProfileMenu(page);
	await page.getByRole("menuitem", { name: "Log out" }).click();
	await expectLoginPage(page);
}

async function createUserThroughAdmin(
	page: Page,
	username: string,
	fullName: string,
) {
	await loginAsAdmin(page);

	// 1. Click the New User action and open the creation dialog.
	await openUsersPage(page);
	await page.getByRole("button", { name: "New User" }).click();

	const dialog = page.getByRole("dialog", { name: "Create User" });

	// 2. Fill the new user's account details and submit the form.
	await dialog.getByRole("textbox", { name: "Username" }).fill(username);
	await dialog.getByRole("textbox", { name: "Full Name" }).fill(fullName);
	await dialog.getByRole("button", { name: "Create user" }).click();

	const successDialog = page.getByRole("dialog", { name: "User created" });

	// 3. Capture the one-time temporary password for the new account.
	await expect(successDialog).toBeVisible();
	const temporaryPassword = await successDialog.getByRole("textbox").inputValue();
	await expect(temporaryPassword).not.toEqual("");
	await successDialog.getByRole("button", { name: "Done" }).click();

	await expect(userRow(page, username)).toBeVisible();
	return temporaryPassword;
}

async function resetUserPasswordThroughAdmin(page: Page, username: string) {
	// 1. Open the reset-password confirmation dialog for the selected user.
	const row = userRow(page, username);
	await row.getByRole("button", { name: "Reset password" }).click();

	let dialog = page.getByRole("dialog", { name: "Reset password" });
	await expect(dialog).toBeVisible();

	// 2. Confirm the reset and capture the newly generated temporary password.
	await dialog.getByRole("button", { name: "Reset password" }).click();
	dialog = page.getByRole("dialog", { name: "Reset password" });
	await expect(dialog.getByText(`Password for ${username} has been reset.`)).toBeVisible();

	const temporaryPassword = await dialog.getByRole("textbox").inputValue();
	await expect(temporaryPassword).not.toEqual("");
	await dialog.getByRole("button", { name: "Done" }).click();

	return temporaryPassword;
}

async function expectForcedPasswordChangePage(page: Page) {
	await expect(page).toHaveURL(/\/change-password$/);
	await expect(page.getByRole("heading", { name: "Set new password" })).toBeVisible();
	await expect(
		page.getByText(
			"Enter your temporary password and choose a new one to unlock your account.",
		),
	).toBeVisible();

	if (showsForcedPasswordGuidance(page)) {
		await expect(page.getByRole("heading", { name: "Secure your account" })).toBeVisible();
		await expect(
			page.getByText(
				"Your administrator has assigned you a temporary password. You must set a new personal password before you can access the workspace.",
			),
		).toBeVisible();
		await expect(
			page.getByText("Enter the temporary password you received"),
		).toBeVisible();
		await expect(page.getByText("Choose a strong new password")).toBeVisible();
		await expect(
			page.getByText("Sign in again with your new password"),
		).toBeVisible();
	}
}

async function expectChangePasswordForm(page: Page) {
	await expect(
		page.getByRole("textbox", { name: "Current password" }),
	).toBeVisible();
	await expect(
		page.getByRole("textbox", { name: "New password", exact: true }),
	).toBeVisible();
	await expect(
		page.getByRole("textbox", { name: "Confirm new password" }),
	).toBeVisible();
	await expect(
		page.getByRole("button", { name: "Show current password" }),
	).toBeVisible();
	await expect(
		page.getByRole("button", { name: "Show new password" }),
	).toBeVisible();
	await expect(
		page.getByRole("button", { name: "Show confirm password" }),
	).toBeVisible();
	await expect(
		page.getByRole("button", { name: "Change password" }),
	).toBeDisabled();
}

async function expectHomePage(page: Page, fullName: string) {
	await expect(page).toHaveURL(/\/home$/);
	await expect(
		page.getByRole("heading", {
			name: new RegExp(`Good (morning|afternoon|evening), ${fullName}`, "i"),
		}),
	).toBeVisible({ timeout: 10000 });
}

async function startForcedPasswordChangeFlow(
	page: Page,
	username: string,
	fullName: string,
) {
	const temporaryPassword = await createUserThroughAdmin(page, username, fullName);

	// 4. Start from a clean unauthenticated session and sign in with the temporary password.
	await signOutAdmin(page);
	await login(page, username, temporaryPassword);
	await expectForcedPasswordChangePage(page);

	return { temporaryPassword };
}

async function changePassword(
	page: Page,
	currentPassword: string,
	newPassword: string,
) {
	await page.getByRole("textbox", { name: "Current password" }).fill(currentPassword);
	await page
		.getByRole("textbox", { name: "New password", exact: true })
		.fill(newPassword);
	await page.getByRole("textbox", { name: "Confirm new password" }).fill(newPassword);
	await page.getByRole("button", { name: "Change password" }).click();
	await expectLoginPage(page);
}

test.describe("Forced password change", () => {
	test.beforeEach(async ({ context, request }) => {
		await context.clearCookies();
		await context.clearPermissions();
		await authenticateAdmin(request);
		await cleanupPasswordChangeUsers(request);
	});

	test.afterEach(async ({ request }) => {
		await authenticateAdmin(request);
		await cleanupPasswordChangeUsers(request);
	});

	test("First sign-in after account creation requires a password change", async ({
		page,
	}) => {
		const username = uniqueUsername("CREATE");
		const fullName = "Password Change Create";

		// 1. Create a new user and store the generated temporary password.
		const temporaryPassword = await createUserThroughAdmin(page, username, fullName);

		// 2. Start from a clean unauthenticated session.
		await signOutAdmin(page);

		// 3. Sign in with the temporary password.
		await login(page, username, temporaryPassword);

		// 4. Verify the user is redirected to the change-password page.
		await expect(page).toHaveURL(/\/change-password$/);

		// 5. Verify the change-password form is shown.
		await expect(page.getByRole("heading", { name: "Set new password" })).toBeVisible();
		await expect(
			page.getByText(
				"Enter your temporary password and choose a new one to unlock your account.",
			),
		).toBeVisible();

		// 6. Verify the guidance panel is present on larger viewports.
		if (showsForcedPasswordGuidance(page)) {
			await expect(
				page.getByRole("heading", { name: "Secure your account" }),
			).toBeVisible();
			await expect(
				page.getByText(
					"Your administrator has assigned you a temporary password. You must set a new personal password before you can access the workspace.",
				),
			).toBeVisible();
			await expect(
				page.getByText("Enter the temporary password you received"),
			).toBeVisible();
			await expect(page.getByText("Choose a strong new password")).toBeVisible();
			await expect(
				page.getByText("Sign in again with your new password"),
			).toBeVisible();
		}

		// 7. Verify the workspace home page is not visible.
		await expect(page).not.toHaveURL(/\/home$/);
		await expect(
			page.getByRole("heading", {
				name: /Good (morning|afternoon|evening),/i,
			}),
		).toHaveCount(0);
	});

	test("First sign-in after a password reset requires a password change", async ({
		page,
	}) => {
		const username = uniqueUsername("RESET");
		const fullName = "Password Change Reset";

		// 1. Create a user, reset the password, and store the generated temporary password.
		await createUserThroughAdmin(page, username, fullName);
		const resetTemporaryPassword = await resetUserPasswordThroughAdmin(page, username);

		// 2. Start from a clean unauthenticated session.
		await signOutAdmin(page);

		// 3. Sign in with the reset temporary password.
		await login(page, username, resetTemporaryPassword);

		// 4. Verify the user is redirected to the change-password page.
		await expect(page).toHaveURL(/\/change-password$/);

		// 5. Verify the Set new password form is visible.
		await expect(page.getByRole("heading", { name: "Set new password" })).toBeVisible();
		await expectChangePasswordForm(page);

		// 6. Verify the workspace home page is not visible.
		await expect(page).not.toHaveURL(/\/home$/);
		await expect(
			page.getByRole("heading", {
				name: /Good (morning|afternoon|evening),/i,
			}),
		).toHaveCount(0);
	});

	test("The form shows the required fields and password visibility controls", async ({
		page,
	}) => {
		const username = uniqueUsername("FIELDS");
		const fullName = "Password Change Fields";

		// 1. Start on the change-password page after signing in with a temporary password.
		await startForcedPasswordChangeFlow(page, username, fullName);

		// 2. Verify the form fields and visibility controls are visible.
		await expectChangePasswordForm(page);
	});

	test("The form remains blocked until all password fields are complete and matching", async ({
		page,
	}) => {
		const username = uniqueUsername("VALIDATION");
		const fullName = "Password Change Validation";
		const nextPassword = "PasswordChangeValidation123!";

		// 1. Start on the change-password page after signing in with a temporary password.
		const { temporaryPassword } = await startForcedPasswordChangeFlow(
			page,
			username,
			fullName,
		);

		// 2. Fill only the current password.
		await page
			.getByRole("textbox", { name: "Current password" })
			.fill(temporaryPassword);

		// 3. Verify the Change password button is disabled.
		await expect(
			page.getByRole("button", { name: "Change password" }),
		).toBeDisabled();

		// 4. Fill a new password that is different from the confirmation.
		await page
			.getByRole("textbox", { name: "New password", exact: true })
			.fill(nextPassword);
		await page
			.getByRole("textbox", { name: "Confirm new password" })
			.fill(`${nextPassword}-mismatch`);

		// 5. Verify the Change password button is disabled.
		await expect(
			page.getByRole("button", { name: "Change password" }),
		).toBeDisabled();

		// 6. Update the confirmation to match the new password.
		await page
			.getByRole("textbox", { name: "Confirm new password" })
			.fill(nextPassword);

		// 7. Verify the Change password button is enabled.
		await expect(
			page.getByRole("button", { name: "Change password" }),
		).toBeEnabled();
	});

	test("Signing out does not bypass the password change requirement", async ({
		page,
	}) => {
		const username = uniqueUsername("SIGNOUT");
		const fullName = "Password Change Signout";

		// 1. Start on the change-password page after signing in with a temporary password.
		const { temporaryPassword } = await startForcedPasswordChangeFlow(
			page,
			username,
			fullName,
		);

		// 2. Click Sign out.
		await page.getByRole("button", { name: "Sign out" }).click();

		// 3. Verify the user is redirected to the login page.
		await expectLoginPage(page);
		await expect(page).toHaveURL(/\/$/);

		// 4. Sign in again with the same temporary password.
		await login(page, username, temporaryPassword);

		// 5. Verify the user is redirected to the change-password page.
		await expect(page).toHaveURL(/\/change-password$/);
		await expect(page.getByRole("heading", { name: "Set new password" })).toBeVisible();
	});

	test("A successful password change returns the user to sign-in", async ({
		page,
	}) => {
		const username = uniqueUsername("SUCCESS");
		const fullName = "Password Change Success";
		const nextPassword = "PasswordChangeSuccess123!";

		// 1. Start on the change-password page after signing in with a temporary password.
		const { temporaryPassword } = await startForcedPasswordChangeFlow(
			page,
			username,
			fullName,
		);

		// 2. Change the password from the temporary password to a valid personal password.
		await changePassword(page, temporaryPassword, nextPassword);

		// 3. Verify the user is redirected to the login page.
		await expectLoginPage(page);
		await expect(page).toHaveURL(/\/$/);

		// 4. Verify the temporary password no longer grants access.
		await login(page, username, temporaryPassword);
		await expect(page.getByText("Invalid username or password.")).toBeVisible();
		await expect(page).toHaveURL(/\/$/);
	});

	test("The new password grants access to the workspace", async ({
		page,
	}) => {
		const username = uniqueUsername("HOME");
		const fullName = "Password Change Home";
		const nextPassword = "PasswordChangeHome123!";

		// 1. Start on the change-password page after signing in with a temporary password.
		const { temporaryPassword } = await startForcedPasswordChangeFlow(
			page,
			username,
			fullName,
		);

		// 2. Change the password from the temporary password to a valid personal password.
		await changePassword(page, temporaryPassword, nextPassword);

		// 3. Sign in with the new password.
		await login(page, username, nextPassword);

		// 4. Verify the user is redirected to the home page.
		await expectHomePage(page, fullName);
	});
});
