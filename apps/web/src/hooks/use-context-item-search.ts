import { useInfiniteQuery, useQuery } from "@tanstack/react-query";
import { useEffect, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { conversationTriggerLabel } from "@/components/projects/agents/conversations-layout";
import {
	type AgentConversation,
	agentsQueryOptions,
	chattableAgentsQueryOptions,
	listConversations,
	listGlobalConversations,
} from "@/lib/agent-api";
import { listAutomationsPage } from "@/lib/automation-api";
import type { ContextItemType } from "@/lib/context-items";
import { listDocumentsPage } from "@/lib/doc-api";
import { searchTasksForPicker } from "@/lib/interaction-api";
import type { LoadMorePagination } from "@/lib/scroll-pagination";
import { useDebouncedCallback } from "./use-debounced-callback";

const SEARCH_DEBOUNCE_MS = 300;

// Below this length, `search` does a leading-wildcard ILIKE scan server-side
// (no trigram index backs it) — cheap enough per-project at 2+ chars, but a
// single character is broad enough to be worth avoiding. Mirrors
// use-task-picker-search.ts's own MIN_SEARCH_LENGTH. Doesn't apply to an
// *empty* query — that's "browse everything," not "search," and every
// backend list endpoint already treats a missing/blank search param as
// "no filter" for exactly this reason.
export const MIN_SEARCH_LENGTH = 2;

const PAGE_SIZE = 20;

/** One search result, normalized across all 4 context-item types so the
 *  popover's result list can render them uniformly. */
export interface ContextSearchResult {
	id: string;
	title: string;
	subtitle?: string;
}

// "conversation" pages carry raw AgentConversation[] instead of an
// already-normalized ContextSearchResult[] — see the `results` useMemo below
// for why the agent-name/trigger-label synthesis has to happen there instead
// of inside queryFn.
interface RawPage {
	items: readonly (ContextSearchResult | AgentConversation)[];
	nextCursor: string | undefined;
}

interface UseContextItemSearchResult {
	search: string;
	setSearch: (value: string) => void;
	/** True only for a 1-character (or otherwise sub-minimum) non-empty
	 *  query — too short to search, but not empty either. `results` is
	 *  always empty while this holds; callers should show a "type more"
	 *  hint instead. An empty query is *not* "too short" — see
	 *  MIN_SEARCH_LENGTH's doc comment. */
	queryTooShort: boolean;
	/** The current query's results — either everything (browsing, query
	 *  empty) or a filtered page (query at least MIN_SEARCH_LENGTH chars).
	 *  Always empty while queryTooShort holds. */
	results: ContextSearchResult[];
	/** True while a debounce or in-flight initial fetch means `results` may
	 *  be stale/incomplete. */
	isLoading: boolean;
	pagination: LoadMorePagination;
}

/**
 * Backs the context-injection popover's search box (see
 * components/assistant-ui/context-injection.tsx), dispatching per
 * ContextItemType to whichever backend search already exists for that type:
 *   - task: the same searchTasksForPicker use-task-picker-search.ts uses.
 *   - doc / automation: listDocumentsPage/listAutomationsPage — cursor-
 *     paginated, same as task/conversation.
 *   - conversation: listConversations (project-scoped) or
 *     listGlobalConversations (no projectId — global chat).
 *
 * All four already treat an empty/omitted `search` param as "no filter," so
 * leaving the query box empty browses the full (paginated) list rather than
 * showing nothing — only a *non-empty* query shorter than MIN_SEARCH_LENGTH
 * is rejected client-side (queryTooShort), to avoid firing an expensive
 * leading-wildcard scan per keystroke on 1 character.
 *
 * Conversations have no title field, so their label is synthesized from the
 * triggering agent's name + conversationTriggerLabel (shared with
 * conversations-layout.tsx's ConversationListItem) + created_at date —
 * recomputed in a useMemo below (not inside the query itself) so it always
 * reflects the latest agents list/translations even if those resolve after
 * the conversation page itself has already loaded.
 */
export function useContextItemSearch(
	type: ContextItemType,
	projectId: string | undefined,
	enabled: boolean,
): UseContextItemSearchResult {
	const { t } = useTranslation("projects");
	const [search, setSearch] = useState("");
	const [debouncedSearch, setDebouncedSearch] = useState("");
	const applyDebounced = useDebouncedCallback(
		setDebouncedSearch,
		SEARCH_DEBOUNCE_MS,
	);

	const updateSearch = (value: string) => {
		setSearch(value);
		applyDebounced(value.trim());
	};

	useEffect(() => {
		if (!enabled) {
			setSearch("");
			setDebouncedSearch("");
		}
	}, [enabled]);

	const queryTooShort =
		search.trim().length > 0 && search.trim().length < MIN_SEARCH_LENGTH;
	const debouncedTooShort =
		debouncedSearch.length > 0 && debouncedSearch.length < MIN_SEARCH_LENGTH;
	// Fires for an empty query (browse everything) or one long enough to
	// search — never for the 1-character in-between state.
	const queryEnabled = enabled && !debouncedTooShort;

	// Only fetched for the "conversation" tab — used to resolve each
	// conversation's agent_id into a display name. Cheap/deduped by
	// react-query when already fetched elsewhere (e.g. the agent picker).
	const agentsBaseOptions = projectId
		? agentsQueryOptions(projectId)
		: chattableAgentsQueryOptions;
	const { data: agents = [] } = useQuery({
		...agentsBaseOptions,
		enabled: queryEnabled && type === "conversation",
	});
	const agentsById = useMemo(
		() => new Map(agents.map((a) => [a.id, a])),
		[agents],
	);

	const { data, isFetching, isFetchingNextPage, hasNextPage, fetchNextPage } =
		useInfiniteQuery({
			queryKey: ["context-item-search", type, projectId, debouncedSearch],
			queryFn: async ({
				pageParam,
			}: {
				pageParam: string | undefined;
			}): Promise<RawPage> => {
				switch (type) {
					case "task": {
						if (!projectId) return { items: [], nextCursor: undefined };
						const page = await searchTasksForPicker(
							projectId,
							debouncedSearch,
							{
								cursor: pageParam,
							},
						);
						return {
							items: page.items.map((task) => ({
								id: task.id,
								title: task.title,
							})),
							nextCursor: page.next_cursor ?? undefined,
						};
					}
					case "doc": {
						if (!projectId) return { items: [], nextCursor: undefined };
						const page = await listDocumentsPage(projectId, {
							search: debouncedSearch,
							cursor: pageParam,
							pageSize: PAGE_SIZE,
						});
						return {
							items: page.items.map((doc) => ({
								id: doc.id,
								title: doc.title,
							})),
							nextCursor: page.next_cursor ?? undefined,
						};
					}
					case "automation": {
						if (!projectId) return { items: [], nextCursor: undefined };
						const page = await listAutomationsPage(projectId, {
							search: debouncedSearch,
							cursor: pageParam,
							pageSize: PAGE_SIZE,
						});
						return {
							items: page.items.map((automation) => ({
								id: automation.id,
								title: automation.name,
							})),
							nextCursor: page.next_cursor ?? undefined,
						};
					}
					// "annotation" has no case here deliberately -- there's no
					// searchable picker tab for comments (see
					// context-injection.tsx's availableTypes), only
					// paste-to-attach. Falls through to the default below.
					case "conversation": {
						const page = projectId
							? await listConversations(projectId, {
									search: debouncedSearch,
									cursor: pageParam,
								})
							: await listGlobalConversations({
									search: debouncedSearch,
									cursor: pageParam,
								});
						// Kept as raw AgentConversation[] here — labeling needs
						// `t` and the agents-by-id map, which aren't available
						// inside queryFn; see the `results` useMemo below.
						return {
							items: page.items,
							nextCursor: page.next_cursor ?? undefined,
						};
					}
					default:
						return { items: [], nextCursor: undefined };
				}
			},
			initialPageParam: undefined as string | undefined,
			getNextPageParam: (lastPage) => lastPage.nextCursor,
			enabled: queryEnabled,
			staleTime: 15_000,
		});

	const results = useMemo((): ContextSearchResult[] => {
		if (!queryEnabled) return [];
		const rawItems = data?.pages.flatMap((page) => page.items) ?? [];
		if (type !== "conversation") return rawItems as ContextSearchResult[];
		return (rawItems as AgentConversation[]).map((conv) => {
			const agentName = agentsById.get(conv.agent_id)?.name;
			return {
				id: conv.id,
				title: agentName ?? conv.agent_id.slice(0, 8),
				subtitle: `${conversationTriggerLabel(conv, t)} · ${new Date(
					conv.created_at,
				).toLocaleDateString()}`,
			};
		});
	}, [data, queryEnabled, type, agentsById, t]);

	const isInitialFetch = isFetching && !isFetchingNextPage;
	const debouncePending = search.trim() !== debouncedSearch;

	return {
		search,
		setSearch: updateSearch,
		queryTooShort,
		results,
		isLoading: queryEnabled && (debouncePending || isInitialFetch),
		pagination: {
			hasMore: queryEnabled && !!hasNextPage,
			isLoadingMore: isFetchingNextPage,
			onLoadMore: () => void fetchNextPage(),
		},
	};
}
