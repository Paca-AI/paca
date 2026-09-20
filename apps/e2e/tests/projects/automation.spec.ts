// spec: features/projects/automation.feature
// seed: tests/seed.spec.ts
//
// The admin account is a super_admin, so it implicitly holds every project
// permission (workflows.read / workflows.write). Scenarios about a member who
// LACKS a permission create a second, limited user through the admin API,
// attach them to the project with a purpose-built role, and sign in as that
// user in the browser (see createLimitedMember).
//
// A11y gaps worked around here (icon-only controls with no accessible name in
// apps/web): the per-card delete button and the builder's rename (pencil)
// button. They are located structurally instead of by name.

import { ensureLoginForm } from '../helpers/e2e-api';
import {
	type APIRequestContext,
	expect,
	type Locator,
	type Page,
	request as pwRequest,
	test,
} from "@playwright/test";

const BASE_URL = process.env.E2E_BASE_URL ?? "http://localhost";
const USERNAME = process.env.E2E_USERNAME ?? "admin";
const PASSWORD = process.env.E2E_PASSWORD ?? "e2e-admin-password";
const TEST_PROJECT_PREFIX = "E2E_AUTOMATION_";
const TEST_USER_PREFIX = "E2E_AUTOMATION_MEMBER_";
const RUN_ID = Date.now().toString(36).slice(-5).toUpperCase();
const TEMP_PASSWORD = "TempPassword123!";
const MEMBER_PASSWORD = "AutomationMember123!";

let projectCounter = 0;
let memberCounter = 0;

// ─── Types ────────────────────────────────────────────────────────────────────

interface LimitedMember {
	username: string;
	password: string;
}

// ─── API Helpers ──────────────────────────────────────────────────────────────

async function authRequest(request: APIRequestContext): Promise<void> {
	const resp = await request.post(`${BASE_URL}/api/v1/auth/login`, {
		data: { username: USERNAME, password: PASSWORD, rememberMe: false },
	});
	expect(resp.ok()).toBeTruthy();
}

async function cleanupTestProjects(request: APIRequestContext): Promise<void> {
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
		} = body.data as { page: number; page_size: number; total: number };
		if (currentPage * page_size >= total) break;
		page++;
	}
	await Promise.all(
		allProjects
			.filter((p) => p.name.startsWith(TEST_PROJECT_PREFIX))
			.map((p) => request.delete(`${BASE_URL}/api/v1/projects/${p.id}`)),
	);
}

async function cleanupTestUsers(request: APIRequestContext): Promise<void> {
	const listResp = await request.get(`${BASE_URL}/api/v1/admin/users`);
	if (!listResp.ok()) return;
	const body = await listResp.json();
	const users: Array<{ id: string; username: string }> =
		body?.data?.items ?? [];
	await Promise.all(
		users
			.filter((u) => u.username.startsWith(TEST_USER_PREFIX))
			.map((u) => request.delete(`${BASE_URL}/api/v1/admin/users/${u.id}`)),
	);
}

async function createProject(
	request: APIRequestContext,
	name: string,
): Promise<string> {
	const resp = await request.post(`${BASE_URL}/api/v1/projects`, {
		data: { name },
	});
	expect(resp.ok()).toBeTruthy();
	const body = await resp.json();
	return body.data.id as string;
}

async function createAutomation(
	request: APIRequestContext,
	projectId: string,
	payload: { name: string; description?: string },
): Promise<string> {
	const resp = await request.post(
		`${BASE_URL}/api/v1/projects/${projectId}/automations`,
		{ data: payload },
	);
	expect(resp.ok()).toBeTruthy();
	const body = await resp.json();
	return body.data.id as string;
}

// The API refuses to activate an automation without at least one action
// node, so seeding an active automation needs a minimal valid action.
async function addWaitAction(
	request: APIRequestContext,
	projectId: string,
	automationId: string,
): Promise<void> {
	const resp = await request.post(
		`${BASE_URL}/api/v1/projects/${projectId}/automations/${automationId}/nodes`,
		{
			data: {
				kind: "action",
				type: "wait",
				config: { wait_minutes: 5 },
				pos_x: 320,
				pos_y: 120,
			},
		},
	);
	expect(resp.ok()).toBeTruthy();
}

async function activateAutomation(
	request: APIRequestContext,
	projectId: string,
	automationId: string,
): Promise<void> {
	await addWaitAction(request, projectId, automationId);
	const resp = await request.post(
		`${BASE_URL}/api/v1/projects/${projectId}/automations/${automationId}/activate`,
	);
	expect(resp.ok()).toBeTruthy();
}

async function addNode(
	request: APIRequestContext,
	projectId: string,
	automationId: string,
	node: { kind: "trigger" | "condition" | "action"; type: string },
): Promise<string> {
	const resp = await request.post(
		`${BASE_URL}/api/v1/projects/${projectId}/automations/${automationId}/nodes`,
		{ data: { ...node, config: {}, pos_x: 120, pos_y: 120 } },
	);
	expect(resp.ok()).toBeTruthy();
	const body = await resp.json();
	return body.data.id as string;
}

async function updateNodeConfig(
	request: APIRequestContext,
	projectId: string,
	automationId: string,
	nodeId: string,
	config: Record<string, unknown>,
): Promise<void> {
	const resp = await request.patch(
		`${BASE_URL}/api/v1/projects/${projectId}/automations/${automationId}/nodes/${nodeId}`,
		{ data: { config } },
	);
	expect(resp.ok()).toBeTruthy();
}

async function createTask(
	request: APIRequestContext,
	projectId: string,
	title: string,
): Promise<string> {
	const resp = await request.post(
		`${BASE_URL}/api/v1/projects/${projectId}/tasks`,
		{ data: { title } },
	);
	expect(resp.ok()).toBeTruthy();
	const body = await resp.json();
	return body.data.id as string;
}

async function listStatusNames(
	request: APIRequestContext,
	projectId: string,
): Promise<string[]> {
	const resp = await request.get(
		`${BASE_URL}/api/v1/projects/${projectId}/task-statuses`,
	);
	expect(resp.ok()).toBeTruthy();
	const body = await resp.json();
	return ((body?.data?.items ?? []) as Array<{ name: string }>).map(
		(s) => s.name,
	);
}

/**
 * Creates a non-admin user, clears their forced first-login password change,
 * and adds them to `projectId` with a role granting exactly `permissions`.
 */
async function createLimitedMember(
	request: APIRequestContext,
	projectId: string,
	permissions: Record<string, boolean>,
): Promise<LimitedMember> {
	memberCounter += 1;
	const username = `${TEST_USER_PREFIX}${RUN_ID}_${memberCounter}`;

	const userResp = await request.post(`${BASE_URL}/api/v1/admin/users`, {
		data: {
			username,
			full_name: "E2E Automation Member",
			role: "USER",
			password: TEMP_PASSWORD,
		},
	});
	expect(userResp.ok()).toBeTruthy();
	const userId = (await userResp.json()).data.id as string;

	const memberApi = await pwRequest.newContext();
	try {
		const loginResp = await memberApi.post(`${BASE_URL}/api/v1/auth/login`, {
			data: { username, password: TEMP_PASSWORD, rememberMe: false },
		});
		expect(loginResp.ok()).toBeTruthy();
		const changeResp = await memberApi.patch(
			`${BASE_URL}/api/v1/users/me/password`,
			{
				data: {
					current_password: TEMP_PASSWORD,
					new_password: MEMBER_PASSWORD,
				},
			},
		);
		expect(changeResp.ok()).toBeTruthy();
	} finally {
		await memberApi.dispose();
	}

	const roleResp = await request.post(
		`${BASE_URL}/api/v1/projects/${projectId}/roles`,
		{
			data: {
				role_name: `E2E Automation Role ${RUN_ID}_${memberCounter}`,
				permissions,
			},
		},
	);
	expect(roleResp.ok()).toBeTruthy();
	const roleId = (await roleResp.json()).data.id as string;

	const memberResp = await request.post(
		`${BASE_URL}/api/v1/projects/${projectId}/members`,
		{ data: { user_id: userId, project_role_id: roleId } },
	);
	expect(memberResp.ok()).toBeTruthy();

	return { username, password: MEMBER_PASSWORD };
}

// ─── UI Helpers ───────────────────────────────────────────────────────────────

const signIn = async (
	page: Page,
	username: string = USERNAME,
	password: string = PASSWORD,
) => {
	await page.goto(`${BASE_URL}/`);
	await ensureLoginForm(page);
	await page.getByRole("textbox", { name: "Username" }).fill(username);
	await page.getByRole("textbox", { name: "Password" }).fill(password);
	await page.getByRole("button", { name: "Sign in" }).click();
	await expect(
		page.getByRole("heading", { name: /Good (morning|afternoon|evening)/i }),
	).toBeVisible();
};

const openAutomationList = async (page: Page, projectId: string) => {
	await page.goto(`${BASE_URL}/projects/${projectId}/automation`);
	await expect(
		page.getByRole("heading", { name: "Automation", exact: true }),
	).toBeVisible();
};

const openBuilder = async (
	page: Page,
	projectId: string,
	automationId: string,
	name: string,
) => {
	await page.goto(
		`${BASE_URL}/projects/${projectId}/automation/${automationId}`,
	);
	await expect(page.getByRole("heading", { name })).toBeVisible();
};

// The card has no role; its nearest `rounded-xl` ancestor is the card itself
// (the icon tile inside uses `rounded-lg`).
function automationCard(page: Page, name: string): Locator {
	return page
		.getByText(name, { exact: true })
		.locator("xpath=ancestor::div[contains(@class,'rounded-xl')][1]");
}

function dependencyMapPanel(page: Page): Locator {
	return page
		.getByRole("heading", { name: "Dependency map" })
		.locator("xpath=ancestor::div[contains(@class,'rounded-xl')][1]");
}

function activeToggle(page: Page): Locator {
	return page.getByRole("switch", {
		name: "Toggle whether this automation is active",
	});
}

// The pencil is the only button next to the automation name heading.
function renameButton(page: Page, name: string): Locator {
	return page.getByRole("heading", { name }).locator("..").getByRole("button");
}

async function expectBuilderUrl(page: Page, projectId: string) {
	await expect(page).toHaveURL(
		new RegExp(`/projects/${projectId}/automation/[0-9a-f-]{36}$`),
	);
}

// ─── Tests ────────────────────────────────────────────────────────────────────

test.describe("Workflow automation", () => {
	let projectId: string;
	let projectName: string;

	test.beforeEach(async ({ request }) => {
		await authRequest(request);
		await cleanupTestProjects(request);
		await cleanupTestUsers(request);
		projectCounter += 1;
		projectName = `${TEST_PROJECT_PREFIX}${RUN_ID}_${projectCounter}`;
		projectId = await createProject(request, projectName);
	});

	test.afterEach(async ({ request }) => {
		await authRequest(request);
		await cleanupTestProjects(request);
		await cleanupTestUsers(request);
	});

	// ── Rule: Automation list page ────────────────────────────────────────────

	test.describe("Automation list page — loading, empty state, and dependency map", () => {
		test("shows loading skeletons while automations are being fetched", async ({
			page,
			request,
		}) => {
			const name = `${TEST_PROJECT_PREFIX}SLOW_${RUN_ID}`;
			await createAutomation(request, projectId, { name });
			await signIn(page);

			await page.route(
				`**/api/v1/projects/${projectId}/automations*`,
				async (route) => {
					if (route.request().method() === "GET") {
						await new Promise((resolve) => setTimeout(resolve, 1500));
					}
					await route.continue();
				},
			);

			await page.goto(`${BASE_URL}/projects/${projectId}/automation`);
			await expect(page.locator('[data-slot="skeleton"]')).toHaveCount(3);
			await expect(page.getByText("No automations yet")).toBeHidden();

			await expect(page.getByText(name, { exact: true })).toBeVisible();
			await expect(page.locator('[data-slot="skeleton"]')).toHaveCount(0);
		});

		test("a project with no automations shows an empty state", async ({
			page,
		}) => {
			await signIn(page);
			await openAutomationList(page, projectId);

			await expect(page.getByText("No automations yet")).toBeVisible();
			await expect(
				page.getByRole("button", { name: "Create your first automation" }),
			).toBeVisible();
		});

		test('the "New Automation" button is visible with workflows.write permission', async ({
			page,
		}) => {
			await signIn(page);
			await openAutomationList(page, projectId);

			await expect(
				page.getByRole("button", { name: "New Automation" }),
			).toBeVisible();
		});

		test('the "New Automation" button is hidden without workflows.write permission', async ({
			page,
			request,
		}) => {
			const member = await createLimitedMember(request, projectId, {
				"workflows.read": true,
			});
			await signIn(page, member.username, member.password);
			await openAutomationList(page, projectId);

			await expect(page.getByText("No automations yet")).toBeVisible();
			await expect(
				page.getByRole("button", { name: "New Automation" }),
			).toHaveCount(0);
			await expect(
				page.getByRole("button", { name: "Create your first automation" }),
			).toHaveCount(0);
		});

		test("the delete button on a card is hidden without workflows.write permission", async ({
			page,
			request,
		}) => {
			const name = `${TEST_PROJECT_PREFIX}READONLY_${RUN_ID}`;
			await createAutomation(request, projectId, { name });
			const member = await createLimitedMember(request, projectId, {
				"workflows.read": true,
			});
			await signIn(page, member.username, member.password);
			await openAutomationList(page, projectId);

			await expect(page.getByText(name, { exact: true })).toBeVisible();
			await expect(automationCard(page, name).getByRole("button")).toHaveCount(
				0,
			);
		});

		test("a member without workflows.read permission sees the no-permission state", async ({
			page,
			request,
		}) => {
			const member = await createLimitedMember(request, projectId, {
				"tasks.read": true,
			});
			await signIn(page, member.username, member.password);
			await openAutomationList(page, projectId);

			await expect(
				page.getByText("You don't have permission to view automations"),
			).toBeVisible();
		});

		test("an automation card shows its name, description, status badge, and last-updated time", async ({
			page,
			request,
		}) => {
			const name = `${TEST_PROJECT_PREFIX}CARD_${RUN_ID}`;
			await createAutomation(request, projectId, {
				name,
				description: "Notify on overdue tasks",
			});
			await signIn(page);
			await openAutomationList(page, projectId);

			const card = automationCard(page, name);
			await expect(card.getByText("Notify on overdue tasks")).toBeVisible();
			await expect(card.getByText("Inactive", { exact: true })).toBeVisible();
			await expect(card.getByText(/^Updated /)).toBeVisible();
		});

		test('an active automation card shows the "Active" status badge', async ({
			page,
			request,
		}) => {
			const name = `${TEST_PROJECT_PREFIX}ACTIVE_${RUN_ID}`;
			const automationId = await createAutomation(request, projectId, { name });
			await addNode(request, projectId, automationId, {
				kind: "trigger",
				type: "task_created",
			});
			await activateAutomation(request, projectId, automationId);
			await signIn(page);
			await openAutomationList(page, projectId);

			await expect(
				automationCard(page, name).getByText("Active", { exact: true }),
			).toBeVisible();
		});

		test("an automation card with no description shows a placeholder", async ({
			page,
			request,
		}) => {
			const name = `${TEST_PROJECT_PREFIX}NODESC_${RUN_ID}`;
			await createAutomation(request, projectId, { name });
			await signIn(page);
			await openAutomationList(page, projectId);

			await expect(
				automationCard(page, name).getByText("No description"),
			).toBeVisible();
		});

		test("toggling the dependency map shows watched-task counts per automation", async ({
			page,
			request,
		}) => {
			const name = `${TEST_PROJECT_PREFIX}DEP_${RUN_ID}`;
			const watchedA = await createTask(request, projectId, "Watched A");
			const watchedB = await createTask(request, projectId, "Watched B");
			const target = await createTask(request, projectId, "Dependent task");
			const automationId = await createAutomation(request, projectId, { name });
			const nodeId = await addNode(request, projectId, automationId, {
				kind: "trigger",
				type: "predecessor_done",
			});
			await updateNodeConfig(request, projectId, automationId, nodeId, {
				watched_task_ids: [watchedA, watchedB],
				target_task_id: target,
			});
			await activateAutomation(request, projectId, automationId);

			await signIn(page);
			await openAutomationList(page, projectId);
			await page.getByRole("button", { name: "Dependency map" }).click();

			const panel = dependencyMapPanel(page);
			await expect(panel.getByText(name, { exact: true })).toBeVisible();
			await expect(panel.getByText("Waiting on 2 task(s)")).toBeVisible();
		});

		test("the dependency map shows an empty state when no automation watches any task", async ({
			page,
		}) => {
			await signIn(page);
			await openAutomationList(page, projectId);
			await page.getByRole("button", { name: "Dependency map" }).click();

			await expect(
				page.getByText("No predecessor dependencies configured yet"),
			).toBeVisible();
		});

		test("clicking the dependency map button again hides the panel", async ({
			page,
		}) => {
			await signIn(page);
			await openAutomationList(page, projectId);

			const button = page.getByRole("button", { name: "Dependency map" });
			await button.click();
			await expect(
				page.getByRole("heading", { name: "Dependency map" }),
			).toBeVisible();
			await button.click();
			await expect(
				page.getByRole("heading", { name: "Dependency map" }),
			).toHaveCount(0);
		});

		test("clicking an automation card navigates into its graph builder", async ({
			page,
			request,
		}) => {
			const name = `${TEST_PROJECT_PREFIX}OPEN_${RUN_ID}`;
			const automationId = await createAutomation(request, projectId, { name });
			await signIn(page);
			await openAutomationList(page, projectId);

			await page.getByText(name, { exact: true }).click();

			await expect(page).toHaveURL(
				`${BASE_URL}/projects/${projectId}/automation/${automationId}`,
			);
			await expect(page.getByRole("heading", { name })).toBeVisible();
		});
	});

	// ── Rule: Creating and deleting an automation ─────────────────────────────

	test.describe("Creating and deleting an automation", () => {
		test("the create dialog requires a name before it can be submitted", async ({
			page,
		}) => {
			await signIn(page);
			await openAutomationList(page, projectId);
			await page.getByRole("button", { name: "New Automation" }).click();

			const dialog = page.getByRole("dialog", { name: "New automation" });
			await expect(dialog).toBeVisible();
			const create = dialog.getByRole("button", {
				name: "Create",
				exact: true,
			});
			await expect(create).toBeDisabled();

			await dialog
				.getByRole("textbox", { name: "Name" })
				.fill(`${TEST_PROJECT_PREFIX}NEW_${RUN_ID}`);
			await expect(create).toBeEnabled();
		});

		test("creating an automation navigates straight into its empty, inactive graph builder", async ({
			page,
		}) => {
			const name = `${TEST_PROJECT_PREFIX}CREATED_${RUN_ID}`;
			await signIn(page);
			await openAutomationList(page, projectId);
			await page.getByRole("button", { name: "New Automation" }).click();

			const dialog = page.getByRole("dialog", { name: "New automation" });
			await dialog.getByRole("textbox", { name: "Name" }).fill(name);
			await dialog
				.getByRole("textbox", { name: "Description" })
				.fill("Runs when a bug is filed");
			await dialog.getByRole("button", { name: "Create", exact: true }).click();

			await expect(dialog).toBeHidden();
			await expectBuilderUrl(page, projectId);
			await expect(page.getByRole("heading", { name })).toBeVisible();
			await expect(page.getByText("Inactive", { exact: true })).toBeVisible();
			await expect(activeToggle(page)).not.toBeChecked();
			// The canvas is an @xyflow surface with no roles of its own.
			await expect(page.locator(".react-flow__node")).toHaveCount(0);
		});

		test("cancelling the create dialog discards the in-progress automation", async ({
			page,
		}) => {
			const name = `${TEST_PROJECT_PREFIX}CANCELLED_${RUN_ID}`;
			await signIn(page);
			await openAutomationList(page, projectId);
			await page.getByRole("button", { name: "New Automation" }).click();

			const dialog = page.getByRole("dialog", { name: "New automation" });
			await dialog.getByRole("textbox", { name: "Name" }).fill(name);
			await dialog.getByRole("button", { name: "Cancel" }).click();

			await expect(dialog).toBeHidden();
			await expect(page.getByText(name, { exact: true })).toHaveCount(0);
			await expect(page.getByText("No automations yet")).toBeVisible();
		});

		test("deleting an automation asks for confirmation before removing it", async ({
			page,
			request,
		}) => {
			const name = `${TEST_PROJECT_PREFIX}TODELETE_${RUN_ID}`;
			await createAutomation(request, projectId, { name });
			await signIn(page);
			await openAutomationList(page, projectId);

			await automationCard(page, name).getByRole("button").click();

			const dialog = page.getByRole("dialog", { name: "Delete automation" });
			await expect(dialog).toBeVisible();
			await expect(dialog.getByText(`"${name}"`)).toBeVisible();
			await dialog.getByRole("button", { name: "Delete", exact: true }).click();

			await expect(dialog).toBeHidden();
			await expect(page.getByText(name, { exact: true })).toHaveCount(0);
		});

		test("cancelling the delete confirmation keeps the automation", async ({
			page,
			request,
		}) => {
			const name = `${TEST_PROJECT_PREFIX}KEEP_${RUN_ID}`;
			await createAutomation(request, projectId, { name });
			await signIn(page);
			await openAutomationList(page, projectId);

			await automationCard(page, name).getByRole("button").click();
			const dialog = page.getByRole("dialog", { name: "Delete automation" });
			await dialog.getByRole("button", { name: "Cancel" }).click();

			await expect(dialog).toBeHidden();
			await expect(page.getByText(name, { exact: true })).toBeVisible();
		});
	});

	// ── Rule: Toggling an automation's active/inactive status ─────────────────

	test.describe("Toggling an automation's active/inactive status", () => {
		test("an inactive automation shows the toggle switched off", async ({
			page,
			request,
		}) => {
			const name = `${TEST_PROJECT_PREFIX}OFF_${RUN_ID}`;
			const automationId = await createAutomation(request, projectId, { name });
			await signIn(page);
			await openBuilder(page, projectId, automationId, name);

			await expect(page.getByText("Inactive", { exact: true })).toBeVisible();
			await expect(activeToggle(page)).not.toBeChecked();
		});

		test("turning the toggle on activates the automation", async ({
			page,
			request,
		}) => {
			const name = `${TEST_PROJECT_PREFIX}ACTIVATE_${RUN_ID}`;
			const automationId = await createAutomation(request, projectId, { name });
			await addNode(request, projectId, automationId, {
				kind: "trigger",
				type: "task_created",
			});
			await addWaitAction(request, projectId, automationId);
			await signIn(page);
			await openBuilder(page, projectId, automationId, name);

			await activeToggle(page).click();

			await expect(activeToggle(page)).toBeChecked();
			await expect(page.getByText("Active", { exact: true })).toBeVisible();
		});

		test("turning the toggle off deactivates the automation", async ({
			page,
			request,
		}) => {
			const name = `${TEST_PROJECT_PREFIX}DEACTIVATE_${RUN_ID}`;
			const automationId = await createAutomation(request, projectId, { name });
			await addNode(request, projectId, automationId, {
				kind: "trigger",
				type: "task_created",
			});
			await activateAutomation(request, projectId, automationId);
			await signIn(page);
			await openBuilder(page, projectId, automationId, name);
			await expect(activeToggle(page)).toBeChecked();

			await activeToggle(page).click();

			await expect(activeToggle(page)).not.toBeChecked();
			await expect(page.getByText("Inactive", { exact: true })).toBeVisible();
		});

		test("renaming an automation updates its name in the header", async ({
			page,
			request,
		}) => {
			const oldName = `${TEST_PROJECT_PREFIX}OLDNAME_${RUN_ID}`;
			const newName = `${TEST_PROJECT_PREFIX}NEWNAME_${RUN_ID}`;
			const automationId = await createAutomation(request, projectId, {
				name: oldName,
			});
			await signIn(page);
			await openBuilder(page, projectId, automationId, oldName);

			await renameButton(page, oldName).click();
			// The rename field is auto-focused and has no accessible name.
			const input = page.locator("input:focus");
			await input.fill(newName);
			// Save (check) icon is the first of the two icon buttons beside the field.
			await input.locator("..").getByRole("button").first().click();

			await expect(page.getByRole("heading", { name: newName })).toBeVisible();
			await expect(page.getByRole("heading", { name: oldName })).toHaveCount(0);
		});

		test("the rename icon and the toggle are disabled without workflows.write permission", async ({
			page,
			request,
		}) => {
			const name = `${TEST_PROJECT_PREFIX}LOCKED_${RUN_ID}`;
			const automationId = await createAutomation(request, projectId, { name });
			const member = await createLimitedMember(request, projectId, {
				"workflows.read": true,
			});
			await signIn(page, member.username, member.password);
			await openBuilder(page, projectId, automationId, name);

			await expect(renameButton(page, name)).toHaveCount(0);
			await expect(activeToggle(page)).toBeDisabled();
		});
	});

	// ── Rule: Building the automation graph ───────────────────────────────────

	test.describe("Building the automation graph — palette, nodes, and run history", () => {
		let automationId: string;
		let automationName: string;

		test.beforeEach(async ({ request }) => {
			automationName = `${TEST_PROJECT_PREFIX}GRAPH_${RUN_ID}`;
			automationId = await createAutomation(request, projectId, {
				name: automationName,
			});
		});

		test("the Graph tab is shown by default with a node palette offering all three node kinds", async ({
			page,
		}) => {
			await signIn(page);
			await openBuilder(page, projectId, automationId, automationName);

			await expect(page.getByRole("button", { name: "Graph" })).toBeVisible();
			await expect(
				page.getByRole("button", { name: "Add Trigger" }),
			).toBeVisible();
			await expect(
				page.getByRole("button", { name: "Add Condition" }),
			).toBeVisible();
			await expect(
				page.getByRole("button", { name: "Add Action" }),
			).toBeVisible();
		});

		test("adding a trigger node from the palette places it on the canvas and opens its config panel", async ({
			page,
		}) => {
			await signIn(page);
			await openBuilder(page, projectId, automationId, automationName);

			await page.getByRole("button", { name: "Add Trigger" }).click();
			await page.getByRole("menuitem", { name: "Status changed" }).click();

			// Canvas node label carries a title attribute; the panel title does not.
			await expect(
				page.getByTitle("Status changed", { exact: true }),
			).toBeVisible();
			await expect(
				page.getByRole("button", { name: "Remove node" }),
			).toBeVisible();
			await expect(
				page.getByText("Trigger", { exact: true }).first(),
			).toBeVisible();
		});

		test("saving a trigger node's configuration updates its description on the canvas", async ({
			page,
			request,
		}) => {
			await addNode(request, projectId, automationId, {
				kind: "trigger",
				type: "status_changed",
			});
			const statusNames = await listStatusNames(request, projectId);
			expect(statusNames.length).toBeGreaterThan(0);
			const statusName = statusNames[0];

			await signIn(page);
			await openBuilder(page, projectId, automationId, automationName);

			await page.getByTitle("Status changed", { exact: true }).click();
			await page
				.locator('[data-slot="select-trigger"]')
				.filter({ hasText: "Any status" })
				.click();
			await page.getByRole("option", { name: statusName, exact: true }).click();
			await page.getByRole("button", { name: "Save", exact: true }).click();

			await expect(page.getByTitle(statusName, { exact: true })).toBeVisible();
		});

		test("removing a node asks for confirmation before deleting it", async ({
			page,
			request,
		}) => {
			await addNode(request, projectId, automationId, {
				kind: "trigger",
				type: "status_changed",
			});
			await signIn(page);
			await openBuilder(page, projectId, automationId, automationName);

			await page.getByTitle("Status changed", { exact: true }).click();
			await page.getByRole("button", { name: "Remove node" }).click();

			const dialog = page.getByRole("dialog", { name: "Remove node" });
			await expect(dialog).toBeVisible();
			await dialog.getByRole("button", { name: "Remove", exact: true }).click();

			await expect(dialog).toBeHidden();
			await expect(
				page.getByTitle("Status changed", { exact: true }),
			).toHaveCount(0);
		});

		test("the node palette is hidden for a member without workflows.write permission", async ({
			page,
			request,
		}) => {
			const member = await createLimitedMember(request, projectId, {
				"workflows.read": true,
			});
			await signIn(page, member.username, member.password);
			await openBuilder(page, projectId, automationId, automationName);

			await expect(page.getByRole("button", { name: "Graph" })).toBeVisible();
			await expect(
				page.getByRole("button", { name: "Add Trigger" }),
			).toHaveCount(0);
			await expect(
				page.getByRole("button", { name: "Add Condition" }),
			).toHaveCount(0);
			await expect(
				page.getByRole("button", { name: "Add Action" }),
			).toHaveCount(0);
		});

		test("switching to the Run history tab shows the run history panel", async ({
			page,
		}) => {
			await signIn(page);
			await openBuilder(page, projectId, automationId, automationName);

			await page.getByRole("button", { name: "Run history" }).click();

			// The node palette only renders on the Graph tab.
			await expect(
				page.getByRole("button", { name: "Add Trigger" }),
			).toHaveCount(0);
			await expect(page.getByText("No runs yet")).toBeVisible();
		});

		test("a run history panel with no runs shows an empty state", async ({
			page,
		}) => {
			await signIn(page);
			await openBuilder(page, projectId, automationId, automationName);

			await page.getByRole("button", { name: "Run history" }).click();

			await expect(page.getByText("No runs yet")).toBeVisible();
		});
	});
});
