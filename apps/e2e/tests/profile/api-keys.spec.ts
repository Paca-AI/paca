// spec: features/profile/api-keys.feature
// seed: tests/seed.spec.ts
//
// Every scenario runs as a throwaway non-admin user (created through the
// admin API and deleted afterwards), so the "empty state" is guaranteed and
// the admin account's own keys are never touched. Keys are seeded through
// POST /users/me/api-keys using an API context logged in as that user.

import {
	type APIRequestContext,
	expect,
	type Page,
	test,
} from "@playwright/test";
import {
	API_URL,
	BASE_URL,
	cleanupUsersByPrefix,
	newRunId,
	RESTRICTED_PASSWORD,
	signIn,
} from "../helpers/e2e-api";
import { createUserWithEmail, loginContext } from "../helpers/profile-users";

const USER_PREFIX = "E2E_APIKEYS_";
const RUN_ID = newRunId();
let userCounter = 0;

interface CreatedKey {
	id: string;
	name: string;
	key: string;
	key_prefix: string;
}

function uniqueUsername(label: string): string {
	userCounter += 1;
	return `${USER_PREFIX}${label}_${RUN_ID}_${userCounter}`;
}

async function seedKey(
	api: APIRequestContext,
	name: string,
): Promise<CreatedKey> {
	const response = await api.post(`${API_URL}/users/me/api-keys`, {
		data: { name },
	});
	expect(response.ok()).toBeTruthy();
	return (await response.json()).data as CreatedKey;
}

async function listKeys(
	api: APIRequestContext,
): Promise<Array<Record<string, unknown> & { id: string; name: string }>> {
	const response = await api.get(`${API_URL}/users/me/api-keys`);
	expect(response.ok()).toBeTruthy();
	return (await response.json()).data;
}

async function openApiKeysPage(page: Page, username: string): Promise<void> {
	await signIn(page, username, RESTRICTED_PASSWORD);
	await page.goto(`${BASE_URL}/profile/api-keys`);
	await expect(page.getByRole("heading", { name: "API Keys" })).toBeVisible();
}

function createDialog(page: Page) {
	return page.getByRole("dialog", { name: "Create API key" });
}

function revealDialog(page: Page) {
	return page.getByRole("dialog", { name: "API key created" });
}

function keyRow(page: Page, name: string) {
	return page.getByRole("row", { name });
}

async function createKeyThroughUi(page: Page, name: string): Promise<void> {
	await page.getByRole("button", { name: "New key" }).click();
	await createDialog(page).getByRole("textbox", { name: "Name" }).fill(name);
	await createDialog(page).getByRole("button", { name: "Create key" }).click();
	await expect(revealDialog(page)).toBeVisible();
}

test.describe("API keys", () => {
	let username: string;
	let userApi: APIRequestContext;

	test.beforeEach(async ({ request, playwright }) => {
		await cleanupUsersByPrefix(request, USER_PREFIX);
		username = uniqueUsername("OWNER");
		await createUserWithEmail(request, playwright, {
			username,
			fullName: username,
		});
		userApi = await loginContext(playwright, username);
	});

	test.afterEach(async ({ request }) => {
		await userApi.dispose();
		await cleanupUsersByPrefix(request, USER_PREFIX);
	});

	test.describe("Empty state and layout", () => {
		test("A user with no keys sees the empty state", async ({ page }) => {
			await openApiKeysPage(page, username);

			await expect(page.getByText("Your keys")).toBeVisible();
			await expect(
				page.getByText("No API keys yet. Create one to get started."),
			).toBeVisible();
			await expect(page.getByRole("button", { name: "New key" })).toBeVisible();
		});
	});

	test.describe("Creating an API key", () => {
		test.beforeEach(async ({ page }) => {
			await openApiKeysPage(page, username);
		});

		test("The Create key button requires a name", async ({ page }) => {
			await page.getByRole("button", { name: "New key" }).click();

			await expect(createDialog(page)).toBeVisible();
			await expect(
				createDialog(page).getByRole("button", { name: "Create key" }),
			).toBeDisabled();

			await createDialog(page)
				.getByRole("textbox", { name: "Name" })
				.fill("CI pipeline");
			await expect(
				createDialog(page).getByRole("button", { name: "Create key" }),
			).toBeEnabled();
		});

		test("Creating a key reveals the full key exactly once", async ({
			page,
		}) => {
			await createKeyThroughUi(page, "CI pipeline");

			const dialog = revealDialog(page);
			await expect(
				dialog.getByText("CI pipeline", { exact: true }),
			).toBeVisible();
			const rawKeyLocator = dialog.getByText(/^paca_[0-9a-f]{64}$/);
			await expect(rawKeyLocator).toBeVisible();
			const rawKey = (await rawKeyLocator.textContent()) ?? "";
			await expect(
				dialog.getByRole("button", { name: "Copy key" }),
			).toBeVisible();

			await dialog.getByRole("button", { name: "Done" }).click();
			await expect(dialog).not.toBeVisible();

			const row = keyRow(page, "CI pipeline");
			await expect(row).toContainText(
				`${rawKey.slice(0, "paca_".length + 8)}…`,
			);
			await expect(row.getByRole("cell", { name: "—" })).toHaveCount(2);
			await expect(page.getByText(rawKey)).toHaveCount(0);

			await page.reload();
			await expect(
				page.getByRole("heading", { name: "API Keys" }),
			).toBeVisible();
			await expect(keyRow(page, "CI pipeline")).toBeVisible();
			await expect(page.getByText(rawKey)).toHaveCount(0);
		});

		// Clipboard access differs per browser engine; the assertion only relies
		// on the app's own "Copied" confirmation, not on reading the clipboard.
		test("The copy button confirms the key was copied", async ({
			page,
			context,
		}) => {
			// Chromium rejects clipboard writes without a grant (the app then
			// alerts instead of confirming); other engines reject these names.
			await context
				.grantPermissions(["clipboard-read", "clipboard-write"])
				.catch(() => {});
			await createKeyThroughUi(page, "Copy test");

			await revealDialog(page)
				.getByRole("button", { name: "Copy key" })
				.click();

			await expect(
				revealDialog(page).getByText("Copied to clipboard!"),
			).toBeVisible();
		});

		test("An optional expiration date is shown in the key list", async ({
			page,
		}) => {
			await page.getByRole("button", { name: "New key" }).click();
			await createDialog(page)
				.getByRole("textbox", { name: "Name" })
				.fill("Short lived");
			await createDialog(page).getByLabel("Expiration date").fill("2099-06-15");
			await createDialog(page)
				.getByRole("button", { name: "Create key" })
				.click();
			await revealDialog(page).getByRole("button", { name: "Done" }).click();

			await expect(keyRow(page, "Short lived")).toContainText("2099");
		});

		test("A name longer than 100 characters is rejected", async ({ page }) => {
			await page.getByRole("button", { name: "New key" }).click();
			await createDialog(page)
				.getByRole("textbox", { name: "Name" })
				.fill("x".repeat(101));
			await createDialog(page)
				.getByRole("button", { name: "Create key" })
				.click();

			await expect(
				createDialog(page).getByText("Name must be 100 characters or fewer."),
			).toBeVisible();
			await expect(createDialog(page)).toBeVisible();
		});

		test("Cancelling the create dialog discards the draft", async ({
			page,
		}) => {
			await page.getByRole("button", { name: "New key" }).click();
			await createDialog(page)
				.getByRole("textbox", { name: "Name" })
				.fill("Never created");
			await createDialog(page).getByRole("button", { name: "Cancel" }).click();

			await expect(createDialog(page)).not.toBeVisible();
			await expect(
				page.getByText("No API keys yet. Create one to get started."),
			).toBeVisible();

			await page.getByRole("button", { name: "New key" }).click();
			await expect(
				createDialog(page).getByRole("textbox", { name: "Name" }),
			).toHaveValue("");
		});
	});

	test.describe("Revoking an API key", () => {
		let revoked: CreatedKey;

		test.beforeEach(async ({ page }) => {
			await seedKey(userApi, "Keep me");
			revoked = await seedKey(userApi, "Revoke me");
			await openApiKeysPage(page, username);
		});

		test("Revoking asks for confirmation and then removes the key", async ({
			page,
		}) => {
			await keyRow(page, "Revoke me")
				.getByRole("button", { name: "Revoke key" })
				.click();

			const dialog = page.getByRole("dialog", { name: "Revoke API key" });
			await expect(dialog).toBeVisible();
			await expect(dialog).toContainText("Revoke me");
			await expect(dialog).toContainText(
				"Any requests using this key will stop working immediately.",
			);

			await dialog.getByRole("button", { name: "Revoke key" }).click();

			await expect(dialog).not.toBeVisible();
			await expect(keyRow(page, "Revoke me")).toHaveCount(0);
			await expect(keyRow(page, "Keep me")).toBeVisible();

			const remaining = await listKeys(userApi);
			expect(remaining.map((key) => key.id)).not.toContain(revoked.id);
			expect(remaining.map((key) => key.name)).toContain("Keep me");
		});

		test("Cancelling the revoke confirmation keeps the key", async ({
			page,
		}) => {
			await keyRow(page, "Revoke me")
				.getByRole("button", { name: "Revoke key" })
				.click();

			const dialog = page.getByRole("dialog", { name: "Revoke API key" });
			await expect(dialog).toBeVisible();
			await dialog.getByRole("button", { name: "Cancel" }).click();

			await expect(dialog).not.toBeVisible();
			await expect(keyRow(page, "Revoke me")).toBeVisible();
		});
	});

	test.describe("API key access control", () => {
		test("Listing keys never exposes the raw key or its hash", async () => {
			const created = await seedKey(userApi, "Listed");
			expect(created.key).toMatch(/^paca_[0-9a-f]{64}$/);

			const keys = await listKeys(userApi);
			const listed = keys.find((key) => key.id === created.id);
			expect(listed).toBeDefined();
			expect(listed).toHaveProperty("key_prefix");
			expect(listed).not.toHaveProperty("key");
			expect(listed).not.toHaveProperty("key_hash");
		});

		test("Keys are private to their owner", async ({ request, playwright }) => {
			const created = await seedKey(userApi, "Private");

			const otherName = uniqueUsername("OTHER");
			await createUserWithEmail(request, playwright, {
				username: otherName,
				fullName: otherName,
			});
			const otherApi = await loginContext(playwright, otherName);
			try {
				const otherKeys = await listKeys(otherApi);
				expect(otherKeys.map((key) => key.name)).not.toContain("Private");

				const attempt = await otherApi.delete(
					`${API_URL}/users/me/api-keys/${created.id}`,
				);
				expect(attempt.ok()).toBeFalsy();

				const ownerKeys = await listKeys(userApi);
				expect(ownerKeys.map((key) => key.id)).toContain(created.id);
			} finally {
				await otherApi.dispose();
			}
		});

		test("An unauthenticated client cannot list keys", async ({
			playwright,
		}) => {
			const anonymous = await playwright.request.newContext();
			try {
				const response = await anonymous.get(`${API_URL}/users/me/api-keys`);
				expect(response.status()).toBe(401);
			} finally {
				await anonymous.dispose();
			}
		});

		test("Creating a key with a blank name is rejected", async () => {
			const response = await userApi.post(`${API_URL}/users/me/api-keys`, {
				data: { name: "   " },
			});

			expect(response.ok()).toBeFalsy();
			expect((await response.json()).error_code).toBe("API_KEY_NAME_INVALID");
		});
	});
});
