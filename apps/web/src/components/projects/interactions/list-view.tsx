import { useMemo } from "react";
import { useTranslation } from "react-i18next";
import { LoadMoreSentinel } from "@/components/shared/load-more-sentinel";

import type {
	ChildTaskFilters,
	Sprint,
	Task,
	ViewConfig,
} from "@/lib/interaction-api";
import type {
	CustomFieldDefinition,
	ProjectMember,
	TaskStatus,
	TaskType,
} from "@/lib/project-api";

import { ListGroup } from "./list-group";
import type { SprintFormPayload } from "./sprint-form-modal";
import {
	resolveSubtaskMode,
	useSubtaskExpansion,
} from "./use-subtask-expansion";
import {
	applyStatusFilterToColumnDefs,
	type ColumnGroupDef,
	DEFAULT_VISIBLE_FIELDS,
	type EpicsPagination,
	getColumnGroupDefs,
	getSwimlaneDefs,
	getTaskColumnKeys,
	type TaskFieldUpdate,
} from "./view-utils";

// ── Props ─────────────────────────────────────────────────────────────────────

export interface ListViewProps {
	projectId: string;
	tasks: Task[];
	taskIdPrefix?: string;
	statuses: TaskStatus[];
	taskTypes: TaskType[];
	members?: ProjectMember[];
	reporters?: ProjectMember[];
	/** The columns the API lists for a column_by whose columns come from the
	 *  data — see ViewContext.dynamicColumns. */
	dynamicColumns?: { key: string; label: string }[];
	customFields?: CustomFieldDefinition[];
	epics?: Task[];
	/** Load-more state for the epic picker, shared by every group/row/card. */
	epicsPagination?: EpicsPagination;
	viewConfig?: ViewConfig;
	canCreate: boolean;
	onCreateTask: (
		statusId: string,
		title: string,
		taskTypeId?: string | null,
		extraFields?: TaskFieldUpdate,
	) => Promise<void>;
	onTaskClick: (task: Task) => void;
	manualSort?: boolean;
	onReorderTask?: (groupKey: string, taskId: string, newIndex: number) => void;
	onStatusChange?: (taskId: string, newStatusId: string) => void;
	canEdit?: boolean;
	sortBy?: string;
	onUpdateTaskField?: (taskId: string, update: TaskFieldUpdate) => void;
	sprints?: Sprint[];
	/** Load-more state for the sprint groups when columns are by sprint. */
	sprintsPagination?: {
		hasMore: boolean;
		isLoadingMore: boolean;
		isError?: boolean;
		onLoadMore: () => void;
	};
	onStartSprint?: (
		sprintId: string,
		payload: SprintFormPayload,
	) => Promise<void>;
	onCreateSprint?: () => void;
	/** Opens the delete-confirmation dialog — Mod+Backspace shortcut. */
	onDeleteTask?: (taskId: string) => void;
	onCollapseChange?: (collapsedColumns: string[]) => void;
	/** The view's filters/sort, applied to subtasks listed under a parent. */
	childFilters?: ChildTaskFilters;
	/** True while any filter or search narrows the list. */
	filtersActive?: boolean;
	/** Identifies the view for remembering which rows are expanded. */
	viewKey?: string;
	columnPagination?: Record<
		string,
		{
			hasMore: boolean;
			isLoadingMore: boolean;
			onLoadMore: () => void;
			totalCount?: number;
			fieldSum?: number;
		}
	>;
}

// ── Component ─────────────────────────────────────────────────────────────────

export function ListView({
	projectId,
	tasks,
	taskIdPrefix = "",
	statuses,
	taskTypes,
	members = [],
	reporters,
	dynamicColumns,
	customFields = [],
	epics = [],
	epicsPagination,
	viewConfig,
	canCreate,
	onCreateTask,
	onTaskClick,
	manualSort,
	onReorderTask,
	onStatusChange,
	canEdit,
	sortBy,
	onUpdateTaskField,
	sprints,
	sprintsPagination,
	onStartSprint,
	onCreateSprint,
	onDeleteTask,
	onCollapseChange,
	childFilters,
	filtersActive = false,
	viewKey = "default",
	columnPagination,
}: ListViewProps) {
	const { t } = useTranslation("projects");
	const subtaskMode = resolveSubtaskMode(viewConfig);
	const subtaskExpansion = useSubtaskExpansion(
		projectId,
		viewKey,
		subtaskMode === "expanded",
	);
	const nestingEnabled = subtaskMode !== "hidden";
	const nesting = useMemo(
		() =>
			nestingEnabled
				? {
						isExpanded: subtaskExpansion.isExpanded,
						onToggle: subtaskExpansion.toggle,
						filters: childFilters ?? {},
						filtersActive,
					}
				: undefined,
		[
			nestingEnabled,
			subtaskExpansion.isExpanded,
			subtaskExpansion.toggle,
			childFilters,
			filtersActive,
		],
	);
	const columnBy = viewConfig?.column_by ?? "status";
	const swimlaneBy = viewConfig?.swimlanes;
	const fieldSum = viewConfig?.field_sum;
	const visibleFields: string[] =
		viewConfig?.fields && viewConfig.fields.length > 0
			? viewConfig.fields
			: DEFAULT_VISIBLE_FIELDS;
	const isStatusGrouping =
		!viewConfig?.column_by || viewConfig.column_by === "status";

	const viewCtx = useMemo(
		() => ({
			statuses,
			taskTypes,
			members,
			reporters,
			customFields,
			sprints,
			dynamicColumns,
		}),
		[
			statuses,
			taskTypes,
			members,
			reporters,
			customFields,
			sprints,
			dynamicColumns,
		],
	);

	const groupDefs = useMemo(
		() => getColumnGroupDefs(columnBy, viewCtx, t),
		[columnBy, viewCtx, t],
	);

	const effectiveGroupDefs = useMemo((): ColumnGroupDef[] => {
		let defs: ColumnGroupDef[];
		if (groupDefs.length > 0) {
			defs = groupDefs;
		} else {
			const seen = new Set<string>();
			const dynamic: ColumnGroupDef[] = [];
			for (const tk of tasks) {
				for (const k of getTaskColumnKeys(tk, columnBy, viewCtx)) {
					if (!seen.has(k)) {
						seen.add(k);
						dynamic.push({
							key: k,
							label: k === "__none" ? t("board.common.none") : k,
							fieldValue: k,
						});
					}
				}
			}
			if (!seen.has("__none")) {
				dynamic.push({
					key: "__none",
					label: t("board.common.none"),
					fieldValue: null,
				});
			}
			defs = dynamic;
		}

		return applyStatusFilterToColumnDefs(
			defs,
			isStatusGrouping,
			viewConfig?.filters?.statuses,
			statuses,
		);
	}, [
		groupDefs,
		tasks,
		columnBy,
		viewCtx,
		isStatusGrouping,
		viewConfig?.filters?.statuses,
		statuses,
		t,
	]);

	const swimlaneDefs = useMemo(
		() => getSwimlaneDefs(swimlaneBy, viewCtx, t),
		[swimlaneBy, viewCtx, t],
	);

	const getGroupTasks = (groupKey: string): Task[] =>
		tasks.filter((t) =>
			getTaskColumnKeys(t, columnBy, viewCtx).includes(groupKey),
		);

	const savedCollapsedColumns = viewConfig?.collapsed_columns;

	const handleGroupCollapseChange = (
		groupKey: string,
		isCollapsed: boolean,
	) => {
		// When no saved preference exists yet, seed from the current visual state
		// (done-category groups are auto-collapsed by isDone). This ensures the
		// first save captures the full actual state rather than an empty baseline.
		const current: string[] =
			savedCollapsedColumns !== undefined
				? savedCollapsedColumns
				: effectiveGroupDefs
						.filter((grp) => {
							const s = isStatusGrouping
								? statuses.find((st) => st.id === grp.key)
								: undefined;
							return s?.category === "done";
						})
						.map((grp) => grp.key);

		const next = isCollapsed
			? [...new Set([...current, groupKey])]
			: current.filter((k) => k !== groupKey);
		onCollapseChange?.(next);
	};

	return (
		<div className="flex flex-col overflow-auto">
			{effectiveGroupDefs.map((grp, idx) => {
				const groupTasks = getGroupTasks(grp.key);
				const status = isStatusGrouping
					? statuses.find((s) => s.id === grp.key)
					: undefined;
				const isDone = status?.category === "done";
				const defaultCollapsed =
					savedCollapsedColumns !== undefined
						? savedCollapsedColumns.includes(grp.key)
						: isDone;

				return (
					<ListGroup
						key={grp.key}
						groupDef={grp}
						prevGroupDef={idx > 0 ? effectiveGroupDefs[idx - 1] : undefined}
						nextGroupDef={
							idx < effectiveGroupDefs.length - 1
								? effectiveGroupDefs[idx + 1]
								: undefined
						}
						allGroupDefs={effectiveGroupDefs}
						onDeleteTask={onDeleteTask}
						projectId={projectId}
						tasks={groupTasks}
						statuses={statuses}
						taskTypes={taskTypes}
						members={members}
						reporters={reporters}
						customFields={customFields}
						epics={epics}
						epicsPagination={epicsPagination}
						canCreate={canCreate}
						defaultCollapsed={defaultCollapsed}
						fieldSum={fieldSum}
						swimlaneDefs={swimlaneDefs}
						swimlaneBy={swimlaneBy}
						onCreateTask={onCreateTask}
						onTaskClick={onTaskClick}
						manualSort={manualSort}
						onReorderTask={onReorderTask}
						onStatusChange={onStatusChange}
						canEdit={canEdit}
						isStatusGrouping={isStatusGrouping}
						sortBy={sortBy}
						onUpdateTaskField={onUpdateTaskField}
						visibleFields={visibleFields}
						taskIdPrefix={taskIdPrefix}
						sprint={sprints?.find((s) => s.id === grp.key)}
						otherActiveSprint={
							sprints?.find((s) => s.status === "active" && s.id !== grp.key) ??
							null
						}
						onStartSprint={onStartSprint}
						onCreateSprint={onCreateSprint}
						columnBy={columnBy}
						onCollapseChange={
							onCollapseChange
								? (isCollapsed) =>
										handleGroupCollapseChange(grp.key, isCollapsed)
								: undefined
						}
						nesting={nesting}
						groupPagination={columnPagination?.[grp.key]}
						totalCount={columnPagination?.[grp.key]?.totalCount}
						apiFieldSum={columnPagination?.[grp.key]?.fieldSum}
						extraCreateFields={
							!isStatusGrouping && columnBy === "sprint"
								? {
										sprint_id:
											grp.key === "__backlog" ? null : (grp.key as string),
									}
								: undefined
						}
					/>
				);
			})}
			{columnBy === "sprint" && sprintsPagination && (
				<LoadMoreSentinel {...sprintsPagination} />
			)}
		</div>
	);
}
