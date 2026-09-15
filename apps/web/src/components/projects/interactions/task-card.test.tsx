import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { useIsMobile } from "@/hooks/use-mobile";
import type { Task } from "@/lib/interaction-api";
import type { TaskStatus, TaskType } from "@/lib/project-api";
import { TaskCard } from "./task-card";

// Real by default (see test/setup.ts's window.matchMedia stub, which always
// resolves to desktop) — mocked directly here so the move-button tests below
// don't depend on jsdom viewport dimensions.
vi.mock("@/hooks/use-mobile", () => ({ useIsMobile: vi.fn(() => false) }));

// TaskCard's epic-picker field now calls useEpicSearch (useInfiniteQuery)
// unconditionally, so it needs a QueryClientProvider ancestor even though the
// query stays disabled (epicOpen is never toggled true) in these tests.
function wrapper({ children }: { children: ReactNode }) {
	const qc = new QueryClient({
		defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
	});
	return <QueryClientProvider client={qc}>{children}</QueryClientProvider>;
}

// ── Fixtures ──────────────────────────────────────────────────────────────────

const makeTask = (overrides: Partial<Task> = {}): Task => ({
	id: "task-1",
	project_id: "proj-1",
	title: "Fix the login bug",
	task_number: 0,
	sprint_id: "sprint-1",
	status_id: "status-1",
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

const NO_TYPES: TaskType[] = [];
const NO_STATUSES: TaskStatus[] = [];

const bugType: TaskType = {
	id: "type-bug",
	project_id: "proj-1",
	name: "Bug",
	icon: null,
	color: "#FF0000",
	description: null,
	created_at: "2026-01-01T00:00:00Z",
	updated_at: "2026-01-01T00:00:00Z",
};

// ── Tests ─────────────────────────────────────────────────────────────────────

describe("TaskCard", () => {
	beforeEach(() => {
		vi.mocked(useIsMobile).mockReturnValue(false);
	});

	it("renders the task title", () => {
		render(
			<TaskCard
				task={makeTask()}
				statuses={NO_STATUSES}
				taskTypes={NO_TYPES}
			/>,
			{ wrapper },
		);
		expect(screen.getByText("Fix the login bug")).toBeInTheDocument();
	});

	it("does not render a type badge when task has no task_type_id", () => {
		render(
			<TaskCard
				task={makeTask({ task_type_id: null })}
				statuses={NO_STATUSES}
				taskTypes={[bugType]}
			/>,
			{ wrapper },
		);
		expect(screen.queryByText("Bug")).not.toBeInTheDocument();
	});

	it("renders the task type badge when a matching type exists", () => {
		render(
			<TaskCard
				task={makeTask({ task_type_id: "type-bug" })}
				statuses={NO_STATUSES}
				taskTypes={[bugType]}
			/>,
			{ wrapper },
		);
		expect(screen.getByText("Bug")).toBeInTheDocument();
	});

	it("calls onClick when the card is clicked", () => {
		const onClick = vi.fn();
		render(
			<TaskCard
				task={makeTask()}
				statuses={NO_STATUSES}
				taskTypes={NO_TYPES}
				onClick={onClick}
			/>,
			{ wrapper },
		);
		fireEvent.click(screen.getByText("Fix the login bug"));
		expect(onClick).toHaveBeenCalledOnce();
	});

	it("is draggable when canEdit=true", () => {
		const { container } = render(
			<TaskCard
				task={makeTask()}
				statuses={NO_STATUSES}
				taskTypes={NO_TYPES}
				canEdit={true}
			/>,
			{ wrapper },
		);
		const card = container.querySelector("[data-task-id='task-1']");
		expect(card).toHaveAttribute("draggable", "true");
	});

	it("is not draggable when canEdit=false", () => {
		const { container } = render(
			<TaskCard
				task={makeTask()}
				statuses={NO_STATUSES}
				taskTypes={NO_TYPES}
				canEdit={false}
			/>,
			{ wrapper },
		);
		const card = container.querySelector("[data-task-id='task-1']");
		expect(card).toHaveAttribute("draggable", "false");
	});

	it("calls onDragStart with the drag event", () => {
		const onDragStart = vi.fn();
		const { container } = render(
			<TaskCard
				task={makeTask()}
				statuses={NO_STATUSES}
				taskTypes={NO_TYPES}
				canEdit={true}
				onDragStart={onDragStart}
			/>,
			{ wrapper },
		);
		const card = container.querySelector("[data-task-id='task-1']") as Element;
		fireEvent.dragStart(card);
		expect(onDragStart).toHaveBeenCalledOnce();
	});

	it("calls onDragEnd when drag ends", () => {
		const onDragEnd = vi.fn();
		const { container } = render(
			<TaskCard
				task={makeTask()}
				statuses={NO_STATUSES}
				taskTypes={NO_TYPES}
				canEdit={true}
				onDragEnd={onDragEnd}
			/>,
			{ wrapper },
		);
		const card = container.querySelector("[data-task-id='task-1']") as Element;
		fireEvent.dragEnd(card);
		expect(onDragEnd).toHaveBeenCalledOnce();
	});

	it("applies dragging styles when isDragging=true", () => {
		const { container } = render(
			<TaskCard
				task={makeTask()}
				statuses={NO_STATUSES}
				taskTypes={NO_TYPES}
				isDragging={true}
			/>,
			{ wrapper },
		);
		const card = container.querySelector("[data-task-id='task-1']") as Element;
		// isDragging adds opacity-50 class
		expect(card.className).toMatch(/opacity-50/);
	});

	it("shows the assignee icon when task has an assignee", () => {
		const { container } = render(
			<TaskCard
				task={makeTask({ assignee_ids: ["user-1"] })}
				statuses={NO_STATUSES}
				taskTypes={NO_TYPES}
			/>,
			{ wrapper },
		);
		// assigned state: filled avatar circle with the primary gradient
		const assigneeEl = container.querySelector(".from-primary\\/20");
		expect(assigneeEl).toBeInTheDocument();
	});

	describe("mobile move buttons", () => {
		it("does not render on desktop even when handlers are provided", () => {
			render(
				<TaskCard
					task={makeTask()}
					statuses={NO_STATUSES}
					taskTypes={NO_TYPES}
					onMoveLeft={vi.fn()}
					onMoveRight={vi.fn()}
				/>,
				{ wrapper },
			);
			expect(
				screen.queryByRole("button", { name: "Move to previous column" }),
			).not.toBeInTheDocument();
			expect(
				screen.queryByRole("button", { name: "Move to next column" }),
			).not.toBeInTheDocument();
		});

		it("does not render on mobile when neither handler is provided", () => {
			vi.mocked(useIsMobile).mockReturnValue(true);
			render(
				<TaskCard
					task={makeTask()}
					statuses={NO_STATUSES}
					taskTypes={NO_TYPES}
				/>,
				{ wrapper },
			);
			expect(
				screen.queryByRole("button", { name: "Move to previous column" }),
			).not.toBeInTheDocument();
			expect(
				screen.queryByRole("button", { name: "Move to next column" }),
			).not.toBeInTheDocument();
		});

		it("renders both buttons enabled on mobile when both handlers are provided", () => {
			vi.mocked(useIsMobile).mockReturnValue(true);
			render(
				<TaskCard
					task={makeTask()}
					statuses={NO_STATUSES}
					taskTypes={NO_TYPES}
					onMoveLeft={vi.fn()}
					onMoveRight={vi.fn()}
				/>,
				{ wrapper },
			);
			expect(
				screen.getByRole("button", { name: "Move to previous column" }),
			).toBeEnabled();
			expect(
				screen.getByRole("button", { name: "Move to next column" }),
			).toBeEnabled();
		});

		it("disables move-left at the first column (no onMoveLeft)", () => {
			vi.mocked(useIsMobile).mockReturnValue(true);
			render(
				<TaskCard
					task={makeTask()}
					statuses={NO_STATUSES}
					taskTypes={NO_TYPES}
					onMoveRight={vi.fn()}
				/>,
				{ wrapper },
			);
			expect(
				screen.getByRole("button", { name: "Move to previous column" }),
			).toBeDisabled();
			expect(
				screen.getByRole("button", { name: "Move to next column" }),
			).toBeEnabled();
		});

		it("disables move-right at the last column (no onMoveRight)", () => {
			vi.mocked(useIsMobile).mockReturnValue(true);
			render(
				<TaskCard
					task={makeTask()}
					statuses={NO_STATUSES}
					taskTypes={NO_TYPES}
					onMoveLeft={vi.fn()}
				/>,
				{ wrapper },
			);
			expect(
				screen.getByRole("button", { name: "Move to previous column" }),
			).toBeEnabled();
			expect(
				screen.getByRole("button", { name: "Move to next column" }),
			).toBeDisabled();
		});

		it("calls onMoveRight without also triggering the card's onClick", () => {
			vi.mocked(useIsMobile).mockReturnValue(true);
			const onClick = vi.fn();
			const onMoveRight = vi.fn();
			render(
				<TaskCard
					task={makeTask()}
					statuses={NO_STATUSES}
					taskTypes={NO_TYPES}
					onClick={onClick}
					onMoveLeft={vi.fn()}
					onMoveRight={onMoveRight}
				/>,
				{ wrapper },
			);
			fireEvent.click(
				screen.getByRole("button", { name: "Move to next column" }),
			);
			expect(onMoveRight).toHaveBeenCalledOnce();
			expect(onClick).not.toHaveBeenCalled();
		});
	});
});
