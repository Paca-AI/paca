// spec: features/iam/git-role-editor.feature
// seed: tests/seed.spec.ts

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
	cleanupProjectsByPrefix,
	createProject,
	newRunId,
	signIn,
} from "../helpers/e2e-api";
import {
	allow,
	listGlobalRoles,
	listProjectRoles,
	policyOf,
	postProjectRole,
	type RoleRecord,
} from "../helpers/iam";

const PREFIX = "E2E_GITRED_";
const RUN_ID = newRunId();
let counter = 0;
const unique = (label: string) => {
	counter += 1;
	return `${PREFIX}${label}_${RUN_ID}${counter}`;
};

const switchOf = (dialog: Locator, label: string) =>
	dialog.getByRole("switch", { name: label, exact: true });
const nameBox = (dialog: Locator) =>
	dialog.getByRole("textbox", { name: "Role Name" });

async function cleanup(request: APIRequestContext) {
	await cleanupProjectsByPrefix(request, PREFIX);
	await cleanupGlobalRolesByPrefix(request, PREFIX);
}

async function newProjectRoleDialog(page: Page, projectId: string) {
	await page.goto(`${BASE_URL}/projects/${projectId}/settings`);
	await page.getByRole("button", { name: "Roles", exact: true }).click();
	await expect(
		page.getByRole("heading", { name: "Project Roles" }),
	).toBeVisible();
	await page.getByRole("button", { name: "New role" }).click();
	const dialog = page.getByRole("dialog", { name: "New Role" });
	await expect(dialog).toBeVisible();
	return dialog;
}

const actionsOf = (role: RoleRecord) =>
	role.policy.statements.flatMap((s) => s.actions).sort();

async function storedProjectRole(
	request: APIRequestContext,
	projectId: string,
	roleName: string,
): Promise<RoleRecord> {
	const role = (await listProjectRoles(request, projectId)).find(
		(r) => r.name === roleName && r.project_id === projectId,
	);
	expect(role, `project role ${roleName} exists`).toBeTruthy();
	return role as RoleRecord;
}

test.describe("Git permissions in the role editor", () => {
	let projectId: string;

	test.beforeEach(async ({ request, page }) => {
		await authRequest(request);
		await cleanup(request);
		projectId = await createProject(request, unique("PROJECT"));
		await signIn(page);
	});

	test.afterAll(async ({ request }) => {
		await authRequest(request);
		await cleanup(request);
	});

	test("A project role offers the four Git switches", async ({ page }) => {
		const dialog = await newProjectRoleDialog(page, projectId);
		for (const label of [
			"View Git",
			"Manage Git",
			"Approve change requests",
			"Merge change requests",
		]) {
			await expect(switchOf(dialog, label)).toBeVisible();
			await expect(switchOf(dialog, label)).not.toBeChecked();
		}
	});

	const SWITCHES: Array<[string, string]> = [
		["View Git", "git:read"],
		["Manage Git", "git:write"],
		["Approve change requests", "git:approve"],
		["Merge change requests", "git:merge"],
	];
	for (const [label, action] of SWITCHES) {
		test(`The "${label}" switch stores ${action}`, async ({
			page,
			request,
		}) => {
			const dialog = await newProjectRoleDialog(page, projectId);
			const roleName = unique("ONE");
			await nameBox(dialog).fill(roleName);
			await switchOf(dialog, label).click();
			await dialog.getByRole("button", { name: "Create role" }).click();
			await expect(dialog).not.toBeVisible();

			const stored = await storedProjectRole(request, projectId, roleName);
			expect(actionsOf(stored)).toEqual([action]);
			expect(stored.policy.statements[0].resources).toEqual([
				`project/${projectId}/*`,
			]);
		});
	}

	test("Several Git switches are stored together", async ({
		page,
		request,
	}) => {
		const dialog = await newProjectRoleDialog(page, projectId);
		const roleName = unique("BOTH");
		await nameBox(dialog).fill(roleName);
		await switchOf(dialog, "View Git").click();
		await switchOf(dialog, "Manage Git").click();
		await dialog.getByRole("button", { name: "Create role" }).click();
		await expect(dialog).not.toBeVisible();

		expect(
			actionsOf(await storedProjectRole(request, projectId, roleName)),
		).toEqual(["git:read", "git:write"]);
	});

	test("A workspace role offers Manage workspace Git connections", async ({
		page,
		request,
	}) => {
		await page.goto(`${BASE_URL}/admin/global-roles`);
		await page.getByRole("button", { name: "New Role" }).click();
		const dialog = page.getByRole("dialog", { name: "Create Role" });
		await expect(dialog).toBeVisible();
		await expect(
			switchOf(dialog, "Manage workspace Git connections"),
		).not.toBeChecked();

		const roleName = unique("WS");
		await nameBox(dialog).fill(roleName);
		await switchOf(dialog, "Manage workspace Git connections").click();
		await dialog.getByRole("button", { name: "Create role" }).click();
		await expect(dialog).not.toBeVisible();

		const stored = (await listGlobalRoles(request)).find(
			(r) => r.name === roleName,
		);
		expect(stored).toBeTruthy();
		// The domain has one action, so the editor writes it as the wildcard.
		expect(stored?.policy.statements.flatMap((s) => s.actions)).toEqual([
			"git.identities:*",
		]);
		expect(stored?.policy.statements[0].resources).toContain("git_identity/*");
	});

	test("A stored Git role reopens in Simple with its switches on", async ({
		page,
		request,
	}) => {
		const roleName = unique("REOPEN");
		await postProjectRole(
			request,
			projectId,
			roleName,
			policyOf(allow(["git:read", "git:write"], [`project/${projectId}/*`])),
		);
		await page.goto(`${BASE_URL}/projects/${projectId}/settings`);
		await page.getByRole("button", { name: "Roles", exact: true }).click();
		await page
			.getByRole("row")
			.filter({ has: page.getByRole("cell", { name: roleName, exact: true }) })
			.getByRole("button", { name: "Edit role" })
			.click();
		const dialog = page.getByRole("dialog", { name: "Edit Role" });
		await expect(
			page.getByRole("tab", { name: "Simple", exact: true }),
		).toHaveAttribute("aria-selected", "true");
		await expect(switchOf(dialog, "View Git")).toBeChecked();
		await expect(switchOf(dialog, "Manage Git")).toBeChecked();
		await expect(switchOf(dialog, "Merge change requests")).not.toBeChecked();
	});
});
