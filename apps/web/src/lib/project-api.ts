import { infiniteQueryOptions, queryOptions } from "@tanstack/react-query";

import { apiClient } from "./api-client";
import type { SuccessEnvelope } from "./api-error";
import type { RoleSummary } from "./role-api";

// ── Shapes ────────────────────────────────────────────────────────────────────

export interface Project {
	id: string;
	name: string;
	description: string;
	task_id_prefix: string;
	settings: Record<string, unknown>;
	// Describes this project's Jev (AI decision API) setup without ever
	// exposing the API key itself — see ProjectJevConfig's doc comment.
	jev_configured: boolean;
	jev_base_url: string;
	jev_model: string;
	avatar_url?: string | null;
	avatar_thumb_url?: string | null;
	created_by?: string;
	created_at: string;
}

/** Mirrors dto.ProjectJevConfigResponse — returned by both GET /projects/:id
 *  (nested via Project's own jev_* fields) and PATCH .../jev-config. */
export interface ProjectJevConfig {
	configured: boolean;
	base_url: string;
	model: string;
}

export interface ProjectListResult {
	items: Project[];
	total: number;
	page: number;
	page_size: number;
}

export interface WorkspaceStats {
	open_task_count: number;
	team_member_count: number;
	ai_agent_count: number;
}

/** Daily history of the workspace counters, from GET
 *  /projects/workspace-trend. dates are UTC calendar dates, oldest first; each
 *  series holds the counter at the end of that day, so its last value is the
 *  live count. A series is empty when the server can't provide it. */
export interface WorkspaceTrend {
	days: number;
	dates: string[];
	projects: number[];
	open_tasks: number[];
	team_members: number[];
}

export interface ProjectMember {
	id: string;
	project_id: string;
	user_id: string;
	/** The roles the member holds inside the project, sorted by name. */
	roles: RoleSummary[];
	username: string;
	full_name: string;
	member_type?: string; // "human" | "agent" | "service_account"
	agent_id?: string;
	/** The member's account when member_type is "service_account". */
	service_account_id?: string;
	agent_name?: string;
	agent_handle?: string;
	avatar_url?: string | null;
	avatar_thumb_url?: string | null;
	// Only meaningful when member_type is "agent" — used to pick a default
	// provider-logo avatar when this member has no avatar_url of its own.
	agent_type?: string; // "llm" | "acp"
	agent_llm_provider?: string;
	agent_acp_provider?: string | null;
	agent_cli_provider?: string | null;
	// Only meaningful for a human member — shown to Jev (the AI decision
	// API) when deciding whether to assign this member a task in Auto mode.
	// An agent member's Jev-facing description comes from the Agent itself
	// (Agent.description) instead.
	description: string;
}

// ── Project CRUD ──────────────────────────────────────────────────────────────

export async function listProjects(
	page = 1,
	pageSize = 50,
): Promise<ProjectListResult> {
	const { data } = await apiClient.instance.get<
		SuccessEnvelope<ProjectListResult>
	>("/projects", { params: { page, page_size: pageSize } });
	return data.data;
}

export async function getWorkspaceStats(): Promise<WorkspaceStats> {
	const { data } = await apiClient.instance.get<
		SuccessEnvelope<WorkspaceStats>
	>("/projects/workspace-stats");
	return data.data;
}

export async function getWorkspaceTrend(days: number): Promise<WorkspaceTrend> {
	const { data } = await apiClient.instance.get<
		SuccessEnvelope<WorkspaceTrend>
	>("/projects/workspace-trend", { params: { days } });
	return data.data;
}

export async function getProject(projectId: string): Promise<Project> {
	const { data } = await apiClient.instance.get<SuccessEnvelope<Project>>(
		`/projects/${projectId}`,
	);
	return data.data;
}

export async function createProject(payload: {
	name: string;
	description?: string;
	task_id_prefix?: string;
}): Promise<Project> {
	const { data } = await apiClient.instance.post<SuccessEnvelope<Project>>(
		"/projects",
		payload,
	);
	return data.data;
}

export async function updateProject(
	projectId: string,
	payload: {
		name?: string;
		description?: string;
		task_id_prefix?: string;
		settings?: Record<string, unknown>;
	},
): Promise<Project> {
	const { data } = await apiClient.instance.patch<SuccessEnvelope<Project>>(
		`/projects/${projectId}`,
		payload,
	);
	return data.data;
}

export async function deleteProject(projectId: string): Promise<void> {
	await apiClient.instance.delete(`/projects/${projectId}`);
}

/** Sets this project's Jev credentials/host/model. Each field is
 *  independently optional (omit = leave unchanged); passing api_key: ""
 *  clears the key and disables Jev for this project. The API key itself is
 *  never returned — only the resulting jev_configured/jev_base_url/jev_model
 *  (see ProjectJevConfig). */
export async function updateProjectJevConfig(
	projectId: string,
	payload: { api_key?: string; base_url?: string; model?: string },
): Promise<ProjectJevConfig> {
	const { data } = await apiClient.instance.patch<
		SuccessEnvelope<ProjectJevConfig>
	>(`/projects/${projectId}/jev-config`, payload);
	return data.data;
}

/** Sends a throwaway question to this project's currently-*stored* Jev
 *  credentials (whatever was last saved via updateProjectJevConfig, not
 *  unsaved form input) and reports whether Jev answered successfully — lets
 *  a user verify their key/host/model actually work. Rejects (the caller's
 *  mutation onError) if Jev isn't configured or the call fails; the
 *  rejection's message is server-provided, human-readable detail. */
export async function testProjectJevConfig(
	projectId: string,
): Promise<{ success: boolean }> {
	const { data } = await apiClient.instance.post<
		SuccessEnvelope<{ success: boolean }>
	>(`/projects/${projectId}/jev-config/test`);
	return data.data;
}

// ── Members ───────────────────────────────────────────────────────────────────

export async function addProjectMember(
	projectId: string,
	payload:
		| { user_id: string; role_ids: string[]; description?: string }
		| { agent_id: string; role_ids: string[] }
		| { service_account_id: string; role_ids: string[] },
): Promise<ProjectMember> {
	const { data } = await apiClient.instance.post<
		SuccessEnvelope<ProjectMember>
	>(`/projects/${projectId}/members`, payload);
	return data.data;
}

export async function updateProjectMemberRole(
	projectId: string,
	memberId: string,
	payload: { description: string },
): Promise<ProjectMember> {
	const { data } = await apiClient.instance.patch<
		SuccessEnvelope<ProjectMember>
	>(`/projects/${projectId}/members/${memberId}`, payload);
	return data.data;
}

export async function removeProjectMember(
	projectId: string,
	memberId: string,
): Promise<void> {
	await apiClient.instance.delete(`/projects/${projectId}/members/${memberId}`);
}

export async function getMyProjectPermissions(
	projectId: string,
): Promise<string[]> {
	const { data } = await apiClient.instance.get<
		SuccessEnvelope<{ actions: string[] }>
	>(`/projects/${projectId}/members/me/permissions`);
	return data.data.actions;
}

// ── Task Types ────────────────────────────────────────────────────────────────

export interface TaskType {
	id: string;
	project_id: string;
	name: string;
	icon?: string | null;
	color?: string | null;
	description?: string | null;
	is_default?: boolean;
	is_system?: boolean;
	created_at: string;
	updated_at: string;
}

export async function listTaskTypes(projectId: string): Promise<TaskType[]> {
	const { data } = await apiClient.instance.get<
		SuccessEnvelope<{ items: TaskType[] }>
	>(`/projects/${projectId}/task-types`);
	return data.data.items;
}

export async function createTaskType(
	projectId: string,
	payload: {
		name: string;
		icon?: string | null;
		color?: string | null;
		description?: string | null;
	},
): Promise<TaskType> {
	const { data } = await apiClient.instance.post<SuccessEnvelope<TaskType>>(
		`/projects/${projectId}/task-types`,
		payload,
	);
	return data.data;
}

export async function updateTaskType(
	projectId: string,
	typeId: string,
	payload: {
		name?: string;
		icon?: string | null;
		color?: string | null;
		description?: string | null;
	},
): Promise<TaskType> {
	const { data } = await apiClient.instance.patch<SuccessEnvelope<TaskType>>(
		`/projects/${projectId}/task-types/${typeId}`,
		payload,
	);
	return data.data;
}

export async function deleteTaskType(
	projectId: string,
	typeId: string,
): Promise<void> {
	await apiClient.instance.delete(
		`/projects/${projectId}/task-types/${typeId}`,
	);
}

export async function setDefaultTaskType(
	projectId: string,
	typeId: string,
): Promise<TaskType> {
	const { data } = await apiClient.instance.put<SuccessEnvelope<TaskType>>(
		`/projects/${projectId}/task-types/${typeId}/set-default`,
	);
	return data.data;
}

// ── Task type role helpers ─────────────────────────────────────────────────────

/** Returns true if this task type is the system "Epic" type. */
export function isEpicType(t: TaskType | undefined | null): boolean {
	return !!t && !!t.is_system && t.name === "Epic";
}

/** Finds the Epic system type from a list of task types. */
export function findEpicType(types: TaskType[]): TaskType | undefined {
	return types.find(isEpicType);
}

/** Returns non-epic task types (Task, Bug, Story, etc). */
export function getNormalTaskTypes(types: TaskType[]): TaskType[] {
	return types.filter((t) => !isEpicType(t));
}

// ── Task Statuses ─────────────────────────────────────────────────────────────

export type StatusCategory =
	| "backlog"
	| "refinement"
	| "ready"
	| "todo"
	| "inprogress"
	| "done";

export const STATUS_CATEGORIES: StatusCategory[] = [
	"backlog",
	"refinement",
	"ready",
	"todo",
	"inprogress",
	"done",
];

export const STATUS_CATEGORY_LABELS: Record<StatusCategory, string> = {
	backlog: "Backlog",
	refinement: "Refinement",
	ready: "Ready",
	todo: "To Do",
	inprogress: "In Progress",
	done: "Done",
};

export interface TaskStatus {
	id: string;
	project_id: string;
	name: string;
	color?: string | null;
	position: number;
	category: StatusCategory;
	is_default?: boolean;
	created_at: string;
	updated_at: string;
}

export async function listTaskStatuses(
	projectId: string,
): Promise<TaskStatus[]> {
	const { data } = await apiClient.instance.get<
		SuccessEnvelope<{ items: TaskStatus[] }>
	>(`/projects/${projectId}/task-statuses`);
	return data.data.items;
}

export async function createTaskStatus(
	projectId: string,
	payload: {
		name: string;
		color?: string | null;
		position: number;
		category: StatusCategory;
	},
): Promise<TaskStatus> {
	const { data } = await apiClient.instance.post<SuccessEnvelope<TaskStatus>>(
		`/projects/${projectId}/task-statuses`,
		payload,
	);
	return data.data;
}

export async function updateTaskStatus(
	projectId: string,
	statusId: string,
	payload: {
		name?: string;
		color?: string | null;
		position?: number;
		category?: StatusCategory;
	},
): Promise<TaskStatus> {
	const { data } = await apiClient.instance.patch<SuccessEnvelope<TaskStatus>>(
		`/projects/${projectId}/task-statuses/${statusId}`,
		payload,
	);
	return data.data;
}

export async function deleteTaskStatus(
	projectId: string,
	statusId: string,
): Promise<void> {
	await apiClient.instance.delete(
		`/projects/${projectId}/task-statuses/${statusId}`,
	);
}

export async function setDefaultTaskStatus(
	projectId: string,
	statusId: string,
): Promise<TaskStatus> {
	const { data } = await apiClient.instance.put<SuccessEnvelope<TaskStatus>>(
		`/projects/${projectId}/task-statuses/${statusId}/set-default`,
	);
	return data.data;
}

/** Persists a new display order for task statuses in a single atomic request. */
export async function reorderTaskStatuses(
	projectId: string,
	orderedStatusIds: string[],
): Promise<void> {
	await apiClient.instance.put(
		`/projects/${projectId}/task-statuses/positions`,
		{
			status_ids: orderedStatusIds,
		},
	);
}

// ── Custom Field Definitions ─────────────────────────────────────────────────

export type FieldType =
	| "text"
	| "number"
	| "date"
	| "select"
	| "multi_select"
	| "boolean"
	| "url"
	| "long_text";

export interface CustomFieldOption {
	value: string;
	color?: string | null;
}

export interface CustomFieldDefinition {
	id: string;
	project_id: string;
	field_key: string;
	display_name: string;
	field_type: FieldType;
	options: CustomFieldOption[];
	is_required: boolean;
	use_in_search: boolean;
	/** Visible lines of a long_text field's editor. */
	rows: number;
	created_at: string;
	updated_at: string;
}

export async function listCustomFieldDefinitions(
	projectId: string,
): Promise<CustomFieldDefinition[]> {
	const { data } = await apiClient.instance.get<
		SuccessEnvelope<{ items: CustomFieldDefinition[] }>
	>(`/projects/${projectId}/custom-fields`);
	return data.data.items;
}

export async function getCustomFieldDefinition(
	projectId: string,
	fieldId: string,
): Promise<CustomFieldDefinition> {
	const { data } = await apiClient.instance.get<
		SuccessEnvelope<CustomFieldDefinition>
	>(`/projects/${projectId}/custom-fields/${fieldId}`);
	return data.data;
}

export async function createCustomFieldDefinition(
	projectId: string,
	payload: {
		display_name: string;
		field_key: string;
		field_type: FieldType;
		options?: CustomFieldOption[];
		is_required?: boolean;
		use_in_search?: boolean;
		rows?: number;
	},
): Promise<CustomFieldDefinition> {
	const { data } = await apiClient.instance.post<
		SuccessEnvelope<CustomFieldDefinition>
	>(`/projects/${projectId}/custom-fields`, payload);
	return data.data;
}

export async function updateCustomFieldDefinition(
	projectId: string,
	fieldId: string,
	payload: {
		display_name?: string;
		options?: CustomFieldOption[];
		is_required?: boolean;
		use_in_search?: boolean;
		rows?: number;
	},
): Promise<CustomFieldDefinition> {
	const { data } = await apiClient.instance.patch<
		SuccessEnvelope<CustomFieldDefinition>
	>(`/projects/${projectId}/custom-fields/${fieldId}`, payload);
	return data.data;
}

export async function deleteCustomFieldDefinition(
	projectId: string,
	fieldId: string,
): Promise<void> {
	await apiClient.instance.delete(
		`/projects/${projectId}/custom-fields/${fieldId}`,
	);
}

// ── Query Options ─────────────────────────────────────────────────────────────

export const projectsQueryOptions = (page = 1, pageSize = 50) =>
	queryOptions({
		queryKey: ["projects", { page, pageSize }],
		queryFn: () => listProjects(page, pageSize),
	});

export const PROJECTS_PAGE_SIZE = 50;

function fetchProjectsPage({ pageParam }: { pageParam: number }) {
	return listProjects(pageParam, PROJECTS_PAGE_SIZE);
}

function getNextProjectsPageParam(lastPage: ProjectListResult) {
	return lastPage.page * lastPage.page_size < lastPage.total
		? lastPage.page + 1
		: undefined;
}

/** Infinite-query version of the project list — backs surfaces that let the
 *  user browse every project a user can access, page by page, at their own
 *  pace (the home dashboard grid's "Load more" button, the sidebar project
 *  switcher's scroll), since ListProjects caps page_size at 100 and there's
 *  no server-side search to narrow the result set. Pages accumulate as the
 *  caller scrolls/loads more, same pattern as usersInfiniteQueryOptions.
 *
 *  Do NOT point a background/eager consumer at this one — every caller
 *  shares this exact cache entry (queryKey ["projects", "all"]), so eagerly
 *  draining it from one place empties the "Load more" affordance for every
 *  other place before the user ever sees it. Use
 *  projectsLookupInfiniteQueryOptions for that instead. */
export const projectsInfiniteQueryOptions = () =>
	infiniteQueryOptions({
		queryKey: ["projects", "all"],
		queryFn: fetchProjectsPage,
		initialPageParam: 1,
		getNextPageParam: getNextProjectsPageParam,
	});

/** Same paging as projectsInfiniteQueryOptions, under its own cache key —
 *  for a consumer that eagerly drains every page in the background (e.g. an
 *  id -> project lookup) without stealing pages out from under the
 *  user-paced "Load more" surfaces sharing ["projects", "all"] above. */
export const projectsLookupInfiniteQueryOptions = () =>
	infiniteQueryOptions({
		queryKey: ["projects", "all", "lookup"],
		queryFn: fetchProjectsPage,
		initialPageParam: 1,
		getNextPageParam: getNextProjectsPageParam,
	});

export const workspaceStatsQueryOptions = () =>
	queryOptions({
		queryKey: ["workspace-stats"],
		queryFn: () => getWorkspaceStats(),
		staleTime: 2 * 60 * 1000,
	});

export const WORKSPACE_TREND_DAYS = 30;

// The series only gain a point per day, so a long staleTime is fine.
export const workspaceTrendQueryOptions = (days = WORKSPACE_TREND_DAYS) =>
	queryOptions({
		queryKey: ["workspace-trend", days],
		queryFn: () => getWorkspaceTrend(days),
		staleTime: 10 * 60 * 1000,
	});

export const projectQueryOptions = (projectId: string) =>
	queryOptions({
		queryKey: ["projects", projectId],
		queryFn: () => getProject(projectId),
		staleTime: 2 * 60 * 1000,
	});

export type ProjectMemberType = "human" | "agent" | "service_account";

export interface ProjectMemberFilters {
	/** Matches username, name or agent handle (every word must appear). */
	search?: string;
	roleId?: string;
	memberType?: ProjectMemberType;
}

export interface ProjectMemberPage {
	items: ProjectMember[];
	page_size: number;
	next_cursor: string | null;
	/** Members matching the filters across all pages. */
	total_count: number;
}

export const PROJECT_MEMBERS_PAGE_SIZE = 20;
/** Most account ids the cursor endpoint accepts in one `ids` lookup. */
export const PROJECT_MEMBERS_MAX_IDS = 100;

/** One page of a project's members (GET /members), keyset-paginated by cursor and filtered
 *  server-side. `ids` (account ids, at most PROJECT_MEMBERS_MAX_IDS) narrows
 *  the result to those accounts — a batch lookup that replaces loading the
 *  whole team just to resolve a few ids. */
export async function listProjectMembers(
	projectId: string,
	opts: ProjectMemberFilters & {
		cursor?: string;
		pageSize?: number;
		ids?: string[];
	} = {},
): Promise<ProjectMemberPage> {
	const params: Record<string, string | number> = {
		page_size: opts.pageSize ?? PROJECT_MEMBERS_PAGE_SIZE,
	};
	if (opts.cursor) params.cursor = opts.cursor;
	if (opts.search?.trim()) params.search = opts.search.trim();
	if (opts.roleId) params.role_id = opts.roleId;
	if (opts.memberType) params.member_type = opts.memberType;
	if (opts.ids?.length) params.ids = opts.ids.join(",");
	const { data } = await apiClient.instance.get<
		SuccessEnvelope<ProjectMemberPage>
	>(`/projects/${projectId}/members`, { params });
	return data.data;
}

/** Infinite-query version of the member list. Keyed under the plain members
 *  key, so invalidating ["projects", id, "members"] refreshes it too. */
export const projectMembersInfiniteQueryOptions = (
	projectId: string,
	filters: ProjectMemberFilters = {},
) =>
	infiniteQueryOptions({
		queryKey: ["projects", projectId, "members", "cursor", filters],
		queryFn: ({ pageParam }: { pageParam: string | undefined }) =>
			listProjectMembers(projectId, { ...filters, cursor: pageParam }),
		initialPageParam: undefined as string | undefined,
		getNextPageParam: (lastPage) => lastPage.next_cursor ?? undefined,
	});

/** Size of the whole team (no filters), read from total_count of a
 *  one-row page. */
export const projectMemberTotalQueryOptions = (projectId: string) =>
	queryOptions({
		queryKey: ["projects", projectId, "members", "total"],
		queryFn: async () =>
			(await listProjectMembers(projectId, { pageSize: 1 })).total_count,
		staleTime: 15_000,
	});

/** Which of `ids` (account ids) are members of the project. */
export const existingMemberIdsQueryOptions = (
	projectId: string,
	ids: string[],
) =>
	queryOptions({
		queryKey: ["projects", projectId, "members", "existing", ids],
		queryFn: async () => {
			const page = await listProjectMembers(projectId, {
				ids,
				pageSize: PROJECT_MEMBERS_MAX_IDS,
			});
			return page.items.flatMap((m) =>
				[m.user_id, m.agent_id, m.service_account_id].filter(
					(id): id is string => !!id,
				),
			);
		},
		staleTime: 15_000,
	});

/** Key prefix of every cached members query for a project (paged lists,
 *  totals, id lookups) — invalidate it after members change. */
export const projectMembersKey = (projectId: string) =>
	["projects", projectId, "members"] as const;

export const myProjectPermissionsQueryOptions = (projectId: string) =>
	queryOptions({
		queryKey: ["projects", projectId, "members", "me", "permissions"],
		queryFn: () => getMyProjectPermissions(projectId),
		staleTime: 2 * 60 * 1000,
		retry: false,
	});

export const taskTypesQueryOptions = (projectId: string) =>
	queryOptions({
		queryKey: ["projects", projectId, "task-types"],
		queryFn: () => listTaskTypes(projectId),
	});

export const taskStatusesQueryOptions = (projectId: string) =>
	queryOptions({
		queryKey: ["projects", projectId, "task-statuses"],
		queryFn: () => listTaskStatuses(projectId),
	});

export const customFieldsQueryOptions = (projectId: string) =>
	queryOptions({
		queryKey: ["projects", projectId, "custom-fields"],
		queryFn: () => listCustomFieldDefinitions(projectId),
	});
