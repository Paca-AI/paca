// spec: features/projects/view-settings.feature, features/projects/view-settings-fields.feature
// seed: tests/seed.spec.ts

import { ensureLoginForm } from '../helpers/e2e-api';
import {
	type APIRequestContext,
	expect,
	type Locator,
	type Page,
	request as playwrightRequest,
	test,
} from "@playwright/test";

const BASE_URL = process.env.E2E_BASE_URL ?? "http://localhost";
const USERNAME = process.env.E2E_USERNAME ?? "admin";
const PASSWORD = process.env.E2E_PASSWORD ?? "e2e-admin-password";
const TEST_PROJECT_PREFIX = "E2E_VS_";
const RUN_ID = Date.now().toString(36).slice(-5).toUpperCase();
const VIEWER_USERNAME = `E2E_VS_VIEWER_${RUN_ID}`;
const VIEWER_TEMP_PASSWORD = "ViewerTemp123!";
const VIEWER_PASSWORD = "ViewerPass123!";

// ─── Types ────────────────────────────────────────────────────────────────────

type ViewConfig = Record<string, unknown> & {
	sort_by?: string;
	fields?: string[];
};

interface InteractionView {
	id: string;
	name: string;
	view_type: string;
	config: ViewConfig;
	shared_config: ViewConfig;
	is_personalized: boolean;
}

// ─── API Helpers ──────────────────────────────────────────────────────────────

async function authRequest(request: APIRequestContext): Promise<void> {
	await request.post(`${BASE_URL}/api/v1/auth/login`, {
		data: { username: USERNAME, password: PASSWORD, rememberMe: false },
	});
}

async function cleanupTestProjects(request: APIRequestContext): Promise<void> {
	await authRequest(request);

	const allProjects: Array<{ id: string; name: string }> = [];
	let page = 1;

	while (true) {
		const listResp = await request.get(
			`${BASE_URL}/api/v1/projects?page=${page}&page_size=100`,
		);
		if (!listResp.ok()) break;
		const body = await listResp.json();
		const items: Array<{ id: string; name: string }> = body?.data?.items ?? [];
		if (items.length === 0) break;
		allProjects.push(...items);
		const {
			page: currentPage,
			page_size,
			total,
		} = body.data as {
			page: number;
			page_size: number;
			total: number;
		};
		if (currentPage * page_size >= total) break;
		page++;
	}

	await Promise.all(
		allProjects
			.filter((p) => p.name.startsWith(TEST_PROJECT_PREFIX))
			.map((p) => request.delete(`${BASE_URL}/api/v1/projects/${p.id}`)),
	);
}

async function createProject(
	request: APIRequestContext,
	name: string,
): Promise<string> {
	const resp = await request.post(`${BASE_URL}/api/v1/projects`, {
		data: { name },
	});
	const body = await resp.json();
	return body.data.id as string;
}

async function createCustomField(
	request: APIRequestContext,
	projectId: string,
	field: { display_name: string; field_type: string; options?: string[] },
): Promise<void> {
	const fieldKey = field.display_name
		.toLowerCase()
		.replace(/[^a-z0-9]+/g, "_")
		.replace(/^_|_$/g, "");
	const resp = await request.post(
		`${BASE_URL}/api/v1/projects/${projectId}/custom-fields`,
		{
			data: {
				display_name: field.display_name,
				field_key: fieldKey,
				field_type: field.field_type,
				options: field.options ?? [],
				is_required: false,
			},
		},
	);
	expect(resp.ok()).toBeTruthy();
}

// The project's Product Backlog ships with a single default Table view.
async function getBacklogView(
	request: APIRequestContext,
	projectId: string,
): Promise<InteractionView> {
	const resp = await request.get(
		`${BASE_URL}/api/v1/projects/${projectId}/views?context=backlog`,
	);
	expect(resp.ok()).toBeTruthy();
	const body = await resp.json();
	const items = (body?.data?.items ?? []) as InteractionView[];
	expect(items.length).toBeGreaterThan(0);
	return items[0];
}

async function savePersonalConfig(
	request: APIRequestContext,
	projectId: string,
	viewId: string,
	config: ViewConfig,
): Promise<void> {
	const resp = await request.put(
		`${BASE_URL}/api/v1/projects/${projectId}/views/${viewId}/config`,
		{
			data: { config },
		},
	);
	expect(resp.ok()).toBeTruthy();
}

async function findRoleId(
	request: APIRequestContext,
	projectId: string,
	roleName: string,
): Promise<string> {
	const resp = await request.get(
		`${BASE_URL}/api/v1/projects/${projectId}/roles`,
	);
	expect(resp.ok()).toBeTruthy();
	const body = await resp.json();
	const roles = (
		Array.isArray(body.data) ? body.data : (body.data?.items ?? [])
	) as Array<{
		id: string;
		role_name: string;
	}>;
	const role = roles.find((r) => r.role_name === roleName);
	expect(role, `project role "${roleName}" should exist`).toBeTruthy();
	return (role as { id: string }).id;
}

// Creates a plain (non-admin) user, clears its forced first-login password
// change over the API so the UI sign-in lands on the home page, and adds it to
// the project with the default "Viewer" role (views.read but NOT views.write).
async function createViewerMember(
	request: APIRequestContext,
	projectId: string,
): Promise<string> {
	const createResp = await request.post(`${BASE_URL}/api/v1/admin/users`, {
		data: {
			username: VIEWER_USERNAME,
			full_name: "E2E View Settings Viewer",
			role: "USER",
			password: VIEWER_TEMP_PASSWORD,
		},
	});
	expect(createResp.ok()).toBeTruthy();
	const userId = (await createResp.json()).data.id as string;

	const userApi = await playwrightRequest.newContext();
	try {
		const login = await userApi.post(`${BASE_URL}/api/v1/auth/login`, {
			data: {
				username: VIEWER_USERNAME,
				password: VIEWER_TEMP_PASSWORD,
				rememberMe: false,
			},
		});
		expect(login.ok()).toBeTruthy();
		const change = await userApi.patch(`${BASE_URL}/api/v1/users/me/password`, {
			data: {
				current_password: VIEWER_TEMP_PASSWORD,
				new_password: VIEWER_PASSWORD,
			},
		});
		expect(change.ok()).toBeTruthy();
	} finally {
		await userApi.dispose();
	}

	const roleId = await findRoleId(request, projectId, "Viewer");
	const addResp = await request.post(
		`${BASE_URL}/api/v1/projects/${projectId}/members`,
		{
			data: { user_id: userId, project_role_id: roleId },
		},
	);
	expect(addResp.ok()).toBeTruthy();
	return userId;
}

async function deleteUser(
	request: APIRequestContext,
	userId: string | undefined,
): Promise<void> {
	if (!userId) return;
	await request.delete(`${BASE_URL}/api/v1/admin/users/${userId}`);
}

async function viewerApiContext(): Promise<APIRequestContext> {
	const ctx = await playwrightRequest.newContext();
	const login = await ctx.post(`${BASE_URL}/api/v1/auth/login`, {
		data: {
			username: VIEWER_USERNAME,
			password: VIEWER_PASSWORD,
			rememberMe: false,
		},
	});
	expect(login.ok()).toBeTruthy();
	return ctx;
}

// ─── UI Helpers ───────────────────────────────────────────────────────────────

const signIn = async (page: Page, username = USERNAME, password = PASSWORD) => {
	await page.goto(`${BASE_URL}/`);
	await ensureLoginForm(page);
	await page.getByRole("textbox", { name: "Username" }).fill(username);
	await page.getByRole("textbox", { name: "Password" }).fill(password);
	await page.getByRole("button", { name: "Sign in" }).click();
	await expect(
		page.getByRole("heading", { name: /Good (morning|afternoon|evening)/i }),
	).toBeVisible();
};

const navigateToBacklog = async (page: Page, projectId: string) => {
	await page.goto(`${BASE_URL}/projects/${projectId}/interactions/backlog`);
	await expect(
		page.getByRole("heading", { name: "Product Backlog" }),
	).toBeVisible({ timeout: 30_000 });
};

const panelTitle = (page: Page): Locator =>
	page.getByText("View settings", { exact: true });

const openSettings = async (page: Page) => {
	await page.getByRole("button", { name: "View settings", exact: true }).click();
	await expect(panelTitle(page)).toBeVisible();
};

// The dropdown trigger sits right after its row label inside the same row.
const settingRowButton = (page: Page, label: string): Locator =>
	page
		.getByText(label, { exact: true })
		.locator("xpath=following-sibling::button");

// Option lists render in a portal appended after the panel, so the last
// matching button is the option, not a same-named table header / filter group.
const optionButton = (page: Page, name: string): Locator =>
	page.getByRole("button", { name, exact: true }).last();

// Picking an option leaves the dropdown popover open; clicking the panel title
// (inside the outer popover) dismisses just the inner one.
const selectDropdownOption = async (
	page: Page,
	rowLabel: string,
	option: string,
) => {
	await settingRowButton(page, rowLabel).click();
	await optionButton(page, option).click();
	await panelTitle(page).click();
	await expect(settingRowButton(page, rowLabel)).toHaveText(option);
};

const saveForEveryoneButton = (page: Page): Locator =>
	page.getByRole("button", { name: "Save for everyone" });
const saveDropdownTrigger = (page: Page): Locator =>
	saveForEveryoneButton(page).locator("xpath=following-sibling::button");
const saveOnlyForMeButton = (page: Page): Locator =>
	page.getByRole("button", { name: "Save only for me" });
const plainSaveButton = (page: Page): Locator =>
	page.getByRole("button", { name: "Save", exact: true });
const resetButton = (page: Page): Locator =>
	page.getByRole("button", { name: "Reset", exact: true });
const personalBadge = (page: Page): Locator =>
	page.getByText("Only visible to you");

const fieldsSummaryButton = (page: Page): Locator =>
	settingRowButton(page, "Fields");

const openFieldPicker = async (page: Page) => {
	await fieldsSummaryButton(page).click();
	await expect(page.getByText("Choose fields")).toBeVisible();
};

const saveForEveryoneAndWaitClosed = async (page: Page) => {
	await saveForEveryoneButton(page).click();
	await expect(saveForEveryoneButton(page)).toBeHidden();
};

// ===========================================================================
// Rule: View settings panel includes all built-in and custom field options
// ===========================================================================

test.describe("View settings panel content", () => {
	let projectId: string;

	test.beforeEach(async ({ request, context }) => {
		await cleanupTestProjects(request);
		projectId = await createProject(
			request,
			`${TEST_PROJECT_PREFIX}PANEL_${RUN_ID}`,
		);
		await context.clearCookies();
		await context.clearPermissions();
	});

	test.afterEach(async ({ request }) => {
		await cleanupTestProjects(request);
	});

	test("The view settings panel opens with all display setting rows and the Filters section", async ({
		page,
	}) => {
		await signIn(page);
		await navigateToBacklog(page, projectId);
		await openSettings(page);

		for (const label of [
			"Fields",
			"Column by",
			"Swimlanes",
			"Sort by",
			"Field sum",
			"Initial size",
			"Per page",
		]) {
			await expect(page.getByText(label, { exact: true })).toBeVisible();
		}
		await expect(page.getByText("Display", { exact: true })).toBeVisible();
		await expect(page.getByText("Filters", { exact: true })).toBeVisible();
		await expect(saveForEveryoneButton(page)).toBeVisible();
	});

	test("The Reset button only appears once the draft differs from the team default", async ({
		page,
	}) => {
		await signIn(page);
		await navigateToBacklog(page, projectId);
		await openSettings(page);

		await expect(resetButton(page)).toHaveCount(0);

		await selectDropdownOption(page, "Sort by", "Title");
		await expect(resetButton(page)).toBeVisible();
	});

	test('"Column by" dropdown includes all built-in fields', async ({
		page,
	}) => {
		await signIn(page);
		await navigateToBacklog(page, projectId);
		await openSettings(page);

		await settingRowButton(page, "Column by").click();
		for (const option of [
			"Status",
			"Sprint",
			"Assignee",
			"Importance",
			"Type",
			"Reporter",
		]) {
			await expect(optionButton(page, option)).toBeVisible();
		}
	});

	test('"Sort by" dropdown includes all built-in sort options', async ({
		page,
	}) => {
		await signIn(page);
		await navigateToBacklog(page, projectId);
		await openSettings(page);

		await settingRowButton(page, "Sort by").click();
		for (const option of [
			"Manual",
			"Importance",
			"Story Points",
			"Title",
			"Created",
			"Start Date",
			"Due Date",
		]) {
			await expect(optionButton(page, option)).toBeVisible();
		}
	});

	test('"Swimlanes" dropdown includes None plus the built-in groupings', async ({
		page,
	}) => {
		await signIn(page);
		await navigateToBacklog(page, projectId);
		await openSettings(page);

		await settingRowButton(page, "Swimlanes").click();
		for (const option of ["None", "Assignee", "Importance", "Type"]) {
			await expect(optionButton(page, option)).toBeVisible();
		}
	});

	test('"Field sum" dropdown includes Count and Story Points', async ({
		page,
	}) => {
		await signIn(page);
		await navigateToBacklog(page, projectId);
		await openSettings(page);

		await settingRowButton(page, "Field sum").click();
		await expect(optionButton(page, "Count")).toBeVisible();
		await expect(optionButton(page, "Story Points")).toBeVisible();
	});

	test("Custom fields of selectable types appear in the dropdowns that support them", async ({
		page,
		request,
	}) => {
		await createCustomField(request, projectId, {
			display_name: "Severity",
			field_type: "select",
			options: ["Low", "Medium", "High"],
		});
		await createCustomField(request, projectId, {
			display_name: "Complexity",
			field_type: "number",
		});

		await signIn(page);
		await navigateToBacklog(page, projectId);
		await openSettings(page);

		await settingRowButton(page, "Column by").click();
		await expect(optionButton(page, "Severity")).toBeVisible();
		await expect(optionButton(page, "Complexity")).toBeVisible();
		await panelTitle(page).click();

		await settingRowButton(page, "Swimlanes").click();
		await expect(optionButton(page, "Severity")).toBeVisible();
		await panelTitle(page).click();

		await settingRowButton(page, "Sort by").click();
		await expect(optionButton(page, "Severity")).toBeVisible();
		await expect(optionButton(page, "Complexity")).toBeVisible();
		await panelTitle(page).click();

		await settingRowButton(page, "Field sum").click();
		await expect(optionButton(page, "Complexity")).toBeVisible();
	});
});

// ===========================================================================
// Rule: The Filters section narrows which tasks a view shows
// ===========================================================================

test.describe("View settings Filters section", () => {
	let projectId: string;

	test.beforeEach(async ({ request, context }) => {
		await cleanupTestProjects(request);
		projectId = await createProject(
			request,
			`${TEST_PROJECT_PREFIX}FILTERS_${RUN_ID}`,
		);
		await context.clearCookies();
		await context.clearPermissions();
	});

	test.afterEach(async ({ request }) => {
		await cleanupTestProjects(request);
	});

	test("The Filters section lists a collapsible group per filterable field", async ({
		page,
	}) => {
		await signIn(page);
		await navigateToBacklog(page, projectId);
		await openSettings(page);

		for (const group of [
			"Sprints",
			"Statuses",
			"Assignees",
			"Task types",
			"Start Date",
			"Due Date",
			"Importance",
			"Story Points",
			"Tags",
		]) {
			// A group with an active filter carries a count badge in its accessible
			// name ("Task types 1" — the default backlog view filters to normal
			// task types), so match on the label prefix.
			await expect(
				page
					.getByRole("button", { name: new RegExp(`^${group}( \\d+)?$`) })
					.last(),
			).toBeVisible();
		}
	});

	test("Every custom field gets its own filter group", async ({
		page,
		request,
	}) => {
		await createCustomField(request, projectId, {
			display_name: "Component",
			field_type: "select",
			options: ["Frontend", "Backend", "API"],
		});

		await signIn(page);
		await navigateToBacklog(page, projectId);
		await openSettings(page);

		await expect(
			page.getByRole("button", { name: "Component", exact: true }).last(),
		).toBeVisible();
	});

	test('"Clear filters" removes every active filter', async ({ page }) => {
		await signIn(page);
		await navigateToBacklog(page, projectId);
		await openSettings(page);

		// The default Product Backlog view ships with a task-type filter, so the
		// button and a "Task types" badge are already present.
		await expect(
			page.getByRole("button", { name: "Clear filters" }),
		).toBeVisible();
		await expect(
			page.getByRole("button", { name: "Task types 1", exact: true }),
		).toBeVisible();

		await page
			.getByRole("button", { name: "Importance", exact: true })
			.last()
			.click();
		// With no importance filter every level starts checked, and clicking a
		// level narrows the filter to just that level (so .check() would be a no-op).
		await page.getByRole("checkbox", { name: "Critical" }).click();
		await expect(
			page.getByRole("button", { name: "Importance 1", exact: true }),
		).toBeVisible();

		await page.getByRole("button", { name: "Clear filters" }).click();
		await expect(
			page.getByRole("button", { name: "Clear filters" }),
		).toHaveCount(0);
		await expect(
			page.getByRole("button", { name: "Task types", exact: true }),
		).toBeVisible();
		await expect(
			page.getByRole("button", { name: "Importance", exact: true }),
		).toBeVisible();
	});
});

// ===========================================================================
// Rule: Field picker — content and interaction
// (features/projects/view-settings-fields.feature)
// ===========================================================================

test.describe("Field picker", () => {
	let projectId: string;

	test.beforeEach(async ({ request, context }) => {
		await cleanupTestProjects(request);
		projectId = await createProject(
			request,
			`${TEST_PROJECT_PREFIX}FIELDS_${RUN_ID}`,
		);
		await context.clearCookies();
		await context.clearPermissions();
	});

	test.afterEach(async ({ request }) => {
		await cleanupTestProjects(request);
	});

	test('The "Fields" summary row shows the defaults when no fields are configured', async ({
		page,
	}) => {
		await signIn(page);
		await navigateToBacklog(page, projectId);
		await openSettings(page);

		await expect(fieldsSummaryButton(page)).toHaveText(
			"Title, Assignee, Importance, Story Points, Type",
		);
	});

	test("Opening the field picker lists all built-in fields but not Title", async ({
		page,
	}) => {
		await signIn(page);
		await navigateToBacklog(page, projectId);
		await openSettings(page);
		await openFieldPicker(page);

		for (const field of [
			"Assignee",
			"Status",
			"Importance",
			"Story Points",
			"Type",
			"Epic",
			"Tags",
			"Reporter",
			"Start Date",
			"Due Date",
			"Created",
		]) {
			await expect(
				page.getByRole("checkbox", { name: field, exact: true }),
			).toBeVisible();
		}
		await expect(
			page.getByRole("checkbox", { name: "Title", exact: true }),
		).toHaveCount(0);
	});

	test("The default visible fields are Assignee, Importance, Story Points, and Type", async ({
		page,
	}) => {
		await signIn(page);
		await navigateToBacklog(page, projectId);
		await openSettings(page);
		await openFieldPicker(page);

		for (const field of ["Assignee", "Importance", "Story Points", "Type"]) {
			await expect(
				page.getByRole("checkbox", { name: field, exact: true }),
			).toBeChecked();
		}
		for (const field of [
			"Status",
			"Epic",
			"Tags",
			"Reporter",
			"Start Date",
			"Due Date",
			"Created",
		]) {
			await expect(
				page.getByRole("checkbox", { name: field, exact: true }),
			).not.toBeChecked();
		}
	});

	test("Field picker also lists project custom fields", async ({
		page,
		request,
	}) => {
		await createCustomField(request, projectId, {
			display_name: "Severity",
			field_type: "select",
			options: ["Low", "Medium", "High"],
		});
		await createCustomField(request, projectId, {
			display_name: "Is Blocked",
			field_type: "boolean",
		});
		await createCustomField(request, projectId, {
			display_name: "Notes",
			field_type: "text",
		});

		await signIn(page);
		await navigateToBacklog(page, projectId);
		await openSettings(page);
		await openFieldPicker(page);

		for (const field of ["Severity", "Is Blocked", "Notes"]) {
			await expect(
				page.getByRole("checkbox", { name: field, exact: true }),
			).not.toBeChecked();
		}
	});

	test('Checking a disabled field adds it to the "Fields" summary once saved', async ({
		page,
		request,
	}) => {
		await signIn(page);
		await navigateToBacklog(page, projectId);
		await openSettings(page);
		await openFieldPicker(page);

		await page.getByRole("checkbox", { name: "Due Date", exact: true }).check();
		await saveForEveryoneAndWaitClosed(page);

		const view = await getBacklogView(request, projectId);
		expect(view.shared_config.fields).toContain("due_date");

		await openSettings(page);
		await expect(fieldsSummaryButton(page)).toContainText("Due Date");
	});

	test('Unchecking an enabled field removes it from the "Fields" summary once saved', async ({
		page,
		request,
	}) => {
		await signIn(page);
		await navigateToBacklog(page, projectId);
		await openSettings(page);
		await openFieldPicker(page);

		await page
			.getByRole("checkbox", { name: "Importance", exact: true })
			.uncheck();
		await saveForEveryoneAndWaitClosed(page);

		const view = await getBacklogView(request, projectId);
		expect(view.shared_config.fields).not.toContain("importance");

		await openSettings(page);
		await expect(fieldsSummaryButton(page)).not.toContainText("Importance");
	});

	test("Field settings survive a page reload", async ({ page }) => {
		await signIn(page);
		await navigateToBacklog(page, projectId);
		await openSettings(page);
		await openFieldPicker(page);

		await page.getByRole("checkbox", { name: "Due Date", exact: true }).check();
		await page
			.getByRole("checkbox", { name: "Importance", exact: true })
			.uncheck();
		await saveForEveryoneAndWaitClosed(page);

		await page.reload();
		await expect(
			page.getByRole("heading", { name: "Product Backlog" }),
		).toBeVisible({ timeout: 30_000 });
		await openSettings(page);

		await expect(fieldsSummaryButton(page)).toContainText("Due Date");
		await expect(fieldsSummaryButton(page)).not.toContainText("Importance");
	});
});

// ===========================================================================
// Rule: Settings changes preview immediately and are discarded unless saved
// ===========================================================================

test.describe("Unsaved settings changes", () => {
	let projectId: string;

	test.beforeEach(async ({ request, context }) => {
		await cleanupTestProjects(request);
		projectId = await createProject(
			request,
			`${TEST_PROJECT_PREFIX}DRAFT_${RUN_ID}`,
		);
		await context.clearCookies();
		await context.clearPermissions();
	});

	test.afterEach(async ({ request }) => {
		await cleanupTestProjects(request);
	});

	test("Changing a setting previews it in the panel without persisting anything", async ({
		page,
		request,
	}) => {
		const before = await getBacklogView(request, projectId);

		await signIn(page);
		await navigateToBacklog(page, projectId);
		await openSettings(page);
		await selectDropdownOption(page, "Sort by", "Title");

		const after = await getBacklogView(request, projectId);
		expect(after.config.sort_by).toEqual(before.config.sort_by);
		expect(after.shared_config.sort_by).toEqual(before.shared_config.sort_by);
		expect(after.is_personalized).toBe(false);
	});

	test("Closing the popup without saving discards all unsaved changes", async ({
		page,
	}) => {
		await signIn(page);
		await navigateToBacklog(page, projectId);
		await openSettings(page);
		await selectDropdownOption(page, "Sort by", "Title");

		await page.keyboard.press("Escape");
		await expect(saveForEveryoneButton(page)).toBeHidden();

		await openSettings(page);
		await expect(settingRowButton(page, "Sort by")).toHaveText("Manual");
	});
});

// ===========================================================================
// Rule: A views.write holder can save settings for everyone or only for themselves
// ===========================================================================

test.describe("Personal vs shared view settings — views.write holder", () => {
	let projectId: string;
	let viewerUserId: string | undefined;

	test.beforeEach(async ({ request, context }) => {
		await cleanupTestProjects(request);
		projectId = await createProject(
			request,
			`${TEST_PROJECT_PREFIX}SHARED_${RUN_ID}`,
		);
		viewerUserId = undefined;
		await context.clearCookies();
		await context.clearPermissions();
	});

	test.afterEach(async ({ request }) => {
		await cleanupTestProjects(request);
		await deleteUser(request, viewerUserId);
	});

	test('The footer offers "Save for everyone" plus a "Save only for me" menu entry', async ({
		page,
	}) => {
		await signIn(page);
		await navigateToBacklog(page, projectId);
		await openSettings(page);

		await expect(saveForEveryoneButton(page)).toBeVisible();
		await expect(saveDropdownTrigger(page)).toBeVisible();
		await expect(plainSaveButton(page)).toHaveCount(0);

		await saveDropdownTrigger(page).click();
		await expect(saveOnlyForMeButton(page)).toBeVisible();
	});

	test("Both save actions are disabled until the draft differs", async ({
		page,
	}) => {
		await signIn(page);
		await navigateToBacklog(page, projectId);
		await openSettings(page);

		await expect(saveForEveryoneButton(page)).toBeDisabled();
		await saveDropdownTrigger(page).click();
		await expect(saveOnlyForMeButton(page)).toBeDisabled();
		await saveDropdownTrigger(page).click();
		await expect(saveOnlyForMeButton(page)).toBeHidden();

		await selectDropdownOption(page, "Sort by", "Title");

		await expect(saveForEveryoneButton(page)).toBeEnabled();
		await saveDropdownTrigger(page).click();
		await expect(saveOnlyForMeButton(page)).toBeEnabled();
	});

	test('"Save for everyone" publishes the settings as the team default', async ({
		page,
		request,
	}) => {
		await signIn(page);
		await navigateToBacklog(page, projectId);
		await openSettings(page);
		await selectDropdownOption(page, "Sort by", "Title");
		await saveForEveryoneAndWaitClosed(page);

		const view = await getBacklogView(request, projectId);
		expect(view.shared_config.sort_by).toBe("title");
		expect(view.is_personalized).toBe(false);

		await openSettings(page);
		await expect(personalBadge(page)).toHaveCount(0);
		await expect(settingRowButton(page, "Sort by")).toHaveText("Title");
	});

	test('"Save only for me" keeps the settings personal', async ({
		page,
		request,
	}) => {
		const before = await getBacklogView(request, projectId);

		await signIn(page);
		await navigateToBacklog(page, projectId);
		await openSettings(page);
		await selectDropdownOption(page, "Sort by", "Title");
		await saveDropdownTrigger(page).click();
		await saveOnlyForMeButton(page).click();
		await expect(saveForEveryoneButton(page)).toBeHidden();

		const view = await getBacklogView(request, projectId);
		expect(view.shared_config.sort_by).toEqual(before.shared_config.sort_by);
		expect(view.config.sort_by).toBe("title");
		expect(view.is_personalized).toBe(true);

		await openSettings(page);
		await expect(personalBadge(page)).toBeVisible();
		await expect(settingRowButton(page, "Sort by")).toHaveText("Title");
	});

	test("Another member never sees a personal-only save", async ({
		page,
		request,
	}) => {
		viewerUserId = await createViewerMember(request, projectId);
		const before = await getBacklogView(request, projectId);

		await signIn(page);
		await navigateToBacklog(page, projectId);
		await openSettings(page);
		await selectDropdownOption(page, "Sort by", "Title");
		await saveDropdownTrigger(page).click();
		await saveOnlyForMeButton(page).click();
		await expect(saveForEveryoneButton(page)).toBeHidden();

		const viewer = await viewerApiContext();
		try {
			const view = await getBacklogView(viewer, projectId);
			expect(view.config.sort_by).toEqual(before.config.sort_by);
			expect(view.config.sort_by).not.toBe("title");
			expect(view.is_personalized).toBe(false);
		} finally {
			await viewer.dispose();
		}
	});

	test('"Save for everyone" clears the publisher\'s own personal override', async ({
		page,
		request,
	}) => {
		const view = await getBacklogView(request, projectId);
		await savePersonalConfig(request, projectId, view.id, { sort_by: "title" });

		await signIn(page);
		await navigateToBacklog(page, projectId);
		await openSettings(page);
		await expect(personalBadge(page)).toBeVisible();
		await expect(settingRowButton(page, "Sort by")).toHaveText("Title");

		await selectDropdownOption(page, "Sort by", "Created");
		await saveForEveryoneAndWaitClosed(page);

		const after = await getBacklogView(request, projectId);
		expect(after.shared_config.sort_by).toBe("created");
		expect(after.is_personalized).toBe(false);

		await openSettings(page);
		await expect(personalBadge(page)).toHaveCount(0);
	});

	test("Reset reverts the draft to the team default, not the last personal save", async ({
		page,
		request,
	}) => {
		const view = await getBacklogView(request, projectId);
		await savePersonalConfig(request, projectId, view.id, { sort_by: "title" });

		await signIn(page);
		await navigateToBacklog(page, projectId);
		await openSettings(page);
		await expect(settingRowButton(page, "Sort by")).toHaveText("Title");

		await resetButton(page).click();
		await expect(settingRowButton(page, "Sort by")).toHaveText("Manual");
		await expect(resetButton(page)).toHaveCount(0);

		// Reset is local until saved: the stored personal override is untouched.
		const after = await getBacklogView(request, projectId);
		expect(after.is_personalized).toBe(true);
		expect(after.config.sort_by).toBe("title");
	});
});

// ===========================================================================
// Rule: A member without views.write can only save personal settings
// ===========================================================================

test.describe("Personal view settings — member without views.write", () => {
	let projectId: string;
	let viewerUserId: string | undefined;

	test.beforeEach(async ({ request, context }) => {
		await cleanupTestProjects(request);
		projectId = await createProject(
			request,
			`${TEST_PROJECT_PREFIX}VIEWER_${RUN_ID}`,
		);
		viewerUserId = await createViewerMember(request, projectId);
		await context.clearCookies();
		await context.clearPermissions();
	});

	test.afterEach(async ({ request }) => {
		await cleanupTestProjects(request);
		await deleteUser(request, viewerUserId);
	});

	test('The footer shows a single "Save" button instead of the split button', async ({
		page,
	}) => {
		await signIn(page, VIEWER_USERNAME, VIEWER_PASSWORD);
		await navigateToBacklog(page, projectId);
		await openSettings(page);

		await expect(plainSaveButton(page)).toBeVisible();
		await expect(saveForEveryoneButton(page)).toHaveCount(0);
		await expect(saveOnlyForMeButton(page)).toHaveCount(0);
	});

	test('"Save" is disabled until the draft differs', async ({ page }) => {
		await signIn(page, VIEWER_USERNAME, VIEWER_PASSWORD);
		await navigateToBacklog(page, projectId);
		await openSettings(page);

		await expect(plainSaveButton(page)).toBeDisabled();
		await selectDropdownOption(page, "Sort by", "Title");
		await expect(plainSaveButton(page)).toBeEnabled();
	});

	test('"Save" stores a personal override and never touches the shared view', async ({
		page,
		request,
	}) => {
		const before = await getBacklogView(request, projectId);

		await signIn(page, VIEWER_USERNAME, VIEWER_PASSWORD);
		await navigateToBacklog(page, projectId);
		await openSettings(page);
		await selectDropdownOption(page, "Sort by", "Title");
		await plainSaveButton(page).click();
		await expect(plainSaveButton(page)).toBeHidden();

		// The admin's view of the same view is unchanged.
		const adminView = await getBacklogView(request, projectId);
		expect(adminView.shared_config.sort_by).toEqual(
			before.shared_config.sort_by,
		);
		expect(adminView.config.sort_by).toEqual(before.config.sort_by);

		const viewer = await viewerApiContext();
		try {
			const view = await getBacklogView(viewer, projectId);
			expect(view.config.sort_by).toBe("title");
			expect(view.shared_config.sort_by).toEqual(before.shared_config.sort_by);
			expect(view.is_personalized).toBe(true);
		} finally {
			await viewer.dispose();
		}

		await openSettings(page);
		await expect(personalBadge(page)).toBeVisible();
	});

	test("Reset reverts to the team default", async ({ page, request }) => {
		const view = await getBacklogView(request, projectId);
		// Team default differs from the viewer's own saved override.
		const patch = await request.patch(
			`${BASE_URL}/api/v1/projects/${projectId}/views/${view.id}`,
			{
				data: { config: { sort_by: "created" } },
			},
		);
		expect(patch.ok()).toBeTruthy();

		const viewer = await viewerApiContext();
		try {
			await savePersonalConfig(viewer, projectId, view.id, {
				sort_by: "title",
			});
		} finally {
			await viewer.dispose();
		}

		await signIn(page, VIEWER_USERNAME, VIEWER_PASSWORD);
		await navigateToBacklog(page, projectId);
		await openSettings(page);
		await expect(settingRowButton(page, "Sort by")).toHaveText("Title");

		await resetButton(page).click();
		await expect(settingRowButton(page, "Sort by")).toHaveText("Created");
	});
});

// ===========================================================================
// Rule: View API access control (API-only, no browser)
// ===========================================================================

test.describe("View settings API access control", () => {
	let projectId: string;
	let viewerUserId: string | undefined;

	test.beforeEach(async ({ request }) => {
		await cleanupTestProjects(request);
		projectId = await createProject(
			request,
			`${TEST_PROJECT_PREFIX}API_${RUN_ID}`,
		);
		viewerUserId = await createViewerMember(request, projectId);
	});

	test.afterEach(async ({ request }) => {
		await cleanupTestProjects(request);
		await deleteUser(request, viewerUserId);
	});

	test("A member without views.write cannot change the shared view", async ({
		request,
	}) => {
		const view = await getBacklogView(request, projectId);
		const viewer = await viewerApiContext();
		try {
			const resp = await viewer.patch(
				`${BASE_URL}/api/v1/projects/${projectId}/views/${view.id}`,
				{
					data: { config: { sort_by: "title" } },
				},
			);
			expect(resp.status()).toBe(403);
		} finally {
			await viewer.dispose();
		}
	});

	test("A member without views.write can save and clear their own personal config", async ({
		request,
	}) => {
		const view = await getBacklogView(request, projectId);
		const viewer = await viewerApiContext();
		try {
			await savePersonalConfig(viewer, projectId, view.id, {
				sort_by: "title",
			});
			expect((await getBacklogView(viewer, projectId)).is_personalized).toBe(
				true,
			);

			const clear = await viewer.delete(
				`${BASE_URL}/api/v1/projects/${projectId}/views/${view.id}/config`,
			);
			expect(clear.ok()).toBeTruthy();
			expect((await getBacklogView(viewer, projectId)).is_personalized).toBe(
				false,
			);
		} finally {
			await viewer.dispose();
		}
	});
});
