import { queryOptions } from "@tanstack/react-query";

import { apiClient } from "./api-client";
import type { SuccessEnvelope } from "./api-error";

/** A project matched by the workspace search. */
export interface SearchProject {
	id: string;
	name: string;
	description: string;
	task_id_prefix: string;
	avatar_thumb_url?: string;
}

/** A task matched by the workspace search. `task_key` is "PRJ-12", or "#12"
 *  when the project has no task-key prefix. */
export interface SearchTask {
	id: string;
	project_id: string;
	project_name: string;
	task_key: string;
	task_number: number;
	title: string;
	status_name?: string;
	status_category?: string;
	/** The task's type (Epic, Story, Bug, ...) in the project's icon and color. */
	type_name?: string;
	type_icon?: string;
	type_color?: string;
}

/** A person matched by the workspace search. `project_id` is one of their
 *  projects whose team page the caller may open, when there is one. */
export interface SearchPerson {
	id: string;
	name: string;
	username: string;
	avatar_thumb_url?: string;
	project_id?: string;
}

export interface SearchResults {
	query: string;
	projects: SearchProject[];
	tasks: SearchTask[];
	people: SearchPerson[];
}

/** Results per kind; the API allows up to 20. */
export const SEARCH_LIMIT = 6;

/** Searches projects, tasks and people across the workspace. The API only
 *  returns what the caller may see: their readable projects, the tasks they
 *  may read in each, and the members of their teams (every user with
 *  users:write). */
export async function searchWorkspace(
	q: string,
	limit = SEARCH_LIMIT,
): Promise<SearchResults> {
	const { data } = await apiClient.instance.get<SuccessEnvelope<SearchResults>>(
		"/search",
		{ params: { q, limit } },
	);
	return data.data;
}

export const searchQueryOptions = (q: string) =>
	queryOptions({
		queryKey: ["search", q],
		queryFn: () => searchWorkspace(q),
		enabled: q.trim().length > 0,
		staleTime: 30_000,
	});
