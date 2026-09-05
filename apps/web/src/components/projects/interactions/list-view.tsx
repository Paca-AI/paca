import { useMemo } from "react";
import { useTranslation } from "react-i18next";

import type { Sprint, Task, ViewConfig } from "@/lib/interaction-api";
import type {
	CustomFieldDefinition,
	ProjectMember,
	TaskStatus,
	TaskType,
} from "@/lib/project-api";

import { ListGroup } from "./list-group";
import type { SprintFormPayload } from "./sprint-form-modal";
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
	onStartSprint?: (
		sprintId: string,
		payload: SprintFormPayload,
	) => Promise<void>;
	onCreateSprint?: () => void;
	/** Opens the delete-confirmation dialog — Mod+Backspace shortcut. */
	onDeleteTask?: (taskId: string) => void;
	onCollapseChange?: (collapsedColumns: string[]) => void;
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
	onStartSprint,
	onCreateSprint,
	onDeleteTask,
	onCollapseChange,
	columnPagination,
}: ListViewProps) {
	const { t } = useTranslation("projects");
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
		() => ({ statuses, taskTypes, members, customFields, sprints }),
		[statuses, taskTypes, members, customFields, sprints],
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
		</div>
	);
}
