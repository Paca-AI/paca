// spec: features/projects/roles.feature
// seed: tests/seed.spec.ts
//
// The admin account is a super_admin, so it can manage roles in every
// project. Scenarios about a member who LACKS a permission create a second,
// limited user through the admin API (createUserWithProjectPermissions), and
// sign in as that user in the browser.
//
// A role is an IAM policy. The role form edits it either as a switch per
// permission (Simple, each switch named by its label) or as the policy JSON
// (Advanced); a role holds actions such as "tasks:read" or "tasks:*".

import {
	type APIRequestContext,
	expect,
	type Locator,
	type Page,
	test,
} from "@playwright/test";
import {
	allowPolicy,
	API_URL,
	BASE_URL,
	cleanupProjectsByPrefix,
	cleanupUsersByPrefix,
	createProject,
	createProjectRole,
	createUserWithProjectPermissions,
	newRunId,
	RESTRICTED_PASSWORD,
	signIn,
} from "../helpers/e2e-api";

const TEST_PREFIX = "E2E_ROLES_";
const RUN_ID = newRunId();

let counter = 0;
function uniqueName(label: string): string {
	counter += 1;
	return `${TEST_PREFIX}${label}_${RUN_ID}${counter}`;
}

// ─── API helpers ─────────────────────────────────────────────────────────────

interface ApiRole {
	id: string;
	name: string;
	description: string;
	project_id: string | null;
	policy: {
		statements: Array<{
			effect: string;
			actions: string[];
			resources: string[];
		}>;
	};
}

async function authAndCleanup(request: APIRequestContext): Promise<void> {
	// cleanupProjectsByPrefix logs in as admin first.
	await cleanupProjectsByPrefix(request, TEST_PREFIX);
	await cleanupUsersByPrefix(request, TEST_PREFIX);
}

async function listRoles(
	request: APIRequestContext,
	projectId: string,
): Promise<ApiRole[]> {
	const response = await request.get(`${API_URL}/projects/${projectId}/roles`);
	expect(response.ok()).toBeTruthy();
	return (await response.json()).data ?? [];
}

/** Creates a project role that allows exactly `actions`, as the Simple view would. */
async function createRole(
	request: APIRequestContext,
	projectId: string,
	roleName: string,
	actions: string[],
	description = "",
): Promise<void> {
	await createProjectRole(
		request,
		projectId,
		roleName,
		allowPolicy(actions, { projectId }),
		description,
	);
}

/** The actions the project's role called `roleName` allows, as stored. */
async function storedActions(
	request: APIRequestContext,
	projectId: string,
	roleName: string,
): Promise<string[]> {
	const role = (await listRoles(request, projectId)).find(
		(r) => r.name === roleName && r.project_id === projectId,
	);
	expect(role, `role ${roleName} should exist`).toBeTruthy();
	return (role?.policy.statements ?? [])
		.filter((statement) => statement.effect === "Allow")
		.flatMap((statement) => statement.actions);
}

// ─── UI helpers ──────────────────────────────────────────────────────────────

async function openRolesSection(page: Page, projectId: string): Promise<void> {
	await page.goto(`${BASE_URL}/projects/${projectId}/settings`);
	await page.getByRole("button", { name: "Roles", exact: true }).click();
}

function rolesTable(page: Page): Locator {
	return page.getByRole("table");
}

function roleCell(page: Page, roleName: string): Locator {
	return rolesTable(page).getByRole("cell", { name: roleName, exact: true });
}

function roleRow(page: Page, roleName: string): Locator {
	// `has` is resolved relative to each row, so it must not repeat the table.
	return rolesTable(page)
		.getByRole("row")
		.filter({ has: page.getByRole("cell", { name: roleName, exact: true }) });
}

async function expectRoleListed(page: Page, roleName: string): Promise<void> {
	await expect(roleCell(page, roleName)).toBeVisible();
}

async function expectRoleNotListed(
	page: Page,
	roleName: string,
): Promise<void> {
	await expect(rolesTable(page)).toBeVisible();
	await expect(roleCell(page, roleName)).toHaveCount(0);
}

function permissionSwitch(dialog: Locator, label: string): Locator {
	return dialog.getByRole("switch", { name: label, exact: true });
}

async function turnOn(dialog: Locator, ...labels: string[]): Promise<void> {
	for (const label of labels) {
		await permissionSwitch(dialog, label).click();
		await expect(permissionSwitch(dialog, label)).toBeChecked();
	}
}

async function turnOff(dialog: Locator, ...labels: string[]): Promise<void> {
	for (const label of labels) {
		await permissionSwitch(dialog, label).click();
		await expect(permissionSwitch(dialog, label)).not.toBeChecked();
	}
}

async function openEditDialog(page: Page, roleName: string): Promise<Locator> {
	await roleRow(page, roleName)
		.getByRole("button", { name: "Edit role" })
		.click();
	const dialog = page.getByRole("dialog", { name: "Edit Role" });
	await expect(dialog).toBeVisible();
	return dialog;
}

async function openDeleteDialog(
	page: Page,
	roleName: string,
): Promise<Locator> {
	await roleRow(page, roleName)
		.getByRole("button", { name: "Delete role" })
		.click();
	const dialog = page.getByRole("dialog", { name: "Delete role" });
	await expect(dialog).toBeVisible();
	return dialog;
}

async function openCreateDialog(page: Page): Promise<Locator> {
	await page.getByRole("button", { name: "New role" }).click();
	const dialog = page.getByRole("dialog", { name: "New Role" });
	await expect(dialog).toBeVisible();
	return dialog;
}

// ─── Roles list ──────────────────────────────────────────────────────────────

test.describe("Roles list", () => {
	let projectId: string;

	test.beforeEach(async ({ page, request }) => {
		await authAndCleanup(request);
		projectId = await createProject(request, uniqueName("LIST"));
		await signIn(page);
	});

	test.afterEach(async ({ request }) => {
		await authAndCleanup(request);
	});

	test("The Roles section lists the default project roles", async ({
		page,
	}) => {
		await openRolesSection(page, projectId);

		await expect(
			page.getByRole("heading", { name: "Project Roles" }),
		).toBeVisible();
		const table = rolesTable(page);
		await expect(
			table.getByRole("columnheader", { name: "Name" }),
		).toBeVisible();
		await expect(
			table.getByRole("columnheader", { name: "Description" }),
		).toBeVisible();
		await expect(
			table.getByRole("columnheader", { name: "Created" }),
		).toBeVisible();
		await expect(
			table.getByRole("columnheader", { name: "Permissions" }),
		).toHaveCount(0);
		await expectRoleListed(page, "Admin");
		await expectRoleListed(page, "Editor");
		await expectRoleListed(page, "Viewer");
	});

	test("The seeded Admin role is a full-access role", async ({
		page,
		request,
	}) => {
		await openRolesSection(page, projectId);
		await expectRoleListed(page, "Admin");

		const admin = (await listRoles(request, projectId)).find(
			(r) => r.name === "Admin",
		);
		expect(
			admin?.policy.statements.flatMap((statement) => statement.actions),
		).toContain("*");
	});

	test("A role without a description shows a placeholder", async ({
		page,
		request,
	}) => {
		const roleName = uniqueName("EMPTY");
		await createRole(request, projectId, roleName, []);
		await openRolesSection(page, projectId);

		await expect(
			roleRow(page, roleName).getByText("No description"),
		).toBeVisible();
	});

	test("The description of a role is listed instead of its permissions", async ({
		page,
		request,
	}) => {
		const roleName = uniqueName("DESCRIBED");
		await createRole(
			request,
			projectId,
			roleName,
			["tasks:read", "docs:read"],
			"Reads tasks and documents",
		);
		await openRolesSection(page, projectId);

		const row = roleRow(page, roleName);
		await expect(row.getByText("Reads tasks and documents")).toBeVisible();
		await expect(row.getByText("No description")).toHaveCount(0);
		await expect(row.getByText("tasks:read", { exact: true })).toHaveCount(0);
	});
});

// ─── Creating a role ─────────────────────────────────────────────────────────

test.describe("Creating a role", () => {
	let projectId: string;

	test.beforeEach(async ({ page, request }) => {
		await authAndCleanup(request);
		projectId = await createProject(request, uniqueName("CREATE"));
		await signIn(page);
		await openRolesSection(page, projectId);
	});

	test.afterEach(async ({ request }) => {
		await authAndCleanup(request);
	});

	test("The New role button opens an empty role form", async ({ page }) => {
		const dialog = await openCreateDialog(page);

		await expect(
			dialog.getByRole("textbox", { name: "Role Name" }),
		).toHaveValue("");
		// Submitting is never blocked up front: an empty name is answered inline.
		await dialog.getByRole("button", { name: "Create role" }).click();
		await expect(
			dialog.getByText("Enter a role name of up to 100 characters."),
		).toBeVisible();
		await expect(dialog).toBeVisible();
	});

	test("The New role form starts in the Simple view with every switch off", async ({
		page,
	}) => {
		const dialog = await openCreateDialog(page);

		await expect(page.getByRole("tab", { name: "Simple" })).toHaveAttribute(
			"aria-selected",
			"true",
		);
		await expect(
			page.getByRole("tab", { name: "Advanced (JSON)" }),
		).toHaveAttribute("aria-selected", "false");
		await expect(dialog.getByRole("switch", { checked: true })).toHaveCount(0);
		await expect(
			dialog.getByRole("button", { name: "Create role" }),
		).toBeEnabled();
	});

	test("Enabling permission switches updates the enabled count", async ({
		page,
	}) => {
		const dialog = await openCreateDialog(page);

		await turnOn(dialog, "View Tasks", "View Documents");

		await expect(dialog.getByText("2 enabled", { exact: true })).toBeVisible();
	});

	test("Creating a role with individual permissions adds it to the list", async ({
		page,
		request,
	}) => {
		const roleName = uniqueName("READER");
		const dialog = await openCreateDialog(page);
		await dialog.getByRole("textbox", { name: "Role Name" }).fill(roleName);
		await turnOn(dialog, "View Tasks", "View Documents");

		await dialog.getByRole("button", { name: "Create role" }).click();

		await expect(dialog).not.toBeVisible();
		await expectRoleListed(page, roleName);
		expect(
			(await storedActions(request, projectId, roleName)).sort(),
		).toEqual(["docs:read", "tasks:read"]);
	});

	test("A role is created with a description that the list shows", async ({
		page,
		request,
	}) => {
		const roleName = uniqueName("DESCRIBED");
		const description = "Can read tasks, nothing else";
		const dialog = await openCreateDialog(page);
		await dialog.getByRole("textbox", { name: "Role Name" }).fill(roleName);
		await dialog.getByRole("textbox", { name: "Description" }).fill(description);
		await turnOn(dialog, "View Tasks");

		await dialog.getByRole("button", { name: "Create role" }).click();

		await expect(dialog).not.toBeVisible();
		await expect(roleRow(page, roleName).getByText(description)).toBeVisible();
		const stored = (await listRoles(request, projectId)).find(
			(r) => r.name === roleName,
		);
		expect(stored?.description).toBe(description);
	});

	test("The permission list can be searched and a group switched on at once", async ({
		page,
	}) => {
		const dialog = await openCreateDialog(page);

		await dialog.getByRole("searchbox", { name: "Search permissions" }).fill(
			"View Tasks",
		);
		await expect(permissionSwitch(dialog, "View Tasks")).toBeVisible();
		await expect(permissionSwitch(dialog, "Edit Tasks")).toHaveCount(0);

		await dialog.getByRole("searchbox", { name: "Search permissions" }).fill(
			"no such permission",
		);
		await expect(dialog.getByRole("switch")).toHaveCount(0);
		await dialog.getByRole("button", { name: "Clear search" }).click();

		await dialog.getByRole("button", { name: "Select all in Tasks" }).click();
		await expect(permissionSwitch(dialog, "View Tasks")).toBeChecked();
		await expect(permissionSwitch(dialog, "Edit Tasks")).toBeChecked();
		await dialog.getByRole("button", { name: "Clear all in Tasks" }).click();
		await expect(permissionSwitch(dialog, "View Tasks")).not.toBeChecked();
	});

	test("Enabling every permission of an area is stored as the area wildcard", async ({
		page,
		request,
	}) => {
		const roleName = uniqueName("TASKS");
		const dialog = await openCreateDialog(page);
		await dialog.getByRole("textbox", { name: "Role Name" }).fill(roleName);
		await turnOn(dialog, "View Tasks", "Edit Tasks");

		await dialog.getByRole("button", { name: "Create role" }).click();

		await expect(dialog).not.toBeVisible();
		await expectRoleListed(page, roleName);
		expect(await storedActions(request, projectId, roleName)).toEqual([
			"tasks:*",
		]);
	});

	test("A role can be written as a policy in the Advanced view, Deny included", async ({
		page,
		request,
	}) => {
		const roleName = uniqueName("POLICY");
		const dialog = await openCreateDialog(page);
		await dialog.getByRole("textbox", { name: "Role Name" }).fill(roleName);
		await page.getByRole("tab", { name: "Advanced (JSON)" }).click();
		await dialog.getByRole("textbox", { name: "Policy (JSON)" }).fill(
			JSON.stringify({
				version: "2026-10-01",
				statements: [
					{
						effect: "Allow",
						actions: ["tasks:read", "tasks:write"],
						resources: [`project/${projectId}/*`],
					},
					{
						effect: "Deny",
						actions: ["tasks:write"],
						resources: [`project/${projectId}/*`],
					},
				],
			}),
		);

		// The server checks the policy as it is typed; Create waits for that.
		await dialog.getByRole("button", { name: "Create role" }).click();

		await expect(dialog).not.toBeVisible();
		// The Deny is kept in the policy next to the Allow.
		await expectRoleListed(page, roleName);
		const stored = (await listRoles(request, projectId)).find(
			(r) => r.name === roleName,
		);
		expect(stored?.policy.statements.map((st) => st.effect)).toEqual([
			"Allow",
			"Deny",
		]);
	});

	test("A project role cannot name resources outside its project", async ({
		request,
	}) => {
		const rejected = await request.post(
			`${API_URL}/projects/${projectId}/roles`,
			{
				data: {
					name: uniqueName("OUTSIDE"),
					description: "",
					policy: {
						version: "2026-10-01",
						statements: [
							{
								effect: "Allow",
								actions: ["tasks:read"],
								resources: ["project/*"],
							},
						],
					},
				},
			},
		);
		expect(rejected.status()).toBe(422);
		const body = JSON.stringify(await rejected.json());
		expect(body).toContain("ROLE_POLICY_INVALID");
		expect(body).toContain("statements[0].resources[0]");

		// A workspace role may name specific projects.
		const workspaceName = uniqueName("WS");
		const created = await request.post(`${API_URL}/admin/roles`, {
			data: {
				name: workspaceName,
				description: "",
				policy: {
					version: "2026-10-01",
					statements: [
						{
							effect: "Allow",
							actions: ["tasks:read"],
							resources: [`project/${projectId}/*`],
						},
					],
				},
			},
		});
		expect(created.status()).toBe(201);
		const role = (await created.json()).data as { id: string };
		await request.delete(`${API_URL}/admin/roles/${role.id}`);
	});

	test("A role name that already exists is rejected", async ({ page }) => {
		const dialog = await openCreateDialog(page);
		await dialog.getByRole("textbox", { name: "Role Name" }).fill("Admin");

		await dialog.getByRole("button", { name: "Create role" }).click();

		await expect(
			dialog.getByText("A role with this name already exists."),
		).toBeVisible();
		await expect(dialog).toBeVisible();
	});

	test("Cancelling the form discards the new role", async ({ page }) => {
		const roleName = uniqueName("DISCARDED");
		const dialog = await openCreateDialog(page);
		await dialog.getByRole("textbox", { name: "Role Name" }).fill(roleName);

		await dialog.getByRole("button", { name: "Cancel" }).click();

		await expect(dialog).not.toBeVisible();
		await expectRoleNotListed(page, roleName);
	});
});

// ─── Editing a role ──────────────────────────────────────────────────────────

test.describe("Editing a role", () => {
	let projectId: string;
	let roleName: string;

	test.beforeEach(async ({ page, request }) => {
		await authAndCleanup(request);
		projectId = await createProject(request, uniqueName("EDIT"));
		roleName = uniqueName("EDITABLE");
		await createRole(request, projectId, roleName, ["tasks:read"]);
		await signIn(page);
		await openRolesSection(page, projectId);
	});

	test.afterEach(async ({ request }) => {
		await authAndCleanup(request);
	});

	test("The edit form is pre-filled with the role's name and permissions", async ({
		page,
	}) => {
		const dialog = await openEditDialog(page, roleName);

		await expect(
			dialog.getByRole("textbox", { name: "Role Name" }),
		).toHaveValue(roleName);
		await expect(permissionSwitch(dialog, "View Tasks")).toBeChecked();
		await expect(permissionSwitch(dialog, "Edit Tasks")).not.toBeChecked();
		await expect(dialog.getByText("1 enabled", { exact: true })).toBeVisible();
	});

	test("Renaming a role and adding a permission updates the list", async ({
		page,
		request,
	}) => {
		const renamed = uniqueName("RENAMED");
		const dialog = await openEditDialog(page, roleName);
		await dialog.getByRole("textbox", { name: "Role Name" }).fill(renamed);
		await turnOn(dialog, "View Documents");

		await dialog.getByRole("button", { name: "Save changes" }).click();

		await expect(dialog).not.toBeVisible();
		await expectRoleListed(page, renamed);
		await expectRoleNotListed(page, roleName);
		expect(
			(await storedActions(request, projectId, renamed)).sort(),
		).toEqual(["docs:read", "tasks:read"]);
	});

	test("The description can be edited and cleared", async ({
		page,
		request,
	}) => {
		let dialog = await openEditDialog(page, roleName);
		const box = dialog.getByRole("textbox", { name: "Description" });
		await expect(box).toHaveValue("");
		await box.fill("Edited description");
		await dialog.getByRole("button", { name: "Save changes" }).click();
		await expect(dialog).not.toBeVisible();
		await expect(
			roleRow(page, roleName).getByText("Edited description"),
		).toBeVisible();

		dialog = await openEditDialog(page, roleName);
		await expect(
			dialog.getByRole("textbox", { name: "Description" }),
		).toHaveValue("Edited description");
		await dialog.getByRole("textbox", { name: "Description" }).fill("");
		await dialog.getByRole("button", { name: "Save changes" }).click();
		await expect(dialog).not.toBeVisible();
		await expect(
			roleRow(page, roleName).getByText("No description"),
		).toBeVisible();
		const stored = (await listRoles(request, projectId)).find(
			(r) => r.name === roleName,
		);
		expect(stored?.description).toBe("");
	});

	test("Turning a permission off removes it from the role", async ({
		page,
		request,
	}) => {
		const dialog = await openEditDialog(page, roleName);
		await turnOff(dialog, "View Tasks");

		await dialog.getByRole("button", { name: "Save changes" }).click();

		await expect(dialog).not.toBeVisible();
		expect(await storedActions(request, projectId, roleName)).toEqual([]);
	});

	test("Cancelling the edit form leaves the role unchanged", async ({
		page,
	}) => {
		const dialog = await openEditDialog(page, roleName);
		await dialog
			.getByRole("textbox", { name: "Role Name" })
			.fill(uniqueName("NOT_SAVED"));

		await dialog.getByRole("button", { name: "Cancel" }).click();

		await expect(dialog).not.toBeVisible();
		await expectRoleListed(page, roleName);
	});
});

// ─── Full access roles ───────────────────────────────────────────────────────

test.describe("Full access roles", () => {
	let projectId: string;
	let fullRoleName: string;

	test.beforeEach(async ({ page, request }) => {
		await authAndCleanup(request);
		projectId = await createProject(request, uniqueName("FULL"));
		fullRoleName = uniqueName("EVERYTHING");
		await createRole(request, projectId, fullRoleName, ["*"]);
		await signIn(page);
		await openRolesSection(page, projectId);
	});

	test.afterEach(async ({ request }) => {
		await authAndCleanup(request);
	});

	test("A role stored as the wildcard shows the Full access badge", async ({
		page,
	}) => {
		const dialog = await openEditDialog(page, fullRoleName);

		await expect(
			dialog.getByText("Full access", { exact: true }),
		).toBeVisible();
		await expect(
			dialog.getByText(
				/This role includes every permission, including ones added in the future/,
			),
		).toBeVisible();
		await expect(dialog.getByRole("switch").first()).toBeChecked();
		await expect(dialog.getByRole("switch", { checked: false })).toHaveCount(0);
	});

	test("The built-in Admin role is full access and can be edited but not deleted", async ({
		page,
		request,
	}) => {
		const adminRow = roleRow(page, "Admin");

		const admin = (await listRoles(request, projectId)).find(
			(r) => r.name === "Admin",
		);
		expect(
			admin?.policy.statements.flatMap((statement) => statement.actions),
		).toContain("*");
		await expect(
			adminRow.getByRole("button", { name: "Edit role" }),
		).toBeVisible();
		await expect(
			adminRow.getByRole("button", { name: "Delete role" }),
		).toHaveCount(0);
	});

	test("A role with an enumerated permission set does not show the Full access badge", async ({
		page,
		request,
	}) => {
		const partial = uniqueName("PARTIAL");
		await createRole(request, projectId, partial, ["tasks:read"]);
		await openRolesSection(page, projectId);

		const dialog = await openEditDialog(page, partial);

		await expect(dialog.getByText("Full access", { exact: true })).toHaveCount(
			0,
		);
	});

	test("Changing any switch converts a Full access role to a fixed permission set", async ({
		page,
		request,
	}) => {
		const dialog = await openEditDialog(page, fullRoleName);
		await turnOff(dialog, "Manage Roles");

		await expect(dialog.getByText("Full access", { exact: true })).toHaveCount(
			0,
		);
		await expect(dialog.getByText(/^\d+ enabled$/)).toBeVisible();

		await dialog.getByRole("button", { name: "Save changes" }).click();

		await expect(dialog).not.toBeVisible();
		await expect(
			roleRow(page, fullRoleName).getByText("*", { exact: true }),
		).toHaveCount(0);
		const stored = await storedActions(request, projectId, fullRoleName);
		expect(stored).not.toContain("*");
		expect(stored).not.toContain("roles:write");
		expect(stored).toContain("roles:read");
	});

	test("Saving an untouched Full access role keeps the wildcard", async ({
		page,
		request,
	}) => {
		const dialog = await openEditDialog(page, fullRoleName);

		await dialog.getByRole("button", { name: "Save changes" }).click();

		await expect(dialog).not.toBeVisible();
		expect(await storedActions(request, projectId, fullRoleName)).toEqual([
			"*",
		]);
	});
});

// ─── Deleting a role ─────────────────────────────────────────────────────────

test.describe("Deleting a role", () => {
	let projectId: string;
	let roleName: string;

	test.beforeEach(async ({ page, request }) => {
		await authAndCleanup(request);
		projectId = await createProject(request, uniqueName("DELETE"));
		roleName = uniqueName("DISPOSABLE");
		await createRole(request, projectId, roleName, ["tasks:read"]);
		await signIn(page);
		await openRolesSection(page, projectId);
	});

	test.afterEach(async ({ request }) => {
		await authAndCleanup(request);
	});

	test("Deleting a role asks for confirmation naming the role", async ({
		page,
	}) => {
		const dialog = await openDeleteDialog(page, roleName);

		await expect(
			dialog.getByText(
				new RegExp(`Are you sure you want to delete ${roleName}\\?`),
			),
		).toBeVisible();
	});

	test("Confirming the deletion removes the role", async ({
		page,
		request,
	}) => {
		const dialog = await openDeleteDialog(page, roleName);

		await dialog.getByRole("button", { name: "Delete role" }).click();

		await expect(dialog).not.toBeVisible();
		await expectRoleNotListed(page, roleName);
		const roles = await listRoles(request, projectId);
		expect(roles.some((r) => r.name === roleName)).toBe(false);
	});

	test("Cancelling the deletion keeps the role", async ({ page }) => {
		const dialog = await openDeleteDialog(page, roleName);

		await dialog.getByRole("button", { name: "Cancel" }).click();

		await expect(dialog).not.toBeVisible();
		await expectRoleListed(page, roleName);
	});

	test("Deleting a role that is assigned to a member takes it away from them", async ({
		page,
		request,
		playwright,
	}) => {
		const inUse = uniqueName("IN_USE");
		const username = uniqueName("ASSIGNED");
		const { memberId } = await createUserWithProjectPermissions(
			request,
			playwright,
			{
				projectId,
				username,
				roleName: inUse,
				permissions: { "tasks:read": true },
			},
		);
		await openRolesSection(page, projectId);

		const dialog = await openDeleteDialog(page, inUse);
		await expect(
			dialog.getByText(
				/Any members currently assigned this role will lose their access/,
			),
		).toBeVisible();
		await dialog.getByRole("button", { name: "Delete role" }).click();

		await expect(dialog).not.toBeVisible();
		await expectRoleNotListed(page, inUse);
		// The member stays; the role is simply no longer one of theirs.
		const response = await request.get(
			`${API_URL}/projects/${projectId}/members`,
		);
		expect(response.ok()).toBeTruthy();
		const body = (await response.json()).data;
		const members: Array<{ id: string; roles: Array<{ name: string }> }> =
			body.items ?? body;
		const member = members.find((m) => m.id === memberId);
		expect(member, "the member is still in the project").toBeTruthy();
		expect(member?.roles.map((r) => r.name)).not.toContain(inUse);
	});
});

// ─── Permission gating ───────────────────────────────────────────────────────

test.describe("Access to role management is permission gated", () => {
	let projectId: string;

	test.beforeEach(async ({ request }) => {
		await authAndCleanup(request);
		projectId = await createProject(request, uniqueName("GATING"));
	});

	test.afterEach(async ({ request }) => {
		await authAndCleanup(request);
	});

	async function signInAsMember(
		page: Page,
		request: APIRequestContext,
		playwright: Parameters<typeof createUserWithProjectPermissions>[1],
		permissions: Record<string, boolean>,
	): Promise<void> {
		const username = uniqueName("MEMBER");
		await createUserWithProjectPermissions(request, playwright, {
			projectId,
			username,
			roleName: uniqueName("LIMITED"),
			permissions,
		});
		await signIn(page, username, RESTRICTED_PASSWORD);
	}

	test("A member without roles:read sees the no-permission state", async ({
		page,
		request,
		playwright,
	}) => {
		await signInAsMember(page, request, playwright, { "tasks:read": true });

		await openRolesSection(page, projectId);

		await expect(
			page.getByText("You don't have permission to view roles"),
		).toBeVisible();
		await expect(rolesTable(page)).toHaveCount(0);
	});

	test("A member with only roles:read can view roles but not change them", async ({
		page,
		request,
		playwright,
	}) => {
		await signInAsMember(page, request, playwright, {
			"roles:read": true,
		});

		await openRolesSection(page, projectId);

		await expectRoleListed(page, "Admin");
		await expectRoleListed(page, "Editor");
		await expectRoleListed(page, "Viewer");
		await expect(page.getByRole("button", { name: "New role" })).toHaveCount(0);
		const editorRow = roleRow(page, "Editor");
		await expect(
			editorRow.getByRole("button", { name: "Edit role" }),
		).toHaveCount(0);
		await expect(
			editorRow.getByRole("button", { name: "Delete role" }),
		).toHaveCount(0);
	});

	test("A member with roles:write can create, edit and delete roles", async ({
		page,
		request,
		playwright,
	}) => {
		await signInAsMember(page, request, playwright, {
			"roles:read": true,
			"roles:write": true,
		});

		await openRolesSection(page, projectId);

		await expect(page.getByRole("button", { name: "New role" })).toBeVisible();
		// The project's own roles can be changed...
		const editorRow = roleRow(page, "Editor");
		await expect(
			editorRow.getByRole("button", { name: "Edit role" }),
		).toBeVisible();
		await expect(
			editorRow.getByRole("button", { name: "Delete role" }),
		).toBeVisible();
		// ...the built-in Admin role can be edited too, but never deleted.
		const adminRow = roleRow(page, "Admin");
		await expect(
			adminRow.getByRole("button", { name: "Edit role" }),
		).toBeVisible();
		await expect(
			adminRow.getByRole("button", { name: "Delete role" }),
		).toHaveCount(0);
	});
});
