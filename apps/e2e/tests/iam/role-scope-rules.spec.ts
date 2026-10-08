// spec: features/iam/role-scope-rules.feature
// seed: tests/seed.spec.ts
//
// Where a role lives decides where it can be used (docs/guides/iam-authorization.md,
// "Role scopes" and "Guard rails"): a project-owned role may only name its own
// project, a workspace role may name anything, a workspace "project template"
// (every resource has a wildcard project segment) attaches per project only,
// built-in roles can be edited but not deleted, the default role cannot be
// deleted, the last full-access holder stays, names follow a few rules, and a
// role can only be saved when its author already holds what it grants.
//
// Everything runs over the API: these are server rules with exact status and
// error codes.

import { type APIRequestContext, expect, test } from "@playwright/test";
import {
	API_URL,
	authRequest,
	cleanupGlobalRolesByPrefix,
	cleanupProjectsByPrefix,
	cleanupUsersByPrefix,
	createGlobalRole,
	createProject,
	createUserWithPassword,
	globalRoleIdByName,
	newRunId,
	type RolePolicy,
} from "../helpers/e2e-api";
import {
	allow,
	asUser,
	createMember,
	createPerson,
	dataOf,
	deny,
	errorCode,
	listGlobalRoles,
	listProjectRoles,
	listTasks,
	policyIssues,
	policyOf,
	postGlobalRole,
	postProjectRole,
	projectRoleFrom,
	putMemberRoles,
	putUserRoles,
	type RoleRecord,
} from "../helpers/iam";

const PREFIX = "E2E_IAMRULES_";
const RUN_ID = newRunId();
const name = (label: string) => `${PREFIX}${label}_${RUN_ID}`;

async function cleanup(request: APIRequestContext) {
	await cleanupProjectsByPrefix(request, PREFIX);
	await cleanupUsersByPrefix(request, PREFIX);
	await cleanupGlobalRolesByPrefix(request, PREFIX);
}

test.beforeEach(async ({ request, context }) => {
	await authRequest(request);
	await cleanup(request);
	await context.clearCookies();
});

test.afterEach(async ({ request }) => {
	await authRequest(request);
	await cleanup(request);
});

const readTasksOn = (...resources: string[]) =>
	policyOf(allow(["projects:read", "tasks:read"], resources));

// ─── Project-owned roles ─────────────────────────────────────────────────────

test.describe("A project-owned role may only name its own project", () => {
	let projectId: string;
	let otherId: string;

	test.beforeEach(async ({ request }) => {
		projectId = await createProject(request, name("OWNED"));
		otherId = await createProject(request, name("OTHER"));
	});

	test("Resources outside the project are refused with ROLE_POLICY_INVALID and an issue path", async ({
		request,
	}) => {
		const outside: Record<string, string> = {
			"every project": "project/*",
			"another project": `project/${otherId}/*`,
			"another project's task": `project/${otherId}/task/*`,
			everything: "*",
			"a platform root": "user/*",
		};
		for (const [label, resource] of Object.entries(outside)) {
			const response = await postProjectRole(
				request,
				projectId,
				name(`OUT_${label.replace(/\W+/g, "_").toUpperCase()}`),
				readTasksOn(resource),
			);
			expect(response.status(), label).toBe(422);
			expect(await errorCode(response), label).toBe("ROLE_POLICY_INVALID");
			const issues = await policyIssues(response);
			expect(
				issues.map((i) => i.path),
				label,
			).toContain("statements[0].resources[0]");
		}
	});

	test("The issue points at the offending resource among several", async ({
		request,
	}) => {
		const response = await postProjectRole(
			request,
			projectId,
			name("TWO_PROJECTS"),
			readTasksOn(`project/${projectId}/*`, `project/${otherId}/*`),
		);
		expect(response.status()).toBe(422);
		const issues = await policyIssues(response);
		expect(issues.map((i) => i.path)).toEqual(["statements[0].resources[1]"]);
	});

	test("The project's own resource forms are accepted", async ({ request }) => {
		for (const [label, resource] of [
			["PROJECT", `project/${projectId}`],
			["ALL", `project/${projectId}/*`],
			["TASKS", `project/${projectId}/task/*`],
			["ONE_SPRINT", `project/${projectId}/sprint/${otherId}`],
		]) {
			const response = await postProjectRole(
				request,
				projectId,
				name(`IN_${label}`),
				readTasksOn(resource),
			);
			expect(response.status(), resource).toBe(201);
			expect((await dataOf<RoleRecord>(response)).project_id).toBe(projectId);
		}
	});

	test("Updating a role to name another project is refused, and so does validating it", async ({
		request,
	}) => {
		const roleId = await projectRoleFrom(
			request,
			projectId,
			name("UPDATE_ME"),
			allow(["tasks:read"], [`project/${projectId}/*`]),
		);
		const update = await request.put(
			`${API_URL}/projects/${projectId}/roles/${roleId}`,
			{
				data: {
					name: name("UPDATE_ME"),
					description: "",
					policy: readTasksOn(`project/${otherId}/*`),
				},
			},
		);
		expect(update.status()).toBe(422);
		expect(await errorCode(update)).toBe("ROLE_POLICY_INVALID");

		const validate = await request.post(
			`${API_URL}/projects/${projectId}/roles/validate`,
			{ data: { policy: readTasksOn("project/*") } },
		);
		expect(validate.status()).toBe(200);
		const result = await dataOf<{
			valid: boolean;
			issues: Array<{ path: string }>;
		}>(validate);
		expect(result.valid).toBe(false);
		expect(result.issues.map((i) => i.path)).toContain(
			"statements[0].resources[0]",
		);

		// The workspace-level validator knows nothing about a project.
		const workspace = await request.post(`${API_URL}/roles/validate`, {
			data: { policy: readTasksOn("project/*") },
		});
		expect((await dataOf<{ valid: boolean }>(workspace)).valid).toBe(true);
	});
});

// ─── Workspace roles and templates ───────────────────────────────────────────

test.describe("Workspace roles and project templates", () => {
	test("A workspace role naming a project is accepted and attachable platform-wide, reaching only that project", async ({
		request,
		playwright,
	}) => {
		const named = await createProject(request, name("NAMED"));
		const unnamed = await createProject(request, name("UNNAMED"));
		const response = await postGlobalRole(
			request,
			name("WS_NAMING_PROJECT"),
			readTasksOn(`project/${named}/*`),
		);
		expect(response.status()).toBe(201);
		const role = await dataOf<RoleRecord>(response);
		expect(role.project_id).toBeNull();

		const person = await createPerson(request, playwright, name("WS_USER"));
		// Platform-wide, to a user who is a member of neither project.
		expect(
			(await putUserRoles(request, person.userId, [role.id])).status(),
		).toBe(200);
		await asUser(playwright, person.username, async (api) => {
			expect((await listTasks(api, named)).status).toBe(200);
			expect((await listTasks(api, unnamed)).status).toBe(403);
		});
	});

	test("A workspace template (project/*) cannot be attached platform-wide or made the default", async ({
		request,
		playwright,
	}) => {
		const template = await createGlobalRole(
			request,
			name("TEMPLATE"),
			readTasksOn("project/*"),
		);
		const person = await createPerson(
			request,
			playwright,
			name("TEMPLATE_USER"),
		);

		const attach = await putUserRoles(request, person.userId, [template.id]);
		expect(attach.status()).toBe(422);
		expect(await errorCode(attach)).toBe("ROLE_NOT_ATTACHABLE");

		const asDefault = await request.put(
			`${API_URL}/admin/roles/${template.id}/default`,
		);
		expect(asDefault.status()).toBe(422);
		expect(await errorCode(asDefault)).toBe("ROLE_NOT_ATTACHABLE");
		// The default is unchanged.
		const roles = await listGlobalRoles(request);
		expect(roles.find((r) => r.is_default)?.name).not.toBe(template.name);
		expect(roles.filter((r) => r.is_default)).toHaveLength(1);
	});

	test("A workspace template is attachable per project and then acts only there", async ({
		request,
		playwright,
	}) => {
		const projectId = await createProject(request, name("TPL_PROJECT"));
		const elsewhere = await createProject(request, name("TPL_ELSEWHERE"));
		const template = await createGlobalRole(
			request,
			name("TEMPLATE_PER_PROJECT"),
			readTasksOn("project/*"),
		);
		const member = await createMember(
			request,
			playwright,
			projectId,
			name("TPL_MEMBER"),
			[template.id],
		);

		await asUser(playwright, member.username, async (api) => {
			expect((await listTasks(api, projectId)).status).toBe(200);
			// "project/*" is every project, but attached here it is cut to this one.
			expect((await listTasks(api, elsewhere)).status).toBe(403);
		});
		// And it can still be swapped out per project.
		expect(
			(
				await putMemberRoles(request, projectId, member.memberId, [template.id])
			).status(),
		).toBe(200);
	});

	test("A role mixing project/* with a platform resource is not a template", async ({
		request,
		playwright,
	}) => {
		const mixed = await createGlobalRole(
			request,
			name("MIXED"),
			policyOf(
				allow(["projects:read", "tasks:read"], ["project/*"]),
				allow(["users:read"], ["user", "user/*"]),
			),
		);
		const person = await createPerson(request, playwright, name("MIXED_USER"));
		expect(
			(await putUserRoles(request, person.userId, [mixed.id])).status(),
		).toBe(200);
	});
});

// ─── A new project's roles ───────────────────────────────────────────────────

test.describe("A new project's own roles", () => {
	test("A project starts with exactly Admin, Editor and Viewer, on its own resources", async ({
		request,
	}) => {
		const projectId = await createProject(request, name("FRESH"));
		const roles = await listProjectRoles(request, projectId);

		expect(roles.map((r) => r.name).sort()).toEqual([
			"Admin",
			"Editor",
			"Viewer",
		]);
		for (const role of roles) {
			expect(role.project_id, role.name).toBe(projectId);
			// The real project id has replaced the template's placeholder.
			const resources = role.policy.statements.flatMap((s) => s.resources);
			expect(resources.length, role.name).toBeGreaterThan(0);
			for (const resource of resources) {
				expect(resource, role.name).toMatch(
					new RegExp(`^project/${projectId}(/|$)`),
				);
			}
		}

		const admin = roles.find((r) => r.name === "Admin");
		expect(admin?.is_system).toBe(true);
		expect(admin?.policy.statements.flatMap((s) => s.actions)).toContain("*");
		expect(roles.find((r) => r.name === "Editor")?.is_system).toBe(false);
		expect(roles.find((r) => r.name === "Viewer")?.is_system).toBe(false);
	});

	test("Viewer reads and Editor writes, as their policies say", async ({
		request,
		playwright,
	}) => {
		const projectId = await createProject(request, name("FRESH2"));
		const roles = await listProjectRoles(request, projectId);
		const byName = (n: string) => roles.find((r) => r.name === n) as RoleRecord;
		const viewerActions = byName("Viewer").policy.statements.flatMap(
			(s) => s.actions,
		);
		const editorActions = byName("Editor").policy.statements.flatMap(
			(s) => s.actions,
		);

		expect(viewerActions).toContain("tasks:read");
		expect(viewerActions).not.toContain("tasks:write");
		expect(editorActions).toEqual(
			expect.arrayContaining(["tasks:read", "tasks:write"]),
		);
		// Neither hands out roles: that is for the Admin.
		expect(editorActions).not.toContain("roles:assign");
		expect(viewerActions).not.toContain("roles:assign");

		const viewer = await createMember(
			request,
			playwright,
			projectId,
			name("VIEWER_USER"),
			[byName("Viewer").id],
		);
		await asUser(playwright, viewer.username, async (api) => {
			expect((await listTasks(api, projectId)).status).toBe(200);
			expect(
				(
					await api.post(`${API_URL}/projects/${projectId}/tasks`, {
						data: { title: "Not allowed" },
					})
				).status(),
			).toBe(403);
		});
	});
});

// ─── Built-in and default roles ──────────────────────────────────────────────

test.describe("Built-in roles can be edited but not deleted", () => {
	test("The workspace built-ins are system roles that refuse deletion", async ({
		request,
	}) => {
		const roles = await listGlobalRoles(request);
		for (const builtIn of ["SUPER_ADMIN", "ADMIN", "USER"]) {
			const role = roles.find((r) => r.name === builtIn);
			expect(role?.is_system, builtIn).toBe(true);
			const response = await request.delete(
				`${API_URL}/admin/roles/${role?.id}`,
			);
			expect(response.status(), builtIn).toBe(409);
			// USER is also the default; either reason refuses the deletion.
			expect(["ROLE_IS_SYSTEM", "ROLE_IS_DEFAULT"], builtIn).toContain(
				await errorCode(response),
			);
		}
		const superAdmin = roles.find((r) => r.name === "SUPER_ADMIN");
		const admin = roles.find((r) => r.name === "ADMIN");
		expect(
			(
				await request.delete(`${API_URL}/admin/roles/${superAdmin?.id}`)
			).status(),
		).toBe(409);
		expect(
			await errorCode(
				await request.delete(`${API_URL}/admin/roles/${admin?.id}`),
			),
		).toBe("ROLE_IS_SYSTEM");
	});

	test("A workspace built-in can be edited, and the edit is kept", async ({
		request,
	}) => {
		const admin = (await listGlobalRoles(request)).find(
			(r) => r.name === "ADMIN",
		) as RoleRecord;
		const original = admin.description;
		const put = (description: string) =>
			request.put(`${API_URL}/admin/roles/${admin.id}`, {
				data: { name: admin.name, description, policy: admin.policy },
			});
		try {
			const edited = await put("Edited by an e2e test");
			expect(edited.status()).toBe(200);
			const reread = await request.get(`${API_URL}/admin/roles/${admin.id}`);
			expect((await dataOf<RoleRecord>(reread)).description).toBe(
				"Edited by an e2e test",
			);
			expect((await dataOf<RoleRecord>(reread)).is_system).toBe(true);
		} finally {
			expect((await put(original)).status()).toBe(200);
		}
	});

	test("A project's Admin role can be edited but not deleted; Editor and Viewer can be deleted", async ({
		request,
	}) => {
		const projectId = await createProject(request, name("BUILTIN"));
		const roles = await listProjectRoles(request, projectId);
		const admin = roles.find((r) => r.name === "Admin") as RoleRecord;
		const url = (id: string) => `${API_URL}/projects/${projectId}/roles/${id}`;

		const edited = await request.put(url(admin.id), {
			data: {
				name: "Admin",
				description: "Runs the project",
				policy: admin.policy,
			},
		});
		expect(edited.status()).toBe(200);
		expect((await dataOf<RoleRecord>(edited)).description).toBe(
			"Runs the project",
		);

		const refused = await request.delete(url(admin.id));
		expect(refused.status()).toBe(409);
		expect(await errorCode(refused)).toBe("ROLE_IS_SYSTEM");

		for (const custom of ["Editor", "Viewer"]) {
			const role = roles.find((r) => r.name === custom) as RoleRecord;
			expect((await request.delete(url(role.id))).status(), custom).toBe(204);
		}
	});
});

test.describe("The default role", () => {
	test("It cannot be deleted until another role is made the default", async ({
		request,
	}) => {
		const userRole = await globalRoleIdByName(request, "USER");
		const candidate = await createGlobalRole(
			request,
			name("DEFAULT_CANDIDATE"),
			policyOf(allow(["users:read"], ["user", "user/*"])),
		);
		try {
			const promoted = await request.put(
				`${API_URL}/admin/roles/${candidate.id}/default`,
			);
			expect(promoted.status()).toBe(200);
			expect((await dataOf<RoleRecord>(promoted)).is_default).toBe(true);

			const refused = await request.delete(
				`${API_URL}/admin/roles/${candidate.id}`,
			);
			expect(refused.status()).toBe(409);
			expect(await errorCode(refused)).toBe("ROLE_IS_DEFAULT");
			expect(
				(await listGlobalRoles(request)).filter((r) => r.is_default),
			).toHaveLength(1);
		} finally {
			// Hand the mark back so every other spec finds USER as the default.
			const restored = await request.put(
				`${API_URL}/admin/roles/${userRole}/default`,
			);
			expect(restored.status()).toBe(200);
		}
		expect(
			(await request.delete(`${API_URL}/admin/roles/${candidate.id}`)).status(),
		).toBe(204);
	});
});

// ─── Last full access ────────────────────────────────────────────────────────

test.describe("The last platform-wide full-access holder stays", () => {
	test("The only full-access account cannot be stripped of its role", async ({
		request,
	}) => {
		// Holders of an unconditional `*` on `*`, counted from the stored roles.
		const roles = await listGlobalRoles(request);
		const fullAccess = new Set(
			roles
				.filter((r) =>
					r.policy.statements.some(
						(s) =>
							s.effect === "Allow" &&
							!s.conditions &&
							s.actions.includes("*") &&
							s.resources.includes("*"),
					),
				)
				.map((r) => r.id),
		);
		const me = (
			await dataOf<{ id: string }>(await request.get(`${API_URL}/users/me`))
		).id;
		const holders: string[] = [];
		for (let page = 1; ; page++) {
			const response = await request.get(
				`${API_URL}/admin/users?page=${page}&page_size=100`,
			);
			expect(response.ok()).toBeTruthy();
			const data = await dataOf<{
				items: Array<{ id: string; roles: Array<{ id: string }> }>;
				total: number;
				page_size: number;
			}>(response);
			for (const user of data.items) {
				if (user.roles.some((r) => fullAccess.has(r.id))) holders.push(user.id);
			}
			if (page * data.page_size >= data.total) break;
		}
		// The rule only bites when this account is the single holder; stripping
		// anything else would succeed and lock the shared test account out.
		test.skip(
			holders.length !== 1 || holders[0] !== me,
			"another account holds full access, so the rule does not apply",
		);

		const stripped = await putUserRoles(request, me, []);
		expect(stripped.status()).toBe(409);
		expect(await errorCode(stripped)).toBe("ROLE_LAST_FULL_ACCESS");

		// Editing the role itself so that it no longer grants `*` is refused too.
		const superAdmin = roles.find(
			(r) => r.name === "SUPER_ADMIN",
		) as RoleRecord;
		const narrowed = await request.put(
			`${API_URL}/admin/roles/${superAdmin.id}`,
			{
				data: {
					name: superAdmin.name,
					description: superAdmin.description,
					policy: policyOf(allow(["users:read"], ["user", "user/*"])),
				},
			},
		);
		expect(narrowed.status()).toBe(409);
		expect(await errorCode(narrowed)).toBe("ROLE_LAST_FULL_ACCESS");

		// Still the holder.
		const stillAdmin = await request.get(`${API_URL}/admin/roles`);
		expect(stillAdmin.status()).toBe(200);
	});
});

// ─── Role names ──────────────────────────────────────────────────────────────

test.describe("Role names", () => {
	test("A name is trimmed and must be 1 to 100 characters", async ({
		request,
	}) => {
		const policy = policyOf();
		for (const [label, value] of [
			["empty", ""],
			["blank", "   "],
			["too long", "x".repeat(101)],
		] as const) {
			const response = await postGlobalRole(request, value, policy);
			expect(response.status(), label).toBe(400);
			expect(await errorCode(response), label).toBe("ROLE_NAME_INVALID");
		}

		const padded = await postGlobalRole(
			request,
			`  ${name("PADDED")}  `,
			policy,
		);
		expect(padded.status()).toBe(201);
		expect((await dataOf<RoleRecord>(padded)).name).toBe(name("PADDED"));

		// 100 characters is the longest allowed.
		const longest = `${PREFIX}${"L".repeat(100 - PREFIX.length)}`;
		expect(longest).toHaveLength(100);
		expect((await postGlobalRole(request, longest, policy)).status()).toBe(201);
	});

	test("A name is unique within its scope: workspace-wide and per project", async ({
		request,
	}) => {
		const policy = policyOf();
		const first = await createProject(request, name("NAMES_A"));
		const second = await createProject(request, name("NAMES_B"));

		const workspace = name("UNIQUE_WS");
		expect((await postGlobalRole(request, workspace, policy)).status()).toBe(
			201,
		);
		const dup = await postGlobalRole(request, workspace, policy);
		expect(dup.status()).toBe(409);
		expect(await errorCode(dup)).toBe("ROLE_NAME_TAKEN");

		const projectName = name("UNIQUE_PROJECT");
		expect(
			(await postProjectRole(request, first, projectName, policy)).status(),
		).toBe(201);
		const projectDup = await postProjectRole(
			request,
			first,
			projectName,
			policy,
		);
		expect(projectDup.status()).toBe(409);
		expect(await errorCode(projectDup)).toBe("ROLE_NAME_TAKEN");

		// The same name is free in another project, and as a workspace role.
		expect(
			(await postProjectRole(request, second, projectName, policy)).status(),
		).toBe(201);
		expect((await postGlobalRole(request, projectName, policy)).status()).toBe(
			201,
		);

		// Every project already has an "Editor": it is taken there.
		const editorDup = await postProjectRole(request, first, "Editor", policy);
		expect(editorDup.status()).toBe(409);
	});

	test("A role is renamed over an existing name only when the name is free", async ({
		request,
	}) => {
		const policy = policyOf();
		const a = await createGlobalRole(request, name("RENAME_A"), policy);
		await createGlobalRole(request, name("RENAME_B"), policy);
		const rename = (to: string) =>
			request.put(`${API_URL}/admin/roles/${a.id}`, {
				data: { name: to, description: "", policy },
			});
		const taken = await rename(name("RENAME_B"));
		expect(taken.status()).toBe(409);
		expect(await errorCode(taken)).toBe("ROLE_NAME_TAKEN");
		// Keeping its own name is not a clash.
		expect((await rename(name("RENAME_A"))).status()).toBe(200);
		expect((await rename(name("RENAME_C"))).status()).toBe(200);
	});

	test("A permission map is not a policy", async ({ request }) => {
		const response = await request.post(`${API_URL}/admin/roles`, {
			data: { name: name("MAP"), permissions: { "tasks.read": true } },
		});
		expect(response.status()).toBe(422);
	});
});

// ─── Policy validation ───────────────────────────────────────────────────────

test.describe("A policy is validated when a role is saved", () => {
	test("An unknown action is reported at its path", async ({ request }) => {
		const response = await postGlobalRole(
			request,
			name("BAD_ACTION"),
			policyOf(allow(["tasks:reed"], ["project/*"])),
		);
		expect(response.status()).toBe(422);
		expect(await errorCode(response)).toBe("ROLE_POLICY_INVALID");
		const issues = await policyIssues(response);
		expect(issues).toEqual([
			expect.objectContaining({ path: "statements[0].actions[0]" }),
		]);
		expect(issues[0].message).toContain("tasks:reed");
	});

	test("Bad resources, effects and operators are reported", async ({
		request,
	}) => {
		const bad: Record<string, { policy: unknown; path: string }> = {
			"unknown root": {
				policy: policyOf(allow(["tasks:read"], ["nothing/here"])),
				path: "statements[0].resources[0]",
			},
			"empty segment": {
				policy: policyOf(allow(["tasks:read"], ["project//task"])),
				path: "statements[0].resources[0]",
			},
			"unknown operator": {
				policy: policyOf(
					allow(["tasks:read"], ["project/*/task/*"], {
						Sometimes: { "task.sprint_id": "x" },
					}),
				),
				path: "statements[0].conditions.Sometimes.task.sprint_id",
			},
		};
		for (const [label, { policy, path }] of Object.entries(bad)) {
			const response = await postGlobalRole(
				request,
				name(`BAD_${label.replace(/\s/g, "_").toUpperCase()}`),
				policy as RolePolicy,
			);
			expect(response.status(), label).toBe(422);
			expect(
				(await policyIssues(response)).map((i) => i.path),
				label,
			).toContain(path);
		}
	});

	test("A condition key tied to another kind of resource is refused", async ({
		request,
	}) => {
		const response = await postGlobalRole(
			request,
			name("BAD_KEY"),
			policyOf(
				allow(["docs:read"], ["project/*/doc/*"], {
					StringEquals: { "task.sprint_id": "x" },
				}),
			),
		);
		expect(response.status()).toBe(422);
		expect(await errorCode(response)).toBe("ROLE_POLICY_INVALID");
	});
});

// ─── Escalation guard ────────────────────────────────────────────────────────

test.describe("No privilege escalation when defining roles", () => {
	test("A workspace role can only be saved when its author holds what it grants", async ({
		request,
		playwright,
	}) => {
		const author = name("AUTHOR");
		const authorRole = await createGlobalRole(
			request,
			name("AUTHOR_ROLE"),
			policyOf(
				allow(["roles:read", "roles:write"], ["role", "role/*"]),
				allow(["users:read"], ["user", "user/*"]),
			),
		);
		await createUserWithPassword(request, playwright, {
			username: author,
			fullName: author,
			roles: [authorRole.name],
		});

		await asUser(playwright, author, async (api) => {
			const create = (label: string, policy: RolePolicy) =>
				postGlobalRole(api, name(label), policy);

			// What they hold themselves: fine.
			const held = await create(
				"HELD",
				policyOf(allow(["users:read"], ["user", "user/*"])),
			);
			expect(held.status()).toBe(201);

			// What they do not hold: refused, whether it is one action or everything.
			for (const [label, policy] of [
				["MORE_ACTIONS", policyOf(allow(["users:write"], ["user", "user/*"]))],
				["EVERYTHING", policyOf(allow(["*"], ["*"]))],
				[
					"ROOT_EQUIVALENT",
					policyOf(allow(["roles:assign"], ["role", "role/*"])),
				],
				["MORE_RESOURCES", policyOf(allow(["users:read"], ["*"]))],
			] as const) {
				const refused = await create(label, policy);
				expect(refused.status(), label).toBe(403);
				expect(await errorCode(refused), label).toBe("FORBIDDEN");
			}

			// A Deny can always be granted: it only takes access away.
			const denyOnly = await create(
				"DENY_ONLY",
				policyOf(deny(["users:write", "settings:write"], ["*"])),
			);
			expect(denyOnly.status()).toBe(201);

			// Updating is guarded the same way as creating.
			const roleId = (await dataOf<RoleRecord>(held)).id;
			const update = (policy: RolePolicy) =>
				api.put(`${API_URL}/admin/roles/${roleId}`, {
					data: { name: name("HELD"), description: "", policy },
				});
			expect(
				(
					await update(policyOf(allow(["users:write"], ["user", "user/*"])))
				).status(),
			).toBe(403);
			expect(
				(
					await update(policyOf(allow(["users:read"], ["user", "user/*"])))
				).status(),
			).toBe(200);
		});
	});

	test("A Deny of the author overlapping the grant makes it ungrantable", async ({
		request,
		playwright,
	}) => {
		const author = name("DENYING_AUTHOR");
		const blocked = "00000000-0000-4000-8000-0000000000aa";
		const authorRole = await createGlobalRole(
			request,
			name("DENYING_AUTHOR_ROLE"),
			policyOf(
				allow(["roles:read", "roles:write"], ["role", "role/*"]),
				allow(["users:read"], ["user", "user/*"]),
				deny(["users:read"], [`user/${blocked}`]),
			),
		);
		await createUserWithPassword(request, playwright, {
			username: author,
			fullName: author,
			roles: [authorRole.name],
		});

		await asUser(playwright, author, async (api) => {
			// user/* covers the denied user, so the author does not really hold it.
			const overlapping = await postGlobalRole(
				api,
				name("OVERLAPPING"),
				policyOf(allow(["users:read"], ["user", "user/*"])),
			);
			expect(overlapping.status()).toBe(403);
			// A resource their Deny does not touch is theirs to grant.
			const disjoint = await postGlobalRole(
				api,
				name("DISJOINT"),
				policyOf(
					allow(["users:read"], ["user/00000000-0000-4000-8000-0000000000bb"]),
				),
			);
			expect(disjoint.status()).toBe(201);
		});
	});

	test("Inside a project the same guard applies to project roles", async ({
		request,
		playwright,
	}) => {
		const projectId = await createProject(request, name("GUARD"));
		const sprintId = "00000000-0000-4000-8000-0000000000cc";
		const manager = await createMember(
			request,
			playwright,
			projectId,
			name("ROLE_MANAGER"),
			[
				await projectRoleFrom(
					request,
					projectId,
					name("ROLE_MANAGER_ROLE"),
					allow(
						["projects:read", "roles:read", "roles:write"],
						[`project/${projectId}`, `project/${projectId}/role/*`],
					),
					allow(["tasks:read"], [`project/${projectId}/*`]),
					allow(["docs:read"], [`project/${projectId}/doc/*`], {
						StringEquals: { "doc.folder_id": sprintId },
					}),
				),
			],
		);

		await asUser(playwright, manager.username, async (api) => {
			const create = (label: string, policy: RolePolicy) =>
				postProjectRole(api, projectId, name(label), policy);

			expect(
				(
					await create(
						"OK_READ",
						policyOf(allow(["tasks:read"], [`project/${projectId}/*`])),
					)
				).status(),
			).toBe(201);
			// Not held: tasks:write, or reading everything instead of tasks only.
			for (const [label, policy] of [
				[
					"NO_WRITE",
					policyOf(allow(["tasks:write"], [`project/${projectId}/*`])),
				],
				[
					"NO_AGENTS",
					policyOf(allow(["agents:read"], [`project/${projectId}/*`])),
				],
			] as const) {
				expect((await create(label, policy)).status(), label).toBe(403);
			}
			// A conditional Allow of their own does not cover an unconditional grant.
			expect(
				(
					await create(
						"CONDITIONAL",
						policyOf(allow(["docs:read"], [`project/${projectId}/doc/*`])),
					)
				).status(),
			).toBe(403);
			// A Deny is always fine.
			expect(
				(
					await create(
						"DENY_OK",
						policyOf(deny(["tasks:write"], [`project/${projectId}/*`])),
					)
				).status(),
			).toBe(201);
		});

		// The project Admin holds everything in the project, so anything within it goes.
		const admin = await listProjectRoles(request, projectId);
		const adminRoleId = admin.find((r) => r.name === "Admin")?.id ?? "";
		const root = await createMember(
			request,
			playwright,
			projectId,
			name("ROOT_MANAGER"),
			[adminRoleId],
		);
		await asUser(playwright, root.username, async (api) => {
			const response = await postProjectRole(
				api,
				projectId,
				name("ADMIN_MADE"),
				policyOf(
					allow(["tasks:write", "agents:write"], [`project/${projectId}/*`]),
				),
			);
			expect(response.status()).toBe(201);
		});
	});
});
