// spec: features/iam/evaluation.feature
// seed: tests/seed.spec.ts
//
// How a request is decided (docs/guides/iam-authorization.md, "How a request is
// decided"): nothing is allowed by default, an explicit Deny always wins, the
// statements of all of a principal's roles add up, an edit applies on the next
// request, and an agent is judged by its own roles.
//
// Everything is set up and asserted over the API: the evaluator is the same
// whatever the client, so these scenarios stay fast and robust. Members sign in
// as themselves (asUser) so each request is judged against that user's roles.

import type { APIResponse } from "@playwright/test";
import { type APIRequestContext, expect, test } from "@playwright/test";
import {
	API_URL,
	authRequest,
	bindGlobalAgentRole,
	cleanupGlobalAgentsByPrefix,
	cleanupGlobalRolesByPrefix,
	cleanupProjectsByPrefix,
	cleanupUsersByPrefix,
	createGlobalAgent,
	createGlobalRole,
	createProject,
	createProjectAgent,
	newRunId,
	projectRoleIdByName,
} from "../helpers/e2e-api";
import {
	agentContext,
	allow,
	asAgent,
	asUser,
	createDoc,
	createMember,
	createSprint,
	createTask,
	dataOf,
	deny,
	listDocTitles,
	listTasks,
	policyOf,
	postDoc,
	postTask,
	projectActions,
	projectRoleFrom,
	putMemberRoles,
} from "../helpers/iam";

const PREFIX = "E2E_IAMEVAL_";
const RUN_ID = newRunId();

async function cleanup(request: APIRequestContext) {
	await cleanupProjectsByPrefix(request, PREFIX);
	await cleanupUsersByPrefix(request, PREFIX);
	await cleanupGlobalAgentsByPrefix(request, PREFIX);
	await cleanupGlobalRolesByPrefix(request, PREFIX);
}

const name = (label: string) => `${PREFIX}${label}_${RUN_ID}`;

test.describe("Evaluating a request", () => {
	let projectId: string;

	test.beforeEach(async ({ request, context }) => {
		await authRequest(request);
		await cleanup(request);
		await context.clearCookies();
		projectId = await createProject(request, name("PROJECT"));
	});

	test.afterEach(async ({ request }) => {
		await authRequest(request);
		await cleanup(request);
	});

	test("Default deny: a member whose role has no statements cannot read tasks", async ({
		request,
		playwright,
	}) => {
		await createTask(request, projectId, "Secret task");
		const emptyRole = await projectRoleFrom(request, projectId, name("EMPTY"));
		const member = await createMember(
			request,
			playwright,
			projectId,
			name("NOSTATEMENTS"),
			[emptyRole],
		);

		await asUser(playwright, member.username, async (api) => {
			// Every guarded route refuses: the gate runs before the handler.
			expect((await listTasks(api, projectId)).status).toBe(403);
			expect((await api.get(`${API_URL}/projects/${projectId}`)).status()).toBe(
				403,
			);
			expect(
				(await api.get(`${API_URL}/projects/${projectId}/sprints`)).status(),
			).toBe(403);
			expect((await postTask(api, projectId, "Nope")).status()).toBe(403);
			// What the member may do is the empty set, not an error.
			expect(await projectActions(api, projectId)).toEqual([]);
		});
	});

	test("Default deny: a member with no role at all sees nothing, and gets access back with a role", async ({
		request,
		playwright,
	}) => {
		await createTask(request, projectId, "Hidden until a role");
		const viewer = await projectRoleIdByName(request, projectId, "Viewer");
		const member = await createMember(
			request,
			playwright,
			projectId,
			name("NOROLE"),
			[viewer],
		);
		// An empty replace-set is accepted and detaches everything.
		expect(
			(await putMemberRoles(request, projectId, member.memberId, [])).status(),
		).toBe(200);

		await asUser(playwright, member.username, async (api) => {
			expect((await listTasks(api, projectId)).status).toBe(403);
			expect(await projectActions(api, projectId)).toEqual([]);
		});

		expect(
			(
				await putMemberRoles(request, projectId, member.memberId, [viewer])
			).status(),
		).toBe(200);
		await asUser(playwright, member.username, async (api) => {
			const page = await listTasks(api, projectId);
			expect(page.status).toBe(200);
			expect(page.titles).toEqual(["Hidden until a role"]);
		});
	});

	test("An action granted on one resource is not granted on another", async ({
		request,
		playwright,
	}) => {
		const otherProject = await createProject(request, name("OTHER"));
		const role = await projectRoleFrom(
			request,
			projectId,
			name("READONLY"),
			allow(["projects:read", "tasks:read"], [`project/${projectId}/*`]),
		);
		const member = await createMember(
			request,
			playwright,
			projectId,
			name("ONEPROJECT"),
			[role],
		);

		await asUser(playwright, member.username, async (api) => {
			expect((await listTasks(api, projectId)).status).toBe(200);
			// A project-scoped attachment only ever acts inside its project.
			expect((await listTasks(api, otherProject)).status).toBe(403);
			// ...and a read-only grant is not a write grant.
			expect((await postTask(api, projectId, "Write")).status()).toBe(403);
		});
	});

	test("An explicit Deny wins over an Allow in the same role and in another role", async ({
		request,
		playwright,
	}) => {
		const taskId = await createTask(request, projectId, "Existing task");
		const editor = await projectRoleIdByName(request, projectId, "Editor");
		const noTaskWrites = await projectRoleFrom(
			request,
			projectId,
			name("NOWRITES"),
			deny(["tasks:write"], [`project/${projectId}/*`]),
		);
		const member = await createMember(
			request,
			playwright,
			projectId,
			name("DENIED"),
			[editor, noTaskWrites],
		);

		await asUser(playwright, member.username, async (api) => {
			// The Editor role allows tasks:write, the Deny role takes it away.
			expect((await postTask(api, projectId, "Created")).status()).toBe(403);
			expect(
				(
					await api.patch(`${API_URL}/projects/${projectId}/tasks/${taskId}`, {
						data: { title: "Renamed" },
					})
				).status(),
			).toBe(403);
			expect(
				(
					await api.delete(`${API_URL}/projects/${projectId}/tasks/${taskId}`)
				).status(),
			).toBe(403);
			// Everything the Deny does not name keeps working.
			expect((await listTasks(api, projectId)).titles).toEqual([
				"Existing task",
			]);
			expect(
				(await postDoc(api, projectId, "Docs still writable")).status(),
			).toBe(201);
		});
	});

	test("A Deny beats the project Admin's full access, but only where it points", async ({
		request,
		playwright,
	}) => {
		const taskId = await createTask(request, projectId, "Admin task");
		const sprintId = await createSprint(request, projectId, name("SPRINT"));
		const admin = await projectRoleIdByName(request, projectId, "Admin");
		const noTasks = await projectRoleFrom(
			request,
			projectId,
			name("NOTASKS"),
			deny(["tasks:read", "tasks:write"], [`project/${projectId}/task/*`]),
		);
		const member = await createMember(
			request,
			playwright,
			projectId,
			name("ADMINDENIED"),
			[admin, noTasks],
		);

		await asUser(playwright, member.username, async (api) => {
			// One task: refused although the Admin role says `*` on everything.
			expect(
				(
					await api.get(`${API_URL}/projects/${projectId}/tasks/${taskId}`)
				).status(),
			).toBe(403);
			// The task list is scoped by the same Deny: no row survives.
			const page = await listTasks(api, projectId);
			expect(page.status).toBe(200);
			expect(page.ids).toEqual([]);
			expect(page.total).toBe(0);
			// Sprints are not tasks: the Admin role still manages them.
			expect(
				(
					await api.patch(
						`${API_URL}/projects/${projectId}/sprints/${sprintId}`,
						{
							data: { name: name("SPRINT_RENAMED") },
						},
					)
				).status(),
			).toBe(200);
		});
	});

	test("Several roles on one member add up their permissions", async ({
		request,
		playwright,
	}) => {
		await createTask(request, projectId, "Task for the union");
		await createDoc(request, projectId, "Doc for the union");
		const readTasks = await projectRoleFrom(
			request,
			projectId,
			name("TASKS"),
			allow(["projects:read", "tasks:read"], [`project/${projectId}/*`]),
		);
		const readDocs = await projectRoleFrom(
			request,
			projectId,
			name("DOCS"),
			allow(["projects:read", "docs:read"], [`project/${projectId}/*`]),
		);
		const member = await createMember(
			request,
			playwright,
			projectId,
			name("UNION"),
			[readTasks],
		);

		const reach = (username: string) =>
			asUser(playwright, username, async (api) => ({
				tasks: (await listTasks(api, projectId)).status,
				docs: (await listDocTitles(api, projectId)).status,
			}));

		expect(await reach(member.username)).toEqual({ tasks: 200, docs: 403 });

		await putMemberRoles(request, projectId, member.memberId, [readDocs]);
		expect(await reach(member.username)).toEqual({ tasks: 403, docs: 200 });

		await putMemberRoles(request, projectId, member.memberId, [
			readTasks,
			readDocs,
		]);
		expect(await reach(member.username)).toEqual({ tasks: 200, docs: 200 });
		await asUser(playwright, member.username, async (api) => {
			expect(await projectActions(api, projectId)).toEqual(
				expect.arrayContaining(["tasks:read", "docs:read", "projects:read"]),
			);
		});
	});

	test("Removing a role removes the access it gave, on the next request", async ({
		request,
		playwright,
	}) => {
		const editor = await projectRoleIdByName(request, projectId, "Editor");
		const viewer = await projectRoleIdByName(request, projectId, "Viewer");
		const member = await createMember(
			request,
			playwright,
			projectId,
			name("DEMOTED"),
			[editor],
		);

		await asUser(playwright, member.username, async (api) => {
			expect((await postTask(api, projectId, "As editor")).status()).toBe(201);
		});

		// Replace Editor with Viewer: reading stays, writing goes.
		await putMemberRoles(request, projectId, member.memberId, [viewer]);
		await asUser(playwright, member.username, async (api) => {
			expect((await listTasks(api, projectId)).status).toBe(200);
			expect((await postTask(api, projectId, "As viewer")).status()).toBe(403);
		});

		// Replace with the empty set: reading goes too.
		await putMemberRoles(request, projectId, member.memberId, []);
		await asUser(playwright, member.username, async (api) => {
			expect((await listTasks(api, projectId)).status).toBe(403);
		});
	});

	test("Editing or deleting a role changes what its holders may do at once", async ({
		request,
		playwright,
	}) => {
		const roleId = await projectRoleFrom(
			request,
			projectId,
			name("MUTABLE"),
			allow(
				["projects:read", "tasks:read", "tasks:write"],
				[`project/${projectId}/*`],
			),
		);
		const member = await createMember(
			request,
			playwright,
			projectId,
			name("HOLDER"),
			[roleId],
		);

		// One session throughout: a role edit must not need a new login.
		await asUser(playwright, member.username, async (api) => {
			expect((await postTask(api, projectId, "Before edit")).status()).toBe(
				201,
			);

			const edited = await request.put(
				`${API_URL}/projects/${projectId}/roles/${roleId}`,
				{
					data: {
						name: name("MUTABLE"),
						description: "",
						policy: policyOf(
							allow(
								["projects:read", "tasks:read"],
								[`project/${projectId}/*`],
							),
						),
					},
				},
			);
			expect(edited.status()).toBe(200);
			expect((await postTask(api, projectId, "After edit")).status()).toBe(403);
			expect((await listTasks(api, projectId)).status).toBe(200);

			// Deleting the role takes its attachments with it.
			expect(
				(
					await request.delete(
						`${API_URL}/projects/${projectId}/roles/${roleId}`,
					)
				).status(),
			).toBe(204);
			expect((await listTasks(api, projectId)).status).toBe(403);
		});
	});

	test("A role's name means nothing: a role called ADMIN grants only what its policy says", async ({
		request,
		playwright,
	}) => {
		const roleId = await projectRoleFrom(
			request,
			projectId,
			"ADMIN",
			allow(["projects:read", "docs:read"], [`project/${projectId}/*`]),
		);
		const member = await createMember(
			request,
			playwright,
			projectId,
			name("LOOKALIKE"),
			[roleId],
		);

		await asUser(playwright, member.username, async (api) => {
			expect((await listDocTitles(api, projectId)).status).toBe(200);
			expect((await listTasks(api, projectId)).status).toBe(403);
			// ...and the name does not unlock the workspace either (the default
			// role every account holds only reads users).
			expect((await api.get(`${API_URL}/admin/roles`)).status()).toBe(403);
		});
	});
});

test.describe("An agent is judged by its own roles", () => {
	test.beforeEach(async ({ request, context }) => {
		await authRequest(request);
		await cleanup(request);
		await context.clearCookies();
	});

	test.afterEach(async ({ request }) => {
		await authRequest(request);
		await cleanup(request);
	});

	/** The workspace actions the agent reports for itself. */
	async function agentGlobalActions(api: APIRequestContext): Promise<string[]> {
		const response = await api.get(`${API_URL}/agents/me/global-permissions`);
		expect(response.status()).toBe(200);
		return (await dataOf<{ actions: string[] }>(response)).actions.sort();
	}

	test("A global agent has the permissions of its own roles, not those of the bot behind the key", async ({
		request,
		playwright,
	}) => {
		const agent = await createGlobalAgent(request, name("GLOBAL_BOT"));
		const plugins = await createGlobalRole(
			request,
			name("PLUGINS_ONLY"),
			policyOf(allow(["plugins:read"], ["plugin", "plugin/*"])),
		);

		// A new global agent starts with the default role (users:read).
		await asAgent(playwright, agent.id, async (api) => {
			expect(await agentGlobalActions(api)).toEqual(["users:read"]);
			expect((await api.get(`${API_URL}/admin/users`)).status()).toBe(200);
			expect((await api.get(`${API_URL}/admin/roles`)).status()).toBe(403);
		});

		await bindGlobalAgentRole(request, agent.id, plugins.name);
		await asAgent(playwright, agent.id, async (api) => {
			expect(await agentGlobalActions(api)).toEqual(["plugins:read"]);
			// The role it no longer holds is gone at once.
			expect((await api.get(`${API_URL}/admin/users`)).status()).toBe(403);
		});

		// The same key without X-Agent-ID is the shared bot user, which holds
		// far more: only the header makes the request the agent's own.
		const bot = await playwright.request.newContext({
			extraHTTPHeaders: {
				"X-API-Key": process.env.E2E_AGENT_API_KEY ?? "e2e-agent-api-key",
			},
		});
		try {
			const response = await bot.get(`${API_URL}/users/me/global-permissions`);
			expect(response.status()).toBe(200);
			const actions = (await dataOf<{ actions: string[] }>(response)).actions;
			expect(actions.length).toBeGreaterThan(1);
		} finally {
			await bot.dispose();
		}
	});

	test("A project agent is judged by the roles it holds in the project", async ({
		request,
		playwright,
	}) => {
		const projectId = await createProject(request, name("AGENT_PROJECT"));
		await createTask(request, projectId, "Agent task");
		const agent = await createProjectAgent(
			request,
			projectId,
			name("PROJECT_BOT"),
		);
		const viewer = await projectRoleIdByName(request, projectId, "Viewer");
		const adminRole = await projectRoleIdByName(request, projectId, "Admin");

		const members = (
			await (
				await request.get(`${API_URL}/projects/${projectId}/members`)
			).json()
		).data as Array<{ id: string; agent_id?: string }>;
		const agentMember = members.find((m) => m.agent_id === agent.id);
		expect(agentMember, "the agent is a member of its project").toBeTruthy();
		const memberId = agentMember?.id ?? "";

		const probe = (api: APIRequestContext) =>
			Promise.all([
				listTasks(api, projectId).then((p) => p.status),
				postTask(api, projectId, "From the agent").then((r: APIResponse) =>
					r.status(),
				),
			]);

		// Created with the project's Admin role: it reads and writes.
		await asAgent(playwright, agent.id, async (api) => {
			expect(await probe(api)).toEqual([200, 201]);
		});

		// As a Viewer it can read but no longer write.
		await putMemberRoles(request, projectId, memberId, [viewer]);
		await asAgent(playwright, agent.id, async (api) => {
			expect(await probe(api)).toEqual([200, 403]);
		});

		// With a Deny on tasks next to Admin, it reads nothing.
		const noTasks = await projectRoleFrom(
			request,
			projectId,
			name("AGENT_NOTASKS"),
			deny(["tasks:*"], [`project/${projectId}/task/*`]),
		);
		await putMemberRoles(request, projectId, memberId, [adminRole, noTasks]);
		await asAgent(playwright, agent.id, async (api) => {
			const page = await listTasks(api, projectId);
			expect(page.status).toBe(200);
			expect(page.ids).toEqual([]);
			expect((await postTask(api, projectId, "Denied")).status()).toBe(403);
		});

		// A claim of an agent that does not exist is not trusted.
		const stranger = await agentContext(
			playwright,
			"00000000-0000-4000-8000-000000000000",
		);
		try {
			expect(
				(await stranger.get(`${API_URL}/agents/me/global-permissions`)).ok(),
			).toBe(false);
		} finally {
			await stranger.dispose();
		}
	});
});
