// Tests for calendar-view.tsx: loading the days on screen, placing tasks,
// rescheduling by drag and drop, the undated panel, overflow, quick add and
// keyboard navigation.

import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
	act,
	fireEvent,
	render,
	screen,
	waitFor,
	within,
} from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import type {
	ListTasksOptions,
	Task,
	TaskListResult,
} from "@/lib/interaction-api";
import type { TaskStatus, TaskType } from "@/lib/project-api";
import { buildGrid, dayToDateParam, firstDayOfWeek } from "./calendar-scale";
import { CalendarView } from "./calendar-view";
import { toDay } from "./gantt-scale";

const notifyError = vi.hoisted(() => vi.fn());
vi.mock("@/lib/notify", () => ({ notifyError }));
const toastAdd = vi.hoisted(() => vi.fn());
vi.mock("@/components/ui/toast", () => ({ toast: { add: toastAdd } }));
const listAllTasks = vi.hoisted(() => vi.fn());
vi.mock("@/lib/interaction-api", async (importOriginal) => ({
	...(await importOriginal<typeof import("@/lib/interaction-api")>()),
	listAllTasks,
}));

const PROJECT_ID = "proj-1";

const statuses: TaskStatus[] = [
	{
		id: "status-todo",
		project_id: PROJECT_ID,
		name: "Todo",
		color: "#aaa",
		position: 1,
		category: "todo",
		is_default: true,
		created_at: "2026-01-01",
		updated_at: "2026-01-01",
	},
];

const taskTypes: TaskType[] = [
	{
		id: "type-task",
		project_id: PROJECT_ID,
		name: "Task",
		is_default: true,
		created_at: "2026-01-01",
		updated_at: "2026-01-01",
	},
];

const makeTask = (overrides: Partial<Task> = {}): Task => ({
	id: "task-1",
	project_id: PROJECT_ID,
	title: "Do the thing",
	task_number: 1,
	sprint_id: null,
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

/** Day number of a "YYYY-MM-DD" date. */
const d = (date: string) => toDay(`${date}T00:00:00Z`) as number;
const iso = (date: string) => `${date}T00:00:00Z`;

const page = (items: Task[]): TaskListResult => ({
	items,
	page_size: 200,
	next_cursor: null,
	total_count: items.length,
});

let windowTasks: Task[] = [];
let undatedTasks: Task[] = [];

function wrapper({ children }: { children: ReactNode }) {
	const qc = new QueryClient({
		defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
	});
	return <QueryClientProvider client={qc}>{children}</QueryClientProvider>;
}

function renderCalendar(
	overrides: Partial<Parameters<typeof CalendarView>[0]> = {},
) {
	return render(
		<CalendarView
			projectId={PROJECT_ID}
			taskFilters={{ taskTypeIds: ["type-task"] }}
			statuses={statuses}
			taskTypes={taskTypes}
			canCreate={true}
			canEdit={true}
			onRescheduleTask={vi.fn().mockResolvedValue({})}
			onCreateTask={vi.fn().mockResolvedValue(undefined)}
			onTaskClick={vi.fn()}
			{...overrides}
		/>,
		{ wrapper },
	);
}

const cell = (date: string) =>
	document.querySelector(`[data-calendar-day="${d(date)}"]`) as HTMLElement;
const chips = (taskId: string) =>
	Array.from(
		document.querySelectorAll<HTMLElement>(`[data-calendar-task="${taskId}"]`),
	);
const chipButton = (taskId: string) =>
	chips(taskId)[0].querySelector("button") as HTMLButtonElement;

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

/** Starts dragging `source` and waits for the view to pick the drag up. */
async function startDragging(source: HTMLElement) {
	const dt = createDt();
	fireEvent.dragStart(source, { dataTransfer: dt });
	await act(async () => {
		await new Promise((r) => setTimeout(r, 5));
	});
	return dt;
}

/** Drags `source` onto the day cell of `date`. */
async function dragTo(source: HTMLElement, date: string) {
	const dt = await startDragging(source);
	fireEvent.dragOver(cell(date), { dataTransfer: dt });
	await act(async () => {
		fireEvent.drop(cell(date), { dataTransfer: dt });
	});
	fireEvent.dragEnd(source, { dataTransfer: dt });
}

/** The options of the last window (not undated) listing. */
function lastWindowOpts(): ListTasksOptions {
	const calls = listAllTasks.mock.calls.filter(
		([, opts]) => !(opts as ListTasksOptions).unscheduled,
	);
	return calls[calls.length - 1][1] as ListTasksOptions;
}

beforeEach(() => {
	// Wednesday, March 11 2026, local noon.
	vi.useFakeTimers({ toFake: ["Date"] });
	vi.setSystemTime(new Date(2026, 2, 11, 12));
	windowTasks = [];
	undatedTasks = [];
	listAllTasks.mockReset();
	listAllTasks.mockImplementation(
		async (_projectId: string, opts: ListTasksOptions) =>
			page(opts.unscheduled ? undatedTasks : windowTasks),
	);
	notifyError.mockClear();
	toastAdd.mockClear();
});

afterEach(() => {
	vi.useRealTimers();
});

describe("CalendarView loading", () => {
	it("loads the tasks of the month on screen with the view's filters", async () => {
		renderCalendar();
		await waitFor(() => expect(listAllTasks).toHaveBeenCalled());

		const grid = buildGrid(
			d("2026-03-11"),
			"month",
			firstDayOfWeek(navigator.language),
		);
		const opts = lastWindowOpts();
		expect(opts.taskTypeIds).toEqual(["type-task"]);
		expect(opts.scheduledFrom).toBe(dayToDateParam(grid.start));
		expect(opts.scheduledTo).toBe(dayToDateParam(grid.end - 1));
		expect(opts.scheduledBy).toBeUndefined();
		expect(opts.pageSize).toBe(200);
		expect(
			screen.getByRole("grid", { name: "March 2026" }),
		).toBeInTheDocument();
	});

	it("asks for the tasks without the dates the view uses", async () => {
		renderCalendar({ viewConfig: { calendar_date_field: "due_date" } });
		await waitFor(() =>
			expect(listAllTasks).toHaveBeenCalledWith(
				PROJECT_ID,
				expect.objectContaining({ unscheduled: true, scheduledBy: "due_date" }),
			),
		);
		expect(lastWindowOpts().scheduledBy).toBe("due_date");
	});

	it("moves to the next month and back to today", async () => {
		renderCalendar();
		await waitFor(() => expect(listAllTasks).toHaveBeenCalled());

		fireEvent.click(screen.getByRole("button", { name: "Next" }));
		await waitFor(() =>
			expect(
				screen.getByRole("grid", { name: "April 2026" }),
			).toBeInTheDocument(),
		);
		const april = buildGrid(
			d("2026-04-01"),
			"month",
			firstDayOfWeek(navigator.language),
		);
		await waitFor(() =>
			expect(lastWindowOpts().scheduledFrom).toBe(dayToDateParam(april.start)),
		);
		expect(lastWindowOpts().scheduledTo).toBe(dayToDateParam(april.end - 1));

		fireEvent.click(screen.getByRole("button", { name: "Today" }));
		expect(
			screen.getByRole("grid", { name: "March 2026" }),
		).toBeInTheDocument();
	});

	it("starts at the saved mode and remembers a new pick", async () => {
		const onModeChange = vi.fn();
		renderCalendar({ mode: "week", onModeChange });
		await waitFor(() => expect(listAllTasks).toHaveBeenCalled());
		expect(screen.getByRole("button", { name: "Week" })).toHaveAttribute(
			"aria-pressed",
			"true",
		);
		// The week of March 11 has seven days on screen.
		expect(document.querySelectorAll('[role="gridcell"]')).toHaveLength(7);

		fireEvent.click(screen.getByRole("button", { name: "Day" }));
		expect(onModeChange).toHaveBeenCalledWith("day");
		expect(document.querySelectorAll('[role="gridcell"]')).toHaveLength(1);
		expect(cell("2026-03-11")).toBeInTheDocument();

		fireEvent.click(screen.getByRole("button", { name: "Day" }));
		expect(onModeChange).toHaveBeenCalledTimes(1);
	});
});

describe("CalendarView placement", () => {
	it("spans a task from its start to its due date, split at week rows", async () => {
		windowTasks = [
			makeTask({
				id: "t1",
				title: "Launch",
				start_date: iso("2026-03-05"),
				due_date: iso("2026-03-17"),
			}),
		];
		renderCalendar();
		await screen.findAllByText("Launch");

		const segments = chips("t1");
		// Thirteen days cross two or three week rows, whichever day starts them.
		expect(segments.length).toBeGreaterThanOrEqual(2);
		expect(Number(segments[0].dataset.start)).toBe(d("2026-03-05"));
		expect(Number(segments[segments.length - 1].dataset.end)).toBe(
			d("2026-03-17"),
		);
	});

	it("places a task on the one date the view uses", async () => {
		windowTasks = [
			makeTask({
				id: "t1",
				title: "Ship",
				start_date: iso("2026-03-02"),
				due_date: iso("2026-03-04"),
			}),
		];
		renderCalendar({ viewConfig: { calendar_date_field: "due_date" } });
		await screen.findByText("Ship");

		const [segment] = chips("t1");
		expect(Number(segment.dataset.start)).toBe(d("2026-03-04"));
		expect(Number(segment.dataset.end)).toBe(d("2026-03-04"));
	});

	it("opens a task on click", async () => {
		const onTaskClick = vi.fn();
		windowTasks = [
			makeTask({ id: "t1", title: "Ship", due_date: iso("2026-03-04") }),
		];
		renderCalendar({ onTaskClick });
		fireEvent.click(await screen.findByText("Ship"));
		expect(onTaskClick).toHaveBeenCalledWith(
			expect.objectContaining({ id: "t1" }),
		);
	});

	it("shows +N more on a crowded day and lists all its tasks", async () => {
		windowTasks = Array.from({ length: 5 }, (_, i) =>
			makeTask({
				id: `t${i}`,
				title: `Busy ${i}`,
				due_date: iso("2026-03-12"),
			}),
		);
		renderCalendar();
		await screen.findByText("Busy 0");

		// Before the rows are measured a month row fits three lanes: two tasks
		// show and the third lane holds the link to the other three.
		expect(screen.getAllByText(/^Busy/)).toHaveLength(2);
		fireEvent.click(within(cell("2026-03-12")).getByText("+3 more"));
		const popup = await screen.findByRole("dialog");
		for (let i = 0; i < 5; i++) {
			expect(within(popup).getByText(`Busy ${i}`)).toBeInTheDocument();
		}
	});
});

describe("CalendarView rescheduling", () => {
	const dated = () =>
		makeTask({
			id: "t1",
			title: "Launch",
			start_date: iso("2026-03-09"),
			due_date: iso("2026-03-10"),
		});

	it("moves a task to another day and keeps its length", async () => {
		const onRescheduleTask = vi.fn().mockResolvedValue({});
		windowTasks = [dated()];
		renderCalendar({ onRescheduleTask });
		await screen.findByText("Launch");

		// The drag starts on the task's first day (jsdom has no layout).
		await dragTo(chipButton("t1"), "2026-03-12");

		expect(onRescheduleTask).toHaveBeenCalledWith("t1", {
			start_date: iso("2026-03-12"),
			due_date: iso("2026-03-13"),
		});
		// Shown on its new days while the list catches up, with an undo.
		await waitFor(() =>
			expect(Number(chips("t1")[0].dataset.start)).toBe(d("2026-03-12")),
		);
		expect(toastAdd).toHaveBeenCalledTimes(1);
		await act(async () => toastAdd.mock.calls[0][0].actionProps.onClick());
		expect(onRescheduleTask).toHaveBeenLastCalledWith("t1", {
			start_date: iso("2026-03-09"),
			due_date: iso("2026-03-10"),
		});
	});

	it("moves both dates when the view places tasks by one of them", async () => {
		const onRescheduleTask = vi.fn().mockResolvedValue({});
		windowTasks = [dated()];
		renderCalendar({
			onRescheduleTask,
			viewConfig: { calendar_date_field: "due_date" },
		});
		await screen.findByText("Launch");

		await dragTo(chipButton("t1"), "2026-03-17");

		expect(onRescheduleTask).toHaveBeenCalledWith("t1", {
			start_date: iso("2026-03-16"),
			due_date: iso("2026-03-17"),
		});
	});

	it("drags an edge to change only that date", async () => {
		const onRescheduleTask = vi.fn().mockResolvedValue({});
		windowTasks = [dated()];
		renderCalendar({ onRescheduleTask });
		await screen.findByText("Launch");

		const edge = chips("t1")[0].querySelector(
			'[data-calendar-edge="end"]',
		) as HTMLElement;
		await dragTo(edge, "2026-03-13");

		expect(onRescheduleTask).toHaveBeenCalledWith("t1", {
			due_date: iso("2026-03-13"),
		});
	});

	it("ends an edge drag released outside the days", async () => {
		const onRescheduleTask = vi.fn().mockResolvedValue({});
		windowTasks = [dated()];
		renderCalendar({ onRescheduleTask });
		await screen.findByText("Launch");

		const edge = chips("t1")[0].querySelector(
			'[data-calendar-edge="end"]',
		) as HTMLElement;
		const dt = await startDragging(edge);
		// The handle is still on the page, so its dragend reaches the view.
		expect(edge).toBeInTheDocument();
		fireEvent.dragEnd(edge, { dataTransfer: dt });

		expect(chips("t1")[0]).toHaveClass("pointer-events-auto");
		expect(chips("t1")[0]).not.toHaveClass("opacity-40");
		// Nothing is left mid-drag for a later drop on a day to pick up.
		fireEvent.dragOver(cell("2026-03-13"), { dataTransfer: dt });
		fireEvent.drop(cell("2026-03-13"), { dataTransfer: dt });
		expect(onRescheduleTask).not.toHaveBeenCalled();
	});

	it("puts the task back and reports the error when the save fails", async () => {
		const onRescheduleTask = vi.fn().mockRejectedValue(new Error("nope"));
		windowTasks = [dated()];
		renderCalendar({ onRescheduleTask });
		await screen.findByText("Launch");

		await dragTo(chipButton("t1"), "2026-03-19");

		await waitFor(() =>
			expect(notifyError).toHaveBeenCalledWith(
				expect.any(Error),
				"Couldn't reschedule the task",
			),
		);
		expect(Number(chips("t1")[0].dataset.start)).toBe(d("2026-03-09"));
		expect(toastAdd).not.toHaveBeenCalled();
	});

	it("schedules an undated task dropped on a day", async () => {
		const onRescheduleTask = vi.fn().mockResolvedValue({});
		undatedTasks = [makeTask({ id: "u1", title: "Someday" })];
		renderCalendar({ onRescheduleTask });

		fireEvent.click(await screen.findByRole("button", { name: /No date/ }));
		const item = await screen.findByText("Someday");
		await dragTo(item.closest("button") as HTMLElement, "2026-03-20");

		expect(onRescheduleTask).toHaveBeenCalledWith("u1", {
			start_date: iso("2026-03-20"),
			due_date: iso("2026-03-20"),
		});
		await waitFor(() =>
			expect(Number(chips("u1")[0].dataset.start)).toBe(d("2026-03-20")),
		);
	});

	it("is read-only without edit permission", async () => {
		const onRescheduleTask = vi.fn().mockResolvedValue({});
		windowTasks = [dated()];
		renderCalendar({ canEdit: false, onRescheduleTask });
		await screen.findByText("Launch");

		expect(chipButton("t1")).toHaveAttribute("draggable", "false");
		expect(document.querySelector("[data-calendar-edge]")).toBeNull();
		await dragTo(chipButton("t1"), "2026-03-12");
		expect(onRescheduleTask).not.toHaveBeenCalled();
	});
});

describe("CalendarView creating and keyboard", () => {
	it("creates a task dated the day its + was clicked", async () => {
		const onCreateTask = vi.fn().mockResolvedValue(undefined);
		renderCalendar({ onCreateTask });
		await waitFor(() => expect(listAllTasks).toHaveBeenCalled());

		fireEvent.click(
			screen.getByRole("button", {
				name: "Add a task on Monday, March 16, 2026",
			}),
		);
		const input = await screen.findByRole("textbox", { name: "Task title" });
		fireEvent.change(input, { target: { value: "Write notes" } });
		await act(async () => {
			fireEvent.click(screen.getByRole("button", { name: "Add" }));
		});

		expect(onCreateTask).toHaveBeenCalledWith(
			"status-todo",
			"Write notes",
			"type-task",
			{ start_date: iso("2026-03-16"), due_date: iso("2026-03-16") },
		);
	});

	it("has no + without create permission", async () => {
		renderCalendar({ canCreate: false });
		await waitFor(() => expect(listAllTasks).toHaveBeenCalled());
		expect(
			screen.queryByRole("button", { name: /^Add a task on/ }),
		).not.toBeInTheDocument();
	});

	it("moves between days with the arrow keys and turns the page off the grid", async () => {
		renderCalendar({ mode: "week" });
		await waitFor(() => expect(listAllTasks).toHaveBeenCalled());

		const today = cell("2026-03-11");
		expect(today).toHaveAttribute("tabindex", "0");
		expect(today).toHaveAttribute("aria-current", "date");
		today.focus();
		fireEvent.keyDown(today, { key: "ArrowRight" });
		expect(cell("2026-03-12")).toHaveFocus();

		fireEvent.keyDown(cell("2026-03-12"), { key: "ArrowDown" });
		// A week later is off this week's grid: the next week shows.
		await waitFor(() => expect(cell("2026-03-19")).toHaveFocus());
		expect(cell("2026-03-12")).toBeNull();
	});

	it("opens the quick add of the focused day with Enter", async () => {
		renderCalendar({ mode: "week" });
		await waitFor(() => expect(listAllTasks).toHaveBeenCalled());
		const today = cell("2026-03-11");
		today.focus();
		fireEvent.keyDown(today, { key: "Enter" });
		expect(
			await screen.findByRole("textbox", { name: "Task title" }),
		).toBeInTheDocument();
	});
});
