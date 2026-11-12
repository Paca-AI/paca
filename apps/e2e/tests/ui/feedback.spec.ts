// spec: features/ui/feedback.feature
// seed: tests/seed.spec.ts

import { expect, type Page, test } from "@playwright/test";
import { ensureLoginForm } from "../helpers/e2e-api";

const ISSUES_ORIGIN = "https://github.com";

function profileMenuButton(page: Page) {
	return page.getByRole("button", { name: /Admin super_admin/i });
}

async function signInAsAdmin(page: Page) {
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
}

/** The real GitHub is never hit: any request to it is answered locally. */
async function stubGithub(page: Page) {
	await page.context().route(`${ISSUES_ORIGIN}/**`, (route) =>
		route.fulfill({
			status: 200,
			contentType: "text/html",
			body: "<html><body>stub</body></html>",
		}),
	);
}

/** Makes the admin agents route's loader request fail with `status`. */
async function failAgentsLoader(page: Page, status: number) {
	await page.route("**/api/v1/agents/llm-models", (route) =>
		route.fulfill({
			status,
			contentType: "application/json",
			body: JSON.stringify({
				success: false,
				error: { code: "E2E_FORCED_ERROR", message: "forced e2e failure" },
			}),
		}),
	);
}

test.describe("Feedback", () => {
	test.beforeEach(async ({ context }) => {
		await context.clearCookies();
		await context.clearPermissions();
	});

	test("The user menu opens the GitHub issue chooser in a new tab", async ({
		page,
	}) => {
		await signInAsAdmin(page);
		await stubGithub(page);

		const viewport = page.viewportSize();
		if (viewport && viewport.width <= 768) {
			await page.getByRole("button", { name: "Toggle Sidebar" }).click();
		}
		await profileMenuButton(page).click();

		const popupPromise = page.waitForEvent("popup");
		await page.getByRole("menuitem", { name: "Send feedback" }).click();
		const popup = await popupPromise;

		const url = new URL(popup.url());
		expect(url.origin).toBe(ISSUES_ORIGIN);
		expect(url.pathname).toBe("/Paca-AI/paca/issues/new/choose");
		// The app itself stays where it was.
		await expect(page).toHaveURL(/\/home/);
	});

	test("An error screen opens a pre-filled bug report with the error details", async ({
		page,
	}) => {
		await signInAsAdmin(page);
		await stubGithub(page);
		// 4xx responses are not retried by the app, so the error screen shows
		// immediately instead of after three backoff attempts.
		await failAgentsLoader(page, 400);

		await page.goto("/admin/agents");
		await expect(page.getByText("Something went wrong")).toBeVisible();

		const popupPromise = page.waitForEvent("popup");
		await page.getByRole("button", { name: "Report this issue" }).click();
		const popup = await popupPromise;

		const url = new URL(popup.url());
		expect(url.origin).toBe(ISSUES_ORIGIN);
		expect(url.pathname).toBe("/Paca-AI/paca/issues/new");
		const params = url.searchParams;
		expect(params.get("template")).toBe("bug_report.yml");
		expect(params.get("title")).toMatch(/^\[Bug\] .+/);
		expect(params.get("area")).toBe("Frontend");
		expect(params.get("environment")).toContain("Page: /admin/agents");
		const logs = params.get("logs") ?? "";
		expect(logs).toContain("HTTP status: 400");
		expect(logs).toContain("/agents/llm-models");
		expect(logs).toContain("E2E_FORCED_ERROR");
	});

	test("A permission error does not offer the report button", async ({
		page,
	}) => {
		await signInAsAdmin(page);
		await failAgentsLoader(page, 403);

		await page.goto("/admin/agents");
		await expect(
			page.getByText("You don't have permission to view this"),
		).toBeVisible();
		await expect(
			page.getByRole("button", { name: "Report this issue" }),
		).toHaveCount(0);
	});
});
