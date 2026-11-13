// spec: features/auth/passkeys.feature
// seed: tests/seed.spec.ts
//
// Passkeys as a second sign-in factor. Chromium's virtual authenticator
// stands in for the user's device, so the real WebAuthn ceremonies run
// against the real server. Every scenario works on throwaway users.

import { expect, type Page, test } from "@playwright/test";
import { dialogPanel } from "../helpers/dialog";
import {
	allowPolicy,
	authRequest,
	BASE_URL,
	cleanupGlobalRolesByPrefix,
	cleanupUsersByPrefix,
	createGlobalRole,
	createUserWithPassword,
	newRunId,
	RESTRICTED_PASSWORD,
	signIn,
} from "../helpers/e2e-api";
import { putPolicy, rawLogin, restorePolicy } from "../helpers/mfa";
import { addVirtualAuthenticator } from "../helpers/passkey";

const PREFIX = "E2E_PASSKEY_";
const RUN_ID = newRunId();
let counter = 0;

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

test.beforeEach(({ browserName }) => {
	test.skip(
		browserName !== "chromium",
		"virtual authenticators are Chromium-only",
	);
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

async function openSecurity(page: Page) {
	await page.goto(`${BASE_URL}/account/security`);
	await expect(page.getByRole("heading", { name: "Security" })).toBeVisible();
}

async function addPasskey(page: Page, name: string) {
	await page.getByRole("button", { name: "Add a passkey" }).click();
	const dialog = dialogPanel(page);
	await dialog.getByLabel("Name").fill(name);
	await dialog.getByRole("button", { name: "Continue" }).click();
	await expect(dialog).toBeHidden();
	await expect(page.getByText(name, { exact: true })).toBeVisible();
}

/** Drops the session and enters the password on the sign-in page. */
async function passwordStep(page: Page, username: string) {
	await page.context().clearCookies();
	await page.goto(`${BASE_URL}/`);
	await page.getByRole("textbox", { name: "Username" }).fill(username);
	await page
		.getByRole("textbox", { name: "Password" })
		.fill(RESTRICTED_PASSWORD);
	await page.getByRole("button", { name: "Sign in" }).click();
	await expect(page).toHaveURL(/\/login\/mfa/);
}

test.describe("Registering and signing in", () => {
	test("a user registers a passkey and signs in with it", async ({
		page,
		request,
		playwright,
	}) => {
		const username = await newUser(request, playwright, "UI");
		await addVirtualAuthenticator(page);
		await signIn(page, username, RESTRICTED_PASSWORD);
		await openSecurity(page);

		await addPasskey(page, "Work laptop");
		await expect(page.getByText(/never used/i)).toBeVisible();

		await passwordStep(page, username);
		await expect(
			page.getByRole("button", { name: "Use a passkey" }),
		).toBeVisible();
		await page.getByRole("button", { name: "Use a passkey" }).click();
		await expect(
			page.getByRole("heading", { name: /Good (morning|afternoon|evening)/i }),
		).toBeVisible();

		await openSecurity(page);
		await expect(page.getByText(/last used/i)).toBeVisible();
	});

	test("a passkey-only account is challenged for a passkey, not a code", async ({
		page,
		request,
		playwright,
	}) => {
		const username = await newUser(request, playwright, "ONLY");
		await addVirtualAuthenticator(page);
		await signIn(page, username, RESTRICTED_PASSWORD);
		await openSecurity(page);
		await addPasskey(page, "Phone");

		const { response } = await rawLogin(playwright, username);
		expect(response.ok()).toBeTruthy();
		const body = (await response.json()).data;
		expect(body.mfa_required).toBe(true);
		expect(body.mfa_methods).toEqual(["passkey"]);

		await passwordStep(page, username);
		await expect(page.getByLabel("Authentication code")).toHaveCount(0);
		await expect(
			page.getByRole("button", { name: "Use a passkey" }),
		).toBeVisible();
	});

	test("an authenticator that lost its credential cannot finish sign-in", async ({
		page,
		request,
		playwright,
	}) => {
		const username = await newUser(request, playwright, "LOST");
		const authenticator = await addVirtualAuthenticator(page);
		await signIn(page, username, RESTRICTED_PASSWORD);
		await openSecurity(page);
		await addPasskey(page, "Old phone");

		await passwordStep(page, username);
		await authenticator.clearCredentials();
		await page.getByRole("button", { name: "Use a passkey" }).click();

		await expect(page.getByRole("alert")).toContainText(
			/cancelled or timed out/i,
		);
		await expect(page).toHaveURL(/\/login\/mfa/);
		await expect(
			page.getByRole("button", { name: "Back to sign in" }),
		).toBeVisible();
	});
});

test.describe("Removing a passkey", () => {
	test("removal is confirmed with a passkey, not the password", async ({
		page,
		request,
		playwright,
	}) => {
		const username = await newUser(request, playwright, "REMOVE");
		const authenticator = await addVirtualAuthenticator(page);
		await signIn(page, username, RESTRICTED_PASSWORD);
		await openSecurity(page);
		await addPasskey(page, "Tablet");

		await page.getByRole("button", { name: "Remove Tablet" }).click();
		const dialog = dialogPanel(page);
		await expect(dialog.getByLabel("Password")).toHaveCount(0);

		// With the authenticator gone nothing can approve the removal.
		await authenticator.clearCredentials();
		await dialog.getByRole("button", { name: "Remove", exact: true }).click();
		await expect(dialog.getByRole("alert")).toBeVisible();
		await expect(page.getByText("Tablet", { exact: true })).toBeVisible();
	});

	test("a passkey the device holds removes itself", async ({
		page,
		request,
		playwright,
	}) => {
		const username = await newUser(request, playwright, "REMOVE_OK");
		await addVirtualAuthenticator(page);
		await signIn(page, username, RESTRICTED_PASSWORD);
		await openSecurity(page);
		await addPasskey(page, "Laptop");

		await page.getByRole("button", { name: "Remove Laptop" }).click();
		const dialog = dialogPanel(page);
		await dialog.getByRole("button", { name: "Remove", exact: true }).click();
		await expect(dialog).toBeHidden();
		await expect(
			page.getByText("You haven't added a passkey yet."),
		).toBeVisible();

		const { response } = await rawLogin(playwright, username);
		expect((await response.json()).data.mfa_required).toBeUndefined();
	});
});

test.describe("When policy requires two-step verification", () => {
	test("a protected user sets up a passkey on the sign-in page instead of an app", async ({
		page,
		request,
		playwright,
	}) => {
		const role = `E2E_PASSKEY_ROLE_${RUN_ID}`;
		await authRequest(request);
		await createGlobalRole(request, role, allowPolicy([]));
		const username = uniqueName("POLICY");
		await createUserWithPassword(request, playwright, {
			username,
			fullName: username,
			roles: [role],
		});
		const saved = await putPolicy(request, {
			privileged_roles: [role],
			require_mfa: true,
			ip_allow_list: [],
		});
		expect(saved.ok()).toBeTruthy();
		await addVirtualAuthenticator(page);

		try {
			await page.goto(`${BASE_URL}/`);
			await page.getByRole("textbox", { name: "Username" }).fill(username);
			await page
				.getByRole("textbox", { name: "Password" })
				.fill(RESTRICTED_PASSWORD);
			await page.getByRole("button", { name: "Sign in" }).click();

			await expect(
				page.getByRole("heading", { name: "Secure your account" }),
			).toBeVisible();
			// The passkey is recommended, so it is already selected.
			await expect(page.getByRole("radio", { name: /Passkey/ })).toBeChecked();
			await page.getByRole("button", { name: "Continue", exact: true }).click();
			await expect(
				page.getByRole("heading", { name: "Passkey added" }),
			).toBeVisible();
			await page.getByRole("button", { name: "Continue to sign in" }).click();

			await expect(page).toHaveURL(/\/login\/mfa/);
			await page.getByRole("button", { name: "Use a passkey" }).click();
			await expect(
				page.getByRole("heading", {
					name: /Good (morning|afternoon|evening)/i,
				}),
			).toBeVisible();
		} finally {
			await restorePolicy(request);
			await cleanupGlobalRolesByPrefix(request, "E2E_PASSKEY_ROLE_");
		}
	});
});
