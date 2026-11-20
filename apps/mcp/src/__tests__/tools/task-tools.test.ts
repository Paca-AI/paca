import { describe, expect, it, vi } from "vitest";

vi.mock("../../utils/index.js", () => ({
	formatTask: vi.fn((t: any) => `task:${t.id}`),
	formatTaskDetail: vi.fn(() => "task-detail"),
	formatList: vi.fn((items: any[], fn: any) => items.map(fn).join("---")),
}));

import { getTaskTools, handleTaskTool } from "../../tools/task-tools.js";

const task = {
	id: "t1",
	project_id: "p1",
	title: "Do something",
	task_number: 1,
	importance: 2,
	custom_fields: {},
	created_at: "2024-01-01T00:00:00Z",
	updated_at: "2024-01-01T00:00:00Z",
};

const EMPTY_PAGE = { items: [], nextCursor: null };

function makeApiClient(overrides: Record<string, any> = {}) {
	return {
		listTasks: vi.fn().mockResolvedValue({ items: [task], nextCursor: null }),
		getTask: vi.fn().mockResolvedValue(task),
		getTaskByNumber: vi.fn().mockResolvedValue(task),
		createTask: vi.fn().mockResolvedValue(task),
		updateTask: vi.fn().mockResolvedValue({ ...task, title: "Updated" }),
		deleteTask: vi.fn().mockResolvedValue(undefined),
		getProject: vi.fn().mockResolvedValue({ id: "p1", name: "P" }),
		getSprint: vi.fn().mockResolvedValue(undefined),
		...overrides,
	} as any;
}

function makeExtendedClient(overrides: Record<string, any> = {}) {
	return {
		listTaskStatuses: vi.fn().mockResolvedValue([]),
		listTaskTypes: vi.fn().mockResolvedValue([]),
		listProjectMembers: vi.fn().mockResolvedValue(EMPTY_PAGE),
		listSubtasks: vi.fn().mockResolvedValue(EMPTY_PAGE),
		listTaskActivities: vi.fn().mockResolvedValue(EMPTY_PAGE),
		listTaskLinks: vi.fn().mockResolvedValue(EMPTY_PAGE),
		...overrides,
	} as any;
}

function makeViewsClient(overrides: Record<string, any> = {}) {
	return {
		listTaskAttachments: vi.fn().mockResolvedValue(EMPTY_PAGE),
		listCustomFieldDefinitions: vi.fn().mockResolvedValue([]),
		...overrides,
	} as any;
}

// ---------------------------------------------------------------------------
// getTaskTools
// ---------------------------------------------------------------------------

describe("getTaskTools", () => {
	it("returns 6 tools", () => {
		expect(getTaskTools()).toHaveLength(6);
	});

	it("includes all expected tool names", () => {
		const names = getTaskTools().map((t) => t.name);
		for (const n of [
			"list_tasks",
			"get_task",
			"get_task_by_number",
			"create_task",
			"update_task",
			"delete_task",
		]) {
			expect(names).toContain(n);
		}
	});
});

// ---------------------------------------------------------------------------
// list_tasks
// ---------------------------------------------------------------------------

describe("handleTaskTool – list_tasks", () => {
	it("calls client.listTasks with projectId and optional pagination/filter params", async () => {
		const client = makeApiClient();
		await handleTaskTool("list_tasks", { projectId: "p1" }, client);
		expect(client.listTasks).toHaveBeenCalledWith("p1", {
			cursor: undefined,
			pageSize: undefined,
			sprintId: undefined,
			statusId: undefined,
			assigneeId: undefined,
			taskTypeIds: undefined,
			parentTaskId: undefined,
		});
	});

	it("passes cursor and pageSize when provided", async () => {
		const client = makeApiClient();
		await handleTaskTool(
			"list_tasks",
			{ projectId: "p1", cursor: "abc", pageSize: 10 },
			client,
		);
		expect(client.listTasks).toHaveBeenCalledWith(
			"p1",
			expect.objectContaining({ cursor: "abc", pageSize: 10 }),
		);
	});

	it("passes filter params when provided", async () => {
		const client = makeApiClient();
		await handleTaskTool(
			"list_tasks",
			{ projectId: "p1", sprintId: "s1", statusId: "st1", assigneeId: "u1" },
			client,
		);
		expect(client.listTasks).toHaveBeenCalledWith(
			"p1",
			expect.objectContaining({
				sprintId: "s1",
				statusId: "st1",
				assigneeId: "u1",
			}),
		);
	});

	it("passes taskTypeIds and parentTaskId when provided", async () => {
		const client = makeApiClient();
		await handleTaskTool(
			"list_tasks",
			{ projectId: "p1", taskTypeIds: ["ty1", "ty2"], parentTaskId: "t0" },
			client,
		);
		expect(client.listTasks).toHaveBeenCalledWith(
			"p1",
			expect.objectContaining({
				taskTypeIds: ["ty1", "ty2"],
				parentTaskId: "t0",
			}),
		);
	});

	it("includes task count in response", async () => {
		const result = await handleTaskTool(
			"list_tasks",
			{ projectId: "p1" },
			makeApiClient(),
		);
		expect(result.content[0].text).toContain("Tasks (1 returned");
	});

	it("adds pagination hint when nextCursor is present", async () => {
		const client = makeApiClient({
			listTasks: vi
				.fn()
				.mockResolvedValue({ items: [task], nextCursor: "next-page" }),
		});
		const result = await handleTaskTool(
			"list_tasks",
			{ projectId: "p1" },
			client,
		);
		expect(result.content[0].text).toContain("next-page");
		expect(result.content[0].text).toContain("more available");
	});

	it("puts the exact cursor before the task list and keeps total", async () => {
		const cursor = `eyJjYSI6${"A".repeat(300)}jIn0=`;
		const longTask = { ...task, description: "x".repeat(50_000) };
		const client = makeApiClient({
			listTasks: vi.fn().mockResolvedValue({
				items: Array.from({ length: 100 }, () => longTask),
				nextCursor: cursor,
				total: 370,
			}),
		});
		const text = (
			await handleTaskTool(
				"list_tasks",
				{ projectId: "p1", pageSize: 100 },
				client,
			)
		).content[0].text as string;
		expect(text).toContain("370 total");
		expect(text).toContain(`next_cursor: ${cursor}`);
		expect(text.indexOf(cursor)).toBeLessThan(text.indexOf("Task #1"));
		expect(text).not.toContain("xxxx");
	});

	it("includes descriptions only when includeDescription is true", async () => {
		const client = makeApiClient();
		const compact = await handleTaskTool(
			"list_tasks",
			{ projectId: "p1" },
			client,
		);
		expect(compact.content[0].text).toContain("Task #1: Do something");
		expect(compact.content[0].text).not.toContain("task:t1");
		const full = await handleTaskTool(
			"list_tasks",
			{ projectId: "p1", includeDescription: true },
			client,
		);
		expect(full.content[0].text).toContain("task:t1");
	});

	it("omits next_cursor on the last page", async () => {
		const result = await handleTaskTool(
			"list_tasks",
			{ projectId: "p1" },
			makeApiClient(),
		);
		expect(result.content[0].text).not.toContain("next_cursor");
	});

	it("throws ZodError when projectId is missing", async () => {
		await expect(
			handleTaskTool("list_tasks", {}, makeApiClient()),
		).rejects.toThrow();
	});
});

// ---------------------------------------------------------------------------
// get_task
// ---------------------------------------------------------------------------

describe("handleTaskTool – get_task", () => {
	it("calls client.getTask with projectId and taskId", async () => {
		const client = makeApiClient();
		await handleTaskTool(
			"get_task",
			{ projectId: "p1", taskId: "t1" },
			client,
			makeExtendedClient(),
			makeViewsClient(),
		);
		expect(client.getTask).toHaveBeenCalledWith("p1", "t1");
	});

	it("fetches supporting data in parallel", async () => {
		const extClient = makeExtendedClient();
		const viewClient = makeViewsClient();
		const client = makeApiClient();
		await handleTaskTool(
			"get_task",
			{ projectId: "p1", taskId: "t1" },
			client,
			extClient,
			viewClient,
		);
		expect(extClient.listTaskStatuses).toHaveBeenCalledWith("p1");
		expect(extClient.listTaskTypes).toHaveBeenCalledWith("p1");
		expect(viewClient.listTaskAttachments).toHaveBeenCalledWith("p1", "t1", {
			pageSize: 20,
		});
	});

	it("loads only the people the task names, by id, not the whole team", async () => {
		const extClient = makeExtendedClient();
		const client = makeApiClient({
			getTask: vi.fn().mockResolvedValue({
				...task,
				assignee_ids: ["m1"],
				reporter_id: "m2",
			}),
		});
		await handleTaskTool(
			"get_task",
			{ projectId: "p1", taskId: "t1" },
			client,
			extClient,
			makeViewsClient(),
		);
		expect(extClient.listProjectMembers).toHaveBeenCalledTimes(1);
		expect(extClient.listProjectMembers).toHaveBeenCalledWith("p1", {
			ids: ["m1", "m2"],
		});
	});

	it("says how to read the rest of a section that has more pages", async () => {
		const extClient = makeExtendedClient({
			listSubtasks: vi
				.fn()
				.mockResolvedValue({ items: [], nextCursor: "c2", total: 45 }),
		});
		const result = await handleTaskTool(
			"get_task",
			{ projectId: "p1", taskId: "t1" },
			makeApiClient(),
			extClient,
			makeViewsClient(),
		);
		const text = result.content[0].text;
		expect(text).toContain("Subtasks");
		expect(text).toContain('cursor="c2"');
		expect(extClient.listSubtasks).toHaveBeenCalledTimes(1);
	});

	it("returns task-detail formatted text", async () => {
		const result = await handleTaskTool(
			"get_task",
			{ projectId: "p1", taskId: "t1" },
			makeApiClient(),
			makeExtendedClient(),
			makeViewsClient(),
		);
		expect(result.content[0].text).toContain("task-detail");
	});

	it("fetches parent task when task has parent_task_id", async () => {
		const taskWithParent = { ...task, parent_task_id: "parent-1" };
		const client = makeApiClient({
			getTask: vi
				.fn()
				.mockResolvedValueOnce(taskWithParent)
				.mockResolvedValueOnce({ ...task, id: "parent-1", title: "Parent" }),
		});
		await handleTaskTool(
			"get_task",
			{ projectId: "p1", taskId: "t1" },
			client,
			makeExtendedClient(),
			makeViewsClient(),
		);
		expect(client.getTask).toHaveBeenCalledWith("p1", "parent-1");
	});

	it("tolerates failure fetching parent task gracefully", async () => {
		const taskWithParent = { ...task, parent_task_id: "bad-parent" };
		const client = makeApiClient({
			getTask: vi
				.fn()
				.mockResolvedValueOnce(taskWithParent)
				.mockRejectedValueOnce(new Error("not found")),
		});
		const result = await handleTaskTool(
			"get_task",
			{ projectId: "p1", taskId: "t1" },
			client,
			makeExtendedClient(),
			makeViewsClient(),
		);
		expect(result.content[0].text).toBeDefined();
	});
});

// ---------------------------------------------------------------------------
// get_task_by_number
// ---------------------------------------------------------------------------

describe("handleTaskTool – get_task_by_number", () => {
	it("calls client.getTaskByNumber and then fetches full detail", async () => {
		const client = makeApiClient();
		await handleTaskTool(
			"get_task_by_number",
			{ projectId: "p1", taskNumber: 42 },
			client,
			makeExtendedClient(),
			makeViewsClient(),
		);
		expect(client.getTaskByNumber).toHaveBeenCalledWith("p1", 42);
		expect(client.getTask).toHaveBeenCalledWith("p1", "t1");
	});
});

// ---------------------------------------------------------------------------
// create_task
// ---------------------------------------------------------------------------

describe("handleTaskTool – create_task", () => {
	it("calls client.createTask with mapped fields", async () => {
		const client = makeApiClient();
		await handleTaskTool(
			"create_task",
			{
				projectId: "p1",
				title: "New Task",
				statusId: "s1",
				typeId: "ty1",
				sprintId: "sp1",
				assigneeId: "u1",
				importance: 3,
				storyPoints: 5,
				tags: ["bug"],
				startDate: "2024-01-01",
				dueDate: "2024-01-15",
			},
			client,
		);
		expect(client.createTask).toHaveBeenCalledWith({
			project_id: "p1",
			title: "New Task",
			description: undefined,
			status_id: "s1",
			task_type_id: "ty1",
			sprint_id: "sp1",
			assignee_id: "u1",
			parent_task_id: undefined,
			importance: 3,
			story_points: 5,
			tags: ["bug"],
			start_date: "2024-01-01",
			due_date: "2024-01-15",
		});
	});

	it("includes 'created successfully' in response", async () => {
		const result = await handleTaskTool(
			"create_task",
			{ projectId: "p1", title: "New Task" },
			makeApiClient(),
		);
		expect(result.content[0].text).toContain("created successfully");
	});

	it("throws ZodError when projectId or title is missing", async () => {
		await expect(
			handleTaskTool("create_task", { projectId: "p1" }, makeApiClient()),
		).rejects.toThrow();
	});
});

// ---------------------------------------------------------------------------
// update_task
// ---------------------------------------------------------------------------

describe("handleTaskTool – update_task", () => {
	it("calls client.updateTask with projectId, taskId, and mapped fields", async () => {
		const client = makeApiClient();
		await handleTaskTool(
			"update_task",
			{ projectId: "p1", taskId: "t1", title: "Renamed", statusId: "done" },
			client,
		);
		expect(client.updateTask).toHaveBeenCalledWith("p1", "t1", {
			title: "Renamed",
			description: undefined,
			status_id: "done",
			task_type_id: undefined,
			sprint_id: undefined,
			assignee_id: undefined,
			parent_task_id: undefined,
			importance: undefined,
			story_points: undefined,
			tags: undefined,
			start_date: undefined,
			due_date: undefined,
		});
	});

	it("passes sprintId null through to move the task to the backlog", async () => {
		const client = makeApiClient();
		await handleTaskTool(
			"update_task",
			{ projectId: "p1", taskId: "t1", sprintId: null },
			client,
		);
		expect(client.updateTask).toHaveBeenCalledWith(
			"p1",
			"t1",
			expect.objectContaining({ sprint_id: null }),
		);
	});

	it("passes null through for assigneeId, startDate and dueDate so they can be cleared", async () => {
		const client = makeApiClient();
		await handleTaskTool(
			"update_task",
			{
				projectId: "p1",
				taskId: "t1",
				assigneeId: null,
				startDate: null,
				dueDate: null,
			},
			client,
		);
		expect(client.updateTask).toHaveBeenCalledWith(
			"p1",
			"t1",
			expect.objectContaining({
				assignee_id: null,
				start_date: null,
				due_date: null,
			}),
		);
	});

	it("includes 'updated successfully' in response", async () => {
		const result = await handleTaskTool(
			"update_task",
			{ projectId: "p1", taskId: "t1" },
			makeApiClient(),
		);
		expect(result.content[0].text).toContain("updated successfully");
	});
});

// ---------------------------------------------------------------------------
// delete_task
// ---------------------------------------------------------------------------

describe("handleTaskTool – delete_task", () => {
	it("calls client.deleteTask with projectId and taskId", async () => {
		const client = makeApiClient();
		await handleTaskTool(
			"delete_task",
			{ projectId: "p1", taskId: "t1" },
			client,
		);
		expect(client.deleteTask).toHaveBeenCalledWith("p1", "t1");
	});

	it("includes 'deleted successfully' and taskId in response", async () => {
		const result = await handleTaskTool(
			"delete_task",
			{ projectId: "p1", taskId: "t1" },
			makeApiClient(),
		);
		expect(result.content[0].text).toContain("t1");
		expect(result.content[0].text).toContain("deleted successfully");
	});
});

// ---------------------------------------------------------------------------
// unknown tool
// ---------------------------------------------------------------------------

describe("handleTaskTool – unknown tool", () => {
	it("throws for an unknown tool name", async () => {
		await expect(
			handleTaskTool("nonexistent", {}, makeApiClient()),
		).rejects.toThrow("Unknown task tool");
	});
});
