// Tests for gantt-view.tsx: row drag-and-drop, zoom, rescheduling and
// dependency arrows.

import { act, fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import type { Task } from "@/lib/interaction-api";
import type { TaskStatus } from "@/lib/project-api";
import { GanttView } from "./gantt-view";

const notifyError = vi.hoisted(() => vi.fn());
vi.mock("@/lib/notify", () => ({ notifyError }));
const toastAdd = vi.hoisted(() => vi.fn());
vi.mock("@/components/ui/toast", () => ({ toast: { add: toastAdd } }));

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

function renderGantt(
	tasks: Task[],
	overrides: Partial<Parameters<typeof GanttView>[0]> = {},
) {
	return render(
		<GanttView
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
	notifyError.mockClear();
	toastAdd.mockClear();
});

describe("GanttView row drag and drop", () => {
	it("moves a task to another status and keeps its sprint_id", async () => {
		renderGantt([makeTask({ id: "t1", title: "Alpha", sprint_id: SPRINT_ID })]);
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
		renderGantt([makeTask({ id: "t1", title: "Alpha" })]);
		const dt = createDt();
		await startDrag("Alpha", dt);

		fireEvent.drop(groupOf("Todo"), { dataTransfer: dt });

		expect(onMoveToColumn).not.toHaveBeenCalled();
		expect(onReorderTask).not.toHaveBeenCalled();
	});

	it("reorders on a same-group row drop under manual sort (lands before target)", async () => {
		renderGantt(
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
		renderGantt([
			makeTask({ id: "t1", title: "Alpha" }),
			makeTask({ id: "t2", title: "Beta" }),
		]);
		const dt = createDt();
		await startDrag("Alpha", dt);

		fireEvent.drop(rowOf("Beta"), { dataTransfer: dt });

		expect(onReorderTask).not.toHaveBeenCalled();
	});

	it("rows are not draggable and no empty groups appear when canEdit=false", async () => {
		renderGantt([makeTask({ id: "t1", title: "Alpha" })], {
			canEdit: false,
		});
		expect(rowOf("Alpha").getAttribute("draggable")).toBe("false");

		await startDrag("Alpha", createDt());
		expect(screen.queryByText("Done")).not.toBeInTheDocument();
		expect(onMoveToColumn).not.toHaveBeenCalled();
	});

	it("shows empty groups as drop targets only while dragging", async () => {
		renderGantt([makeTask({ id: "t1", title: "Alpha" })]);
		expect(screen.queryByText("Done")).not.toBeInTheDocument();

		await startDrag("Alpha", createDt());
		expect(screen.getByText("Done")).toBeInTheDocument();
	});

	it("does not offer status columns filtered out by the view as drop targets", async () => {
		renderGantt([makeTask({ id: "t1", title: "Alpha" })], {
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

// ── Zoom, rescheduling and dependencies ──────────────────────────────────────

/** The bar of a task (rows without dates have none). */
function barOf(taskId: string) {
	return document.querySelector(
		`[data-gantt-bar="${taskId}"]`,
	) as HTMLElement | null;
}

/** Drags `el` horizontally by `dx` pixels with a pointer. */
function pointerDrag(el: HTMLElement, dx: number, target: HTMLElement = el) {
	fireEvent.pointerDown(el, { button: 0, pointerId: 1, clientX: 100 });
	fireEvent.pointerMove(target, { pointerId: 1, clientX: 100 + dx });
	fireEvent.pointerUp(target, { pointerId: 1, clientX: 100 + dx });
}

// Two-day task on Mar 10–12, 2026; the day zoom draws 40px per day.
const dated = (overrides: Partial<Task> = {}) =>
	makeTask({
		id: "t1",
		title: "Alpha",
		start_date: "2026-03-10T00:00:00Z",
		due_date: "2026-03-12T00:00:00Z",
		...overrides,
	});

describe("GanttView zoom", () => {
	it("starts at the saved zoom and remembers a new pick", () => {
		const onZoomChange = vi.fn();
		renderGantt([dated()], { zoom: "month", onZoomChange });
		const month = screen.getByRole("button", { name: "Month" });
		expect(month).toHaveAttribute("aria-pressed", "true");

		fireEvent.click(screen.getByRole("button", { name: "Day" }));

		expect(onZoomChange).toHaveBeenCalledWith("day");
		expect(screen.getByRole("button", { name: "Day" })).toHaveAttribute(
			"aria-pressed",
			"true",
		);
		expect(month).toHaveAttribute("aria-pressed", "false");
	});

	it("defaults to the week zoom and does not save an unchanged pick", () => {
		const onZoomChange = vi.fn();
		renderGantt([dated()], { onZoomChange });
		const week = screen.getByRole("button", { name: "Week" });
		expect(week).toHaveAttribute("aria-pressed", "true");
		fireEvent.click(week);
		expect(onZoomChange).not.toHaveBeenCalled();
	});

	it("scales bars with the zoom", () => {
		const { rerender } = renderGantt([dated()], { zoom: "day" });
		expect(barOf("t1")?.style.width).toBe("120px"); // 3 days × 40px
		rerender(
			<GanttView
				tasks={[dated()]}
				statuses={statuses}
				taskTypes={[]}
				zoom="week"
				onTaskClick={vi.fn()}
			/>,
		);
		expect(barOf("t1")?.style.width).toBe("60px"); // 3 days × 20px
	});

	it("has a jump-to-today button", () => {
		renderGantt([dated()]);
		fireEvent.click(screen.getByRole("button", { name: "Today" }));
	});
});

describe("GanttView rescheduling", () => {
	it("moves a bar by whole days and saves both dates", async () => {
		const onRescheduleTask = vi.fn().mockResolvedValue({});
		renderGantt([dated()], { zoom: "day", onRescheduleTask });

		await act(async () => pointerDrag(barOf("t1") as HTMLElement, 85));

		expect(onRescheduleTask).toHaveBeenCalledWith("t1", {
			start_date: "2026-03-12T00:00:00Z",
			due_date: "2026-03-14T00:00:00Z",
		});
	});

	it("drags an edge to change only that date", async () => {
		const onRescheduleTask = vi.fn().mockResolvedValue({});
		renderGantt([dated()], { zoom: "day", onRescheduleTask });
		const bar = barOf("t1") as HTMLElement;

		await act(async () =>
			pointerDrag(
				bar.querySelector('[data-gantt-handle="end"]') as HTMLElement,
				40,
				bar,
			),
		);
		expect(onRescheduleTask).toHaveBeenLastCalledWith("t1", {
			due_date: "2026-03-13T00:00:00Z",
		});

		await act(async () =>
			pointerDrag(
				bar.querySelector('[data-gantt-handle="start"]') as HTMLElement,
				-80,
				bar,
			),
		);
		expect(onRescheduleTask).toHaveBeenLastCalledWith("t1", {
			start_date: "2026-03-08T00:00:00Z",
		});
	});

	it("shows the new dates while saving and offers an undo", async () => {
		let resolve: (v: unknown) => void = () => {};
		const onRescheduleTask = vi
			.fn()
			.mockImplementationOnce(() => new Promise((r) => (resolve = r)))
			.mockResolvedValue({});
		renderGantt([dated()], { zoom: "day", onRescheduleTask });
		const startDay = barOf("t1")?.dataset.start;

		await act(async () => pointerDrag(barOf("t1") as HTMLElement, 40));
		// Optimistic: drawn one day later before the save resolves.
		expect(Number(barOf("t1")?.dataset.start)).toBe(Number(startDay) + 1);
		expect(toastAdd).not.toHaveBeenCalled();

		await act(async () => resolve({}));
		expect(toastAdd).toHaveBeenCalledTimes(1);
		const { actionProps } = toastAdd.mock.calls[0][0];

		await act(async () => actionProps.onClick());
		expect(onRescheduleTask).toHaveBeenLastCalledWith("t1", {
			start_date: "2026-03-10T00:00:00Z",
			due_date: "2026-03-12T00:00:00Z",
		});
	});

	it("puts the bar back and reports the error when the save fails", async () => {
		const onRescheduleTask = vi.fn().mockRejectedValue(new Error("nope"));
		renderGantt([dated()], { zoom: "day", onRescheduleTask });
		const startDay = barOf("t1")?.dataset.start;

		await act(async () => pointerDrag(barOf("t1") as HTMLElement, 120));

		expect(onRescheduleTask).toHaveBeenCalled();
		expect(barOf("t1")?.dataset.start).toBe(startDay);
		expect(notifyError).toHaveBeenCalledWith(
			expect.any(Error),
			"Couldn't reschedule the task",
		);
		expect(toastAdd).not.toHaveBeenCalled();
	});

	it("opens the task on a plain click but not after a drag", async () => {
		const onTaskClick = vi.fn();
		const onRescheduleTask = vi.fn().mockResolvedValue({});
		renderGantt([dated()], { zoom: "day", onRescheduleTask, onTaskClick });
		const bar = barOf("t1") as HTMLElement;

		await act(async () => {
			pointerDrag(bar, 40);
			fireEvent.click(bar);
		});
		expect(onTaskClick).not.toHaveBeenCalled();

		await act(async () => {
			await new Promise((r) => setTimeout(r, 5));
		});
		fireEvent.click(bar);
		expect(onTaskClick).toHaveBeenCalledTimes(1);
	});

	it("cancels a drag on Escape without opening the task", async () => {
		const onTaskClick = vi.fn();
		const onRescheduleTask = vi.fn().mockResolvedValue({});
		renderGantt([dated()], { zoom: "day", onRescheduleTask, onTaskClick });
		const bar = barOf("t1") as HTMLElement;
		const startDay = bar.dataset.start;

		fireEvent.pointerDown(bar, { button: 0, pointerId: 1, clientX: 100 });
		fireEvent.pointerMove(bar, { pointerId: 1, clientX: 180 });
		expect(bar.dataset.start).not.toBe(startDay);
		fireEvent.keyDown(window, { key: "Escape" });
		expect(bar.dataset.start).toBe(startDay);
		fireEvent.pointerMove(bar, { pointerId: 1, clientX: 260 });
		fireEvent.pointerUp(bar, { pointerId: 1, clientX: 260 });
		fireEvent.click(bar);

		expect(onRescheduleTask).not.toHaveBeenCalled();
		expect(onTaskClick).not.toHaveBeenCalled();
		expect(bar.dataset.start).toBe(startDay);
	});

	it("is read-only without edit permission", async () => {
		const onRescheduleTask = vi.fn().mockResolvedValue({});
		renderGantt([dated()], {
			zoom: "day",
			canEdit: false,
			onRescheduleTask,
		});
		const bar = barOf("t1") as HTMLElement;
		expect(bar.querySelector("[data-gantt-handle]")).toBeNull();

		await act(async () => pointerDrag(bar, 80));
		expect(onRescheduleTask).not.toHaveBeenCalled();
	});
});

describe("GanttView dependencies", () => {
	const blocker = makeTask({
		id: "b1",
		title: "Design",
		start_date: "2026-03-02T00:00:00Z",
		due_date: "2026-03-06T00:00:00Z",
	});
	const link = (target: string) => ({
		id: `l-${target}`,
		source_task_id: "b1",
		target_task_id: target,
		link_type: "blocks" as const,
		created_at: "2026-01-01T00:00:00Z",
	});

	it("draws an arrow for a blocks link between two shown bars", () => {
		const after = makeTask({
			id: "a1",
			title: "Build",
			start_date: "2026-03-09T00:00:00Z",
			due_date: "2026-03-12T00:00:00Z",
		});
		renderGantt([blocker, after], { blockLinks: [link("a1")] });

		const arrows = screen
			.getByTestId("gantt-dependencies")
			.querySelectorAll(":scope > path");
		expect(arrows).toHaveLength(1);
		expect(arrows[0]).not.toHaveAttribute("data-violation");
		expect(barOf("a1")).not.toHaveAttribute("data-violation");
	});

	it("flags a blocked task that starts before its blocker ends", () => {
		const early = makeTask({
			id: "e1",
			title: "Ship",
			start_date: "2026-03-05T00:00:00Z",
			due_date: "2026-03-12T00:00:00Z",
		});
		renderGantt([blocker, early], { blockLinks: [link("e1")] });

		const arrow = screen
			.getByTestId("gantt-dependencies")
			.querySelector(":scope > path");
		expect(arrow).toHaveAttribute("data-violation", "true");
		const bar = barOf("e1") as HTMLElement;
		expect(bar).toHaveAttribute("data-violation", "true");
		expect(bar.title).toContain("Starts before “Design” ends");
	});

	it("skips links to tasks that are not shown or have no dates", () => {
		const undated = makeTask({ id: "u1", title: "Someday" });
		renderGantt([blocker, undated], {
			blockLinks: [link("u1"), link("missing")],
		});
		expect(screen.queryByTestId("gantt-dependencies")).toBeNull();
	});

	it("draws nothing when the view hides dependencies", () => {
		const early = makeTask({
			id: "e1",
			title: "Ship",
			start_date: "2026-03-05T00:00:00Z",
		});
		renderGantt([blocker, early], {
			blockLinks: [link("e1")],
			showDependencies: false,
		});
		expect(screen.queryByTestId("gantt-dependencies")).toBeNull();
		expect(barOf("e1")).not.toHaveAttribute("data-violation");
	});
});
