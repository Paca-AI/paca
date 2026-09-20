// spec: features/profile/profile.feature
// seed: tests/seed.spec.ts
//
// Mutating profile tests never touch the admin account: every scenario
// creates a throwaway non-admin user through the admin API and signs in as
// that user in the browser. Avatar upload is intentionally not covered.

import { expect, type Page, test } from "@playwright/test";
import {
	API_URL,
	BASE_URL,
	cleanupUsersByPrefix,
	newRunId,
	RESTRICTED_PASSWORD,
	signIn,
} from "../helpers/e2e-api";
import { createUserWithEmail, loginContext } from "../helpers/profile-users";

const USER_PREFIX = "E2E_PROFILE_";
const RUN_ID = newRunId();
let userCounter = 0;

interface TestUser {
	username: string;
	fullName: string;
	email: string;
}

function uniqueUser(label: string): TestUser {
	userCounter += 1;
	const username = `${USER_PREFIX}${label}_${RUN_ID}_${userCounter}`;
	return {
		username,
		fullName: `E2E Profile ${label} ${RUN_ID}${userCounter}`,
		email: `${username.toLowerCase()}@example.test`,
	};
}

async function openProfile(page: Page, username: string): Promise<void> {
	await signIn(page, username, RESTRICTED_PASSWORD);
	await page.goto(`${BASE_URL}/profile`);
	await expect(page.getByRole("heading", { name: "My Profile" })).toBeVisible();
}

// The label and its value share a parent <div>, so the parent stands in for
// the whole "field" (the value paragraph has no role or accessible name).
function field(page: Page, label: string) {
	return page.getByText(label, { exact: true }).locator("..");
}

test.beforeEach(async ({ request }) => {
	await cleanupUsersByPrefix(request, USER_PREFIX);
});

test.afterEach(async ({ request }) => {
	await cleanupUsersByPrefix(request, USER_PREFIX);
});

test.describe("Viewing the profile", () => {
	let user: TestUser;

	test.beforeEach(async ({ page, request, playwright }) => {
		user = uniqueUser("VIEW");
		await createUserWithEmail(request, playwright, user);
		await openProfile(page, user.username);
	});

	test("The profile page shows the account header and details", async ({
		page,
	}) => {
		await expect(
			page.getByText("View and update your account information."),
		).toBeVisible();
		// Display name and @username each appear in the header card and (for the
		// name / username fields) again in the details list below it.
		await expect(
			page.getByText(user.fullName, { exact: true }).first(),
		).toBeVisible();
		await expect(
			page.getByText(`@${user.username}`, { exact: true }).first(),
		).toBeVisible();
		await expect(page.getByText("USER", { exact: true }).first()).toBeVisible();
		await expect(page.getByText(/^Joined /)).toBeVisible();

		await expect(field(page, "Full name")).toContainText(user.fullName);
		await expect(field(page, "Username")).toContainText(`@${user.username}`);
		await expect(field(page, "Email")).toContainText(user.email);
		await expect(
			page.getByRole("button", { name: "Edit profile" }),
		).toBeVisible();
	});
});

test.describe("Viewing a profile without an email", () => {
	test("A user without an email sees 'Not set'", async ({
		page,
		request,
		playwright,
	}) => {
		const noEmail = uniqueUser("NOEMAIL");
		await createUserWithEmail(request, playwright, {
			username: noEmail.username,
			fullName: noEmail.fullName,
		});

		await openProfile(page, noEmail.username);

		await expect(field(page, "Email")).toContainText("Not set");
	});
});

test.describe("Editing the profile", () => {
	let user: TestUser;

	test.beforeEach(async ({ page, request, playwright }) => {
		user = uniqueUser("EDIT");
		await createUserWithEmail(request, playwright, user);
		await openProfile(page, user.username);
	});

	test("Edit profile turns the name and email into inputs", async ({
		page,
	}) => {
		await page.getByRole("button", { name: "Edit profile" }).click();

		await expect(page.getByRole("textbox", { name: "Full name" })).toHaveValue(
			user.fullName,
		);
		await expect(page.getByRole("textbox", { name: "Email" })).toHaveValue(
			user.email,
		);
		await expect(
			page.getByRole("button", { name: "Save changes" }),
		).toBeVisible();
		await expect(page.getByRole("button", { name: "Cancel" })).toBeVisible();
		await expect(
			page.getByRole("button", { name: "Edit profile" }),
		).toHaveCount(0);
		await expect(page.getByRole("textbox", { name: "Username" })).toHaveCount(
			0,
		);
	});

	test("Saving persists the new name and email", async ({ page }) => {
		const newName = `E2E Profile Renamed ${RUN_ID}`;
		const newEmail = `renamed_${RUN_ID.toLowerCase()}@example.test`;

		await page.getByRole("button", { name: "Edit profile" }).click();
		await page.getByRole("textbox", { name: "Full name" }).fill(newName);
		await page.getByRole("textbox", { name: "Email" }).fill(newEmail);
		await page.getByRole("button", { name: "Save changes" }).click();

		await expect(
			page.getByRole("button", { name: "Edit profile" }),
		).toBeVisible();
		await expect(field(page, "Full name")).toContainText(newName);
		await expect(field(page, "Email")).toContainText(newEmail);

		await page.reload();
		await expect(
			page.getByRole("heading", { name: "My Profile" }),
		).toBeVisible();
		await expect(field(page, "Full name")).toContainText(newName);
		await expect(field(page, "Email")).toContainText(newEmail);
	});

	test("Cancel discards the draft changes", async ({ page }) => {
		await page.getByRole("button", { name: "Edit profile" }).click();
		await page
			.getByRole("textbox", { name: "Full name" })
			.fill("Should Not Be Saved");
		await page.getByRole("button", { name: "Cancel" }).click();

		await expect(
			page.getByRole("button", { name: "Edit profile" }),
		).toBeVisible();
		await expect(field(page, "Full name")).toContainText(user.fullName);

		await page.reload();
		await expect(
			page.getByRole("heading", { name: "My Profile" }),
		).toBeVisible();
		await expect(field(page, "Full name")).toContainText(user.fullName);
		await expect(page.getByText("Should Not Be Saved")).toHaveCount(0);
	});

	test("Save is disabled while the full name is blank", async ({ page }) => {
		await page.getByRole("button", { name: "Edit profile" }).click();
		await page.getByRole("textbox", { name: "Full name" }).fill("");

		await expect(
			page.getByRole("button", { name: "Save changes" }),
		).toBeDisabled();
	});

	test("A malformed email is flagged before saving", async ({ page }) => {
		await page.getByRole("button", { name: "Edit profile" }).click();
		await page.getByRole("textbox", { name: "Email" }).fill("not-an-email");

		await expect(
			page.getByText("Please enter a valid email address."),
		).toBeVisible();
		await expect(
			page.getByRole("button", { name: "Save changes" }),
		).toBeDisabled();
	});

	test("Clearing the email leaves the existing email unchanged", async ({
		page,
	}) => {
		await page.getByRole("button", { name: "Edit profile" }).click();
		await page.getByRole("textbox", { name: "Email" }).fill("");
		await page.getByRole("button", { name: "Save changes" }).click();

		await expect(
			page.getByRole("button", { name: "Edit profile" }),
		).toBeVisible();
		await expect(field(page, "Email")).toContainText(user.email);
	});

	test("An email already used by another account is rejected", async ({
		page,
		request,
		playwright,
	}) => {
		const other = uniqueUser("OTHER");
		await createUserWithEmail(request, playwright, other);

		await page.getByRole("button", { name: "Edit profile" }).click();
		await page.getByRole("textbox", { name: "Email" }).fill(other.email);
		await page.getByRole("button", { name: "Save changes" }).click();

		await expect(
			page.getByText("This email is already in use by another account."),
		).toBeVisible();
		await expect(
			page.getByRole("button", { name: "Save changes" }),
		).toBeVisible();
	});
});

test.describe("Profile API access control", () => {
	test("An unauthenticated request cannot update a profile", async ({
		playwright,
	}) => {
		const anonymous = await playwright.request.newContext();
		try {
			const response = await anonymous.patch(`${API_URL}/users/me`, {
				data: { full_name: "Nobody" },
			});
			expect(response.status()).toBe(401);
		} finally {
			await anonymous.dispose();
		}
	});

	// APP BUG: UpdateProfileRequest declares `full_name` as binding:"required",
	// but the handler's middleware.BindJSON does not enforce gin `binding` tags,
	// so PATCH /users/me without full_name returns 200 and blanks the user's
	// name. Remove the fixme once the handler validates full_name.
	test.fixme("A profile update without a full name is rejected", async ({
		request,
		playwright,
	}) => {
		const user = uniqueUser("API");
		await createUserWithEmail(request, playwright, user);

		const userApi = await loginContext(playwright, user.username);
		try {
			const response = await userApi.patch(`${API_URL}/users/me`, {
				data: { email: user.email },
			});
			expect(response.ok()).toBeFalsy();
			expect(response.status()).toBeLessThan(500);
		} finally {
			await userApi.dispose();
		}
	});
});
