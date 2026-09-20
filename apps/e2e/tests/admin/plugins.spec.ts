// spec: features/admin/plugins.feature
// seed: tests/seed.spec.ts
//
// The marketplace catalog (GET /admin/plugins/marketplace) and the installed
// plugin list (GET /plugins) are stubbed with page.route: there is no real
// marketplace fixture to install from, and stubbing keeps the scenarios
// independent of whatever the stack has installed. Install / uninstall /
// upgrade are intentionally not clicked.
//
// Plugin cards have no role or accessible name of their own, so they are
// located structurally (nearest `space-y-3` ancestor of the display name).

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

const PREFIX = "E2E_PLUGINS_";
const RUN_ID = newRunId();
const PLUGINS_URL = `${BASE_URL}/admin/plugins`;

const MARKETPLACE_URL = /\/api\/v1\/admin\/plugins\/marketplace(\?.*)?$/;
const INSTALLED_URL = /\/api\/v1\/plugins(\?.*)?$/;

interface StubMarketplacePlugin {
	name: string;
	display_name: string;
	description: string;
	version: string;
	artifacts: Record<string, string>;
}

const TIME_LOGGING: StubMarketplacePlugin = {
	name: "e2e-time-logging",
	display_name: "Time Logging",
	description: "Track time spent on tasks across projects.",
	version: "1.2.0",
	artifacts: {
		manifest_tar_gz_url: "https://example.invalid/time-logging/manifest.tar.gz",
		backend_tar_gz_url: "https://example.invalid/time-logging/backend.tar.gz",
		frontend_tar_gz_url: "https://example.invalid/time-logging/frontend.tar.gz",
	},
};

const RELEASE_NOTES: StubMarketplacePlugin = {
	name: "e2e-release-notes",
	display_name: "Release Notes",
	description: "Publish release notes from completed sprints.",
	version: "0.4.1",
	artifacts: {
		manifest_tar_gz_url:
			"https://example.invalid/release-notes/manifest.tar.gz",
	},
};

function installedPlugin(plugin: StubMarketplacePlugin, version: string) {
	return {
		id: `00000000-0000-4000-8000-${RUN_ID.padStart(12, "0")}`,
		name: plugin.name,
		version,
		manifest: {
			id: plugin.name,
			displayName: plugin.display_name,
			version,
		},
		enabled: true,
		installed_at: "2026-01-01T00:00:00Z",
		updated_at: "2026-01-01T00:00:00Z",
	};
}

// Must run before the page loads /admin/plugins (and before signIn, since
// the app fetches /plugins for its extension registry right after login).
async function stubPlugins(
	page: Page,
	opts: {
		marketplace: StubMarketplacePlugin[];
		installed?: ReturnType<typeof installedPlugin>[];
	},
) {
	await page.route(MARKETPLACE_URL, (route) =>
		route.fulfill({
			status: 200,
			contentType: "application/json",
			body: JSON.stringify({
				success: true,
				data: { plugins: opts.marketplace },
			}),
		}),
	);
	await page.route(INSTALLED_URL, (route) =>
		route.fulfill({
			status: 200,
			contentType: "application/json",
			body: JSON.stringify({
				success: true,
				data: { plugins: opts.installed ?? [] },
			}),
		}),
	);
}

const pageHeading = (page: Page) =>
	page.getByRole("heading", { name: "Plugin Settings" });
const marketplaceTab = (page: Page) =>
	page.getByRole("button", { name: "Marketplace", exact: true });
const layoutTab = (page: Page) =>
	page.getByRole("button", { name: "Extension Point Layout", exact: true });
const searchBox = (page: Page) =>
	page.getByRole("textbox", { name: "Search plugins" });

function pluginCard(page: Page, displayName: string): Locator {
	return page
		.getByText(displayName, { exact: true })
		.locator("xpath=ancestor::div[contains(@class,'space-y-3')][1]");
}

async function openPluginsPage(page: Page) {
	await signIn(page);
	await page.goto(PLUGINS_URL);
	await expect(pageHeading(page)).toBeVisible();
}

// ===========================================================================
// Rule: Access is gated by the plugins.write permission
// ===========================================================================

test.describe("Plugins page permission gating", () => {
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
			// A role must grant something; projects.read is irrelevant to Plugins
			// and lets the account reach the home page after signing in.
			permissions: { "projects.read": true, ...permissions },
		});
		return username;
	}

	test("A user without plugins.write is redirected away from the Plugins page", async ({
		page,
		request,
		playwright,
	}) => {
		const username = await createRestrictedUser(
			request,
			playwright,
			"NOPLUGINS",
			{},
		);

		await signIn(page, username, RESTRICTED_PASSWORD);
		await page.goto(PLUGINS_URL);

		await expect(page).toHaveURL(/\/home/);
		await expect(page).not.toHaveURL(/\/admin\/plugins/);
		await expect(pageHeading(page)).toHaveCount(0);
	});

	test("A user with only plugins.write can open the Plugins page", async ({
		page,
		request,
		playwright,
	}) => {
		const username = await createRestrictedUser(
			request,
			playwright,
			"PLUGINSWRITER",
			{ "plugins.write": true },
		);
		await stubPlugins(page, { marketplace: [TIME_LOGGING] });

		await signIn(page, username, RESTRICTED_PASSWORD);
		await page.goto(PLUGINS_URL);

		await expect(pageHeading(page)).toBeVisible();
		await expect(marketplaceTab(page)).toBeVisible();
		await expect(layoutTab(page)).toBeVisible();
	});
});

// ===========================================================================
// Rule: Marketplace tab
// ===========================================================================

test.describe("Marketplace tab", () => {
	test.beforeEach(async ({ page }) => {
		await stubPlugins(page, { marketplace: [TIME_LOGGING, RELEASE_NOTES] });
		await openPluginsPage(page);
	});

	test("The Marketplace tab is shown by default", async ({ page }) => {
		await expect(pageHeading(page)).toBeVisible();
		await expect(
			page.getByText(
				"Install or uninstall plugins from the public paca-plugins catalog.",
			),
		).toBeVisible();
		await expect(page.getByText("Time Logging", { exact: true })).toBeVisible();
		await expect(
			page.getByText("Release Notes", { exact: true }),
		).toBeVisible();
	});

	test("A marketplace card shows its version, description and feature badges", async ({
		page,
	}) => {
		const card = pluginCard(page, "Time Logging");

		await expect(card.getByText("1.2.0", { exact: true })).toBeVisible();
		await expect(card.getByText(TIME_LOGGING.description)).toBeVisible();
		await expect(card.getByText("Backend", { exact: true })).toBeVisible();
		await expect(card.getByText("Frontend", { exact: true })).toBeVisible();
		await expect(card.getByText("Migrations", { exact: true })).toHaveCount(0);
		await expect(
			card.getByRole("button", { name: "Install", exact: true }),
		).toBeVisible();
	});

	test("Searching the marketplace filters the plugin list", async ({
		page,
	}) => {
		await searchBox(page).fill("release");

		await expect(
			page.getByText("Release Notes", { exact: true }),
		).toBeVisible();
		await expect(page.getByText("Time Logging", { exact: true })).toHaveCount(
			0,
		);
	});

	test("Searching for a plugin that does not exist shows an empty state", async ({
		page,
	}) => {
		await searchBox(page).fill("no-such-plugin");

		await expect(page.getByText("No marketplace plugins found.")).toBeVisible();
	});

	test("An empty marketplace catalog shows an empty state", async ({
		page,
	}) => {
		// Routes registered later take precedence over the ones from beforeEach.
		await stubPlugins(page, { marketplace: [] });
		await page.reload();

		await expect(pageHeading(page)).toBeVisible();
		await expect(page.getByText("No marketplace plugins found.")).toBeVisible();
	});
});

// ===========================================================================
// Rule: Installed plugins are reflected on marketplace cards
// ===========================================================================

test.describe("Installed plugins on marketplace cards", () => {
	test("An installed plugin shows the Installed badge and an Uninstall button", async ({
		page,
	}) => {
		await stubPlugins(page, {
			marketplace: [TIME_LOGGING],
			installed: [installedPlugin(TIME_LOGGING, "1.2.0")],
		});
		await openPluginsPage(page);

		const card = pluginCard(page, "Time Logging");
		await expect(card.getByText("Installed", { exact: true })).toBeVisible();
		await expect(
			card.getByRole("button", { name: "Uninstall", exact: true }),
		).toBeVisible();
		await expect(
			card.getByRole("button", { name: "Install", exact: true }),
		).toHaveCount(0);
		await expect(card.getByText("Update available")).toHaveCount(0);
	});

	test("An installed plugin with a newer marketplace version offers an upgrade", async ({
		page,
	}) => {
		await stubPlugins(page, {
			marketplace: [TIME_LOGGING],
			installed: [installedPlugin(TIME_LOGGING, "1.0.0")],
		});
		await openPluginsPage(page);

		const card = pluginCard(page, "Time Logging");
		await expect(card.getByText("Update available")).toBeVisible();
		await expect(
			card.getByRole("button", { name: "Upgrade to 1.2.0" }),
		).toBeVisible();
	});
});

// ===========================================================================
// Rule: Extension Point Layout tab
// ===========================================================================

test.describe("Extension Point Layout tab", () => {
	test.beforeEach(async ({ page }) => {
		await stubPlugins(page, { marketplace: [TIME_LOGGING] });
		await openPluginsPage(page);
	});

	test("The Layout tab shows an empty state when no plugin contributes extension points", async ({
		page,
	}) => {
		await layoutTab(page).click();

		await expect(
			page.getByText(
				"Drag to reorder plugin panels within each extension point. Toggle visibility to show or hide panels for all users.",
			),
		).toBeVisible();
		await expect(
			page.getByText("No plugins with extension points are installed."),
		).toBeVisible();
	});

	test("Switching back to the Marketplace tab shows the catalog again", async ({
		page,
	}) => {
		await layoutTab(page).click();
		await marketplaceTab(page).click();

		await expect(searchBox(page)).toBeVisible();
	});
});

// ===========================================================================
// Rule: Plugin-contributed admin pages
// ===========================================================================

test.describe("Plugin-contributed admin pages", () => {
	// APP BUG: AdminPluginPage (routes/_authenticated/admin/plugins/$pluginId/
	// $slug.tsx) calls `throw notFound()` from the component body, but no
	// notFoundComponent is configured, so the app's error boundary shows a
	// generic "Something went wrong" + Retry state instead of a Not Found page.
	// Remove the fixme once the route renders a real not-found state.
	test.fixme("Navigating to an admin page of a plugin that is not installed shows Not Found", async ({
		page,
	}) => {
		await stubPlugins(page, { marketplace: [] });
		await signIn(page);
		await page.goto(`${PLUGINS_URL}/no-such-plugin/no-such-page`);

		await expect(page.getByText("Not Found", { exact: true })).toBeVisible();
		await expect(pageHeading(page)).toHaveCount(0);
	});
});
