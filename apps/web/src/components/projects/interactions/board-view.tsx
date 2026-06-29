import { useMutation, useQueryClient } from "@tanstack/react-query";
import { ChevronLeft, ChevronRight } from "lucide-react";
import { useEffect, useMemo, useState } from "react";

import {
	type Sprint,
	type Task,
	updateTask,
	type ViewConfig,
} from "@/lib/interaction-api";
import type {
	CustomFieldDefinition,
	ProjectMember,
	TaskStatus,
	TaskType,
} from "@/lib/project-api";
import { cn } from "@/lib/utils";

import { AddTaskRow } from "./add-task-row";
import { TaskCard } from "./task-card";
import {
	applyStatusFilterToColumnDefs,
	buildColumnDropUpdate,
	type ColumnGroupDef,
	DEFAULT_VISIBLE_FIELDS,
	getColumnGroupDefs,
	getSwimlaneDefs,
	getTaskColumnKeys,
	getTaskSwimlaneKey,
	type TaskFieldUpdate,
} from "./view-utils";

// ── Props ────────────────────────────────────────────────────────────────────

interface BoardViewProps {
	projectId: string;
	taskIdPrefix?: string;
	tasks: Task[];
	statuses: TaskStatus[];
	taskTypes: TaskType[];
	members?: ProjectMember[];
	customFields?: CustomFieldDefinition[];
	sprints?: Sprint[];
	viewConfig?: ViewConfig;
	canCreate: boolean;
	canEdit: boolean;
	tasksQueryKey: unknown[];
	onCreateTask: (
		statusId: string,
		title: string,
		taskTypeId?: string | null,
		extraFields?: TaskFieldUpdate,
	) => Promise<void>;
	onTaskClick: (task: Task) => void;
	epics?: Task[];
	onUpdateTask?: (taskId: string, payload: TaskFieldUpdate) => void;
	onMoveToColumn?: (taskId: string, update: TaskFieldUpdate) => void;
	manualSort?: boolean;
	onReorderTask?: (groupKey: string, taskId: string, newIndex: number) => void;
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

// ── Board view ────────────────────────────────────────────────────────────────

export function BoardView({
	projectId,
	taskIdPrefix = "",
	tasks,
	statuses,
	taskTypes,
	members = [],
	customFields = [],
	sprints = [],
	viewConfig,
	canCreate,
	canEdit,
	tasksQueryKey,
	epics = [],
	onCreateTask,
	onTaskClick,
	onUpdateTask,
	onMoveToColumn,
	manualSort,
	onReorderTask,
	onCollapseChange,
	columnPagination,
}: BoardViewProps) {
	const qc = useQueryClient();
	const [draggingId, setDraggingId] = useState<string | null>(null);
	const [overColumnKey, setOverColumnKey] = useState<string | null>(null);
	const [overCardId, setOverCardId] = useState<string | null>(null);
	// Tracks which swimlane band is being hovered: "colKey|swimKey"
	const [overSwimKey, setOverSwimKey] = useState<string | null>(null);
	const [collapsedColumns, setCollapsedColumns] = useState<Set<string>>(
		() => new Set(viewConfig?.collapsed_columns ?? []),
	);

	useEffect(() => {
		setCollapsedColumns(new Set(viewConfig?.collapsed_columns ?? []));
	}, [viewConfig?.collapsed_columns]);

	const toggleCollapse = (colKey: string) => {
		setCollapsedColumns((prev) => {
			const next = new Set(prev);
			if (next.has(colKey)) next.delete(colKey);
			else next.add(colKey);
			const cols = [...next];
			onCollapseChange?.(cols);
			return next;
		});
	};

	// Generic field-update for drag between columns
	const updateMutation = useMutation({
		mutationFn: ({
			taskId,
			update,
		}: {
			taskId: string;
			update: TaskFieldUpdate;
		}) => updateTask(projectId, taskId, update),
		onSuccess: () => qc.invalidateQueries({ queryKey: tasksQueryKey }),
	});

	// Inline field update handler used by TaskCard — delegates to onMoveToColumn
	// (which does proper cache invalidation) or falls back to updateMutation.
	const handleInlineUpdate = (taskId: string, payload: TaskFieldUpdate) => {
		if (onUpdateTask) {
			onUpdateTask(taskId, payload);
		} else if (onMoveToColumn) {
			onMoveToColumn(taskId, payload);
		} else {
			updateMutation.mutate({ taskId, update: payload });
		}
	};

	// ── View context ──────────────────────────────────────────────────────────

	const columnBy = viewConfig?.column_by ?? "status";
	const swimlaneBy = viewConfig?.swimlanes;
	const fieldSum = viewConfig?.field_sum;
	const isStatusGrouping =
		!viewConfig?.column_by || viewConfig.column_by === "status";
	const visibleFields: string[] =
		viewConfig?.fields && viewConfig.fields.length > 0
			? viewConfig.fields
			: DEFAULT_VISIBLE_FIELDS;

	const viewCtx = useMemo(
		() => ({ statuses, taskTypes, members, customFields, sprints }),
		[statuses, taskTypes, members, customFields, sprints],
	);

	// Static column definitions (all possible values)
	const columnDefs = useMemo(
		() => getColumnGroupDefs(columnBy, viewCtx),
		[columnBy, viewCtx],
	);

	// Swimlane definitions
	const swimlaneDefs = useMemo(
		() => getSwimlaneDefs(swimlaneBy, viewCtx),
		[swimlaneBy, viewCtx],
	);

	// ── Column tasks helper ───────────────────────────────────────────────────

	const getColumnTasks = (colKey: string): Task[] =>
		tasks.filter((t) =>
			getTaskColumnKeys(t, columnBy, viewCtx).includes(colKey),
		);

	const getDisplayCount = (colKey: string): number => {
		const colPagination = columnPagination?.[colKey];
		if (fieldSum && fieldSum !== "count") return colPagination?.fieldSum ?? 0;
		return colPagination?.totalCount ?? getColumnTasks(colKey).length;
	};

	// ── Swimlane task helper ──────────────────────────────────────────────────

	const getSwimlaneColumnTasks = (colKey: string, swimKey: string): Task[] => {
		const colTasks = getColumnTasks(colKey);
		if (swimKey === "__all") return colTasks;
		return colTasks.filter(
			(t) => getTaskSwimlaneKey(t, swimlaneBy, viewCtx) === swimKey,
		);
	};

	// ── Drag handlers ────────────────────────────────────────────────────────

	const handleDragStart = (e: React.DragEvent, taskId: string) => {
		if (!canEdit) return;
		setDraggingId(taskId);
		e.dataTransfer.effectAllowed = "move";
		e.dataTransfer.setData("text/plain", taskId);
		e.dataTransfer.setData("application/x-paca-task-id", taskId);
	};

	const handleDragEnd = () => {
		setDraggingId(null);
		setOverColumnKey(null);
		setOverCardId(null);
		setOverSwimKey(null);
	};

	const handleDropOnColumn = (e: React.DragEvent, colDef: ColumnGroupDef) => {
		e.preventDefault();
		const taskId = e.dataTransfer.getData("text/plain");
		if (!taskId || !canEdit) return;

		const task = tasks.find((t) => t.id === taskId);
		if (!task) {
			setDraggingId(null);
			setOverColumnKey(null);
			return;
		}

		// Check if the task is already in this column
		const currentKeys = getTaskColumnKeys(task, columnBy, viewCtx);
		if (!currentKeys.includes(colDef.key)) {
			const update = buildColumnDropUpdate(
				columnBy,
				colDef.fieldValue,
				customFields,
			);
			// Preserve sprint_id when changing status so the task doesn't silently
			// get moved to the product backlog.
			if (isStatusGrouping) {
				update.sprint_id = task.sprint_id;
			}
			if (onMoveToColumn) {
				onMoveToColumn(taskId, update);
			} else {
				updateMutation.mutate({ taskId, update });
			}
		}
		setDraggingId(null);
		setOverColumnKey(null);
		setOverCardId(null);
		setOverSwimKey(null);
	};

	const handleDropOnCard = (
		e: React.DragEvent,
		colDef: ColumnGroupDef,
		targetTaskId: string,
		targetIndex: number,
		swimDef?: ColumnGroupDef,
	) => {
		e.preventDefault();
		e.stopPropagation();
		const taskId = e.dataTransfer.getData("text/plain");
		if (!taskId || !canEdit) {
			setDraggingId(null);
			setOverCardId(null);
			setOverSwimKey(null);
			return;
		}
		const task = tasks.find((t) => t.id === taskId);
		if (!task) {
			setDraggingId(null);
			setOverCardId(null);
			setOverSwimKey(null);
			return;
		}

		const updates: TaskFieldUpdate = {};
		const currentColKeys = getTaskColumnKeys(task, columnBy, viewCtx);
		const colChanged = !currentColKeys.includes(colDef.key);

		if (colChanged) {
			const colUpdate = buildColumnDropUpdate(
				columnBy,
				colDef.fieldValue,
				customFields,
			);
			Object.assign(updates, colUpdate);
			// Preserve sprint_id when changing status so the task doesn't silently
			// get moved to the product backlog.
			if (isStatusGrouping) {
				updates.sprint_id = task.sprint_id;
			}
		}

		// Update swimlane field if task dropped onto a different band
		if (
			swimDef &&
			swimDef.key !== "__all" &&
			swimlaneBy &&
			swimlaneBy !== "none"
		) {
			const currentSwimKey = getTaskSwimlaneKey(task, swimlaneBy, viewCtx);
			if (currentSwimKey !== swimDef.key) {
				const swimUpdate = buildColumnDropUpdate(
					swimlaneBy,
					swimDef.fieldValue,
					customFields,
				);
				if (swimUpdate.custom_fields && updates.custom_fields) {
					updates.custom_fields = {
						...updates.custom_fields,
						...swimUpdate.custom_fields,
					};
				} else {
					Object.assign(updates, swimUpdate);
				}
			}
		}

		if (Object.keys(updates).length > 0) {
			if (onMoveToColumn) {
				onMoveToColumn(taskId, updates);
			} else {
				updateMutation.mutate({ taskId, update: updates });
			}
		} else if (manualSort && taskId !== targetTaskId && !colChanged) {
			// Reorder within same column
			const current = getColumnTasks(colDef.key);
			const srcIdx = current.findIndex((t) => t.id === taskId);
			if (srcIdx !== -1) {
				// After removing source, indices shift by -1 for elements past it.
				// Adjust so the item lands BEFORE the visual drop target.
				const adjustedTarget =
					srcIdx < targetIndex ? targetIndex - 1 : targetIndex;
				if (isStatusGrouping) {
					onReorderTask?.(colDef.key, taskId, adjustedTarget);
				}
			}
		}
		setDraggingId(null);
		setOverColumnKey(null);
		setOverCardId(null);
		setOverSwimKey(null);
	};

	/** Handles dropping a card directly onto a swimlane band (updates swimlane + column field). */
	const handleDropOnSwimlaneBand = (
		e: React.DragEvent,
		colDef: ColumnGroupDef,
		swimDef: ColumnGroupDef,
	) => {
		e.preventDefault();
		e.stopPropagation();
		const taskId = e.dataTransfer.getData("text/plain");
		if (!taskId || !canEdit) {
			setDraggingId(null);
			setOverSwimKey(null);
			return;
		}
		const task = tasks.find((t) => t.id === taskId);
		if (!task) {
			setDraggingId(null);
			setOverSwimKey(null);
			return;
		}

		const updates: TaskFieldUpdate = {};

		// Update column field if moved to a different column
		const currentColKeys = getTaskColumnKeys(task, columnBy, viewCtx);
		if (!currentColKeys.includes(colDef.key)) {
			const colUpdate = buildColumnDropUpdate(
				columnBy,
				colDef.fieldValue,
				customFields,
			);
			Object.assign(updates, colUpdate);
			// Preserve sprint_id when changing status so the task doesn't silently
			// get moved to the product backlog.
			if (isStatusGrouping) {
				updates.sprint_id = task.sprint_id;
			}
		}

		// Update swimlane field if moved to a different band
		if (swimDef.key !== "__all" && swimlaneBy && swimlaneBy !== "none") {
			const currentSwimKey = getTaskSwimlaneKey(task, swimlaneBy, viewCtx);
			if (currentSwimKey !== swimDef.key) {
				const swimUpdate = buildColumnDropUpdate(
					swimlaneBy,
					swimDef.fieldValue,
					customFields,
				);
				if (swimUpdate.custom_fields && updates.custom_fields) {
					updates.custom_fields = {
						...updates.custom_fields,
						...swimUpdate.custom_fields,
					};
				} else {
					Object.assign(updates, swimUpdate);
				}
			}
		}

		if (Object.keys(updates).length > 0) {
			if (onMoveToColumn) {
				onMoveToColumn(taskId, updates);
			} else {
				updateMutation.mutate({ taskId, update: updates });
			}
		}
		setDraggingId(null);
		setOverColumnKey(null);
		setOverCardId(null);
		setOverSwimKey(null);
	};

	// ── Dynamic column defs (for number/text/date fields with no preset values) ──

	const effectiveColumnDefs: ColumnGroupDef[] = useMemo(() => {
		let defs: ColumnGroupDef[];
		if (columnDefs.length > 0) {
			defs = columnDefs;
		} else {
			// Build columns from unique task values (for number/text fields)
			const seen = new Set<string>();
			const dynamic: ColumnGroupDef[] = [];
			for (const t of tasks) {
				for (const k of getTaskColumnKeys(t, columnBy, viewCtx)) {
					if (!seen.has(k)) {
						seen.add(k);
						dynamic.push({
							key: k,
							label: k === "__none" ? "None" : k,
							fieldValue: k,
						});
					}
				}
			}
			if (!seen.has("__none")) {
				dynamic.push({ key: "__none", label: "None", fieldValue: null });
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
		columnDefs,
		tasks,
		columnBy,
		viewCtx,
		isStatusGrouping,
		viewConfig?.filters?.statuses,
		statuses,
	]);

	// ── Helpers ───────────────────────────────────────────────────────────────

	const hasSwimlanes = Boolean(swimlaneBy && swimlaneBy !== "none");

	/** Renders the cards inside one [column × swimlane] cell. */
	const renderCellCards = (colDef: ColumnGroupDef, swimDef: ColumnGroupDef) => {
		const swimOverKey = `${colDef.key}|${swimDef.key}`;
		const laneTasks = getSwimlaneColumnTasks(colDef.key, swimDef.key);
		const isOver =
			overSwimKey === swimOverKey ||
			(!hasSwimlanes && overColumnKey === colDef.key);

		return (
			// biome-ignore lint/a11y/noStaticElementInteractions: drag-and-drop drop zone
			<div
				className={cn(
					"flex flex-col gap-2 rounded-xl p-2 min-h-28 transition-all duration-200",
					isOver
						? "bg-primary/8 ring-2 ring-primary/20"
						: "bg-muted/40 dark:bg-muted",
				)}
				onDragOver={(e) => {
					e.preventDefault();
					e.dataTransfer.dropEffect = "move";
					setOverColumnKey(colDef.key);
					setOverSwimKey(swimOverKey);
				}}
				onDragLeave={(e) => {
					if (!e.currentTarget.contains(e.relatedTarget as Node)) {
						setOverSwimKey(null);
					}
				}}
				onDrop={(e) =>
					hasSwimlanes
						? handleDropOnSwimlaneBand(e, colDef, swimDef)
						: handleDropOnColumn(e, colDef)
				}
			>
				{laneTasks.length === 0 && !columnPagination?.[colDef.key]?.hasMore && (
					<div className="flex flex-1 flex-col items-center justify-center py-6 text-muted-foreground/30">
						<p className="text-sm">No tasks</p>
					</div>
				)}
				{laneTasks.map((task, index) => (
					// biome-ignore lint/a11y/noStaticElementInteractions: drag-and-drop card slot
					<div
						key={task.id}
						className={cn(
							"relative",
							manualSort &&
								overCardId === task.id &&
								draggingId !== task.id &&
								"border-t-2 border-primary/60",
						)}
						onDragOver={(e) => {
							e.preventDefault();
							e.stopPropagation();
							setOverColumnKey(colDef.key);
							setOverSwimKey(swimOverKey);
							if (manualSort) setOverCardId(task.id);
						}}
						onDrop={(e) =>
							handleDropOnCard(
								e,
								colDef,
								task.id,
								index,
								hasSwimlanes ? swimDef : undefined,
							)
						}
					>
						<TaskCard
							task={task}
							taskIdPrefix={taskIdPrefix}
							statuses={statuses}
							taskTypes={taskTypes}
							members={members}
							customFields={customFields}
							epics={epics}
							visibleFields={visibleFields}
							canEdit={canEdit}
							isDragging={draggingId === task.id}
							onDragStart={(e) => handleDragStart(e, task.id)}
							onDragEnd={handleDragEnd}
							onClick={() => onTaskClick(task)}
							onUpdate={canEdit ? handleInlineUpdate : undefined}
							onDoubleClick={() => handleDoubleClick(task)}
						/>
					</div>
				))}
				{(() => {
					const pg = columnPagination?.[colDef.key];
					if (!pg?.hasMore) return null;
					return (
						<button
							type="button"
							onClick={pg.onLoadMore}
							disabled={pg.isLoadingMore}
							className="mt-1 w-full rounded-lg border border-dashed border-border/40 py-1.5 text-xs font-medium text-muted-foreground/70 hover:border-primary/40 hover:text-primary transition-all duration-150 disabled:opacity-50"
						>
							{pg.isLoadingMore ? "Loading…" : "View more"}
						</button>
					);
				})()}
				{canCreate &&
					(isStatusGrouping || columnBy === "sprint") &&
					colDef.key !== "__none" && (
						<AddTaskRow
							variant="board"
							taskTypes={taskTypes}
							onAdd={(title, typeId) => {
								const extra: TaskFieldUpdate = {};
								if (!isStatusGrouping && columnBy === "sprint") {
									extra.sprint_id =
										colDef.key === "__backlog" ? null : (colDef.key as string);
								}
								if (
									hasSwimlanes &&
									swimDef.key !== "__all" &&
									swimlaneBy &&
									swimlaneBy !== "none"
								) {
									const swimUpdate = buildColumnDropUpdate(
										swimlaneBy,
										swimDef.fieldValue,
										customFields,
									);
									Object.assign(extra, swimUpdate);
								}
								const statusId = isStatusGrouping
									? colDef.key
									: (statuses.find((s) => s.category !== "done")?.id ??
										statuses[0]?.id ??
										"");
								onCreateTask(
									statusId,
									title,
									typeId,
									Object.keys(extra).length > 0 ? extra : undefined,
								);
							}}
						/>
					)}
			</div>
		);
	};

	// ── Double-click to toggle task between backlog and sprint ─────────────
	const handleDoubleClick = (task: Task) => {
		if (!canEdit || !onMoveToColumn) return;
		const sprint = sprints.find(s => s.status === "active") || sprints[sprints.length - 1];
		const targetSprintId = task.sprint_id ? null : sprint?.id;
		if (task.sprint_id === targetSprintId) return;
		onMoveToColumn(task.id, { sprint_id: targetSprintId as string | null });
	};

	// ── Render ────────────────────────────────────────────────────────────────

	/** Column header chip — used both in swimlane and non-swimlane layouts. */
	const renderColHeader = (colDef: ColumnGroupDef) => {
		const displayCount = getDisplayCount(colDef.key);
		const isCollapsed = collapsedColumns.has(colDef.key);
		return (
			<div className="flex items-center gap-2 px-2 pb-1 group">
				{colDef.color && (
					<span
						className="size-1.75 rounded-full shrink-0"
						style={{
							background: colDef.color,
							boxShadow: `0 0 6px ${colDef.color}40`,
						}}
					/>
				)}
				<span className="text-xs font-bold text-foreground/80 tracking-[0.08em] uppercase flex-1 truncate">
					{colDef.label}
				</span>
				<button
					type="button"
					onClick={() => toggleCollapse(colDef.key)}
					className="flex size-5 shrink-0 items-center justify-center rounded opacity-0 group-hover:opacity-100 transition-opacity hover:bg-muted/60"
					title={isCollapsed ? "Expand column" : "Collapse column"}
				>
					{isCollapsed ? (
						<ChevronRight className="size-3 text-muted-foreground" />
					) : (
						<ChevronLeft className="size-3 text-muted-foreground" />
					)}
				</button>
				<span className="rounded-full bg-muted/60 px-2 py-0.5 text-xs font-bold text-muted-foreground/70 tabular-nums">
					{displayCount}
				</span>
			</div>
		);
	};

	if (hasSwimlanes) {
		// ── Swimlanes-outer layout: swimlane rows → column cells inside ──────
		// Shared singleton swimlane def for "no swimlane" filter
		const noSwim: ColumnGroupDef = {
			key: "__all",
			label: "",
			fieldValue: null,
		};
		// Only use defined defs; filter out the __all sentinel
		const visibleSwimDefs = swimlaneDefs.filter((s) => s.key !== "__all");

		return (
			<div className="flex flex-1 min-h-0 flex-col overflow-auto">
				<div className="min-w-max px-6 pt-5 pb-8 flex flex-col gap-0">
					{/* Sticky column-header row */}
					<div className="flex gap-4 pb-2 sticky top-0 z-10 bg-background border-b border-border/20 mb-1">
						{/* Swimlane label placeholder to align with row labels */}
						<div className="w-36 shrink-0" />
						{effectiveColumnDefs.map((colDef) => {
							const isCollapsed = collapsedColumns.has(colDef.key);
							const displayCount = getDisplayCount(colDef.key);

							if (isCollapsed) {
								return (
									<div
										key={colDef.key}
										className="w-10 shrink-0 flex flex-col items-center gap-1.5 pt-1"
									>
										<button
											type="button"
											onClick={() => toggleCollapse(colDef.key)}
											className="flex size-7 shrink-0 items-center justify-center rounded-lg hover:bg-muted/60 transition-colors"
											title="Expand column"
										>
											<ChevronRight className="size-3.5 text-muted-foreground" />
										</button>
										<span className="rounded-full bg-muted/60 px-2 py-0.5 text-xs font-bold text-muted-foreground/70 tabular-nums">
											{displayCount}
										</span>
										{colDef.color && (
											<span
												className="size-1.75 rounded-full shrink-0"
												style={{
													background: colDef.color,
													boxShadow: `0 0 6px ${colDef.color}40`,
												}}
											/>
										)}
										<div className="flex flex-1 items-start justify-center pt-1">
											<span
												className="text-xs font-bold text-foreground/60 tracking-[0.08em] uppercase whitespace-nowrap"
												style={{
													writingMode: "vertical-rl",
													transform: "rotate(180deg)",
												}}
											>
												{colDef.label}
											</span>
										</div>
									</div>
								);
							}

							return (
								<div key={colDef.key} className="w-72 shrink-0">
									{renderColHeader(colDef)}
								</div>
							);
						})}
					</div>

					{/* One row per swimlane */}
					{(visibleSwimDefs.length > 0 ? visibleSwimDefs : [noSwim]).map(
						(swimDef) => (
							<div
								key={swimDef.key}
								className="flex gap-4 py-3 border-b border-border/15 last:border-0"
							>
								{/* Swimlane label */}
								<div className="w-36 shrink-0 flex items-start pt-1 gap-2">
									{swimDef.color && (
										<span
											className="size-1.5 rounded-full mt-1.5 shrink-0"
											style={{ background: swimDef.color }}
										/>
									)}
									<span className="text-xs font-bold uppercase tracking-[0.08em] text-foreground/70 wrap-break-word leading-snug">
										{swimDef.label}
									</span>
								</div>

								{/* Column cells */}
								{effectiveColumnDefs.map((colDef) => {
									const isCollapsed = collapsedColumns.has(colDef.key);
									return (
										<div
											key={colDef.key}
											className={cn("shrink-0", isCollapsed ? "w-10" : "w-72")}
										>
											{!isCollapsed && renderCellCards(colDef, swimDef)}
										</div>
									);
								})}
							</div>
						),
					)}
				</div>
			</div>
		);
	}

	// ── No-swimlane layout: horizontal columns ────────────────────────────────
	const noSwimAll: ColumnGroupDef = {
		key: "__all",
		label: "",
		fieldValue: null,
	};

	return (
		<div className="flex flex-1 min-h-0 items-start gap-4 overflow-auto px-6 py-5 pb-8">
			{effectiveColumnDefs.map((colDef) => {
				const isCollapsed = collapsedColumns.has(colDef.key);
				const displayCount = getDisplayCount(colDef.key);

				if (isCollapsed) {
					return (
						<div
							key={colDef.key}
							data-column-key={colDef.key}
							className="flex w-10 shrink-0 flex-col items-center gap-2 pt-1"
						>
							<button
								type="button"
								onClick={() => toggleCollapse(colDef.key)}
								className="flex size-7 shrink-0 items-center justify-center rounded-lg hover:bg-muted/60 transition-colors"
								title="Expand column"
							>
								<ChevronRight className="size-3.5 text-muted-foreground" />
							</button>
							<span className="rounded-full bg-muted/60 px-2 py-0.5 text-xs font-bold text-muted-foreground/70 tabular-nums">
								{displayCount}
							</span>
							{colDef.color && (
								<span
									className="size-1.75 rounded-full shrink-0"
									style={{
										background: colDef.color,
										boxShadow: `0 0 6px ${colDef.color}40`,
									}}
								/>
							)}
							<div className="flex flex-1 items-start justify-center pt-1">
								<span
									className="text-xs font-bold text-foreground/60 tracking-[0.08em] uppercase whitespace-nowrap"
									style={{
										writingMode: "vertical-rl",
										transform: "rotate(180deg)",
									}}
								>
									{colDef.label}
								</span>
							</div>
						</div>
					);
				}

				return (
					<div
						key={colDef.key}
						data-column-key={colDef.key}
						className="flex w-72 shrink-0 flex-col gap-2.5"
					>
						{renderColHeader(colDef)}
						{renderCellCards(colDef, noSwimAll)}
					</div>
				);
			})}
		</div>
	);
}
