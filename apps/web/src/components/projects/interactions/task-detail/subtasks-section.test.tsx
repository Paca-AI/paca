import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

import type { Task } from "@/lib/interaction-api";
import { SubtasksSection } from "./subtasks-section";

function makeSubtask(overrides: Partial<Task> = {}): Task {
	return {
		id: "sub-1",
		project_id: "p1",
		title: "Do the thing",
		task_number: 1,
		importance: 0,
		custom_fields: {},
		created_at: "2026-01-01T00:00:00.000Z",
		updated_at: "2026-01-01T00:00:00.000Z",
		...overrides,
	} as Task;
}

const baseTask = makeSubtask({ id: "parent-1", title: "Parent task" });

describe("SubtasksSection", () => {
	it("renders every loaded subtask", () => {
		render(
			<SubtasksSection
				parentTaskId="parent-1"
				task={baseTask}
				subtasks={[
					makeSubtask({ id: "sub-1", title: "First subtask" }),
					makeSubtask({ id: "sub-2", title: "Second subtask" }),
				]}
				statuses={[]}
				canEdit={false}
			/>,
		);

		expect(screen.getByText("First subtask")).toBeInTheDocument();
		expect(screen.getByText("Second subtask")).toBeInTheDocument();
	});

	it("hides the load-more button when hasMore is false", () => {
		render(
			<SubtasksSection
				parentTaskId="parent-1"
				task={baseTask}
				subtasks={[makeSubtask()]}
				statuses={[]}
				canEdit={false}
				hasMore={false}
			/>,
		);

		expect(
			screen.queryByRole("button", { name: "Load more subtasks" }),
		).not.toBeInTheDocument();
	});

	it("shows the load-more button when hasMore is true and calls onLoadMore when clicked", async () => {
		const onLoadMore = vi.fn();
		render(
			<SubtasksSection
				parentTaskId="parent-1"
				task={baseTask}
				subtasks={[makeSubtask()]}
				statuses={[]}
				canEdit={false}
				hasMore
				onLoadMore={onLoadMore}
			/>,
		);

		const button = screen.getByRole("button", { name: "Load more subtasks" });
		await userEvent.click(button);

		expect(onLoadMore).toHaveBeenCalledTimes(1);
	});

	it("shows a loading indicator and disables the button while fetching the next page", () => {
		render(
			<SubtasksSection
				parentTaskId="parent-1"
				task={baseTask}
				subtasks={[makeSubtask()]}
				statuses={[]}
				canEdit={false}
				hasMore
				isLoadingMore
				onLoadMore={vi.fn()}
			/>,
		);

		const button = screen.getByRole("button", { name: /Loading more/ });
		expect(button).toBeDisabled();
	});

	it("shows the empty state when there are no subtasks and editing is disabled", () => {
		render(
			<SubtasksSection
				parentTaskId="parent-1"
				task={baseTask}
				subtasks={[]}
				statuses={[]}
				canEdit={false}
			/>,
		);

		expect(screen.getByText("No subtasks yet")).toBeInTheDocument();
	});
});
