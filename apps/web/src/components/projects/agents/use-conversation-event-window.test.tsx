import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, renderHook, waitFor } from "@testing-library/react";
import type { PropsWithChildren } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";

const { mockGet } = vi.hoisted(() => ({ mockGet: vi.fn() }));

vi.mock("@/lib/api-client", () => ({
	apiClient: { instance: { get: mockGet } },
}));

import {
	type AgentConversationEvent,
	conversationEventsTailKey,
	conversationEventWindowKey,
} from "@/lib/agent-api";
import { useConversationEventWindow } from "./use-conversation-event-window";

const PROJECT_ID = "proj-1";
const CONVERSATION_ID = "conv-1";

function ev(index: number): AgentConversationEvent {
	return {
		id: `event-${index}`,
		conversation_id: CONVERSATION_ID,
		event_index: index,
		event_type: "ACPToolCallEvent",
		event_source: "agent",
		payload: {},
		created_at: "2026-01-01T00:00:00Z",
	};
}

const range = (from: number, to: number) =>
	Array.from({ length: to - from + 1 }, (_, i) => ev(from + i));

const PROJECT_PATH = `/projects/${PROJECT_ID}/conversations/${CONVERSATION_ID}/events`;
const GLOBAL_PATH = `/agents/conversations/${CONVERSATION_ID}/events`;

/** Serves `[offset, offset+limit)` plus the current total, as the API does. */
function fakeStream(initialCount: number) {
	let count = initialCount;
	mockGet.mockImplementation(
		async (
			_path: string,
			{ params }: { params: { offset: number; limit: number } },
		) => ({
			data: {
				success: true,
				data: {
					items: range(
						params.offset,
						Math.min(params.offset + params.limit, count) - 1,
					).filter((e) => e.event_index < count),
					total: count,
				},
			},
		}),
	);
	return {
		grow(by: number) {
			count += by;
			return count - 1; // highest index now present
		},
	};
}

const requests = (path: string) =>
	mockGet.mock.calls.filter(([p]) => p === path);
const paramsOf = (path: string, index = 0) =>
	requests(path)[index]?.[1]?.params;

function harness() {
	const queryClient = new QueryClient({
		defaultOptions: { queries: { retry: false } },
	});
	const wrapper = ({ children }: PropsWithChildren) => (
		<QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
	);
	/**
	 * What the realtime hooks do per agent event: invalidate the conversation
	 * prefixes *and* write the tail signal. A window or signal keyed under either
	 * prefix would be refetched or reset by the event meant to report it.
	 */
	const signal = (index: number | null) =>
		act(() => {
			void queryClient.invalidateQueries({
				queryKey: ["projects", PROJECT_ID, "conversations"],
			});
			void queryClient.invalidateQueries({
				queryKey: ["global-chat", "conversations"],
			});
			queryClient.setQueryData(
				conversationEventsTailKey(CONVERSATION_ID),
				(prev: { tick: number; index: number | null } | undefined) => ({
					tick: (prev?.tick ?? 0) + 1,
					index,
				}),
			);
		});
	return { queryClient, wrapper, signal };
}

const lastIndex = (events: AgentConversationEvent[]) =>
	events.at(-1)?.event_index;

function open(
	props: Partial<Parameters<typeof useConversationEventWindow>[0]> = {},
) {
	const { wrapper, signal, queryClient } = harness();
	const rendered = renderHook(
		() =>
			useConversationEventWindow({
				projectId: PROJECT_ID,
				conversationId: CONVERSATION_ID,
				eventCount: 275,
				pageSize: 200,
				...props,
			}),
		{ wrapper },
	);
	return { ...rendered, signal, queryClient };
}

describe("useConversationEventWindow", () => {
	beforeEach(() => {
		vi.clearAllMocks();
	});

	it("opens on the newest page rather than the whole stream", async () => {
		fakeStream(275);
		const { result } = open();

		await waitFor(() => expect(result.current.isLoading).toBe(false));
		expect(result.current.events).toHaveLength(200);
		expect(result.current.events[0].event_index).toBe(75);
		expect(lastIndex(result.current.events)).toBe(274);
		expect(result.current.hasOlder).toBe(true);
		expect(requests(PROJECT_PATH)).toHaveLength(1);
	});

	it("pages older events in without refetching what it holds", async () => {
		fakeStream(275);
		const { result } = open({ pageSize: 100 });
		await waitFor(() => expect(result.current.events).toHaveLength(100));

		act(() => result.current.loadOlder());
		await waitFor(() => expect(result.current.events).toHaveLength(200));

		// One request per page, and still one unbroken ascending run.
		expect(requests(PROJECT_PATH)).toHaveLength(2);
		expect(result.current.events[0].event_index).toBe(75);
		expect(lastIndex(result.current.events)).toBe(274);
	});

	it("stops offering older events at the start of the stream", async () => {
		fakeStream(10);
		const { result } = open({ eventCount: 10 });

		await waitFor(() => expect(result.current.events).toHaveLength(10));
		expect(result.current.hasOlder).toBe(false);
	});

	it("appends events as realtime reports them", async () => {
		const stream = fakeStream(275);
		const { result, signal } = open();
		await waitFor(() => expect(result.current.events).toHaveLength(200));

		await signal(stream.grow(1));
		await waitFor(() => expect(lastIndex(result.current.events)).toBe(275));
	});

	it("counts rather than fetches while the reader is scrolled away", async () => {
		const stream = fakeStream(275);
		const { result, signal } = open();
		await waitFor(() => expect(result.current.events).toHaveLength(200));
		const callsAfterOpen = requests(PROJECT_PATH).length;

		act(() => result.current.setFollowing(false));
		await signal(stream.grow(3));

		// Reported, not downloaded.
		await waitFor(() => expect(result.current.newBelow).toBe(3));
		expect(lastIndex(result.current.events)).toBe(274);
		expect(requests(PROJECT_PATH)).toHaveLength(callsAfterOpen);

		act(() => result.current.jumpToLatest());
		await waitFor(() => expect(lastIndex(result.current.events)).toBe(277));
		expect(result.current.newBelow).toBe(0);
	});

	it("takes events for a conversation that was empty when opened", async () => {
		const stream = fakeStream(0);
		const { result, signal } = open({ eventCount: 0 });
		await waitFor(() => expect(result.current.isLoading).toBe(false));
		expect(result.current.events).toHaveLength(0);

		await signal(stream.grow(1));
		await waitFor(() => expect(result.current.events).toHaveLength(1));
		expect(lastIndex(result.current.events)).toBe(0);
	});

	it("catches up across a burst that outruns one page", async () => {
		const stream = fakeStream(10);
		const { result, signal } = open({ eventCount: 10, pageSize: 5 });
		await waitFor(() => expect(lastIndex(result.current.events)).toBe(9));

		// 12 new events with a 5-event page: needs three round trips.
		await signal(stream.grow(12));
		await waitFor(() => expect(lastIndex(result.current.events)).toBe(21), {
			timeout: 3000,
		});
	});

	it("probes for the count when the API does not report one", async () => {
		fakeStream(50);
		const { result } = open({ eventCount: undefined });

		await waitFor(() => expect(result.current.events).toHaveLength(50));
		// First call is the one-row probe, then the window itself.
		expect(paramsOf(PROJECT_PATH, 0)).toEqual({ offset: 0, limit: 1 });
	});

	it("holds the first fetch until the event count is settled", async () => {
		fakeStream(275);
		const { wrapper } = harness();

		type Props = { ready: boolean; eventCount: number | undefined };
		const initialProps: Props = { ready: false, eventCount: undefined };

		const { result, rerender } = renderHook(
			({ ready, eventCount }: Props) =>
				useConversationEventWindow({
					projectId: PROJECT_ID,
					conversationId: CONVERSATION_ID,
					eventCount,
					ready,
					pageSize: 200,
				}),
			{ wrapper, initialProps },
		);

		expect(requests(PROJECT_PATH)).toHaveLength(0);

		rerender({ ready: true, eventCount: 275 });
		await waitFor(() => expect(result.current.events).toHaveLength(200));
		// Went straight to the tail: no probe was needed.
		expect(paramsOf(PROJECT_PATH, 0)).toEqual({ offset: 75, limit: 200 });
	});

	it("reads the global route when there is no project", async () => {
		fakeStream(30);
		const { result } = open({ projectId: undefined, eventCount: 30 });

		await waitFor(() => expect(result.current.events).toHaveLength(30));
		expect(requests(GLOBAL_PATH)).toHaveLength(1);
		expect(requests(PROJECT_PATH)).toHaveLength(0);
	});

	it("keys the window and the signal outside the invalidated prefixes", () => {
		// An infinite query refetches every page it holds, so an incidental prefix
		// invalidation would re-read the whole stream.
		for (const key of [
			conversationEventWindowKey(CONVERSATION_ID),
			conversationEventsTailKey(CONVERSATION_ID),
		]) {
			expect(key[0]).not.toBe("projects");
			expect(key[0]).not.toBe("global-chat");
		}
	});
	it("keeps paging back to the start without overlapping what is held", async () => {
		fakeStream(250);
		const { result } = open({ eventCount: 250, pageSize: 100 });
		await waitFor(() => expect(result.current.events).toHaveLength(100));

		act(() => result.current.loadOlder());
		await waitFor(() => expect(result.current.events).toHaveLength(200));

		// The last hop back covers 50 events, not a full page: asking for a full
		// one would refetch events 50..99, which are already loaded.
		act(() => result.current.loadOlder());
		await waitFor(() => expect(result.current.events).toHaveLength(250));

		expect(result.current.events[0].event_index).toBe(0);
		expect(result.current.hasOlder).toBe(false);
		// One unbroken ascending run, no duplicates.
		expect(result.current.events.map((e) => e.event_index)).toEqual(
			Array.from({ length: 250 }, (_, i) => i),
		);
	});
});
