import { useInfiniteQuery } from "@tanstack/react-query";
import { useEffect, useState } from "react";
import { useDebouncedCallback } from "@/hooks/use-debounced-callback";
import { searchEpicTasks, type Task } from "@/lib/interaction-api";
import type { EpicsPagination } from "./view-utils";

const SEARCH_DEBOUNCE_MS = 300;

// Below this length, `search` does a leading-wildcard ILIKE scan server-side
// (no trigram index backs it) — cheap enough per-project at 2+ chars, but a
// single character is broad enough to be worth avoiding.
const MIN_SEARCH_LENGTH = 2;

interface UseEpicSearchResult {
	search: string;
	setSearch: (value: string) => void;
	/** True once the user has typed a query of at least MIN_SEARCH_LENGTH —
	 *  callers should swap in `results` for their normal paginated epic list
	 *  while this holds. */
	isSearching: boolean;
	/** Server-searched matches; only meaningful while isSearching is true. */
	results: Task[];
	/** True while a debounce or in-flight initial fetch means `results` may
	 *  be stale/incomplete — callers should show a full-list loading state. */
	isLoading: boolean;
	/** Load-more state for the search results, in the same shape as the
	 *  picker's normal (non-search) epicsPagination prop, so callers can
	 *  swap between the two without branching on structure. */
	pagination: EpicsPagination;
}

/** Backs an epic picker's search input with a debounced, cursor-paginated
 *  server-side search instead of filtering whatever page of epics the
 *  picker happens to have preloaded — a project can (and in practice does)
 *  have far more epics than are paginated into memory at once, so
 *  client-side filtering would silently miss most of them. `enabled` should
 *  track the picker's open state so the query only fires while it's visible
 *  and search state resets the moment it closes. */
export function useEpicSearch(
	projectId: string | undefined,
	epicTypeId: string | undefined,
	enabled: boolean,
): UseEpicSearchResult {
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

	const isSearching = search.trim().length >= MIN_SEARCH_LENGTH;

	const { data, isFetching, isFetchingNextPage, hasNextPage, fetchNextPage } =
		useInfiniteQuery({
			queryKey: [
				"projects",
				projectId,
				"tasks",
				"epics",
				epicTypeId,
				"search",
				debouncedSearch,
			],
			queryFn: ({ pageParam }: { pageParam: string | undefined }) =>
				searchEpicTasks(projectId ?? "", epicTypeId ?? "", debouncedSearch, {
					cursor: pageParam,
				}),
			initialPageParam: undefined as string | undefined,
			getNextPageParam: (lastPage) => lastPage.next_cursor ?? undefined,
			enabled:
				enabled &&
				!!projectId &&
				!!epicTypeId &&
				debouncedSearch.length >= MIN_SEARCH_LENGTH,
			staleTime: 15_000,
		});

	// Debounce is still pending (input ahead of what's been queried) or the
	// initial page for this query text hasn't landed yet — as opposed to
	// isFetchingNextPage, which covers subsequent pages and shouldn't block
	// the whole list.
	const isInitialFetch = isFetching && !isFetchingNextPage;
	const debouncePending = search.trim() !== debouncedSearch;

	return {
		search,
		setSearch: updateSearch,
		isSearching,
		results: isSearching
			? (data?.pages.flatMap((page) => page.items) ?? [])
			: [],
		isLoading: isSearching && (debouncePending || isInitialFetch),
		pagination: {
			hasMore: isSearching && !!hasNextPage,
			isLoadingMore: isFetchingNextPage,
			onLoadMore: () => void fetchNextPage(),
		},
	};
}
