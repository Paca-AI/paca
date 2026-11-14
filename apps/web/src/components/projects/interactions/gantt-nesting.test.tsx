import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { Task } from "@/lib/interaction-api";
import { GanttView } from "./gantt-view";
import { resetSubtaskViewState } from "./use-subtask-expansion";

const get = vi.hoisted(() => vi.fn());
vi.mock("@/lib/api-client", () => ({
	apiClient: { instance: { get, post: vi.fn() } },
}));

const makeTask = (overrides: Partial<Task> = {}): Task => ({
	id: "epic-1",
	project_id: "p1",
	title: "Epic one",
	task_number: 1,
	importance: 0,
	custom_fields: {},
	status_id: "s1",
	start_date: "2026-03-01",
	due_date: "2026-03-10",
	created_at: "2026-01-01T00:00:00Z",
	updated_at: "2026-01-01T00:00:00Z",
	assignment_mode: "manual",
	...overrides,
});

describe("GanttView nested tasks", () => {
	beforeEach(() => {
		get.mockReset();
		localStorage.clear();
		resetSubtaskViewState();
	});

	it("expands an epic to load and show its child tasks", async () => {
		get.mockResolvedValue({
			data: {
				data: {
					items: [makeTask({ id: "c1", title: "Child task" })],
					page_size: 50,
					next_cursor: null,
				},
			},
		});
		const qc = new QueryClient({
			defaultOptions: { queries: { retry: false } },
		});
		render(
			<QueryClientProvider client={qc}>
				<GanttView
					projectId="p1"
					viewKey="v1"
					tasks={[makeTask({ subtask_count: 1, subtask_done_count: 0 })]}
					statuses={[
						{
							id: "s1",
							project_id: "p1",
							name: "Todo",
							color: "#aaa",
							position: 1,
							category: "todo",
							created_at: "2026-01-01",
							updated_at: "2026-01-01",
						},
					]}
					taskTypes={[]}
					onTaskClick={vi.fn()}
				/>
			</QueryClientProvider>,
		);
		expect(screen.getByText("0/1")).toBeTruthy();
		expect(screen.queryByText("Child task")).toBeNull();
		fireEvent.click(
			screen
				.getAllByRole("button")
				.find((b) => b.getAttribute("aria-expanded") !== null) as HTMLElement,
		);
		expect(await screen.findByText("Child task")).toBeTruthy();
		expect(get.mock.calls[0][1].params.parent_task_id).toBe("epic-1");
	});
});
