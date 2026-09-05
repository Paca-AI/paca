import { useMutation, useQueryClient } from "@tanstack/react-query";
import { ChevronLeft, ChevronRight, Loader2 } from "lucide-react";
import type { ReactNode } from "react";
import { useEffect, useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";

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
import {
	createLoadMoreScrollHandler,
	type LoadMorePagination,
} from "@/lib/scroll-pagination";
import { cn } from "@/lib/utils";

import { AddTaskRow } from "./add-task-row";
import { TaskCard } from "./task-card";
import { TaskContextMenu } from "./task-context-menu";
import {
	applyStatusFilterToColumnDefs,
	buildColumnDropUpdate,
	type ColumnGroupDef,
	DEFAULT_VISIBLE_FIELDS,
	type EpicsPagination,
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
	/** Load-more state for the epic picker, shared by every card in this view. */
	epicsPagination?: EpicsPagination;
	onUpdateTask?: (taskId: string, payload: TaskFieldUpdate) => void;
	onMoveToColumn?: (taskId: string, update: TaskFieldUpdate) => void;
	/** Opens the delete-confirmation dialog for a task — wired to the
	 * Mod+Backspace keyboard shortcut while a card is hovered. */
	onDeleteTask?: (taskId: string) => void;
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
			/** True when the column's last load-more attempt failed. Stops
			 * ColumnScrollArea's auto-fill effect from immediately retrying on
			 * every render; a manual scroll or button click can still retry. */
			lastLoadMoreFailed?: boolean;
		}
	>;
}

// ── Column scroll area ──────────────────────────────────────────────────────

/** How long ColumnScrollArea waits before retrying its own auto-fill request
 * after a failure, instead of retrying on the very next render. */
const AUTO_FILL_RETRY_BACKOFF_MS = 4000;

/**
 * Wraps a column's scrollable card list. `onScroll` alone only covers the
 * case where there's already enough content to scroll — if the initial page
 * (e.g. a small configured page size) doesn't fill the visible column
 * height, no `scroll` event will ever fire, so infinite scroll would never
 * kick in even though more pages exist. This effect checks after every
 * render whether the container is actually scrollable and, if not (and more
 * pages are available), requests the next page directly — repeating until
 * either the content fills the column or there's nothing left to load.
 */
function ColumnScrollArea({
	pagination,
	children,
}: {
	pagination:
		| (LoadMorePagination & {
				/** True when the last load-more attempt failed — see the
				 * dedicated comment on this effect below. */
				lastLoadMoreFailed?: boolean;
		  })
		| undefined;
	children: ReactNode;
}) {
	const scrollRef = useRef<HTMLDivElement>(null);
	// Lets the backoff timer below retry with whatever is current by the time
	// it fires, rather than the (possibly several-renders-stale) pagination
	// object captured when the timer was scheduled.
	const paginationRef = useRef(pagination);
	paginationRef.current = pagination;
	// Guards against queuing a new backoff timer on every render while one is
	// already pending.
	const retryScheduledRef = useRef(false);
	// Lets the mount/unmount effect below clear a pending backoff timer if the
	// column unmounts before it fires — otherwise it would still call
	// onLoadMore() for a column that's no longer visible.
	const retryTimeoutRef = useRef<ReturnType<typeof setTimeout> | null>(null);

	useEffect(() => {
		return () => {
			if (retryTimeoutRef.current) clearTimeout(retryTimeoutRef.current);
		};
	}, []);

	useEffect(() => {
		const el = scrollRef.current;
		if (!el || !pagination?.hasMore || pagination.isLoadingMore) return;
		if (el.scrollHeight > el.clientHeight) return;

		if (pagination.lastLoadMoreFailed) {
			// A failed fetch (network blip, 5xx) leaves the column just as
			// under-filled as before it tried — since this effect has no
			// dependency array, it re-runs on every subsequent render, and
			// retrying immediately would hammer the backend in a tight loop.
			// Back off instead: wait, then retry once with fresh state. This
			// column has no visible scrollbar to manually retry from (that's
			// the whole reason auto-fill exists), so without this the column
			// would otherwise be stuck at its under-filled size forever.
			if (retryScheduledRef.current) return;
			retryScheduledRef.current = true;
			retryTimeoutRef.current = setTimeout(() => {
				retryScheduledRef.current = false;
				retryTimeoutRef.current = null;
				const current = paginationRef.current;
				if (current?.hasMore && !current.isLoadingMore) {
					current.onLoadMore();
				}
			}, AUTO_FILL_RETRY_BACKOFF_MS);
			return;
		}

		pagination.onLoadMore();
	});

	return (
		<div
			ref={scrollRef}
			className="min-h-0 flex-1 overflow-y-auto pb-2"
			onScroll={createLoadMoreScrollHandler(pagination)}
		>
			{children}
		</div>
	);
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
	epicsPagination,
	onCreateTask,
	onTaskClick,
	onUpdateTask,
	onMoveToColumn,
	onDeleteTask,
	manualSort,
	onReorderTask,
	onCollapseChange,
	columnPagination,
}: BoardViewProps) {
	const { t } = useTranslation("projects");
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
		() => getColumnGroupDefs(columnBy, viewCtx, t),
		[columnBy, viewCtx, t],
	);

	// Swimlane definitions
	const swimlaneDefs = useMemo(
		() => getSwimlaneDefs(swimlaneBy, viewCtx, t),
		[swimlaneBy, viewCtx, t],
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

	/** Moves a task into `colDef` — shared by drag-and-drop and the
	 * keyboard move-left/move-right shortcuts so both stay in sync. */
	const moveTaskToColumnDef = (task: Task, colDef: ColumnGroupDef) => {
		const currentKeys = getTaskColumnKeys(task, columnBy, viewCtx);
		if (currentKeys.includes(colDef.key)) return;
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
			onMoveToColumn(task.id, update);
		} else {
			updateMutation.mutate({ taskId: task.id, update });
		}
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

		moveTaskToColumnDef(task, colDef);
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
		columnDefs,
		tasks,
		columnBy,
		viewCtx,
		isStatusGrouping,
		viewConfig?.filters?.statuses,
		statuses,
		t,
	]);

	// ── Helpers ───────────────────────────────────────────────────────────────

	const hasSwimlanes = Boolean(swimlaneBy && swimlaneBy !== "none");

	/** Renders the "add task" row for one [column × swimlane] cell, or null if
	 * task creation isn't available there. Factored out so the no-swimlane
	 * layout can pin it as a non-scrolling footer (like the header) instead of
	 * letting it scroll away with the cards. */
	const renderAddTaskRow = (
		colDef: ColumnGroupDef,
		swimDef: ColumnGroupDef,
	) => {
		if (
			!canCreate ||
			!(isStatusGrouping || columnBy === "sprint") ||
			colDef.key === "__none"
		) {
			return null;
		}
		return (
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
		);
	};

	/** Renders the cards inside one [column × swimlane] cell.
	 * `minHeightClassName` lets the no-swimlane layout stretch this to fill its
	 * independently-scrolling column (`min-h-full`) while the swimlane layout
	 * keeps a fixed floor (`min-h-28`) since its cells aren't height-bound.
	 * `showAddTaskRow` is false for the no-swimlane layout, which renders its
	 * own pinned footer instead (see `renderAddTaskRow` above).
	 * `useScrollPagination` is true for the no-swimlane layout, whose column
	 * has its own scroll container to drive infinite scroll from (see the
	 * `onScroll` handler at its call site) — it then shows a loading
	 * indicator instead of a click-to-load button. The swimlane layout has no
	 * such per-cell scroll container, so it keeps the explicit button. */
	const renderCellCards = (
		colDef: ColumnGroupDef,
		swimDef: ColumnGroupDef,
		minHeightClassName: string = "min-h-28",
		showAddTaskRow = true,
		useScrollPagination = false,
	) => {
		const swimOverKey = `${colDef.key}|${swimDef.key}`;
		const laneTasks = getSwimlaneColumnTasks(colDef.key, swimDef.key);
		const isOver =
			overSwimKey === swimOverKey ||
			(!hasSwimlanes && overColumnKey === colDef.key);

		// Adjacent columns for the Mod+Left/Right "move column" shortcut.
		const colIdx = effectiveColumnDefs.findIndex((c) => c.key === colDef.key);
		const prevColDef = colIdx > 0 ? effectiveColumnDefs[colIdx - 1] : undefined;
		const nextColDef =
			colIdx !== -1 && colIdx < effectiveColumnDefs.length - 1
				? effectiveColumnDefs[colIdx + 1]
				: undefined;

		return (
			// biome-ignore lint/a11y/noStaticElementInteractions: drag-and-drop drop zone
			<div
				className={cn(
					"flex flex-col gap-2 rounded-xl p-2 transition-all duration-200",
					minHeightClassName,
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
						<p className="text-sm">{t("board.view.noTasks")}</p>
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
						<TaskContextMenu
							task={task}
							statuses={statuses}
							taskTypes={taskTypes}
							members={members}
							epics={epics}
							epicsPagination={epicsPagination}
							canEdit={!!canEdit}
							onOpen={() => onTaskClick(task)}
							onUpdate={handleInlineUpdate}
							onDelete={canEdit ? onDeleteTask : undefined}
							columnDefs={effectiveColumnDefs}
							onMoveToColumnDef={moveTaskToColumnDef}
							taskIdPrefix={taskIdPrefix}
							projectId={projectId}
						>
							<TaskCard
								task={task}
								taskIdPrefix={taskIdPrefix}
								statuses={statuses}
								taskTypes={taskTypes}
								members={members}
								customFields={customFields}
								epics={epics}
								epicsPagination={epicsPagination}
								visibleFields={visibleFields}
								canEdit={canEdit}
								isDragging={draggingId === task.id}
								onDragStart={(e) => handleDragStart(e, task.id)}
								onDragEnd={handleDragEnd}
								onClick={() => onTaskClick(task)}
								onUpdate={canEdit ? handleInlineUpdate : undefined}
								onDelete={canEdit ? onDeleteTask : undefined}
								onMoveLeft={
									canEdit && prevColDef
										? () => moveTaskToColumnDef(task, prevColDef)
										: undefined
								}
								onMoveRight={
									canEdit && nextColDef
										? () => moveTaskToColumnDef(task, nextColDef)
										: undefined
								}
							/>
						</TaskContextMenu>
					</div>
				))}
				{(() => {
					const pg = columnPagination?.[colDef.key];
					if (!pg?.hasMore) return null;
					if (useScrollPagination) {
						// The scroll container itself (see the `onScroll` handler at
						// this cell's call site) triggers the fetch — this is just
						// the in-flight indicator, not a click target.
						if (!pg.isLoadingMore) return null;
						return (
							<div className="flex items-center justify-center gap-1.5 py-2 text-xs text-muted-foreground/50">
								<Loader2 className="size-3 animate-spin" />
								{t("board.view.loading")}
							</div>
						);
					}
					return (
						<button
							type="button"
							onClick={pg.onLoadMore}
							disabled={pg.isLoadingMore}
							className="mt-1 w-full rounded-lg border border-dashed border-border/40 py-1.5 text-xs font-medium text-muted-foreground/70 hover:border-primary/40 hover:text-primary transition-all duration-150 disabled:opacity-50"
						>
							{pg.isLoadingMore
								? t("board.view.loading")
								: t("board.view.viewMore")}
						</button>
					);
				})()}
				{showAddTaskRow && renderAddTaskRow(colDef, swimDef)}
			</div>
		);
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
					title={
						isCollapsed
							? t("board.view.expandColumn")
							: t("board.view.collapseColumn")
					}
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
											title={t("board.view.expandColumn")}
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
		<div className="flex flex-1 min-h-0 items-stretch gap-4 overflow-x-auto overflow-y-hidden px-6 pt-5 pb-8">
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
								title={t("board.view.expandColumn")}
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

				const addTaskRow = renderAddTaskRow(colDef, noSwimAll);
				return (
					<div
						key={colDef.key}
						data-column-key={colDef.key}
						className="flex w-72 shrink-0 flex-col gap-2"
					>
						{/* Header stays outside the scroll area below, so it never
						 * scrolls away — same effect as `sticky top-0` without the
						 * z-index/stacking-context edge cases. */}
						<div className="shrink-0">{renderColHeader(colDef)}</div>
						<ColumnScrollArea pagination={columnPagination?.[colDef.key]}>
							{renderCellCards(colDef, noSwimAll, "min-h-full", false, true)}
						</ColumnScrollArea>
						{/* Pinned footer, same reasoning as the header above — always
						 * reachable without scrolling to the bottom of the column. */}
						{addTaskRow && <div className="shrink-0">{addTaskRow}</div>}
					</div>
				);
			})}
		</div>
	);
}
