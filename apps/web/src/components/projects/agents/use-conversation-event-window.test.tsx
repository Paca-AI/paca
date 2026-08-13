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
	type ConversationEventsTail,
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

// Opaque only in the sense the hook never parses it — this fake mirrors the
// real API's cursor as "the event_index it was encoded from" so the fake
// server below can decode it straight back.
const cursorOf = (index: number) => `cur:${index}`;
const indexOfCursor = (cursor: string) => Number(cursor.slice(4));

/**
 * Serves the same {items, total, next_cursor, prev_cursor} shape the real
 * API does: no cursor returns the newest `limit` events; `after`/`before`
 * seek by event_index in either direction. next_cursor is always present
 * for a non-empty page (a live stream can never definitively rule out more
 * arriving); prev_cursor is null only once event_index 0 is included.
 */
function fakeStream(initialCount: number) {
	let count = initialCount;
	mockGet.mockImplementation(
		async (
			_path: string,
			{
				params,
			}: { params: { after?: string; before?: string; limit: number } },
		) => {
			const { after, before, limit } = params;
			let items: AgentConversationEvent[];
			if (after !== undefined) {
				const from = indexOfCursor(after) + 1;
				const to = Math.min(from + limit - 1, count - 1);
				items = from <= to ? range(from, to) : [];
			} else if (before !== undefined) {
				const to = indexOfCursor(before) - 1;
				const from = Math.max(0, to - limit + 1);
				items = to >= 0 ? range(from, to) : [];
			} else {
				const to = count - 1;
				const from = Math.max(0, to - limit + 1);
				items = to >= 0 ? range(from, to) : [];
			}
			const first = items[0];
			const last = items.at(-1);
			return {
				data: {
					success: true,
					data: {
						items,
						total: count,
						next_cursor: last ? cursorOf(last.event_index) : null,
						prev_cursor:
							first && first.event_index > 0
								? cursorOf(first.event_index)
								: null,
					},
				},
			};
		},
	);
	return {
		/** The events server-side growth adds, ready to hand straight to `signal`. */
		grow(by: number): AgentConversationEvent[] {
			const from = count;
			count += by;
			return range(from, count - 1);
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
	 * What useProjectRealtime's applyRealtimeAgentEvent does per persisted
	 * agent event now: invalidate the conversation prefixes and append the
	 * full event onto the tail's live buffer (mirrored here rather than
	 * imported, to keep this test decoupled from that hook's internals). A
	 * window or signal keyed under either invalidated prefix would be
	 * refetched or reset by the event meant to report it.
	 */
	const signal = (events: AgentConversationEvent[]) =>
		act(() => {
			void queryClient.invalidateQueries({
				queryKey: ["projects", PROJECT_ID, "conversations"],
			});
			void queryClient.invalidateQueries({
				queryKey: ["global-chat", "conversations"],
			});
			queryClient.setQueryData(
				conversationEventsTailKey(CONVERSATION_ID),
				(prev: ConversationEventsTail | undefined): ConversationEventsTail => {
					const base = prev ?? { tick: 0, index: null, events: [] };
					const maxIndex = events.reduce(
						(max, e) => Math.max(max, e.event_index),
						base.index ?? -1,
					);
					return {
						tick: base.tick + 1,
						index: maxIndex,
						events: [...base.events, ...events],
					};
				},
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
		// No cursor on the opening request — it doesn't need to already know
		// how many events the conversation holds.
		expect(paramsOf(PROJECT_PATH, 0)).toEqual({ limit: 200 });
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
		const { result } = open();

		await waitFor(() => expect(result.current.events).toHaveLength(10));
		expect(result.current.hasOlder).toBe(false);
	});

	it("appends events as realtime reports them, without a re-fetch", async () => {
		const stream = fakeStream(275);
		const { result, signal } = open();
		await waitFor(() => expect(result.current.events).toHaveLength(200));
		const callsAfterOpen = requests(PROJECT_PATH).length;

		await signal(stream.grow(1));
		await waitFor(() => expect(lastIndex(result.current.events)).toBe(275));
		// The realtime message carried the full event — no GET needed to
		// render it.
		expect(requests(PROJECT_PATH)).toHaveLength(callsAfterOpen);
	});

	it("counts rather than fetches while the reader is scrolled away", async () => {
		const stream = fakeStream(275);
		const { result, signal } = open();
		await waitFor(() => expect(result.current.events).toHaveLength(200));
		const callsAfterOpen = requests(PROJECT_PATH).length;

		act(() => result.current.setFollowing(false));
		await signal(stream.grow(3));

		// Reported, not merged in — the reader's scrolled-away view shouldn't
		// move under them.
		await waitFor(() => expect(result.current.newBelow).toBe(3));
		expect(lastIndex(result.current.events)).toBe(274);
		expect(requests(PROJECT_PATH)).toHaveLength(callsAfterOpen);

		act(() => result.current.jumpToLatest());
		await waitFor(() => expect(lastIndex(result.current.events)).toBe(277));
		expect(result.current.newBelow).toBe(0);
		// Jumping back to latest surfaces what the live buffer already had —
		// still no extra fetch.
		expect(requests(PROJECT_PATH)).toHaveLength(callsAfterOpen);
	});

	it("keeps already-merged live events visible when following pauses mid-turn", async () => {
		const stream = fakeStream(200);
		const { result, signal } = open();
		await waitFor(() => expect(result.current.events).toHaveLength(200));
		const callsAfterOpen = requests(PROJECT_PATH).length;

		// A tool call starts streaming — still only in the live buffer, not
		// yet paginated (a real fetch only reconciles on a status transition).
		await signal(stream.grow(1));
		await waitFor(() => expect(lastIndex(result.current.events)).toBe(200));

		// The reader pauses following without navigating away — e.g.
		// expanding that tool call's panel nudges the viewport off the exact
		// bottom pixel. The event they're looking at must not disappear.
		act(() => result.current.setFollowing(false));
		expect(lastIndex(result.current.events)).toBe(200);
		expect(result.current.events).toHaveLength(201);

		// Further growth is still just reported, not merged, while paused.
		await signal(stream.grow(1));
		await waitFor(() => expect(result.current.newBelow).toBe(1));
		expect(lastIndex(result.current.events)).toBe(200);
		expect(requests(PROJECT_PATH)).toHaveLength(callsAfterOpen);

		act(() => result.current.jumpToLatest());
		await waitFor(() => expect(lastIndex(result.current.events)).toBe(201));
		expect(result.current.newBelow).toBe(0);
	});

	it("takes events for a conversation that was empty when opened", async () => {
		const stream = fakeStream(0);
		const { result, signal } = open();
		await waitFor(() => expect(result.current.isLoading).toBe(false));
		expect(result.current.events).toHaveLength(0);

		await signal(stream.grow(1));
		await waitFor(() => expect(result.current.events).toHaveLength(1));
		expect(lastIndex(result.current.events)).toBe(0);
	});

	it("merges a burst of live events all at once, regardless of page size", async () => {
		const stream = fakeStream(10);
		const { result, signal } = open({ pageSize: 5 });
		await waitFor(() => expect(lastIndex(result.current.events)).toBe(9));
		const callsAfterOpen = requests(PROJECT_PATH).length;

		// 12 new events arriving live, well past one 5-event page's worth —
		// the live buffer isn't paginated, so this needs no extra round
		// trips at all, unlike the old fetchNextPage-per-event catch-up.
		await signal(stream.grow(12));
		await waitFor(() => expect(lastIndex(result.current.events)).toBe(21));
		expect(requests(PROJECT_PATH)).toHaveLength(callsAfterOpen);
	});

	it("holds the first fetch until ready", async () => {
		fakeStream(275);
		const { wrapper } = harness();

		type Props = { ready: boolean };
		const initialProps: Props = { ready: false };

		const { result, rerender } = renderHook(
			({ ready }: Props) =>
				useConversationEventWindow({
					projectId: PROJECT_ID,
					conversationId: CONVERSATION_ID,
					ready,
					pageSize: 200,
				}),
			{ wrapper, initialProps },
		);

		expect(requests(PROJECT_PATH)).toHaveLength(0);

		rerender({ ready: true });
		await waitFor(() => expect(result.current.events).toHaveLength(200));
		expect(paramsOf(PROJECT_PATH, 0)).toEqual({ limit: 200 });
	});

	it("reads the global route when there is no project", async () => {
		fakeStream(30);
		const { result } = open({ projectId: undefined });

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
		const { result } = open({ pageSize: 100 });
		await waitFor(() => expect(result.current.events).toHaveLength(100));

		act(() => result.current.loadOlder());
		await waitFor(() => expect(result.current.events).toHaveLength(200));

		// The last hop back covers 50 events, not a full page — keyset
		// pagination on event_index can't overlap what's already loaded, unlike
		// an offset scheme where a naive full-page request here would have
		// refetched events 50..99.
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
