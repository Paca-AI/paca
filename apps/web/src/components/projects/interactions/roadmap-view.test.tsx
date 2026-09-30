// Tests for roadmap-view.tsx drag-and-drop.

import { act, fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import type { Task } from "@/lib/interaction-api";
import type { TaskStatus } from "@/lib/project-api";
import { RoadmapView } from "./roadmap-view";

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
	assignment_mode: "manual",
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

const onMoveToColumn = vi.fn();
const onReorderTask = vi.fn();

function renderRoadmap(
	tasks: Task[],
	overrides: Partial<Parameters<typeof RoadmapView>[0]> = {},
) {
	return render(
		<RoadmapView
			tasks={tasks}
			statuses={statuses}
			taskTypes={[]}
			canEdit={true}
			manualSort={false}
			onMoveToColumn={onMoveToColumn}
			onReorderTask={onReorderTask}
			onCreateTask={vi.fn()}
			onTaskClick={vi.fn()}
			{...overrides}
		/>,
	);
}

/** The draggable row wrapping a task title. */
function rowOf(title: string) {
	return screen.getByText(title).closest("[draggable]") as HTMLElement;
}

/** Start a drag and flush the deferred `draggingId` update. */
async function startDrag(title: string, dt: ReturnType<typeof createDt>) {
	fireEvent.dragStart(rowOf(title), { dataTransfer: dt });
	await act(async () => {
		await new Promise((r) => setTimeout(r, 5));
	});
}

function groupOf(label: string) {
	return screen
		.getByText(label)
		.closest("div.border-border\\/20") as HTMLElement;
}

beforeEach(() => {
	onMoveToColumn.mockClear();
	onReorderTask.mockClear();
});

describe("RoadmapView drag and drop", () => {
	it("moves a task to another status and keeps its sprint_id", async () => {
		renderRoadmap([
			makeTask({ id: "t1", title: "Alpha", sprint_id: SPRINT_ID }),
		]);
		const dt = createDt();
		await startDrag("Alpha", dt);

		const done = groupOf("Done");
		fireEvent.dragOver(done, { dataTransfer: dt });
		fireEvent.drop(done, { dataTransfer: dt });

		expect(onMoveToColumn).toHaveBeenCalledWith("t1", {
			status_id: "status-done",
			sprint_id: SPRINT_ID,
		});
	});

	it("does not move when dropped on the same group", async () => {
		renderRoadmap([makeTask({ id: "t1", title: "Alpha" })]);
		const dt = createDt();
		await startDrag("Alpha", dt);

		fireEvent.drop(groupOf("Todo"), { dataTransfer: dt });

		expect(onMoveToColumn).not.toHaveBeenCalled();
		expect(onReorderTask).not.toHaveBeenCalled();
	});

	it("reorders on a same-group row drop under manual sort (lands before target)", async () => {
		renderRoadmap(
			[
				makeTask({ id: "t1", title: "Alpha" }),
				makeTask({ id: "t2", title: "Beta" }),
				makeTask({ id: "t3", title: "Gamma" }),
			],
			{ manualSort: true },
		);
		const dt = createDt();
		await startDrag("Alpha", dt);

		fireEvent.drop(rowOf("Gamma"), { dataTransfer: dt });

		// src index 0 < target index 2 → lands at 1 (before Gamma).
		expect(onReorderTask).toHaveBeenCalledWith("status-todo", "t1", 1);
		expect(onMoveToColumn).not.toHaveBeenCalled();
	});

	it("does not reorder when sort is not manual", async () => {
		renderRoadmap([
			makeTask({ id: "t1", title: "Alpha" }),
			makeTask({ id: "t2", title: "Beta" }),
		]);
		const dt = createDt();
		await startDrag("Alpha", dt);

		fireEvent.drop(rowOf("Beta"), { dataTransfer: dt });

		expect(onReorderTask).not.toHaveBeenCalled();
	});

	it("rows are not draggable and no empty groups appear when canEdit=false", async () => {
		renderRoadmap([makeTask({ id: "t1", title: "Alpha" })], {
			canEdit: false,
		});
		expect(rowOf("Alpha").getAttribute("draggable")).toBe("false");

		await startDrag("Alpha", createDt());
		expect(screen.queryByText("Done")).not.toBeInTheDocument();
		expect(onMoveToColumn).not.toHaveBeenCalled();
	});

	it("shows empty groups as drop targets only while dragging", async () => {
		renderRoadmap([makeTask({ id: "t1", title: "Alpha" })]);
		expect(screen.queryByText("Done")).not.toBeInTheDocument();

		await startDrag("Alpha", createDt());
		expect(screen.getByText("Done")).toBeInTheDocument();
	});

	it("does not offer status columns filtered out by the view as drop targets", async () => {
		renderRoadmap([makeTask({ id: "t1", title: "Alpha" })], {
			viewConfig: {
				filters: {
					statuses: { all: false, items: { "status-todo": true } },
				},
			},
		});
		await startDrag("Alpha", createDt());

		expect(screen.getByText("Todo")).toBeInTheDocument();
		expect(screen.queryByText("Done")).not.toBeInTheDocument();
	});
});
