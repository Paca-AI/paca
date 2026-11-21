import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";

// ── Mocks ─────────────────────────────────────────────────────────────────────

const mocks = vi.hoisted(() => ({
	navigateMock: vi.fn(),
	listWaitingTasksMock: vi.fn(),
	listProjectsMock: vi.fn(),
}));

const defaultProjectsPage = {
	items: [
		{
			id: "proj-a",
			name: "Project Alpha",
			description: "",
			task_id_prefix: "ALP",
			settings: {},
			created_at: "2026-01-01T00:00:00Z",
		},
		{
			id: "proj-b",
			name: "Project Beta",
			description: "",
			task_id_prefix: "BET",
			settings: {},
			created_at: "2026-01-01T00:00:00Z",
		},
	],
	total: 2,
	page: 1,
	page_size: 50,
};

vi.mock("@tanstack/react-router", () => ({
	useNavigate: () => mocks.navigateMock,
}));

vi.mock("@/lib/interaction-api", async () => {
	const actual = await vi.importActual<typeof import("@/lib/interaction-api")>(
		"@/lib/interaction-api",
	);
	return {
		...actual,
		waitingTasksQueryOptions: (pageSize: number) => ({
			queryKey: ["test", "waiting-tasks", pageSize],
			queryFn: () => mocks.listWaitingTasksMock(pageSize),
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
		projectsLookupInfiniteQueryOptions: () => ({
			queryKey: ["test", "projects", "lookup"],
			queryFn: mocks.listProjectsMock,
			initialPageParam: 1,
			getNextPageParam: (lastPage: {
				page: number;
				page_size: number;
				total: number;
			}) =>
				lastPage.page * lastPage.page_size < lastPage.total
					? lastPage.page + 1
					: undefined,
		}),
		taskStatusesQueryOptions: (projectId: string) => ({
			queryKey: ["test", "statuses", projectId],
			queryFn: async () => [],
		}),
		taskTypesQueryOptions: (projectId: string) => ({
			queryKey: ["test", "types", projectId],
			queryFn: async () => [],
		}),
	};
});

// The real ListGroup pulls in the full board/list task-row machinery, which
// needs a lot of unrelated fixture data (statuses, members, custom fields...).
// Stubbing it keeps this suite focused on WaitingTasksList's own job:
// fetching, grouping tasks by project, and wiring navigation.
vi.mock("../projects/interactions/list-group", () => ({
	ListGroup: (props: {
		projectId: string;
		groupDef: { label: string };
		tasks: { id: string; title: string; project_id: string }[];
		visibleFields: string[];
		onTaskClick: (task: {
			id: string;
			title: string;
			project_id: string;
		}) => void;
	}) => (
		<div
			data-testid={`list-group-${props.projectId}`}
			data-visible-fields={props.visibleFields.join(",")}
		>
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

import { WaitingTasksList } from "./waiting-tasks-list";

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
	return render(<WaitingTasksList />, { wrapper });
}

// ── Tests ─────────────────────────────────────────────────────────────────────

beforeEach(() => {
	localStorage.clear();
	mocks.navigateMock.mockReset();
	mocks.listWaitingTasksMock.mockReset();
	mocks.listProjectsMock.mockReset();
	mocks.listProjectsMock.mockResolvedValue(defaultProjectsPage);
});

describe("WaitingTasksList", () => {
	it("shows an error state instead of the empty state when the fetch fails", async () => {
		mocks.listWaitingTasksMock.mockRejectedValue(new Error("network error"));

		renderWidget();

		expect(
			await screen.findByText("Couldn't load waiting tasks. Please try again."),
		).toBeInTheDocument();
		expect(screen.queryByText("Nothing is stuck")).not.toBeInTheDocument();
		expect(screen.queryByTestId(/^list-group-/)).not.toBeInTheDocument();
	});

	it("shows the empty state when there are no waiting tasks", async () => {
		mocks.listWaitingTasksMock.mockResolvedValue({
			items: [],
			page_size: 10,
			has_more: false,
		});

		renderWidget();

		expect(await screen.findByText("Nothing is stuck")).toBeInTheDocument();
		expect(
			screen.getByText("No open tasks are waiting on anyone right now."),
		).toBeInTheDocument();
		expect(screen.queryByTestId(/^list-group-/)).not.toBeInTheDocument();
	});

	it("groups tasks by project, wires task clicks to navigation, and flags more results", async () => {
		mocks.listWaitingTasksMock.mockResolvedValue({
			items: [
				makeTask({ id: "task-a1", project_id: "proj-a", title: "A task" }),
				makeTask({ id: "task-b1", project_id: "proj-b", title: "B task" }),
			],
			page_size: 10,
			has_more: true,
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

		expect(
			screen.getByText("More waiting tasks are not shown"),
		).toBeInTheDocument();
	});

	it("passes the default visible columns to the list and persists a count change", async () => {
		mocks.listWaitingTasksMock.mockResolvedValue({
			items: [
				makeTask({ id: "task-a1", project_id: "proj-a", title: "A task" }),
			],
			page_size: 10,
			has_more: false,
		});

		renderWidget();

		const group = await screen.findByTestId("list-group-proj-a");
		// Defaults: status + waiting age; type/importance are opt-in.
		expect(group.dataset.visibleFields).toBe("status,waiting");

		// Switching the list size persists and refetches with the new size.
		await userEvent.click(screen.getAllByText("Show 10")[0]);
		await userEvent.click(await screen.findByText("Show 25"));

		await screen.findByText("Show 25");
		expect(localStorage.getItem("home.waitingTasks.pageSize")).toBe("25");
	});
});
