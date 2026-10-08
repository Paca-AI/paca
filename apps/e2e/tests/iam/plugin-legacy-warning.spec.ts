// spec: features/iam/plugin-legacy-warning.feature
// seed: tests/seed.spec.ts
//
// A plugin whose package still uses the retired `requirePermissions` middleware
// is flagged `legacy_permissions` by the plugin list. Administrators with
// plugins:write see a dismissible banner on the home page, and the marketplace
// card of that plugin carries an "Uses old permissions" badge.
//
// No real legacy plugin can be installed (the API rejects `requirePermissions`
// at install), so the installed-plugin list (GET /plugins) and the marketplace
// catalogue (GET /admin/plugins/marketplace) are stubbed with page.route, as in
// tests/admin/plugins.spec.ts. Stubs must be registered before signing in:
// the app reads the plugin list right after login.

import {
	type APIRequestContext,
	expect,
	type Locator,
	type Page,
	test,
} from "@playwright/test";
import {
	authRequest,
	BASE_URL,
	cleanupGlobalRolesByPrefix,
	cleanupUsersByPrefix,
	createUserWithGlobalPermissions,
	newRunId,
	RESTRICTED_PASSWORD,
	signIn,
} from "../helpers/e2e-api";

const PREFIX = "E2E_IAMPLUG_";
const RUN_ID = newRunId();
const MARKETPLACE_URL = /\/api\/v1\/admin\/plugins\/marketplace(\?.*)?$/;
const INSTALLED_URL = /\/api\/v1\/plugins(\?.*)?$/;
const DISMISS_KEY = "paca-legacy-plugins-banner-dismissed";

interface StubPlugin {
	name: string;
	displayName: string;
	version: string;
	legacy: boolean;
}

const LEGACY: StubPlugin = {
	name: "e2e-legacy-reports",
	displayName: "Legacy Reports",
	version: "1.0.0",
	legacy: true,
};
const SECOND_LEGACY: StubPlugin = {
	name: "e2e-legacy-billing",
	displayName: "Legacy Billing",
	version: "2.1.0",
	legacy: true,
};
const MODERN: StubPlugin = {
	name: "e2e-modern-timelog",
	displayName: "Modern Time Log",
	version: "3.0.0",
	legacy: false,
};

function installed(plugin: StubPlugin, index: number) {
	return {
		id: `00000000-0000-4000-8000-${`${RUN_ID}${index}`.padStart(12, "0")}`,
		name: plugin.name,
		version: plugin.version,
		manifest: {
			id: plugin.name,
			displayName: plugin.displayName,
			version: plugin.version,
		},
		enabled: true,
		...(plugin.legacy ? { legacy_permissions: true } : {}),
		installed_at: "2026-01-01T00:00:00Z",
		updated_at: "2026-01-01T00:00:00Z",
	};
}

function catalogue(plugin: StubPlugin) {
	return {
		name: plugin.name,
		display_name: plugin.displayName,
		description: `${plugin.displayName} from the e2e catalogue.`,
		version: plugin.version,
		artifacts: {
			manifest_tar_gz_url: `https://example.invalid/${plugin.name}/manifest.tar.gz`,
		},
	};
}

async function stubPlugins(page: Page, plugins: StubPlugin[]) {
	await page.route(INSTALLED_URL, (route) =>
		route.fulfill({
			status: 200,
			contentType: "application/json",
			body: JSON.stringify({
				success: true,
				data: { plugins: plugins.map(installed) },
			}),
		}),
	);
	await page.route(MARKETPLACE_URL, (route) =>
		route.fulfill({
			status: 200,
			contentType: "application/json",
			body: JSON.stringify({
				success: true,
				data: { plugins: plugins.map(catalogue) },
			}),
		}),
	);
}

const banner = (page: Page) =>
	page.getByRole("alert").filter({ hasText: /Plugins? needs? an update/ });

function pluginCard(page: Page, displayName: string): Locator {
	return page
		.getByText(displayName, { exact: true })
		.locator("xpath=ancestor::div[contains(@class,'space-y-3')][1]");
}

async function cleanup(request: APIRequestContext) {
	await cleanupUsersByPrefix(request, PREFIX);
	await cleanupGlobalRolesByPrefix(request, PREFIX);
}

test.beforeEach(async ({ request, context }) => {
	await authRequest(request);
	await cleanup(request);
	await context.clearCookies();
});

test.afterEach(async ({ request }) => {
	await authRequest(request);
	await cleanup(request);
});

// ─── The home banner ─────────────────────────────────────────────────────────

test.describe("The home page banner for plugins on old permissions", () => {
	test("An administrator sees the banner naming the plugin, with a link to manage plugins", async ({
		page,
	}) => {
		await stubPlugins(page, [LEGACY, MODERN]);
		await signIn(page);

		const alert = banner(page);
		await expect(alert).toBeVisible();
		await expect(alert).toContainText("Plugin needs an update");
		// Only the affected plugin is named, and the cause is spelled out.
		await expect(alert).toContainText(LEGACY.displayName);
		await expect(alert).not.toContainText(MODERN.displayName);
		await expect(alert).toContainText("requirePermissions");
		await expect(alert).toContainText("requireActions");

		await alert.getByRole("link", { name: "Manage plugins" }).click();
		await expect(page).toHaveURL(/\/admin\/plugins/);
	});

	test("Several affected plugins are named together under a plural title", async ({
		page,
	}) => {
		await stubPlugins(page, [LEGACY, SECOND_LEGACY]);
		await signIn(page);

		const alert = banner(page);
		await expect(alert).toContainText("Plugins need an update");
		await expect(alert).toContainText(LEGACY.displayName);
		await expect(alert).toContainText(SECOND_LEGACY.displayName);
	});

	test("No banner while every plugin uses the new permissions", async ({
		page,
	}) => {
		await stubPlugins(page, [MODERN]);
		await signIn(page);
		await expect(
			page.getByRole("heading", { name: /Good (morning|afternoon|evening)/i }),
		).toBeVisible();
		await expect(banner(page)).toHaveCount(0);
	});

	test("Dismissing hides it, remembers the choice, and a newly affected plugin shows it again", async ({
		page,
	}) => {
		await stubPlugins(page, [LEGACY]);
		await signIn(page);
		await expect(banner(page)).toBeVisible();

		await page.getByRole("button", { name: "Dismiss" }).click();
		await expect(banner(page)).toHaveCount(0);
		expect(
			await page.evaluate((key) => localStorage.getItem(key), DISMISS_KEY),
		).toBe(LEGACY.name);

		// The dismissal survives a reload.
		await page.reload();
		await expect(
			page.getByRole("heading", { name: /Good (morning|afternoon|evening)/i }),
		).toBeVisible();
		await expect(banner(page)).toHaveCount(0);

		// The dismissal is for this set of plugins: another affected plugin re-shows it.
		await page.unroute(INSTALLED_URL);
		await page.unroute(MARKETPLACE_URL);
		await stubPlugins(page, [LEGACY, SECOND_LEGACY]);
		await page.reload();
		await expect(banner(page)).toContainText("Plugins need an update");
	});

	test("Someone without plugins:write never sees it", async ({
		page,
		request,
		playwright,
	}) => {
		const username = `${PREFIX}NOPLUGINS_${RUN_ID}`;
		await createUserWithGlobalPermissions(request, playwright, {
			username,
			roleName: `${PREFIX}ROLE_NOPLUGINS_${RUN_ID}`,
			permissions: { "projects:read": true, "plugins:read": true },
		});
		await stubPlugins(page, [LEGACY]);
		await signIn(page, username, RESTRICTED_PASSWORD);

		await expect(
			page.getByRole("heading", { name: /Good (morning|afternoon|evening)/i }),
		).toBeVisible();
		await expect(banner(page)).toHaveCount(0);
		await expect(page.getByText(LEGACY.displayName)).toHaveCount(0);
	});

	test("A user holding plugins:write does see it", async ({
		page,
		request,
		playwright,
	}) => {
		const username = `${PREFIX}PLUGINS_${RUN_ID}`;
		await createUserWithGlobalPermissions(request, playwright, {
			username,
			roleName: `${PREFIX}ROLE_PLUGINS_${RUN_ID}`,
			permissions: { "projects:read": true, "plugins:write": true },
		});
		await stubPlugins(page, [LEGACY]);
		await signIn(page, username, RESTRICTED_PASSWORD);

		await expect(banner(page)).toContainText(LEGACY.displayName);
	});
});

// ─── The marketplace card ────────────────────────────────────────────────────

test.describe("The marketplace card of a plugin on old permissions", () => {
	async function openMarketplace(page: Page) {
		await signIn(page);
		await page.goto(`${BASE_URL}/admin/plugins`);
		await expect(
			page.getByRole("heading", { name: "Plugin Settings" }),
		).toBeVisible();
		await page
			.getByRole("button", { name: "Marketplace", exact: true })
			.click();
	}

	test("An installed legacy plugin carries an 'Uses old permissions' badge and an explanation", async ({
		page,
	}) => {
		await stubPlugins(page, [LEGACY, MODERN]);
		await openMarketplace(page);

		const legacy = pluginCard(page, LEGACY.displayName);
		await expect(legacy).toBeVisible();
		await expect(legacy.getByText("Uses old permissions")).toBeVisible();
		await expect(legacy.getByText("Installed", { exact: true })).toBeVisible();
		await expect(legacy.getByRole("alert")).toContainText(
			"it can't be reinstalled or upgraded until its author publishes a version that uses requireActions",
		);
	});

	test("A plugin on the new permissions has no badge", async ({ page }) => {
		await stubPlugins(page, [LEGACY, MODERN]);
		await openMarketplace(page);

		const modern = pluginCard(page, MODERN.displayName);
		await expect(modern).toBeVisible();
		await expect(modern.getByText("Installed", { exact: true })).toBeVisible();
		await expect(modern.getByText("Uses old permissions")).toHaveCount(0);
		await expect(modern.getByRole("alert")).toHaveCount(0);
	});

	test("A legacy plugin that is not installed shows no badge", async ({
		page,
	}) => {
		// The catalogue lists it, but the installed list is empty: nothing to warn about.
		await page.route(MARKETPLACE_URL, (route) =>
			route.fulfill({
				status: 200,
				contentType: "application/json",
				body: JSON.stringify({
					success: true,
					data: { plugins: [catalogue(LEGACY)] },
				}),
			}),
		);
		await page.route(INSTALLED_URL, (route) =>
			route.fulfill({
				status: 200,
				contentType: "application/json",
				body: JSON.stringify({ success: true, data: { plugins: [] } }),
			}),
		);
		await openMarketplace(page);

		const card = pluginCard(page, LEGACY.displayName);
		await expect(card).toBeVisible();
		await expect(card.getByText("Uses old permissions")).toHaveCount(0);
	});
});
