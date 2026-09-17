// Tests for conversations-layout.tsx's mobile master-detail branching (which
// of the list / detail (Outlet) panes show for each combination of
// isMobile, activeConversationId (from the URL params), and the `compose`
// search param — see conversations/index.tsx's validateSearch), plus a
// couple of light checks on the list item's name/rename-delete-menu
// rendering (deep click-through-the-menu-into-a-dialog interaction is left
// to manual browser verification — no other test in this codebase drives a
// Radix DropdownMenu through jsdom, and this component's own dialogs are
// simple, already-proven patterns copied from rename-view-dialog.tsx /
// interaction-layout.tsx's task-delete dialog).

import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import type { ComponentProps, ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { AgentConversation } from "@/lib/agent-api";

const { mockUseParams, mockUseSearch, mockUseIsMobile, conversationItemsRef } =
	vi.hoisted(() => ({
		mockUseParams: vi.fn(),
		mockUseSearch: vi.fn(),
		mockUseIsMobile: vi.fn(),
		conversationItemsRef: { current: [] as AgentConversation[] },
	}));

// ConversationsLayout reads conversationId/compose via useParams/useSearch
// and renders <Link>/<Outlet> — none of which resolve without a real
// RouterProvider ancestor. Mock all four so the component's own show/hide
// branching (what's under test here) can render standalone.
vi.mock("@tanstack/react-router", async () => {
	const actual = await vi.importActual<typeof import("@tanstack/react-router")>(
		"@tanstack/react-router",
	);
	return {
		...actual,
		useParams: mockUseParams,
		useSearch: mockUseSearch,
		Link: ({
			children,
			to,
			...rest
		}: ComponentProps<"a"> & Record<string, unknown>) => (
			<a href={typeof to === "string" ? to : undefined} {...rest}>
				{children}
			</a>
		),
		Outlet: () => <div data-testid="outlet-pane" />,
	};
});

vi.mock("@/hooks/use-mobile", () => ({ useIsMobile: mockUseIsMobile }));

vi.mock("@/hooks/use-project-permissions", () => ({
	useProjectPermissions: () => ({
		hasProjectPermission: () => true,
		isLoading: false,
	}),
}));

vi.mock("@/hooks/use-project-realtime", () => ({
	useProjectRealtime: () => {},
}));

vi.mock("@/hooks/use-global-agent-realtime", () => ({
	useGlobalAgentRealtime: () => {},
}));

// agentsQueryOptions/conversationsQueryOptions call their fetchers from
// within the *same* module, so mocking just the fetcher export doesn't
// intercept that internal call — mock the *QueryOptions functions directly
// instead (same reasoning as agent-activity-tab.test.tsx).
vi.mock("@/lib/agent-api", async () => {
	const actual =
		await vi.importActual<typeof import("@/lib/agent-api")>("@/lib/agent-api");
	const page = () => ({
		items: conversationItemsRef.current,
		next_cursor: null,
	});
	return {
		...actual,
		agentsQueryOptions: () => ({
			queryKey: ["test", "agents"],
			queryFn: () => Promise.resolve([]),
		}),
		chattableAgentsQueryOptions: {
			queryKey: ["test", "chattable-agents"],
			queryFn: () => Promise.resolve([]),
		},
		conversationsQueryOptions: () => ({
			queryKey: ["test", "conversations"],
			queryFn: () => Promise.resolve(page()),
			initialPageParam: undefined,
			getNextPageParam: () => undefined,
		}),
		globalConversationsQueryOptions: () => ({
			queryKey: ["test", "global-conversations"],
			queryFn: () => Promise.resolve(page()),
			initialPageParam: undefined,
			getNextPageParam: () => undefined,
		}),
	};
});

import { ConversationsLayout } from "./conversations-layout";

function wrapper({ children }: { children: ReactNode }) {
	const qc = new QueryClient({
		defaultOptions: { queries: { retry: false } },
	});
	return <QueryClientProvider client={qc}>{children}</QueryClientProvider>;
}

function setRoute({
	conversationId,
	compose = false,
}: {
	conversationId?: string;
	compose?: boolean;
}) {
	mockUseParams.mockReturnValue({ conversationId });
	mockUseSearch.mockReturnValue({ compose });
}

function fakeConversation(
	overrides: Partial<AgentConversation> = {},
): AgentConversation {
	return {
		id: "conv-1",
		agent_id: "agent-1",
		trigger_type: "chat_message",
		status: "finished",
		iteration_count: 0,
		input_tokens: 0,
		output_tokens: 0,
		total_tokens: 0,
		created_at: "2026-01-01T00:00:00Z",
		updated_at: "2026-01-01T00:00:00Z",
		...overrides,
	};
}

describe("ConversationsLayout", () => {
	beforeEach(() => {
		mockUseIsMobile.mockReturnValue(false);
		setRoute({});
		conversationItemsRef.current = [];
	});

	it("shows both the list and the detail pane on desktop", () => {
		setRoute({ conversationId: undefined, compose: false });
		render(<ConversationsLayout projectId="proj-1" />, { wrapper });
		expect(screen.getByText("Conversations")).toBeInTheDocument();
		expect(screen.getByTestId("outlet-pane")).toBeInTheDocument();
	});

	it("shows both panes on desktop even with an active conversation", () => {
		setRoute({ conversationId: "conv-1", compose: false });
		render(<ConversationsLayout projectId="proj-1" />, { wrapper });
		expect(screen.getByText("Conversations")).toBeInTheDocument();
		expect(screen.getByTestId("outlet-pane")).toBeInTheDocument();
	});

	it("shows only the list on mobile when landing fresh", () => {
		mockUseIsMobile.mockReturnValue(true);
		setRoute({ conversationId: undefined, compose: false });
		render(<ConversationsLayout projectId="proj-1" />, { wrapper });
		expect(screen.getByText("Conversations")).toBeInTheDocument();
		expect(screen.queryByTestId("outlet-pane")).not.toBeInTheDocument();
		expect(screen.queryByText("Back")).not.toBeInTheDocument();
	});

	it("shows only the detail pane, with a Back link, after New conversation sets ?compose=true", () => {
		mockUseIsMobile.mockReturnValue(true);
		setRoute({ conversationId: undefined, compose: true });
		render(<ConversationsLayout projectId="proj-1" />, { wrapper });
		expect(screen.queryByText("Conversations")).not.toBeInTheDocument();
		expect(screen.getByTestId("outlet-pane")).toBeInTheDocument();
		expect(screen.getByText("Back")).toBeInTheDocument();
	});

	it("shows only the detail pane, with a Back link, when viewing an actual conversation", () => {
		mockUseIsMobile.mockReturnValue(true);
		setRoute({ conversationId: "conv-1", compose: false });
		render(<ConversationsLayout projectId="proj-1" />, { wrapper });
		expect(screen.queryByText("Conversations")).not.toBeInTheDocument();
		expect(screen.getByTestId("outlet-pane")).toBeInTheDocument();
		expect(screen.getByText("Back")).toBeInTheDocument();
	});

	describe("conversation naming", () => {
		it("shows a custom title in place of the agent name once one is set", async () => {
			conversationItemsRef.current = [
				fakeConversation({ title: "Fix the login bug" }),
			];
			render(<ConversationsLayout projectId="proj-1" />, { wrapper });
			expect(await screen.findByText("Fix the login bug")).toBeInTheDocument();
		});

		it("falls back to the agent id when there is no title and no matching agent (unchanged pre-naming behavior)", async () => {
			conversationItemsRef.current = [fakeConversation({ title: null })];
			render(<ConversationsLayout projectId="proj-1" />, { wrapper });
			expect(
				await screen.findByText("agent-1".slice(0, 8)),
			).toBeInTheDocument();
		});

		it("shows the rename/delete menu trigger when the caller can manage conversations", async () => {
			conversationItemsRef.current = [fakeConversation()];
			render(<ConversationsLayout projectId="proj-1" />, { wrapper });
			expect(
				await screen.findByRole("button", { name: "More actions" }),
			).toBeInTheDocument();
		});
	});
});
