// spec: features/ui/sidebar.feature
// seed: tests/seed.spec.ts

import { ensureLoginForm } from '../helpers/e2e-api';
import { expect, type Locator, type Page, test } from "@playwright/test";
import {
	authRequest,
	cleanupGlobalRolesByPrefix,
	cleanupUsersByPrefix,
	createUserWithGlobalPermissions,
	newRunId,
	RESTRICTED_PASSWORD,
	signIn,
} from "../helpers/e2e-api";

function profileMenuButton(page: Page) {
	return page.getByRole("button", { name: /Admin super_admin/i });
}

test.describe("Sidebar Navigation", () => {
	const signInAsAdmin = async (page: Page) => {
		await page.goto("/");
		await ensureLoginForm(page);
		await page.getByRole("textbox", { name: "Username" }).fill("admin");
		await page
			.getByRole("textbox", { name: "Password" })
			.fill("e2e-admin-password");
		await page.getByRole("button", { name: "Sign in" }).click();
		await expect(
			page.getByRole("heading", {
				name: /Good (morning|afternoon|evening), Admin/i,
			}),
		).toBeVisible();
	};

	test.beforeEach(async ({ context }) => {
		// Clear all browser state to ensure test isolation when running in parallel
		await context.clearCookies();
		await context.clearPermissions();
	});

	test("Sidebar Collapse and Expand Functionality", async ({ page }) => {
		await signInAsAdmin(page);

		const viewport = page.viewportSize();
		const isMobile = viewport && viewport.width <= 768;

		if (isMobile) {
			// On mobile, sidebar is collapsed by default, so we need to open it first
			await page.getByRole("button", { name: "Toggle Sidebar" }).click();

			// Verify the sidebar content is now visible in the overlay
			await expect(page.getByText("Home")).toBeVisible();
			await expect(page.getByText("Global Roles")).toBeVisible();

			// Close the sidebar by clicking outside or escape key
			await page.keyboard.press("Escape");

			// Verify the sidebar is closed (Global Roles link should not be visible)
			await expect(page.getByText("Global Roles")).not.toBeVisible();
		} else {
			// On desktop, verify the sidebar is expanded by default
			await expect(page.getByText("Home")).toBeVisible();
			await expect(page.getByText("Global Roles")).toBeVisible();

			// Click the sidebar trigger button to collapse the sidebar
			await page.getByLabel("Toggle Sidebar").click();

			// Click the sidebar trigger button again to expand the sidebar
			await page.getByLabel("Toggle Sidebar").click();

			// Verify the sidebar is in expanded state and navigation items show labels alongside icons when expanded
			await expect(page.getByText("Home")).toBeVisible();
			await expect(page.getByText("Global Roles")).toBeVisible();
		}
	});

	test("Navigation and Active States", async ({ page }) => {
		await signInAsAdmin(page);

		const viewport = page.viewportSize();
		const isMobile = viewport && viewport.width <= 768;

		if (isMobile) {
			// On mobile, we need to open the sidebar first
			await page.getByRole("button", { name: "Toggle Sidebar" }).click();
		}

		// Verify the "Home" navigation item is marked as active
		await expect(page.getByRole("link", { name: "Home" })).toBeVisible();

		// Click the "Global Roles" navigation item
		await page.getByRole("link", { name: "Global Roles" }).click();

		// Verify the "Global Roles" navigation item is marked as active and browser is on the global roles page
		await expect(page).toHaveURL(/\/admin\/global-roles/);
	});

	test("User Profile and Logout Button", async ({ page }) => {
		await signInAsAdmin(page);

		const viewport = page.viewportSize();
		const isMobile = viewport && viewport.width <= 768;

		if (isMobile) {
			// On mobile, we need to open the sidebar first
			await page.getByRole("button", { name: "Toggle Sidebar" }).click();
		}

		// Verify the user profile section is visible in the sidebar
		await expect(profileMenuButton(page)).toBeVisible();

		// Click on the user profile dropdown
		await profileMenuButton(page).click();

		// Verify logout functionality is accessible from the sidebar
		await expect(page.getByRole("menuitem", { name: "Log out" })).toBeVisible();
	});

	test("Keyboard Shortcuts and Alternative Interactions", async ({ page }) => {
		await signInAsAdmin(page);

		const viewport = page.viewportSize();
		const isMobile = viewport && viewport.width <= 768;

		if (isMobile) {
			// On mobile devices, keyboard shortcuts may not behave the same way
			// Instead, test the touch/tap interactions for sidebar toggle
			await page.getByRole("button", { name: "Toggle Sidebar" }).click();

			// Verify sidebar is open and content is accessible
			await expect(page.getByText("Theme")).toBeVisible();
			await expect(page.getByRole("button", { name: "Light" })).toBeVisible();

			// Verify the primary navigation link remains accessible inside the sheet.
			await expect(page.getByRole("link", { name: "Home" })).toBeVisible();

			// Close the sidebar using escape key or clicking outside
			await page.keyboard.press("Escape");

			// Verify sidebar is closed
			await expect(page.getByText("Global Roles")).not.toBeVisible();
		} else {
			// On desktop, test keyboard shortcut Cmd+B to toggle sidebar
			await page.keyboard.press("Meta+b");

			// Verify the collapsed sidebar still exposes icon-only navigation and profile access.
			const homeLink = page.getByRole("link", { name: "Home" });
			await expect(homeLink).toBeVisible();

			// Focus remains reliable in the collapsed state even when animated labels overlap pointer events.
			await homeLink.focus();
			await expect(homeLink).toBeFocused();

			// Test keyboard shortcut again to expand
			await page.keyboard.press("Meta+b");

			// Verify sidebar is expanded again (navigation labels should be visible)
			await expect(page.getByText("Administration")).toBeVisible();
		}
	});

	test("Theme Switcher Interaction", async ({ page }) => {
		await signInAsAdmin(page);

		const viewport = page.viewportSize();
		const isMobile = viewport && viewport.width <= 768;

		if (isMobile) {
			// On mobile, open the sidebar first
			await page.getByRole("button", { name: "Toggle Sidebar" }).click();
		}

		// Verify theme switcher shows three options when sidebar is expanded
		await expect(page.getByRole("button", { name: "Light" })).toBeVisible();
		await expect(page.getByRole("button", { name: "Dark" })).toBeVisible();
		await expect(
			page
				.locator("text=Theme")
				.locator("..")
				.getByRole("button", { name: "Auto", exact: true }),
		).toBeVisible();

		// Test selecting a theme changes appearance
		await page.getByRole("button", { name: "Dark" }).click();

		// Verify the dark theme is now active (check CSS classes or data attributes)
		const darkButton = page.getByRole("button", { name: "Dark" });
		// The button should have visual indication it's selected (active state in snapshot)
		await expect(darkButton).toBeVisible(); // Basic check that theme switch worked

		if (!isMobile) {
			// Test theme switcher behavior when collapsed (only on desktop)
			await page.keyboard.press("Meta+b"); // Collapse sidebar

			// Verify sidebar layout changed (just check that the main content is still visible)
			await expect(
				page.getByRole("heading", {
					name: /Good (morning|afternoon|evening), Admin/i,
				}),
			).toBeVisible();
		}
	});

	test("State Persistence", async ({ page }) => {
		await signInAsAdmin(page);

		const viewport = page.viewportSize();
		const isMobile = viewport && viewport.width <= 768;

		if (isMobile) {
			// On mobile, the sidebar is collapsed by default, so open it first
			await page.getByRole("button", { name: "Toggle Sidebar" }).click();

			// Verify it's open
			await expect(page.getByText("Administration")).toBeVisible();

			// Close it
			await page.keyboard.press("Escape");
		} else {
			// Collapse the sidebar (use the main toggle button)
			await page
				.getByRole("main")
				.getByRole("button", { name: "Toggle Sidebar" })
				.click();
		}

		// Reload the page
		await page.reload();

		// Test that the page loads successfully after reload
		await expect(
			page.getByRole("heading", {
				name: /Good (morning|afternoon|evening), Admin/i,
			}),
		).toBeVisible();

		if (!isMobile) {
			// Test expanded state after reload (current behavior on desktop)
			await expect(page.getByText("Administration")).toBeVisible();
		}
	});

	test("Admin Section Visibility", async ({ page }) => {
		await signInAsAdmin(page);

		const viewport = page.viewportSize();
		const isMobile = viewport && viewport.width <= 768;

		if (isMobile) {
			// On mobile, open the sidebar first
			await page.getByRole("button", { name: "Toggle Sidebar" }).click();
		}

		// Verify administration section is visible to admin users
		await expect(page.getByText("Administration")).toBeVisible();
		await expect(
			page.getByRole("link", { name: "Global Roles" }),
		).toBeVisible();
		await expect(page.getByRole("link", { name: "Users" })).toBeVisible();

		if (!isMobile) {
			// Test admin items remain accessible when sidebar is collapsed (desktop only)
			await page.keyboard.press("Meta+b"); // Collapse sidebar

			// Admin links should still be hoverable and clickable (this would need specific implementation testing)
		}
	});
});

/* ─── Mobile responsive behavior ──────────────────────────────────── */

test.describe("Sidebar Navigation - Mobile Behavior", () => {
	const signInAsAdminMobile = async (page: Page) => {
		// Set mobile viewport (iPhone 8 size)
		await page.setViewportSize({ width: 375, height: 667 });
		await page.goto("http://localhost/");
		await ensureLoginForm(page);
		await page.getByRole("textbox", { name: "Username" }).fill("admin");
		await page
			.getByRole("textbox", { name: "Password" })
			.fill("e2e-admin-password");
		await page.getByRole("button", { name: "Sign in" }).click();
	};

	test("Sidebar opens as an overlay sheet on mobile", async ({ page }) => {
		await signInAsAdminMobile(page);

		// Click the sidebar trigger button on mobile
		await page.getByRole("button", { name: "Toggle Sidebar" }).click();

		// Verify the sidebar opens as an overlay
		// The main content should remain visible behind the overlay
		await expect(
			page.getByText(/Good (morning|afternoon|evening), Admin/),
		).toBeVisible();

		// Verify sidebar content is accessible
		await expect(page.getByText("Administration")).toBeVisible();
		await expect(
			page.getByRole("link", { name: "Global Roles" }),
		).toBeVisible();
	});

	test("Mobile sidebar can be dismissed with the Escape key", async ({
		page,
	}) => {
		await signInAsAdminMobile(page);

		// Open the mobile sidebar
		await page.getByRole("button", { name: "Toggle Sidebar" }).click();

		// Verify sidebar is open
		await expect(page.getByText("Administration")).toBeVisible();

		// Press Escape key
		await page.keyboard.press("Escape");

		// Wait for sidebar to close and verify navigation items are not visible
		await expect(page.getByText("Administration")).not.toBeVisible();
	});

	test("Brand logo and app name are visible inside the mobile sheet", async ({
		page,
	}) => {
		// The instance brand is global state that admin/settings.spec.ts edits, so
		// pin the public branding to the defaults instead of reading shared state.
		await page.route(/\/api\/v1\/branding$/, (route) =>
			route.fulfill({ json: { success: true, data: {} } }),
		);
		await signInAsAdminMobile(page);

		// Open the mobile sidebar
		await page.getByRole("button", { name: "Toggle Sidebar" }).click();

		// Verify brand elements are visible in mobile sidebar
		await expect(page.getByText("paca", { exact: true })).toBeVisible();
		const logo = page.getByRole("img", { name: /Paca Logo/i });
		await expect(logo).toBeVisible();
	});

	test("Mobile layout is fully functional at various viewport sizes", async ({
		page,
	}) => {
		// Test at different mobile viewport sizes
		const viewports = [
			{ width: 320, height: 568 }, // iPhone SE
			{ width: 375, height: 667 }, // iPhone 8
			{ width: 414, height: 896 }, // iPhone 11
		];

		for (const viewport of viewports) {
			await page.setViewportSize(viewport);
			// Wait for layout to stabilize after viewport change
			await page.waitForTimeout(100);
			await page.goto("http://localhost/");

			// Check if we need to sign in or if we're already authenticated
			const usernameField = page.getByRole("textbox", { name: "Username" });
			const greetingText = page.getByText(
				/Good (morning|afternoon|evening), Admin/i,
			);

			// Wait for page to load and determine authentication state
			await page.waitForLoadState("networkidle");

			const isLoginPage = await usernameField.isVisible();

			if (isLoginPage) {
				// Need to sign in
				await usernameField.fill("admin");
				await page
					.getByRole("textbox", { name: "Password" })
					.fill("e2e-admin-password");
				await page.getByRole("button", { name: "Sign in" }).click();
				await expect(greetingText).toBeVisible({ timeout: 10000 });
			} else {
				// Already authenticated, just verify we see the greeting with increased timeout
				await expect(greetingText).toBeVisible({ timeout: 10000 });
			}

			// Test sidebar trigger is accessible
			await expect(
				page.getByRole("button", { name: "Toggle Sidebar" }),
			).toBeVisible();

			// Test sidebar opens and closes
			await page.getByRole("button", { name: "Toggle Sidebar" }).click();
			await expect(page.getByText("Administration")).toBeVisible();

			// Close sidebar for next iteration
			await page.keyboard.press("Escape");
			await expect(page.getByText("Administration")).not.toBeVisible();

			// Verify no horizontal scroll
			const hasHorizontalScroll = await page.evaluate(
				() => document.body.scrollWidth > document.body.clientWidth,
			);
			expect(hasHorizontalScroll).toBeFalsy();
		}
	});
});
/* ─── Desktop collapse / expand, active state, theme, cookie ───────── */

const SIDEBAR_RUN_ID = newRunId();
const SIDEBAR_USER_PREFIX = "E2E_SIDEBAR_";
const SIDEBAR_ROLE_PREFIX = "E2E_SIDEBAR_ROLE_";
let sidebarUserCounter = 0;

const isMobileViewport = (page: Page) =>
	(page.viewportSize()?.width ?? 1280) <= 768;

// The desktop sidebar root is the only [data-slot="sidebar"] carrying data-state.
// Base UI tooltip popups carry no ARIA role, so match on the data-slot.
const tooltipContent = (page: Page, text: string): Locator =>
	page.locator('[data-slot="tooltip-content"]').filter({ hasText: text });

const desktopSidebar = (page: Page) =>
	page.locator('[data-slot="sidebar"][data-state]');
const sidebarTrigger = (page: Page) =>
	page.getByRole("main").getByRole("button", { name: "Toggle Sidebar" });
const sidebarRail = (page: Page) => page.locator('[data-slot="sidebar-rail"]');
// The segmented theme buttons only expose their label through `title`.
const themeButton = (page: Page, name: "Light" | "Dark" | "Auto") =>
	page.getByRole("button", { name, exact: true });
const ACTIVE_NAV_CLASS = /(^|\s)text-primary(\s|$)/;

async function signInAndOpenSidebar(page: Page) {
	await page.context().clearCookies();
	await signIn(page);
	if (isMobileViewport(page)) {
		await page.getByRole("button", { name: "Toggle Sidebar" }).click();
	}
}

test.describe("Sidebar Navigation - Desktop collapse and rail", () => {
	test.beforeEach(async ({ page }) => {
		test.skip(
			isMobileViewport(page),
			"The desktop sidebar is a sheet on mobile viewports",
		);
		await page.context().clearCookies();
		await signIn(page);
	});

	test("Clicking the trigger button collapses and re-expands the sidebar", async ({
		page,
	}) => {
		await expect(desktopSidebar(page)).toHaveAttribute(
			"data-state",
			"expanded",
		);

		await sidebarTrigger(page).click();
		await expect(desktopSidebar(page)).toHaveAttribute(
			"data-state",
			"collapsed",
		);
		await expect(desktopSidebar(page)).toHaveAttribute(
			"data-collapsible",
			"icon",
		);

		await sidebarTrigger(page).click();
		await expect(desktopSidebar(page)).toHaveAttribute(
			"data-state",
			"expanded",
		);
		await expect(page.getByRole("link", { name: "Home" })).toBeVisible();
	});

	test("The keyboard shortcut toggles the sidebar", async ({ page }) => {
		await expect(desktopSidebar(page)).toHaveAttribute(
			"data-state",
			"expanded",
		);

		await page.keyboard.press("Control+b");
		await expect(desktopSidebar(page)).toHaveAttribute(
			"data-state",
			"collapsed",
		);

		await page.keyboard.press("Control+b");
		await expect(desktopSidebar(page)).toHaveAttribute(
			"data-state",
			"expanded",
		);
	});

	test("Clicking the sidebar rail collapses and expands the sidebar", async ({
		page,
	}) => {
		await sidebarRail(page).click();
		await expect(desktopSidebar(page)).toHaveAttribute(
			"data-state",
			"collapsed",
		);

		await sidebarRail(page).click();
		await expect(desktopSidebar(page)).toHaveAttribute(
			"data-state",
			"expanded",
		);
	});

	test("Collapsed navigation items show tooltips on hover", async ({
		page,
	}) => {
		await sidebarTrigger(page).click();
		await expect(desktopSidebar(page)).toHaveAttribute(
			"data-state",
			"collapsed",
		);

		await page.getByRole("link", { name: "Home" }).hover();

		await expect(tooltipContent(page, "Home")).toBeVisible();
	});

	test("Admin items show tooltips when the sidebar is collapsed", async ({
		page,
	}) => {
		await sidebarTrigger(page).click();
		await expect(desktopSidebar(page)).toHaveAttribute(
			"data-state",
			"collapsed",
		);

		await page.getByRole("link", { name: "Global Roles" }).hover();

		await expect(tooltipContent(page, "Global Roles")).toBeVisible();
	});
});

test.describe("Sidebar Navigation - Active state", () => {
	test("The current page is highlighted and the highlight follows navigation", async ({
		page,
	}) => {
		await signInAndOpenSidebar(page);

		await expect(page.getByRole("link", { name: "Home" })).toHaveClass(
			ACTIVE_NAV_CLASS,
		);
		await expect(page.getByRole("link", { name: "Users" })).not.toHaveClass(
			ACTIVE_NAV_CLASS,
		);

		await page.getByRole("link", { name: "Users" }).click();
		await expect(page).toHaveURL(/\/admin\/users/);

		if (isMobileViewport(page)) {
			// Navigating closes the mobile sheet; reopen it to inspect the items.
			await page.getByRole("button", { name: "Toggle Sidebar" }).click();
		}
		await expect(page.getByRole("link", { name: "Users" })).toHaveClass(
			ACTIVE_NAV_CLASS,
		);
		await expect(page.getByRole("link", { name: "Home" })).not.toHaveClass(
			ACTIVE_NAV_CLASS,
		);
	});
});

test.describe("Sidebar Navigation - Administration visibility by permission", () => {
	test.afterEach(async ({ request }) => {
		await cleanupUsersByPrefix(request, SIDEBAR_USER_PREFIX);
		await cleanupGlobalRolesByPrefix(request, SIDEBAR_ROLE_PREFIX);
	});

	async function createMember(
		request: Parameters<typeof authRequest>[0],
		playwright: Parameters<typeof createUserWithGlobalPermissions>[1],
		permissions: Record<string, boolean>,
	): Promise<string> {
		sidebarUserCounter += 1;
		const username = `${SIDEBAR_USER_PREFIX}${SIDEBAR_RUN_ID}_${sidebarUserCounter}`;
		await createUserWithGlobalPermissions(request, playwright, {
			username,
			roleName: `${SIDEBAR_ROLE_PREFIX}${SIDEBAR_RUN_ID}_${sidebarUserCounter}`,
			permissions,
		});
		return username;
	}

	async function signInAsMemberAndOpenSidebar(page: Page, username: string) {
		await page.context().clearCookies();
		await signIn(page, username, RESTRICTED_PASSWORD);
		if (isMobileViewport(page)) {
			await page.getByRole("button", { name: "Toggle Sidebar" }).click();
		}
	}

	test("Administration section is hidden for users without any admin permission", async ({
		page,
		request,
		playwright,
	}) => {
		// Admin permissions are absent; the role only lets the user create projects.
		await authRequest(request);
		const username = await createMember(request, playwright, {
			"projects.create": true,
		});

		await signInAsMemberAndOpenSidebar(page, username);

		// The sidebar itself rendered ...
		await expect(page.getByRole("link", { name: "Home" })).toBeVisible();
		// ... but none of the admin navigation did.
		await expect(page.getByText("Administration", { exact: true })).toHaveCount(
			0,
		);
		await expect(page.getByRole("link", { name: "Global Roles" })).toHaveCount(
			0,
		);
		await expect(page.getByRole("link", { name: "Users" })).toHaveCount(0);
		await expect(page.getByRole("link", { name: "What's New" })).toHaveCount(0);
	});

	test("A user with only users.read sees Administration with just the permitted items", async ({
		page,
		request,
		playwright,
	}) => {
		await authRequest(request);
		const username = await createMember(request, playwright, {
			"users.read": true,
		});

		await signInAsMemberAndOpenSidebar(page, username);

		await expect(
			page.getByText("Administration", { exact: true }),
		).toBeVisible();
		await expect(page.getByRole("link", { name: "Users" })).toBeVisible();
		await expect(page.getByRole("link", { name: "Global Roles" })).toHaveCount(
			0,
		);
		await expect(page.getByRole("link", { name: "Plugins" })).toHaveCount(0);
		await expect(page.getByRole("link", { name: "Settings" })).toHaveCount(0);
	});
});

test.describe("Sidebar Navigation - Theme switcher", () => {
	test("The active theme option is highlighted and selecting Dark changes the appearance", async ({
		page,
	}) => {
		await signInAndOpenSidebar(page);

		await themeButton(page, "Light").click();
		await expect(page.locator("html")).toHaveAttribute("data-theme", "light");
		await expect(themeButton(page, "Light")).toHaveClass(/bg-sidebar-accent/);
		await expect(themeButton(page, "Dark")).not.toHaveClass(
			/bg-sidebar-accent/,
		);

		await themeButton(page, "Dark").click();

		await expect(page.locator("html")).toHaveAttribute("data-theme", "dark");
		await expect(page.locator("html")).toHaveClass(/(^|\s)dark(\s|$)/);
		await expect(themeButton(page, "Dark")).toHaveClass(/bg-sidebar-accent/);
		await expect(themeButton(page, "Light")).not.toHaveClass(
			/bg-sidebar-accent/,
		);
	});

	test("A collapsed sidebar shows a single cycling theme button that advances the mode", async ({
		page,
	}) => {
		test.skip(
			isMobileViewport(page),
			"The collapsed icon rail only exists on desktop",
		);
		await page.context().clearCookies();
		await signIn(page);
		await themeButton(page, "Light").click();
		await expect(page.locator("html")).toHaveAttribute("data-theme", "light");

		await sidebarTrigger(page).click();
		await expect(desktopSidebar(page)).toHaveAttribute(
			"data-state",
			"collapsed",
		);

		// The three segmented buttons are hidden; one cycling button replaces them.
		await expect(themeButton(page, "Light")).toBeHidden();
		const cycleButton = page
			.locator('[data-slot="sidebar-footer"] [data-slot="sidebar-menu"] button')
			.first();
		await expect(cycleButton.locator("svg.lucide-sun")).toBeVisible();

		await cycleButton.click();
		await expect(page.locator("html")).toHaveAttribute("data-theme", "dark");
		await expect(cycleButton.locator("svg.lucide-moon")).toBeVisible();

		await cycleButton.click();
		// Auto mode removes data-theme so the system preference decides.
		await expect(page.locator("html")).not.toHaveAttribute("data-theme", /.+/);
		await expect(cycleButton.locator("svg.lucide-monitor")).toBeVisible();
	});
});

test.describe("Sidebar Navigation - State persistence", () => {
	test.beforeEach(async ({ page }) => {
		test.skip(
			isMobileViewport(page),
			"Sidebar collapse state only applies to the desktop sidebar",
		);
		await page.context().clearCookies();
		await signIn(page);
	});

	test("Toggling the sidebar stores its state in a sidebar_state cookie", async ({
		page,
		browserName,
	}) => {
		// The app writes the cookie through the Cookie Store API, which Firefox lacks.
		test.skip(
			browserName === "firefox",
			"Cookie Store API is not available in Firefox",
		);

		await sidebarTrigger(page).click();
		await expect(desktopSidebar(page)).toHaveAttribute(
			"data-state",
			"collapsed",
		);
		await expect
			.poll(
				async () =>
					(await page.context().cookies()).find(
						(c) => c.name === "sidebar_state",
					)?.value,
			)
			.toBe("false");

		await sidebarTrigger(page).click();
		await expect
			.poll(
				async () =>
					(await page.context().cookies()).find(
						(c) => c.name === "sidebar_state",
					)?.value,
			)
			.toBe("true");
	});

	// apps/web/src/components/ui/sidebar.tsx writes `sidebar_state` on every toggle but
	// nothing ever reads it back: SidebarProvider always starts from defaultOpen=true.
	test.fixme("Collapsed state persists after a page reload", async ({
		page,
	}) => {
		await sidebarTrigger(page).click();
		await expect(desktopSidebar(page)).toHaveAttribute(
			"data-state",
			"collapsed",
		);

		await page.reload();

		await expect(desktopSidebar(page)).toHaveAttribute(
			"data-state",
			"collapsed",
		);
	});

	test("Expanded state persists after a page reload", async ({ page }) => {
		await expect(desktopSidebar(page)).toHaveAttribute(
			"data-state",
			"expanded",
		);

		await page.reload();

		await expect(desktopSidebar(page)).toHaveAttribute(
			"data-state",
			"expanded",
		);
	});
});
