// Tests for board-view.tsx
// Key regression: DnD status-change mutation must always include sprint_id so tasks
// don't silently get moved to the product backlog when changing their status.

import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

// ── Mock @/lib/interaction-api before any imports that pull it in ─────────────

const { mockUpdateTask } = vi.hoisted(() => ({
	mockUpdateTask: vi.fn(),
}));

vi.mock("@/lib/interaction-api", () => ({
	updateTask: mockUpdateTask,
}));

import type { Task } from "@/lib/interaction-api";
import type { TaskStatus, TaskType } from "@/lib/project-api";
import { BoardView } from "./board-view";

// ── Fixtures ──────────────────────────────────────────────────────────────────

const PROJECT_ID = "proj-1";
const SPRINT_ID = "sprint-99";

const statuses: TaskStatus[] = [
	{
		id: "status-todo",
		project_id: PROJECT_ID,
		name: "Todo",
		color: "#aaa",
		position: 1,
		category: "todo" as const,
		created_at: "2026-01-01",
		updated_at: "2026-01-01",
	},
	{
		id: "status-done",
		project_id: PROJECT_ID,
		name: "Done",
		color: "#0f0",
		position: 2,
		category: "done",
		created_at: "2026-01-01",
		updated_at: "2026-01-01",
	},
];

const taskTypes: TaskType[] = [];

const makeTask = (overrides: Partial<Task> = {}): Task => ({
	id: "task-1",
	project_id: PROJECT_ID,
	title: "Do the thing",
	task_number: 0,
	sprint_id: SPRINT_ID,
	status_id: "status-todo",
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

// ── Helpers ───────────────────────────────────────────────────────────────────

function createDt() {
	const store: Record<string, string> = {};
	return {
		setData: (k: string, v: string) => {
			store[k] = v;
		},
		getData: (k: string) => store[k] ?? "",
		effectAllowed: "move" as DataTransfer["effectAllowed"],
		dropEffect: "move" as DataTransfer["dropEffect"],
	};
}

function wrapper({ children }: { children: React.ReactNode }) {
	const qc = new QueryClient({
		defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
	});
	return <QueryClientProvider client={qc}>{children}</QueryClientProvider>;
}

function renderBoard(
	tasks: Task[],
	overrides: Partial<Parameters<typeof BoardView>[0]> = {},
) {
	return render(
		<BoardView
			projectId={PROJECT_ID}
			tasks={tasks}
			statuses={statuses}
			taskTypes={taskTypes}
			canCreate={false}
			canEdit={true}
			tasksQueryKey={["tasks"]}
			onCreateTask={vi.fn()}
			onTaskClick={vi.fn()}
			{...overrides}
		/>,
		{ wrapper },
	);
}

// ── Tests ─────────────────────────────────────────────────────────────────────

beforeEach(() => {
	mockUpdateTask.mockClear();
	mockUpdateTask.mockResolvedValue({ data: { data: {}, success: true } });
});

describe("BoardView", () => {
	describe("rendering", () => {
		it("renders a column for each status", () => {
			renderBoard([]);
			expect(screen.getByText("Todo")).toBeInTheDocument();
			expect(screen.getByText("Done")).toBeInTheDocument();
		});

		it("renders task cards in the correct status column", () => {
			const task = makeTask({
				title: "Fix login bug",
				status_id: "status-todo",
			});
			renderBoard([task]);
			expect(screen.getByText("Fix login bug")).toBeInTheDocument();
		});

		it("renders zero for empty columns", () => {
			renderBoard([]);
			// each column header shows a count
			const counts = screen.getAllByText("0");
			expect(counts.length).toBeGreaterThanOrEqual(2);
		});

		it("shows 'No tasks' placeholder in empty columns", () => {
			renderBoard([]);
			const placeholders = screen.getAllByText("No tasks");
			expect(placeholders.length).toBeGreaterThanOrEqual(1);
		});

		it("renders multiple tasks in the same column", () => {
			const t1 = makeTask({
				id: "t1",
				title: "Task Alpha",
				status_id: "status-todo",
			});
			const t2 = makeTask({
				id: "t2",
				title: "Task Beta",
				status_id: "status-todo",
			});
			renderBoard([t1, t2]);
			expect(screen.getByText("Task Alpha")).toBeInTheDocument();
			expect(screen.getByText("Task Beta")).toBeInTheDocument();
		});

		it("renders tasks in different columns", () => {
			const t1 = makeTask({
				id: "t1",
				title: "Todo Task",
				status_id: "status-todo",
			});
			const t2 = makeTask({
				id: "t2",
				title: "Done Task",
				status_id: "status-done",
			});
			renderBoard([t1, t2]);
			expect(screen.getByText("Todo Task")).toBeInTheDocument();
			expect(screen.getByText("Done Task")).toBeInTheDocument();
		});
	});

	describe("drag and drop — status change", () => {
		it("calls updateTask with the task's sprint_id when dragged to a new column (regression)", async () => {
			// This is the core regression test: dragging from 'Todo' → 'Done' must preserve sprint_id.
			const task = makeTask({
				id: "task-sprint",
				title: "Sprint Task",
				status_id: "status-todo",
				sprint_id: SPRINT_ID,
			});
			const { container } = renderBoard([task]);

			const dt = createDt();
			const card = container.querySelector(
				"[data-task-id='task-sprint']",
			) as Element;

			// Simulate drag start on the task card
			fireEvent.dragStart(card, { dataTransfer: dt });

			// Find the "Done" column drop zone (the div wrapping Done column cards)
			const doneHeader = screen
				.getByText("Done")
				.closest('[class*="flex-col"]');
			expect(doneHeader).not.toBeNull();

			// Simulate dragOver then drop onto the Done column's drop zone container
			const doneColumn =
				(doneHeader as Element).querySelector('[class*="rounded-xl"]') ??
				(doneHeader as Element);
			fireEvent.dragOver(doneColumn, { dataTransfer: dt });
			fireEvent.drop(doneColumn, { dataTransfer: dt });

			await waitFor(() => {
				expect(mockUpdateTask).toHaveBeenCalledWith(PROJECT_ID, "task-sprint", {
					status_id: "status-done",
					sprint_id: SPRINT_ID,
				});
			});
		});

		it("calls updateTask with sprint_id=null for tasks without a sprint", async () => {
			const task = makeTask({
				id: "task-backlog",
				title: "Backlog Task",
				status_id: "status-todo",
				sprint_id: null,
			});
			const { container } = renderBoard([task]);

			const dt = createDt();
			const card = container.querySelector(
				"[data-task-id='task-backlog']",
			) as Element;
			fireEvent.dragStart(card, { dataTransfer: dt });

			const doneHeader = screen
				.getByText("Done")
				.closest('[class*="flex-col"]');
			const doneColumn =
				(doneHeader as Element).querySelector('[class*="rounded-xl"]') ??
				(doneHeader as Element);
			fireEvent.dragOver(doneColumn, { dataTransfer: dt });
			fireEvent.drop(doneColumn, { dataTransfer: dt });

			await waitFor(() => {
				expect(mockUpdateTask).toHaveBeenCalledWith(
					PROJECT_ID,
					"task-backlog",
					{
						status_id: "status-done",
						sprint_id: null,
					},
				);
			});
		});

		it("does NOT call updateTask when dropped on the same column", async () => {
			const task = makeTask({
				id: "task-same",
				title: "Same Column Task",
				status_id: "status-todo",
			});
			const { container } = renderBoard([task]);

			const dt = createDt();
			const card = container.querySelector(
				"[data-task-id='task-same']",
			) as Element;
			fireEvent.dragStart(card, { dataTransfer: dt });

			const todoHeader = screen
				.getByText("Todo")
				.closest('[class*="flex-col"]');
			const todoColumn =
				(todoHeader as Element).querySelector('[class*="rounded-xl"]') ??
				(todoHeader as Element);
			fireEvent.dragOver(todoColumn, { dataTransfer: dt });
			fireEvent.drop(todoColumn, { dataTransfer: dt });

			// Give time for any async mutation to fire
			await new Promise((r) => setTimeout(r, 50));
			expect(mockUpdateTask).not.toHaveBeenCalled();
		});

		it("does NOT call updateTask when canEdit=false", async () => {
			const task = makeTask({
				id: "task-readonly",
				title: "Readonly Task",
				status_id: "status-todo",
			});
			const { container } = renderBoard([task], { canEdit: false });

			const dt = createDt();
			const card = container.querySelector(
				"[data-task-id='task-readonly']",
			) as Element;
			fireEvent.dragStart(card, { dataTransfer: dt });

			const doneHeader = screen
				.getByText("Done")
				.closest('[class*="flex-col"]');
			const doneColumn =
				(doneHeader as Element).querySelector('[class*="rounded-xl"]') ??
				(doneHeader as Element);
			fireEvent.dragOver(doneColumn, { dataTransfer: dt });
			fireEvent.drop(doneColumn, { dataTransfer: dt });

			await new Promise((r) => setTimeout(r, 50));
			expect(mockUpdateTask).not.toHaveBeenCalled();
		});
	});

	describe("task click", () => {
		it("calls onTaskClick when a task card is clicked", async () => {
			const onTaskClick = vi.fn();
			const task = makeTask({
				title: "Clickable Task",
				status_id: "status-todo",
			});
			renderBoard([task], { onTaskClick });
			fireEvent.click(screen.getByText("Clickable Task"));
			expect(onTaskClick).toHaveBeenCalledWith(task);
		});
	});

	describe("column auto-fill pagination", () => {
		// jsdom never lays out real dimensions, so a column's scrollHeight and
		// clientHeight are both 0 — the same "not scrollable yet" state as a real
		// under-filled column — which is what lets these tests exercise the
		// auto-fill effect without needing to mock element sizes.

		it("requests the next page on mount when a column has more but isn't scrollable", async () => {
			const onLoadMore = vi.fn();
			renderBoard([], {
				columnPagination: {
					"status-todo": { hasMore: true, isLoadingMore: false, onLoadMore },
				},
			});
			await waitFor(() => expect(onLoadMore).toHaveBeenCalled());
		});

		it("does not request another page while one is already loading", async () => {
			const onLoadMore = vi.fn();
			renderBoard([], {
				columnPagination: {
					"status-todo": { hasMore: true, isLoadingMore: true, onLoadMore },
				},
			});
			await new Promise((r) => setTimeout(r, 0));
			expect(onLoadMore).not.toHaveBeenCalled();
		});

		it("does not request another page once there's nothing left to load", async () => {
			const onLoadMore = vi.fn();
			renderBoard([], {
				columnPagination: {
					"status-todo": { hasMore: false, isLoadingMore: false, onLoadMore },
				},
			});
			await new Promise((r) => setTimeout(r, 0));
			expect(onLoadMore).not.toHaveBeenCalled();
		});

		it("backs off before retrying an auto-fill after a failed load-more, instead of retrying every render", () => {
			vi.useFakeTimers();
			try {
				const onLoadMore = vi.fn();
				renderBoard([], {
					columnPagination: {
						"status-todo": {
							hasMore: true,
							isLoadingMore: false,
							onLoadMore,
							lastLoadMoreFailed: true,
						},
					},
				});
				// Mirrors AUTO_FILL_RETRY_BACKOFF_MS in board-view.tsx.
				const backoffMs = 4000;
				vi.advanceTimersByTime(backoffMs - 1);
				expect(onLoadMore).not.toHaveBeenCalled();

				vi.advanceTimersByTime(1);
				expect(onLoadMore).toHaveBeenCalledTimes(1);
			} finally {
				vi.useRealTimers();
			}
		});

		it("cancels a pending auto-fill retry when the column unmounts before it fires", () => {
			vi.useFakeTimers();
			try {
				const onLoadMore = vi.fn();
				const { unmount } = renderBoard([], {
					columnPagination: {
						"status-todo": {
							hasMore: true,
							isLoadingMore: false,
							onLoadMore,
							lastLoadMoreFailed: true,
						},
					},
				});

				unmount();
				vi.advanceTimersByTime(4000);
				expect(onLoadMore).not.toHaveBeenCalled();
			} finally {
				vi.useRealTimers();
			}
		});
	});
});
