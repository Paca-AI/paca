// spec: features/iam/git-role-templates.feature
// seed: tests/seed.spec.ts

import { type APIRequestContext, expect, test } from "@playwright/test";
import {
	API_URL,
	assignGlobalRoles,
	authRequest,
	cleanupGlobalRolesByPrefix,
	cleanupProjectsByPrefix,
	cleanupUsersByPrefix,
	createGlobalRole,
	createProject,
	newRunId,
	projectRoleIdByName,
} from "../helpers/e2e-api";
import { gitUrl, refused, repoUrl } from "../helpers/git";
import {
	allow,
	asUser,
	createMember,
	createPerson,
	dataOf,
	errorCode,
	policyOf,
} from "../helpers/iam";

const PREFIX = "E2E_GITROLE_";
const RUN_ID = newRunId();
const name = (label: string) => `${PREFIX}${label}_${RUN_ID}`;

interface Role {
	id: string;
	name: string;
	policy: { statements: Array<{ effect: string; actions: string[] }> };
}

/** The Git actions a role's Allow statements grant (wildcards kept as written). */
const gitActionsOf = (role: Role) =>
	role.policy.statements
		.filter((s) => s.effect === "Allow")
		.flatMap((s) => s.actions)
		.filter((a) => a === "*" || a.startsWith("git"));

async function roleNamed(
	request: APIRequestContext,
	projectId: string,
	roleName: string,
): Promise<Role> {
	const id = await projectRoleIdByName(request, projectId, roleName);
	const response = await request.get(
		`${API_URL}/projects/${projectId}/roles/${id}`,
	);
	expect(response.ok()).toBeTruthy();
	return dataOf<Role>(response);
}

test.describe("Git actions in roles", () => {
	let projectId: string;

	test.beforeAll(async ({ request }) => {
		await authRequest(request);
		await cleanupProjectsByPrefix(request, PREFIX);
		await cleanupUsersByPrefix(request, PREFIX);
		await cleanupGlobalRolesByPrefix(request, PREFIX);
	});

	test.beforeEach(async ({ request }) => {
		await authRequest(request);
		projectId = await createProject(request, name("PROJECT"));
	});

	test.afterAll(async ({ request }) => {
		await authRequest(request);
		await cleanupProjectsByPrefix(request, PREFIX);
		await cleanupUsersByPrefix(request, PREFIX);
		await cleanupGlobalRolesByPrefix(request, PREFIX);
	});

	test("Editor reads and writes Git but cannot approve or merge", async ({
		request,
	}) => {
		const actions = gitActionsOf(await roleNamed(request, projectId, "Editor"));
		expect(actions).toEqual(expect.arrayContaining(["git:read", "git:write"]));
		expect(actions).not.toContain("git:approve");
		expect(actions).not.toContain("git:merge");
		expect(actions).not.toContain("git:*");
	});

	test("Viewer has no Git action", async ({ request }) => {
		expect(gitActionsOf(await roleNamed(request, projectId, "Viewer"))).toEqual(
			[],
		);
	});

	test("Admin covers every Git action", async ({ request }) => {
		const actions = gitActionsOf(await roleNamed(request, projectId, "Admin"));
		const covers = (a: string) =>
			actions.includes("*") || actions.includes("git:*") || actions.includes(a);
		for (const a of ["git:read", "git:write", "git:approve", "git:merge"]) {
			expect(covers(a), a).toBe(true);
		}
	});

	for (const action of [
		"git:read",
		"git:write",
		"git:approve",
		"git:merge",
		"git:*",
	]) {
		test(`A custom project role may hold ${action}`, async ({ request }) => {
			const response = await request.post(
				`${API_URL}/projects/${projectId}/roles`,
				{
					data: {
						name: name(`CUSTOM_${action.replace(/\W/g, "")}`),
						description: "",
						policy: policyOf(allow([action], [`project/${projectId}/*`])),
					},
				},
			);
			expect(response.status()).toBe(201);
			expect(gitActionsOf(await dataOf<Role>(response))).toEqual([action]);
		});
	}

	test("A workspace role may hold git.identities:manage on its own resource", async ({
		request,
	}) => {
		const role = await createGlobalRole(
			request,
			name("WS_OK"),
			policyOf(allow(["git.identities:manage"], ["git_identity/*"])),
		);
		expect(role.name).toBe(name("WS_OK"));
	});

	test("The workspace action is refused on an unknown resource root or action", async ({
		request,
	}) => {
		const unknownRoot = await request.post(`${API_URL}/admin/roles`, {
			data: {
				name: name("WS_BAD"),
				description: "",
				policy: policyOf(allow(["git.identities:manage"], ["bogus/*"])),
			},
		});
		expect(unknownRoot.status()).toBe(422);
		expect(await errorCode(unknownRoot)).toBe("ROLE_POLICY_INVALID");

		const unknownAction = await request.post(`${API_URL}/admin/roles`, {
			data: {
				name: name("WS_BAD2"),
				description: "",
				policy: policyOf(allow(["git.identities:nope"], ["git_identity/*"])),
			},
		});
		expect(unknownAction.status()).toBe(422);
	});

	test("The workspace action on a project resource is accepted but grants nothing", async ({
		request,
		playwright,
	}) => {
		const roleName = name("WS_ON_PROJECT");
		await createGlobalRole(
			request,
			roleName,
			policyOf(allow(["git.identities:manage"], [`project/${projectId}/*`])),
		);
		const person = await createPerson(request, playwright, name("WS_INERT"));
		await assignGlobalRoles(request, person.userId, [roleName]);
		await asUser(playwright, person.username, async (api) => {
			expect((await api.get(`${API_URL}/git/identities`)).status()).toBe(403);
			expect((await api.get(`${API_URL}/git/providers`)).status()).toBe(403);
		});
	});

	test("A project role naming the workspace action never reaches the workspace routes", async ({
		request,
		playwright,
	}) => {
		const response = await request.post(
			`${API_URL}/projects/${projectId}/roles`,
			{
				data: {
					name: name("PROJ_WS"),
					description: "",
					policy: policyOf(
						allow(["git.identities:manage"], [`project/${projectId}/*`]),
					),
				},
			},
		);
		// Accepted (the validator checks the root and the action name only) but
		// inert: the workspace routes are checked on git_identity/*.
		expect(response.status()).toBe(201);
		const role = await dataOf<Role>(response);
		const member = await createMember(
			request,
			playwright,
			projectId,
			name("PROJ_WS_MEMBER"),
			[role.id],
		);
		await asUser(playwright, member.username, async (api) => {
			expect((await api.get(`${API_URL}/git/identities`)).status()).toBe(403);
		});
	});

	test("The built-in Editor role works end to end", async ({
		request,
		playwright,
	}) => {
		const editorId = await projectRoleIdByName(request, projectId, "Editor");
		const member = await createMember(
			request,
			playwright,
			projectId,
			name("EDITOR"),
			[editorId],
		);
		await asUser(playwright, member.username, async (api) => {
			expect((await api.get(gitUrl(projectId, "/repositories"))).status()).toBe(
				200,
			);
			expect((await api.get(gitUrl(projectId, "/settings"))).status()).toBe(
				200,
			);
			expect(
				(
					await api.put(gitUrl(projectId, "/settings"), {
						data: { paused: true },
					})
				).status(),
			).toBe(200);
			expect(
				refused(
					await api.post(repoUrl(projectId, "/change-requests/1/approvals"), {
						data: {},
					}),
				),
			).toBe(true);
			expect(
				refused(
					await api.post(repoUrl(projectId, "/change-requests/1/merge"), {
						data: {},
					}),
				),
			).toBe(true);
			// Editor holds git:write, so linking passes the permission gate; the
			// e2e stack has no provider plugin, so it then fails as unavailable.
			expect(
				(
					await api.post(gitUrl(projectId, "/repositories"), {
						data: { provider_id: "x", path_or_url: "a/b" },
					})
				).status(),
			).not.toBe(403);
		});
	});
});
