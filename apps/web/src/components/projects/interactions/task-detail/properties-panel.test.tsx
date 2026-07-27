import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { describe, expect, it } from "vitest";
import type { Task } from "@/lib/interaction-api";
import type { TaskType } from "@/lib/project-api";
import { getPriority } from "../priority";
import { PropertiesPanel } from "./properties-panel";

// PropertiesPanel's epic picker calls useEpicSearch (useInfiniteQuery)
// unconditionally, so it needs a QueryClientProvider ancestor even though the
// query stays disabled (the dropdown is never opened) in these tests.
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
	task_number: 1,
	sprint_id: null,
	status_id: null,
	task_type_id: "type-task",
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

const epicType: TaskType = {
	id: "type-epic",
	project_id: "proj-1",
	name: "Epic",
	icon: null,
	color: "#8b5cf6",
	description: null,
	is_system: true,
	created_at: "2026-01-01T00:00:00Z",
	updated_at: "2026-01-01T00:00:00Z",
};

const storyType: TaskType = {
	id: "type-story",
	project_id: "proj-1",
	name: "Story",
	icon: null,
	color: "#22c55e",
	description: null,
	is_system: false,
	created_at: "2026-01-01T00:00:00Z",
	updated_at: "2026-01-01T00:00:00Z",
};

const TASK_TYPES = [epicType, storyType];

// ── Tests ─────────────────────────────────────────────────────────────────────

describe("PropertiesPanel epic field", () => {
	it("resolves the epic from epicTasks when it's already paginated", () => {
		const epic = makeTask({
			id: "epic-1",
			title: "Epic Already Loaded",
			task_type_id: "type-epic",
		});
		const task = makeTask({ parent_task_id: "epic-1" });

		render(
			<PropertiesPanel
				task={task}
				status={undefined}
				taskType={storyType}
				priority={getPriority(0)}
				assignees={[]}
				reporter={undefined}
				taskTypes={TASK_TYPES}
				taskRole="normal"
				epicTasks={[epic]}
			/>,
			{ wrapper },
		);

		expect(screen.getByText("Epic Already Loaded")).toBeInTheDocument();
	});

	it("falls back to parentTask when the epic isn't in the paginated epicTasks list", () => {
		// Regression test for the bug fixed in this change: epicTasks is
		// paginated 20-per-page, so a task's own epic can legitimately be
		// missing from it. The field must still resolve via parentTask
		// (fetched directly, independent of pagination) instead of rendering
		// as unset.
		const parentTask = makeTask({
			id: "epic-1",
			title: "Unpaginated Epic",
			task_type_id: "type-epic",
		});
		const task = makeTask({ parent_task_id: "epic-1" });

		render(
			<PropertiesPanel
				task={task}
				status={undefined}
				taskType={storyType}
				priority={getPriority(0)}
				assignees={[]}
				reporter={undefined}
				taskTypes={TASK_TYPES}
				taskRole="normal"
				epicTasks={[]}
				parentTask={parentTask}
			/>,
			{ wrapper },
		);

		expect(screen.getByText("Unpaginated Epic")).toBeInTheDocument();
		// It must resolve via the Epic field specifically, not the generic
		// Parent field (which is where pre-fix code rendered it, since it only
		// checked epicTasks membership rather than parentTask's actual type).
		expect(screen.queryByText("Parent")).not.toBeInTheDocument();
	});

	it("does not fall back to parentTask when it isn't Epic-typed", () => {
		// A non-epic parentTask (story/task nesting) is a legitimate value for
		// the *Parent* field, but must never be picked up by the Epic field's
		// fallback. If it were, "Nested Story" would render twice: once
		// (wrongly) as the epic and once (correctly) in the Parent field.
		const parentTask = makeTask({
			id: "story-1",
			title: "Nested Story",
			task_type_id: "type-story",
		});
		const task = makeTask({ parent_task_id: "story-1" });

		render(
			<PropertiesPanel
				task={task}
				status={undefined}
				taskType={storyType}
				priority={getPriority(0)}
				assignees={[]}
				reporter={undefined}
				taskTypes={TASK_TYPES}
				taskRole="normal"
				epicTasks={[]}
				parentTask={parentTask}
			/>,
			{ wrapper },
		);

		expect(screen.getAllByText("Nested Story")).toHaveLength(1);
	});
});

describe("PropertiesPanel parent field", () => {
	it("shows the Parent field when parentTask is not Epic-typed", () => {
		const parentTask = makeTask({
			id: "story-1",
			title: "Parent Story",
			task_type_id: "type-story",
		});
		const task = makeTask({ parent_task_id: "story-1" });

		render(
			<PropertiesPanel
				task={task}
				status={undefined}
				taskType={storyType}
				priority={getPriority(0)}
				assignees={[]}
				reporter={undefined}
				taskTypes={TASK_TYPES}
				taskRole="normal"
				epicTasks={[]}
				parentTask={parentTask}
			/>,
			{ wrapper },
		);

		expect(screen.getByText("Parent Story")).toBeInTheDocument();
	});

	it("hides the Parent field when parentTask is Epic-typed but absent from paginated epicTasks", () => {
		// Regression test: previously this checked `epicTasks.find(...)`, so an
		// unpaginated epic parent would be misclassified as a generic "Parent"
		// instead of being recognized as the task's epic.
		const parentTask = makeTask({
			id: "epic-1",
			title: "Unpaginated Epic",
			task_type_id: "type-epic",
		});
		const task = makeTask({ parent_task_id: "epic-1" });

		render(
			<PropertiesPanel
				task={task}
				status={undefined}
				taskType={storyType}
				priority={getPriority(0)}
				assignees={[]}
				reporter={undefined}
				taskTypes={TASK_TYPES}
				taskRole="normal"
				epicTasks={[]}
				parentTask={parentTask}
			/>,
			{ wrapper },
		);

		expect(screen.queryByText("Parent")).not.toBeInTheDocument();
		// The epic field should be the one rendering it, not a generic parent field.
		expect(screen.getByText("Unpaginated Epic")).toBeInTheDocument();
	});
});
