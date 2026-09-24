// spec: features/projects/jev.feature
// seed: tests/seed.spec.ts

import {
	type APIRequestContext,
	expect,
	type Page,
	test,
} from "@playwright/test";
import {
	API_URL,
	BASE_URL,
	cleanupProjectsByPrefix,
	createProject,
	newRunId,
	signIn,
} from "../helpers/e2e-api";

const TEST_PROJECT_PREFIX = "E2E_JEV_";
const RUN_ID = newRunId();
// Never reachable from the stack — Jev is only ever called through the
// stubbed connection test below, so this just needs to be a valid URL.
const FAKE_HOST = "https://jev.e2e.invalid/v1/systemone";

async function setJevConfig(
	request: APIRequestContext,
	projectId: string,
	body: { api_key?: string; base_url?: string; model?: string },
): Promise<void> {
	const resp = await request.patch(
		`${API_URL}/projects/${projectId}/jev-config`,
		{
			data: body,
		},
	);
	expect(resp.ok()).toBeTruthy();
}

async function getProject(
	request: APIRequestContext,
	projectId: string,
): Promise<Record<string, unknown>> {
	const resp = await request.get(`${API_URL}/projects/${projectId}`);
	expect(resp.ok()).toBeTruthy();
	return (await resp.json()).data;
}

async function createAutomation(
	request: APIRequestContext,
	projectId: string,
	name: string,
): Promise<string> {
	const resp = await request.post(
		`${API_URL}/projects/${projectId}/automations`,
		{
			data: { name },
		},
	);
	expect(resp.ok()).toBeTruthy();
	return (await resp.json()).data.id as string;
}

async function openJevSettings(page: Page, projectId: string): Promise<void> {
	await page.goto(`${BASE_URL}/projects/${projectId}/settings`);
	await expect(page.getByRole("heading", { name: "Settings" })).toBeVisible();
	await page.getByRole("button", { name: "Jev AI" }).click();
	await expect(page.getByRole("heading", { name: "Jev AI" })).toBeVisible();
}

async function openBuilder(
	page: Page,
	projectId: string,
	automationId: string,
	name: string,
): Promise<void> {
	await page.goto(
		`${BASE_URL}/projects/${projectId}/automation/${automationId}`,
	);
	await expect(page.getByRole("heading", { name })).toBeVisible();
}

/** Stubs the connection test's response — the stack has no real provider. */
async function stubConnectionTest(page: Page, ok: boolean): Promise<void> {
	await page.route("**/api/v1/projects/*/jev-config/test", (route) =>
		route.fulfill({
			status: ok ? 200 : 400,
			contentType: "application/json",
			body: JSON.stringify(
				ok
					? { success: true, data: { success: true } }
					: {
							success: false,
							error_code: "BAD_REQUEST",
							error: "Could not reach Jev with these credentials",
						},
			),
		}),
	);
}

const apiKeyInput = (page: Page) =>
	page.getByRole("textbox", { name: "API Key" });

test.describe("Jev AI project settings", () => {
	let projectId: string;

	test.beforeEach(async ({ request, context }) => {
		await cleanupProjectsByPrefix(request, TEST_PROJECT_PREFIX);
		projectId = await createProject(
			request,
			`${TEST_PROJECT_PREFIX}${RUN_ID}_${Math.random().toString(36).slice(2, 7)}`,
		);
		await context.clearCookies();
	});

	test.afterEach(async ({ request }) => {
		await cleanupProjectsByPrefix(request, TEST_PROJECT_PREFIX);
	});

	// ── Rule: Unconfigured project ──────────────────────────────────────────

	test.describe("Unconfigured project", () => {
		test("Jev AI section shows the not-configured state", async ({ page }) => {
			await signIn(page);
			await openJevSettings(page, projectId);

			await expect(
				page.getByText("Not configured", { exact: true }),
			).toBeVisible();
			await expect(page.getByText("Jev isn't configured yet")).toBeVisible();
			await expect(
				page.getByText("Task Auto-fill", { exact: true }),
			).toHaveCount(0);
			await expect(
				page.getByText("Task Auto-assign", { exact: true }),
			).toHaveCount(0);
			await expect(
				page.getByRole("button", { name: "Test connection" }),
			).toHaveCount(0);
			await expect(
				page.getByRole("button", { name: "Save credentials" }),
			).toBeDisabled();
		});

		test("the Jev Condition node is hidden from the automation palette", async ({
			page,
			request,
		}) => {
			const name = `${TEST_PROJECT_PREFIX}AUTO_${RUN_ID}`;
			const automationId = await createAutomation(request, projectId, name);

			await signIn(page);
			await openBuilder(page, projectId, automationId, name);
			await page.getByRole("button", { name: "Add Condition" }).click();

			await expect(
				page.getByRole("menuitem", { name: "Condition", exact: true }),
			).toBeVisible();
			await expect(
				page.getByRole("menuitem", { name: "Jev Condition" }),
			).toHaveCount(0);
		});
	});

	// ── Rule: Configuring credentials ───────────────────────────────────────

	test.describe("Configuring credentials", () => {
		test("saving credentials enables Jev and never reveals the key", async ({
			page,
			request,
		}) => {
			await signIn(page);
			await openJevSettings(page, projectId);

			await apiKeyInput(page).fill("e2e-secret-jev-key");
			await page.getByLabel("Host (optional)").fill(FAKE_HOST);
			await page.getByLabel("Model (optional)").fill("e2e-model");
			await page.getByRole("button", { name: "Save credentials" }).click();

			await expect(page.getByText("Saved ✓")).toBeVisible();
			await expect(page.getByText("Configured", { exact: true })).toBeVisible();
			await expect(apiKeyInput(page)).toHaveValue("");
			await expect(apiKeyInput(page)).toHaveAttribute(
				"placeholder",
				"Leave blank to keep the current key",
			);
			await expect(
				page.getByText("Task Auto-fill", { exact: true }),
			).toBeVisible();
			await expect(
				page.getByText("Task Auto-assign", { exact: true }),
			).toBeVisible();

			await page.reload();
			await page.getByRole("button", { name: "Jev AI" }).click();
			await expect(page.getByLabel("Host (optional)")).toHaveValue(FAKE_HOST);
			await expect(page.getByLabel("Model (optional)")).toHaveValue(
				"e2e-model",
			);

			const project = await getProject(request, projectId);
			expect(project.jev_configured).toBe(true);
			expect(project.jev_base_url).toBe(FAKE_HOST);
			expect(project.jev_model).toBe("e2e-model");
			expect(JSON.stringify(project)).not.toContain("e2e-secret-jev-key");
		});

		test("testing the connection reports success", async ({
			page,
			request,
		}) => {
			await setJevConfig(request, projectId, {
				api_key: "k",
				base_url: FAKE_HOST,
			});
			await stubConnectionTest(page, true);

			await signIn(page);
			await openJevSettings(page, projectId);
			await page.getByRole("button", { name: "Test connection" }).click();

			await expect(page.getByText("Connection successful")).toBeVisible();
		});

		test("testing the connection reports failure", async ({
			page,
			request,
		}) => {
			await setJevConfig(request, projectId, {
				api_key: "k",
				base_url: FAKE_HOST,
			});
			await stubConnectionTest(page, false);

			await signIn(page);
			await openJevSettings(page, projectId);
			await page.getByRole("button", { name: "Test connection" }).click();

			await expect(page.getByText(/Couldn't connect/)).toBeVisible();
		});

		test("clearing the key disables Jev again", async ({ page, request }) => {
			await setJevConfig(request, projectId, {
				api_key: "k",
				base_url: FAKE_HOST,
			});
			await setJevConfig(request, projectId, { api_key: "" });

			await signIn(page);
			await openJevSettings(page, projectId);

			await expect(
				page.getByText("Not configured", { exact: true }),
			).toBeVisible();
			await expect(page.getByText("Jev isn't configured yet")).toBeVisible();
			expect((await getProject(request, projectId)).jev_configured).toBe(false);
		});
	});

	// ── Rule: Feature settings ──────────────────────────────────────────────

	test.describe("Feature settings", () => {
		test.beforeEach(async ({ request }) => {
			await setJevConfig(request, projectId, {
				api_key: "k",
				base_url: FAKE_HOST,
			});
		});

		test("excluding fields and narrowing auto-assign scope persists", async ({
			page,
			request,
		}) => {
			await signIn(page);
			await openJevSettings(page, projectId);

			const save = page.getByRole("button", { name: "Save changes" });
			await expect(save).toBeDisabled();
			await page
				.getByRole("button", { name: "Importance", exact: true })
				.click();
			await page.getByRole("button", { name: "Human members only" }).click();
			await expect(
				page.getByText("Excludes AI agents from consideration."),
			).toBeVisible();
			await save.click();

			await expect(page.getByText("Saved ✓")).toBeVisible();
			const settings = (await getProject(request, projectId)).settings as {
				jev?: Record<string, unknown>;
			};
			expect(settings.jev).toEqual({
				autofill_enabled: true,
				autofill_excluded_fields: ["importance"],
				auto_assign_scope: "human",
			});

			await page.reload();
			await page.getByRole("button", { name: "Jev AI" }).click();
			await expect(
				page.getByText("Excludes AI agents from consideration."),
			).toBeVisible();
			await expect(
				page.getByRole("button", { name: "Save changes" }),
			).toBeDisabled();
		});

		test("turning auto-fill off hides the field list and persists", async ({
			page,
			request,
		}) => {
			await signIn(page);
			await openJevSettings(page, projectId);

			await expect(page.getByText("Fields Jev can set")).toBeVisible();
			await page.getByRole("switch").click();
			await expect(page.getByText("Fields Jev can set")).toHaveCount(0);
			await page.getByRole("button", { name: "Save changes" }).click();
			await expect(page.getByText("Saved ✓")).toBeVisible();

			const settings = (await getProject(request, projectId)).settings as {
				jev?: Record<string, unknown>;
			};
			expect(settings.jev?.autofill_enabled).toBe(false);
		});

		test("the Jev Condition node is offered once Jev is configured", async ({
			page,
			request,
		}) => {
			const name = `${TEST_PROJECT_PREFIX}AUTO_${RUN_ID}`;
			const automationId = await createAutomation(request, projectId, name);

			await signIn(page);
			await openBuilder(page, projectId, automationId, name);
			await page.getByRole("button", { name: "Add Condition" }).click();
			await page.getByRole("menuitem", { name: "Jev Condition" }).click();

			await expect(
				page.getByTitle("Jev Condition", { exact: true }),
			).toBeVisible();
			await expect(page.getByText("Answer type")).toBeVisible();
			await expect(page.getByText("Question for Jev")).toBeVisible();
		});
	});
});
