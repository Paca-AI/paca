// spec: features/admin/settings.feature
// seed: tests/seed.spec.ts

import {
	type APIRequestContext,
	expect,
	type Page,
	test,
} from "@playwright/test";
import {
	API_URL,
	authRequest,
	BASE_URL,
	cleanupGlobalRolesByPrefix,
	cleanupUsersByPrefix,
	createUserWithGlobalPermissions,
	newRunId,
	RESTRICTED_PASSWORD,
	signIn,
} from "../helpers/e2e-api";

const PREFIX = "E2E_SETTINGS_";
const RUN_ID = newRunId();
const SETTINGS_URL = `${BASE_URL}/admin/settings`;
const COLOR_PRESET_NAMES = [
	"Green",
	"Blue",
	"Teal",
	"Indigo",
	"Purple",
	"Pink",
	"Red",
	"Orange",
];

interface Branding {
	brand_name: string | null;
	primary_color_light: string | null;
	primary_color_dark: string | null;
}

// GET /branding is public and omits unset fields (omitempty), so normalise
// missing keys to null, which is also what PATCH /admin/settings takes to
// clear an override.
async function readBranding(request: APIRequestContext): Promise<Branding> {
	const response = await request.get(`${API_URL}/branding`);
	expect(response.ok()).toBeTruthy();
	const data = (await response.json()).data ?? {};
	return {
		brand_name: data.brand_name ?? null,
		primary_color_light: data.primary_color_light ?? null,
		primary_color_dark: data.primary_color_dark ?? null,
	};
}

async function writeBranding(request: APIRequestContext, branding: Branding) {
	await authRequest(request);
	const response = await request.patch(`${API_URL}/admin/settings`, {
		data: branding,
	});
	expect(response.ok()).toBeTruthy();
}

const brandNameField = (page: Page) =>
	page.getByRole("textbox", { name: "Brand Name" });
const saveButton = (page: Page) =>
	page.getByRole("button", { name: "Save changes" });
const colorPreset = (page: Page, name: string) =>
	page.getByRole("button", { name, exact: true });

async function openSettings(page: Page) {
	await signIn(page);
	await page.goto(SETTINGS_URL);
	await expect(
		page.getByRole("heading", { name: "Workspace Branding" }),
	).toBeVisible();
}

// ===========================================================================
// Rule: Access is gated by the settings.write permission
// ===========================================================================

test.describe("Settings page permission gating", () => {
	// Sign in fresh as the restricted user rather than reusing any stored session.
	test.use({ storageState: { cookies: [], origins: [] } });

	async function cleanup(request: APIRequestContext) {
		// Users must go first: a global role that still has members can't be deleted.
		await cleanupUsersByPrefix(request, PREFIX);
		await cleanupGlobalRolesByPrefix(request, PREFIX);
	}

	test.beforeEach(async ({ request }) => {
		await authRequest(request);
		await cleanup(request);
	});

	test.afterEach(async ({ request }) => {
		await cleanup(request);
	});

	async function createRestrictedUser(
		request: APIRequestContext,
		playwright: Parameters<typeof createUserWithGlobalPermissions>[1],
		label: string,
		permissions: Record<string, boolean>,
	): Promise<string> {
		const username = `${PREFIX}${label}_${RUN_ID}`;
		await createUserWithGlobalPermissions(request, playwright, {
			username,
			roleName: `${PREFIX}ROLE_${label}_${RUN_ID}`,
			// A role must grant something; projects.read is irrelevant to Settings
			// and lets the account reach the home page after signing in.
			permissions: { "projects.read": true, ...permissions },
		});
		return username;
	}

	test("A user without settings.write is redirected away from the Settings page", async ({
		page,
		request,
		playwright,
	}) => {
		const username = await createRestrictedUser(
			request,
			playwright,
			"NOSETTINGS",
			{},
		);

		await signIn(page, username, RESTRICTED_PASSWORD);
		await page.goto(SETTINGS_URL);

		await expect(page).toHaveURL(/\/home/);
		await expect(page).not.toHaveURL(/\/admin\/settings/);
		await expect(
			page.getByRole("heading", { name: "Workspace Branding" }),
		).toHaveCount(0);
	});

	test("A user with only settings.write can open the Settings page", async ({
		page,
		request,
		playwright,
	}) => {
		const username = await createRestrictedUser(
			request,
			playwright,
			"SETTINGSWRITER",
			{ "settings.write": true },
		);

		await signIn(page, username, RESTRICTED_PASSWORD);
		await page.goto(SETTINGS_URL);

		await expect(
			page.getByRole("heading", { name: "Workspace Branding" }),
		).toBeVisible();
		await expect(brandNameField(page)).toBeVisible();
	});
});

// ===========================================================================
// Rule: Branding form
// ===========================================================================

test.describe("Branding form", () => {
	let original: Branding;

	test.beforeEach(async ({ request }) => {
		await authRequest(request);
		// Remember the instance's real branding, then start every test from a
		// known "nothing overridden" state.
		original = await readBranding(request);
		await writeBranding(request, {
			brand_name: null,
			primary_color_light: null,
			primary_color_dark: null,
		});
	});

	test.afterEach(async ({ request }) => {
		await writeBranding(request, original);
	});

	test("The Settings page shows the branding form with nothing selected", async ({
		page,
	}) => {
		await openSettings(page);

		await expect(brandNameField(page)).toHaveValue("");
		for (const name of COLOR_PRESET_NAMES) {
			await expect(colorPreset(page, name)).toHaveAttribute(
				"aria-pressed",
				"false",
			);
		}
		await expect(saveButton(page)).toBeDisabled();
	});

	test("Editing the brand name enables the Save button", async ({ page }) => {
		await openSettings(page);

		await brandNameField(page).fill(`${PREFIX}BRAND_${RUN_ID}`);

		await expect(saveButton(page)).toBeEnabled();
	});

	test("Restoring the stored brand name disables the Save button again", async ({
		page,
		request,
	}) => {
		const storedName = `${PREFIX}STORED_${RUN_ID}`;
		await writeBranding(request, {
			brand_name: storedName,
			primary_color_light: null,
			primary_color_dark: null,
		});
		await openSettings(page);
		await expect(brandNameField(page)).toHaveValue(storedName);
		await expect(saveButton(page)).toBeDisabled();

		await brandNameField(page).fill(`${storedName}_EDITED`);
		await expect(saveButton(page)).toBeEnabled();

		await brandNameField(page).fill(storedName);

		await expect(saveButton(page)).toBeDisabled();
	});

	test("Selecting a colour preset marks only that preset as selected", async ({
		page,
	}) => {
		await openSettings(page);

		await colorPreset(page, "Blue").click();

		await expect(colorPreset(page, "Blue")).toHaveAttribute(
			"aria-pressed",
			"true",
		);
		for (const name of COLOR_PRESET_NAMES.filter((n) => n !== "Blue")) {
			await expect(colorPreset(page, name)).toHaveAttribute(
				"aria-pressed",
				"false",
			);
		}
		await expect(saveButton(page)).toBeEnabled();
	});

	test("Selecting another preset moves the selection", async ({ page }) => {
		await openSettings(page);
		await colorPreset(page, "Green").click();
		await expect(colorPreset(page, "Green")).toHaveAttribute(
			"aria-pressed",
			"true",
		);

		await colorPreset(page, "Red").click();

		await expect(colorPreset(page, "Red")).toHaveAttribute(
			"aria-pressed",
			"true",
		);
		await expect(colorPreset(page, "Green")).toHaveAttribute(
			"aria-pressed",
			"false",
		);
	});

	test("Saving persists the brand name and colour and shows a confirmation", async ({
		page,
		request,
	}) => {
		const brandName = `${PREFIX}BRAND_${RUN_ID}`;
		await openSettings(page);
		await brandNameField(page).fill(brandName);
		await colorPreset(page, "Purple").click();

		const patch = page.waitForRequest(
			(req) =>
				req.method() === "PATCH" &&
				req.url().endsWith("/api/v1/admin/settings"),
		);
		await saveButton(page).click();

		const payload = (await patch).postDataJSON();
		expect(payload.brand_name).toBe(brandName);
		expect(payload.primary_color_light).toBe("#7c3aed");
		expect(payload.primary_color_dark).toBe("#a78bfa");

		await expect(page.getByText("Saved", { exact: true })).toBeVisible();
		await expect(saveButton(page)).toBeDisabled();

		const stored = await readBranding(request);
		expect(stored).toEqual({
			brand_name: brandName,
			primary_color_light: "#7c3aed",
			primary_color_dark: "#a78bfa",
		});
	});

	test("Saved branding is still shown after reloading the page", async ({
		page,
	}) => {
		const brandName = `${PREFIX}RELOAD_${RUN_ID}`;
		await openSettings(page);
		await brandNameField(page).fill(brandName);
		await colorPreset(page, "Teal").click();
		await saveButton(page).click();
		await expect(page.getByText("Saved", { exact: true })).toBeVisible();

		await page.reload();
		await expect(
			page.getByRole("heading", { name: "Workspace Branding" }),
		).toBeVisible();

		await expect(brandNameField(page)).toHaveValue(brandName);
		await expect(colorPreset(page, "Teal")).toHaveAttribute(
			"aria-pressed",
			"true",
		);
	});

	test("A failed save shows an error and keeps the form editable", async ({
		page,
	}) => {
		await openSettings(page);
		await page.route("**/api/v1/admin/settings", (route) =>
			route.request().method() === "PATCH"
				? route.fulfill({
						status: 500,
						contentType: "application/json",
						body: JSON.stringify({
							success: false,
							error: { code: "INTERNAL_ERROR", message: "boom" },
						}),
					})
				: route.continue(),
		);

		await brandNameField(page).fill(`${PREFIX}FAIL_${RUN_ID}`);
		await saveButton(page).click();

		await expect(
			page.getByText("Failed to save. Please try again."),
		).toBeVisible();
		await expect(page.getByText("Saved", { exact: true })).toHaveCount(0);
		await expect(saveButton(page)).toBeEnabled();
	});
});
