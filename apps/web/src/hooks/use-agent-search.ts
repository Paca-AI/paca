import { keepPreviousData, useInfiniteQuery } from "@tanstack/react-query";
import { useEffect, useMemo, useState } from "react";

import { useDebouncedCallback } from "@/hooks/use-debounced-callback";
import {
	type AgentScope,
	agentsInfiniteQueryOptions,
	chattableAgentsInfiniteQueryOptions,
} from "@/lib/agent-api";
import { getAgentDirectory } from "@/lib/agent-directory";

/** Server-side searchable, cursor-paged agent list for pickers: a project's
 *  agents when projectId is given, otherwise the global agents anyone may chat
 *  with. Nothing is fetched until `enabled`; typing re-queries the server after
 *  a short pause while the pages already loaded stay on screen. Loaded agents
 *  also seed the id → agent directory. */
export function useAgentSearch(
	projectId: string | null | undefined,
	enabled: boolean,
	opts: { scope?: AgentScope } = {},
) {
	const [query, setQuery] = useState("");
	const [debouncedQuery, setDebouncedQuery] = useState("");
	const applyQuery = useDebouncedCallback(setDebouncedQuery, 250);

	const projectQuery = useInfiniteQuery({
		...agentsInfiniteQueryOptions(projectId ?? "", {
			scope: opts.scope,
			search: debouncedQuery,
		}),
		enabled: enabled && !!projectId,
		placeholderData: keepPreviousData,
	});
	const globalQuery = useInfiniteQuery({
		...chattableAgentsInfiniteQueryOptions(debouncedQuery),
		enabled: enabled && !projectId,
		placeholderData: keepPreviousData,
	});
	const active = projectId ? projectQuery : globalQuery;

	// Every caller picks an agent to run work (chat, filter its conversations, write
	// with AI). A meeting assistant joins calls instead and cannot run any of that.
	const agents = useMemo(
		() =>
			(active.data?.pages.flatMap((p) => p.items) ?? []).filter(
				(a) => a.agent_type !== "meeting",
			),
		[active.data],
	);
	useEffect(() => {
		if (agents.length > 0) getAgentDirectory(projectId ?? null).seed(agents);
	}, [projectId, agents]);

	return {
		query,
		onQueryChange: (value: string) => {
			setQuery(value);
			applyQuery(value);
		},
		agents,
		isLoading: active.isLoading,
		hasMore: !!active.hasNextPage,
		isLoadingMore: active.isFetchingNextPage,
		isError: active.isFetchNextPageError,
		onLoadMore: () => void active.fetchNextPage(),
	};
}
