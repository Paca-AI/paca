import type { UIEvent } from "react";

/** Load-more state/handler for a list backed by a paginated `useInfiniteQuery`.
 *  Generic across every "scroll near the bottom to load more" list in the
 *  app (epic pickers, the team-member picker, etc.) so each caller doesn't
 *  reimplement the same threshold check. */
export interface LoadMorePagination {
	hasMore: boolean;
	isLoadingMore: boolean;
	onLoadMore: () => void;
}

const LOAD_MORE_SCROLL_THRESHOLD_PX = 48;

/** Builds an onScroll handler that fires `pagination.onLoadMore()` once the
 *  list is scrolled within LOAD_MORE_SCROLL_THRESHOLD_PX of its bottom,
 *  instead of requiring an explicit "Load more" click. */
export function createLoadMoreScrollHandler(
	pagination: LoadMorePagination | undefined,
): (e: UIEvent<HTMLDivElement>) => void {
	return (e) => {
		if (!pagination?.hasMore || pagination.isLoadingMore) return;
		const el = e.currentTarget;
		if (
			el.scrollHeight - el.scrollTop - el.clientHeight <
			LOAD_MORE_SCROLL_THRESHOLD_PX
		) {
			pagination.onLoadMore();
		}
	};
}
