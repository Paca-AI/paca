import { useInfiniteQuery, useQuery } from "@tanstack/react-query";
import { useEffect, useMemo, useState } from "react";
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
 * extending upwards on demand and forwards as events arrive.
 *
 * Cursor-paginated on `event_index` (see conversationEventWindowInfiniteOptions):
 * the window opens on the newest page with no cursor at all, so unlike an
 * offset scheme it never needs to learn the conversation's length first.
 * Pages stay contiguous, which `eventsToThreadMessages` requires — it carries
 * state across the array it is given (open tool calls keyed by id, the
 * assistant message being accumulated) and is only correct over an unbroken
 * run.
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
	// Whether the reader is at the newest event. Gates fetching forwards, so a
	// reader looking at history does not pull events they cannot see.
	const [following, setFollowing] = useState(true);

	const { data: tail } = useQuery(
		conversationEventsTailQueryOptions(conversationId),
	);
	const tailIndex = tail?.index ?? null;

	const {
		data,
		isPending,
		hasNextPage,
		isFetchingNextPage,
		fetchNextPage,
		hasPreviousPage,
		isFetchingPreviousPage,
		fetchPreviousPage,
		refetch,
	} = useInfiniteQuery({
		...conversationEventWindowInfiniteOptions({
			projectId,
			conversationId,
			tailIndex,
			pageSize,
		}),
		enabled: ready,
	});

	const events = useMemo(
		() => data?.pages.flatMap((page) => page.items) ?? [],
		[data],
	);

	// Catch up to the tail a page at a time. Depends on `data` so an append that
	// still leaves more to fetch re-runs this: the other values are unchanged
	// across it, and a burst larger than one page would otherwise stall.
	//
	// A conversation with no events yet is the one case fetchNextPage can't
	// drive this: its sole (empty) page carries no next_cursor to resume
	// from, since there is no last event to encode one from. Re-opening on
	// the tail via a plain refetch once realtime reports the first event
	// replaces that empty page in place, rather than appending a second
	// (still cursor-less) one that fetchNextPage would otherwise produce.
	useEffect(() => {
		if (!data || !following || isFetchingNextPage) return;
		if (events.length === 0) {
			if ((tailIndex ?? -1) >= 0) void refetch();
			return;
		}
		if (!hasNextPage) return;
		void fetchNextPage();
	}, [
		data,
		following,
		hasNextPage,
		isFetchingNextPage,
		fetchNextPage,
		events.length,
		tailIndex,
		refetch,
	]);

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
