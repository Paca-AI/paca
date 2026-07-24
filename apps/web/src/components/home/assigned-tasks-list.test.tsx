import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";

// ── Mocks ─────────────────────────────────────────────────────────────────────

const mocks = vi.hoisted(() => ({
	navigateMock: vi.fn(),
	listAssignedTasksMock: vi.fn(),
}));

vi.mock("@tanstack/react-router", () => ({
	useNavigate: () => mocks.navigateMock,
}));

vi.mock("@/lib/interaction-api", async () => {
	const actual = await vi.importActual<typeof import("@/lib/interaction-api")>(
		"@/lib/interaction-api",
	);
	return {
		...actual,
		assignedTasksQueryOptions: () => ({
			queryKey: ["test", "assigned-tasks"],
			queryFn: mocks.listAssignedTasksMock,
			initialPageParam: undefined,
			getNextPageParam: (lastPage: { next_cursor?: string | null }) =>
				lastPage.next_cursor ?? undefined,
		}),
	};
});

vi.mock("@/lib/project-api", async () => {
	const actual =
		await vi.importActual<typeof import("@/lib/project-api")>(
			"@/lib/project-api",
		);
	return {
		...actual,
		projectsQueryOptions: () => ({
			queryKey: ["test", "projects"],
			queryFn: async () => ({
				items: [
					{
						id: "proj-a",
						name: "Project Alpha",
						description: "",
						is_public: false,
						task_id_prefix: "ALP",
						settings: {},
						created_at: "2026-01-01T00:00:00Z",
					},
					{
						id: "proj-b",
						name: "Project Beta",
						description: "",
						is_public: false,
						task_id_prefix: "BET",
						settings: {},
						created_at: "2026-01-01T00:00:00Z",
					},
				],
				total: 2,
				page: 1,
				page_size: 50,
			}),
		}),
		taskStatusesQueryOptions: (projectId: string) => ({
			queryKey: ["test", "statuses", projectId],
			queryFn: async () => [],
		}),
		taskTypesQueryOptions: (projectId: string) => ({
			queryKey: ["test", "types", projectId],
			queryFn: async () => [],
		}),
		projectMembersQueryOptions: (projectId: string) => ({
			queryKey: ["test", "members", projectId],
			queryFn: async () => [],
		}),
	};
});

// The real ListGroup pulls in the full board/list task-row machinery, which
// needs a lot of unrelated fixture data (statuses, members, custom fields...).
// Stubbing it keeps this suite focused on AssignedTasksList's own job:
// fetching, grouping tasks by project, and wiring pagination/navigation.
vi.mock("../projects/interactions/list-group", () => ({
	ListGroup: (props: {
		projectId: string;
		groupDef: { label: string };
		tasks: { id: string; title: string; project_id: string }[];
		onTaskClick: (task: {
			id: string;
			title: string;
			project_id: string;
		}) => void;
	}) => (
		<div data-testid={`list-group-${props.projectId}`}>
			<div data-testid="group-label">{props.groupDef.label}</div>
			{props.tasks.map((task) => (
				<button
					type="button"
					key={task.id}
					onClick={() => props.onTaskClick(task)}
				>
					{task.title}
				</button>
			))}
		</div>
	),
}));

import { AssignedTasksList } from "./assigned-tasks-list";

// ── Fixtures ──────────────────────────────────────────────────────────────────

function makeTask(overrides: Record<string, unknown> = {}) {
	return {
		id: "task-1",
		project_id: "proj-a",
		title: "Do the thing",
		task_number: 1,
		importance: 0,
		custom_fields: {},
		created_at: "2026-01-01T00:00:00Z",
		updated_at: "2026-01-01T00:00:00Z",
		...overrides,
	};
}

function wrapper({ children }: { children: ReactNode }) {
	const qc = new QueryClient({
		defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
	});
	return <QueryClientProvider client={qc}>{children}</QueryClientProvider>;
}

function renderWidget() {
	return render(<AssignedTasksList />, { wrapper });
}

// ── Tests ─────────────────────────────────────────────────────────────────────

beforeEach(() => {
	mocks.navigateMock.mockReset();
	mocks.listAssignedTasksMock.mockReset();
});

describe("AssignedTasksList", () => {
	it("shows an error state instead of the empty state when the fetch fails", async () => {
		mocks.listAssignedTasksMock.mockRejectedValue(new Error("network error"));

		renderWidget();

		expect(
			await screen.findByText("Couldn't load your tasks. Please try again."),
		).toBeInTheDocument();
		expect(screen.queryByText("You're all caught up")).not.toBeInTheDocument();
		expect(screen.queryByTestId(/^list-group-/)).not.toBeInTheDocument();
	});

	it("shows the empty state when there are no assigned tasks", async () => {
		mocks.listAssignedTasksMock.mockResolvedValue({
			items: [],
			page_size: 10,
			next_cursor: null,
		});

		renderWidget();

		expect(await screen.findByText("You're all caught up")).toBeInTheDocument();
		expect(
			screen.getByText("No open tasks are assigned to you right now."),
		).toBeInTheDocument();
		expect(screen.queryByTestId(/^list-group-/)).not.toBeInTheDocument();
	});

	it("groups tasks by project and wires task clicks to navigation", async () => {
		mocks.listAssignedTasksMock.mockResolvedValue({
			items: [
				makeTask({ id: "task-a1", project_id: "proj-a", title: "A task" }),
				makeTask({ id: "task-b1", project_id: "proj-b", title: "B task" }),
			],
			page_size: 10,
			next_cursor: null,
		});

		renderWidget();

		const groupA = await screen.findByTestId("list-group-proj-a");
		const groupB = screen.getByTestId("list-group-proj-b");
		expect(within(groupA).getByText("Project Alpha")).toBeInTheDocument();
		expect(within(groupA).getByText("A task")).toBeInTheDocument();
		expect(within(groupB).getByText("Project Beta")).toBeInTheDocument();
		expect(within(groupB).getByText("B task")).toBeInTheDocument();

		await userEvent.click(within(groupB).getByText("B task"));
		expect(mocks.navigateMock).toHaveBeenCalledWith({
			to: "/projects/$projectId/tasks/$taskId",
			params: { projectId: "proj-b", taskId: "task-b1" },
		});
	});

	it("loads the next page on 'Load more' and hides the button once exhausted", async () => {
		mocks.listAssignedTasksMock.mockImplementation(
			async ({ pageParam }: { pageParam?: string }) => {
				if (!pageParam) {
					return {
						items: [makeTask({ id: "task-1", title: "First task" })],
						page_size: 1,
						next_cursor: "cursor-2",
					};
				}
				return {
					items: [makeTask({ id: "task-2", title: "Second task" })],
					page_size: 1,
					next_cursor: null,
				};
			},
		);

		renderWidget();

		expect(await screen.findByText("First task")).toBeInTheDocument();
		const loadMoreButton = screen.getByRole("button", { name: "Load more" });

		await userEvent.click(loadMoreButton);

		await waitFor(() => {
			expect(screen.getByText("Second task")).toBeInTheDocument();
		});
		expect(mocks.listAssignedTasksMock).toHaveBeenCalledTimes(2);
		expect(
			screen.queryByRole("button", { name: "Load more" }),
		).not.toBeInTheDocument();
	});
});
