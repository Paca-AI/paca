import { ChevronDown, ChevronRight, Play, Plus } from "lucide-react";
import { useEffect, useState } from "react";

import type { Sprint, Task } from "@/lib/interaction-api";
import type {
	CustomFieldDefinition,
	ProjectMember,
	TaskStatus,
	TaskType,
} from "@/lib/project-api";
import { cn } from "@/lib/utils";

import { AddTaskRow } from "./add-task-row";
import { StartSprintModal } from "./start-sprint-modal";
import { getRowColConfig, TaskRow } from "./task-row";
import {
	buildColumnDropUpdate,
	type ColumnGroupDef,
	getTaskSwimlaneKey,
	type TaskFieldUpdate,
} from "./view-utils";

// ── Props ────────────────────────────────────────────────────────────────────

export interface ListGroupProps {
	groupDef: ColumnGroupDef;
	tasks: Task[];
	statuses: TaskStatus[];
	taskTypes: TaskType[];
	members: ProjectMember[];
	customFields: CustomFieldDefinition[];
	epics?: Task[];
	canCreate: boolean;
	defaultCollapsed?: boolean;
	fieldSum?: string;
	swimlaneDefs: ColumnGroupDef[];
	swimlaneBy: string | undefined;
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
	isStatusGrouping: boolean;
	sortBy?: string;
	onUpdateTaskField?: (taskId: string, update: TaskFieldUpdate) => void;
	visibleFields: string[];
	taskIdPrefix?: string;
	/** Sprint data when column_by === "sprint" */
	sprint?: Sprint;
	onStartSprint?: (
		sprintId: string,
		payload: {
			name: string;
			goal: string | null;
			start_date: string | null;
			end_date: string | null;
			status: "active";
		},
	) => Promise<void>;
	onCreateSprint?: () => void;
	onTaskDoubleClick?: (task: Task) => void;
	/** Extra fields merged into the create-task payload (e.g. sprint_id) */
	extraCreateFields?: TaskFieldUpdate;
	columnBy?: string;
	onCollapseChange?: (collapsed: boolean) => void;
	groupPagination?: {
		hasMore: boolean;
		isLoadingMore: boolean;
		onLoadMore: () => void;
	};
	/** Total task count from the API, used in place of tasks.length in the header badge */
	totalCount?: number;
	/** Field sum from the API, used in place of the front-end computed sum in the header badge */
	apiFieldSum?: number;
}

// ── Component ────────────────────────────────────────────────────────────────

export function ListGroup({
	groupDef,
	tasks,
	statuses,
	taskTypes,
	members,
	customFields,
	epics = [],
	canCreate,
	defaultCollapsed,
	fieldSum,
	swimlaneDefs,
	swimlaneBy,
	onCreateTask,
	onTaskClick,
	manualSort,
	onReorderTask,
	onStatusChange,
	canEdit,
	isStatusGrouping,
	onUpdateTaskField,
	visibleFields,
	taskIdPrefix = "",
	sprint,
	onStartSprint,
	onCreateSprint,
		onTaskDoubleClick,
	extraCreateFields,
	columnBy,
	onCollapseChange,
	groupPagination,
	totalCount,
	apiFieldSum,
}: ListGroupProps) {
	const [collapsed, setCollapsed] = useState(defaultCollapsed ?? false);
	const [draggingId, setDraggingId] = useState<string | null>(null);
	const [dragOverId, setDragOverId] = useState<string | null>(null);
	const [dragOverSwimKey, setDragOverSwimKey] = useState<string | null>(null);
	const [isDropTarget, setIsDropTarget] = useState(false);
	const [orderedTasks, setOrderedTasks] = useState<Task[]>(tasks);
	const [startSprintOpen, setStartSprintOpen] = useState(false);

	useEffect(() => {
		setOrderedTasks(tasks);
	}, [tasks]);

	const isDraggable = !!(canEdit || manualSort);
	const getViewCtxTasks = () => orderedTasks;

	// Builds onCreateTask arguments for either grouping mode
	const handleAdd = (title: string, typeId: string | null) => {
		if (isStatusGrouping) {
			return onCreateTask(groupDef.key as string, title, typeId);
		}
		const defaultStatus =
			statuses.find((s) => s.category !== "done") ?? statuses[0];
		return onCreateTask(
			defaultStatus?.id ?? "",
			title,
			typeId,
			extraCreateFields,
		);
	};

	// ── Drag helpers ─────────────────────────────────────────────────────────

	const handleIntraGroupDrop = (
		e: React.DragEvent,
		targetTask: Task,
		targetIndex: number,
	) => {
		e.preventDefault();
		e.stopPropagation();
		const taskId = e.dataTransfer.getData("text/plain");
		const sourceGroupKey = e.dataTransfer.getData(
			"application/x-source-group-key",
		);

		if (sourceGroupKey && sourceGroupKey !== groupDef.key) {
			if (canEdit) {
				if (isStatusGrouping) {
					onStatusChange?.(taskId, groupDef.key as string);
				} else {
					const colUpdate = buildColumnDropUpdate(
						columnBy,
						groupDef.fieldValue,
						customFields,
					);
					if (Object.keys(colUpdate).length > 0)
						onUpdateTaskField?.(taskId, colUpdate);
				}
			}
			setDraggingId(null);
			setDragOverId(null);
			setIsDropTarget(false);
			return;
		}

		if (!manualSort) {
			setDraggingId(null);
			setDragOverId(null);
			setIsDropTarget(false);
			return;
		}

		const currentDraggingId = draggingId;
		if (!currentDraggingId || currentDraggingId === targetTask.id) return;
		const sourceIndex = orderedTasks.findIndex(
			(t) => t.id === currentDraggingId,
		);
		if (sourceIndex === -1) return;
		// After removing source, indices shift by -1 for elements past it.
		// Adjust so the item lands BEFORE the visual drop target (top-border indicator).
		const adjustedTarget =
			sourceIndex < targetIndex ? targetIndex - 1 : targetIndex;
		const updated = [...orderedTasks];
		const [moved] = updated.splice(sourceIndex, 1);
		updated.splice(adjustedTarget, 0, moved);
		setOrderedTasks(updated);
		onReorderTask?.(groupDef.key, currentDraggingId, adjustedTarget);
		setDraggingId(null);
		setDragOverId(null);
	};

	const handleGroupDragOver = (e: React.DragEvent) => {
		if (!isDraggable) return;
		e.preventDefault();
		e.dataTransfer.dropEffect = "move";
		setIsDropTarget(true);
	};

	const handleGroupDrop = (e: React.DragEvent) => {
		e.preventDefault();
		const taskId = e.dataTransfer.getData("text/plain");
		const sourceGroupKey = e.dataTransfer.getData(
			"application/x-source-group-key",
		);
		setIsDropTarget(false);
		setDraggingId(null);
		setDragOverId(null);
		if (!taskId || !canEdit) return;
		if (sourceGroupKey && sourceGroupKey !== groupDef.key) {
			if (isStatusGrouping) {
				onStatusChange?.(taskId, groupDef.key as string);
			} else {
				const colUpdate = buildColumnDropUpdate(
					columnBy,
					groupDef.fieldValue,
					customFields,
				);
				if (Object.keys(colUpdate).length > 0)
					onUpdateTaskField?.(taskId, colUpdate);
			}
		}
	};

	const hasSwimlanes = swimlaneBy && swimlaneBy !== "none";

	// ── Column-header row ─────────────────────────────────────────────────────

	const columnHeaders = (
		<div className="flex items-center gap-3 px-4 py-1.5 bg-muted/20 border-b border-border/25">
			{isDraggable && <div className="w-3 shrink-0" />}
			<div className="w-20 shrink-0 text-xs font-bold uppercase tracking-[0.08em] text-muted-foreground/60">
				ID
			</div>
			<div className="flex-1 text-xs font-bold uppercase tracking-[0.08em] text-muted-foreground/60">
				Title
			</div>
			{visibleFields.map((fk) => {
				const col = getRowColConfig(fk, customFields);
				return (
					<div
						key={fk}
						className={cn(
							col.className,
							"text-xs font-bold uppercase tracking-[0.08em] text-muted-foreground/60",
							col.responsive ? "hidden sm:block" : "",
						)}
					>
						{col.headerLabel}
					</div>
				);
			})}
		</div>
	);

	// ── Task row renderer ─────────────────────────────────────────────────────

	const renderTaskRow = (
		task: Task,
		index: number,
		groupKey: string,
		swimKey?: string,
	) => (
		// biome-ignore lint/a11y/noStaticElementInteractions: drag-and-drop row slot
		<div
			key={task.id}
			className={cn(
				"relative",
				manualSort &&
					dragOverId === task.id &&
					draggingId !== task.id &&
					"border-t-2 border-primary/60",
			)}
			draggable={isDraggable}
			onDragStart={(e) => {
				e.dataTransfer.effectAllowed = "move";
				e.dataTransfer.setData("text/plain", task.id);
				e.dataTransfer.setData("application/x-paca-task-id", task.id);
				e.dataTransfer.setData("application/x-source-group-key", groupKey);
				if (swimKey) {
					e.dataTransfer.setData("application/x-source-swim-key", swimKey);
				}
				setDraggingId(task.id);
			}}
			onDragEnd={() => {
				setDraggingId(null);
				setDragOverId(null);
				setIsDropTarget(false);
			}}
			onDragOver={(e) => {
				e.preventDefault();
				if (manualSort) setDragOverId(task.id);
			}}
			onDrop={
				swimKey
					? (e) => {
							e.preventDefault();
							e.stopPropagation();
							const taskId = e.dataTransfer.getData("text/plain");
							const sourceGroupKey = e.dataTransfer.getData(
								"application/x-source-group-key",
							);
							const sourceSwimKey = e.dataTransfer.getData(
								"application/x-source-swim-key",
							);
							setDragOverSwimKey(null);
							// Cross-band drop
							if (
								swimKey !== "__all" &&
								sourceSwimKey &&
								sourceSwimKey !== swimKey
							) {
								setDraggingId(null);
								setDragOverId(null);
								if (canEdit) {
									const swimUpdate = buildColumnDropUpdate(
										swimlaneBy,
										swimlaneDefs.find((s) => s.key === swimKey)?.fieldValue,
										customFields,
									);
									if (Object.keys(swimUpdate).length > 0)
										onUpdateTaskField?.(taskId, swimUpdate);
								}
								return;
							}
							if (sourceGroupKey && sourceGroupKey !== groupDef.key) {
								if (canEdit) {
									if (isStatusGrouping) {
										onStatusChange?.(taskId, groupDef.key as string);
									} else {
										const colUpdate = buildColumnDropUpdate(
											columnBy,
											groupDef.fieldValue,
											customFields,
										);
										if (Object.keys(colUpdate).length > 0)
											onUpdateTaskField?.(taskId, colUpdate);
									}
								}
								setDraggingId(null);
								setDragOverId(null);
								setIsDropTarget(false);
								return;
							}
							if (!manualSort) {
								setDraggingId(null);
								setDragOverId(null);
								setIsDropTarget(false);
								return;
							}
							const currentDraggingId = draggingId;
							if (!currentDraggingId || currentDraggingId === task.id) return;
							const sourceOrderedIndex = orderedTasks.findIndex(
								(t) => t.id === currentDraggingId,
							);
							const targetOrderedIndex = orderedTasks.findIndex(
								(t) => t.id === task.id,
							);
							if (sourceOrderedIndex === -1 || targetOrderedIndex === -1)
								return;
							const adjustedTarget =
								sourceOrderedIndex < targetOrderedIndex
									? targetOrderedIndex - 1
									: targetOrderedIndex;
							const updated = [...orderedTasks];
							const [moved] = updated.splice(sourceOrderedIndex, 1);
							updated.splice(adjustedTarget, 0, moved);
							setOrderedTasks(updated);
							onReorderTask?.(groupDef.key, currentDraggingId, adjustedTarget);
							setDraggingId(null);
							setDragOverId(null);
						}
					: (e) => handleIntraGroupDrop(e, task, index)
			}
		>
			<TaskRow
				task={task}
				taskIdPrefix={taskIdPrefix}
				statuses={statuses}
				taskTypes={taskTypes}
				members={members}
				epics={epics}
				customFields={customFields}
				visibleFields={visibleFields}
				onClick={() => onTaskClick(task)}
				showDragHandle={isDraggable}
				isDragging={draggingId === task.id}
				canEdit={canEdit}
				onUpdateTaskField={onUpdateTaskField}
				onDoubleClick={onTaskDoubleClick ? () => onTaskDoubleClick(task) : undefined}
			/>
		</div>
	);

	const showAddTask =
		canCreate &&
		groupDef.key !== "__none" &&
		(isStatusGrouping || !!extraCreateFields);

	const viewMoreButton = groupPagination?.hasMore ? (
		<button
			type="button"
			onClick={groupPagination.onLoadMore}
			disabled={groupPagination.isLoadingMore}
			className="flex w-full items-center justify-center border-t border-border/10 py-2 text-xs font-medium text-muted-foreground/60 hover:text-primary hover:bg-primary/5 transition-all duration-150 disabled:opacity-50"
		>
			{groupPagination.isLoadingMore ? "Loading…" : "View more"}
		</button>
	) : null;

	// ── Render ────────────────────────────────────────────────────────────────

	return (
		// biome-ignore lint/a11y/noStaticElementInteractions: drag-and-drop group
		<div
			className={cn(
				"border-b border-border/25 last:border-0 transition-all duration-150",
				isDropTarget && "bg-primary/5 ring-inset ring-2 ring-primary/20",
			)}
			onDragOver={handleGroupDragOver}
			onDragLeave={() => setIsDropTarget(false)}
			onDrop={handleGroupDrop}
		>
			{/* Group header */}
			{/* biome-ignore lint/a11y/useSemanticElements: div contains child buttons and cannot be converted to button element */}
			<div
				onClick={() => {
					const next = !collapsed;
					setCollapsed(next);
					onCollapseChange?.(next);
				}}
				onKeyDown={(e) => {
					if (e.key === "Enter" || e.key === " ") {
						const next = !collapsed;
						setCollapsed(next);
						onCollapseChange?.(next);
					}
				}}
				role="button"
				tabIndex={0}
				className="flex w-full items-center gap-2.5 px-4 py-3 hover:bg-muted/30 transition-colors duration-150 cursor-pointer"
			>
				{collapsed ? (
					<ChevronRight className="size-3.5 text-muted-foreground/60 shrink-0" />
				) : (
					<ChevronDown className="size-3.5 text-muted-foreground/60 shrink-0" />
				)}
				{groupDef.color && (
					<span
						className="size-1.75 rounded-full shrink-0"
						style={{
							background: groupDef.color,
							boxShadow: `0 0 6px ${groupDef.color}40`,
						}}
					/>
				)}
				<span className="text-xs font-bold uppercase tracking-[0.08em] text-foreground/80 flex-1 text-left truncate">
					{groupDef.label}
				</span>

				{/* Sprint: "Start sprint" button */}
				{sprint && sprint.status === "planned" && onStartSprint && (
					<button
						type="button"
						onClick={(e) => {
							e.stopPropagation();
							setStartSprintOpen(true);
						}}
						className="flex items-center gap-1.5 rounded-md bg-emerald-500 px-2.5 py-1 text-xs font-semibold text-white shadow-sm hover:bg-emerald-600 active:scale-95 transition-all duration-150 shrink-0"
					>
						<Play className="size-3 fill-white" />
						Start sprint
					</button>
				)}

				{/* Backlog: "New sprint" button */}
				{groupDef.key === "__backlog" && onCreateSprint && (
					<button
						type="button"
						onClick={(e) => {
							e.stopPropagation();
							onCreateSprint();
						}}
						className="flex items-center gap-1.5 rounded-md border border-dashed border-primary/40 bg-primary/5 px-2.5 py-1 text-xs font-semibold text-primary hover:bg-primary/10 hover:border-primary/60 active:scale-95 transition-all duration-150 shrink-0"
					>
						<Plus className="size-3" />
						New sprint
					</button>
				)}

				{/* Task count / field sum badge */}
				<span className="rounded-full bg-muted/60 px-2 py-0.5 text-xs font-bold text-muted-foreground/70 tabular-nums">
					{fieldSum && fieldSum !== "count"
						? `${apiFieldSum ?? 0}`
						: (totalCount ?? tasks.length)}
				</span>
			</div>

			{/* Start Sprint modal */}
			{sprint && onStartSprint && (
				<StartSprintModal
					sprint={sprint}
					open={startSprintOpen}
					onOpenChange={setStartSprintOpen}
					onSubmit={onStartSprint}
				/>
			)}

			{!collapsed &&
				(hasSwimlanes ? (
					<>
						{/* Swimlane bands */}
						{swimlaneDefs.map((swimDef) => {
							const viewCtxForSwim = {
								statuses,
								taskTypes,
								members,
								customFields,
							};
							const laneTasks =
								swimDef.key === "__all"
									? getViewCtxTasks()
									: getViewCtxTasks().filter(
											(t) =>
												getTaskSwimlaneKey(t, swimlaneBy, viewCtxForSwim) ===
												swimDef.key,
										);

							const handleSwimBandDragOver = (e: React.DragEvent) => {
								if (!isDraggable || swimDef.key === "__all") return;
								e.preventDefault();
								e.dataTransfer.dropEffect = "move";
								setDragOverSwimKey(swimDef.key);
							};

							const handleSwimBandDrop = (e: React.DragEvent) => {
								e.preventDefault();
								const taskId = e.dataTransfer.getData("text/plain");
								const sourceSwimKey = e.dataTransfer.getData(
									"application/x-source-swim-key",
								);
								setDragOverSwimKey(null);
								setDraggingId(null);
								setDragOverId(null);
								if (!taskId || !canEdit || swimDef.key === "__all") return;
								if (sourceSwimKey && sourceSwimKey !== swimDef.key) {
									const swimUpdate = buildColumnDropUpdate(
										swimlaneBy,
										swimDef.fieldValue,
										customFields,
									);
									if (Object.keys(swimUpdate).length > 0)
										onUpdateTaskField?.(taskId, swimUpdate);
								}
							};

							return (
								// biome-ignore lint/a11y/noStaticElementInteractions: drag-and-drop swimlane band
								<div
									key={swimDef.key}
									className={cn(
										"border-t border-border/15",
										dragOverSwimKey === swimDef.key &&
											isDraggable &&
											"bg-primary/5 ring-inset ring-1 ring-primary/20",
									)}
									onDragOver={handleSwimBandDragOver}
									onDragLeave={(e) => {
										if (!e.currentTarget.contains(e.relatedTarget as Node)) {
											setDragOverSwimKey(null);
										}
									}}
									onDrop={handleSwimBandDrop}
								>
									{swimDef.key !== "__all" && (
										<div className="flex items-center gap-2 px-8 py-1.5 bg-muted/10">
											<span className="text-xs font-bold uppercase tracking-widest text-muted-foreground/50">
												{swimDef.label}
											</span>
										</div>
									)}
									{/* Column headers shown once for the first band */}
									{swimDef.key === (swimlaneDefs[0]?.key ?? "__all") &&
										columnHeaders}
									{laneTasks.length === 0 && !groupPagination?.hasMore ? (
										<div className="flex flex-col items-center py-5 text-muted-foreground/40">
											<p className="text-sm font-medium">No tasks</p>
										</div>
									) : (
										laneTasks.map((task, index) =>
											renderTaskRow(task, index, groupDef.key, swimDef.key),
										)
									)}
								</div>
							);
						})}

						{viewMoreButton}

						{/* Add task button after all swimlane bands */}
						{showAddTask && (
							<AddTaskRow
								variant="list"
								taskTypes={taskTypes}
								onAdd={handleAdd}
							/>
						)}
					</>
				) : (
					<>
						{columnHeaders}
						{orderedTasks.length === 0 && !groupPagination?.hasMore ? (
							<div className="flex flex-col items-center py-8 text-muted-foreground/40">
								<p className="text-sm font-medium">No tasks</p>
							</div>
						) : (
							orderedTasks.map((task, index) =>
								renderTaskRow(task, index, groupDef.key),
							)
						)}
						{viewMoreButton}

						{showAddTask && (
							<AddTaskRow
								variant="list"
								taskTypes={taskTypes}
								onAdd={handleAdd}
							/>
						)}
					</>
				))}
		</div>
	);
}
