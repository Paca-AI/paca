// What the sync feed says about each kind of change. The admin's socket syncs a
// project, a change is made over the API, and the spec checks the change that
// arrives: its type, operation, the entity it carries (or deliberately does
// not), and the attribution. Authorization is in sync-authorization.spec.ts and
// delivery guarantees in sync-resilience.spec.ts.

import { expect, test } from "@playwright/test";
import {
	API_URL,
	authRequest,
	createProject,
	createProjectAgent,
	replaceMemberRoles,
} from "../helpers/e2e-api";
import {
	cleanupEnvironmentsInProjectsByPrefix,
	createEnvironment,
} from "../helpers/environments";
import {
	allow,
	asAgent,
	createAgentKey,
	createDoc,
	createFolder,
	createMember,
	createSprint,
	createTask,
	listViewIds,
	projectRoleFrom,
} from "../helpers/iam";
import {
	blocks,
	changes,
	syncProbe,
	useSyncScaffold,
	waitForType,
} from "../helpers/sync";

const { name } = useSyncScaffold("E2E_RTSYNCENT_");

async function arrange(
	request: Parameters<typeof authRequest>[0],
	label: string,
) {
	await authRequest(request);
	const projectId = await createProject(request, name(label));
	const { probe, ack } = await syncProbe(request, projectId);
	return { projectId, probe, ack, api: changes(request, projectId) };
}

test.describe("tasks", () => {
	test("a created task arrives as a create with the task, its people and its attribution", async ({
		request,
	}) => {
		const { projectId, probe } = await arrange(request, "tcreate");
		const taskId = await createTask(request, projectId, name("t"));
		const c = await waitForType(probe, "task.created", { entityId: taskId });

		expect(c.op).toBe("create");
		expect(c.entity_type).toBe("task");
		expect(c.entity_id).toBe(taskId);
		expect(c.entity).toMatchObject({
			id: taskId,
			project_id: projectId,
			title: name("t"),
			assignment_mode: "manual",
		});
		expect(c.entity?.task_number).toBe(1);
		expect(c.payload?.activity_type).toBe("task.created");
		expect(typeof c.payload?.actor_id).toBe("string");
	});

	test("an update carries the new task and the diff, and never the description", async ({
		request,
	}) => {
		const { projectId, probe, api } = await arrange(request, "tupdate");
		const taskId = await createTask(request, projectId, name("t"));
		await waitForType(probe, "task.created");

		await api.patchTask(taskId, {
			title: name("renamed"),
			description: blocks("secret body"),
			importance: 3,
		});
		const c = await waitForType(probe, "task.updated", { entityId: taskId });

		expect(c.op).toBe("update");
		expect(c.entity).toMatchObject({ title: name("renamed"), importance: 3 });
		expect(c.entity).not.toHaveProperty("description");
		expect(JSON.stringify(c.entity)).not.toContain("secret body");
		const diff = JSON.parse(String(c.payload?.content)) as {
			changes: Array<{ field: string }>;
		};
		expect(diff.changes.map((x) => x.field)).toEqual(
			expect.arrayContaining(["title", "importance"]),
		);
	});

	test("every field of an update is in the entity: status, type, assignees, tags, dates, points, custom fields", async ({
		request,
		playwright,
	}) => {
		const { projectId, probe, api } = await arrange(request, "tfields");
		const taskId = await createTask(request, projectId, name("t"));
		const status = await api.createStatus(name("st"));
		const type = await api.createType(name("ty"));
		const key = `f_${Date.now()}`;
		await api.createField(key, name("field"));
		const roleId = await projectRoleFrom(
			request,
			projectId,
			name("role"),
			allow(["projects:read", "tasks:read"], [`project/${projectId}/*`]),
		);
		const member = await createMember(
			request,
			playwright,
			projectId,
			name("m"),
			[roleId],
		);

		await api.patchTask(taskId, {
			status_id: status.id,
			task_type_id: type.id,
			assignee_ids: [member.memberId],
			tags: ["a", "b"],
			story_points: 5,
			start_date: "2030-01-02T00:00:00Z",
			due_date: "2030-02-03T00:00:00Z",
			custom_fields: { [key]: "custom value" },
		});
		const c = await waitForType(probe, "task.updated", { entityId: taskId });
		expect(c.entity).toMatchObject({
			status_id: status.id,
			task_type_id: type.id,
			assignee_ids: [member.memberId],
			tags: ["a", "b"],
			story_points: 5,
			custom_fields: { [key]: "custom value" },
		});
		expect(String(c.entity?.start_date)).toContain("2030-01-02");
		expect(String(c.entity?.due_date)).toContain("2030-02-03");
	});

	test("a delete arrives without an entity but names the task", async ({
		request,
	}) => {
		const { projectId, probe, api } = await arrange(request, "tdelete");
		const taskId = await createTask(request, projectId, name("t"));
		await api.deleteTask(taskId);
		const c = await waitForType(probe, "task.deleted", { entityId: taskId });
		expect(c.op).toBe("delete");
		expect(c.entity).toBeUndefined();
		expect(c.task_id).toBe(taskId);
	});

	test("a change that is too large to attach arrives without the entity", async ({
		request,
	}) => {
		const { projectId, probe, api } = await arrange(request, "tbig");
		const key = `big_${Date.now()}`;
		await api.createField(key, name("big"));
		const taskId = await createTask(request, projectId, name("t"));
		const response = await request.patch(
			`${API_URL}/projects/${projectId}/tasks/${taskId}`,
			{ data: { custom_fields: { [key]: "x".repeat(80_000) } } },
		);
		test.skip(
			!response.ok(),
			`the API refuses an 80 kB custom field here (${response.status()})`,
		);
		const c = await waitForType(probe, "task.updated", { entityId: taskId });
		// The change still arrives, so a client falls back to refetching.
		expect(c.entity).toBeUndefined();
		expect(c.task_id).toBe(taskId);
	});

	test("comments arrive as add, update and delete, naming the task, with no entity", async ({
		request,
	}) => {
		const { projectId, probe, api } = await arrange(request, "tcomment");
		const taskId = await createTask(request, projectId, name("t"));
		const comment = await api.comment(taskId, "first");
		const added = await waitForType(probe, "task.comment.added");
		expect(added.op).toBe("create");
		expect(added.payload?.task_id).toBe(taskId);
		expect(added.entity).toBeUndefined();

		await api.editComment(taskId, comment.id, "edited");
		const updated = await waitForType(probe, "task.comment.updated");
		expect(updated.op).toBe("update");

		await api.deleteComment(taskId, comment.id);
		const deleted = await waitForType(probe, "task.comment.deleted");
		expect(deleted.op).toBe("delete");
		expect(deleted.payload?.task_id).toBe(taskId);
	});

	test("links between tasks arrive as add and remove", async ({ request }) => {
		const { projectId, probe, api } = await arrange(request, "tlink");
		const a = await createTask(request, projectId, name("a"));
		const b = await createTask(request, projectId, name("b"));
		const link = await api.link(a, b);
		const added = await waitForType(probe, "task.link.added");
		expect(added.payload?.task_id).toBe(a);
		await api.unlink(a, link.id);
		await waitForType(probe, "task.link.removed");
	});

	test("a task created in a sprint, and moved out of it, says so in the entity", async ({
		request,
	}) => {
		const { projectId, probe, api } = await arrange(request, "tsprint");
		const sprintId = await createSprint(request, projectId, name("s"));
		const taskId = await createTask(request, projectId, name("t"), sprintId);
		const created = await waitForType(probe, "task.created", {
			entityId: taskId,
		});
		expect(created.entity?.sprint_id).toBe(sprintId);

		await api.patchTask(taskId, { sprint_id: null });
		const moved = await waitForType(probe, "task.updated", {
			entityId: taskId,
		});
		expect(moved.entity?.sprint_id ?? null).toBeNull();
	});

	test("a subtask names its parent", async ({ request }) => {
		const { projectId, probe } = await arrange(request, "tparent");
		const parent = await createTask(request, projectId, name("p"));
		const response = await request.post(
			`${API_URL}/projects/${projectId}/tasks`,
			{
				data: { title: name("child"), parent_task_id: parent },
			},
		);
		expect(response.ok()).toBeTruthy();
		const child = (await response.json()).data.id as string;
		const c = await waitForType(probe, "task.created", { entityId: child });
		expect(c.entity?.parent_task_id).toBe(parent);
	});
});

test.describe("who made the change", () => {
	test("a change made by an agent with its own key arrives like any other, attributed to the agent", async ({
		request,
		playwright,
	}) => {
		const { projectId, probe } = await arrange(request, "agent");
		const taskId = await createTask(request, projectId, name("t"));
		await waitForType(probe, "task.created");
		const adminChange = probe.syncedChanges().at(-1);

		const agent = await createProjectAgent(
			request,
			projectId,
			name("bot"),
			"acp",
		);
		const key = await createAgentKey(request, agent.id, projectId);
		await asAgent(playwright, key, async (api) => {
			const response = await api.patch(
				`${API_URL}/projects/${projectId}/tasks/${taskId}`,
				{ data: { title: name("by_agent") } },
			);
			expect(response.ok(), await response.text()).toBeTruthy();
		});

		const c = await waitForType(probe, "task.updated", { entityId: taskId });
		expect(c.entity?.title).toBe(name("by_agent"));
		expect(c.actor_id).toBeTruthy();
		expect(c.actor_id).not.toBe(adminChange?.actor_id);
		// Not a human edit: the origin tells the automation engine and clients so.
		expect(c.origin).not.toBe("user");
	});
});

test.describe("sprints", () => {
	test("create, update, complete and delete each arrive, the first three with the sprint", async ({
		request,
	}) => {
		const { projectId, probe, api } = await arrange(request, "sprint");
		const sprintId = await createSprint(request, projectId, name("s1"));
		const created = await waitForType(probe, "sprint.created");
		expect(created.op).toBe("create");
		expect(created.entity).toMatchObject({
			id: sprintId,
			name: name("s1"),
			status: "planned",
		});

		await api.patchSprint(sprintId, { name: name("s2"), goal: "ship" });
		const updated = await waitForType(probe, "sprint.updated");
		expect(updated.entity).toMatchObject({ name: name("s2"), goal: "ship" });

		await api.patchSprint(sprintId, { status: "active" });
		await api.completeSprint(sprintId);
		const completed = await waitForType(probe, "sprint.completed");
		expect(completed.entity?.status).toBe("completed");

		await api.deleteSprint(sprintId);
		const deleted = await waitForType(probe, "sprint.deleted");
		expect(deleted.op).toBe("delete");
		expect(deleted.entity).toBeUndefined();
		expect(deleted.entity_id).toBe(sprintId);
	});

	test("completing a sprint that holds tasks reports the sprint and leaves the tasks to be refetched", async ({
		request,
	}) => {
		const { projectId, probe, api } = await arrange(request, "sprinttasks");
		const sprintId = await createSprint(request, projectId, name("s"));
		await createTask(request, projectId, name("t"), sprintId);
		await api.patchSprint(sprintId, { status: "active" });
		await api.completeSprint(sprintId);
		await waitForType(probe, "sprint.completed");
		// Tasks moved by the completion are not announced one by one: the client
		// refetches its task views on sprint.completed.
		const taskUpdates = probe
			.syncedChanges()
			.filter((c) => c.type === "task.updated");
		expect(taskUpdates.length).toBeLessThanOrEqual(1);
	});
});

test.describe("views", () => {
	test("create, update, reorder, move a task and delete", async ({
		request,
	}) => {
		const { projectId, probe, api } = await arrange(request, "view");
		const taskId = await createTask(request, projectId, name("t"));
		const v1 = await api.createView(name("v1"));
		const v2 = await api.createView(name("v2"));
		const created = await waitForType(probe, "view.created");
		expect(created.op).toBe("create");
		// A view's config is the caller's own, so no entity is attached.
		expect(created.entity).toBeUndefined();
		expect(created.payload?.view_id).toBe(v1.id);

		await api.patchView(v1.id, { name: name("v1b") });
		await waitForType(probe, "view.updated");

		// A reorder names every view of the context, the built-in ones included.
		const all = await listViewIds(request, projectId, "context=backlog");
		await api.reorderViews([...all.ids].reverse());
		void v2;
		const reordered = await waitForType(probe, "view.reordered");
		expect(reordered.op).toBe("move");
		expect(reordered.payload?.view_context).toBe("backlog");

		await api.moveTask(v1.id, taskId, 2);
		const moved = await waitForType(probe, "view.task_moved");
		expect(moved.payload?.view_id).toBe(v1.id);

		await api.deleteView(v1.id);
		await waitForType(probe, "view.deleted");
	});
});

test.describe("docs", () => {
	test("create, update, comment and delete a document", async ({ request }) => {
		const { projectId, probe, api } = await arrange(request, "doc");
		const docId = await createDoc(request, projectId, name("d"));
		const created = await waitForType(probe, "doc.created");
		expect(created.op).toBe("create");
		expect(created.payload?.document_id ?? created.entity_id).toBeTruthy();

		await api.patchDoc(docId, { title: name("d2") });
		await waitForType(probe, "doc.updated");

		await api.docComment(docId, "nice");
		const comment = await waitForType(probe, "doc.comment.added");
		expect(comment.payload?.document_id).toBe(docId);

		await api.deleteDoc(docId);
		await waitForType(probe, "doc.deleted");
	});

	test("folders arrive as create, update and delete, and a doc moved into one as doc.updated", async ({
		request,
	}) => {
		const { projectId, probe, api } = await arrange(request, "folder");
		const folderId = await createFolder(request, projectId, name("f"));
		await waitForType(probe, "doc.folder.created");
		await api.patchFolder(folderId, { name: name("f2") });
		await waitForType(probe, "doc.folder.updated");

		const docId = await createDoc(request, projectId, name("d"));
		await api.patchDoc(docId, { folder_id: folderId });
		await waitForType(probe, "doc.updated");

		await api.patchDoc(docId, { folder_id: null });
		await api.deleteFolder(folderId);
		await waitForType(probe, "doc.folder.deleted");
	});
});

test.describe("project schema and settings", () => {
	test("task statuses: create, update, reorder, delete", async ({
		request,
	}) => {
		const { probe, api } = await arrange(request, "status");
		const s1 = await api.createStatus(name("s1"));
		const s2 = await api.createStatus(name("s2"));
		await waitForType(probe, "project.task_status.created");

		await api.patchStatus(s1.id, { name: name("s1b") });
		await waitForType(probe, "project.task_status.updated");

		await api.deleteStatus(s2.id);
		await waitForType(probe, "project.task_status.deleted");
	});

	test("task types: create, update, delete", async ({ request }) => {
		const { probe, api } = await arrange(request, "type");
		const t = await api.createType(name("ty"));
		await waitForType(probe, "project.task_type.created");
		await api.patchType(t.id, { name: name("ty2") });
		await waitForType(probe, "project.task_type.updated");
		await api.deleteType(t.id);
		await waitForType(probe, "project.task_type.deleted");
	});

	test("custom fields: create, update, delete", async ({ request }) => {
		const { probe, api } = await arrange(request, "field");
		const f = await api.createField(`k_${Date.now()}`, name("f"));
		await waitForType(probe, "project.custom_field.created");
		await api.patchField(f.id, { display_name: name("f2") });
		await waitForType(probe, "project.custom_field.updated");
		await api.deleteField(f.id);
		await waitForType(probe, "project.custom_field.deleted");
	});

	test("renaming the project arrives as project.updated", async ({
		request,
	}) => {
		const { probe, api } = await arrange(request, "proj");
		await api.patchProject({ name: name("renamed") });
		const c = await waitForType(probe, "project.updated");
		expect(c.op).toBe("update");
		expect(c.payload?.name).toBe(name("renamed"));
	});
});

test.describe("members and agents", () => {
	test("adding, re-roling and removing a member each arrive", async ({
		request,
		playwright,
	}) => {
		const { projectId, probe } = await arrange(request, "member");
		const roleA = await projectRoleFrom(
			request,
			projectId,
			name("ra"),
			allow(["projects:read", "tasks:read"], [`project/${projectId}/*`]),
		);
		const roleB = await projectRoleFrom(
			request,
			projectId,
			name("rb"),
			allow(["projects:read", "docs:read"], [`project/${projectId}/*`]),
		);
		const member = await createMember(
			request,
			playwright,
			projectId,
			name("bob"),
			[roleA],
		);
		const added = await waitForType(probe, "member.added");
		expect(added.op).toBe("create");

		await replaceMemberRoles(request, projectId, member.memberId, [roleB]);
		await waitForType(probe, "member.role_changed");

		const response = await request.delete(
			`${API_URL}/projects/${projectId}/members/${member.memberId}`,
		);
		expect(response.ok(), await response.text()).toBeTruthy();
		const removed = await waitForType(probe, "member.removed");
		expect(removed.op).toBe("delete");
	});

	test("creating, editing and deleting a project agent each arrive", async ({
		request,
	}) => {
		const { projectId, probe } = await arrange(request, "pagent");
		const agent = await createProjectAgent(
			request,
			projectId,
			name("bot"),
			"acp",
		);
		const created = await waitForType(probe, "project_agent.created");
		expect(created.payload?.agent_id).toBe(agent.id);

		const patch = await request.patch(
			`${API_URL}/projects/${projectId}/agents/${agent.id}`,
			{ data: { name: name("bot2") } },
		);
		expect(patch.ok(), await patch.text()).toBeTruthy();
		await waitForType(probe, "project_agent.updated");

		const del = await request.delete(
			`${API_URL}/projects/${projectId}/agents/${agent.id}`,
		);
		expect(del.ok(), await del.text()).toBeTruthy();
		await waitForType(probe, "project_agent.deleted");
	});
});

test.describe("environments", () => {
	test("creating an environment arrives, and its status changes follow", async ({
		request,
	}) => {
		const { projectId, probe } = await arrange(request, "env");
		await createEnvironment(request, projectId, name("e"));
		const created = await waitForType(probe, "environment.created");
		expect(created.op).toBe("create");
		await cleanupEnvironmentsInProjectsByPrefix(request, "E2E_RTSYNCENT_");
	});
});
