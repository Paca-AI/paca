import {
	useInfiniteQuery,
	useMutation,
	useQueries,
	useQuery,
	useQueryClient,
} from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";

// Upper bound for manual-sort positions.  All computed positions stay strictly
// inside (0, POSITION_MAX) by always taking midpoints toward the boundaries, so
// positions can never go negative and never overflow float64.
const POSITION_MAX = Number.MAX_SAFE_INTEGER; // 2^53 − 1 ≈ 9 × 10^15

import {
	ChevronDown,
	KanbanSquare,
	List,
	Loader2,
	Map as MapIcon,
	Plus,
	Puzzle,
	Search,
	X,
} from "lucide-react";
import type { ReactNode } from "react";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";

import { Button } from "@/components/ui/button";
import {
	Dialog,
	DialogContent,
	DialogDescription,
	DialogFooter,
	DialogHeader,
	DialogTitle,
} from "@/components/ui/dialog";
import {
	DropdownMenu,
	DropdownMenuContent,
	DropdownMenuItem,
	DropdownMenuSeparator,
	DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Skeleton } from "@/components/ui/skeleton";
import { useDebouncedCallback } from "@/hooks/use-debounced-callback";
import { isTaskNotFoundError } from "@/lib/api-error";
import {
	allTasksQueryOptions,
	bulkMoveViewTaskPositions,
	type CustomFieldFilterQuery,
	createSprint,
	createTask,
	createViewByContext,
	deleteTask,
	deleteViewById,
	epicTasksInfiniteQueryOptions,
	type FilterConfig,
	type InteractionView,
	type ListTasksOptions,
	layoutToViewType,
	listAllTasks,
	reorderViewsByContext,
	resolveFilterConfig,
	resolveTaskTypeFilter,
	sprintsQueryOptions,
	type Task,
	type TaskListResult,
	taskQueryOptions,
	updateMyViewConfig,
	updateSprint,
	updateTask,
	updateViewById,
	type ViewConfig,
	type ViewLayout,
	type ViewsContext,
	viewsByContextQueryOptions,
} from "@/lib/interaction-api";
import type { PluginRegistration } from "@/lib/plugin-api";
import { RemoteComponent } from "@/lib/plugins/loader";
import { usePluginBaseProps } from "@/lib/plugins/plugin-props";
import { usePluginRegistry } from "@/lib/plugins/registry";
import {
	customFieldsQueryOptions,
	findEpicType,
	projectMembersQueryOptions,
	projectQueryOptions,
	taskStatusesQueryOptions,
	taskTypesQueryOptions,
} from "@/lib/project-api";
import { formatChord, isMacPlatform } from "@/lib/shortcuts/keymap";
import { usePageShortcutStore } from "@/lib/shortcuts/page-shortcut-store";
import { cn } from "@/lib/utils";
import { BoardView } from "./board-view";
import { ListView } from "./list-view";
import { NewViewPopover } from "./new-view-popover";
import { getImportanceBucketBounds } from "./priority";
import { RenameViewDialog } from "./rename-view-dialog";
import { RoadmapView } from "./roadmap-view";
import { TaskDetailModal } from "./task-detail-modal";
import { UNASSIGNED_FILTER_ID, ViewSettingsPanel } from "./view-settings-panel";
import {
	type EpicsPagination,
	getColumnGroupDefs,
	getDefaultInitialPageSize,
	getDefaultPageSize,
	getTaskColumnKeys,
	keepPreviousDataOnPageSizeChangeOnly,
	resolveSelectedTask,
	shouldClearColumnExtras,
	type TaskFieldUpdate,
	type ViewContext,
} from "./view-utils";

// ── Loading skeletons ─────────────────────────────────────────────────────────

function ListViewSkeleton() {
	return (
		<div className="flex flex-col overflow-hidden h-full">
			{/* group header */}
			<div className="flex items-center gap-2 px-4 py-2.5 border-b border-border/30 bg-muted/20">
				<Skeleton className="size-4 rounded" />
				<Skeleton className="h-3.5 w-20" />
				<Skeleton className="h-4 w-6 rounded-full ml-1" />
			</div>
			{/* column header row */}
			<div className="flex items-center gap-4 px-4 py-2 border-b border-border/20 bg-muted/10">
				<Skeleton className="h-3 w-14 shrink-0" />
				<Skeleton className="h-3 flex-1" />
				<Skeleton className="h-3 w-16 shrink-0" />
				<Skeleton className="h-3 w-14 shrink-0" />
				<Skeleton className="h-3 w-20 shrink-0" />
				<Skeleton className="h-3 w-16 shrink-0" />
			</div>
			{/* task rows */}
			{[
				{ id: "sk-r1", w: "w-48", wp: "w-16", ws: "w-20" },
				{ id: "sk-r2", w: "w-64", wp: "w-14", ws: "w-24" },
				{ id: "sk-r3", w: "w-40", wp: "w-20", ws: "w-16" },
				{ id: "sk-r4", w: "w-56", wp: "w-12", ws: "w-20" },
				{ id: "sk-r5", w: "w-52", wp: "w-18", ws: "w-24" },
			].map(({ id, w, wp, ws }) => (
				<div
					key={id}
					className="flex items-center gap-4 px-4 py-3 border-b border-border/15 last:border-0"
				>
					<Skeleton className="size-5 rounded shrink-0" />
					<Skeleton className={`h-3.5 ${w} shrink-0`} />
					<div className="flex-1" />
					<Skeleton className={`h-3 ${wp} shrink-0`} />
					<Skeleton className={`h-3 ${ws} shrink-0`} />
					<Skeleton className="size-6 rounded-full shrink-0" />
				</div>
			))}
		</div>
	);
}

function BoardViewSkeleton() {
	const cols = [
		{
			id: "sk-col1",
			w: "w-24",
			cards: [
				{ id: "sk-c1r1", tw: "w-32", th: "h-3.5" },
				{ id: "sk-c1r2", tw: "w-40", th: "h-3" },
				{ id: "sk-c1r3", tw: "w-28", th: "h-4" },
			],
		},
		{
			id: "sk-col2",
			w: "w-20",
			cards: [
				{ id: "sk-c2r1", tw: "w-36", th: "h-3.5" },
				{ id: "sk-c2r2", tw: "w-24", th: "h-3" },
			],
		},
		{
			id: "sk-col3",
			w: "w-28",
			cards: [
				{ id: "sk-c3r1", tw: "w-28", th: "h-4" },
				{ id: "sk-c3r2", tw: "w-44", th: "h-3.5" },
				{ id: "sk-c3r3", tw: "w-32", th: "h-3" },
				{ id: "sk-c3r4", tw: "w-20", th: "h-3.5" },
			],
		},
		{
			id: "sk-col4",
			w: "w-16",
			cards: [{ id: "sk-c4r1", tw: "w-40", th: "h-3" }],
		},
	];
	return (
		<div className="flex h-full gap-3 overflow-x-auto px-4 py-4">
			{cols.map((col) => (
				<div key={col.id} className="flex w-64 shrink-0 flex-col gap-2">
					{/* column header */}
					<div className="flex items-center gap-2 px-1">
						<Skeleton className={`h-3.5 ${col.w}`} />
						<Skeleton className="h-4 w-5 rounded-full" />
					</div>
					{/* cards */}
					{col.cards.map(({ id, tw, th }) => (
						<div
							key={id}
							className="rounded-xl border border-border/50 bg-card p-3.5 space-y-3"
						>
							<div className="flex items-center gap-2">
								<Skeleton className="size-4 rounded shrink-0" />
								<Skeleton className={`${th} ${tw}`} />
							</div>
							<div className="flex items-center justify-between">
								<Skeleton className="h-4 w-16 rounded-full" />
								<Skeleton className="size-5 rounded-full" />
							</div>
						</div>
					))}
				</div>
			))}
		</div>
	);
}

interface InteractionLayoutProps {
	projectId: string;
	interactionKey: string;
	title: string;
	description?: string | null;
	canCreate: boolean;
	canEdit: boolean;
	canManageViews: boolean;
	onTaskClick?: (task: Task) => void;
	sprintId?: string | null;
	/** The view context — drives which API bucket is used for views */
	context: ViewsContext;
	/** Optional action buttons to show in the page header */
	headerActions?: ReactNode;
}

/**
 * Translates a column key + column_by field into API filter options.
 * Returns null when the column cannot be filtered server-side.
 */
function buildColumnFilter(
	colKey: string,
	columnBy: string,
	baseOpts: ListTasksOptions,
): ListTasksOptions | null {
	switch (columnBy) {
		case "status": {
			if (colKey === "__none") return null; // no-status not filterable server-side
			return { ...baseOpts, statusIds: [colKey], statusId: undefined };
		}
		case "sprint": {
			if (colKey === "__backlog") {
				return { ...baseOpts, sprintId: null, sprintIds: undefined };
			}
			return { ...baseOpts, sprintId: colKey, sprintIds: undefined };
		}
		case "assignee": {
			if (colKey === "__unassigned") {
				return {
					...baseOpts,
					assigneeNull: true,
					assigneeIds: undefined,
					assigneeId: undefined,
				};
			}
			return {
				...baseOpts,
				assigneeIds: [colKey],
				assigneeNull: false,
				assigneeId: undefined,
			};
		}
		case "type": {
			if (colKey === "__none") {
				return { ...baseOpts, taskTypeNull: true, taskTypeIds: undefined };
			}
			return { ...baseOpts, taskTypeIds: [colKey], taskTypeNull: false };
		}
		default:
			return null;
	}
}

/** Sets `?taskId=` on the current URL without triggering a route navigation. */
function setTaskIdSearchParam(taskId: string) {
	try {
		const url = new URL(window.location.href);
		url.searchParams.set("taskId", taskId);
		window.history.pushState({}, "", url.toString());
	} catch {
		/* ignore */
	}
}

/** Removes `?taskId=` from the current URL without triggering a route navigation. */
function clearTaskIdSearchParam() {
	try {
		const url = new URL(window.location.href);
		url.searchParams.delete("taskId");
		window.history.pushState({}, "", url.toString());
	} catch {
		/* ignore */
	}
}

export function InteractionLayout({
	projectId,
	interactionKey,
	title,
	description,
	canCreate,
	canEdit,
	canManageViews,
	onTaskClick,
	sprintId,
	context,
	headerActions,
}: InteractionLayoutProps) {
	const { t } = useTranslation("projects");
	const qc = useQueryClient();
	const navigate = useNavigate();

	const { data: project } = useQuery(projectQueryOptions(projectId));
	const taskIdPrefix = project?.task_id_prefix ?? "";

	const { data: statuses = [] } = useQuery(taskStatusesQueryOptions(projectId));
	const { data: taskTypes = [] } = useQuery(taskTypesQueryOptions(projectId));

	// Seed default task type IDs are only needed for initial view config seeding.
	// Timeline seeds with Epics only; all other contexts seed with non-system types.
	const defaultPageTaskTypeIds = useMemo(() => {
		const defaultTypes =
			context === "timeline"
				? taskTypes.filter((tt) => tt.is_system && tt.name === "Epic")
				: taskTypes.filter((tt) => !tt.is_system);
		return defaultTypes.map((tt) => tt.id);
	}, [taskTypes, context]);
	const buildDefaultViewConfig = useCallback(
		(layout: ViewLayout, baseConfig?: ViewConfig): ViewConfig | undefined => {
			const next: ViewConfig = { ...(baseConfig ?? {}) };
			if (!next.column_by) {
				if (sprintId) next.column_by = "status";
				else if (context !== "timeline" && layout === "Table")
					next.column_by = "sprint";
			}
			if (next.filters === undefined) {
				const filters: NonNullable<ViewConfig["filters"]> = {};
				if (context === "timeline") {
					// Timeline: show only explicit epic-type task types
					if (defaultPageTaskTypeIds.length > 0) {
						const items: Record<string, boolean> = {};
						for (const id of defaultPageTaskTypeIds) items[id] = true;
						const taskTypesConfig: FilterConfig = { all: false, items };
						filters.task_types = taskTypesConfig;
					}
				} else {
					// All other contexts: use the "normal" virtual group
					const taskTypesConfig: FilterConfig = {
						all: false,
						items: { normal: { all: true } },
					};
					filters.task_types = taskTypesConfig;
				}
				if (sprintId) {
					const sprintsConfig: FilterConfig = {
						all: false,
						items: { [sprintId]: true },
					};
					filters.sprints = sprintsConfig;
				}
				if (Object.keys(filters).length > 0) {
					next.filters = filters;
				}
			}
			return Object.keys(next).length > 0 ? next : undefined;
		},
		[defaultPageTaskTypeIds, context, sprintId],
	);
	const { data: customFields = [] } = useQuery(
		customFieldsQueryOptions(projectId),
	);

	const viewsQuery = useQuery(
		viewsByContextQueryOptions(projectId, context, sprintId),
	);

	const views = viewsQuery.data ?? [];

	const viewsQueryKey = viewsByContextQueryOptions(
		projectId,
		context,
		sprintId,
	).queryKey;

	const seedingRef = useRef(false);
	useEffect(() => {
		if (
			!viewsQuery.isSuccess ||
			views.length > 0 ||
			seedingRef.current ||
			taskTypes.length === 0
		)
			return;
		seedingRef.current = true;
		const seed =
			context === "sprint" && sprintId
				? Promise.all([
						createViewByContext(
							projectId,
							context,
							{
								name: "Board",
								view_type: "board",
								config: buildDefaultViewConfig("Board"),
							},
							sprintId,
						),
						createViewByContext(
							projectId,
							context,
							{
								name: "Table",
								view_type: "table",
								config: buildDefaultViewConfig("Table"),
							},
							sprintId,
						),
					])
				: context === "timeline"
					? createViewByContext(projectId, context, {
							name: "Roadmap",
							view_type: "roadmap",
							config: buildDefaultViewConfig("Roadmap"),
						})
					: createViewByContext(projectId, context, {
							name: "Table",
							view_type: "table",
							config: buildDefaultViewConfig("Table"),
						});
		seed
			.then(() => qc.invalidateQueries({ queryKey: viewsQueryKey }))
			.catch(console.error);
	}, [
		buildDefaultViewConfig,
		viewsQuery.isSuccess,
		views.length,
		taskTypes.length,
		sprintId,
		context,
		projectId,
		qc,
		viewsQueryKey,
	]);

	const initializedFiltersRef = useRef<Set<string>>(new Set());
	useEffect(() => {
		if (!viewsQuery.isSuccess || defaultPageTaskTypeIds.length === 0) return;
		const uninitializedViews = views.filter(
			(view) =>
				view.layout !== "Plugin" &&
				!initializedFiltersRef.current.has(view.id) &&
				view.config?.filters === undefined,
		);
		if (uninitializedViews.length === 0) return;
		for (const view of uninitializedViews) {
			initializedFiltersRef.current.add(view.id);
		}
		Promise.all(
			uninitializedViews.map((view) => {
				const config = buildDefaultViewConfig(view.layout, view.config);
				if (!config) return Promise.resolve(view);
				return updateViewById(projectId, view.id, { config });
			}),
		)
			.then(() => qc.invalidateQueries({ queryKey: viewsQueryKey }))
			.catch(console.error);
	}, [
		buildDefaultViewConfig,
		defaultPageTaskTypeIds.length,
		projectId,
		qc,
		views,
		viewsQuery.isSuccess,
		viewsQueryKey,
	]);

	const [previewConfig, setPreviewConfig] = useState<ViewConfig | undefined>(
		undefined,
	);
	const [preferredViewId, setPreferredViewId] = useState<string>(() => {
		try {
			return localStorage.getItem(`paca:active-view:${interactionKey}`) ?? "";
		} catch {
			return "";
		}
	});

	const activeView = views.find((v) => v.id === preferredViewId) ?? views[0];
	const activeViewId = activeView?.id ?? "";

	// Plugin view registrations (for the "Add view" popover layout options)
	const { getRegistrations } = usePluginRegistry();
	const pluginViewRegistrations = getRegistrations("view").filter(
		(r) => !r.hidden,
	);

	// If the active view is a plugin view, resolve its registration from config
	const activePluginView =
		activeView?.layout === "Plugin"
			? (pluginViewRegistrations.find(
					(r) =>
						r.pluginId === activeView.config?.plugin_manifest_id &&
						r.component === activeView.config?.plugin_component,
				) ?? null)
			: null;

	// BaseExtensionProps (api/ui/meta) for the active plugin view, if any —
	// every ViewExtensionProps component expects these alongside projectId.
	const activePluginViewBaseProps = usePluginBaseProps(
		activePluginView ?? undefined,
		projectId,
	);

	useEffect(() => {
		if (!activeViewId) return;
		try {
			localStorage.setItem(`paca:active-view:${interactionKey}`, activeViewId);
		} catch {
			/* ignore */
		}
	}, [activeViewId, interactionKey]);

	// biome-ignore lint/correctness/useExhaustiveDependencies: clear preview when switching views to prevent settings from bleeding across views
	useEffect(() => {
		setPreviewConfig(undefined);
	}, [activeViewId]);

	const [renameTarget, setRenameTarget] = useState<InteractionView | null>(
		null,
	);
	const [renameOpen, setRenameOpen] = useState(false);
	const [settingsOpen, setSettingsOpen] = useState(false);
	const activeViewConfig = previewConfig ?? activeView?.config;
	// Creatable task types follow the active view's filter; if no filter is set,
	// all non-system types are available.  This lets views on any page control
	// which types can be created without hard-coding page-level rules.
	const creatableTaskTypes = useMemo(() => {
		const filterConfig = activeViewConfig?.filters?.task_types;
		if (filterConfig) {
			const resolvedIds = resolveTaskTypeFilter(filterConfig, taskTypes);
			if (resolvedIds.length > 0) {
				return taskTypes.filter((tt) => resolvedIds.includes(tt.id));
			}
		}
		return taskTypes.filter((tt) => !tt.is_system);
	}, [taskTypes, activeViewConfig?.filters?.task_types]);
	const isManualSort =
		!activeViewConfig?.sort_by ||
		activeViewConfig?.sort_by?.toLowerCase() === "manual";
	const [searchQuery, setSearchQuery] = useState("");
	// Debounced so search doesn't fire a request on every keystroke now that
	// it's a server-side query (needed for correct pagination — see colBaseOpts).
	const [debouncedSearchQuery, setDebouncedSearchQuery] = useState("");
	const debouncedSetSearchQuery = useDebouncedCallback(
		(q: string) => setDebouncedSearchQuery(q),
		300,
	);
	const [searchOpen, setSearchOpen] = useState(false);
	const searchRef = useRef<HTMLInputElement>(null);
	const [selectedTaskId, setSelectedTaskId] = useState<string | null>(() => {
		try {
			return new URL(window.location.href).searchParams.get("taskId");
		} catch {
			return null;
		}
	});

	const { data: members = [] } = useQuery(
		projectMembersQueryOptions(projectId),
	);

	const { data: sprints = [] } = useQuery(sprintsQueryOptions(projectId));

	// Fetch Epic tasks for display in the epic field on task cards/rows.
	// Paginated 20-per-page (see epicTasksInfiniteQueryOptions) — every epic
	// picker app-wide (task detail, board/list-view cards & rows, the
	// right-click context menu) shares this single query and its
	// fetchNextPage/hasNextPage via the epicsPagination object below.
	const epicType = findEpicType(taskTypes);
	const {
		data: epicPages,
		fetchNextPage: fetchNextEpicsPage,
		hasNextPage: hasMoreEpics,
		isFetchingNextPage: isFetchingMoreEpics,
	} = useInfiniteQuery({
		...epicTasksInfiniteQueryOptions(projectId, epicType?.id ?? ""),
		enabled: !!epicType?.id,
	});
	const epicTasks = useMemo(
		() => epicPages?.pages.flatMap((page) => page.items) ?? [],
		[epicPages],
	);
	const epicsPagination: EpicsPagination = useMemo(
		() => ({
			hasMore: !!hasMoreEpics,
			isLoadingMore: isFetchingMoreEpics,
			onLoadMore: () => void fetchNextEpicsPage(),
		}),
		[hasMoreEpics, isFetchingMoreEpics, fetchNextEpicsPage],
	);

	const isRealView = !!activeViewId && !activeViewId.startsWith("__default-");
	const effectiveViewId = isManualSort && isRealView ? activeViewId : undefined;
	const hasExplicitFilterConfig = activeViewConfig?.filters !== undefined;
	const apiFilters = useMemo(() => {
		let assignee_ids: string[] | undefined;
		let assignee_null: true | undefined;
		if (activeViewConfig?.filters?.assignees) {
			const resolved = resolveFilterConfig(
				activeViewConfig.filters.assignees,
				members.map((m) => m.id),
			);
			const hasUnassigned = resolved.includes(UNASSIGNED_FILTER_ID);
			const memberIds = resolved.filter((id) => id !== UNASSIGNED_FILTER_ID);
			assignee_ids = memberIds.length > 0 ? memberIds : undefined;
			assignee_null = hasUnassigned || undefined;
		}

		let task_type_ids: string[] | undefined;
		if (!activeViewConfig?.filters) {
			task_type_ids = defaultPageTaskTypeIds;
		} else if (activeViewConfig.filters.task_types) {
			task_type_ids = resolveTaskTypeFilter(
				activeViewConfig.filters.task_types,
				taskTypes,
			);
		}

		let custom_field_filters:
			| Record<string, CustomFieldFilterQuery>
			| undefined;
		const cfFilterConfig = activeViewConfig?.filters?.custom_fields;
		if (cfFilterConfig) {
			const resolved: Record<string, CustomFieldFilterQuery> = {};
			for (const cf of customFields) {
				const f = cfFilterConfig[cf.field_key];
				if (!f) continue;
				if (cf.field_type === "select" || cf.field_type === "multi_select") {
					if (!f.selector) continue;
					const values = resolveFilterConfig(f.selector, cf.options);
					if (values.length > 0) resolved[cf.field_key] = { values };
				} else if (cf.field_type === "boolean") {
					if (!f.selector) continue;
					const values = resolveFilterConfig(f.selector, ["true", "false"]);
					if (values.length > 0) resolved[cf.field_key] = { values };
				} else if (cf.field_type === "number") {
					if (f.min == null && f.max == null) continue;
					resolved[cf.field_key] = { min: f.min, max: f.max };
				} else if (cf.field_type === "date") {
					if (!f.after && !f.before) continue;
					resolved[cf.field_key] = { after: f.after, before: f.before };
				} else {
					// text / url
					if (!f.contains) continue;
					resolved[cf.field_key] = { contains: f.contains };
				}
			}
			if (Object.keys(resolved).length > 0) custom_field_filters = resolved;
		}

		let importance_ranges: { min: number; max: number }[] | undefined;
		const importanceBuckets = activeViewConfig?.filters?.importance;
		if (importanceBuckets && importanceBuckets.length > 0) {
			importance_ranges = importanceBuckets.map((bucket) =>
				getImportanceBucketBounds(bucket),
			);
		}

		const tags =
			activeViewConfig?.filters?.tags &&
			activeViewConfig.filters.tags.length > 0
				? activeViewConfig.filters.tags
				: undefined;

		return {
			sprint_ids:
				activeViewConfig?.filters !== undefined
					? activeViewConfig.filters.sprints
						? resolveFilterConfig(
								activeViewConfig.filters.sprints,
								sprints.map((s) => s.id),
							)
						: undefined
					: sprintId
						? [sprintId]
						: undefined,
			status_ids: activeViewConfig?.filters?.statuses
				? resolveFilterConfig(
						activeViewConfig.filters.statuses,
						statuses.map((s) => s.id),
					)
				: undefined,
			assignee_ids,
			assignee_null,
			task_type_ids,
			custom_field_filters,
			start_date_after: activeViewConfig?.filters?.start_date?.after,
			start_date_before: activeViewConfig?.filters?.start_date?.before,
			due_date_after: activeViewConfig?.filters?.due_date?.after,
			due_date_before: activeViewConfig?.filters?.due_date?.before,
			story_points_min: activeViewConfig?.filters?.story_points?.min,
			story_points_max: activeViewConfig?.filters?.story_points?.max,
			importance_ranges,
			tags,
		};
	}, [
		activeViewConfig?.filters,
		customFields,
		defaultPageTaskTypeIds,
		members,
		sprints,
		sprintId,
		statuses,
		taskTypes,
	]);
	const viewCtx: ViewContext = useMemo(
		() => ({ statuses, taskTypes, members, customFields, sprints }),
		[statuses, taskTypes, members, customFields, sprints],
	);

	// ── Per-column pagination ─────────────────────────────────────────────────
	const columnBy = activeViewConfig?.column_by ?? "status";
	const isColumnBySupported =
		columnBy === "status" ||
		columnBy === "sprint" ||
		columnBy === "assignee" ||
		columnBy === "type";

	const fetchColumnDefs = useMemo(() => {
		if (!isColumnBySupported) return [];
		return getColumnGroupDefs(columnBy, viewCtx, t);
	}, [isColumnBySupported, columnBy, viewCtx, t]);

	// Guard: do not start column queries until views have finished loading.
	// Without this, queries fire before effectiveViewId is available, fetching
	// tasks without view_id and briefly rendering them in created_at order.
	const colQueriesEnabled =
		fetchColumnDefs.length > 0 &&
		!viewsQuery.isLoading &&
		activeView?.layout !== "Roadmap";

	// Initial page size: configured view setting wins; otherwise falls back to
	// the active layout's default (see PAGE_SIZE_DEFAULTS in view-utils.ts).
	// "Load more" batches use the separate configuredPageSize below.
	const configuredPageSize = activeViewConfig?.page_size;
	const configuredInitialPageSize = activeViewConfig?.initial_page_size;
	const initialColPageSize =
		configuredInitialPageSize ?? getDefaultInitialPageSize(activeView?.layout);

	// Base options for column queries (shared filters, excluding the dimension used for column grouping)
	const colBaseOpts = useMemo(
		(): ListTasksOptions => ({
			sprintId:
				context !== "timeline" && !hasExplicitFilterConfig
					? sprintId
					: undefined,
			sprintIds: apiFilters.sprint_ids,
			statusIds: columnBy !== "status" ? apiFilters.status_ids : undefined,
			assigneeIds:
				columnBy !== "assignee" ? apiFilters.assignee_ids : undefined,
			assigneeNull:
				columnBy !== "assignee" ? apiFilters.assignee_null : undefined,
			taskTypeIds: columnBy !== "type" ? apiFilters.task_type_ids : undefined,
			pageSize: initialColPageSize,
			sumField: activeViewConfig?.field_sum,
			sortBy: activeViewConfig?.sort_by,
			viewId: effectiveViewId,
			search: debouncedSearchQuery || undefined,
			customFieldFilters: apiFilters.custom_field_filters,
			startDateAfter: apiFilters.start_date_after,
			startDateBefore: apiFilters.start_date_before,
			dueDateAfter: apiFilters.due_date_after,
			dueDateBefore: apiFilters.due_date_before,
			storyPointsMin: apiFilters.story_points_min,
			storyPointsMax: apiFilters.story_points_max,
			importanceRanges: apiFilters.importance_ranges,
			tags: apiFilters.tags,
		}),
		[
			context,
			hasExplicitFilterConfig,
			sprintId,
			apiFilters,
			columnBy,
			initialColPageSize,
			activeViewConfig?.field_sum,
			activeViewConfig?.sort_by,
			effectiveViewId,
			debouncedSearchQuery,
		],
	);

	// Tracks per-column total visible count so WS refetches restore the same depth.
	const [colExpandedPageSizes, setColExpandedPageSizes] = useState<
		Record<string, number>
	>({});

	const columnQueries = useQueries({
		queries: colQueriesEnabled
			? fetchColumnDefs.map((col) => {
					const effectivePageSize =
						colExpandedPageSizes[col.key] ?? initialColPageSize;
					const colOpts = buildColumnFilter(col.key, columnBy, {
						...colBaseOpts,
						pageSize: effectivePageSize,
					});
					if (!colOpts) {
						return {
							queryKey: ["noop", col.key] as const,
							queryFn: () =>
								Promise.resolve({
									items: [] as Task[],
									page_size: 0,
									next_cursor: null,
								} as TaskListResult),
							enabled: false,
						};
					}
					return {
						queryKey: [
							"projects",
							projectId,
							"tasks",
							"col",
							col.key,
							colOpts,
						] as const,
						queryFn: () => listAllTasks(projectId, colOpts),
						staleTime: 15_000,
						// "Load more" grows this column's pageSize, which changes the
						// queryKey above — without this, React Query would drop `data`
						// back to undefined for the new key until it resolves, making
						// already-visible tasks disappear from the list mid-fetch.
						// Keeping the previous (smaller) page's data displayed avoids
						// that gap. Scoped to pageSize-only key changes so a genuine
						// filter/sort/search change still drops to `undefined` and
						// shows the loading skeleton, instead of silently rendering
						// the previous (non-matching) filter's results.
						placeholderData: keepPreviousDataOnPageSizeChangeOnly(colOpts),
					};
				})
			: [],
	});

	// Fallback single query for non-supported column_by (importance, custom fields) or roadmap
	// Tracks total items to show so WS-triggered refetches restore the same depth.
	const [globalExpandedPageSize, setGlobalExpandedPageSize] = useState<
		number | null
	>(null);

	// Filter-only opts (no pageSize) — reference changes only when filters change,
	// not when the expanded page size grows via "view more".
	const fallbackBaseOpts = useMemo(
		() => ({
			sprintId:
				context !== "timeline" && !hasExplicitFilterConfig
					? sprintId
					: undefined,
			sprintIds: apiFilters.sprint_ids,
			statusIds: apiFilters.status_ids,
			assigneeIds: apiFilters.assignee_ids,
			assigneeNull: apiFilters.assignee_null,
			taskTypeIds: apiFilters.task_type_ids,
			sortBy: activeViewConfig?.sort_by,
			viewId: effectiveViewId,
			search: debouncedSearchQuery || undefined,
			customFieldFilters: apiFilters.custom_field_filters,
			startDateAfter: apiFilters.start_date_after,
			startDateBefore: apiFilters.start_date_before,
			dueDateAfter: apiFilters.due_date_after,
			dueDateBefore: apiFilters.due_date_before,
			storyPointsMin: apiFilters.story_points_min,
			storyPointsMax: apiFilters.story_points_max,
			importanceRanges: apiFilters.importance_ranges,
			tags: apiFilters.tags,
		}),
		[
			context,
			hasExplicitFilterConfig,
			sprintId,
			apiFilters,
			activeViewConfig?.sort_by,
			effectiveViewId,
			debouncedSearchQuery,
		],
	);

	const initialGlobalPageSize =
		configuredInitialPageSize ?? getDefaultInitialPageSize(activeView?.layout);
	const fallbackOpts = {
		...fallbackBaseOpts,
		pageSize: globalExpandedPageSize ?? initialGlobalPageSize,
	};
	const fallbackQueryOpts = allTasksQueryOptions(projectId, fallbackOpts);
	const fallbackQuery = useQuery({
		...fallbackQueryOpts,
		enabled: !colQueriesEnabled && !viewsQuery.isLoading,
		// See the matching comment on the column queries above: "load more"
		// grows pageSize (and thus the queryKey), so keep showing the smaller
		// page's data while the larger one fetches instead of dropping to
		// undefined — scoped to pageSize-only key changes so a genuine
		// filter/sort/search change still shows the loading skeleton.
		placeholderData: keepPreviousDataOnPageSizeChangeOnly(fallbackOpts),
	});

	// Per-column load-more state
	const [colNextCursors, setColNextCursors] = useState<
		Record<string, string | null>
	>({});
	const [colExtraTasks, setColExtraTasks] = useState<Record<string, Task[]>>(
		{},
	);
	const [colLoadingMore, setColLoadingMore] = useState<Record<string, boolean>>(
		{},
	);

	// Sync next cursors from initial column query results; reset extras once
	// each column's own base query has re-fetched at its expanded depth.
	const colDataUpdatedKey = columnQueries.map((q) => q.dataUpdatedAt).join(",");
	// biome-ignore lint/correctness/useExhaustiveDependencies: re-sync only when column query data changes
	useEffect(() => {
		if (!colQueriesEnabled) return;
		const updated: Record<string, string | null> = {};
		fetchColumnDefs.forEach((col, idx) => {
			const data = columnQueries[idx]?.data;
			if (data) updated[col.key] = data.next_cursor ?? null;
		});
		setColNextCursors(updated);
		// "Load more" bumps colExpandedPageSizes[col.key], which changes that
		// column's query to fetch the same expanded depth directly — once it
		// resolves, the base data already covers what the extras held, so the
		// extras are redundant and can be dropped. Dropping extras for every
		// column on *any* column's refetch (e.g. a WS-triggered invalidation
		// of an unrelated column) used to make already-loaded tasks flash out
		// of the list — and out from under an open task detail dialog — until
		// that column's own expanded refetch caught up. Only drop a column's
		// extras once its own base data has actually reached that depth —
		// or, if the true total has shrunk below that depth (a loaded task
		// got deleted or moved out of the column/filter), once the base
		// query reports there's nothing left to fetch (`next_cursor: null`),
		// since that response is authoritative even though it's shorter than
		// expected. Without the second condition, a permanently-shrunk total
		// would never satisfy the length check, leaving deleted/moved tasks
		// stuck as unclearable ghosts in colExtraTasks indefinitely.
		setColExtraTasks((prev) => {
			let changed = false;
			const next = { ...prev };
			for (const [key, extras] of Object.entries(prev)) {
				if (extras.length === 0) continue;
				const idx = fetchColumnDefs.findIndex((col) => col.key === key);
				const data = idx >= 0 ? columnQueries[idx]?.data : undefined;
				const expectedDepth = colExpandedPageSizes[key] ?? initialColPageSize;
				if (shouldClearColumnExtras(data, expectedDepth)) {
					delete next[key];
					changed = true;
				}
			}
			return changed ? next : prev;
		});
	}, [colDataUpdatedKey, colQueriesEnabled]);

	// colBaseOpts is a useMemo, but an upstream dependency (e.g. apiFilters,
	// which depends on customFields/sprints — queries the task detail modal
	// re-observes on open, both without an explicit staleTime) can recompute
	// to a *new object* with the *same values* once that background refetch
	// resolves. Comparing colBaseOpts by reference treated that as "filters
	// changed" and wiped the "load more" depth back to the initial page size
	// — confirmed via a network capture showing the resulting request had
	// every filter field unchanged, only a reverted page_size. Compare by
	// value instead, so only a genuine filter/sort/search change resets it.
	const colBaseOptsKey = useMemo(
		() => JSON.stringify(colBaseOpts),
		[colBaseOpts],
	);
	// colBaseOptsKey is a value-equality fingerprint of colBaseOpts, kept in
	// the dep array purely to control *when* this effect re-runs even though
	// the body doesn't read it.
	// biome-ignore lint/correctness/useExhaustiveDependencies: reset only when the *value* of colBaseOpts changes
	useEffect(() => {
		setColExpandedPageSizes({});
	}, [colBaseOptsKey]);

	const handleLoadMoreColumn = useCallback(
		async (colKey: string) => {
			if (colLoadingMore[colKey]) return;
			const cursor = colNextCursors[colKey];
			if (!cursor) return;
			const colOpts = buildColumnFilter(colKey, columnBy, {
				...colBaseOpts,
				pageSize: configuredPageSize ?? getDefaultPageSize(activeView?.layout),
				cursor,
			});
			if (!colOpts) return;
			setColLoadingMore((prev) => ({ ...prev, [colKey]: true }));
			try {
				const result = await listAllTasks(projectId, colOpts);
				setColExtraTasks((prev) => ({
					...prev,
					[colKey]: [...(prev[colKey] ?? []), ...result.items],
				}));
				setColNextCursors((prev) => ({
					...prev,
					[colKey]: result.next_cursor ?? null,
				}));
				// Grow the effective page size so the next WS-triggered refetch
				// returns the same number of items currently visible.
				setColExpandedPageSizes((prev) => ({
					...prev,
					[colKey]: (prev[colKey] ?? initialColPageSize) + result.items.length,
				}));
			} finally {
				setColLoadingMore((prev) => ({ ...prev, [colKey]: false }));
			}
		},
		[
			colNextCursors,
			columnBy,
			colBaseOpts,
			projectId,
			colLoadingMore,
			initialColPageSize,
			configuredPageSize,
			activeView?.layout,
		],
	);

	// Global load-more (roadmap / non-column views)
	const [globalNextCursor, setGlobalNextCursor] = useState<string | null>(null);
	const [globalExtraTasks, setGlobalExtraTasks] = useState<Task[]>([]);
	const [globalLoadingMore, setGlobalLoadingMore] = useState(false);

	useEffect(() => {
		if (colQueriesEnabled) return;
		setGlobalNextCursor(fallbackQuery.data?.next_cursor ?? null);
		setGlobalExtraTasks([]);
	}, [colQueriesEnabled, fallbackQuery.data?.next_cursor]);

	// biome-ignore lint/correctness/useExhaustiveDependencies: reset only when filters/layout change
	useEffect(() => {
		setGlobalExpandedPageSize(null);
	}, [fallbackBaseOpts, initialGlobalPageSize]);

	const handleLoadMoreGlobal = useCallback(async () => {
		if (globalLoadingMore) return;
		if (!globalNextCursor) return;
		setGlobalLoadingMore(true);
		try {
			const result = await listAllTasks(projectId, {
				...fallbackBaseOpts,
				pageSize: configuredPageSize ?? getDefaultPageSize(activeView?.layout),
				cursor: globalNextCursor,
			});
			setGlobalExtraTasks((prev) => [...prev, ...result.items]);
			setGlobalNextCursor(result.next_cursor ?? null);
			// Grow the effective page size so the next WS-triggered refetch
			// returns the same number of items currently visible.
			setGlobalExpandedPageSize(
				(prev) => (prev ?? initialGlobalPageSize) + result.items.length,
			);
		} finally {
			setGlobalLoadingMore(false);
		}
	}, [
		globalNextCursor,
		projectId,
		fallbackBaseOpts,
		globalLoadingMore,
		initialGlobalPageSize,
		configuredPageSize,
		activeView?.layout,
	]);

	const tasks = useMemo(() => {
		if (colQueriesEnabled) {
			const base = columnQueries.flatMap((q) => q.data?.items ?? []);
			const extra = Object.values(colExtraTasks).flat();
			const seen = new Set<string>();
			return [...base, ...extra].filter((t) => {
				if (seen.has(t.id)) return false;
				seen.add(t.id);
				return true;
			});
		}
		// Deduplicate by task ID: when globalExpandedPageSize changes and triggers
		// a refetch, fallbackQuery.data.items and globalExtraTasks can momentarily
		// overlap, producing duplicate keys that corrupt React's DOM ordering.
		const combined = [
			...(fallbackQuery.data?.items ?? []),
			...globalExtraTasks,
		];
		const seen = new Set<string>();
		return combined.filter((t) => {
			if (seen.has(t.id)) return false;
			seen.add(t.id);
			return true;
		});
	}, [
		colQueriesEnabled,
		columnQueries,
		colExtraTasks,
		fallbackQuery.data,
		globalExtraTasks,
	]);

	// Epics referenced by tasks currently on screen but not yet among the
	// paginated epicTasks pages (the picker loads 20 at a time — a task's
	// epic may sort past that window even though it hasn't been "loaded
	// more" by anyone). Fetch those specific epics directly so the epic
	// badge on cards/rows resolves instead of silently rendering as unset.
	const loadedEpicIds = useMemo(
		() => new Set(epicTasks.map((e) => e.id)),
		[epicTasks],
	);
	const missingEpicIds = useMemo(() => {
		// Once every epic page has been loaded, any parent_task_id that's
		// still absent from loadedEpicIds is guaranteed to be a non-epic
		// parent (story/task nesting) rather than an unpaginated epic — skip
		// fetching it. This keeps the direct-fetch fallback scoped to the
		// (typically brief) window before pagination catches up, instead of
		// firing for every non-epic parent on every board/list render.
		if (!hasMoreEpics) return [];
		const ids = new Set<string>();
		for (const task of tasks) {
			if (task.parent_task_id && !loadedEpicIds.has(task.parent_task_id)) {
				ids.add(task.parent_task_id);
			}
		}
		return Array.from(ids);
	}, [tasks, loadedEpicIds, hasMoreEpics]);
	// `combine` gives the filtered result structural-sharing/memoization
	// across renders (plain useQueries() returns a new array every render,
	// which would otherwise defeat displayEpics' useMemo below).
	const missingEpics = useQueries({
		queries: missingEpicIds.map((epicId) => ({
			...taskQueryOptions(projectId, epicId),
			enabled: !!epicType?.id,
		})),
		combine: (results) =>
			// parent_task_id also links non-epic parents (story/task nesting), so
			// only fold fetched tasks in here when they're actually Epic-typed —
			// otherwise a task's non-epic parent could render as its "epic".
			results
				.map((r) => r.data)
				.filter(
					(task): task is Task => !!task && task.task_type_id === epicType?.id,
				),
	});
	const displayEpics = useMemo(
		() =>
			missingEpics.length > 0 ? [...epicTasks, ...missingEpics] : epicTasks,
		[epicTasks, missingEpics],
	);

	// A column's queryKey changes every time its page size grows ("load
	// more" bumps colExpandedPageSizes), which makes React Query treat it as
	// a brand-new query — `isLoading` (isPending && isFetching) is true for
	// that key until it resolves, even though `placeholderData` is already
	// showing the previous page's tasks. Driving the full-list skeleton off
	// `isLoading` therefore replaced the whole list with a skeleton on every
	// "load more" and every background refetch of an expanded column. Check
	// for the absence of data instead: with `keepPreviousData` in place that
	// only happens on a column's genuine first fetch (disabled/noop columns
	// are excluded via fetchStatus, since they'll never fetch).
	const tasksLoading =
		viewsQuery.isLoading ||
		(colQueriesEnabled
			? columnQueries.some(
					(q) => q.data === undefined && q.fetchStatus !== "idle",
				)
			: fallbackQuery.data === undefined &&
				fallbackQuery.fetchStatus !== "idle");

	// Per-column pagination props for views
	const columnPagination = useMemo(() => {
		if (!colQueriesEnabled)
			return {} as Record<
				string,
				{
					hasMore: boolean;
					isLoadingMore: boolean;
					onLoadMore: () => void;
					totalCount?: number;
					fieldSum?: number;
				}
			>;
		const result: Record<
			string,
			{
				hasMore: boolean;
				isLoadingMore: boolean;
				onLoadMore: () => void;
				totalCount?: number;
				fieldSum?: number;
			}
		> = {};
		for (let i = 0; i < fetchColumnDefs.length; i++) {
			const col = fetchColumnDefs[i];
			const apiFieldSum = columnQueries[i]?.data?.field_sum;
			result[col.key] = {
				hasMore: Boolean(colNextCursors[col.key]),
				isLoadingMore: Boolean(colLoadingMore[col.key]),
				onLoadMore: () => handleLoadMoreColumn(col.key),
				totalCount: columnQueries[i]?.data?.total_count,
				fieldSum: apiFieldSum != null ? apiFieldSum : undefined,
			};
		}
		return result;
	}, [
		colQueriesEnabled,
		fetchColumnDefs,
		colNextCursors,
		colLoadingMore,
		handleLoadMoreColumn,
		columnQueries,
	]);

	const globalPagination = useMemo(
		() => ({
			hasMore: Boolean(globalNextCursor),
			isLoadingMore: globalLoadingMore,
			onLoadMore: handleLoadMoreGlobal,
		}),
		[globalNextCursor, globalLoadingMore, handleLoadMoreGlobal],
	);

	const tasksListQueryKey = useMemo(
		() => ["projects", projectId, "tasks"],
		[projectId],
	);

	// `tasks` is the paginated/grouped list currently loaded for the active
	// view, so it can transiently stop containing the selected task even
	// while the detail modal is open — e.g. the per-column "load more" extras
	// are cleared on every background refetch (see the colExtraTasks reset
	// effect below) before the expanded page is re-fetched. If `selectedTask`
	// tracked `tasks` directly, that gap would flip `open` to false and the
	// modal would flash closed. Cache the last resolved task per id so the
	// modal stays open (backed by its own fresh-task query) across such gaps,
	// and only clears when the selection itself changes.
	const lastSelectedTaskRef = useRef<Task | null>(null);
	// A task opened via a direct `?taskId=` URL (deep link, bookmark, share)
	// may not be among the currently loaded/filtered `tasks` at all — e.g. the
	// backlog view only paginates a page at a time, or the task doesn't match
	// the active view's filters. Fetch it by id directly so the dialog can
	// still open; this shares its query key with the mutations below, so it
	// stays in sync with edits made elsewhere. Only enabled when the task
	// isn't already resolvable from `tasks` — the common case of clicking a
	// task that's already loaded needs no extra request.
	const selectedTaskInList =
		!!selectedTaskId && tasks.some((t) => t.id === selectedTaskId);
	const selectedTaskDirectQuery = useQuery({
		...taskQueryOptions(projectId, selectedTaskId ?? ""),
		enabled: !!selectedTaskId && !selectedTaskInList,
		// A confirmed 404 is never worth retrying — fail fast so the `?taskId=`
		// cleanup below runs promptly. Any other error (network, 5xx) keeps the
		// default bounded retry, since those are transient and a valid deep
		// link shouldn't be abandoned after a single blip.
		retry: (failureCount, error) =>
			!isTaskNotFoundError(error) && failureCount < 3,
	});
	const selectedTask = useMemo(() => {
		const { resolved, nextLastKnown } = resolveSelectedTask(
			tasks,
			selectedTaskId,
			lastSelectedTaskRef.current,
		);
		const directFetched =
			selectedTaskId && selectedTaskDirectQuery.data?.id === selectedTaskId
				? selectedTaskDirectQuery.data
				: null;
		const final = resolved ?? directFetched;
		lastSelectedTaskRef.current = final ?? nextLastKnown;
		return final;
	}, [selectedTaskId, tasks, selectedTaskDirectQuery.data]);

	// A selected id that's absent from the loaded list and the API has
	// authoritatively confirmed (404 TASK_NOT_FOUND) doesn't exist — deleted,
	// moved to another project, or a mistyped id — can never resolve to a
	// task. Without this, a broken `?taskId=` deep link leaves the param in
	// the URL forever with no dialog and no feedback. Deliberately scoped to
	// TASK_NOT_FOUND rather than any query error: a transient network/5xx
	// failure says nothing about whether the task exists, so it must not
	// wipe out an otherwise-valid deep link.
	const selectedTaskConfirmedMissing =
		selectedTaskDirectQuery.isError &&
		isTaskNotFoundError(selectedTaskDirectQuery.error);
	useEffect(() => {
		if (!selectedTaskId || !selectedTaskConfirmedMissing) return;
		if (tasks.some((t) => t.id === selectedTaskId)) return;
		setSelectedTaskId(null);
		clearTaskIdSearchParam();
	}, [selectedTaskId, selectedTaskConfirmedMissing, tasks]);

	const handleTaskClick = (task: Task) => {
		setSelectedTaskId(task.id);
		onTaskClick?.(task);
		setTaskIdSearchParam(task.id);
	};

	const updateStatusMutation = useMutation({
		mutationFn: ({
			taskId,
			statusId,
			taskSprintId,
		}: {
			taskId: string;
			statusId: string;
			taskSprintId: string | null | undefined;
		}) =>
			updateTask(projectId, taskId, {
				status_id: statusId,
				sprint_id: taskSprintId ?? null,
			}),
		onSuccess: () => {
			qc.invalidateQueries({ queryKey: tasksListQueryKey });
			// Status changes affect a sprint's incomplete-task count (surfaced in
			// the Complete Sprint dialog's move-to-sprint suggestion).
			qc.invalidateQueries({ queryKey: ["projects", projectId, "sprints"] });
		},
	});

	const handleStatusChange = useCallback(
		(taskId: string, newStatusId: string) => {
			const task = tasks.find((t) => t.id === taskId);
			updateStatusMutation.mutate({
				taskId,
				statusId: newStatusId,
				taskSprintId: task?.sprint_id,
			});
		},
		[updateStatusMutation, tasks],
	);

	const createTaskMutation = useMutation({
		mutationFn: async (payload: {
			title: string;
			statusId: string;
			taskTypeId?: string | null;
			extraFields?: TaskFieldUpdate;
		}) => {
			// sprint_id: prefer explicit extraFields.sprint_id, else fall back to route sprint param
			const sprintIdForTask =
				payload.extraFields?.sprint_id !== undefined
					? payload.extraFields.sprint_id
					: (sprintId ?? null);
			const task = await createTask(projectId, {
				title: payload.title,
				status_id: payload.statusId || undefined,
				sprint_id: sprintIdForTask,
				task_type_id: payload.taskTypeId ?? null,
			});
			// Apply remaining extraFields (excluding sprint_id which was handled above)
			const { sprint_id: _sid, ...remainingFields } = payload.extraFields ?? {};
			if (Object.keys(remainingFields).length > 0) {
				return updateTask(projectId, task.id, remainingFields);
			}
			return task;
		},
		onSuccess: () => {
			qc.invalidateQueries({ queryKey: tasksListQueryKey });
			// A newly created (non-done) task changes its sprint's incomplete-task
			// count (surfaced in the Complete Sprint dialog's move-to-sprint
			// suggestion), so refresh the sprint-scoped queries too.
			qc.invalidateQueries({ queryKey: ["projects", projectId, "sprints"] });
		},
	});

	const handleCreateTask = async (
		statusId: string,
		title: string,
		taskTypeId?: string | null,
		extraFields?: TaskFieldUpdate,
	) => {
		// Fall back to the first available creatable type when none is specified.
		// The creatableTaskTypes list is already filtered by the active view config,
		// so this naturally handles Epic-only views (e.g. Timeline).
		const effectiveTaskTypeId = taskTypeId ?? creatableTaskTypes[0]?.id ?? null;
		await createTaskMutation.mutateAsync({
			title,
			statusId,
			taskTypeId: effectiveTaskTypeId,
			extraFields,
		});
	};

	const handleReorderTask = useCallback(
		(groupKey: string, taskId: string, newIndex: number) => {
			if (!effectiveViewId) return;
			const groupTasks = tasks.filter((t) =>
				getTaskColumnKeys(t, columnBy, viewCtx).includes(groupKey),
			);
			const srcIdx = groupTasks.findIndex((t) => t.id === taskId);
			const reordered = [...groupTasks];
			if (srcIdx !== -1) {
				const [removed] = reordered.splice(srcIdx, 1);
				reordered.splice(newIndex, 0, removed);
			}

			// ── Virtual positions for unpositioned tasks ───────────────────────
			// Null-positioned tasks are ordered by created_at at the bottom of the
			// sorted list.  To compute correct midpoints when the drag lands next
			// to one of them, we assign each a virtual position that evenly fills
			// the range (lastPositionedValue, POSITION_MAX).  The virtual positions
			// are ordered by the tasks' slots in `reordered` (= their created_at
			// order, since only `taskId` was moved).
			const nullNonMoved = reordered.filter(
				(t) => t.view_position == null && t.id !== taskId,
			);
			const lastExplicit = reordered
				.filter((t) => t.view_position != null)
				.reduce((max, t) => Math.max(max, t.view_position as number), 0);
			const virtualPosMap = new Map<string, number>();
			nullNonMoved.forEach((t, i) => {
				virtualPosMap.set(
					t.id,
					lastExplicit +
						((POSITION_MAX - lastExplicit) * (i + 1)) /
							(nullNonMoved.length + 1),
				);
			});
			const effectivePos = (t: Task): number =>
				t.view_position ?? virtualPosMap.get(t.id) ?? POSITION_MAX / 2;

			// ── Compute new position using bounded midpoint rules ──────────────
			const prevTask = reordered[newIndex - 1];
			const nextTask = reordered[newIndex + 1];
			const prev = prevTask ? effectivePos(prevTask) : null;
			const next = nextTask ? effectivePos(nextTask) : null;

			let position: number;
			if (prev !== null && next !== null) {
				// Midpoint between neighbours — stays inside (prev, next).
				position = (prev + next) / 2;
			} else if (prev !== null) {
				// Append: midpoint toward ceiling — always < POSITION_MAX.
				position = (prev + POSITION_MAX) / 2;
			} else if (next !== null) {
				// Prepend: midpoint toward zero — always > 0.
				position = next / 2;
			} else {
				// Sole task in an all-null group — centre of the full range.
				position = POSITION_MAX / 2;
			}

			// ── Build update list ──────────────────────────────────────────────
			// If the drag landed next to at least one null-positioned task, also
			// materialise all null tasks so their DB positions match the order the
			// user established (otherwise they revert to created_at on re-render).
			const updates: Array<{ id: string; pos: number }> = [
				{ id: taskId, pos: position },
			];
			const hasNullNeighbour =
				(prevTask?.view_position == null && prevTask?.id !== taskId) ||
				(nextTask?.view_position == null && nextTask?.id !== taskId);
			if (hasNullNeighbour) {
				for (const [id, pos] of virtualPosMap.entries()) {
					updates.push({ id, pos });
				}
			}

			const bulkItems = updates.map((u) => ({
				task_id: u.id,
				position: u.pos,
				group_key: groupKey,
			}));
			bulkMoveViewTaskPositions(projectId, effectiveViewId, bulkItems)
				.then(() => qc.invalidateQueries({ queryKey: tasksListQueryKey }))
				.catch(console.error);
		},
		[
			effectiveViewId,
			tasks,
			projectId,
			qc,
			columnBy,
			viewCtx,
			tasksListQueryKey,
		],
	);

	const handleMoveToColumn = useCallback(
		(taskId: string, update: TaskFieldUpdate) => {
			updateTask(projectId, taskId, update)
				.then((updatedTask) => {
					// Write the server response directly into the per-task cache so the
					// detail modal immediately shows the updated value without a separate fetch.
					qc.setQueryData(
						["projects", projectId, "tasks", taskId],
						updatedTask,
					);
					qc.invalidateQueries({ queryKey: tasksListQueryKey });
					// Moving a task between status/sprint columns affects a sprint's
					// incomplete-task count (surfaced in the Complete Sprint dialog's
					// move-to-sprint suggestion), so refresh the sprint-scoped queries too.
					return qc.invalidateQueries({
						queryKey: ["projects", projectId, "sprints"],
					});
				})
				.catch(console.error);
		},
		[projectId, qc, tasksListQueryKey],
	);

	// ── Delete task (Mod+Backspace shortcut / context menu) ─────────────────
	const [deleteConfirmTask, setDeleteConfirmTask] = useState<Task | null>(null);
	const deleteTaskMutation = useMutation({
		mutationFn: (taskId: string) => deleteTask(projectId, taskId),
		onSuccess: (_, taskId) => {
			qc.invalidateQueries({ queryKey: tasksListQueryKey });
			// Deleting a task changes its sprint's incomplete-task count (surfaced
			// in the Complete Sprint dialog's move-to-sprint suggestion).
			qc.invalidateQueries({ queryKey: ["projects", projectId, "sprints"] });
			setDeleteConfirmTask(null);
			if (selectedTaskId === taskId) setSelectedTaskId(null);
		},
	});
	const handleRequestDeleteTask = useCallback(
		(taskId: string) => {
			const task = tasks.find((t) => t.id === taskId);
			if (task) setDeleteConfirmTask(task);
		},
		[tasks],
	);

	const createViewMutation = useMutation({
		mutationFn: (payload: {
			name: string;
			layout: ViewLayout;
			pluginRegistration?: PluginRegistration;
		}) => {
			const view_type = layoutToViewType(payload.layout);
			const config =
				payload.layout === "Plugin" && payload.pluginRegistration
					? {
							plugin_manifest_id: payload.pluginRegistration.pluginId,
							plugin_component: payload.pluginRegistration.component,
						}
					: buildDefaultViewConfig(payload.layout);
			return createViewByContext(
				projectId,
				context,
				{ name: payload.name, view_type, config },
				sprintId,
			);
		},
		onSuccess: (view) => {
			qc.invalidateQueries({ queryKey: viewsQueryKey });
			setPreferredViewId(view.id);
		},
	});

	const renameViewMutation = useMutation({
		mutationFn: (payload: { viewId: string; name: string }) =>
			updateViewById(projectId, payload.viewId, { name: payload.name }),
		onSuccess: () => qc.invalidateQueries({ queryKey: viewsQueryKey }),
	});

	// View settings & filters are PER-USER: persist to the current user's
	// personal override (updateMyViewConfig) instead of the shared view row, so
	// one member's sort/filter/field choices never change what others see.
	const updateViewConfigMutation = useMutation({
		mutationFn: (payload: { viewId: string; config: ViewConfig }) =>
			updateMyViewConfig(projectId, payload.viewId, payload.config),
		onSuccess: () => {
			setPreviewConfig(undefined);
			qc.invalidateQueries({ queryKey: viewsQueryKey });
		},
	});

	const deleteViewMutation = useMutation({
		mutationFn: (viewId: string) => deleteViewById(projectId, viewId),
		onSuccess: (_, deletedId) => {
			qc.invalidateQueries({ queryKey: viewsQueryKey });
			if (preferredViewId === deletedId) {
				const remaining = views.filter((v) => v.id !== deletedId);
				setPreferredViewId(remaining[0]?.id ?? "");
			}
		},
	});

	const reorderViewMutation = useMutation({
		mutationFn: (orderedIds: string[]) =>
			reorderViewsByContext(projectId, context, orderedIds, sprintId),
		onSuccess: () => qc.invalidateQueries({ queryKey: viewsQueryKey }),
	});

	const [tabDragId, setTabDragId] = useState<string | null>(null);
	const [tabDragOverId, setTabDragOverId] = useState<string | null>(null);
	const [localViews, setLocalViews] = useState<InteractionView[] | null>(null);

	// biome-ignore lint/correctness/useExhaustiveDependencies: intentionally reset local order when server views refresh
	useEffect(() => {
		if (!tabDragId) setLocalViews(null);
	}, [views]);

	const displayViews = localViews ?? views;

	const handleTabDrop = (targetId: string, draggedId: string) => {
		if (!draggedId || draggedId === targetId) return;
		const current = localViews ?? views;
		const srcIdx = current.findIndex((v) => v.id === draggedId);
		const tgtIdx = current.findIndex((v) => v.id === targetId);
		if (srcIdx === -1 || tgtIdx === -1) return;
		const next = [...current];
		const [moved] = next.splice(srcIdx, 1);
		next.splice(tgtIdx, 0, moved);
		const withPositions = next.map((v, i) => ({ ...v, position: i }));
		setLocalViews(withPositions);
		reorderViewMutation.mutate(withPositions.map((v) => v.id));
	};

	// ── Sprint management (backlog only) ────────────────────────────────────
	const createSprintMutation = useMutation({
		mutationFn: (name: string) =>
			createSprint(projectId, { name, status: "planned" }),
		onSuccess: () =>
			qc.invalidateQueries({ queryKey: ["projects", projectId, "sprints"] }),
	});

	const handleNewSprint = () => {
		const nextNum = sprints.length + 1;
		createSprintMutation.mutate(`Sprint ${nextNum}`);
	};

	const updateSprintMutation = useMutation({
		mutationFn: ({
			sprintId: sid,
			payload,
		}: {
			sprintId: string;
			payload: Parameters<typeof updateSprint>[2];
		}) => updateSprint(projectId, sid, payload),
		onSuccess: () => {
			qc.invalidateQueries({ queryKey: ["projects", projectId, "sprints"] });
		},
	});

	// ── Keyboard shortcuts (page scope) ─────────────────────────────────────
	const viewContentRef = useRef<HTMLDivElement>(null);

	useEffect(() => {
		usePageShortcutStore.getState().setActive({
			prevView: () => {
				if (displayViews.length < 2) return;
				const idx = displayViews.findIndex((v) => v.id === activeView?.id);
				const prevIdx = idx <= 0 ? displayViews.length - 1 : idx - 1;
				setPreferredViewId(displayViews[prevIdx].id);
			},
			nextView: () => {
				if (displayViews.length < 2) return;
				const idx = displayViews.findIndex((v) => v.id === activeView?.id);
				const nextIdx =
					idx === -1 || idx === displayViews.length - 1 ? 0 : idx + 1;
				setPreferredViewId(displayViews[nextIdx].id);
			},
			focusSearch: () => {
				setSearchOpen(true);
				setTimeout(() => searchRef.current?.focus(), 0);
			},
			toggleViewSettings: () => {
				if (!activeView || activeView.layout === "Plugin") return;
				setSettingsOpen((prev) => !prev);
			},
			focusCreateTask: () => {
				viewContentRef.current
					?.querySelector<HTMLButtonElement>(
						'[data-shortcut="add-task-trigger"]',
					)
					?.click();
			},
		});
		return () => usePageShortcutStore.getState().clearActive();
	}, [displayViews, activeView]);

	return (
		<div className="flex h-full flex-col overflow-hidden">
			{/* Header */}
			<div className="shrink-0 border-b border-border/30 px-8 py-5">
				<div className="flex items-center gap-3">
					<h1 className="font-[Syne] text-2xl font-bold tracking-tight flex-1">
						{title}
					</h1>
					{headerActions}
					{context === "backlog" && canCreate && (
						<button
							type="button"
							onClick={handleNewSprint}
							disabled={createSprintMutation.isPending}
							className="flex items-center gap-1.5 rounded-lg border border-dashed border-border/60 bg-muted/10 px-3 py-1.5 text-xs font-semibold text-muted-foreground hover:border-primary/50 hover:bg-primary/5 hover:text-primary transition-all duration-150 disabled:opacity-50"
						>
							<Plus className="size-3.5 shrink-0" />
							{t("layout.shell.newSprint")}
						</button>
					)}
				</div>
				{description && (
					<p className="mt-1 text-sm text-muted-foreground">{description}</p>
				)}
			</div>

			{/* View tab bar */}
			<div className="flex shrink-0 items-center gap-1 border-b border-border/25 bg-muted/20 px-4">
				<div className="flex items-center gap-0.5 overflow-x-auto overflow-y-hidden flex-1 min-w-0">
					{displayViews.map((view) => {
						const isActive = view.id === activeView?.id;
						const isDragOver =
							tabDragOverId === view.id && tabDragId !== view.id;
						return (
							// biome-ignore lint/a11y/noStaticElementInteractions: draggable tab; pointer events only
							<div
								key={view.id}
								draggable={canManageViews}
								className={cn(
									"relative flex items-center shrink-0 transition-all duration-100",
									isActive && "border-b-2 border-primary -mb-px",
									isDragOver && "border-l-2 border-primary/60",
									tabDragId === view.id && "opacity-40",
									canManageViews && "cursor-grab active:cursor-grabbing",
								)}
								onDragStart={(e) => {
									setTabDragId(view.id);
									e.dataTransfer.effectAllowed = "move";
									e.dataTransfer.setData("text/plain", view.id);
								}}
								onDragEnd={() => {
									setTabDragId(null);
									setTabDragOverId(null);
								}}
								onDragOver={(e) => {
									if (!canManageViews) return;
									e.preventDefault();
									e.dataTransfer.dropEffect = "move";
									setTabDragOverId(view.id);
								}}
								onDragLeave={() => {
									if (tabDragOverId === view.id) setTabDragOverId(null);
								}}
								onDrop={(e) => {
									e.preventDefault();
									const draggedId = e.dataTransfer.getData("text/plain");
									setTabDragId(null);
									setTabDragOverId(null);
									handleTabDrop(view.id, draggedId);
								}}
							>
								<button
									type="button"
									onClick={() => {
										setPreferredViewId(view.id);
									}}
									className={cn(
										"flex items-center gap-1.5 px-2.5 py-2.5 text-xs font-medium transition-all duration-150",
										isActive
											? "text-primary"
											: "text-muted-foreground/80 hover:text-foreground",
									)}
								>
									{view.layout === "Board" ? (
										<KanbanSquare className="size-3.5" />
									) : view.layout === "Roadmap" ? (
										<MapIcon className="size-3.5" />
									) : view.layout === "Plugin" ? (
										<Puzzle className="size-3.5" />
									) : (
										<List className="size-3.5" />
									)}
									{view.name}
								</button>

								{isActive && (
									<DropdownMenu>
										<DropdownMenuTrigger
											render={
												<button
													type="button"
													className="flex size-6 items-center justify-center rounded-md text-muted-foreground/60 hover:text-foreground hover:bg-muted/60 transition-all duration-150"
												/>
											}
										>
											<ChevronDown className="size-3" />
										</DropdownMenuTrigger>
										<DropdownMenuContent align="start" sideOffset={4}>
											<DropdownMenuItem
												onClick={() => {
													setRenameTarget(view);
													setRenameOpen(true);
												}}
											>
												{t("layout.shell.renameView")}
											</DropdownMenuItem>
											<DropdownMenuSeparator />
											<DropdownMenuItem
												disabled={views.length <= 1}
												onClick={() => deleteViewMutation.mutate(view.id)}
												className="text-destructive focus:text-destructive"
											>
												{t("layout.shell.deleteView")}
											</DropdownMenuItem>
										</DropdownMenuContent>
									</DropdownMenu>
								)}
							</div>
						);
					})}

					{canManageViews && (
						<NewViewPopover
							onSubmit={(name, layout, pluginRegistration) =>
								createViewMutation.mutateAsync({
									name,
									layout,
									pluginRegistration,
								})
							}
							isPending={createViewMutation.isPending}
							pluginRegistrations={pluginViewRegistrations}
						/>
					)}
				</div>

				<div className="flex shrink-0 items-center gap-1 pl-3 border-l border-border/25 ml-2">
					{searchOpen ? (
						<div className="flex items-center gap-1.5 rounded-lg border border-border/30 bg-muted/15 px-3 py-1.5 focus-within:border-primary/40 focus-within:ring-2 focus-within:ring-primary/15 transition-all duration-150">
							<Search className="size-3.5 text-muted-foreground/60 shrink-0" />
							<input
								ref={searchRef}
								value={searchQuery}
								onChange={(e) => {
									setSearchQuery(e.target.value);
									debouncedSetSearchQuery(e.target.value);
								}}
								placeholder={t("layout.shell.searchTasksPlaceholder")}
								className="w-36 bg-transparent text-xs font-medium outline-none placeholder:text-muted-foreground/50"
								onKeyDown={(e) => {
									if (e.key === "Escape") {
										setSearchOpen(false);
										setSearchQuery("");
										setDebouncedSearchQuery("");
									}
								}}
							/>
							<button
								type="button"
								onClick={() => {
									setSearchOpen(false);
									setSearchQuery("");
									setDebouncedSearchQuery("");
								}}
								className="flex size-5 items-center justify-center rounded-md text-muted-foreground/60 hover:text-foreground transition-all duration-150"
							>
								<X className="size-3" />
							</button>
						</div>
					) : (
						<button
							type="button"
							onClick={() => setSearchOpen(true)}
							title={`${t("layout.shell.searchTasksPlaceholder")} (${formatChord({ mod: true, key: "F" }, isMacPlatform())})`}
							className="flex size-7 items-center justify-center rounded-md text-muted-foreground/60 hover:text-foreground hover:bg-muted/60 transition-all duration-150"
						>
							<Search className="size-3.5" />
						</button>
					)}

					{activeView && activeView.layout !== "Plugin" && (
						<ViewSettingsPanel
							projectId={projectId}
							view={activeView}
							open={settingsOpen}
							onOpenChange={setSettingsOpen}
							onSave={(viewId, config) =>
								updateViewConfigMutation.mutateAsync({ viewId, config })
							}
							onPreview={setPreviewConfig}
							isPending={updateViewConfigMutation.isPending}
						/>
					)}
				</div>
			</div>

			{/* View content */}
			<div
				ref={viewContentRef}
				className="flex flex-1 flex-col overflow-hidden"
			>
				{activePluginView && activeView ? (
					<RemoteComponent
						registration={activePluginView}
						componentProps={{
							...activePluginViewBaseProps,
							projectId,
							viewId: activeView.id,
							tasks: tasks,
							statuses,
							taskTypes,
							members,
							canCreate,
							canEdit,
							searchQuery,
							onTaskClick: handleTaskClick,
						}}
					/>
				) : activeView?.layout === "Plugin" ? (
					<div className="flex flex-1 items-center justify-center text-muted-foreground text-sm">
						{t("layout.shell.pluginNotAvailable")}
					</div>
				) : tasksLoading ? (
					activeView?.layout === "Board" ? (
						<BoardViewSkeleton />
					) : (
						<ListViewSkeleton />
					)
				) : activeView?.layout === "Board" ? (
					<BoardView
						projectId={projectId}
						taskIdPrefix={taskIdPrefix}
						tasks={tasks}
						statuses={statuses}
						taskTypes={creatableTaskTypes}
						members={members}
						customFields={customFields}
						sprints={sprints}
						epics={displayEpics}
						epicsPagination={epicsPagination}
						viewConfig={activeViewConfig}
						canCreate={canCreate}
						canEdit={canEdit}
						tasksQueryKey={tasksListQueryKey}
						columnPagination={columnPagination}
						onCreateTask={handleCreateTask}
						onTaskClick={handleTaskClick}
						onUpdateTask={canEdit ? handleMoveToColumn : undefined}
						onMoveToColumn={canEdit ? handleMoveToColumn : undefined}
						onDeleteTask={canEdit ? handleRequestDeleteTask : undefined}
						manualSort={isManualSort}
						onReorderTask={effectiveViewId ? handleReorderTask : undefined}
						onCollapseChange={
							isRealView && activeView
								? (columns) =>
										updateViewConfigMutation.mutate({
											viewId: activeView.id,
											config: {
												...(activeView.config ?? {}),
												collapsed_columns:
													columns.length > 0 ? columns : undefined,
											},
										})
								: undefined
						}
					/>
				) : activeView?.layout === "Roadmap" ? (
					<RoadmapView
						tasks={tasks}
						statuses={statuses}
						taskTypes={creatableTaskTypes}
						members={members}
						sprints={sprints}
						customFields={customFields}
						columnBy={columnBy}
						canCreate={canCreate}
						pagination={globalPagination}
						onCreateTask={handleCreateTask}
						onTaskClick={handleTaskClick}
					/>
				) : (
					<ListView
						projectId={projectId}
						tasks={tasks}
						taskIdPrefix={taskIdPrefix}
						statuses={statuses}
						taskTypes={creatableTaskTypes}
						members={members}
						customFields={customFields}
						epics={displayEpics}
						epicsPagination={epicsPagination}
						viewConfig={activeViewConfig}
						canCreate={canCreate}
						columnPagination={columnPagination}
						onCreateTask={handleCreateTask}
						onTaskClick={handleTaskClick}
						manualSort={isManualSort}
						onReorderTask={effectiveViewId ? handleReorderTask : undefined}
						onStatusChange={canEdit ? handleStatusChange : undefined}
						canEdit={canEdit}
						sortBy={activeViewConfig?.sort_by}
						onUpdateTaskField={canEdit ? handleMoveToColumn : undefined}
						onDeleteTask={canEdit ? handleRequestDeleteTask : undefined}
						sprints={context === "backlog" ? sprints : undefined}
						onStartSprint={
							context === "backlog" && canCreate
								? async (sid, payload) => {
										await updateSprintMutation.mutateAsync({
											sprintId: sid,
											payload,
										});
										navigate({
											to: "/projects/$projectId/interactions/sprints/$sprintId",
											params: { projectId, sprintId: sid },
										});
									}
								: undefined
						}
						onCreateSprint={
							context === "backlog" && canCreate ? handleNewSprint : undefined
						}
						onCollapseChange={
							isRealView && activeView
								? (columns) =>
										updateViewConfigMutation.mutate({
											viewId: activeView.id,
											config: {
												...(activeView.config ?? {}),
												collapsed_columns:
													columns.length > 0 ? columns : undefined,
											},
										})
								: undefined
						}
					/>
				)}
			</div>

			<RenameViewDialog
				view={renameTarget}
				open={renameOpen}
				onOpenChange={(v) => {
					setRenameOpen(v);
					if (!v) setRenameTarget(null);
				}}
				onSubmit={(viewId, name) =>
					renameViewMutation.mutateAsync({ viewId, name })
				}
				isPending={renameViewMutation.isPending}
			/>

			<TaskDetailModal
				task={selectedTask}
				open={!!selectedTask}
				onOpenChange={(v) => {
					if (!v) {
						setSelectedTaskId(null);
						clearTaskIdSearchParam();
					}
				}}
				projectId={projectId}
				statuses={statuses}
				taskTypes={taskTypes}
				members={members}
				canEdit={canEdit}
			/>

			<Dialog
				open={!!deleteConfirmTask}
				onOpenChange={(v) => {
					if (!v) setDeleteConfirmTask(null);
				}}
			>
				<DialogContent className="max-w-sm">
					<DialogHeader>
						<DialogTitle>
							{t("taskDetail.header.deleteDialog.title")}
						</DialogTitle>
						<DialogDescription>
							{t("taskDetail.header.deleteDialog.description", {
								title: deleteConfirmTask?.title ?? "",
							})}
						</DialogDescription>
					</DialogHeader>
					<DialogFooter>
						<Button
							variant="outline"
							onClick={() => setDeleteConfirmTask(null)}
							disabled={deleteTaskMutation.isPending}
						>
							{t("taskDetail.header.deleteDialog.cancel")}
						</Button>
						<Button
							variant="destructive"
							onClick={() =>
								deleteConfirmTask &&
								deleteTaskMutation.mutate(deleteConfirmTask.id)
							}
							disabled={deleteTaskMutation.isPending}
						>
							{deleteTaskMutation.isPending ? (
								<Loader2 className="size-4 animate-spin" />
							) : (
								t("taskDetail.header.deleteDialog.delete")
							)}
						</Button>
					</DialogFooter>
				</DialogContent>
			</Dialog>
		</div>
	);
}
