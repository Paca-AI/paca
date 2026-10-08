// spec: features/iam/roles-assign.feature
// seed: tests/seed.spec.ts
//
// Assigning roles is its own privilege, `roles:assign`, and works like AWS IAM
// `iam:PassRole`: the resource of the statement says WHICH roles its holder may
// attach or detach, and every role a request adds or removes is judged on its
// own resource (`project/<P>/role/<id>` inside a project, `role/<id>`
// platform-wide). Holding the permissions of a role is not needed to assign it,
// and roles that stay as they are need no permission.
//
// The rules are asserted over the API; the Team page is checked in the browser
// where the scenario is about what the UI offers.

import {
	type APIRequestContext,
	expect,
	type Locator,
	type Page,
	test,
} from "@playwright/test";
import {
	API_URL,
	authRequest,
	BASE_URL,
	cleanupGlobalAgentsByPrefix,
	cleanupGlobalRolesByPrefix,
	cleanupProjectsByPrefix,
	cleanupUsersByPrefix,
	createGlobalAgent,
	createGlobalRole,
	createProject,
	createUserWithPassword,
	globalRoleIdByName,
	newRunId,
	projectRoleIdByName,
	RESTRICTED_PASSWORD,
	signIn,
} from "../helpers/e2e-api";
import {
	allow,
	asUser,
	createMember,
	createPerson,
	errorCode,
	memberRoleNames,
	policyOf,
	projectRoleFrom,
	putMemberRoles,
	putUserRoles,
} from "../helpers/iam";
import { roleOptionIn } from "../helpers/role-select";

const PREFIX = "E2E_IAMASSIGN_";
const RUN_ID = newRunId();
const name = (label: string) => `${PREFIX}${label}_${RUN_ID}`;

async function cleanup(request: APIRequestContext) {
	await cleanupProjectsByPrefix(request, PREFIX);
	await cleanupUsersByPrefix(request, PREFIX);
	await cleanupGlobalAgentsByPrefix(request, PREFIX);
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

/** What an assigner in a project needs to see the team and the list of roles. */
function seeTeam(projectId: string) {
	return allow(
		["projects:read", "project.members:read", "roles:read"],
		[`project/${projectId}`, `project/${projectId}/role/*`],
	);
}

const assignIn = (projectId: string, roleId: string) =>
	allow(["roles:assign"], [`project/${projectId}/role/${roleId}`]);

// ─── Inside a project ────────────────────────────────────────────────────────

test.describe("Assigning roles inside a project", () => {
	let projectId: string;
	let editor: string;
	let viewer: string;
	let adminRole: string;
	let roleA: string;
	let roleB: string;
	let bob: Awaited<ReturnType<typeof createMember>>;

	test.beforeEach(async ({ request, playwright }) => {
		projectId = await createProject(request, name("PROJECT"));
		editor = await projectRoleIdByName(request, projectId, "Editor");
		viewer = await projectRoleIdByName(request, projectId, "Viewer");
		adminRole = await projectRoleIdByName(request, projectId, "Admin");
		roleA = await projectRoleFrom(
			request,
			projectId,
			name("ASSIGNABLE_A"),
			allow(["tasks:read"], [`project/${projectId}/*`]),
		);
		roleB = await projectRoleFrom(
			request,
			projectId,
			name("ASSIGNABLE_B"),
			allow(["tasks:write"], [`project/${projectId}/*`]),
		);
		bob = await createMember(request, playwright, projectId, name("BOB"), [
			viewer,
		]);
	});

	test("roles:assign alone, without project.members:write, changes a member's roles", async ({
		request,
		playwright,
	}) => {
		const assigner = await createMember(
			request,
			playwright,
			projectId,
			name("ASSIGNER"),
			[
				await projectRoleFrom(
					request,
					projectId,
					name("ASSIGNER_ROLE"),
					seeTeam(projectId),
					allow(["roles:assign"], [`project/${projectId}/role/*`]),
				),
			],
		);

		await asUser(playwright, assigner.username, async (api) => {
			const changed = await putMemberRoles(api, projectId, bob.memberId, [
				editor,
			]);
			expect(changed.status()).toBe(200);
			// Managing members is a different permission: removing is refused.
			const removal = await api.delete(
				`${API_URL}/projects/${projectId}/members/${bob.memberId}`,
			);
			expect(removal.status()).toBe(403);
		});
		expect(await memberRoleNames(request, projectId, bob.memberId)).toEqual([
			"Editor",
		]);
	});

	test("project.members:write alone assigns nothing; adding a member needs both permissions", async ({
		request,
		playwright,
	}) => {
		const memberWith = (
			label: string,
			...statements: ReturnType<typeof allow>[]
		) =>
			projectRoleFrom(
				request,
				projectId,
				name(`${label}_ROLE`),
				seeTeam(projectId),
				...statements,
			).then((roleId) =>
				createMember(request, playwright, projectId, name(label), [roleId]),
			);
		const manager = await memberWith(
			"MEMBERS_ONLY",
			allow(["project.members:write"], [`project/${projectId}`]),
		);
		const assigner = await memberWith(
			"ASSIGN_ONLY",
			allow(["roles:assign"], [`project/${projectId}/role/*`]),
		);
		const both = await memberWith(
			"BOTH_PERMS",
			allow(["project.members:write"], [`project/${projectId}`]),
			allow(["roles:assign"], [`project/${projectId}/role/*`]),
		);
		const newcomer = await createPerson(request, playwright, name("NEWCOMER"));
		const add = (api: APIRequestContext) =>
			api.post(`${API_URL}/projects/${projectId}/members`, {
				data: { user_id: newcomer.userId, role_ids: [viewer] },
			});

		await asUser(playwright, manager.username, async (api) => {
			expect(
				(await putMemberRoles(api, projectId, bob.memberId, [editor])).status(),
			).toBe(403);
			expect((await add(api)).status()).toBe(403);
		});
		await asUser(playwright, assigner.username, async (api) => {
			expect((await add(api)).status()).toBe(403);
		});
		await asUser(playwright, both.username, async (api) => {
			expect((await add(api)).status()).toBe(201);
		});
		expect(await memberRoleNames(request, projectId, bob.memberId)).toEqual([
			"Viewer",
		]);
	});

	test("A new member needs at least one role", async ({
		request,
		playwright,
	}) => {
		const newcomer = await createPerson(request, playwright, name("NOROLE"));
		const response = await request.post(
			`${API_URL}/projects/${projectId}/members`,
			{ data: { user_id: newcomer.userId, role_ids: [] } },
		);
		expect(response.status()).toBe(400);
		expect(await errorCode(response)).toBe("ROLE_REQUIRED");
	});

	test("Without roles:assign, a member cannot change anyone's roles", async ({
		request,
		playwright,
	}) => {
		const plain = await createMember(
			request,
			playwright,
			projectId,
			name("PLAIN"),
			[editor],
		);
		await asUser(playwright, plain.username, async (api) => {
			const response = await putMemberRoles(api, projectId, bob.memberId, [
				editor,
			]);
			expect(response.status()).toBe(403);
			expect(await errorCode(response)).toBe("FORBIDDEN");
		});
		expect(await memberRoleNames(request, projectId, bob.memberId)).toEqual([
			"Viewer",
		]);
	});

	test("A scoped roles:assign assigns exactly the roles it names, and removal is judged too", async ({
		request,
		playwright,
	}) => {
		const lead = await createMember(
			request,
			playwright,
			projectId,
			name("LEAD"),
			[
				await projectRoleFrom(
					request,
					projectId,
					name("LEAD_ROLE"),
					seeTeam(projectId),
					allow(["project.members:write"], [`project/${projectId}`]),
					assignIn(projectId, roleA),
				),
			],
		);
		const heldBy = () => memberRoleNames(request, projectId, bob.memberId);
		const nameOf = (...labels: string[]) => labels.sort();

		await asUser(playwright, lead.username, async (api) => {
			// Viewer stays as it is (free); adding A is the only change, and A is theirs.
			expect(
				(
					await putMemberRoles(api, projectId, bob.memberId, [viewer, roleA])
				).status(),
			).toBe(200);
			expect(await heldBy()).toEqual(nameOf("Viewer", name("ASSIGNABLE_A")));

			// B, alone or next to A, and the project Admin role are not theirs.
			for (const ids of [
				[viewer, roleA, roleB],
				[viewer, roleB],
				[viewer, roleA, adminRole],
			]) {
				const refused = await putMemberRoles(api, projectId, bob.memberId, ids);
				expect(refused.status(), JSON.stringify(ids)).toBe(403);
				expect(await errorCode(refused)).toBe("FORBIDDEN");
			}
			// A refused assignment applies nothing.
			expect(await heldBy()).toEqual(nameOf("Viewer", name("ASSIGNABLE_A")));
		});

		// The admin attaches B too. The lead can neither take B away nor wipe the set.
		await putMemberRoles(request, projectId, bob.memberId, [
			viewer,
			roleA,
			roleB,
		]);
		await asUser(playwright, lead.username, async (api) => {
			for (const ids of [[viewer, roleA], []]) {
				expect(
					(await putMemberRoles(api, projectId, bob.memberId, ids)).status(),
					JSON.stringify(ids),
				).toBe(403);
			}
			// Taking A away while B stays only changes A, which is theirs.
			expect(
				(
					await putMemberRoles(api, projectId, bob.memberId, [viewer, roleB])
				).status(),
			).toBe(200);
		});
		expect(await heldBy()).toEqual(nameOf("Viewer", name("ASSIGNABLE_B")));
	});

	test("The project Admin can assign any role inside its project, but nothing platform-wide", async ({
		request,
		playwright,
	}) => {
		const workspaceRole = await createGlobalRole(
			request,
			name("WORKSPACE_ROLE"),
			policyOf(allow(["users:read"], ["user", "user/*"])),
		);
		const admin = await createMember(
			request,
			playwright,
			projectId,
			name("PADMIN"),
			[adminRole],
		);

		await asUser(playwright, admin.username, async (api) => {
			// Any of the project's roles, and a workspace role attached inside it.
			for (const ids of [
				[editor],
				[adminRole],
				[roleA, roleB],
				[workspaceRole.id],
			]) {
				expect(
					(await putMemberRoles(api, projectId, bob.memberId, ids)).status(),
					JSON.stringify(ids),
				).toBe(200);
			}
			// The same role platform-wide is a different resource: role/<id>.
			expect(
				(await putUserRoles(api, bob.userId, [workspaceRole.id])).status(),
			).toBe(403);
		});
	});

	test("A project Admin can assign a richer role than it could grant by hand, yet it stays inside the project", async ({
		request,
		playwright,
	}) => {
		const everything = await createGlobalRole(
			request,
			name("EVERYTHING"),
			policyOf(allow(["*"], ["*"])),
		);
		const admin = await createMember(
			request,
			playwright,
			projectId,
			name("PADMIN2"),
			[adminRole],
		);

		await asUser(playwright, admin.username, async (api) => {
			expect(
				(
					await putMemberRoles(api, projectId, bob.memberId, [everything.id])
				).status(),
			).toBe(200);
		});

		// A project-scoped attachment is intersected with the project, so the
		// workspace-wide `*` gives bob everything inside it and nothing outside.
		await asUser(playwright, bob.username, async (api) => {
			expect(
				(
					await api.post(`${API_URL}/projects/${projectId}/docs`, {
						data: { title: "Allowed" },
					})
				).status(),
			).toBe(201);
			expect((await api.get(`${API_URL}/admin/roles`)).status()).toBe(403);
			expect(
				(await api.get(`${API_URL}/projects/${projectId}/sprints`)).status(),
			).toBe(200);
		});
	});

	test("Unknown role ids and roles of another project cannot be attached", async ({
		request,
	}) => {
		const other = await createProject(request, name("OTHER"));
		const foreign = await projectRoleIdByName(request, other, "Editor");

		const unknown = await putMemberRoles(request, projectId, bob.memberId, [
			"00000000-0000-4000-8000-000000000000",
		]);
		expect(unknown.status()).toBe(422);
		expect(await errorCode(unknown)).toBe("ROLE_NOT_ATTACHABLE");

		const crossProject = await putMemberRoles(
			request,
			projectId,
			bob.memberId,
			[foreign],
		);
		expect(crossProject.status()).toBe(422);
		expect(await errorCode(crossProject)).toBe("ROLE_NOT_ATTACHABLE");
		expect(await memberRoleNames(request, projectId, bob.memberId)).toEqual([
			"Viewer",
		]);
	});
});

// ─── Platform-wide ───────────────────────────────────────────────────────────

test.describe("Assigning workspace roles to users and agents", () => {
	test("roles:assign on role/* assigns any workspace role to a user", async ({
		request,
		playwright,
	}) => {
		const target = await createPerson(request, playwright, name("TARGET"));
		const wanted = await createGlobalRole(
			request,
			name("WANTED"),
			policyOf(allow(["users:read"], ["user", "user/*"])),
		);
		const support = name("SUPPORT");
		const supportRole = await createGlobalRole(
			request,
			name("SUPPORT_ROLE"),
			policyOf(allow(["roles:assign"], ["role", "role/*"])),
		);
		await createUserWithPassword(request, playwright, {
			username: support,
			fullName: support,
			roles: [supportRole.name],
		});
		const userRole = await globalRoleIdByName(request, "USER");

		await asUser(playwright, support, async (api) => {
			const response = await putUserRoles(api, target.userId, [
				userRole,
				wanted.id,
			]);
			expect(response.status()).toBe(200);
			const roles: Array<{ name: string }> = (await response.json()).data;
			expect(roles.map((r) => r.name).sort()).toEqual(
				["USER", wanted.name].sort(),
			);
		});
	});

	test("Without roles:assign a user cannot assign workspace roles", async ({
		request,
		playwright,
	}) => {
		const target = await createPerson(request, playwright, name("TARGET2"));
		const wanted = await createGlobalRole(
			request,
			name("WANTED2"),
			policyOf(allow(["users:read"], ["user", "user/*"])),
		);
		const userRole = await globalRoleIdByName(request, "USER");
		const reader = name("READER");
		const readerRole = await createGlobalRole(
			request,
			name("READER_ROLE"),
			policyOf(
				allow(
					["users:read", "users:write", "roles:read"],
					["user", "user/*", "role", "role/*"],
				),
			),
		);
		await createUserWithPassword(request, playwright, {
			username: reader,
			fullName: reader,
			roles: [readerRole.name],
		});

		await asUser(playwright, reader, async (api) => {
			const response = await putUserRoles(api, target.userId, [
				userRole,
				wanted.id,
			]);
			expect(response.status()).toBe(403);
			expect(await errorCode(response)).toBe("FORBIDDEN");
		});
	});

	test("A scoped role/<id> assigns that role only, and cannot remove another", async ({
		request,
		playwright,
	}) => {
		const target = await createPerson(request, playwright, name("TARGET3"));
		const roleX = await createGlobalRole(
			request,
			name("ROLE_X"),
			policyOf(allow(["users:read"], ["user", "user/*"])),
		);
		const roleY = await createGlobalRole(
			request,
			name("ROLE_Y"),
			policyOf(allow(["plugins:read"], ["plugin", "plugin/*"])),
		);
		const userRole = await globalRoleIdByName(request, "USER");
		const onlyX = name("ONLY_X");
		const onlyXRole = await createGlobalRole(
			request,
			name("ONLY_X_ROLE"),
			policyOf(allow(["roles:assign"], [`role/${roleX.id}`])),
		);
		await createUserWithPassword(request, playwright, {
			username: onlyX,
			fullName: onlyX,
			roles: [onlyXRole.name],
		});

		await asUser(playwright, onlyX, async (api) => {
			expect(
				(await putUserRoles(api, target.userId, [userRole, roleX.id])).status(),
			).toBe(200);
			expect(
				(
					await putUserRoles(api, target.userId, [userRole, roleX.id, roleY.id])
				).status(),
			).toBe(403);
		});

		// Y is attached by the admin; the scoped assigner cannot take it away,
		// but may take X away while Y stays.
		await putUserRoles(request, target.userId, [userRole, roleX.id, roleY.id]);
		await asUser(playwright, onlyX, async (api) => {
			expect(
				(await putUserRoles(api, target.userId, [userRole, roleX.id])).status(),
			).toBe(403);
			expect(
				(await putUserRoles(api, target.userId, [userRole, roleY.id])).status(),
			).toBe(200);
		});
	});

	test("A workspace-wide role/* does not reach inside a project", async ({
		request,
		playwright,
	}) => {
		const projectId = await createProject(request, name("INSIDE"));
		const viewer = await projectRoleIdByName(request, projectId, "Viewer");
		const editor = await projectRoleIdByName(request, projectId, "Editor");
		const bob = await createMember(
			request,
			playwright,
			projectId,
			name("BOB2"),
			[viewer],
		);
		const support = name("SUPPORT2");
		const supportRole = await createGlobalRole(
			request,
			name("SUPPORT2_ROLE"),
			policyOf(allow(["roles:assign"], ["role", "role/*"])),
		);
		await createUserWithPassword(request, playwright, {
			username: support,
			fullName: support,
			roles: [supportRole.name],
		});

		await asUser(playwright, support, async (api) => {
			expect(
				(await putMemberRoles(api, projectId, bob.memberId, [editor])).status(),
			).toBe(403);
		});
	});

	test("A global agent's roles need agents:write on the agent as well as roles:assign", async ({
		request,
		playwright,
	}) => {
		const agent = await createGlobalAgent(request, name("GLOBAL_BOT"));
		const roleX = await createGlobalRole(
			request,
			name("AGENT_ROLE"),
			policyOf(allow(["users:read"], ["user", "user/*"])),
		);
		const putAgentRoles = (api: APIRequestContext) =>
			api.put(`${API_URL}/admin/agents/${agent.id}/roles`, {
				data: { role_ids: [roleX.id] },
			});
		const userWith = async (
			label: string,
			...statements: ReturnType<typeof allow>[]
		) => {
			const username = name(label);
			const role = await createGlobalRole(
				request,
				name(`${label}_ROLE`),
				policyOf(...statements),
			);
			await createUserWithPassword(request, playwright, {
				username,
				fullName: username,
				roles: [role.name],
			});
			return username;
		};
		const assignOnly = await userWith(
			"AGENT_ASSIGN_ONLY",
			allow(["roles:assign"], ["role", "role/*"]),
		);
		const writeOnly = await userWith(
			"AGENT_WRITE_ONLY",
			allow(["agents:read", "agents:write"], ["agent", "agent/*"]),
		);
		const both = await userWith(
			"AGENT_BOTH",
			allow(["roles:assign"], ["role", "role/*"]),
			allow(["agents:read", "agents:write"], ["agent", "agent/*"]),
		);

		await asUser(playwright, assignOnly, async (api) => {
			expect((await putAgentRoles(api)).status()).toBe(403);
		});
		await asUser(playwright, writeOnly, async (api) => {
			expect((await putAgentRoles(api)).status()).toBe(403);
		});
		await asUser(playwright, both, async (api) => {
			expect((await putAgentRoles(api)).status()).toBe(200);
		});
	});
});

// ─── The Team page ───────────────────────────────────────────────────────────

test.describe("The Team page offers what the viewer may do", () => {
	let projectId: string;
	let viewer: string;
	let roleA: string;
	let bob: Awaited<ReturnType<typeof createMember>>;

	test.beforeEach(async ({ request, playwright }) => {
		projectId = await createProject(request, name("TEAMUI"));
		viewer = await projectRoleIdByName(request, projectId, "Viewer");
		roleA = await projectRoleFrom(
			request,
			projectId,
			name("UI_ROLE_A"),
			allow(["tasks:read"], [`project/${projectId}/*`]),
		);
		await projectRoleFrom(
			request,
			projectId,
			name("UI_ROLE_B"),
			allow(["tasks:write"], [`project/${projectId}/*`]),
		);
		bob = await createMember(request, playwright, projectId, name("UIBOB"), [
			viewer,
		]);
	});

	const teamUrl = () => `${BASE_URL}/projects/${projectId}/team`;
	const rowOf = (page: Page, username: string): Locator =>
		page.getByText(`@${username}`, { exact: true }).locator("../..");

	async function signInTo(page: Page, username: string) {
		await signIn(page, username, RESTRICTED_PASSWORD);
		await page.goto(teamUrl());
		await expect(page.getByRole("heading", { name: "Team" })).toBeVisible();
	}

	test("The role editor is shown only to someone holding roles:assign", async ({
		page,
		request,
		playwright,
	}) => {
		const reader = await createMember(
			request,
			playwright,
			projectId,
			name("UIREADER"),
			[
				await projectRoleFrom(
					request,
					projectId,
					name("UI_READER_ROLE"),
					allow(
						["projects:read", "project.members:read", "roles:read"],
						[`project/${projectId}`, `project/${projectId}/role/*`],
					),
				),
			],
		);
		const assigner = await createMember(
			request,
			playwright,
			projectId,
			name("UIASSIGNER"),
			[
				await projectRoleFrom(
					request,
					projectId,
					name("UI_ASSIGNER_ROLE"),
					seeTeam(projectId),
					allow(["roles:assign"], [`project/${projectId}/role/*`]),
				),
			],
		);

		// A reader sees every member's roles as badges but has no way to change them.
		await signInTo(page, reader.username);
		await expect(rowOf(page, bob.username)).toBeVisible();
		await expect(
			rowOf(page, bob.username).getByText("Viewer", { exact: true }),
		).toBeVisible();
		await expect(page.getByRole("button", { name: "Change role" })).toHaveCount(
			0,
		);

		// Someone holding roles:assign gets the editor on every row.
		await page.context().clearCookies();
		await signInTo(page, assigner.username);
		await expect(
			rowOf(page, bob.username).getByRole("button", { name: "Change role" }),
		).toBeVisible();
	});

	test("Add Member needs project.members:write and roles:assign together", async ({
		page,
		request,
		playwright,
	}) => {
		const withPermissions = async (
			label: string,
			...statements: ReturnType<typeof allow>[]
		) => {
			const roleId = await projectRoleFrom(
				request,
				projectId,
				name(`${label}_ROLE`),
				seeTeam(projectId),
				...statements,
			);
			return createMember(request, playwright, projectId, name(label), [
				roleId,
			]);
		};
		const membersOnly = await withPermissions(
			"UIMEMBERS",
			allow(["project.members:write"], [`project/${projectId}`]),
		);
		const assignOnly = await withPermissions(
			"UIASSIGNONLY",
			allow(["roles:assign"], [`project/${projectId}/role/*`]),
		);
		const both = await withPermissions(
			"UIBOTH",
			allow(["project.members:write"], [`project/${projectId}`]),
			allow(["roles:assign"], [`project/${projectId}/role/*`]),
		);
		const addMember = page.getByRole("button", { name: "Add Member" });

		await signInTo(page, membersOnly.username);
		await expect(rowOf(page, bob.username)).toBeVisible();
		await expect(addMember).toHaveCount(0);

		await page.context().clearCookies();
		await signInTo(page, assignOnly.username);
		await expect(rowOf(page, bob.username)).toBeVisible();
		await expect(addMember).toHaveCount(0);

		await page.context().clearCookies();
		await signInTo(page, both.username);
		await expect(addMember).toBeVisible();
		await addMember.click();
		await expect(
			page.getByRole("dialog", { name: "Add member" }),
		).toBeVisible();
	});

	test("A scoped assigner can pick the role they may give, and is told when a role is not theirs", async ({
		page,
		request,
		playwright,
	}) => {
		// The permissions hint behind the page lists roles:assign when it is
		// allowed on the project or on "some role" (a wildcard) inside it, so
		// the lead's grant also names the project itself; the API still judges
		// every role on its own resource (see the fixme below for a grant that
		// names specific roles only).
		const lead = await createMember(
			request,
			playwright,
			projectId,
			name("UILEAD"),
			[
				await projectRoleFrom(
					request,
					projectId,
					name("UI_LEAD_ROLE"),
					seeTeam(projectId),
					allow(
						["roles:assign"],
						[`project/${projectId}`, `project/${projectId}/role/${roleA}`],
					),
				),
			],
		);
		await signInTo(page, lead.username);

		const row = rowOf(page, bob.username);
		await row.getByRole("button", { name: "Change role" }).click();
		const list = page.getByRole("listbox", { name: "Change role" });
		await expect(list).toBeVisible();

		// Role B is not theirs: the API refuses and the page says so.
		await roleOptionIn(list, name("UI_ROLE_B")).click();
		await expect(
			page.getByRole("alert").filter({
				hasText:
					"You don't have permission to assign or remove one of these roles.",
			}),
		).toBeVisible();
		expect(await memberRoleNames(request, projectId, bob.memberId)).toEqual([
			"Viewer",
		]);

		// Role A is theirs: it is added next to Viewer.
		await roleOptionIn(list, name("UI_ROLE_A")).click();
		await expect
			.poll(() => memberRoleNames(request, projectId, bob.memberId))
			.toEqual(["Viewer", name("UI_ROLE_A")].sort());
	});

	test("The project Admin can give any role of the project from the Team page", async ({
		page,
		request,
	}) => {
		await signIn(page);
		await page.goto(teamUrl());
		const row = rowOf(page, bob.username);
		await row.getByRole("button", { name: "Change role" }).click();
		const list = page.getByRole("listbox", { name: "Change role" });
		await roleOptionIn(list, name("UI_ROLE_B")).click();
		await expect
			.poll(() => memberRoleNames(request, projectId, bob.memberId))
			.toEqual(["Viewer", name("UI_ROLE_B")].sort());
	});
	test("A lead whose roles:assign names specific roles still gets the role editor on the Team page", async ({
		page,
		request,
		playwright,
	}) => {
		const lead = await createMember(
			request,
			playwright,
			projectId,
			name("UIFIXME"),
			[
				await projectRoleFrom(
					request,
					projectId,
					name("UI_FIXME_ROLE"),
					seeTeam(projectId),
					assignIn(projectId, roleA),
				),
			],
		);
		await asUser(playwright, lead.username, async (api) => {
			const response = await api.get(
				`${API_URL}/projects/${projectId}/members/me/permissions`,
			);
			expect((await response.json()).data.actions).toContain("roles:assign");
		});
		await signInTo(page, lead.username);
		await expect(
			rowOf(page, bob.username).getByRole("button", { name: "Change role" }),
		).toBeVisible();
	});
});
