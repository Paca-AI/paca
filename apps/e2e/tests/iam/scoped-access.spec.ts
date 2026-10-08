// spec: features/iam/scoped-access.feature
// seed: tests/seed.spec.ts
//
// Roles can be limited to part of a project: the tasks of one sprint
// (`task.sprint_id`), the views of one sprint (`view.sprint_id`), one sprint by
// its resource id, the documents of a folder and its subfolders
// (`doc.ancestor_folder_ids`), or everything except one agent or environment
// (a Deny on its resource). Lists are narrowed inside the query, and a request
// that names something outside the scope is refused with 403.
//
// Setup and assertions go through the API: the admin creates the content, the
// limited member signs in as themselves (asUser) and every request is judged
// against their roles.

import { type APIRequestContext, expect, test } from "@playwright/test";
import {
	API_URL,
	authRequest,
	cleanupProjectsByPrefix,
	cleanupUsersByPrefix,
	createProject,
	createProjectAgent,
	denyResourcePolicy,
	newRunId,
	projectRoleIdByName,
	replaceMemberRoles,
	type SeededAgent,
} from "../helpers/e2e-api";
import {
	cleanupEnvironmentsInProjectsByPrefix,
	createEnvironment,
	type SeededEnvironment,
} from "../helpers/environments";
import {
	allow,
	asUser,
	createDoc,
	createFolder,
	createMember,
	createSprint,
	createTask,
	listDocTitles,
	listSprintIds,
	listTasks,
	listViewIds,
	type Member,
	openProject,
	postDoc,
	postTask,
	projectActions,
	projectRoleFrom,
	sprintViewIds,
} from "../helpers/iam";

const PREFIX = "E2E_IAMSCOPE_";
const RUN_ID = newRunId();
const name = (label: string) => `${PREFIX}${label}_${RUN_ID}`;

async function cleanup(request: APIRequestContext) {
	await cleanupEnvironmentsInProjectsByPrefix(request, PREFIX);
	await cleanupProjectsByPrefix(request, PREFIX);
	await cleanupUsersByPrefix(request, PREFIX);
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

// ─── Tasks limited to one sprint ─────────────────────────────────────────────

test.describe("A role limited to the tasks of one sprint", () => {
	let projectId: string;
	let sprint1: string;
	let sprint2: string;
	let inSprint1: string[];
	let inSprint2: string[];
	let backlogTask: string;
	let member: Member;

	test.beforeEach(async ({ request, playwright }) => {
		projectId = await createProject(request, name("TASKS"));
		sprint1 = await createSprint(request, projectId, name("S1"));
		sprint2 = await createSprint(request, projectId, name("S2"));
		inSprint1 = [];
		inSprint2 = [];
		for (const title of ["A1", "A2", "A3"]) {
			inSprint1.push(await createTask(request, projectId, title, sprint1));
		}
		for (const title of ["B1", "B2"]) {
			inSprint2.push(await createTask(request, projectId, title, sprint2));
		}
		backlogTask = await createTask(request, projectId, "N1");

		const role = await projectRoleFrom(
			request,
			projectId,
			name("ONLY_SPRINT1"),
			openProject(projectId, ["tasks:read", "tasks:write", "sprints:read"]),
			allow(["tasks:read", "tasks:write"], [`project/${projectId}/task/*`], {
				StringEquals: { "task.sprint_id": sprint1 },
			}),
		);
		member = await createMember(
			request,
			playwright,
			projectId,
			name("CONTRACTOR"),
			[role],
		);
	});

	test("The task list returns only that sprint's tasks, with matching totals", async ({
		playwright,
	}) => {
		await asUser(playwright, member.username, async (api) => {
			const all = await listTasks(api, projectId);
			expect(all.status).toBe(200);
			expect(all.titles).toEqual(["A1", "A2", "A3"]);
			expect(all.total).toBe(3);

			// Asking for another sprint or the backlog narrows an empty scope.
			const other = await listTasks(api, projectId, `sprint_id=${sprint2}`);
			expect(other.status).toBe(200);
			expect(other.ids).toEqual([]);
			expect(other.total).toBe(0);
			const backlog = await listTasks(api, projectId, "sprint_id=null");
			expect(backlog.status).toBe(200);
			expect(backlog.total).toBe(0);

			// Asking for its own sprint explicitly gives the same three.
			const own = await listTasks(api, projectId, `sprint_id=${sprint1}`);
			expect(own.titles).toEqual(["A1", "A2", "A3"]);
		});
	});

	test("Opening a task of another sprint, or of no sprint, is refused", async ({
		request,
		playwright,
	}) => {
		const numberOf = async (taskId: string) =>
			(
				await (
					await request.get(`${API_URL}/projects/${projectId}/tasks/${taskId}`)
				).json()
			).data.task_number as number;
		await asUser(playwright, member.username, async (api) => {
			const get = (taskId: string) =>
				api.get(`${API_URL}/projects/${projectId}/tasks/${taskId}`);
			expect((await get(inSprint1[0])).status()).toBe(200);
			expect((await get(inSprint2[0])).status()).toBe(403);
			expect((await get(backlogTask)).status()).toBe(403);

			// The by-number route is judged on the task it finds.
			const byNumber = async (n: number) =>
				(
					await api.get(`${API_URL}/projects/${projectId}/tasks/by-number/${n}`)
				).status();
			expect(await byNumber(await numberOf(inSprint1[0]))).toBe(200);
			expect(await byNumber(await numberOf(inSprint2[0]))).toBe(403);
		});
	});

	test("Creating a task needs the sprint: another sprint or none is refused", async ({
		playwright,
	}) => {
		await asUser(playwright, member.username, async (api) => {
			expect(
				(await postTask(api, projectId, "Own sprint", sprint1)).status(),
			).toBe(201);
			expect(
				(await postTask(api, projectId, "Other sprint", sprint2)).status(),
			).toBe(403);
			// "A task-limited role cannot create tasks without the sprint."
			expect((await postTask(api, projectId, "No sprint")).status()).toBe(403);

			const titles = (await listTasks(api, projectId)).titles;
			expect(titles).toContain("Own sprint");
			expect(titles).not.toContain("Other sprint");
			expect(titles).not.toContain("No sprint");
		});
	});

	test("A task can neither be moved out of the sprint nor into another one", async ({
		request,
		playwright,
	}) => {
		const patch = (api: APIRequestContext, taskId: string, data: object) =>
			api.patch(`${API_URL}/projects/${projectId}/tasks/${taskId}`, { data });
		await asUser(playwright, member.username, async (api) => {
			// Editing a task of the sprint without moving it is fine.
			expect(
				(await patch(api, inSprint1[0], { title: "A1 renamed" })).status(),
			).toBe(200);
			// Out of the sprint: the new value (another sprint, none) is not allowed.
			expect(
				(await patch(api, inSprint1[0], { sprint_id: sprint2 })).status(),
			).toBe(403);
			expect(
				(await patch(api, inSprint1[0], { sprint_id: null })).status(),
			).toBe(403);
			// Into the sprint: the old value is not allowed.
			expect(
				(await patch(api, inSprint2[0], { sprint_id: sprint1 })).status(),
			).toBe(403);
			expect(
				(await patch(api, backlogTask, { sprint_id: sprint1 })).status(),
			).toBe(403);
			// Editing a task of another sprint is refused outright.
			expect(
				(await patch(api, inSprint2[0], { title: "B1 renamed" })).status(),
			).toBe(403);
		});

		// Nothing moved: the admin still sees every task where it was.
		const sprint1Tasks = await listTasks(
			request,
			projectId,
			`sprint_id=${sprint1}`,
		);
		expect(sprint1Tasks.total).toBe(3);
		const sprint2Tasks = await listTasks(
			request,
			projectId,
			`sprint_id=${sprint2}`,
		);
		expect(sprint2Tasks.total).toBe(2);
	});

	test("Deleting is limited the same way", async ({ playwright }) => {
		await asUser(playwright, member.username, async (api) => {
			const del = (taskId: string) =>
				api.delete(`${API_URL}/projects/${projectId}/tasks/${taskId}`);
			expect((await del(inSprint2[1])).status()).toBe(403);
			expect((await del(backlogTask)).status()).toBe(403);
			expect((await del(inSprint1[2])).ok()).toBe(true);
			expect((await listTasks(api, projectId)).total).toBe(2);
		});
	});

	test("The member's permissions still list tasks, because some tasks are allowed", async ({
		playwright,
	}) => {
		await asUser(playwright, member.username, async (api) => {
			expect(await projectActions(api, projectId)).toEqual(
				expect.arrayContaining(["projects:read", "tasks:read", "tasks:write"]),
			);
		});
	});
});

// ─── Views limited to one sprint ─────────────────────────────────────────────

test.describe("A role limited to the views of one sprint", () => {
	let projectId: string;
	let sprint1: string;
	let sprint2: string;
	let own: string[];
	let others: string[];
	let member: Member;

	test.beforeEach(async ({ request, playwright }) => {
		projectId = await createProject(request, name("VIEWS"));
		sprint1 = await createSprint(request, projectId, name("S1"));
		sprint2 = await createSprint(request, projectId, name("S2"));
		own = await sprintViewIds(request, projectId, sprint1);
		others = await sprintViewIds(request, projectId, sprint2);
		expect(own.length).toBeGreaterThan(1);
		expect(others.length).toBeGreaterThan(0);

		const role = await projectRoleFrom(
			request,
			projectId,
			name("VIEWS_S1"),
			openProject(projectId, ["views:read", "views:write"]),
			allow(["views:read", "views:write"], [`project/${projectId}/view/*`], {
				StringEquals: { "view.sprint_id": sprint1 },
			}),
		);
		member = await createMember(
			request,
			playwright,
			projectId,
			name("VIEWER_S1"),
			[role],
		);
	});

	const base = () => `${API_URL}/projects/${projectId}/views`;

	test("Lists return the sprint's views; other sprints and the backlog are empty", async ({
		playwright,
	}) => {
		await asUser(playwright, member.username, async (api) => {
			const ownList = await listViewIds(
				api,
				projectId,
				`context=sprint&sprint_id=${sprint1}`,
			);
			expect(ownList.status).toBe(200);
			expect(ownList.ids.sort()).toEqual([...own].sort());

			const otherList = await listViewIds(
				api,
				projectId,
				`context=sprint&sprint_id=${sprint2}`,
			);
			expect(otherList.status).toBe(200);
			expect(otherList.ids).toEqual([]);

			// Backlog and timeline views belong to no sprint, so the positive
			// condition leaves them out.
			for (const context of ["backlog", "timeline"]) {
				const list = await listViewIds(api, projectId, `context=${context}`);
				expect(list.status).toBe(200);
				expect(list.ids, context).toEqual([]);
			}
		});
	});

	test("A view of another sprint, or a backlog view, cannot be opened or changed", async ({
		request,
		playwright,
	}) => {
		const backlogViews = await listViewIds(
			request,
			projectId,
			"context=backlog",
		);
		expect(backlogViews.ids.length).toBeGreaterThan(0);
		await asUser(playwright, member.username, async (api) => {
			expect((await api.get(`${base()}/${own[0]}`)).status()).toBe(200);
			expect((await api.get(`${base()}/${others[0]}`)).status()).toBe(403);
			expect((await api.get(`${base()}/${backlogViews.ids[0]}`)).status()).toBe(
				403,
			);

			expect(
				(
					await api.patch(`${base()}/${own[0]}`, { data: { name: "Renamed" } })
				).status(),
			).toBe(200);
			expect(
				(
					await api.patch(`${base()}/${others[0]}`, { data: { name: "Nope" } })
				).status(),
			).toBe(403);
		});
	});

	test("Views are created in the sprint only, never in another sprint or at project level", async ({
		playwright,
	}) => {
		await asUser(playwright, member.username, async (api) => {
			const create = (query: string) =>
				api.post(`${base()}?${query}`, { data: { name: "New view" } });
			expect(
				(await create(`context=sprint&sprint_id=${sprint1}`)).status(),
			).toBe(201);
			expect(
				(await create(`context=sprint&sprint_id=${sprint2}`)).status(),
			).toBe(403);
			expect((await create("context=backlog")).status()).toBe(403);
			expect((await create("context=timeline")).status()).toBe(403);
		});
	});

	test("Reordering is refused when any listed view is not allowed", async ({
		playwright,
	}) => {
		await asUser(playwright, member.username, async (api) => {
			const reorder = (query: string, viewIds: string[]) =>
				api.put(`${base()}/positions?${query}`, {
					data: { view_ids: viewIds },
				});
			expect(
				(
					await reorder(
						`context=sprint&sprint_id=${sprint1}`,
						[...own].reverse(),
					)
				).status(),
			).toBe(204);
			expect(
				(await reorder(`context=sprint&sprint_id=${sprint2}`, others)).status(),
			).toBe(403);
			expect(
				(
					await reorder(`context=sprint&sprint_id=${sprint1}`, [
						own[0],
						others[0],
					])
				).status(),
			).toBe(403);
		});
	});

	test("Deleting a view is limited to the sprint", async ({ playwright }) => {
		await asUser(playwright, member.username, async (api) => {
			expect((await api.delete(`${base()}/${others[0]}`)).status()).toBe(403);
			expect((await api.delete(`${base()}/${own[0]}`)).status()).toBe(204);
			const left = await listViewIds(
				api,
				projectId,
				`context=sprint&sprint_id=${sprint1}`,
			);
			expect(left.ids).not.toContain(own[0]);
		});
	});

	test("A negated condition includes the project-level views too", async ({
		request,
		playwright,
	}) => {
		// StringNotEquals is true for a missing attribute, so backlog and
		// timeline views stay readable while one sprint's views are hidden.
		const role = await projectRoleFrom(
			request,
			projectId,
			name("VIEWS_NOT_S2"),
			openProject(projectId, ["views:read"]),
			allow(["views:read"], [`project/${projectId}/view/*`], {
				StringNotEquals: { "view.sprint_id": sprint2 },
			}),
		);
		const other = await createMember(
			request,
			playwright,
			projectId,
			name("VIEWER_NOT_S2"),
			[role],
		);
		const backlog = (await listViewIds(request, projectId, "context=backlog"))
			.ids;
		await asUser(playwright, other.username, async (api) => {
			expect(
				(await listViewIds(api, projectId, "context=backlog")).ids.sort(),
			).toEqual([...backlog].sort());
			expect(
				(
					await listViewIds(
						api,
						projectId,
						`context=sprint&sprint_id=${sprint1}`,
					)
				).ids.sort(),
			).toEqual([...own].sort());
			expect(
				(
					await listViewIds(
						api,
						projectId,
						`context=sprint&sprint_id=${sprint2}`,
					)
				).ids,
			).toEqual([]);
		});
	});
});

// ─── Sprints limited by resource id ──────────────────────────────────────────

test.describe("A role limited to one sprint by its resource id", () => {
	test("Only that sprint is listed, readable and writable", async ({
		request,
		playwright,
	}) => {
		const projectId = await createProject(request, name("SPRINTS"));
		const allowed = await createSprint(request, projectId, name("ALLOWED"));
		const hidden = await createSprint(request, projectId, name("HIDDEN"));
		const role = await projectRoleFrom(
			request,
			projectId,
			name("ONE_SPRINT"),
			openProject(projectId, ["sprints:read"]),
			allow(
				["sprints:read", "sprints:write"],
				[`project/${projectId}/sprint/${allowed}`],
			),
		);
		const member = await createMember(
			request,
			playwright,
			projectId,
			name("SPRINT_MEMBER"),
			[role],
		);

		await asUser(playwright, member.username, async (api) => {
			const list = await listSprintIds(api, projectId);
			expect(list.status).toBe(200);
			expect(list.ids).toEqual([allowed]);

			const sprintUrl = (id: string) =>
				`${API_URL}/projects/${projectId}/sprints/${id}`;
			expect((await api.get(sprintUrl(allowed))).status()).toBe(200);
			expect((await api.get(sprintUrl(hidden))).status()).toBe(403);
			expect(
				(
					await api.patch(sprintUrl(allowed), { data: { goal: "Ship it" } })
				).status(),
			).toBe(200);
			expect(
				(
					await api.patch(sprintUrl(hidden), { data: { goal: "Nope" } })
				).status(),
			).toBe(403);
			expect((await api.delete(sprintUrl(hidden))).status()).toBe(403);

			// Creating a sprint is a project-level write the role does not hold.
			expect(
				(
					await api.post(`${API_URL}/projects/${projectId}/sprints`, {
						data: { name: "New sprint" },
					})
				).status(),
			).toBe(403);
		});
	});
});

// ─── Documents limited to a folder ───────────────────────────────────────────

test.describe("A role limited to the documents of a folder", () => {
	let projectId: string;
	let member: Member;
	let folder: string;
	let subfolder: string;
	let otherFolder: string;
	const docs: Record<string, string> = {};

	test.beforeEach(async ({ request, playwright }) => {
		projectId = await createProject(request, name("DOCS"));
		folder = await createFolder(request, projectId, "Handbook");
		subfolder = await createFolder(request, projectId, "Chapters", folder);
		otherFolder = await createFolder(request, projectId, "Private");
		docs.top = await createDoc(request, projectId, "In the folder", folder);
		docs.sub = await createDoc(
			request,
			projectId,
			"In the subfolder",
			subfolder,
		);
		docs.other = await createDoc(
			request,
			projectId,
			"In another folder",
			otherFolder,
		);
		docs.root = await createDoc(request, projectId, "At the root");

		const role = await projectRoleFrom(
			request,
			projectId,
			name("ONE_FOLDER"),
			openProject(projectId, ["docs:read", "docs:write"]),
			allow(["docs:read", "docs:write"], [`project/${projectId}/doc/*`], {
				In: { "doc.ancestor_folder_ids": [folder] },
			}),
		);
		member = await createMember(
			request,
			playwright,
			projectId,
			name("DOC_MEMBER"),
			[role],
		);
	});

	test("The folder and its subfolders are listed; everything else is left out", async ({
		playwright,
	}) => {
		await asUser(playwright, member.username, async (api) => {
			const list = await listDocTitles(api, projectId);
			expect(list.status).toBe(200);
			expect(list.titles).toEqual(["In the folder", "In the subfolder"]);
		});
	});

	test("A document outside the folder cannot be opened or edited", async ({
		playwright,
	}) => {
		await asUser(playwright, member.username, async (api) => {
			const doc = (id: string) => `${API_URL}/projects/${projectId}/docs/${id}`;
			expect((await api.get(doc(docs.top))).status()).toBe(200);
			expect((await api.get(doc(docs.sub))).status()).toBe(200);
			expect((await api.get(doc(docs.other))).status()).toBe(403);
			expect((await api.get(doc(docs.root))).status()).toBe(403);
			expect(
				(
					await api.patch(doc(docs.sub), { data: { title: "Edited" } })
				).status(),
			).toBe(200);
			expect(
				(
					await api.patch(doc(docs.other), { data: { title: "Nope" } })
				).status(),
			).toBe(403);
			expect((await api.delete(doc(docs.other))).status()).toBe(403);
		});
	});

	test("A document is created in the folder or a subfolder, nowhere else", async ({
		playwright,
	}) => {
		await asUser(playwright, member.username, async (api) => {
			expect((await postDoc(api, projectId, "New", folder)).status()).toBe(201);
			expect(
				(await postDoc(api, projectId, "New nested", subfolder)).status(),
			).toBe(201);
			expect(
				(await postDoc(api, projectId, "Elsewhere", otherFolder)).status(),
			).toBe(403);
			expect((await postDoc(api, projectId, "No folder")).status()).toBe(403);
		});
	});

	test("A document cannot be moved out of the folder", async ({
		playwright,
	}) => {
		await asUser(playwright, member.username, async (api) => {
			const move = (folderId: string | null) =>
				api.patch(`${API_URL}/projects/${projectId}/docs/${docs.top}`, {
					data: { folder_id: folderId },
				});
			expect((await move(otherFolder)).status()).toBe(403);
			expect((await move(null)).status()).toBe(403);
			// Into a subfolder stays inside the scope.
			expect((await move(subfolder)).status()).toBe(200);
		});
	});
});

// ─── Agents and environments: deny by resource ───────────────────────────────

test.describe("Denying one agent and one environment narrows the lists", () => {
	// Environments are provisioned by a worker; cleanup waits for them to settle.
	test.describe.configure({ timeout: 150_000 });

	let projectId: string;
	let secretAgent: SeededAgent;
	let openAgent: SeededAgent;
	let secretEnv: SeededEnvironment;
	let openEnv: SeededEnvironment;
	let editorRole: string;
	let restricted: Member;
	let unaffected: Member;

	test.beforeEach(async ({ request, playwright }) => {
		projectId = await createProject(request, name("RESOURCES"));
		secretAgent = await createProjectAgent(
			request,
			projectId,
			name("SECRET_BOT"),
		);
		openAgent = await createProjectAgent(request, projectId, name("OPEN_BOT"));
		secretEnv = await createEnvironment(request, projectId, name("SECRET_ENV"));
		openEnv = await createEnvironment(request, projectId, name("OPEN_ENV"));
		editorRole = await projectRoleIdByName(request, projectId, "Editor");
		restricted = await createMember(
			request,
			playwright,
			projectId,
			name("RESTRICTED"),
			[editorRole],
		);
		unaffected = await createMember(
			request,
			playwright,
			projectId,
			name("UNAFFECTED"),
			[editorRole],
		);
		const denyRole = await projectRoleFrom(
			request,
			projectId,
			name("DENY_BOTH"),
			...denyResourcePolicy(projectId, "agent", secretAgent.id, [
				"agents:*",
				"conversations:*",
			]).statements,
			...denyResourcePolicy(projectId, "environment", secretEnv.id, [
				"environments:*",
			]).statements,
		);
		await replaceMemberRoles(request, projectId, restricted.memberId, [
			editorRole,
			denyRole,
		]);
	});

	const agentIds = async (api: APIRequestContext) =>
		(
			(await (await api.get(`${API_URL}/projects/${projectId}/agents`)).json())
				.data.items as Array<{ id: string }>
		)
			.map((a) => a.id)
			.sort();
	const environmentIds = async (api: APIRequestContext) =>
		(
			(
				await (
					await api.get(`${API_URL}/projects/${projectId}/environments`)
				).json()
			).data.environments as Array<{ id: string }>
		)
			.map((e) => e.id)
			.sort();

	test("Both lists leave the denied resource out and keep the rest", async ({
		playwright,
	}) => {
		await asUser(playwright, restricted.username, async (api) => {
			expect(await agentIds(api)).toEqual([openAgent.id]);
			expect(await environmentIds(api)).toEqual([openEnv.id]);
		});
		await asUser(playwright, unaffected.username, async (api) => {
			expect(await agentIds(api)).toEqual(
				[openAgent.id, secretAgent.id].sort(),
			);
			expect(await environmentIds(api)).toEqual(
				[openEnv.id, secretEnv.id].sort(),
			);
		});
	});

	test("Routes on the denied resources answer 403, the others 200", async ({
		playwright,
	}) => {
		await asUser(playwright, restricted.username, async (api) => {
			const agents = `${API_URL}/projects/${projectId}/agents`;
			const environments = `${API_URL}/projects/${projectId}/environments`;
			expect((await api.get(`${agents}/${secretAgent.id}`)).status()).toBe(403);
			expect((await api.get(`${agents}/${openAgent.id}`)).status()).toBe(200);
			expect((await api.get(`${environments}/${secretEnv.id}`)).status()).toBe(
				403,
			);
			expect((await api.get(`${environments}/${openEnv.id}`)).status()).toBe(
				200,
			);
		});
	});

	test("Unrelated lists are untouched by the Deny", async ({
		request,
		playwright,
	}) => {
		await createTask(request, projectId, "Still visible");
		await asUser(playwright, restricted.username, async (api) => {
			expect((await listTasks(api, projectId)).titles).toEqual([
				"Still visible",
			]);
			expect(
				(await api.get(`${API_URL}/projects/${projectId}/sprints`)).status(),
			).toBe(200);
		});
	});
});
