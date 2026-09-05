import type { TFunction } from "i18next";
import {
	type FilterConfig,
	type ListTasksOptions,
	resolveFilterConfig,
	type Sprint,
	type Task,
	type TaskListResult,
	type ViewLayout,
} from "@/lib/interaction-api";
import type {
	CustomFieldDefinition,
	ProjectMember,
	TaskStatus,
	TaskType,
} from "@/lib/project-api";
import {
	createLoadMoreScrollHandler,
	type LoadMorePagination,
} from "@/lib/scroll-pagination";

import {
	getImportanceBucket,
	IMPORTANCE_BUCKET_VALUES,
	PRIORITY_LEVELS,
} from "./priority";

// ── Context ──────────────────────────────────────────────────────────────────

export interface ViewContext {
	statuses: TaskStatus[];
	taskTypes: TaskType[];
	members: ProjectMember[];
	customFields: CustomFieldDefinition[];
	sprints?: Sprint[];
}

// ── Column / Swimlane group definition ───────────────────────────────────────

export interface ColumnGroupDef {
	/** Unique key for this column */
	key: string;
	/** Display label */
	label: string;
	/** Optional badge colour */
	color?: string;
	/**
	 * The raw field value associated with this column.
	 * Used when dragging a task to this column to update the task's field.
	 */
	fieldValue: unknown;
}

// ── Built-in column groups per field ─────────────────────────────────────────

/**
 * Returns all possible column group definitions for the given column_by field.
 * All columns are returned even if they contain zero tasks.
 */
export function getColumnGroupDefs(
	columnBy: string | undefined,
	ctx: ViewContext,
	t: TFunction<"projects">,
): ColumnGroupDef[] {
	if (!columnBy || columnBy === "status") {
		return ctx.statuses
			.slice()
			.sort((a, b) => a.position - b.position)
			.map((s) => ({
				key: s.id,
				label: s.name,
				color: s.color ?? undefined,
				fieldValue: s.id,
			}));
	}

	if (columnBy === "sprint") {
		const sprintGroups = (ctx.sprints ?? [])
			.filter((s) => s.status !== "completed")
			.sort((a, b) => {
				if (a.status === b.status) return a.name.localeCompare(b.name);
				return a.status === "active" ? -1 : 1;
			})
			.map((s) => ({
				key: s.id,
				label: s.name,
				fieldValue: s.id,
			}));
		return [
			...sprintGroups,
			{
				key: "__backlog",
				label: t("viewUtils.groups.backlog"),
				fieldValue: null,
			},
		];
	}

	if (columnBy === "assignee") {
		return [
			...ctx.members.map((m) => ({
				key: m.id,
				label: m.full_name || m.username,
				fieldValue: m.id,
			})),
			{
				key: "__unassigned",
				label: t("viewUtils.groups.unassigned"),
				fieldValue: null,
			},
		];
	}

	if (columnBy === "reporter") {
		return [
			...[...ctx.members]
				.sort((a, b) =>
					(a.full_name || a.username).localeCompare(b.full_name || b.username),
				)
				.map((m) => ({
					key: m.id,
					label: m.full_name || m.username,
					fieldValue: m.id,
				})),
			{
				key: "__none",
				label: t("viewUtils.groups.noReporter"),
				fieldValue: null,
			},
		];
	}

	if (columnBy === "importance") {
		return PRIORITY_LEVELS.map((p) => ({
			key: String(p.value),
			label: t(p.labelKey),
			color: p.value > 0 ? p.color : undefined,
			// fieldValue is the representative raw importance to assign on drop
			fieldValue: IMPORTANCE_BUCKET_VALUES[p.value] ?? 0,
		}));
	}

	if (columnBy === "type") {
		return [
			...ctx.taskTypes.map((tt) => ({
				key: tt.id,
				label: tt.name,
				color: tt.color ?? undefined,
				fieldValue: tt.id,
			})),
			{ key: "__none", label: t("viewUtils.groups.noType"), fieldValue: null },
		];
	}

	// Custom field
	const cf = ctx.customFields.find((f) => f.field_key === columnBy);
	if (cf) {
		if (cf.field_type === "select" || cf.field_type === "multi_select") {
			return [
				...cf.options.map((opt) => ({
					key: opt.value,
					label: opt.value,
					color: opt.color ?? undefined,
					fieldValue: opt.value,
				})),
				{ key: "__none", label: t("viewUtils.groups.none"), fieldValue: null },
			];
		}
		if (cf.field_type === "boolean") {
			return [
				{ key: "true", label: t("viewUtils.groups.yes"), fieldValue: true },
				{ key: "false", label: t("viewUtils.groups.no"), fieldValue: false },
			];
		}
		// number / text / date → dynamic groups from task values (built at render time)
		return [];
	}

	return [];
}

/**
 * Hides column defs whose status key is excluded by the active status filter.
 * Pass the full defs list; returns a filtered copy (or the same reference when
 * no filter applies so callers can avoid needless re-renders via useMemo).
 */
export function applyStatusFilterToColumnDefs(
	defs: ColumnGroupDef[],
	isStatusGrouping: boolean,
	statusFilterConfig: FilterConfig | undefined,
	statuses: TaskStatus[],
): ColumnGroupDef[] {
	if (!isStatusGrouping || !statusFilterConfig) return defs;
	const allowed = new Set(
		resolveFilterConfig(
			statusFilterConfig,
			statuses.map((s) => s.id),
		),
	);
	return defs.filter((col) => allowed.has(col.key));
}

/**
 * Returns the column key(s) for a task given the column_by field.
 * Returns an array because multi_select tasks can appear in multiple columns.
 */
export function getTaskColumnKeys(
	task: Task,
	columnBy: string | undefined,
	ctx: ViewContext,
): string[] {
	if (!columnBy || columnBy === "status") {
		return [task.status_id ?? "__none"];
	}
	if (columnBy === "sprint") {
		return [task.sprint_id ?? "__backlog"];
	}
	if (columnBy === "assignee") {
		return [task.assignee_ids?.[0] ?? "__unassigned"];
	}
	if (columnBy === "reporter") {
		return [task.reporter_id ?? "__none"];
	}
	if (columnBy === "importance") {
		// Bucket the raw numeric importance into one of the 5 named levels
		return [String(getImportanceBucket(task.importance))];
	}
	if (columnBy === "type") {
		return [task.task_type_id ?? "__none"];
	}

	const cf = ctx.customFields.find((f) => f.field_key === columnBy);
	if (cf) {
		const val = task.custom_fields[cf.field_key];
		if (cf.field_type === "multi_select") {
			if (Array.isArray(val) && val.length > 0) {
				return val.map(String);
			}
			return ["__none"];
		}
		if (val === null || val === undefined || val === "") {
			return ["__none"];
		}
		if (cf.field_type === "boolean") {
			return [val ? "true" : "false"];
		}
		return [String(val)];
	}

	return ["__none"];
}

// ── Swimlane helpers ──────────────────────────────────────────────────────────

/**
 * Returns swimlane group definitions.
 * Returns a single "__all" group when swimlanes are disabled.
 */
export function getSwimlaneDefs(
	swimlanes: string | undefined,
	ctx: ViewContext,
	t: TFunction<"projects">,
): ColumnGroupDef[] {
	if (!swimlanes || swimlanes === "none") {
		return [{ key: "__all", label: "", fieldValue: null }];
	}
	const defs = getColumnGroupDefs(swimlanes, ctx, t);
	// Show Critical (highest bucket) first, None last
	if (swimlanes === "importance") return [...defs].reverse();
	return defs;
}

/** Returns the swimlane key for a task. */
export function getTaskSwimlaneKey(
	task: Task,
	swimlanes: string | undefined,
	ctx: ViewContext,
): string {
	if (!swimlanes || swimlanes === "none") return "__all";
	return getTaskColumnKeys(task, swimlanes, ctx)[0];
}

// ── Option builders for ViewSettingsPanel dropdowns ──────────────────────────

export const BUILTIN_COLUMN_BY = [
	{ key: "status", labelKey: "viewUtils.columnBy.status" },
	{ key: "sprint", labelKey: "viewUtils.columnBy.sprint" },
	{ key: "assignee", labelKey: "viewUtils.columnBy.assignee" },
	{ key: "importance", labelKey: "viewUtils.columnBy.importance" },
	{ key: "type", labelKey: "viewUtils.columnBy.type" },
	{ key: "reporter", labelKey: "viewUtils.columnBy.reporter" },
] as const satisfies { key: string; labelKey: string }[];

export const BUILTIN_SORT_BY = [
	{ key: "manual", labelKey: "viewUtils.sortBy.manual" },
	{ key: "importance", labelKey: "viewUtils.sortBy.importance" },
	{ key: "story_points", labelKey: "viewUtils.sortBy.storyPoints" },
	{ key: "title", labelKey: "viewUtils.sortBy.title" },
	{ key: "created", labelKey: "viewUtils.sortBy.created" },
	{ key: "start_date", labelKey: "viewUtils.sortBy.startDate" },
	{ key: "due_date", labelKey: "viewUtils.sortBy.dueDate" },
] as const satisfies { key: string; labelKey: string }[];

export const BUILTIN_SWIMLANES = [
	{ key: "none", labelKey: "viewUtils.swimlanes.none" },
	{ key: "assignee", labelKey: "viewUtils.swimlanes.assignee" },
	{ key: "importance", labelKey: "viewUtils.swimlanes.importance" },
	{ key: "type", labelKey: "viewUtils.swimlanes.type" },
] as const satisfies { key: string; labelKey: string }[];

export const FIELD_SUM_COUNT = {
	key: "count",
	labelKey: "viewUtils.fieldSum.count",
} as const satisfies { key: string; labelKey: string };

/**
 * Per-layout pagination defaults, keyed by `ViewLayout`. `initial` is the page
 * size used on a view's first load; `perPage` is the batch size used by
 * subsequent "load more" fetches. A saved view's `initial_page_size` /
 * `page_size` always takes priority over these — they only apply when unset.
 * Plugin views render arbitrary content and don't paginate through this
 * mechanism, so they reuse the Table (list) defaults.
 */
export const PAGE_SIZE_DEFAULTS: Record<
	ViewLayout,
	{ initial: number; perPage: number }
> = {
	Table: { initial: 5, perPage: 20 },
	Board: { initial: 20, perPage: 20 },
	Roadmap: { initial: 100, perPage: 100 },
	Plugin: { initial: 5, perPage: 20 },
};

export function getDefaultInitialPageSize(
	layout: ViewLayout | undefined,
): number {
	return PAGE_SIZE_DEFAULTS[layout ?? "Table"].initial;
}

export function getDefaultPageSize(layout: ViewLayout | undefined): number {
	return PAGE_SIZE_DEFAULTS[layout ?? "Table"].perPage;
}

export const PAGE_SIZE_OPTIONS: { key: string; label: string }[] = [
	{ key: "5", label: "5" },
	{ key: "10", label: "10" },
	{ key: "20", label: "20" },
	{ key: "50", label: "50" },
	{ key: "100", label: "100" },
];

/** All built-in fields available for the Field Picker. Title is excluded — it is always visible. */
export const BUILTIN_FIELDS = [
	{ key: "assignee", labelKey: "viewUtils.fields.assignee" },
	{ key: "status", labelKey: "viewUtils.fields.status" },
	{ key: "importance", labelKey: "viewUtils.fields.importance" },
	{ key: "story_points", labelKey: "viewUtils.fields.storyPoints" },
	{ key: "type", labelKey: "viewUtils.fields.type" },
	{ key: "epic", labelKey: "viewUtils.fields.epic" },
	{ key: "tags", labelKey: "viewUtils.fields.tags" },
	{ key: "reporter", labelKey: "viewUtils.fields.reporter" },
	{ key: "start_date", labelKey: "viewUtils.fields.startDate" },
	{ key: "due_date", labelKey: "viewUtils.fields.dueDate" },
	{ key: "created", labelKey: "viewUtils.fields.created" },
] as const satisfies { key: string; labelKey: string }[];

/**
 * Default visible fields (excluding title, which is always shown).
 * Used when a view has no field config saved yet.
 */
export const DEFAULT_VISIBLE_FIELDS = [
	"assignee",
	"importance",
	"story_points",
	"type",
];

export function buildColumnByOptions(
	customFields: CustomFieldDefinition[],
	t: TFunction<"projects">,
): { key: string; label: string }[] {
	const custom = customFields
		.filter((cf) =>
			["select", "multi_select", "boolean", "number"].includes(cf.field_type),
		)
		.map((cf) => ({ key: cf.field_key, label: cf.display_name }));
	return [
		...BUILTIN_COLUMN_BY.map((b) => ({ key: b.key, label: t(b.labelKey) })),
		...custom,
	];
}

export function buildSortByOptions(
	customFields: CustomFieldDefinition[],
	t: TFunction<"projects">,
): { key: string; label: string }[] {
	const custom = customFields
		.filter((cf) => ["number", "date", "select"].includes(cf.field_type))
		.map((cf) => ({ key: cf.field_key, label: cf.display_name }));
	return [
		...BUILTIN_SORT_BY.map((b) => ({ key: b.key, label: t(b.labelKey) })),
		...custom,
	];
}

export function buildSwimlaneOptions(
	customFields: CustomFieldDefinition[],
	t: TFunction<"projects">,
): { key: string; label: string }[] {
	const custom = customFields
		.filter((cf) =>
			["select", "multi_select", "boolean"].includes(cf.field_type),
		)
		.map((cf) => ({ key: cf.field_key, label: cf.display_name }));
	return [
		...BUILTIN_SWIMLANES.map((b) => ({ key: b.key, label: t(b.labelKey) })),
		...custom,
	];
}

export function buildFieldSumOptions(
	customFields: CustomFieldDefinition[],
	t: TFunction<"projects">,
): { key: string; label: string }[] {
	const custom = customFields
		.filter((cf) => cf.field_type === "number")
		.map((cf) => ({ key: cf.field_key, label: cf.display_name }));
	return [
		{ key: FIELD_SUM_COUNT.key, label: t(FIELD_SUM_COUNT.labelKey) },
		{ key: "story_points", label: t("viewUtils.fields.storyPoints") },
		...custom,
	];
}

export function buildAllFieldOptions(
	customFields: CustomFieldDefinition[],
	t: TFunction<"projects">,
): { key: string; label: string }[] {
	const custom = customFields.map((cf) => ({
		key: cf.field_key,
		label: cf.display_name,
	}));
	return [
		...BUILTIN_FIELDS.map((b) => ({ key: b.key, label: t(b.labelKey) })),
		...custom,
	];
}

// ── Task update payload builder for column drag ───────────────────────────────

/** Load-more state/handler for an epic picker fed by a paginated (20-per-page)
 * epic query. Threaded from the view/layout root, which owns the single
 * `useInfiniteQuery`, down to whichever picker UI (Popover/DropdownMenu/
 * ContextMenu) is currently rendering the epic list, so every epic picker
 * app-wide shares one "Load more" affordance instead of duplicating queries.
 * Re-exported from the generic scroll-pagination helper — kept as a named
 * alias here since every epic-picker call site already imports it by this
 * name from "./view-utils". */
export type EpicsPagination = LoadMorePagination;
export const createEpicScrollHandler = createLoadMoreScrollHandler;

export type TaskFieldUpdate = Partial<{
	status_id: string | null;
	assignee_ids: string[];
	importance: number;
	story_points: number | null;
	task_type_id: string | null;
	custom_fields: Record<string, unknown>;
	sprint_id: string | null;
	parent_task_id: string | null;
}>;

/**
 * Builds the update payload when a task is dragged into a column whose
 * column_by field value is `columnFieldValue`.
 */
export function buildColumnDropUpdate(
	columnBy: string | undefined,
	columnFieldValue: unknown,
	customFields: CustomFieldDefinition[],
): TaskFieldUpdate {
	if (!columnBy || columnBy === "status") {
		return { status_id: (columnFieldValue as string | null) ?? null };
	}
	if (columnBy === "sprint") {
		return { sprint_id: (columnFieldValue as string | null) ?? null };
	}
	if (columnBy === "assignee") {
		return {
			assignee_ids: columnFieldValue ? [columnFieldValue as string] : [],
		};
	}
	if (columnBy === "importance") {
		return { importance: Number(columnFieldValue) || 0 };
	}
	if (columnBy === "type") {
		return { task_type_id: (columnFieldValue as string | null) ?? null };
	}

	const cf = customFields.find((f) => f.field_key === columnBy);
	if (cf) {
		return { custom_fields: { [cf.field_key]: columnFieldValue } };
	}

	return {};
}

// ── Per-column "load more" extras reconciliation ─────────────────────────────

/**
 * Whether a column's `colExtraTasks` ("load more" overflow, tracked
 * separately from the column's base query result) is now redundant and can
 * be dropped in favor of the base query's own data.
 *
 * True once the base query's data either (a) has caught up to the expanded
 * depth the extras represent, or (b) reports there's nothing left to fetch
 * (`next_cursor` is null/undefined) — which is authoritative even when
 * shorter than the expected depth. (b) matters when the true total has
 * permanently shrunk below the expected depth (an already-loaded task got
 * deleted, or moved out of this column/filter by someone else): without it,
 * the base query can never again return `expectedDepth` items, so extras
 * would never clear and a deleted/moved task would linger as an unclearable
 * ghost row indefinitely.
 */
export function shouldClearColumnExtras(
	data: Pick<TaskListResult, "items" | "next_cursor"> | undefined,
	expectedDepth: number,
): boolean {
	if (!data) return false;
	return data.items.length >= expectedDepth || data.next_cursor == null;
}

// ── Selected-task resolution (survives transient gaps in the task list) ──────

/**
 * Resolves the task detail dialog's target task from the currently-loaded
 * `tasks` list, falling back to the last-resolved task for the same id when
 * `tasks` transiently doesn't contain it (e.g. a background refetch drops it
 * from the loaded page right before a re-fetch restores it). Without this
 * fallback, that gap flips the dialog's `open` prop to false and it flashes
 * closed.
 *
 * `nextLastKnown` mirrors exactly what the caller should store for the next
 * call: it only changes on a genuine selection change (cleared) or a
 * successful lookup (updated) — a transient miss for the *same* selection
 * leaves it untouched, so a later miss can't be masked by an unrelated
 * earlier selection's cached task.
 */
export function resolveSelectedTask(
	tasks: Task[],
	selectedTaskId: string | null,
	lastKnownTask: Task | null,
): { resolved: Task | null; nextLastKnown: Task | null } {
	if (!selectedTaskId) {
		return { resolved: null, nextLastKnown: null };
	}
	const found = tasks.find((t) => t.id === selectedTaskId) ?? null;
	if (found) {
		return { resolved: found, nextLastKnown: found };
	}
	const fallback = lastKnownTask?.id === selectedTaskId ? lastKnownTask : null;
	return { resolved: fallback, nextLastKnown: lastKnownTask };
}

// ── Scoped placeholder data for pagination queries ────────────────────────────

/**
 * Builds a React Query `placeholderData` function that keeps the previous
 * query's result only when the *only* difference between the previous
 * request's options and `currentOpts` is `pageSize` — i.e. this queryKey
 * change is a "load more" page-size bump or a WS-triggered depth-restore
 * refetch, not a genuine filter/sort/search change.
 *
 * These queries key on the full filter-options object, so the stock
 * `keepPreviousData` helper (which keeps previous data across *any* queryKey
 * change) would also kick in when a user edits a filter or types a search:
 * the previous, now non-matching results would keep rendering — with no
 * loading skeleton and no `isPlaceholderData` indicator — until the new
 * request resolves. Scoping the reuse to pageSize-only changes restores the
 * loading state for genuine filter/sort/search changes while still avoiding
 * the pagination flash this was introduced to fix.
 */
export function keepPreviousDataOnPageSizeChangeOnly(
	currentOpts: ListTasksOptions,
) {
	return (
		previousData: TaskListResult | undefined,
		previousQuery: { queryKey: readonly unknown[] } | undefined,
	): TaskListResult | undefined => {
		if (previousData === undefined || !previousQuery) return undefined;
		const prevOpts = previousQuery.queryKey.at(-1) as
			| ListTasksOptions
			| undefined;
		if (!prevOpts || typeof prevOpts !== "object") return undefined;
		const { pageSize: _prevPageSize, ...prevRest } = prevOpts;
		const { pageSize: _currPageSize, ...currRest } = currentOpts;
		return JSON.stringify(prevRest) === JSON.stringify(currRest)
			? previousData
			: undefined;
	};
}
