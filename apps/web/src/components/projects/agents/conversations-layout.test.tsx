// Tests for conversations-layout.tsx's mobile master-detail branching: which
// of the list / detail (Outlet) panes show for each combination of
// isMobile, activeConversationId (from the URL params), and the `compose`
// search param (see conversations/index.tsx's validateSearch) that tracks
// "New conversation" intent while still on the bare index route.

import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import type { ComponentProps, ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";

const { mockUseParams, mockUseSearch, mockUseIsMobile } = vi.hoisted(() => ({
	mockUseParams: vi.fn(),
	mockUseSearch: vi.fn(),
	mockUseIsMobile: vi.fn(),
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
	const emptyPage = { items: [], next_cursor: null };
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
			queryFn: () => Promise.resolve(emptyPage),
			initialPageParam: undefined,
			getNextPageParam: () => undefined,
		}),
		globalConversationsQueryOptions: () => ({
			queryKey: ["test", "global-conversations"],
			queryFn: () => Promise.resolve(emptyPage),
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

describe("ConversationsLayout", () => {
	beforeEach(() => {
		mockUseIsMobile.mockReturnValue(false);
		setRoute({});
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
});
