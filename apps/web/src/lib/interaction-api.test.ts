import { beforeEach, describe, expect, it, vi } from "vitest";

const { mockGet, mockPost, mockPatch, mockDelete } = vi.hoisted(() => ({
	mockGet: vi.fn(),
	mockPost: vi.fn(),
	mockPatch: vi.fn(),
	mockDelete: vi.fn(),
}));

vi.mock("./api-client", () => ({
	apiClient: {
		instance: {
			get: mockGet,
			post: mockPost,
			patch: mockPatch,
			delete: mockDelete,
		},
	},
}));

import {
	createTask,
	deleteTask,
	getTask,
	layoutToViewType,
	listAllTasks,
	listSprintTasks,
	type Task,
	updateTask,
} from "./interaction-api";

// ── Fixtures ──────────────────────────────────────────────────────────────────

const PROJECT_ID = "proj-1";
const TASK_ID = "task-1";
const SPRINT_ID = "sprint-1";
const STATUS_ID = "status-2";

const makeTask = (overrides: Partial<Task> = {}): Task => ({
	id: TASK_ID,
	project_id: PROJECT_ID,
	title: "Test Task",
	task_number: 0,
	sprint_id: SPRINT_ID,
	status_id: null,
	task_type_id: null,
	parent_task_id: null,
	description: null,
	importance: 0,
	assignee_ids: [],
	reporter_id: null,
	custom_fields: {},
	view_position: null,
	view_group_key: null,
	created_at: "2026-01-01T00:00:00Z",
	updated_at: "2026-01-01T00:00:00Z",
	...overrides,
});

function ok<T>(data: T) {
	return { data: { data, success: true } };
}

// ── Tests ─────────────────────────────────────────────────────────────────────

describe("interaction-api", () => {
	beforeEach(() => {
		vi.clearAllMocks();
	});

	// ── getTask ───────────────────────────────────────────────────────────────

	describe("getTask", () => {
		it("fetches the task by id and unwraps it", async () => {
			const task = makeTask();
			mockGet.mockResolvedValue(ok(task));

			await expect(getTask(PROJECT_ID, TASK_ID)).resolves.toEqual(task);
			expect(mockGet).toHaveBeenCalledWith(
				`/projects/${PROJECT_ID}/tasks/${TASK_ID}`,
			);
		});
	});

	// ── createTask ────────────────────────────────────────────────────────────

	describe("createTask", () => {
		it("posts the payload and returns the created task", async () => {
			const task = makeTask({ title: "New Task" });
			mockPost.mockResolvedValue(ok(task));

			const result = await createTask(PROJECT_ID, { title: "New Task" });
			expect(result).toEqual(task);
			expect(mockPost).toHaveBeenCalledWith(`/projects/${PROJECT_ID}/tasks`, {
				title: "New Task",
			});
		});

		it("includes sprint_id and status_id when provided", async () => {
			mockPost.mockResolvedValue(ok(makeTask()));

			await createTask(PROJECT_ID, {
				title: "Sprint Task",
				sprint_id: SPRINT_ID,
				status_id: STATUS_ID,
			});

			expect(mockPost).toHaveBeenCalledWith(`/projects/${PROJECT_ID}/tasks`, {
				title: "Sprint Task",
				sprint_id: SPRINT_ID,
				status_id: STATUS_ID,
			});
		});
	});

	// ── updateTask ────────────────────────────────────────────────────────────

	describe("updateTask", () => {
		it("sends only the provided fields — status_id only should not include sprint_id", async () => {
			const updated = makeTask({ status_id: STATUS_ID });
			mockPatch.mockResolvedValue(ok(updated));

			const result = await updateTask(PROJECT_ID, TASK_ID, {
				status_id: STATUS_ID,
			});
			expect(result).toEqual(updated);

			const patchedPayload = mockPatch.mock.calls[0][1] as Record<
				string,
				unknown
			>;
			// The payload must contain the status_id we sent
			expect(patchedPayload).toMatchObject({ status_id: STATUS_ID });
		});

		it("sends sprint_id when explicitly provided alongside status_id", async () => {
			const updated = makeTask({ status_id: STATUS_ID, sprint_id: SPRINT_ID });
			mockPatch.mockResolvedValue(ok(updated));

			await updateTask(PROJECT_ID, TASK_ID, {
				status_id: STATUS_ID,
				sprint_id: SPRINT_ID,
			});

			const patchedPayload = mockPatch.mock.calls[0][1] as Record<
				string,
				unknown
			>;
			expect(patchedPayload).toMatchObject({
				status_id: STATUS_ID,
				sprint_id: SPRINT_ID,
			});
		});

		it("allows explicitly setting sprint_id to null to move task to backlog", async () => {
			const updated = makeTask({ sprint_id: null });
			mockPatch.mockResolvedValue(ok(updated));

			await updateTask(PROJECT_ID, TASK_ID, { sprint_id: null });

			const patchedPayload = mockPatch.mock.calls[0][1] as Record<
				string,
				unknown
			>;
			expect(patchedPayload).toMatchObject({ sprint_id: null });
		});

		it("calls the correct API path", async () => {
			mockPatch.mockResolvedValue(ok(makeTask()));
			await updateTask(PROJECT_ID, TASK_ID, { status_id: STATUS_ID });
			expect(mockPatch).toHaveBeenCalledWith(
				`/projects/${PROJECT_ID}/tasks/${TASK_ID}`,
				expect.any(Object),
			);
		});
	});

	// ── deleteTask ────────────────────────────────────────────────────────────

	describe("deleteTask", () => {
		it("sends a DELETE request to the correct path and resolves void", async () => {
			mockDelete.mockResolvedValue({ data: { data: null, success: true } });

			await expect(deleteTask(PROJECT_ID, TASK_ID)).resolves.toBeUndefined();
			expect(mockDelete).toHaveBeenCalledWith(
				`/projects/${PROJECT_ID}/tasks/${TASK_ID}`,
			);
		});

		it("does not send a request body", async () => {
			mockDelete.mockResolvedValue({ data: { data: null, success: true } });

			await deleteTask(PROJECT_ID, TASK_ID);

			expect(mockDelete).toHaveBeenCalledTimes(1);
			expect(mockDelete).toHaveBeenCalledWith(
				`/projects/${PROJECT_ID}/tasks/${TASK_ID}`,
			);
		});
	});

	// ── listSprintTasks ───────────────────────────────────────────────────────

	describe("listSprintTasks", () => {
		it("fetches tasks for a sprint and returns TaskListResult", async () => {
			const task = makeTask();
			mockGet.mockResolvedValue(
				ok({ items: [task], total: 1, page: 1, page_size: 200 }),
			);

			const result = await listSprintTasks(PROJECT_ID, SPRINT_ID);
			expect(result).toEqual({
				items: [task],
				total: 1,
				page: 1,
				page_size: 200,
			});
			expect(mockGet).toHaveBeenCalledWith(
				`/projects/${PROJECT_ID}/tasks`,
				expect.objectContaining({
					params: expect.objectContaining({
						sprint_id: SPRINT_ID,
					}),
				}),
			);
			const [, config] = mockGet.mock.calls[0] as [
				string,
				{ params: Record<string, unknown> },
			];
			expect(config.params?.context).toBeUndefined();
			expect(config.params?.view_id).toBeUndefined();
			expect(config.params?.backlog).toBeUndefined();
		});
	});

	describe("listAllTasks", () => {
		it("passes only explicit filtering params through the single tasks endpoint", async () => {
			mockGet.mockResolvedValue(
				ok({ items: [], total: 0, page: 1, page_size: 200 }),
			);

			await listAllTasks(PROJECT_ID, {
				sprintId: null,
				sprintIds: ["sprint-a", "sprint-b"],
				statusIds: ["status-a", "status-b"],
				assigneeIds: ["user-1"],
				taskTypeIds: ["epic-type"],
			});

			expect(mockGet).toHaveBeenCalledWith(
				`/projects/${PROJECT_ID}/tasks`,
				expect.objectContaining({
					params: expect.objectContaining({
						sprint_id: "null",
						sprint_ids: "sprint-a,sprint-b",
						status_ids: "status-a,status-b",
						assignee_ids: "user-1",
						task_type_ids: "epic-type",
					}),
				}),
			);
			const [, config] = mockGet.mock.calls[0] as [
				string,
				{ params: Record<string, unknown> },
			];
			expect(config.params?.context).toBeUndefined();
			expect(config.params?.view_id).toBeUndefined();
			expect(config.params?.backlog).toBeUndefined();
		});

		it("includes a trimmed search param when provided", async () => {
			mockGet.mockResolvedValue(
				ok({ items: [], total: 0, page: 1, page_size: 200 }),
			);

			await listAllTasks(PROJECT_ID, { search: "  login bug  " });

			expect(mockGet).toHaveBeenCalledWith(
				`/projects/${PROJECT_ID}/tasks`,
				expect.objectContaining({
					params: expect.objectContaining({ search: "login bug" }),
				}),
			);
		});

		it("omits the search param when blank or whitespace-only", async () => {
			mockGet.mockResolvedValue(
				ok({ items: [], total: 0, page: 1, page_size: 200 }),
			);

			await listAllTasks(PROJECT_ID, { search: "   " });

			const [, config] = mockGet.mock.calls[0] as [
				string,
				{ params: Record<string, unknown> },
			];
			expect(config.params?.search).toBeUndefined();
		});

		it("serializes customFieldFilters as a single JSON query param", async () => {
			mockGet.mockResolvedValue(
				ok({ items: [], total: 0, page: 1, page_size: 200 }),
			);

			await listAllTasks(PROJECT_ID, {
				customFieldFilters: {
					priority: { values: ["High", "Urgent"] },
					effort: { min: 2, max: 8 },
				},
			});

			const [, config] = mockGet.mock.calls[0] as [
				string,
				{ params: Record<string, unknown> },
			];
			expect(config.params?.custom_field_filters).toBe(
				JSON.stringify({
					priority: { values: ["High", "Urgent"] },
					effort: { min: 2, max: 8 },
				}),
			);
		});

		it("omits custom_field_filters when customFieldFilters is empty or unset", async () => {
			mockGet.mockResolvedValue(
				ok({ items: [], total: 0, page: 1, page_size: 200 }),
			);

			await listAllTasks(PROJECT_ID, { customFieldFilters: {} });

			const [, config] = mockGet.mock.calls[0] as [
				string,
				{ params: Record<string, unknown> },
			];
			expect(config.params?.custom_field_filters).toBeUndefined();
		});

		it("serializes the built-in date/story-points/importance/tags filters", async () => {
			mockGet.mockResolvedValue(
				ok({ items: [], total: 0, page: 1, page_size: 200 }),
			);

			await listAllTasks(PROJECT_ID, {
				startDateAfter: "2024-01-01",
				startDateBefore: "2024-06-01",
				dueDateAfter: "2024-02-01",
				dueDateBefore: "2024-07-01",
				storyPointsMin: 2,
				storyPointsMax: 8,
				importanceRanges: [
					{ min: 1, max: 19 },
					{ min: 100, max: 2147483647 },
				],
				tags: ["urgent", "needs review"],
			});

			const [, config] = mockGet.mock.calls[0] as [
				string,
				{ params: Record<string, unknown> },
			];
			expect(config.params?.start_date_after).toBe("2024-01-01");
			expect(config.params?.start_date_before).toBe("2024-06-01");
			expect(config.params?.due_date_after).toBe("2024-02-01");
			expect(config.params?.due_date_before).toBe("2024-07-01");
			expect(config.params?.story_points_min).toBe(2);
			expect(config.params?.story_points_max).toBe(8);
			expect(config.params?.importance_ranges).toBe(
				JSON.stringify([
					{ min: 1, max: 19 },
					{ min: 100, max: 2147483647 },
				]),
			);
			expect(config.params?.tags).toBe("urgent,needs review");
		});

		it("omits the built-in filter params when unset or empty", async () => {
			mockGet.mockResolvedValue(
				ok({ items: [], total: 0, page: 1, page_size: 200 }),
			);

			await listAllTasks(PROJECT_ID, { importanceRanges: [], tags: [] });

			const [, config] = mockGet.mock.calls[0] as [
				string,
				{ params: Record<string, unknown> },
			];
			expect(config.params?.start_date_after).toBeUndefined();
			expect(config.params?.due_date_before).toBeUndefined();
			expect(config.params?.story_points_min).toBeUndefined();
			expect(config.params?.importance_ranges).toBeUndefined();
			expect(config.params?.tags).toBeUndefined();
		});
	});

	// ── layoutToViewType ─────────────────────────────────────────────────────

	describe("layoutToViewType", () => {
		it("maps Board → board", () => {
			expect(layoutToViewType("Board")).toBe("board");
		});
		it("maps Table → table", () => {
			expect(layoutToViewType("Table")).toBe("table");
		});
		it("maps Roadmap → roadmap", () => {
			expect(layoutToViewType("Roadmap")).toBe("roadmap");
		});
	});
});
