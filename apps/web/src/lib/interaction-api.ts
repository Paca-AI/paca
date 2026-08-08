import { infiniteQueryOptions, queryOptions } from "@tanstack/react-query";

import { apiClient } from "./api-client";
import type { SuccessEnvelope } from "./api-error";

// ── Shapes ────────────────────────────────────────────────────────────────────

export type SprintStatus = "planned" | "active" | "completed";

export interface Sprint {
	id: string;
	project_id: string;
	name: string;
	start_date?: string | null;
	end_date?: string | null;
	goal?: string | null;
	status: SprintStatus;
	created_at: string;
	updated_at: string;
}

export interface SprintListResult {
	items: Sprint[];
}

export interface Task {
	id: string;
	project_id: string;
	title: string;
	task_number: number;
	task_type_id?: string | null;
	status_id?: string | null;
	sprint_id?: string | null;
	parent_task_id?: string | null;
	description?: unknown[] | null;
	importance: number;
	story_points?: number | null;
	assignee_ids?: string[];
	reporter_id?: string | null;
	custom_fields: Record<string, unknown>;
	start_date?: string | null;
	due_date?: string | null;
	tags?: string[];
	view_position?: number | null;
	view_group_key?: string | null;
	created_at: string;
	updated_at: string;
}

export interface TaskListResult {
	items: Task[];
	page_size: number;
	next_cursor?: string | null;
	total_count?: number;
	field_sum?: number | null;
}

export interface TaskPosition {
	view_id: string;
	task_id: string;
	position: number;
	group_key?: string | null;
}

interface TaskPositionListResult {
	items: TaskPosition[];
}

// ── View types ─────────────────────────────────────────────────────────────────
export type ViewType = "table" | "board" | "roadmap" | "plugin";

/**
 * A single entry in a FilterConfig's `items` map.
 * - `true`  → include this item / group
 * - `false` → exclude this item / group
 * - `FilterConfig` → recursively apply a sub-selector (used for named groups
 *   such as `"normal"` in task-type filters)
 */
export type FilterEntry = boolean | FilterConfig;

/**
 * A generic, recursive filter selector stored per view dimension.
 *
 * - `all: true`  → include everything by default; `items` act as exclusion overrides.
 * - `all: false` → include nothing by default; `items` act as inclusions.
 *
 * Item keys are either entity IDs (UUIDs) or named virtual groups.
 * The `"normal"` key in a task-types filter expands client-side to all
 * non-system type IDs, enabling dynamic inclusion without hard-coded snapshots.
 */
export interface FilterConfig {
	all: boolean;
	items?: Record<string, FilterEntry>;
}

/**
 * A per-custom-field filter selector stored inside a view's filters, keyed by
 * the target custom field's `field_key`. Which members are meaningful depends
 * on the field's type (looked up from its `CustomFieldDefinition` at resolve
 * time, not stored here):
 * - `select` / `multi_select` / `boolean` → `selector` (matches ANY of the
 *   resolved option values, same recursive semantics as `FilterConfig`)
 * - `number` → `min` / `max` (inclusive range; either may be unset)
 * - `date` → `after` / `before` (inclusive range, "YYYY-MM-DD"; either may be unset)
 * - `text` / `url` → `contains` (case-insensitive substring match)
 */
export interface CustomFieldFilterConfig {
	selector?: FilterConfig;
	min?: number;
	max?: number;
	after?: string;
	before?: string;
	contains?: string;
}

/** An inclusive date range filter, "YYYY-MM-DD"; either bound may be unset. */
export interface DateRangeFilter {
	after?: string;
	before?: string;
}

/** An inclusive numeric range filter; either bound may be unset. */
export interface NumberRangeFilter {
	min?: number;
	max?: number;
}

/**
 * Per-view filter configuration stored in the database.
 * Each dimension uses an optional FilterConfig selector; absent means no filter
 * (include everything) for that dimension.
 */
export interface ViewFilters {
	task_types?: FilterConfig;
	statuses?: FilterConfig;
	assignees?: FilterConfig;
	sprints?: FilterConfig;
	custom_fields?: Record<string, CustomFieldFilterConfig>;
	start_date?: DateRangeFilter;
	due_date?: DateRangeFilter;
	story_points?: NumberRangeFilter;
	/**
	 * Selected priority bucket indices (0=None, 1=Low, 2=Medium, 3=High,
	 * 4=Critical — see priority.ts). Resolved into concrete importance ranges
	 * client-side (via getImportanceBucketBounds) before querying, so
	 * non-contiguous selections like "Low or Critical" work correctly.
	 */
	importance?: number[];
	/** Matches tasks that have ANY of these tag values. */
	tags?: string[];
}

/**
 * The resolved (query-ready) shape of a single custom field filter, sent to
 * the API as part of `custom_field_filters`. Unlike `CustomFieldFilterConfig`,
 * `values` is already an explicit list — any `selector` FilterConfig has been
 * resolved client-side against the field's option values.
 */
export interface CustomFieldFilterQuery {
	values?: string[];
	min?: number;
	max?: number;
	after?: string;
	before?: string;
	contains?: string;
}

export interface ViewConfig {
	fields?: string[];
	column_by?: string;
	swimlanes?: string;
	sort_by?: string;
	field_sum?: string;
	slice_by?: string;
	filters?: ViewFilters;
	collapsed_columns?: string[];
	/** Saved number of tasks to fetch when paginating further ("load more") within this view. Unset falls back to a fixed default. */
	page_size?: number;
	/** Saved number of tasks to fetch on the first load of this view, independent of page_size. Unset falls back to the layout's default. */
	initial_page_size?: number;
	/** Populated only for plugin views (view_type = "plugin") */
	plugin_manifest_id?: string;
	plugin_component?: string;
}

export type ViewLayout = "Board" | "Table" | "Roadmap" | "Plugin";

// ── Filter resolver helpers ───────────────────────────────────────────────────

/**
 * Resolves a generic FilterConfig to an array of IDs from `allIds`.
 * Does not handle virtual group keys — use domain-specific resolvers for those.
 */
export function resolveFilterConfig(
	config: FilterConfig,
	allIds: string[],
): string[] {
	const included = new Set<string>(config.all ? allIds : []);
	for (const [key, entry] of Object.entries(config.items ?? {})) {
		const include = entry === true || (typeof entry === "object" && entry.all);
		if (include) included.add(key);
		else included.delete(key);
	}
	return Array.from(included);
}

/**
 * Resolves a task-types FilterConfig to an explicit list of task type IDs.
 *
 * The virtual key `"normal"` expands to all non-system type IDs so newly
 * created task types are automatically included without updating stored views.
 */
export function resolveTaskTypeFilter(
	config: FilterConfig,
	taskTypes: { id: string; is_system?: boolean }[],
): string[] {
	const normalTypeIds = taskTypes.filter((t) => !t.is_system).map((t) => t.id);
	const allIds = taskTypes.map((t) => t.id);

	const included = new Set<string>(config.all ? allIds : []);

	for (const [key, entry] of Object.entries(config.items ?? {})) {
		if (key === "normal") {
			if (entry === true) {
				for (const id of normalTypeIds) included.add(id);
			} else if (entry === false) {
				for (const id of normalTypeIds) included.delete(id);
			} else {
				// Nested FilterConfig: resolve within the normal-type subset
				const subIds = resolveFilterConfig(entry, normalTypeIds);
				if (config.all) {
					// Replace the normal-type portion with the sub-config result
					for (const id of normalTypeIds) included.delete(id);
				}
				for (const id of subIds) included.add(id);
			}
		} else {
			// Direct UUID key
			const include =
				entry === true || (typeof entry === "object" && entry.all);
			if (include) included.add(key);
			else included.delete(key);
		}
	}

	return Array.from(included);
}

export interface InteractionView {
	id: string;
	name: string;
	view_type: ViewType;
	layout: ViewLayout;
	config?: ViewConfig;
	position: number;
}

// ── View shape helpers ─────────────────────────────────────────────────────────
function viewTypeToLayout(vt: ViewType): ViewLayout {
	if (vt === "board") return "Board";
	if (vt === "roadmap") return "Roadmap";
	if (vt === "plugin") return "Plugin";
	return "Table";
}

export function layoutToViewType(l: ViewLayout): ViewType {
	if (l === "Board") return "board";
	if (l === "Roadmap") return "roadmap";
	if (l === "Plugin") return "plugin";
	return "table";
}

function mapView(raw: Omit<InteractionView, "layout">): InteractionView {
	return { ...raw, layout: viewTypeToLayout(raw.view_type) };
}

// ── View API ──────────────────────────────────────────────────────────────────
interface ViewListResult {
	items: Omit<InteractionView, "layout">[];
}

// ── Unified context-based View API ───────────────────────────────────────────

export type ViewsContext = "sprint" | "backlog" | "timeline";

function viewsContextQuery(
	context: ViewsContext,
	sprintId?: string | null,
): string {
	if (context === "sprint" && sprintId)
		return `context=sprint&sprint_id=${sprintId}`;
	return `context=${context}`;
}

export async function listViewsByContext(
	projectId: string,
	context: ViewsContext,
	sprintId?: string | null,
): Promise<InteractionView[]> {
	const { data } = await apiClient.instance.get<
		SuccessEnvelope<ViewListResult>
	>(`/projects/${projectId}/views?${viewsContextQuery(context, sprintId)}`);
	return data.data.items.map(mapView);
}

export async function createViewByContext(
	projectId: string,
	context: ViewsContext,
	payload: { name: string; view_type: ViewType; config?: ViewConfig },
	sprintId?: string | null,
): Promise<InteractionView> {
	const { data } = await apiClient.instance.post<
		SuccessEnvelope<Omit<InteractionView, "layout">>
	>(
		`/projects/${projectId}/views?${viewsContextQuery(context, sprintId)}`,
		payload,
	);
	return mapView(data.data);
}

export async function updateViewById(
	projectId: string,
	viewId: string,
	payload: Partial<{
		name: string;
		view_type: ViewType;
		config: ViewConfig;
		position: number;
	}>,
): Promise<InteractionView> {
	const { data } = await apiClient.instance.patch<
		SuccessEnvelope<Omit<InteractionView, "layout">>
	>(`/projects/${projectId}/views/${viewId}`, payload);
	return mapView(data.data);
}

export async function deleteViewById(
	projectId: string,
	viewId: string,
): Promise<void> {
	await apiClient.instance.delete(`/projects/${projectId}/views/${viewId}`);
}

export async function reorderViewsByContext(
	projectId: string,
	context: ViewsContext,
	viewIds: string[],
	sprintId?: string | null,
): Promise<void> {
	await apiClient.instance.put(
		`/projects/${projectId}/views/positions?${viewsContextQuery(context, sprintId)}`,
		{ view_ids: viewIds },
	);
}

export async function bulkMoveViewTaskPositions(
	projectId: string,
	viewId: string,
	items: Array<{
		task_id: string;
		position: number;
		group_key?: string | null;
	}>,
): Promise<void> {
	await apiClient.instance.put(
		`/projects/${projectId}/views/${viewId}/task-positions`,
		{ items },
	);
}

export const viewsByContextQueryOptions = (
	projectId: string,
	context: ViewsContext,
	sprintId?: string | null,
) => {
	const queryKey = [
		"projects",
		projectId,
		"views",
		{ context, sprintId: sprintId ?? undefined },
	] as const;
	return queryOptions({
		queryKey,
		queryFn: () => listViewsByContext(projectId, context, sprintId),
		staleTime: 30_000,
	});
};

// ── Sprint API ────────────────────────────────────────────────────────────────

export async function listSprints(projectId: string): Promise<Sprint[]> {
	const { data } = await apiClient.instance.get<
		SuccessEnvelope<SprintListResult>
	>(`/projects/${projectId}/sprints`);
	return data.data.items;
}

export async function getSprint(
	projectId: string,
	sprintId: string,
): Promise<Sprint> {
	const { data } = await apiClient.instance.get<SuccessEnvelope<Sprint>>(
		`/projects/${projectId}/sprints/${sprintId}`,
	);
	return data.data;
}

export interface CreateSprintPayload {
	name: string;
	status?: SprintStatus;
	goal?: string | null;
	start_date?: string | null;
	end_date?: string | null;
}

export async function createSprint(
	projectId: string,
	payload: CreateSprintPayload,
): Promise<Sprint> {
	const { data } = await apiClient.instance.post<SuccessEnvelope<Sprint>>(
		`/projects/${projectId}/sprints`,
		payload,
	);
	return data.data;
}

export interface UpdateSprintPayload {
	name?: string;
	status?: SprintStatus;
	goal?: string | null;
	start_date?: string | null;
	end_date?: string | null;
}

export async function updateSprint(
	projectId: string,
	sprintId: string,
	payload: UpdateSprintPayload,
): Promise<Sprint> {
	const { data } = await apiClient.instance.patch<SuccessEnvelope<Sprint>>(
		`/projects/${projectId}/sprints/${sprintId}`,
		payload,
	);
	return data.data;
}

export interface CompleteSprintPayload {
	move_to_sprint_id?: string | null;
}

export async function completeSprint(
	projectId: string,
	sprintId: string,
	payload: CompleteSprintPayload = {},
): Promise<Sprint> {
	const { data } = await apiClient.instance.post<SuccessEnvelope<Sprint>>(
		`/projects/${projectId}/sprints/${sprintId}/complete`,
		payload,
	);
	return data.data;
}

// ── Task API ──────────────────────────────────────────────────────────────────

export interface ListTasksOptions {
	sprintId?: string | null;
	sprintIds?: string[];
	statusId?: string;
	statusIds?: string[];
	assigneeId?: string;
	assigneeIds?: string[];
	assigneeNull?: boolean;
	taskTypeIds?: string[];
	taskTypeNull?: boolean;
	parentTaskId?: string;
	pageSize?: number;
	cursor?: string;
	/** When set to a field key (e.g. "story_points" or a custom field key), the API returns
	 *  the total sum of that field across all matching tasks in the `field_sum` response field. */
	sumField?: string;
	/** Sort key to pass to the API.  "manual" and custom-field sorts are handled
	 *  client-side only and must not be forwarded; all other recognised keys are
	 *  sent as `sort_by` so the database applies the correct ORDER BY. */
	sortBy?: string;
	/** When provided for a manual-sort view, passed as view_id so the backend
	 *  can JOIN view_task_positions and return tasks in saved position order. */
	viewId?: string;
	/** Free-text search matched server-side against title and "#<task_number>". */
	search?: string;
	/** Resolved per-custom-field filter criteria, keyed by field key. Sent as a single JSON query param since fields are dynamic per project. */
	customFieldFilters?: Record<string, CustomFieldFilterQuery>;
	/** Inclusive start_date range, "YYYY-MM-DD". */
	startDateAfter?: string;
	startDateBefore?: string;
	/** Inclusive due_date range, "YYYY-MM-DD". */
	dueDateAfter?: string;
	dueDateBefore?: string;
	/** Inclusive story_points range. */
	storyPointsMin?: number;
	storyPointsMax?: number;
	/** Inclusive importance ranges (OR'd together), resolved from selected priority buckets. */
	importanceRanges?: { min: number; max: number }[];
	/** Matches tasks with ANY of these tag values. */
	tags?: string[];
}

function buildTaskQueryParams(opts: ListTasksOptions = {}) {
	const params: Record<string, string | number | boolean> = {};
	params.page_size = opts.pageSize ?? 20;
	if (opts.cursor) params.cursor = opts.cursor;
	if (opts.sprintId === null) params.sprint_id = "null";
	else if (opts.sprintId) params.sprint_id = opts.sprintId;
	if (opts.sprintIds && opts.sprintIds.length > 0)
		params.sprint_ids = opts.sprintIds.join(",");
	if (opts.statusId) params.status_id = opts.statusId;
	if (opts.statusIds && opts.statusIds.length > 0)
		params.status_ids = opts.statusIds.join(",");
	if (opts.assigneeNull) {
		params.assignee_id = "null";
		if (opts.assigneeIds && opts.assigneeIds.length > 0)
			params.assignee_ids = opts.assigneeIds.join(",");
	} else if (opts.assigneeId) {
		params.assignee_id = opts.assigneeId;
	} else if (opts.assigneeIds && opts.assigneeIds.length > 0) {
		params.assignee_ids = opts.assigneeIds.join(",");
	}
	if (opts.taskTypeNull) params.task_type_id = "null";
	else if (opts.taskTypeIds && opts.taskTypeIds.length > 0)
		params.task_type_ids = opts.taskTypeIds.join(",");
	if (opts.parentTaskId) params.parent_task_id = opts.parentTaskId;
	if (opts.sumField && opts.sumField !== "count")
		params.sum_field = opts.sumField;
	// For non-manual sorts, pass sort_by to the backend so the database applies the
	// correct ORDER BY. For manual sort (explicit "manual" or empty string), do not
	// pass sort_by - the backend will use view_position ordering when view_id is provided.
	if (opts.sortBy && opts.sortBy !== "manual" && opts.sortBy !== "") {
		params.sort_by = opts.sortBy;
	}
	if (opts.viewId) {
		params.view_id = opts.viewId;
	}
	if (opts.search?.trim()) params.search = opts.search.trim();
	if (
		opts.customFieldFilters &&
		Object.keys(opts.customFieldFilters).length > 0
	) {
		params.custom_field_filters = JSON.stringify(opts.customFieldFilters);
	}
	if (opts.startDateAfter) params.start_date_after = opts.startDateAfter;
	if (opts.startDateBefore) params.start_date_before = opts.startDateBefore;
	if (opts.dueDateAfter) params.due_date_after = opts.dueDateAfter;
	if (opts.dueDateBefore) params.due_date_before = opts.dueDateBefore;
	if (opts.storyPointsMin != null)
		params.story_points_min = opts.storyPointsMin;
	if (opts.storyPointsMax != null)
		params.story_points_max = opts.storyPointsMax;
	if (opts.importanceRanges && opts.importanceRanges.length > 0) {
		params.importance_ranges = JSON.stringify(opts.importanceRanges);
	}
	if (opts.tags && opts.tags.length > 0) params.tags = opts.tags.join(",");
	return params;
}

export async function listAllTasks(
	projectId: string,
	opts: ListTasksOptions = {},
): Promise<TaskListResult> {
	const params = buildTaskQueryParams(opts);
	const { data } = await apiClient.instance.get<
		SuccessEnvelope<TaskListResult>
	>(`/projects/${projectId}/tasks`, { params });
	return data.data;
}

export async function listSprintTasks(
	projectId: string,
	sprintId: string,
	opts: ListTasksOptions = {},
): Promise<TaskListResult> {
	return listAllTasks(projectId, {
		...opts,
		sprintId,
	});
}

export const ASSIGNED_TASKS_PAGE_SIZE = 10;

/** Open tasks assigned to the current user across every project they belong
 *  to — backs the home page "My Tasks" widget. Cursor-paginated the same way
 *  as listAllTasks; the server always sorts by importance (highest first,
 *  ties broken by created_at ascending) and excludes done-category tasks. */
export async function listAssignedTasks(
	opts: { cursor?: string; pageSize?: number } = {},
): Promise<TaskListResult> {
	const { data } = await apiClient.instance.get<
		SuccessEnvelope<TaskListResult>
	>("/users/me/tasks", {
		params: {
			cursor: opts.cursor,
			page_size: opts.pageSize ?? ASSIGNED_TASKS_PAGE_SIZE,
		},
	});
	return data.data;
}

// No refetchInterval — the home page is a landing spot, not a live board;
// the query just re-fetches on next visit/focus like the rest of the page.
export const assignedTasksQueryOptions = () =>
	infiniteQueryOptions({
		queryKey: ["users", "me", "tasks"],
		queryFn: ({ pageParam }: { pageParam: string | undefined }) =>
			listAssignedTasks({ cursor: pageParam }),
		initialPageParam: undefined as string | undefined,
		getNextPageParam: (lastPage) => lastPage.next_cursor ?? undefined,
	});

export async function createTask(
	projectId: string,
	payload: {
		title: string;
		status_id?: string | null;
		sprint_id?: string | null;
		task_type_id?: string | null;
		assignee_ids?: string[];
		parent_task_id?: string | null;
	},
): Promise<Task> {
	const { data } = await apiClient.instance.post<SuccessEnvelope<Task>>(
		`/projects/${projectId}/tasks`,
		payload,
	);
	return data.data;
}

export async function getTask(
	projectId: string,
	taskId: string,
): Promise<Task> {
	const { data } = await apiClient.instance.get<SuccessEnvelope<Task>>(
		`/projects/${projectId}/tasks/${taskId}`,
	);
	return data.data;
}

export async function updateTask(
	projectId: string,
	taskId: string,
	payload: Partial<{
		title: string;
		status_id: string | null;
		sprint_id: string | null;
		task_type_id: string | null;
		assignee_ids: string[];
		reporter_id: string | null;
		parent_task_id: string | null;
		description: unknown[] | null;
		importance: number;
		story_points?: number | null;
		start_date: string | null;
		due_date: string | null;
		tags: string[];
		custom_fields: Record<string, unknown>;
	}>,
): Promise<Task> {
	const { data } = await apiClient.instance.patch<SuccessEnvelope<Task>>(
		`/projects/${projectId}/tasks/${taskId}`,
		payload,
	);
	return data.data;
}

export async function deleteTask(
	projectId: string,
	taskId: string,
): Promise<void> {
	await apiClient.instance.delete(`/projects/${projectId}/tasks/${taskId}`);
}

export async function listSubtasks(
	projectId: string,
	parentTaskId: string,
): Promise<Task[]> {
	const { data } = await apiClient.instance.get<
		SuccessEnvelope<TaskListResult>
	>(`/projects/${projectId}/tasks`, {
		params: { parent_task_id: parentTaskId, page: 1, page_size: 200 },
	});
	return data.data.items;
}

export async function listViewTaskPositions(
	projectId: string,
	viewId: string,
): Promise<TaskPosition[]> {
	const { data } = await apiClient.instance.get<
		SuccessEnvelope<TaskPositionListResult>
	>(`/projects/${projectId}/views/${viewId}/task-positions`);
	return data.data.items;
}

// ── Query Options ─────────────────────────────────────────────────────────────

export const taskQueryOptions = (projectId: string, taskId: string) =>
	queryOptions({
		queryKey: ["projects", projectId, "tasks", taskId],
		queryFn: () => getTask(projectId, taskId),
		staleTime: 15_000,
	});

export const subtasksQueryOptions = (projectId: string, parentTaskId: string) =>
	queryOptions({
		queryKey: ["projects", projectId, "tasks", parentTaskId, "subtasks"],
		queryFn: () => listSubtasks(projectId, parentTaskId),
		staleTime: 15_000,
	});

export const sprintsQueryOptions = (projectId: string) =>
	queryOptions({
		queryKey: ["projects", projectId, "sprints"],
		queryFn: () => listSprints(projectId),
		staleTime: 30_000,
	});

export const sprintQueryOptions = (projectId: string, sprintId: string) =>
	queryOptions({
		queryKey: ["projects", projectId, "sprints", sprintId],
		queryFn: () => getSprint(projectId, sprintId),
		staleTime: 30_000,
	});

export const allTasksQueryOptions = (
	projectId: string,
	opts: ListTasksOptions = {},
) =>
	queryOptions({
		queryKey: ["projects", projectId, "tasks", opts],
		queryFn: () => listAllTasks(projectId, opts),
		staleTime: 15_000,
	});

export const viewTaskPositionsQueryOptions = (
	projectId: string,
	viewId: string,
) =>
	queryOptions({
		queryKey: ["projects", projectId, "views", viewId, "task-positions"],
		queryFn: () => listViewTaskPositions(projectId, viewId),
		staleTime: 15_000,
	});

export const sprintTasksQueryOptions = (projectId: string, sprintId: string) =>
	queryOptions({
		queryKey: ["projects", projectId, "sprints", sprintId, "tasks"],
		queryFn: () => listSprintTasks(projectId, sprintId),
		staleTime: 15_000,
	});

export const EPIC_TASKS_PAGE_SIZE = 20;

/** Fetches one page of tasks whose task_type is "Epic" for a project.
 *  Cursor-paginated at EPIC_TASKS_PAGE_SIZE (well under the backend's 200
 *  max page size) — callers that need every epic should page through with
 *  epicTasksInfiniteQueryOptions rather than requesting one giant page. */
export async function listEpicTasks(
	projectId: string,
	epicTypeId: string,
	opts: { cursor?: string } = {},
): Promise<TaskListResult> {
	return listAllTasks(projectId, {
		taskTypeIds: [epicTypeId],
		pageSize: EPIC_TASKS_PAGE_SIZE,
		cursor: opts.cursor,
	});
}

/** Infinite-query version of the epic-task fetch — backs every epic picker
 *  (task detail Epic field, board/list-view quick-pick popovers, the
 *  right-click "Epic" submenu). Pages load 20 at a time via next_cursor;
 *  consumers flatten `data.pages` into a flat epic list and expose
 *  fetchNextPage/hasNextPage as "Load more" affordances in the picker UI. */
export const epicTasksInfiniteQueryOptions = (
	projectId: string,
	epicTypeId: string,
) =>
	infiniteQueryOptions({
		queryKey: ["projects", projectId, "tasks", "epics", epicTypeId],
		queryFn: ({ pageParam }: { pageParam: string | undefined }) =>
			listEpicTasks(projectId, epicTypeId, { cursor: pageParam }),
		initialPageParam: undefined as string | undefined,
		getNextPageParam: (lastPage) => lastPage.next_cursor ?? undefined,
		staleTime: 30_000,
		enabled: !!projectId && !!epicTypeId,
	});

/** Cursor-paginated search backing the epic picker's search box — queried
 *  directly (outside the shared useInfiniteQuery above) against the same
 *  `search` param used elsewhere for task lookup, since a project can have
 *  far more epics than the picker keeps paginated locally and filtering
 *  only the loaded page would silently miss most of them. Callers debounce
 *  input before calling this, and page through matches the same way
 *  listEpicTasks does. */
export async function searchEpicTasks(
	projectId: string,
	epicTypeId: string,
	query: string,
	opts: { cursor?: string } = {},
): Promise<TaskListResult> {
	return listAllTasks(projectId, {
		taskTypeIds: [epicTypeId],
		search: query,
		pageSize: EPIC_TASKS_PAGE_SIZE,
		cursor: opts.cursor,
	});
}

export const TASK_PICKER_PAGE_SIZE = 20;

/** Fetches one page of any task in the project (no type filter) — backs
 *  generic "pick a task" pickers (e.g. an automation trigger's fixed target
 *  task), same shape and page size as listEpicTasks minus the type filter. */
export async function listTasksForPicker(
	projectId: string,
	opts: { cursor?: string } = {},
): Promise<TaskListResult> {
	return listAllTasks(projectId, {
		pageSize: TASK_PICKER_PAGE_SIZE,
		cursor: opts.cursor,
	});
}

/** Infinite-query version of listTasksForPicker — mirrors
 *  epicTasksInfiniteQueryOptions for pickers that choose from any task
 *  rather than epics only. */
export const tasksPickerInfiniteQueryOptions = (projectId: string) =>
	infiniteQueryOptions({
		queryKey: ["projects", projectId, "tasks", "picker"],
		queryFn: ({ pageParam }: { pageParam: string | undefined }) =>
			listTasksForPicker(projectId, { cursor: pageParam }),
		initialPageParam: undefined as string | undefined,
		getNextPageParam: (lastPage) => lastPage.next_cursor ?? undefined,
		staleTime: 30_000,
		enabled: !!projectId,
	});

/** Cursor-paginated search backing a generic task picker's search box — same
 *  shape as searchEpicTasks minus the epic type filter. */
export async function searchTasksForPicker(
	projectId: string,
	query: string,
	opts: { cursor?: string } = {},
): Promise<TaskListResult> {
	return listAllTasks(projectId, {
		search: query,
		pageSize: TASK_PICKER_PAGE_SIZE,
		cursor: opts.cursor,
	});
}

/** Fetches child tasks of an epic (tasks with parent_task_id = epicId). */
export const epicChildTasksQueryOptions = (projectId: string, epicId: string) =>
	queryOptions({
		queryKey: ["projects", projectId, "tasks", epicId, "children"],
		queryFn: () => listSubtasks(projectId, epicId),
		staleTime: 15_000,
	});

// ── Activity & Comments API ────────────────────────────────────────────────────

export interface Activity {
	id: string;
	task_id: string;
	actor_id?: string | null;
	actor_name: string;
	actor_username: string;
	actor_avatar_url?: string | null;
	actor_avatar_thumb_url?: string | null;
	activity_type: string;
	content: Record<string, unknown> | unknown[];
	created_at: string;
	updated_at: string;
}

interface ActivityListResult {
	items: Activity[];
}

export async function listTaskActivities(
	projectId: string,
	taskId: string,
): Promise<Activity[]> {
	const { data } = await apiClient.instance.get<
		SuccessEnvelope<ActivityListResult>
	>(`/projects/${projectId}/tasks/${taskId}/activities`);
	return data.data.items;
}

export async function addComment(
	projectId: string,
	taskId: string,
	content: unknown[],
): Promise<Activity> {
	const { data } = await apiClient.instance.post<SuccessEnvelope<Activity>>(
		`/projects/${projectId}/tasks/${taskId}/activities/comments`,
		{ content },
	);
	return data.data;
}

export async function updateComment(
	projectId: string,
	taskId: string,
	commentId: string,
	content: unknown[],
): Promise<Activity> {
	const { data } = await apiClient.instance.patch<SuccessEnvelope<Activity>>(
		`/projects/${projectId}/tasks/${taskId}/activities/comments/${commentId}`,
		{ content },
	);
	return data.data;
}

export async function deleteComment(
	projectId: string,
	taskId: string,
	commentId: string,
): Promise<void> {
	await apiClient.instance.delete(
		`/projects/${projectId}/tasks/${taskId}/activities/comments/${commentId}`,
	);
}

export const taskActivitiesQueryOptions = (projectId: string, taskId: string) =>
	queryOptions({
		queryKey: ["projects", projectId, "tasks", taskId, "activities"],
		queryFn: () => listTaskActivities(projectId, taskId),
		staleTime: 15_000,
		enabled: !!projectId && !!taskId,
	});

// ── Task Links ────────────────────────────────────────────────────────────────

export type LinkType = "blocks" | "relates_to" | "duplicates";

export type DisplayLinkType =
	| "blocks"
	| "is_blocked_by"
	| "relates_to"
	| "duplicates"
	| "is_duplicated_by";

export interface LinkedTaskSummary {
	id: string;
	task_number: number;
	title: string;
	status_id?: string | null;
	task_type_id?: string | null;
}

export interface TaskLink {
	id: string;
	source_task_id: string;
	target_task_id: string;
	link_type: LinkType;
	display_link_type: DisplayLinkType;
	linked_task: LinkedTaskSummary;
	created_by?: string | null;
	created_at: string;
}

export const LINK_TYPE_LABELS: Record<DisplayLinkType, string> = {
	blocks: "blocks",
	is_blocked_by: "is blocked by",
	relates_to: "relates to",
	duplicates: "duplicates",
	is_duplicated_by: "is duplicated by",
};

export async function listTaskLinks(
	projectId: string,
	taskId: string,
): Promise<TaskLink[]> {
	const { data } = await apiClient.instance.get<
		SuccessEnvelope<{ items: TaskLink[] }>
	>(`/projects/${projectId}/tasks/${taskId}/links`);
	return data.data.items;
}

export async function createTaskLink(
	projectId: string,
	taskId: string,
	payload: { target_task_id: string; link_type: LinkType },
): Promise<TaskLink> {
	const { data } = await apiClient.instance.post<SuccessEnvelope<TaskLink>>(
		`/projects/${projectId}/tasks/${taskId}/links`,
		payload,
	);
	return data.data;
}

export async function deleteTaskLink(
	projectId: string,
	taskId: string,
	linkId: string,
): Promise<void> {
	await apiClient.instance.delete(
		`/projects/${projectId}/tasks/${taskId}/links/${linkId}`,
	);
}

export const taskLinksQueryOptions = (projectId: string, taskId: string) =>
	queryOptions({
		queryKey: ["projects", projectId, "tasks", taskId, "links"],
		queryFn: () => listTaskLinks(projectId, taskId),
		staleTime: 15_000,
		enabled: !!projectId && !!taskId,
	});
