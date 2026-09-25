import { infiniteQueryOptions } from "@tanstack/react-query";
import { localDateExclusiveEndISO, localDateStartISO } from "./agent-api";
import { apiClient } from "./api-client";
import type { SuccessEnvelope } from "./api-error";

// ── Project activity log ─────────────────────────────────────────────────────
// Every change to every entity in a project, newest first — the one
// activities table the task/doc timelines and the agent tab also read.

export const ACTIVITY_ENTITY_TYPES = [
	"task",
	"doc",
	"sprint",
	"view",
	"automation",
	"environment",
	"member",
	"role",
	"agent",
	"project",
] as const;
export type ActivityEntityType = (typeof ACTIVITY_ENTITY_TYPES)[number];

export const ACTIVITY_ORIGINS = [
	"user",
	"agent",
	"automation",
	"jev",
	"annotation",
	"system",
] as const;
export type ActivityOrigin = (typeof ACTIVITY_ORIGINS)[number];

export interface ProjectActivity {
	id: string;
	entity_type: ActivityEntityType;
	entity_id?: string;
	/** The entity's current title; still set for a soft-deleted task/doc. */
	entity_title: string;
	/** True once the entity is gone — don't link to it. */
	entity_deleted: boolean;
	/** project_members.id; absent for system entries. */
	actor_id?: string;
	actor_name: string;
	actor_username: string;
	actor_avatar_url?: string;
	actor_avatar_thumb_url?: string;
	origin: ActivityOrigin;
	/** The event topic, e.g. "task.updated", or "comment". */
	activity_type: string;
	/** Shape depends on activity_type; see describeProjectActivity. */
	content: Record<string, unknown> | unknown[];
	created_at: string;
}

export interface ProjectActivityListResult {
	items: ProjectActivity[];
	page_size: number;
	next_cursor: string | null;
}

export interface ProjectActivityFilters {
	entityTypes?: ActivityEntityType[];
	/** project_members.id values. */
	actorIds?: string[];
	origins?: ActivityOrigin[];
	/** Local calendar dates ("YYYY-MM-DD"), inclusive. */
	createdAfter?: string;
	createdBefore?: string;
	search?: string;
}

export const PROJECT_ACTIVITIES_PAGE_SIZE = 50;

function buildParams(
	filters: ProjectActivityFilters,
	cursor: string | undefined,
): Record<string, string | number> {
	const params: Record<string, string | number> = {
		page_size: PROJECT_ACTIVITIES_PAGE_SIZE,
	};
	if (cursor) params.cursor = cursor;
	if (filters.entityTypes?.length)
		params.entity_type = filters.entityTypes.join(",");
	if (filters.actorIds?.length) params.actor_id = filters.actorIds.join(",");
	if (filters.origins?.length) params.origin = filters.origins.join(",");
	if (filters.createdAfter)
		params.created_after = localDateStartISO(filters.createdAfter);
	if (filters.createdBefore)
		params.created_before = localDateExclusiveEndISO(filters.createdBefore);
	if (filters.search?.trim()) params.search = filters.search.trim();
	return params;
}

export async function listProjectActivities(
	projectId: string,
	filters: ProjectActivityFilters = {},
	cursor?: string,
): Promise<ProjectActivityListResult> {
	const { data } = await apiClient.instance.get<
		SuccessEnvelope<ProjectActivityListResult>
	>(`/projects/${projectId}/activities`, {
		params: buildParams(filters, cursor),
	});
	return data.data;
}

export const projectActivitiesQueryOptions = (
	projectId: string,
	filters: ProjectActivityFilters = {},
) =>
	infiniteQueryOptions({
		queryKey: ["projects", projectId, "projectActivities", filters],
		queryFn: ({ pageParam }: { pageParam: string | undefined }) =>
			listProjectActivities(projectId, filters, pageParam),
		initialPageParam: undefined as string | undefined,
		getNextPageParam: (lastPage) => lastPage.next_cursor ?? undefined,
	});
