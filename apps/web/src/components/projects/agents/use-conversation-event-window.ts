import { useInfiniteQuery, useQuery } from "@tanstack/react-query";
import { useMemo, useState } from "react";
import {
	type AgentConversationEvent,
	CONVERSATION_EVENTS_PAGE_SIZE,
	conversationEventsTailQueryOptions,
	conversationEventWindowInfiniteOptions,
} from "@/lib/agent-api";

export type UseConversationEventWindow = {
	/** The loaded events, oldest first and contiguous. */
	events: AgentConversationEvent[];
	isLoading: boolean;
	hasOlder: boolean;
	isLoadingOlder: boolean;
	loadOlder: () => void;
	/** Events waiting past the window while not following. */
	newBelow: number;
	following: boolean;
	setFollowing: (following: boolean) => void;
	jumpToLatest: () => void;
};

/**
 * Reads a conversation's events as a window anchored at the newest page,
 * extending upwards on demand, with newly-arrived events merged in live
 * from realtime rather than fetched.
 *
 * Cursor-paginated on `event_index` (see conversationEventWindowInfiniteOptions):
 * the window opens on the newest page with no cursor at all, so unlike an
 * offset scheme it never needs to learn the conversation's length first.
 * Pages stay contiguous, which `eventsToThreadMessages` requires — it carries
 * state across the array it is given (open tool calls keyed by id, the
 * assistant message being accumulated) and is only correct over an unbroken
 * run.
 *
 * Realtime no longer just reports "something changed, go re-fetch" — every
 * persisted event's message now carries the full row (see
 * services/agent-runner's `persistAndPublish` and this module's
 * `applyRealtimeAgentEvent`), buffered in the conversation's tail cache. So
 * instead of calling `fetchNextPage()` once per realtime message (one HTTP
 * round trip per event — the actual thing this used to do), that buffer is
 * merged directly on top of whatever's been fetched. A real fetch still
 * happens for the initial page and for `loadOlder`, and gets triggered
 * externally (see useProjectRealtime) to reconcile the two whenever a
 * conversation reaches a status transition — the live buffer's own job is
 * just to cover the gap until that lands.
 */
export function useConversationEventWindow({
	projectId,
	conversationId,
	ready = true,
	pageSize = CONVERSATION_EVENTS_PAGE_SIZE,
}: {
	/** Absent for a global-chat conversation. */
	projectId?: string | undefined;
	conversationId: string;
	ready?: boolean;
	pageSize?: number;
}): UseConversationEventWindow {
	// Whether the reader is at the newest event. Gates merging the live
	// buffer in, so a reader looking at history doesn't have new content
	// silently appended under them — see `events` below.
	const [following, setFollowing] = useState(true);

	const { data: tail } = useQuery(
		conversationEventsTailQueryOptions(conversationId),
	);
	const tailIndex = tail?.index ?? null;
	const liveEvents = tail?.events ?? [];

	const {
		data,
		isPending,
		hasPreviousPage,
		isFetchingPreviousPage,
		fetchPreviousPage,
	} = useInfiniteQuery({
		...conversationEventWindowInfiniteOptions({
			projectId,
			conversationId,
			tailIndex,
			pageSize,
		}),
		enabled: ready,
	});

	const pagedEvents = useMemo(
		() => data?.pages.flatMap((page) => page.items) ?? [],
		[data],
	);
	const lastPagedIndex =
		pagedEvents.length > 0 ? (pagedEvents.at(-1)?.event_index ?? -1) : -1;

	// Merge in whatever the tail has buffered live that a real fetch hasn't
	// caught up to yet — only while following. A reader scrolled away into
	// history sees `newBelow`'s count grow instead, so their view doesn't
	// shift under them.
	const events = useMemo(() => {
		if (!following) return pagedEvents;
		const extra = liveEvents.filter((e) => e.event_index > lastPagedIndex);
		return extra.length > 0 ? [...pagedEvents, ...extra] : pagedEvents;
	}, [pagedEvents, liveEvents, lastPagedIndex, following]);

	const loaded =
		events.length === 0 ? 0 : (events.at(-1)?.event_index ?? -1) + 1;
	const serverKnown = Math.max(
		data?.pages.at(-1)?.total ?? 0,
		(tailIndex ?? -1) + 1,
		loaded,
	);

	return {
		events,
		isLoading: isPending,
		hasOlder: hasPreviousPage,
		isLoadingOlder: isFetchingPreviousPage,
		loadOlder: () => {
			void fetchPreviousPage();
		},
		newBelow: following ? 0 : Math.max(0, serverKnown - loaded),
		following,
		setFollowing,
		jumpToLatest: () => setFollowing(true),
	};
}
