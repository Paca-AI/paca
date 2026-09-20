// spec: features/projects/roles.feature
// seed: tests/seed.spec.ts
//
// The admin account is a super_admin, so it can manage roles in every
// project. Scenarios about a member who LACKS a permission create a second,
// limited user through the admin API (createUserWithProjectPermissions), and
// sign in as that user in the browser.
//
// A11y note: the permission switches in the role form have no accessible name
// of their own (the label lives in a sibling <span>), so they are located by
// finding the row that contains the permission's label and taking its switch.

import {
	type APIRequestContext,
	expect,
	type Locator,
	type Page,
	test,
} from "@playwright/test";
import {
	API_URL,
	BASE_URL,
	cleanupProjectsByPrefix,
	cleanupUsersByPrefix,
	createProject,
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
	role_name: string;
	project_id: string | null;
	permissions: Record<string, boolean>;
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

async function createRole(
	request: APIRequestContext,
	projectId: string,
	roleName: string,
	permissions: Record<string, boolean>,
): Promise<void> {
	const response = await request.post(
		`${API_URL}/projects/${projectId}/roles`,
		{ data: { role_name: roleName, permissions } },
	);
	expect(response.ok()).toBeTruthy();
}

async function storedPermissions(
	request: APIRequestContext,
	projectId: string,
	roleName: string,
): Promise<Record<string, boolean>> {
	const role = (await listRoles(request, projectId)).find(
		(r) => r.role_name === roleName,
	);
	expect(role, `role ${roleName} should exist`).toBeTruthy();
	return role?.permissions ?? {};
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

function permissionBadge(row: Locator, permission: string): Locator {
	return row.getByText(permission, { exact: true });
}

function permissionSwitch(dialog: Locator, label: string): Locator {
	return dialog
		.locator("div.justify-between")
		.filter({ has: dialog.page().getByText(label, { exact: true }) })
		.getByRole("switch");
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
			table.getByRole("columnheader", { name: "Permissions" }),
		).toBeVisible();
		await expect(
			table.getByRole("columnheader", { name: "Created" }),
		).toBeVisible();
		await expectRoleListed(page, "Admin");
		await expectRoleListed(page, "Editor");
		await expectRoleListed(page, "Viewer");
	});

	test("The seeded Admin role shows the wildcard grant", async ({ page }) => {
		await openRolesSection(page, projectId);

		await expect(permissionBadge(roleRow(page, "Admin"), "*")).toBeVisible();
	});

	test("A role with no permissions shows a placeholder", async ({
		page,
		request,
	}) => {
		const roleName = uniqueName("EMPTY");
		await createRole(request, projectId, roleName, {});
		await openRolesSection(page, projectId);

		await expect(
			roleRow(page, roleName).getByText("No permissions assigned"),
		).toBeVisible();
	});

	test("The permission badges of a role list its granted permissions", async ({
		page,
		request,
	}) => {
		const roleName = uniqueName("BADGES");
		await createRole(request, projectId, roleName, {
			"tasks.read": true,
			"docs.read": true,
		});
		await openRolesSection(page, projectId);

		const row = roleRow(page, roleName);
		await expect(permissionBadge(row, "tasks.read")).toBeVisible();
		await expect(permissionBadge(row, "docs.read")).toBeVisible();
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
		await expect(
			dialog.getByRole("button", { name: "Create role" }),
		).toBeDisabled();
	});

	test("The Create role button is enabled once a name is entered", async ({
		page,
	}) => {
		const dialog = await openCreateDialog(page);

		await dialog
			.getByRole("textbox", { name: "Role Name" })
			.fill(uniqueName("NEW"));

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
	}) => {
		const roleName = uniqueName("READER");
		const dialog = await openCreateDialog(page);
		await dialog.getByRole("textbox", { name: "Role Name" }).fill(roleName);
		await turnOn(dialog, "View Tasks", "View Documents");

		await dialog.getByRole("button", { name: "Create role" }).click();

		await expect(dialog).not.toBeVisible();
		await expectRoleListed(page, roleName);
		const row = roleRow(page, roleName);
		await expect(permissionBadge(row, "tasks.read")).toBeVisible();
		await expect(permissionBadge(row, "docs.read")).toBeVisible();
	});

	test("Enabling every permission of an area is stored as the area wildcard", async ({
		page,
	}) => {
		const roleName = uniqueName("TASKS");
		const dialog = await openCreateDialog(page);
		await dialog.getByRole("textbox", { name: "Role Name" }).fill(roleName);
		await turnOn(dialog, "View Tasks", "Edit Tasks");

		await dialog.getByRole("button", { name: "Create role" }).click();

		await expect(dialog).not.toBeVisible();
		await expect(
			permissionBadge(roleRow(page, roleName), "tasks.*"),
		).toBeVisible();
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
		await createRole(request, projectId, roleName, { "tasks.read": true });
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
	}) => {
		const renamed = uniqueName("RENAMED");
		const dialog = await openEditDialog(page, roleName);
		await dialog.getByRole("textbox", { name: "Role Name" }).fill(renamed);
		await turnOn(dialog, "View Documents");

		await dialog.getByRole("button", { name: "Save changes" }).click();

		await expect(dialog).not.toBeVisible();
		await expectRoleListed(page, renamed);
		await expectRoleNotListed(page, roleName);
		const row = roleRow(page, renamed);
		await expect(permissionBadge(row, "tasks.read")).toBeVisible();
		await expect(permissionBadge(row, "docs.read")).toBeVisible();
	});

	test("Turning a permission off removes it from the role", async ({
		page,
	}) => {
		const dialog = await openEditDialog(page, roleName);
		await turnOff(dialog, "View Tasks");

		await dialog.getByRole("button", { name: "Save changes" }).click();

		await expect(dialog).not.toBeVisible();
		await expect(
			roleRow(page, roleName).getByText("No permissions assigned"),
		).toBeVisible();
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
		await createRole(request, projectId, fullRoleName, { "*": true });
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
				/This role automatically includes every permission, including ones added in the future/,
			),
		).toBeVisible();
		await expect(dialog.getByRole("switch").first()).toBeChecked();
		await expect(dialog.getByRole("switch", { checked: false })).toHaveCount(0);
	});

	test("The seeded Admin role is a Full access role", async ({ page }) => {
		const dialog = await openEditDialog(page, "Admin");

		await expect(
			dialog.getByText("Full access", { exact: true }),
		).toBeVisible();
	});

	test("A role with an enumerated permission set does not show the Full access badge", async ({
		page,
		request,
	}) => {
		const partial = uniqueName("PARTIAL");
		await createRole(request, projectId, partial, { "tasks.read": true });
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
		const stored = await storedPermissions(request, projectId, fullRoleName);
		expect(stored["*"]).toBeUndefined();
		expect(stored["project.roles.write"]).toBeUndefined();
		expect(stored["project.roles.read"]).toBe(true);
	});

	test("Saving an untouched Full access role keeps the wildcard", async ({
		page,
		request,
	}) => {
		const dialog = await openEditDialog(page, fullRoleName);

		await dialog.getByRole("button", { name: "Save changes" }).click();

		await expect(dialog).not.toBeVisible();
		expect(await storedPermissions(request, projectId, fullRoleName)).toEqual({
			"*": true,
		});
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
		await createRole(request, projectId, roleName, { "tasks.read": true });
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
		expect(roles.some((r) => r.role_name === roleName)).toBe(false);
	});

	test("Cancelling the deletion keeps the role", async ({ page }) => {
		const dialog = await openDeleteDialog(page, roleName);

		await dialog.getByRole("button", { name: "Cancel" }).click();

		await expect(dialog).not.toBeVisible();
		await expectRoleListed(page, roleName);
	});

	test("A role that is still assigned to a member cannot be deleted", async ({
		page,
		request,
		playwright,
	}) => {
		const inUse = uniqueName("IN_USE");
		await createUserWithProjectPermissions(request, playwright, {
			projectId,
			username: uniqueName("ASSIGNED"),
			roleName: inUse,
			permissions: { "tasks.read": true },
		});
		await openRolesSection(page, projectId);

		const dialog = await openDeleteDialog(page, inUse);
		await dialog.getByRole("button", { name: "Delete role" }).click();

		await expect(
			dialog.getByText(
				"This role cannot be deleted because it is still assigned to one or more members.",
			),
		).toBeVisible();
		await dialog.getByRole("button", { name: "Cancel" }).click();
		await expect(dialog).not.toBeVisible();
		await expectRoleListed(page, inUse);
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

	test("A member without project.roles.read sees the no-permission state", async ({
		page,
		request,
		playwright,
	}) => {
		await signInAsMember(page, request, playwright, { "tasks.read": true });

		await openRolesSection(page, projectId);

		await expect(
			page.getByText("You don't have permission to view roles"),
		).toBeVisible();
		await expect(rolesTable(page)).toHaveCount(0);
	});

	test("A member with only project.roles.read can view roles but not change them", async ({
		page,
		request,
		playwright,
	}) => {
		await signInAsMember(page, request, playwright, {
			"project.roles.read": true,
		});

		await openRolesSection(page, projectId);

		await expectRoleListed(page, "Admin");
		await expectRoleListed(page, "Editor");
		await expectRoleListed(page, "Viewer");
		await expect(page.getByRole("button", { name: "New role" })).toHaveCount(0);
		const adminRow = roleRow(page, "Admin");
		await expect(
			adminRow.getByRole("button", { name: "Edit role" }),
		).toHaveCount(0);
		await expect(
			adminRow.getByRole("button", { name: "Delete role" }),
		).toHaveCount(0);
	});

	test("A member with project.roles.write can create, edit and delete roles", async ({
		page,
		request,
		playwright,
	}) => {
		await signInAsMember(page, request, playwright, {
			"project.roles.read": true,
			"project.roles.write": true,
		});

		await openRolesSection(page, projectId);

		await expect(page.getByRole("button", { name: "New role" })).toBeVisible();
		const adminRow = roleRow(page, "Admin");
		await expect(
			adminRow.getByRole("button", { name: "Edit role" }),
		).toBeVisible();
		await expect(
			adminRow.getByRole("button", { name: "Delete role" }),
		).toBeVisible();
	});
});
