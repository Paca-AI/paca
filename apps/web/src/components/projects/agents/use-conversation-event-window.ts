import { useInfiniteQuery, useQuery } from "@tanstack/react-query";
import { useEffect, useMemo, useState } from "react";
import {
	type AgentConversationEvent,
	CONVERSATION_EVENTS_PAGE_SIZE,
	conversationEventCountQueryOptions,
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
 * extending upwards on demand and forwards as events arrive.
 *
 * Paging by offset is safe on a live stream because `event_index` is gapless: an
 * index is an offset, so a page addresses the same events however many arrive
 * later. Pages therefore stay contiguous, which `eventsToThreadMessages`
 * requires — it carries state across the array it is given (open tool calls keyed
 * by id, the assistant message being accumulated) and is only correct over an
 * unbroken run.
 */
export function useConversationEventWindow({
	projectId,
	conversationId,
	eventCount,
	/** Holds the first fetch until `eventCount` is settled, avoiding a probe. */
	ready = true,
	pageSize = CONVERSATION_EVENTS_PAGE_SIZE,
}: {
	/** Absent for a global-chat conversation. */
	projectId?: string | undefined;
	conversationId: string;
	eventCount: number | undefined;
	ready?: boolean;
	pageSize?: number;
}): UseConversationEventWindow {
	// Whether the reader is at the newest event. Gates fetching forwards, so a
	// reader looking at history does not pull events they cannot see.
	const [following, setFollowing] = useState(true);

	const { data: tail } = useQuery(
		conversationEventsTailQueryOptions(conversationId),
	);
	const tailIndex = tail?.index ?? null;

	const needsCount = typeof eventCount !== "number";
	const { data: probedCount } = useQuery({
		...conversationEventCountQueryOptions({ projectId, conversationId }),
		enabled: ready && needsCount,
	});
	const count = eventCount ?? probedCount;
	// Realtime can know about events before a count fetched earlier does.
	const known = Math.max(count ?? 0, (tailIndex ?? -1) + 1);

	const {
		data,
		isPending,
		hasNextPage,
		isFetchingNextPage,
		fetchNextPage,
		hasPreviousPage,
		isFetchingPreviousPage,
		fetchPreviousPage,
	} = useInfiniteQuery({
		...conversationEventWindowInfiniteOptions({
			projectId,
			conversationId,
			count: known,
			tailIndex,
			pageSize,
		}),
		// Nothing to window until at least one event exists; a conversation that
		// is empty when opened starts fetching when realtime reports its first.
		enabled: ready && count !== undefined && known > 0,
	});

	const events = useMemo(
		() => data?.pages.flatMap((page) => page.items) ?? [],
		[data],
	);

	// Catch up to the tail a page at a time. Depends on `data` so an append that
	// still leaves more to fetch re-runs this: the other values are unchanged
	// across it, and a burst larger than one page would otherwise stall.
	useEffect(() => {
		if (!data || !following || !hasNextPage || isFetchingNextPage) return;
		void fetchNextPage();
	}, [data, following, hasNextPage, isFetchingNextPage, fetchNextPage]);

	const loaded =
		events.length === 0 ? 0 : (events.at(-1)?.event_index ?? -1) + 1;
	const serverKnown = Math.max(
		data?.pages.at(-1)?.total ?? 0,
		(tailIndex ?? -1) + 1,
		loaded,
	);

	return {
		events,
		// A conversation with no events is loaded, not loading.
		isLoading: count === undefined || (known > 0 && isPending),
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
