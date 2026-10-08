// spec: features/iam/role-editor.feature
// seed: tests/seed.spec.ts
//
// The role dialog (a project's Roles page, and Administration > Global Roles)
// edits one policy in two views: Simple (a switch per permission) and Advanced
// (the policy as JSON, validated as you type, with a Simulate panel and
// suggestions). A role opens in Advanced, and stays JSON-only, when the switches
// cannot express it: a Deny, conditions, specific resources or actions the
// switches do not offer. Several plain Allow statements are the union of their
// actions, so they open in Simple.
//
// Roles to edit are created over the API; the browser is used for what the
// editor shows and does.

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
	type RolePolicy,
	signIn,
} from "../helpers/e2e-api";
import {
	allow,
	deny,
	listGlobalRoles,
	listProjectRoles,
	policyOf,
	postGlobalRole,
	postProjectRole,
	type RoleRecord,
} from "../helpers/iam";

const PREFIX = "E2E_IAMEDIT_";
const RUN_ID = newRunId();
let counter = 0;
const unique = (label: string) => {
	counter += 1;
	return `${PREFIX}${label}_${RUN_ID}${counter}`;
};

async function cleanup(request: APIRequestContext) {
	await cleanupProjectsByPrefix(request, PREFIX);
	await cleanupGlobalRolesByPrefix(request, PREFIX);
}

// ─── Locators ────────────────────────────────────────────────────────────────

const modeTab = (page: Page, mode: "Simple" | "Advanced (JSON)") =>
	page.getByRole("tab", { name: mode, exact: true });
const policyBox = (dialog: Locator) =>
	dialog.getByRole("textbox", { name: "Policy (JSON)" });
const switchOf = (dialog: Locator, label: string) =>
	dialog.getByRole("switch", { name: label, exact: true });
const nameBox = (dialog: Locator) =>
	dialog.getByRole("textbox", { name: "Role Name" });
const descriptionBox = (dialog: Locator) =>
	dialog.getByRole("textbox", { name: "Description" });
const advancedNotice = (dialog: Locator) =>
	dialog.getByRole("status").filter({
		hasText: "This role uses advanced features and can only be edited as JSON.",
	});

async function openProjectRoles(page: Page, projectId: string) {
	await page.goto(`${BASE_URL}/projects/${projectId}/settings`);
	await page.getByRole("button", { name: "Roles", exact: true }).click();
	await expect(
		page.getByRole("heading", { name: "Project Roles" }),
	).toBeVisible();
}

async function newProjectRoleDialog(page: Page, projectId: string) {
	await openProjectRoles(page, projectId);
	await page.getByRole("button", { name: "New role" }).click();
	const dialog = page.getByRole("dialog", { name: "New Role" });
	await expect(dialog).toBeVisible();
	return dialog;
}

async function editProjectRole(
	page: Page,
	projectId: string,
	roleName: string,
) {
	await openProjectRoles(page, projectId);
	await page
		.getByRole("row")
		.filter({ has: page.getByRole("cell", { name: roleName, exact: true }) })
		.getByRole("button", { name: "Edit role" })
		.click();
	const dialog = page.getByRole("dialog", { name: "Edit Role" });
	await expect(dialog).toBeVisible();
	return dialog;
}

async function newGlobalRoleDialog(page: Page) {
	await page.goto(`${BASE_URL}/admin/global-roles`);
	await expect(
		page.getByRole("heading", { name: "Global Roles" }),
	).toBeVisible();
	await page.getByRole("button", { name: "New Role" }).click();
	const dialog = page.getByRole("dialog", { name: "Create Role" });
	await expect(dialog).toBeVisible();
	return dialog;
}

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

const actionsOf = (role: RoleRecord) =>
	role.policy.statements.flatMap((s) => s.actions).sort();

test.beforeEach(async ({ request, page }) => {
	await authRequest(request);
	await cleanup(request);
	await signIn(page);
});

test.afterEach(async ({ request }) => {
	await authRequest(request);
	await cleanup(request);
});

// ─── Simple view ─────────────────────────────────────────────────────────────

test.describe("The Simple view", () => {
	test("A project role offers View Project and Assign Roles next to the other switches", async ({
		page,
		request,
	}) => {
		const projectId = await createProject(request, unique("SIMPLE"));
		const dialog = await newProjectRoleDialog(page, projectId);

		await expect(modeTab(page, "Simple")).toHaveAttribute(
			"aria-selected",
			"true",
		);
		await expect(switchOf(dialog, "View Project")).toBeVisible();
		await expect(switchOf(dialog, "Assign Roles")).toBeVisible();
		await expect(switchOf(dialog, "View Tasks")).toBeVisible();
		await expect(dialog.getByRole("switch", { checked: true })).toHaveCount(0);

		// Both are ordinary actions: the stored policy names them.
		const roleName = unique("ASSIGNER");
		await nameBox(dialog).fill(roleName);
		await switchOf(dialog, "View Project").click();
		await switchOf(dialog, "Assign Roles").click();
		await dialog.getByRole("button", { name: "Create role" }).click();
		await expect(dialog).not.toBeVisible();

		const stored = await storedProjectRole(request, projectId, roleName);
		expect(actionsOf(stored)).toEqual(["projects:read", "roles:assign"]);
		expect(stored.policy.statements).toHaveLength(1);
		expect(stored.policy.statements[0].resources).toEqual([
			`project/${projectId}/*`,
		]);
	});

	test("A workspace role offers Assign Global Roles and writes the workspace resources", async ({
		page,
		request,
	}) => {
		const dialog = await newGlobalRoleDialog(page);
		const roleName = unique("WS_ASSIGN");

		await expect(switchOf(dialog, "Assign Global Roles")).toBeVisible();
		await expect(switchOf(dialog, "Read Global Roles")).toBeVisible();
		await nameBox(dialog).fill(roleName);
		await switchOf(dialog, "Assign Global Roles").click();
		await dialog.getByRole("button", { name: "Create role" }).click();
		await expect(dialog).not.toBeVisible();

		const stored = (await listGlobalRoles(request)).find(
			(r) => r.name === roleName,
		);
		expect(stored?.project_id).toBeNull();
		expect(stored?.policy.statements).toHaveLength(1);
		expect(stored?.policy.statements[0].actions).toEqual(["roles:assign"]);
		expect(stored?.policy.statements[0].resources).toEqual(
			expect.arrayContaining(["role", "role/*"]),
		);
	});

	test("A role of several plain Allow statements opens in Simple with the union switched on", async ({
		page,
		request,
	}) => {
		// What the old permission model migrated to: one Allow per area.
		const projectId = await createProject(request, unique("MIGRATED"));
		const roleName = unique("MIGRATED_ROLE");
		const response = await postProjectRole(
			request,
			projectId,
			roleName,
			policyOf(
				allow(["projects:read"], [`project/${projectId}`]),
				allow(["tasks:read", "tasks:write"], [`project/${projectId}/*`]),
				allow(["docs:read"], [`project/${projectId}/*`]),
			),
		);
		expect(response.status()).toBe(201);

		const dialog = await editProjectRole(page, projectId, roleName);
		await expect(modeTab(page, "Simple")).toHaveAttribute(
			"aria-selected",
			"true",
		);
		await expect(advancedNotice(dialog)).toHaveCount(0);
		for (const on of [
			"View Project",
			"View Tasks",
			"Edit Tasks",
			"View Documents",
		]) {
			await expect(switchOf(dialog, on), on).toBeChecked();
		}
		await expect(switchOf(dialog, "Assign Roles")).not.toBeChecked();

		// Saving without a change keeps every action.
		await dialog.getByRole("button", { name: "Save changes" }).click();
		await expect(dialog).not.toBeVisible();
		expect(
			actionsOf(await storedProjectRole(request, projectId, roleName)),
		).toEqual(
			// tasks:read + tasks:write are the whole tasks domain: stored as its wildcard.
			["docs:read", "projects:read", "tasks:*"],
		);
	});
});

// ─── Roles the switches cannot express ───────────────────────────────────────

test.describe("Roles that open in Advanced", () => {
	const cases: Array<{
		label: string;
		reason: string;
		policy: (projectId: string) => RolePolicy;
	}> = [
		{
			label: "a Deny statement",
			reason: "It has a Deny statement.",
			policy: (p) =>
				policyOf(
					allow(["projects:read", "tasks:read"], [`project/${p}/*`]),
					deny(["tasks:write"], [`project/${p}/*`]),
				),
		},
		{
			label: "conditions",
			reason: "It has conditions.",
			policy: (p) =>
				policyOf(
					allow(["tasks:read"], [`project/${p}/task/*`], {
						StringEquals: {
							"task.sprint_id": "00000000-0000-4000-8000-000000000001",
						},
					}),
				),
		},
		{
			label: "specific resources",
			reason: "It applies to specific resources.",
			policy: (p) => policyOf(allow(["tasks:read"], [`project/${p}/task/*`])),
		},
	];

	for (const { label, reason, policy } of cases) {
		test(`A role with ${label} opens in Advanced with the notice, and switching back is refused`, async ({
			page,
			request,
		}) => {
			const projectId = await createProject(request, unique("ADV"));
			const roleName = unique("ADV_ROLE");
			const created = await postProjectRole(
				request,
				projectId,
				roleName,
				policy(projectId),
			);
			expect(created.status()).toBe(201);

			const dialog = await editProjectRole(page, projectId, roleName);
			await expect(modeTab(page, "Advanced (JSON)")).toHaveAttribute(
				"aria-selected",
				"true",
			);
			await expect(advancedNotice(dialog)).toContainText(reason);
			await expect(dialog.getByRole("switch")).toHaveCount(0);
			await expect(policyBox(dialog)).toHaveValue(/"statements"/);

			// The JSON is never rewritten from switches that cannot show it.
			await modeTab(page, "Simple").click();
			await expect(modeTab(page, "Advanced (JSON)")).toHaveAttribute(
				"aria-selected",
				"true",
			);
			await expect(advancedNotice(dialog)).toContainText(reason);

			// Saving untouched keeps the policy as it was.
			const before = await storedProjectRole(request, projectId, roleName);
			await dialog.getByRole("button", { name: "Save changes" }).click();
			await expect(dialog).not.toBeVisible();
			const after = await storedProjectRole(request, projectId, roleName);
			expect(after.policy).toEqual(before.policy);
		});
	}
});

// ─── Advanced view ───────────────────────────────────────────────────────────

test.describe("The Advanced view", () => {
	let projectId: string;

	test.beforeEach(async ({ request }) => {
		projectId = await createProject(request, unique("ADVANCED"));
	});

	async function openAdvanced(page: Page) {
		const dialog = await newProjectRoleDialog(page, projectId);
		await modeTab(page, "Advanced (JSON)").click();
		await expect(policyBox(dialog)).toBeVisible();
		return dialog;
	}

	test("Problems are listed by path as the policy is typed, and block saving until fixed", async ({
		page,
	}) => {
		const dialog = await openAdvanced(page);
		await nameBox(dialog).fill(unique("INVALID"));
		const create = dialog.getByRole("button", { name: "Create role" });

		await policyBox(dialog).fill(
			JSON.stringify(
				policyOf(allow(["tasks:reed"], [`project/${projectId}/*`])),
			),
		);
		const problems = dialog
			.getByRole("alert")
			.filter({ hasText: "statements[0].actions[0]" });
		await expect(problems).toContainText("unknown action");
		await expect(create).toBeDisabled();

		// A resource outside the project is reported at its own path.
		await policyBox(dialog).fill(
			JSON.stringify(policyOf(allow(["tasks:read"], ["project/*"]))),
		);
		await expect(
			dialog
				.getByRole("alert")
				.filter({ hasText: "statements[0].resources[0]" }),
		).toBeVisible();
		await expect(create).toBeDisabled();

		await policyBox(dialog).fill(
			JSON.stringify(
				policyOf(allow(["tasks:read"], [`project/${projectId}/*`])),
			),
		);
		await expect(dialog.getByRole("alert")).toHaveCount(0);
		await expect(create).toBeEnabled();
	});

	test("Broken JSON is reported and blocks saving", async ({ page }) => {
		const dialog = await openAdvanced(page);
		await policyBox(dialog).fill('{ "statements": [');
		await expect(
			dialog.getByRole("alert").filter({ hasText: "The JSON is not valid" }),
		).toBeVisible();
		await expect(
			dialog.getByRole("button", { name: "Create role" }),
		).toBeDisabled();
	});

	test("A role is created straight from JSON, Deny and conditions kept as typed", async ({
		page,
		request,
	}) => {
		const dialog = await newGlobalRoleDialog(page);
		await modeTab(page, "Advanced (JSON)").click();
		const roleName = unique("FROM_JSON");
		const typed = policyOf(
			{
				sid: "ReadTasks",
				...allow(["tasks:read"], [`project/${projectId}/task/*`], {
					StringEquals: {
						"task.sprint_id": "00000000-0000-4000-8000-000000000002",
					},
				}),
			},
			{
				sid: "NoProdShell",
				...deny(["environments:connect"], ["project/*/environment/*"]),
			},
		);
		await nameBox(dialog).fill(roleName);
		await policyBox(dialog).fill(JSON.stringify(typed));
		await dialog.getByRole("button", { name: "Create role" }).click();
		await expect(dialog).not.toBeVisible();

		const stored = (await listGlobalRoles(request)).find(
			(r) => r.name === roleName,
		);
		expect(stored?.policy.statements).toHaveLength(2);
		// The Deny is kept as typed; a single condition value is stored as a list.
		expect(stored?.policy.statements[1]).toEqual(typed.statements[1]);
		expect(stored?.policy.statements[0]).toEqual({
			...typed.statements[0],
			conditions: {
				StringEquals: {
					"task.sprint_id": ["00000000-0000-4000-8000-000000000002"],
				},
			},
		});
	});

	test("Simulate says whether a request is allowed and which statement decided", async ({
		page,
	}) => {
		const dialog = await openAdvanced(page);
		const sprintId = "00000000-0000-4000-8000-0000000000aa";
		await policyBox(dialog).fill(
			JSON.stringify(
				policyOf(
					{
						sid: "OnlyThisSprint",
						...allow(["tasks:read"], [`project/${projectId}/task/*`], {
							StringEquals: { "task.sprint_id": sprintId },
						}),
					},
					{
						sid: "NoWrites",
						...deny(["tasks:write"], [`project/${projectId}/*`]),
					},
				),
			),
		);
		const simulate = dialog.getByRole("region", { name: "Simulate a request" });
		const action = simulate.getByRole("combobox", { name: "Action" });
		const resource = simulate.getByRole("textbox", { name: "Resource" });
		const attributes = simulate.getByRole("textbox", {
			name: "Attributes (optional)",
		});
		const run = simulate.getByRole("button", { name: "Simulate" });
		const task = `project/${projectId}/task/00000000-0000-4000-8000-0000000000bb`;
		const result = simulate.getByRole("status");

		// Allowed, by the named statement, once the condition's attribute is supplied.
		await action.fill("tasks:read");
		await resource.fill(task);
		await attributes.fill(`task.sprint_id=${sprintId}`);
		await run.click();
		await expect(result).toContainText("Allowed");
		await expect(result).toContainText("Decided by OnlyThisSprint (Allow).");

		// Without the attribute the Allow does not apply: denied by default.
		await attributes.fill("");
		await run.click();
		await expect(result).toContainText("Denied");
		await expect(result).toContainText(
			"No statement allows this request, so it is denied by default.",
		);

		// An explicit Deny is named as the decider.
		await action.fill("tasks:write");
		await run.click();
		await expect(result).toContainText("Denied");
		await expect(result).toContainText("Decided by NoWrites (Deny).");

		// A malformed attribute line is reported before anything is sent.
		await attributes.fill("not a pair");
		await run.click();
		await expect(simulate.getByRole("alert")).toContainText(
			"Not a key=value pair: not a pair",
		);
	});

	test("Typing an action or a condition key offers suggestions that complete it", async ({
		page,
	}) => {
		const dialog = await openAdvanced(page);
		const suggestions = dialog.getByRole("listbox", { name: "Suggestions" });

		await policyBox(dialog).fill(
			'{"statements":[{"effect":"Allow","actions":["tasks:wr',
		);
		await expect(suggestions).toBeVisible();
		const chip = suggestions.getByRole("option", {
			name: "tasks:write",
			exact: true,
		});
		await expect(chip).toBeVisible();
		await chip.click();
		await expect(policyBox(dialog)).toHaveValue(/"actions":\["tasks:write/);

		await policyBox(dialog).fill(
			'{"statements":[{"conditions":{"StringEquals":{"task.sp',
		);
		const key = suggestions.getByRole("option", {
			name: "task.sprint_id",
			exact: true,
		});
		await expect(key).toBeVisible();
		await key.click();
		await expect(policyBox(dialog)).toHaveValue(/"task\.sprint_id/);
	});

	test("A link to the IAM roles guide opens in a new tab", async ({ page }) => {
		const dialog = await openAdvanced(page);
		const link = dialog.getByRole("link", { name: "Read the IAM roles guide" });
		await expect(link).toBeVisible();
		await expect(link).toHaveAttribute(
			"href",
			/docs\/guides\/iam-authorization\.md/,
		);
		await expect(link).toHaveAttribute("target", "_blank");
		await expect(link).toHaveAttribute("rel", /noopener/);
		// The Simple view has no such link.
		await modeTab(page, "Simple").click();
		await expect(
			dialog.getByRole("link", { name: "Read the IAM roles guide" }),
		).toHaveCount(0);
	});
});

// ─── Switching views ─────────────────────────────────────────────────────────

test.describe("Switching between Simple and Advanced", () => {
	test("The switches become a policy, and an edited policy comes back as switches", async ({
		page,
		request,
	}) => {
		const projectId = await createProject(request, unique("ROUNDTRIP"));
		const dialog = await newProjectRoleDialog(page, projectId);

		await switchOf(dialog, "View Tasks").click();
		await switchOf(dialog, "View Project").click();
		await modeTab(page, "Advanced (JSON)").click();

		const written = JSON.parse(
			await policyBox(dialog).inputValue(),
		) as RolePolicy;
		expect(written.statements).toHaveLength(1);
		expect(written.statements[0].effect).toBe("Allow");
		expect(written.statements[0].actions.sort()).toEqual([
			"projects:read",
			"tasks:read",
		]);

		// Add an action in JSON, then go back: its switch is on.
		written.statements[0].actions.push("docs:read");
		await policyBox(dialog).fill(JSON.stringify(written));
		await modeTab(page, "Simple").click();
		await expect(modeTab(page, "Simple")).toHaveAttribute(
			"aria-selected",
			"true",
		);
		for (const on of ["View Tasks", "View Project", "View Documents"]) {
			await expect(switchOf(dialog, on), on).toBeChecked();
		}
		await expect(switchOf(dialog, "Assign Roles")).not.toBeChecked();

		const roleName = unique("ROUNDTRIP_ROLE");
		await nameBox(dialog).fill(roleName);
		await dialog.getByRole("button", { name: "Create role" }).click();
		await expect(dialog).not.toBeVisible();
		expect(
			actionsOf(await storedProjectRole(request, projectId, roleName)),
		).toEqual(["docs:read", "projects:read", "tasks:read"]);
	});

	test("JSON the switches cannot show stays JSON, with the reason", async ({
		page,
		request,
	}) => {
		const projectId = await createProject(request, unique("STUCK"));
		const dialog = await newProjectRoleDialog(page, projectId);
		await modeTab(page, "Advanced (JSON)").click();
		await policyBox(dialog).fill(
			JSON.stringify(
				policyOf(deny(["tasks:write"], [`project/${projectId}/*`])),
			),
		);

		await modeTab(page, "Simple").click();
		await expect(modeTab(page, "Advanced (JSON)")).toHaveAttribute(
			"aria-selected",
			"true",
		);
		await expect(dialog.getByRole("status")).toContainText(
			"It has a Deny statement.",
		);
		await expect(policyBox(dialog)).toHaveValue(/"Deny"/);

		// Broken JSON cannot be shown as switches either.
		await policyBox(dialog).fill("{ nope");
		await modeTab(page, "Simple").click();
		await expect(dialog.getByRole("status")).toContainText(
			"The JSON is not valid yet.",
		);
	});
});

// ─── Name, description and scope ─────────────────────────────────────────────

test.describe("Name, description and scope in the dialog", () => {
	test("The description has a line of its own and is saved and listed in the table", async ({
		page,
		request,
	}) => {
		const projectId = await createProject(request, unique("DESCRIBED"));
		const dialog = await newProjectRoleDialog(page, projectId);
		const roleName = unique("DESCRIBED_ROLE");
		const description = "Contractors who triage incoming tasks";

		const name = await nameBox(dialog).boundingBox();
		const desc = await descriptionBox(dialog).boundingBox();
		expect(name).not.toBeNull();
		expect(desc).not.toBeNull();
		// Below the name, not beside it.
		expect((desc?.y ?? 0) >= (name?.y ?? 0) + (name?.height ?? 0)).toBe(true);
		await expect(dialog.getByText("(optional)")).toBeVisible();

		await nameBox(dialog).fill(roleName);
		await descriptionBox(dialog).fill(description);
		await expect(dialog.getByText(`${description.length}/500`)).toBeVisible();
		await dialog.getByRole("button", { name: "Create role" }).click();
		await expect(dialog).not.toBeVisible();

		const row = page
			.getByRole("row")
			.filter({ has: page.getByRole("cell", { name: roleName, exact: true }) });
		await expect(row.getByText(description, { exact: true })).toBeVisible();
		expect(
			(await storedProjectRole(request, projectId, roleName)).description,
		).toBe(description);

		await row.getByRole("button", { name: "Edit role" }).click();
		const edit = page.getByRole("dialog", { name: "Edit Role" });
		await expect(descriptionBox(edit)).toHaveValue(description);
	});

	test("There is no Limit-to control: scope is written in the policy", async ({
		page,
		request,
	}) => {
		const projectId = await createProject(request, unique("NOLIMIT"));
		const project = await newProjectRoleDialog(page, projectId);
		for (const mode of ["Simple", "Advanced (JSON)"] as const) {
			await modeTab(page, mode).click();
			await expect(
				project.getByText(/limit(ed)? to/i),
				`project ${mode}`,
			).toHaveCount(0);
			await expect(
				project.getByRole("combobox", { name: /limit/i }),
				`project ${mode}`,
			).toHaveCount(0);
			await expect(project.getByLabel(/limit/i), `project ${mode}`).toHaveCount(
				0,
			);
		}
		await project.getByRole("button", { name: "Cancel" }).click();

		const workspace = await newGlobalRoleDialog(page);
		for (const mode of ["Simple", "Advanced (JSON)"] as const) {
			await modeTab(page, mode).click();
			await expect(
				workspace.getByText(/limit(ed)? to/i),
				`workspace ${mode}`,
			).toHaveCount(0);
			await expect(
				workspace.getByRole("combobox", { name: /limit/i }),
				`workspace ${mode}`,
			).toHaveCount(0);
			await expect(
				workspace.getByLabel(/limit/i),
				`workspace ${mode}`,
			).toHaveCount(0);
		}
	});

	test("The dialog says whether it edits a workspace or a project role", async ({
		page,
		request,
	}) => {
		const projectId = await createProject(request, unique("SCOPETAG"));
		const project = await newProjectRoleDialog(page, projectId);
		await expect(
			project.getByText("Project role", { exact: true }),
		).toBeVisible();
		await project.getByRole("button", { name: "Cancel" }).click();

		const workspace = await newGlobalRoleDialog(page);
		await expect(
			workspace.getByText("Workspace role", { exact: true }),
		).toBeVisible();
	});

	test("A name that is taken is refused inline, in a project and in the workspace", async ({
		page,
		request,
	}) => {
		const projectId = await createProject(request, unique("TAKEN"));
		const project = await newProjectRoleDialog(page, projectId);
		await nameBox(project).fill("Editor");
		await project.getByRole("button", { name: "Create role" }).click();
		await expect(
			project.getByText("A role with this name already exists."),
		).toBeVisible();
		await expect(project).toBeVisible();
		await project.getByRole("button", { name: "Cancel" }).click();

		const taken = unique("WS_TAKEN");
		expect((await postGlobalRole(request, taken, policyOf())).status()).toBe(
			201,
		);
		const workspace = await newGlobalRoleDialog(page);
		await nameBox(workspace).fill(taken);
		await workspace.getByRole("button", { name: "Create role" }).click();
		await expect(
			workspace.getByText("A role with this name already exists."),
		).toBeVisible();
		// Renaming clears the error.
		await nameBox(workspace).fill(`${taken}_2`);
		await expect(
			workspace.getByText("A role with this name already exists."),
		).toHaveCount(0);
	});
});
