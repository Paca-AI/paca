// Tests for agent-activity-tab.tsx
// Key regression: task activity descriptions (assignee/reporter/sprint changes)
// must resolve project member/sprint names, not fall back to raw member ids.

import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import type { ComponentProps, ReactNode } from "react";
import { describe, expect, it, vi } from "vitest";

// ActivityRow always renders a router <Link>, which needs a RouterProvider
// ancestor to resolve href/isServer — replace it with a plain anchor so the
// component can render standalone in this test.
vi.mock("@tanstack/react-router", async () => {
	const actual = await vi.importActual<typeof import("@tanstack/react-router")>(
		"@tanstack/react-router",
	);
	return {
		...actual,
		Link: ({
			children,
			to,
			params,
		}: ComponentProps<"a"> & Record<string, unknown>) => (
			<a
				href={typeof to === "string" ? to : undefined}
				data-params={JSON.stringify(params)}
			>
				{children}
			</a>
		),
	};
});

// agentActivitiesQueryOptions/projectMembersQueryOptions/sprintsQueryOptions
// all call their fetcher (listAgentActivities/listProjectMembers/listSprints)
// from within the *same* module, so mocking just the fetcher export doesn't
// intercept that internal call (ESM same-module bindings aren't affected by
// vi.mock's export override). Mock the *QueryOptions functions directly instead.
const { mockListAgentActivities, mockListProjectMembers, mockListSprints } =
	vi.hoisted(() => ({
		mockListAgentActivities: vi.fn(),
		mockListProjectMembers: vi.fn(),
		mockListSprints: vi.fn(),
	}));

vi.mock("@/lib/agent-api", async () => {
	const actual =
		await vi.importActual<typeof import("@/lib/agent-api")>("@/lib/agent-api");
	return {
		...actual,
		agentActivitiesQueryOptions: () => ({
			queryKey: ["test", "agentActivities"],
			queryFn: mockListAgentActivities,
			initialPageParam: undefined,
			getNextPageParam: (lastPage: { next_cursor: string | null }) =>
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
		projectMembersQueryOptions: () => ({
			queryKey: ["test", "members"],
			queryFn: mockListProjectMembers,
		}),
	};
});

vi.mock("@/lib/interaction-api", async () => {
	const actual = await vi.importActual<typeof import("@/lib/interaction-api")>(
		"@/lib/interaction-api",
	);
	return {
		...actual,
		sprintsQueryOptions: () => ({
			queryKey: ["test", "sprints"],
			queryFn: mockListSprints,
		}),
	};
});

import { AgentActivityTab } from "./agent-activity-tab";

function wrapper({ children }: { children: ReactNode }) {
	const qc = new QueryClient({
		defaultOptions: { queries: { retry: false } },
	});
	return <QueryClientProvider client={qc}>{children}</QueryClientProvider>;
}

const PROJECT_ID = "proj-1";
const AGENT_ID = "agent-1";
const MEMBER_ID = "3f7c2b10-aaaa-bbbb-cccc-1234567890ab";

describe("AgentActivityTab", () => {
	it("resolves the assignee's display name instead of a truncated member id", async () => {
		mockListAgentActivities.mockResolvedValue({
			items: [
				{
					id: "activity-1",
					source_type: "task",
					source_id: "task-1",
					source_title: "Fix the login bug",
					source_deleted: false,
					activity_type: "task.updated",
					content: {
						changes: [{ field: "assignee", old: [], new: [MEMBER_ID] }],
					},
					created_at: "2026-01-01T00:00:00Z",
					updated_at: "2026-01-01T00:00:00Z",
				},
			],
			page_size: 20,
			next_cursor: null,
		});
		mockListProjectMembers.mockResolvedValue([
			{
				id: MEMBER_ID,
				project_id: PROJECT_ID,
				user_id: "user-1",
				project_role_id: "role-1",
				username: "jdoe",
				full_name: "Jane Doe",
				role_name: "Member",
			},
		]);
		mockListSprints.mockResolvedValue([]);

		render(<AgentActivityTab projectId={PROJECT_ID} agentId={AGENT_ID} />, {
			wrapper,
		});

		await waitFor(() => {
			expect(screen.getByText(/Jane Doe/)).toBeInTheDocument();
		});
		expect(
			screen.queryByText(new RegExp(MEMBER_ID.slice(0, 8))),
		).not.toBeInTheDocument();
	});

	it("falls back to a truncated id while member names haven't loaded yet", async () => {
		mockListAgentActivities.mockResolvedValue({
			items: [
				{
					id: "activity-1",
					source_type: "task",
					source_id: "task-1",
					source_title: "Fix the login bug",
					source_deleted: false,
					activity_type: "task.updated",
					content: {
						changes: [{ field: "assignee", old: [], new: [MEMBER_ID] }],
					},
					created_at: "2026-01-01T00:00:00Z",
					updated_at: "2026-01-01T00:00:00Z",
				},
			],
			page_size: 20,
			next_cursor: null,
		});
		mockListProjectMembers.mockResolvedValue([]);
		mockListSprints.mockResolvedValue([]);

		render(<AgentActivityTab projectId={PROJECT_ID} agentId={AGENT_ID} />, {
			wrapper,
		});

		await waitFor(() => {
			expect(
				screen.getByText(new RegExp(MEMBER_ID.slice(0, 8))),
			).toBeInTheDocument();
		});
	});

	it("renders a deleted source's title as plain text instead of a link", async () => {
		mockListAgentActivities.mockResolvedValue({
			items: [
				{
					id: "activity-1",
					source_type: "task",
					source_id: "task-1",
					source_title: "Fix the login bug",
					source_deleted: true,
					activity_type: "task.deleted",
					content: {},
					created_at: "2026-01-01T00:00:00Z",
					updated_at: "2026-01-01T00:00:00Z",
				},
			],
			page_size: 20,
			next_cursor: null,
		});
		mockListProjectMembers.mockResolvedValue([]);
		mockListSprints.mockResolvedValue([]);

		render(<AgentActivityTab projectId={PROJECT_ID} agentId={AGENT_ID} />, {
			wrapper,
		});

		await waitFor(() => {
			expect(screen.getByText("Fix the login bug")).toBeInTheDocument();
		});
		expect(
			screen.queryByRole("link", { name: /Fix the login bug/ }),
		).not.toBeInTheDocument();
	});
});
