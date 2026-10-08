// spec: features/iam/docs-guide-examples.feature
// seed: tests/seed.spec.ts
//
// The worked examples of docs/guides/roles-and-policies.md must work as written.
// Each example's JSON is read from the guide itself, its placeholders (PROJECT_ID,
// SPRINT_ID, ...) are replaced with real ids, the role is created over the API
// and then what the guide promises is asserted. If someone edits an example in
// the guide and breaks it, this spec fails.

import fs from "node:fs";
import path from "node:path";
import { type APIRequestContext, expect, test } from "@playwright/test";
import {
	API_URL,
	authRequest,
	cleanupGlobalRolesByPrefix,
	cleanupProjectsByPrefix,
	cleanupUsersByPrefix,
	createGlobalRole,
	createProject,
	createProjectAgent,
	createUserWithPassword,
	globalRoleIdByName,
	newRunId,
	projectRoleIdByName,
	type RolePolicy,
	replaceMemberRoles,
} from "../helpers/e2e-api";
import {
	asUser,
	createDoc,
	createFolder,
	createMember,
	createPerson,
	createSprint,
	createTask,
	dataOf,
	errorCode,
	listDocTitles,
	listTasks,
	listViewIds,
	policyIssues,
	postDoc,
	postGlobalRole,
	postProjectRole,
	postTask,
	putMemberRoles,
	putUserRoles,
	sprintViewIds,
} from "../helpers/iam";

const PREFIX = "E2E_IAMDOCS_";
const RUN_ID = newRunId();
const name = (label: string) => `${PREFIX}${label}_${RUN_ID}`;

const GUIDE = path.resolve(
	__dirname,
	"../../../../docs/guides/roles-and-policies.md",
);

/**
 * The `index`-th ```json block under the heading `heading` of the guide, with
 * every placeholder replaced by its value.
 */
function example(
	heading: string,
	index: number,
	values: Record<string, string>,
): RolePolicy {
	const lines = fs.readFileSync(GUIDE, "utf8").split("\n");
	const start = lines.findIndex((line) => line.trim() === `### ${heading}`);
	expect(start, `the guide has a "${heading}" example`).toBeGreaterThanOrEqual(
		0,
	);
	let end = lines.findIndex((line, i) => i > start && line.startsWith("## "));
	end = end === -1 ? lines.length : end;
	const next = lines.findIndex(
		(line, i) => i > start && line.startsWith("### "),
	);
	if (next !== -1 && next < end) end = next;

	const blocks: string[] = [];
	let current: string[] | null = null;
	for (const line of lines.slice(start, end)) {
		if (line.trim() === "```json") current = [];
		else if (line.trim() === "```" && current) {
			blocks.push(current.join("\n"));
			current = null;
		} else if (current) current.push(line);
	}
	expect(blocks.length, `${heading}: json blocks`).toBeGreaterThan(index);
	let text = blocks[index];
	for (const [placeholder, value] of Object.entries(values)) {
		text = text.replaceAll(placeholder, value);
	}
	return JSON.parse(text) as RolePolicy;
}

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

// ─── Limit tasks, sprint and views to one sprint ─────────────────────────────

test.describe('"Limit tasks, sprint and views to one sprint"', () => {
	let projectId: string;
	let sprint: string;
	let otherSprint: string;
	let username: string;
	let policy: RolePolicy;

	test.beforeEach(async ({ request, playwright }) => {
		projectId = await createProject(request, name("SPRINT_GUIDE"));
		sprint = await createSprint(request, projectId, name("S1"));
		otherSprint = await createSprint(request, projectId, name("S2"));
		for (const title of ["Mine 1", "Mine 2", "Mine 3"]) {
			await createTask(request, projectId, title, sprint);
		}
		for (const title of ["Theirs 1", "Theirs 2"]) {
			await createTask(request, projectId, title, otherSprint);
		}
		await createTask(request, projectId, "Backlog task");

		policy = example("Limit tasks, sprint and views to one sprint", 0, {
			PROJECT_ID: projectId,
			SPRINT_ID: sprint,
		});
		const response = await postProjectRole(
			request,
			projectId,
			name("CONTRACTOR_ROLE"),
			policy,
		);
		expect(response.status(), await response.text()).toBe(201);
		const member = await createMember(
			request,
			playwright,
			projectId,
			name("CONTRACTOR"),
			[(await dataOf<{ id: string }>(response)).id],
		);
		username = member.username;
	});

	test("Task lists return only that sprint's tasks, filtered by the database so pages and counts are right", async ({
		playwright,
	}) => {
		await asUser(playwright, username, async (api) => {
			const first = await listTasks(api, projectId, "page_size=2");
			expect(first.total).toBe(3);
			expect(first.ids).toHaveLength(2);
			expect(first.nextCursor).not.toBeNull();
			const second = await listTasks(
				api,
				projectId,
				`page_size=2&cursor=${encodeURIComponent(first.nextCursor ?? "")}`,
			);
			expect(second.ids).toHaveLength(1);
			expect(second.nextCursor).toBeNull();
			expect([...first.titles, ...second.titles].sort()).toEqual([
				"Mine 1",
				"Mine 2",
				"Mine 3",
			]);
		});
	});

	test("Opening another sprint's task is refused, and a task can neither be created without the sprint nor moved out of it", async ({
		request,
		playwright,
	}) => {
		const theirs = (
			await listTasks(request, projectId, `sprint_id=${otherSprint}`)
		).ids[0];
		const mine = (await listTasks(request, projectId, `sprint_id=${sprint}`))
			.ids[0];
		await asUser(playwright, username, async (api) => {
			const task = (id: string) =>
				`${API_URL}/projects/${projectId}/tasks/${id}`;
			expect((await api.get(task(mine))).status()).toBe(200);
			expect((await api.get(task(theirs))).status()).toBe(403);

			expect((await postTask(api, projectId, "No sprint")).status()).toBe(403);
			expect(
				(await postTask(api, projectId, "In sprint", sprint)).status(),
			).toBe(201);

			expect(
				(
					await api.patch(task(mine), { data: { sprint_id: otherSprint } })
				).status(),
			).toBe(403);
			expect(
				(
					await api.patch(task(theirs), { data: { sprint_id: sprint } })
				).status(),
			).toBe(403);
			expect(
				(await api.patch(task(mine), { data: { title: "Renamed" } })).status(),
			).toBe(200);
		});
	});

	test("Views of that sprint are listed, the other sprint's lists are empty and its views cannot be opened", async ({
		request,
		playwright,
	}) => {
		const own = await sprintViewIds(request, projectId, sprint);
		const others = await sprintViewIds(request, projectId, otherSprint);
		const backlog = (await listViewIds(request, projectId, "context=backlog"))
			.ids;
		await asUser(playwright, username, async (api) => {
			expect(
				(
					await listViewIds(
						api,
						projectId,
						`context=sprint&sprint_id=${sprint}`,
					)
				).ids.sort(),
			).toEqual([...own].sort());
			expect(
				(
					await listViewIds(
						api,
						projectId,
						`context=sprint&sprint_id=${otherSprint}`,
					)
				).ids,
			).toEqual([]);
			const view = (id: string) =>
				`${API_URL}/projects/${projectId}/views/${id}`;
			expect((await api.get(view(own[0]))).status()).toBe(200);
			expect((await api.get(view(others[0]))).status()).toBe(403);
			// Backlog and timeline views belong to no sprint: a positive condition leaves them out.
			expect((await api.get(view(backlog[0]))).status()).toBe(403);
		});
	});

	test("Adding views:write to the statement lets the member manage that sprint's views only", async ({
		request,
		playwright,
	}) => {
		// The guide: "Add views:write to the statement to let the member create,
		// rename, reorder and delete views of that sprint". Two statements are
		// involved (the project-level one lets the request in), so the action is
		// added where the guide's own statement list reads views.
		const writable: RolePolicy = JSON.parse(JSON.stringify(policy));
		for (const statement of writable.statements) {
			if (statement.actions.includes("views:read")) {
				statement.actions.push("views:write");
			}
		}
		const response = await postProjectRole(
			request,
			projectId,
			name("VIEW_WRITER_ROLE"),
			writable,
		);
		expect(response.status(), await response.text()).toBe(201);
		const writer = await createMember(
			request,
			playwright,
			projectId,
			name("VIEW_WRITER"),
			[(await dataOf<{ id: string }>(response)).id],
		);
		const own = await sprintViewIds(request, projectId, sprint);
		const others = await sprintViewIds(request, projectId, otherSprint);

		await asUser(playwright, writer.username, async (api) => {
			const views = `${API_URL}/projects/${projectId}/views`;
			const create = (query: string) =>
				api.post(`${views}?${query}`, {
					data: { name: "Made by the contractor" },
				});
			expect(
				(await create(`context=sprint&sprint_id=${sprint}`)).status(),
			).toBe(201);
			expect(
				(await create(`context=sprint&sprint_id=${otherSprint}`)).status(),
			).toBe(403);
			expect((await create("context=backlog")).status()).toBe(403);
			expect(
				(
					await api.patch(`${views}/${own[0]}`, { data: { name: "Renamed" } })
				).status(),
			).toBe(200);
			expect(
				(
					await api.patch(`${views}/${others[0]}`, { data: { name: "Nope" } })
				).status(),
			).toBe(403);
			// Reordering is refused when any listed view is not allowed.
			expect(
				(
					await api.put(
						`${views}/positions?context=sprint&sprint_id=${sprint}`,
						{
							data: { view_ids: [own[0], others[0]] },
						},
					)
				).status(),
			).toBe(403);
			expect((await api.delete(`${views}/${others[0]}`)).status()).toBe(403);
			expect((await api.delete(`${views}/${own[0]}`)).status()).toBe(204);
		});
	});
});

// ─── Documents of a folder and its subfolders ────────────────────────────────

test.describe('"The same pattern limits documents to a folder and its subfolders"', () => {
	test("The guide's doc.ancestor_folder_ids statement, with the docs:read on the project it asks for", async ({
		request,
		playwright,
	}) => {
		const projectId = await createProject(request, name("FOLDER_GUIDE"));
		const folder = await createFolder(request, projectId, "Guide");
		const nested = await createFolder(request, projectId, "Nested", folder);
		const elsewhere = await createFolder(request, projectId, "Elsewhere");
		await createDoc(request, projectId, "Top", folder);
		await createDoc(request, projectId, "Deep", nested);
		await createDoc(request, projectId, "Other", elsewhere);

		const statement = example(
			"Limit tasks, sprint and views to one sprint",
			1,
			{ PROJECT_ID: projectId, FOLDER_ID: folder },
		) as unknown as RolePolicy["statements"][number];
		// The guide shows a bare statement "(plus a docs:read on project/PROJECT_ID,
		// as in the first statement above)".
		const policy: RolePolicy = {
			version: "2026-10-01",
			statements: [
				{
					effect: "Allow",
					actions: ["projects:read", "docs:read"],
					resources: [`project/${projectId}`],
				},
				statement,
			],
		};
		const response = await postProjectRole(
			request,
			projectId,
			name("FOLDER_ROLE"),
			policy,
		);
		expect(response.status(), await response.text()).toBe(201);
		const member = await createMember(
			request,
			playwright,
			projectId,
			name("FOLDER_USER"),
			[(await dataOf<{ id: string }>(response)).id],
		);

		await asUser(playwright, member.username, async (api) => {
			expect((await listDocTitles(api, projectId)).titles).toEqual([
				"Deep",
				"Top",
			]);
		});
	});
});

// ─── A workspace role that gives access to two projects ──────────────────────

test.describe('"A workspace role that gives access to two projects"', () => {
	test("It attaches platform-wide and reaches exactly those two projects", async ({
		request,
		playwright,
	}) => {
		const a = await createProject(request, name("TWO_A"));
		const b = await createProject(request, name("TWO_B"));
		const c = await createProject(request, name("TWO_C"));
		const policy = example(
			"A workspace role that gives access to two projects",
			0,
			{
				PROJECT_A_ID: a,
				PROJECT_B_ID: b,
			},
		);

		const created = await postGlobalRole(request, name("TWO_PROJECTS"), policy);
		expect(created.status(), await created.text()).toBe(201);
		const role = await dataOf<{ id: string }>(created);
		const person = await createPerson(request, playwright, name("TWO_USER"));
		expect(
			(await putUserRoles(request, person.userId, [role.id])).status(),
		).toBe(200);

		await asUser(playwright, person.username, async (api) => {
			expect((await listTasks(api, a)).status).toBe(200);
			expect((await listTasks(api, b)).status).toBe(200);
			expect((await listTasks(api, c)).status).toBe(403);
		});
	});

	test("The same policy on a project's own Roles page is refused at statements[0].resources[1]", async ({
		request,
	}) => {
		const a = await createProject(request, name("TWO_A2"));
		const b = await createProject(request, name("TWO_B2"));
		const policy = example(
			"A workspace role that gives access to two projects",
			0,
			{
				PROJECT_A_ID: a,
				PROJECT_B_ID: b,
			},
		);
		const refused = await postProjectRole(
			request,
			a,
			name("TWO_ON_PROJECT"),
			policy,
		);
		expect(refused.status()).toBe(422);
		expect(await errorCode(refused)).toBe("ROLE_POLICY_INVALID");
		expect((await policyIssues(refused)).map((i) => i.path)).toEqual([
			"statements[0].resources[1]",
		]);
	});
});

// ─── Developer in one project ────────────────────────────────────────────────

test.describe('"Developer in one project"', () => {
	test("Attached platform-wide it gives the same result as attached in the project: that project and no other", async ({
		request,
		playwright,
	}) => {
		const mine = await createProject(request, name("DEV_MINE"));
		const other = await createProject(request, name("DEV_OTHER"));
		await createTask(request, mine, "A task");
		const policy = example("Developer in one project", 0, { PROJECT_ID: mine });
		const created = await postGlobalRole(request, name("DEVELOPER"), policy);
		expect(created.status(), await created.text()).toBe(201);
		const role = await dataOf<{ id: string }>(created);
		const person = await createPerson(request, playwright, name("DEV_USER"));
		await putUserRoles(request, person.userId, [role.id]);

		await asUser(playwright, person.username, async (api) => {
			expect((await listTasks(api, mine)).titles).toEqual(["A task"]);
			expect((await postTask(api, mine, "Written")).status()).toBe(201);
			expect((await listTasks(api, other)).status).toBe(403);
			expect((await postDoc(api, mine, "Doc")).status()).toBe(201);
		});
	});
});

// ─── Restrict an agent ───────────────────────────────────────────────────────

test.describe('"Restrict an agent"', () => {
	test("The Deny role hides the agent from whoever holds it", async ({
		request,
		playwright,
	}) => {
		const projectId = await createProject(request, name("AGENT_GUIDE"));
		const agent = await createProjectAgent(
			request,
			projectId,
			name("SALES_BOT"),
		);
		const editor = await projectRoleIdByName(request, projectId, "Editor");
		const denied = await createMember(
			request,
			playwright,
			projectId,
			name("DENIED"),
			[editor],
		);
		const allowed = await createMember(
			request,
			playwright,
			projectId,
			name("ALLOWED"),
			[editor],
		);

		const policy = example("Restrict an agent", 0, {
			PROJECT_ID: projectId,
			AGENT_ID: agent.id,
		});
		const response = await postProjectRole(
			request,
			projectId,
			name("NO_SALES_BOT"),
			policy,
		);
		expect(response.status(), await response.text()).toBe(201);
		await replaceMemberRoles(request, projectId, denied.memberId, [
			editor,
			(await dataOf<{ id: string }>(response)).id,
		]);

		const agents = (api: APIRequestContext) =>
			api
				.get(`${API_URL}/projects/${projectId}/agents`)
				.then(async (r) =>
					((await r.json()).data.items as Array<{ id: string }>).map(
						(a) => a.id,
					),
				);
		await asUser(playwright, denied.username, async (api) => {
			expect(await agents(api)).toEqual([]);
			expect(
				(
					await api.get(`${API_URL}/projects/${projectId}/agents/${agent.id}`)
				).status(),
			).toBe(403);
		});
		await asUser(playwright, allowed.username, async (api) => {
			expect(await agents(api)).toEqual([agent.id]);
		});
	});

	test("The 'only these people' form denies everyone except the listed users", async ({
		request,
		playwright,
	}) => {
		const projectId = await createProject(request, name("ONLY_THESE"));
		const agent = await createProjectAgent(
			request,
			projectId,
			name("ONLY_BOT"),
		);
		const editor = await projectRoleIdByName(request, projectId, "Editor");
		const insider = await createMember(
			request,
			playwright,
			projectId,
			name("INSIDER"),
			[editor],
		);
		const outsider = await createMember(
			request,
			playwright,
			projectId,
			name("OUTSIDER"),
			[editor],
		);

		const policy = example("Restrict an agent", 1, {
			PROJECT_ID: projectId,
			AGENT_ID: agent.id,
			USER_ID_1: insider.userId,
			USER_ID_2: "00000000-0000-4000-8000-000000000001",
		});
		const response = await postProjectRole(
			request,
			projectId,
			name("ONLY_THESE_ROLE"),
			policy,
		);
		expect(response.status(), await response.text()).toBe(201);
		const roleId = (await dataOf<{ id: string }>(response)).id;
		// Attached to every member of the project, as the guide says.
		await replaceMemberRoles(request, projectId, insider.memberId, [
			editor,
			roleId,
		]);
		await replaceMemberRoles(request, projectId, outsider.memberId, [
			editor,
			roleId,
		]);

		const detail = (api: APIRequestContext) =>
			api
				.get(`${API_URL}/projects/${projectId}/agents/${agent.id}`)
				.then((r) => r.status());
		await asUser(playwright, insider.username, async (api) => {
			expect(await detail(api)).toBe(200);
		});
		await asUser(playwright, outsider.username, async (api) => {
			expect(await detail(api)).toBe(403);
		});
	});
});

// ─── roles:assign examples ───────────────────────────────────────────────────

test.describe('"Let project leads assign only Editor and Viewer"', () => {
	test("The lead assigns Editor and Viewer; Admin, in either direction, is refused with 403", async ({
		request,
		playwright,
	}) => {
		const projectId = await createProject(request, name("LEADS"));
		const editor = await projectRoleIdByName(request, projectId, "Editor");
		const viewer = await projectRoleIdByName(request, projectId, "Viewer");
		const adminRole = await projectRoleIdByName(request, projectId, "Admin");
		const policy = example(
			"Let project leads assign only Editor and Viewer",
			0,
			{
				PROJECT_ID: projectId,
				EDITOR_ID: editor,
				VIEWER_ID: viewer,
			},
		);
		const created = await postProjectRole(
			request,
			projectId,
			name("LEAD_ROLE"),
			policy,
		);
		expect(created.status(), await created.text()).toBe(201);
		const lead = await createMember(
			request,
			playwright,
			projectId,
			name("LEAD"),
			[(await dataOf<{ id: string }>(created)).id],
		);
		const newbie = await createMember(
			request,
			playwright,
			projectId,
			name("NEWBIE"),
			[viewer],
		);
		const boss = await createMember(
			request,
			playwright,
			projectId,
			name("BOSS"),
			[adminRole],
		);
		const spare = await createPerson(request, playwright, name("SPARE"));

		await asUser(playwright, lead.username, async (api) => {
			expect(
				(
					await putMemberRoles(api, projectId, newbie.memberId, [editor])
				).status(),
			).toBe(200);
			expect(
				(
					await putMemberRoles(api, projectId, newbie.memberId, [
						viewer,
						editor,
					])
				).status(),
			).toBe(200);
			// "An attempt to assign Admin, or to take Admin away from someone, is
			// refused with 403 FORBIDDEN."
			const assignAdmin = await putMemberRoles(
				api,
				projectId,
				newbie.memberId,
				[viewer, adminRole],
			);
			expect(assignAdmin.status()).toBe(403);
			expect(await errorCode(assignAdmin)).toBe("FORBIDDEN");
			const takeAway = await putMemberRoles(api, projectId, boss.memberId, [
				viewer,
			]);
			expect(takeAway.status()).toBe(403);
			expect(await errorCode(takeAway)).toBe("FORBIDDEN");

			// "They can manage members": add one with Editor, but not with Admin.
			const add = (roleId: string) =>
				api.post(`${API_URL}/projects/${projectId}/members`, {
					data: { user_id: spare.userId, role_ids: [roleId] },
				});
			expect((await add(adminRole)).status()).toBe(403);
			expect((await add(editor)).status()).toBe(201);
		});
	});
});

test.describe('"Let a support admin assign any workspace role"', () => {
	test("The policy lets its holder assign any workspace role without being able to edit roles", async ({
		request,
		playwright,
	}) => {
		const policy = example(
			"Let a support admin assign any workspace role",
			0,
			{},
		);
		const role = await createGlobalRole(request, name("SUPPORT_ADMIN"), policy);
		const support = name("SUPPORT");
		await createUserWithPassword(request, playwright, {
			username: support,
			fullName: support,
			roles: [role.name],
		});
		const target = await createPerson(
			request,
			playwright,
			name("SUPPORT_TARGET"),
		);
		const adminId = await globalRoleIdByName(request, "ADMIN");
		const userId = await globalRoleIdByName(request, "USER");

		await asUser(playwright, support, async (api) => {
			// Any workspace role, ADMIN included.
			expect(
				(await putUserRoles(api, target.userId, [userId, adminId])).status(),
			).toBe(200);
			// roles:read lets them see who and what they assign...
			expect((await api.get(`${API_URL}/admin/roles`)).status()).toBe(200);
			// ...but defining roles is roles:write, which they do not hold.
			expect(
				(
					await postGlobalRole(api, name("BY_SUPPORT"), {
						version: "2026-10-01",
						statements: [],
					})
				).status(),
			).toBe(403);
		});

		// "role/* includes the SUPER_ADMIN role": the statement covers it. Nothing
		// is assigned here; the simulator answers whether it would be allowed.
		const superAdmin = await globalRoleIdByName(request, "SUPER_ADMIN");
		const simulated = await request.post(`${API_URL}/roles/simulate`, {
			data: {
				policy,
				action: "roles:assign",
				resource: `role/${superAdmin}`,
			},
		});
		expect((await dataOf<{ allowed: boolean }>(simulated)).allowed).toBe(true);
	});
});

test.describe('"Let someone assign any role in one project"', () => {
	test("The holder assigns any role of the project, the Admin role included, and manages members", async ({
		request,
		playwright,
	}) => {
		const projectId = await createProject(request, name("ANY_ROLE"));
		const viewer = await projectRoleIdByName(request, projectId, "Viewer");
		const adminRole = await projectRoleIdByName(request, projectId, "Admin");
		const policy = example("Let someone assign any role in one project", 0, {
			PROJECT_ID: projectId,
		});
		const created = await postProjectRole(
			request,
			projectId,
			name("ANY_ROLE_ROLE"),
			policy,
		);
		expect(created.status(), await created.text()).toBe(201);
		const holder = await createMember(
			request,
			playwright,
			projectId,
			name("HOLDER"),
			[(await dataOf<{ id: string }>(created)).id],
		);
		const target = await createMember(
			request,
			playwright,
			projectId,
			name("TARGET"),
			[viewer],
		);
		const spare = await createPerson(request, playwright, name("ANY_SPARE"));

		await asUser(playwright, holder.username, async (api) => {
			expect(
				(
					await putMemberRoles(api, projectId, target.memberId, [adminRole])
				).status(),
			).toBe(200);
			expect(
				(
					await putMemberRoles(api, projectId, target.memberId, [viewer])
				).status(),
			).toBe(200);
			const add = await api.post(`${API_URL}/projects/${projectId}/members`, {
				data: { user_id: spare.userId, role_ids: [adminRole] },
			});
			expect(add.status()).toBe(201);
		});
	});
});
