import type { TFunction } from "i18next";
import {
	type FilterConfig,
	resolveFilterConfig,
	type Sprint,
	type Task,
	type ViewLayout,
} from "@/lib/interaction-api";
import type {
	CustomFieldDefinition,
	ProjectMember,
	TaskStatus,
	TaskType,
} from "@/lib/project-api";

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
			{ key: "__backlog", label: "Backlog", fieldValue: null },
		];
	}

	if (columnBy === "assignee") {
		return [
			...ctx.members.map((m) => ({
				key: m.id,
				label: m.full_name || m.username,
				fieldValue: m.id,
			})),
			{ key: "__unassigned", label: "Unassigned", fieldValue: null },
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
			{ key: "__none", label: "No Reporter", fieldValue: null },
		];
	}

	if (columnBy === "importance") {
		return PRIORITY_LEVELS.map((p) => ({
			key: String(p.value),
			label: p.label,
			color: p.value > 0 ? p.color : undefined,
			// fieldValue is the representative raw importance to assign on drop
			fieldValue: IMPORTANCE_BUCKET_VALUES[p.value] ?? 0,
		}));
	}

	if (columnBy === "type") {
		return [
			...ctx.taskTypes.map((t) => ({
				key: t.id,
				label: t.name,
				color: t.color ?? undefined,
				fieldValue: t.id,
			})),
			{ key: "__none", label: "No Type", fieldValue: null },
		];
	}

	// Custom field
	const cf = ctx.customFields.find((f) => f.field_key === columnBy);
	if (cf) {
		if (cf.field_type === "select" || cf.field_type === "multi_select") {
			return [
				...cf.options.map((opt) => ({
					key: opt,
					label: opt,
					fieldValue: opt,
				})),
				{ key: "__none", label: "None", fieldValue: null },
			];
		}
		if (cf.field_type === "boolean") {
			return [
				{ key: "true", label: "Yes", fieldValue: true },
				{ key: "false", label: "No", fieldValue: false },
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
		return [task.assignee_id ?? "__unassigned"];
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
): ColumnGroupDef[] {
	if (!swimlanes || swimlanes === "none") {
		return [{ key: "__all", label: "", fieldValue: null }];
	}
	const defs = getColumnGroupDefs(swimlanes, ctx);
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

const FIELD_NAME_KEYS = "project.interactions.viewSettings.fieldNames";

export const BUILTIN_COLUMN_BY: { key: string; labelKey: string }[] = [
	{ key: "status", labelKey: `${FIELD_NAME_KEYS}.status` },
	{ key: "sprint", labelKey: `${FIELD_NAME_KEYS}.sprint` },
	{ key: "assignee", labelKey: `${FIELD_NAME_KEYS}.assignee` },
	{ key: "importance", labelKey: `${FIELD_NAME_KEYS}.importance` },
	{ key: "type", labelKey: `${FIELD_NAME_KEYS}.type` },
	{ key: "reporter", labelKey: `${FIELD_NAME_KEYS}.reporter` },
];

export const BUILTIN_SORT_BY: { key: string; labelKey: string }[] = [
	{ key: "manual", labelKey: `${FIELD_NAME_KEYS}.manual` },
	{ key: "importance", labelKey: `${FIELD_NAME_KEYS}.importance` },
	{ key: "story_points", labelKey: `${FIELD_NAME_KEYS}.storyPoints` },
	{ key: "title", labelKey: `${FIELD_NAME_KEYS}.title` },
	{ key: "created", labelKey: `${FIELD_NAME_KEYS}.created` },
	{ key: "start_date", labelKey: `${FIELD_NAME_KEYS}.startDate` },
	{ key: "due_date", labelKey: `${FIELD_NAME_KEYS}.dueDate` },
];

export const BUILTIN_SWIMLANES: { key: string; labelKey: string }[] = [
	{ key: "none", labelKey: `${FIELD_NAME_KEYS}.none` },
	{ key: "assignee", labelKey: `${FIELD_NAME_KEYS}.assignee` },
	{ key: "importance", labelKey: `${FIELD_NAME_KEYS}.importance` },
	{ key: "type", labelKey: `${FIELD_NAME_KEYS}.type` },
];

export const FIELD_SUM_COUNT: { key: string; labelKey: string } = {
	key: "count",
	labelKey: `${FIELD_NAME_KEYS}.count`,
};

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
export const BUILTIN_FIELDS: { key: string; labelKey: string }[] = [
	{ key: "assignee", labelKey: `${FIELD_NAME_KEYS}.assignee` },
	{ key: "status", labelKey: `${FIELD_NAME_KEYS}.status` },
	{ key: "importance", labelKey: `${FIELD_NAME_KEYS}.importance` },
	{ key: "story_points", labelKey: `${FIELD_NAME_KEYS}.storyPoints` },
	{ key: "type", labelKey: `${FIELD_NAME_KEYS}.type` },
	{ key: "epic", labelKey: `${FIELD_NAME_KEYS}.epic` },
	{ key: "reporter", labelKey: `${FIELD_NAME_KEYS}.reporter` },
	{ key: "start_date", labelKey: `${FIELD_NAME_KEYS}.startDate` },
	{ key: "due_date", labelKey: `${FIELD_NAME_KEYS}.dueDate` },
	{ key: "created", labelKey: `${FIELD_NAME_KEYS}.created` },
];

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
	t: TFunction,
): { key: string; label: string }[] {
	const custom = customFields
		.filter((cf) =>
			["select", "multi_select", "boolean", "number"].includes(cf.field_type),
		)
		.map((cf) => ({ key: cf.field_key, label: cf.display_name }));
	return [
		...BUILTIN_COLUMN_BY.map((o) => ({ key: o.key, label: t(o.labelKey) })),
		...custom,
	];
}

export function buildSortByOptions(
	customFields: CustomFieldDefinition[],
	t: TFunction,
): { key: string; label: string }[] {
	const custom = customFields
		.filter((cf) => ["number", "date", "select"].includes(cf.field_type))
		.map((cf) => ({ key: cf.field_key, label: cf.display_name }));
	return [
		...BUILTIN_SORT_BY.map((o) => ({ key: o.key, label: t(o.labelKey) })),
		...custom,
	];
}

export function buildSwimlaneOptions(
	customFields: CustomFieldDefinition[],
	t: TFunction,
): { key: string; label: string }[] {
	const custom = customFields
		.filter((cf) =>
			["select", "multi_select", "boolean"].includes(cf.field_type),
		)
		.map((cf) => ({ key: cf.field_key, label: cf.display_name }));
	return [
		...BUILTIN_SWIMLANES.map((o) => ({ key: o.key, label: t(o.labelKey) })),
		...custom,
	];
}

export function buildFieldSumOptions(
	customFields: CustomFieldDefinition[],
	t: TFunction,
): { key: string; label: string }[] {
	const custom = customFields
		.filter((cf) => cf.field_type === "number")
		.map((cf) => ({ key: cf.field_key, label: cf.display_name }));
	return [
		{ key: FIELD_SUM_COUNT.key, label: t(FIELD_SUM_COUNT.labelKey) },
		{ key: "story_points", label: t(`${FIELD_NAME_KEYS}.storyPoints`) },
		...custom,
	];
}

export function buildAllFieldOptions(
	customFields: CustomFieldDefinition[],
	t: TFunction,
): { key: string; label: string }[] {
	const custom = customFields.map((cf) => ({
		key: cf.field_key,
		label: cf.display_name,
	}));
	return [
		...BUILTIN_FIELDS.map((o) => ({ key: o.key, label: t(o.labelKey) })),
		...custom,
	];
}

// ── Task update payload builder for column drag ───────────────────────────────

export type TaskFieldUpdate = Partial<{
	status_id: string | null;
	assignee_id: string | null;
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
		return { assignee_id: (columnFieldValue as string | null) ?? null };
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
