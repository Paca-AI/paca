import { useInfiniteQuery } from "@tanstack/react-query";
import { CalendarDays, ChevronRight, Loader2 } from "lucide-react";
import { Fragment, useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { PaneResizeHandle } from "@/components/shared/pane-resize-handle";
import {
	type ChildTaskFilters,
	childTasksInfiniteQueryOptions,
	type Sprint,
	type Task,
	type ViewConfig,
} from "@/lib/interaction-api";
import { ROADMAP_TASK_PANE, usePaneWidth } from "@/lib/pane-width";
import type {
	CustomFieldDefinition,
	ProjectMember,
	TaskStatus,
	TaskType,
} from "@/lib/project-api";
import { cn } from "@/lib/utils";
import { AddTaskRow } from "./add-task-row";
import {
	resolveSubtaskMode,
	useSubtaskExpansion,
} from "./use-subtask-expansion";
import {
	applyStatusFilterToColumnDefs,
	buildColumnDropUpdate,
	buildDynamicGroupDefs,
	type ColumnGroupDef,
	getColumnGroupDefs,
	getTaskColumnKeys,
	type TaskFieldUpdate,
} from "./view-utils";

// ─── Layout constants ─────────────────────────────────────────────────────────
const PX_PER_DAY = 28; // chart pixels per calendar day
const MS_PER_DAY = 86_400_000;
const PAD_DAYS = 7; // extra days buffered on each side of the date range

// ─── Helpers ──────────────────────────────────────────────────────────────────
function parseDateMs(s?: string | null): number | null {
	if (!s) return null;
	const d = new Date(s);
	return Number.isNaN(d.getTime()) ? null : d.getTime();
}

function fmtDate(ms: number): string {
	return new Date(ms).toLocaleDateString("default", {
		month: "short",
		day: "numeric",
		year: "numeric",
	});
}

// ─── Types ────────────────────────────────────────────────────────────────────
interface RoadmapViewProps {
	projectId?: string;
	/** The view's filters/sort, applied to the tasks listed under a parent. */
	childFilters?: ChildTaskFilters;
	/** Identifies the view for remembering which rows are expanded. */
	viewKey?: string;
	tasks: Task[];
	statuses: TaskStatus[];
	taskTypes: TaskType[];
	members?: ProjectMember[];
	reporters?: ProjectMember[];
	sprints?: Sprint[];
	customFields?: CustomFieldDefinition[];
	columnBy?: string;
	/** Active view config — used to hide status columns the view filters out. */
	viewConfig?: ViewConfig;
	canCreate?: boolean;
	canEdit?: boolean;
	/** True when the view is sorted "manual" — enables drag-to-reorder. */
	manualSort?: boolean;
	onReorderTask?: (groupKey: string, taskId: string, newIndex: number) => void;
	/** Moves a task to another group (drag between groups). */
	onMoveToColumn?: (taskId: string, update: TaskFieldUpdate) => void;
	onCreateTask?: (
		statusId: string,
		title: string,
		taskTypeId?: string | null,
	) => Promise<void>;
	onTaskClick: (task: Task) => void;
	pagination?: {
		hasMore: boolean;
		isLoadingMore: boolean;
		onLoadMore: () => void;
	};
}

interface MonthCell {
	label: string;
	px: number;
	widthPx: number;
}

interface BarData {
	leftPx: number;
	widthPx: number;
	singleDate: boolean;
	overdue: boolean;
	tooltip: string;
}

// ─── Component ────────────────────────────────────────────────────────────────
export function RoadmapView({
	projectId,
	childFilters,
	viewKey = "default",
	tasks,
	statuses,
	taskTypes,
	members = [],
	reporters,
	sprints = [],
	customFields = [],
	columnBy = "status",
	viewConfig,
	canCreate = false,
	canEdit = false,
	manualSort = false,
	onReorderTask,
	onMoveToColumn,
	onCreateTask,
	onTaskClick,
	pagination,
}: RoadmapViewProps) {
	const { t } = useTranslation("projects");
	const { t: tShared } = useTranslation("shared");
	const taskPane = usePaneWidth(ROADMAP_TASK_PANE);
	const leftColW = taskPane.width;
	const subtaskMode = resolveSubtaskMode(viewConfig);
	const expansion = useSubtaskExpansion(
		projectId ?? "",
		viewKey,
		subtaskMode === "expanded",
	);
	const nestable = !!projectId && subtaskMode !== "hidden";
	const [draggingId, setDraggingId] = useState<string | null>(null);
	const [overGroupKey, setOverGroupKey] = useState<string | null>(null);
	const [overTaskId, setOverTaskId] = useState<string | null>(null);
	const isStatusGrouping = !columnBy || columnBy === "status";
	const canDrag = canEdit && !!onMoveToColumn;
	const canReorder = manualSort && !!onReorderTask;

	const dragStartTimer = useRef<ReturnType<typeof setTimeout> | null>(null);

	const resetDrag = () => {
		if (dragStartTimer.current) clearTimeout(dragStartTimer.current);
		dragStartTimer.current = null;
		setDraggingId(null);
		setOverGroupKey(null);
		setOverTaskId(null);
	};

	/** Drop onto a group (`targetTaskId` null → group body) or onto a row. */
	const handleDrop = (
		e: React.DragEvent,
		group: ColumnGroupDef,
		groupTasks: Task[],
		targetTaskId: string | null,
	) => {
		e.preventDefault();
		e.stopPropagation();
		const taskId = e.dataTransfer.getData("text/plain");
		const task = tasks.find((tk) => tk.id === taskId);
		resetDrag();
		if (!task || !canDrag) return;

		const colChanged = !getTaskColumnKeys(task, columnBy, viewCtx).includes(
			group.key,
		);
		if (colChanged) {
			const update = buildColumnDropUpdate(
				columnBy,
				group.fieldValue,
				customFields,
			);
			// Keep the sprint so a status change doesn't move it to the backlog.
			if (isStatusGrouping) update.sprint_id = task.sprint_id;
			if (Object.keys(update).length > 0) onMoveToColumn?.(taskId, update);
			return;
		}

		if (!canReorder || !targetTaskId || targetTaskId === taskId) return;
		const srcIdx = groupTasks.findIndex((tk) => tk.id === taskId);
		const targetIdx = groupTasks.findIndex((tk) => tk.id === targetTaskId);
		if (srcIdx === -1 || targetIdx === -1) return;
		// Land BEFORE the drop target (top-border indicator).
		onReorderTask?.(
			group.key,
			taskId,
			srcIdx < targetIdx ? targetIdx - 1 : targetIdx,
		);
	};
	// Stable "now" — fixed at mount so all bars are consistent
	const now = useMemo(() => Date.now(), []);

	const viewCtx = useMemo(
		() => ({ statuses, taskTypes, members, reporters, customFields, sprints }),
		[statuses, taskTypes, members, reporters, customFields, sprints],
	);

	// Drop targets must respect the view's status filter (as Board/List do),
	// otherwise a drop could move a task into a status the view hides.
	const groupDefs = useMemo(() => {
		const presetDefs = getColumnGroupDefs(columnBy, viewCtx, t);
		return applyStatusFilterToColumnDefs(
			// Fields without preset groups (e.g. tags) group by the loaded values.
			presetDefs.length > 0
				? presetDefs
				: buildDynamicGroupDefs(tasks, columnBy, viewCtx, t),
			isStatusGrouping,
			viewConfig?.filters?.statuses,
			statuses,
		);
	}, [
		tasks,
		columnBy,
		viewCtx,
		t,
		isStatusGrouping,
		viewConfig?.filters?.statuses,
		statuses,
	]);

	const defaultStatusId = useMemo(
		() =>
			statuses.find((s) => s.is_default)?.id ??
			[...statuses].sort((a, b) => a.position - b.position)[0]?.id ??
			"",
		[statuses],
	);

	// ── Timeline geometry ──────────────────────────────────────────────────────
	const { chartWidth, toPx, months, todayPx } = useMemo(() => {
		const allMs: number[] = [];
		for (const t of tasks) {
			const s = parseDateMs(t.start_date);
			const d = parseDateMs(t.due_date);
			if (s !== null) allMs.push(s);
			if (d !== null) allMs.push(d);
		}

		const rawMin =
			allMs.length > 0 ? Math.min(...allMs) : now - 15 * MS_PER_DAY;
		const rawMax =
			allMs.length > 0 ? Math.max(...allMs) : now + 15 * MS_PER_DAY;
		const minMs = rawMin - PAD_DAYS * MS_PER_DAY;
		const maxMs = rawMax + PAD_DAYS * MS_PER_DAY;

		const totalDays = Math.ceil((maxMs - minMs) / MS_PER_DAY);
		const chartWidth = Math.max(totalDays * PX_PER_DAY, 800);

		// Pixel offset from the left edge of the chart area for any timestamp
		function toPx(ms: number): number {
			return ((ms - minMs) / MS_PER_DAY) * PX_PER_DAY;
		}

		// Build month header cells
		const months: MonthCell[] = [];
		let cursor = new Date(
			new Date(minMs).getFullYear(),
			new Date(minMs).getMonth(),
			1,
		);
		while (cursor.getTime() < maxMs) {
			const mStart = cursor.getTime();
			const next = new Date(cursor.getFullYear(), cursor.getMonth() + 1, 1);
			const days = (next.getTime() - mStart) / MS_PER_DAY;
			months.push({
				label: cursor.toLocaleString("default", {
					month: "long",
					year: "numeric",
				}),
				px: toPx(mStart),
				widthPx: days * PX_PER_DAY,
			});
			cursor = next;
		}

		return { chartWidth, toPx, months, todayPx: toPx(now) };
	}, [tasks, now]);

	const todayInRange = todayPx >= 0 && todayPx <= chartWidth;

	// ── Bar geometry for one task ─────────────────────────────────────────────
	function getBar(task: Task): BarData | null {
		const startMs = parseDateMs(task.start_date);
		const dueMs = parseDateMs(task.due_date);
		if (startMs === null && dueMs === null) return null;

		const singleDate = startMs === null || dueMs === null;
		const effectiveStart = startMs ?? (dueMs as number);
		const effectiveEnd = dueMs ?? (startMs as number) + MS_PER_DAY;

		const leftPx = toPx(effectiveStart);
		const widthPx = Math.max(8, toPx(effectiveEnd) - leftPx);
		const overdue = dueMs !== null && dueMs < now;

		let tooltip: string;
		if (startMs !== null && dueMs !== null) {
			tooltip = t("layout.roadmap.dateRangeTooltip", {
				start: fmtDate(startMs),
				end: fmtDate(dueMs),
			});
		} else if (startMs !== null) {
			tooltip = t("layout.roadmap.startDateTooltip", {
				date: fmtDate(startMs),
			});
		} else {
			tooltip = t("layout.roadmap.dueDateTooltip", {
				date: fmtDate(dueMs as number),
			});
		}

		// Nested rows can fall outside the range computed from the top-level
		// tasks, so keep their bars inside the chart.
		const clampedLeft = Math.min(Math.max(0, leftPx), chartWidth - 8);
		const clampedRight = Math.min(chartWidth, Math.max(leftPx + widthPx, 8));
		return {
			leftPx: clampedLeft,
			widthPx: Math.max(8, clampedRight - clampedLeft),
			singleDate,
			overdue,
			tooltip,
		};
	}

	// ── Shared chart cell ─────────────────────────────────────────────────────
	function ChartCell({ bar }: { bar: BarData | null }) {
		return (
			<div
				className="relative shrink-0 flex items-center"
				style={{ width: chartWidth, height: 40 }}
			>
				{/* Month boundary gridlines */}
				{months.map((m) => (
					<div
						key={m.label}
						className="absolute inset-y-0 w-px bg-border/12"
						style={{ left: m.px }}
					/>
				))}

				{/* Today line (subtle, per-row) */}
				{todayInRange && (
					<div
						className="absolute inset-y-0 w-0.5 bg-primary/18"
						style={{ left: todayPx, transform: "translateX(-50%)" }}
					/>
				)}

				{/* Bar or no-date placeholder */}
				{bar ? (
					<div
						title={bar.tooltip}
						className={cn(
							"absolute h-6 rounded-full shadow-sm",
							"opacity-80 group-hover:opacity-100 transition-opacity duration-150",
							bar.overdue ? "bg-destructive/55" : "bg-primary/55",
							bar.singleDate && "border border-primary/50 bg-primary/30",
						)}
						style={{ left: bar.leftPx, width: bar.widthPx }}
					/>
				) : (
					<div
						title={t("layout.roadmap.noDatesSet")}
						className="absolute h-5 w-20 rounded border border-dashed border-muted-foreground/20"
						style={{ left: Math.max(0, todayPx - 40) }}
					/>
				)}
			</div>
		);
	}

	// ── Task row ──────────────────────────────────────────────────────────────
	/** The clickable row (name column + bar). `depth` > 0 marks a nested child. */
	function renderRowBody(task: Task, depth: number) {
		const bar = getBar(task);
		const type = taskTypes.find((tt) => tt.id === task.task_type_id) ?? null;
		const count = task.subtask_count ?? 0;
		const isOpen = expansion.isExpanded(task.id);
		return (
			// biome-ignore lint/a11y/useSemanticElements: the chevron is a real button and cannot nest inside one
			<div
				role="button"
				tabIndex={0}
				className="group flex w-full cursor-pointer border-b border-border/10 text-left last:border-0"
				onClick={() => onTaskClick(task)}
				onKeyDown={(e) => {
					if (e.target !== e.currentTarget) return;
					if (e.key === "Enter" || e.key === " ") {
						e.preventDefault();
						onTaskClick(task);
					}
				}}
			>
				{/* Sticky task name */}
				<div
					className={cn(
						"sticky left-0 z-10 flex shrink-0 items-center gap-1.5",
						"border-r border-border/20 bg-background px-4 py-2.5",
						"group-hover:bg-muted/30 transition-colors duration-100",
					)}
					style={{ width: leftColW, paddingLeft: 16 + depth * 16 }}
				>
					{nestable &&
						(count > 0 ? (
							<button
								type="button"
								aria-expanded={isOpen}
								aria-label={t(
									isOpen ? "board.subtasks.collapse" : "board.subtasks.expand",
									{ n: count },
								)}
								onClick={(e) => {
									e.stopPropagation();
									expansion.toggle(task.id);
								}}
								className="-ml-1.5 flex size-6 shrink-0 items-center justify-center rounded-md text-muted-foreground/70 hover:bg-muted/60 hover:text-foreground focus-visible:outline-2 focus-visible:outline-primary"
							>
								<ChevronRight
									className={cn(
										"size-3.5 transition-transform duration-150 motion-reduce:transition-none",
										isOpen && "rotate-90",
									)}
								/>
							</button>
						) : (
							<span aria-hidden className="-ml-1.5 size-6 shrink-0" />
						))}
					<span
						className="size-1.5 shrink-0 rounded-full"
						style={{
							background:
								type?.color ?? "oklch(var(--muted-foreground) / 0.25)",
						}}
					/>
					<span className="min-w-0 truncate text-sm font-medium text-foreground/85">
						{task.title}
					</span>
					{nestable && count > 0 && (
						<span
							title={t("board.subtasks.progress", {
								done: task.subtask_done_count ?? 0,
								n: count,
							})}
							className="shrink-0 rounded-md bg-muted/50 px-1.5 py-0.5 text-[11px] font-semibold tabular-nums text-muted-foreground"
						>
							{task.subtask_done_count ?? 0}/{count}
						</span>
					)}
				</div>

				{/* Chart area */}
				<ChartCell bar={bar} />
			</div>
		);
	}

	// Called as a function (not rendered as a component) so the draggable node
	// isn't remounted by state changes mid-drag.
	function renderTaskRow(
		task: Task,
		group: ColumnGroupDef,
		groupTasks: Task[],
	) {
		// Position only matters for same-group drops; cross-group drops just
		// change the column, so don't show the "insert before" indicator.
		const draggingInGroup = groupTasks.some((tk) => tk.id === draggingId);

		return (
			<Fragment key={task.id}>
				{/* biome-ignore lint/a11y/noStaticElementInteractions: drag-and-drop row slot */}
				<div
					draggable={canDrag}
					className={cn(
						draggingId === task.id && "opacity-40",
						canReorder &&
							overTaskId === task.id &&
							draggingId !== task.id &&
							draggingInGroup &&
							"border-t-2 border-primary/60",
					)}
					onDragStart={(e) => {
						e.dataTransfer.effectAllowed = "move";
						e.dataTransfer.setData("text/plain", task.id);
						// Defer: empty groups render while dragging, and changing the
						// layout synchronously inside dragstart makes Chrome cancel the
						// drag (hit rows below an empty group, e.g. non-backlog statuses).
						const id = task.id;
						dragStartTimer.current = setTimeout(() => setDraggingId(id), 0);
					}}
					onDragEnd={resetDrag}
					onDragOver={(e) => {
						if (!canDrag) return;
						e.preventDefault();
						e.stopPropagation();
						e.dataTransfer.dropEffect = "move";
						setOverGroupKey(group.key);
						setOverTaskId(task.id);
					}}
					onDrop={(e) => handleDrop(e, group, groupTasks, task.id)}
				>
					{renderRowBody(task, 0)}
				</div>
				{nestable &&
					projectId &&
					expansion.isExpanded(task.id) &&
					(task.subtask_count ?? 0) > 0 && (
						<RoadmapChildRows
							projectId={projectId}
							parent={task}
							depth={1}
							filters={childFilters ?? {}}
							isExpanded={expansion.isExpanded}
							renderRow={renderRowBody}
							leftColWidth={leftColW}
						/>
					)}
			</Fragment>
		);
	}

	// ── Group header ──────────────────────────────────────────────────────────
	function GroupHeader({
		group,
		count,
	}: {
		group: ColumnGroupDef;
		count: number;
	}) {
		return (
			<div className="flex items-center gap-2 border-b border-border/15 bg-muted/20 px-4 py-2">
				<span
					className="size-2 shrink-0 rounded-full"
					style={{
						background: group.color ?? "oklch(var(--muted-foreground) / 0.4)",
						boxShadow: group.color ? `0 0 6px ${group.color}40` : undefined,
					}}
				/>
				<span className="text-xs font-bold uppercase tracking-[0.07em] text-foreground/65">
					{group.label}
				</span>
				<span className="rounded-full bg-muted/70 px-2 py-px text-xs font-bold tabular-nums text-muted-foreground/60">
					{count}
				</span>
			</div>
		);
	}

	// ─── Render ────────────────────────────────────────────────────────────────
	return (
		<div className="flex h-full flex-col overflow-hidden">
			<div className="flex-1 overflow-auto">
				{/*
				 * Single scrollable container. minWidth triggers horizontal scroll.
				 * Left column cells are sticky left-0; header row is sticky top-0.
				 */}
				<div style={{ minWidth: leftColW + chartWidth }}>
					{/* ── Sticky timeline header ───────────────────────────── */}
					<div
						className={cn(
							"sticky top-0 z-20 flex h-11 shrink-0",
							"border-b border-border/25 bg-background",
						)}
					>
						{/* Task column label */}
						<div
							className={cn(
								"sticky left-0 z-30 flex shrink-0 items-center gap-1.5",
								"border-r border-border/25 bg-background/95 px-4",
							)}
							style={{ width: leftColW }}
						>
							<CalendarDays className="size-3 text-muted-foreground/45" />
							<span className="text-xs font-semibold uppercase tracking-[0.08em] text-muted-foreground/55">
								{t("layout.roadmap.taskColumnLabel")}
							</span>
							<PaneResizeHandle
								config={ROADMAP_TASK_PANE}
								pane={taskPane}
								label={tShared("paneResize.taskColumn")}
								edge="right"
								showGrip
								testId="roadmap-task-column-resize-handle"
							/>
						</div>

						{/* Month labels */}
						<div
							className="relative overflow-hidden"
							style={{ width: chartWidth }}
						>
							{months.map((m) => (
								<div
									key={m.label}
									className={cn(
										"absolute inset-y-0 flex items-center overflow-hidden",
										"border-r border-border/15 px-3",
									)}
									style={{ left: m.px, width: m.widthPx }}
								>
									<span className="text-xs font-semibold text-muted-foreground/50 whitespace-nowrap">
										{m.label}
									</span>
								</div>
							))}

							{/* Today indicator: ring dot + vertical line */}
							{todayInRange && (
								<div
									className="absolute inset-y-0 z-10 flex flex-col items-center"
									style={{ left: todayPx, transform: "translateX(-50%)" }}
								>
									<div className="mt-1.5 size-2 shrink-0 rounded-full bg-primary ring-2 ring-primary/25" />
									<div className="w-px flex-1 bg-primary/70" />
								</div>
							)}
						</div>
					</div>

					{/* ── Content ────────────────────────────────────────────── */}
					{tasks.length === 0 ? (
						<div className="flex flex-col items-center py-20 text-muted-foreground/40">
							<CalendarDays className="mb-2 size-7" />
							<p className="text-sm font-medium">
								{t("layout.roadmap.noTasksToDisplay")}
							</p>
						</div>
					) : (
						groupDefs.map((group) => {
							const groupTasks = tasks.filter((task) =>
								getTaskColumnKeys(task, columnBy, viewCtx).includes(group.key),
							);
							// Keep empty groups as drop targets while dragging.
							if (groupTasks.length === 0 && !(canDrag && draggingId))
								return null;

							return (
								// biome-ignore lint/a11y/noStaticElementInteractions: drag-and-drop group drop zone
								<div
									key={group.key}
									className={cn(
										"border-b border-border/20 last:border-0",
										canDrag &&
											overGroupKey === group.key &&
											draggingId &&
											"bg-primary/5",
									)}
									onDragOver={(e) => {
										if (!canDrag) return;
										e.preventDefault();
										e.dataTransfer.dropEffect = "move";
										setOverGroupKey(group.key);
									}}
									onDragLeave={(e) => {
										if (!e.currentTarget.contains(e.relatedTarget as Node)) {
											setOverGroupKey(null);
											setOverTaskId(null);
										}
									}}
									onDrop={(e) => handleDrop(e, group, groupTasks, null)}
								>
									<GroupHeader group={group} count={groupTasks.length} />
									{groupTasks.map((task) =>
										renderTaskRow(task, group, groupTasks),
									)}
								</div>
							);
						})
					)}

					{/* ── Pagination button ─────────────────────────────────── */}
					{pagination?.hasMore && (
						<div className="flex justify-center py-4 border-t border-border/20">
							<button
								type="button"
								onClick={pagination.onLoadMore}
								disabled={pagination.isLoadingMore}
								className="rounded-lg border border-border/40 px-4 py-1.5 text-xs font-medium text-muted-foreground hover:border-primary/50 hover:text-primary transition-all duration-150 disabled:opacity-50"
							>
								{pagination.isLoadingMore
									? t("layout.roadmap.loading")
									: t("layout.roadmap.viewMore")}
							</button>
						</div>
					)}

					{/* ── Add task ─────────────────────────────────────────── */}
					{canCreate &&
						defaultStatusId &&
						onCreateTask &&
						taskTypes.length > 0 && (
							<div className="border-t border-border/20 bg-muted/5">
								<AddTaskRow
									taskTypes={taskTypes}
									variant="list"
									onAdd={(title, taskTypeId) => {
										void onCreateTask(defaultStatusId, title, taskTypeId);
									}}
								/>
							</div>
						)}
				</div>
			</div>
		</div>
	);
}

// ─── Nested child rows ────────────────────────────────────────────────────────
interface RoadmapChildRowsProps {
	projectId: string;
	parent: Task;
	depth: number;
	filters: ChildTaskFilters;
	isExpanded: (taskId: string) => boolean;
	renderRow: (task: Task, depth: number) => React.ReactNode;
	leftColWidth: number;
}

/** The children of an expanded timeline row, fetched when it opens with the
 *  view's filters and sort. Recurses for children that have their own. */
function RoadmapChildRows({
	projectId,
	parent,
	depth,
	filters,
	isExpanded,
	renderRow,
	leftColWidth,
}: RoadmapChildRowsProps) {
	const { t } = useTranslation("projects");
	const query = useInfiniteQuery(
		childTasksInfiniteQueryOptions(projectId, parent.id, filters),
	);
	const children = useMemo(
		() => query.data?.pages.flatMap((p) => p.items) ?? [],
		[query.data],
	);
	const note = (text: React.ReactNode) => (
		<div className="flex border-b border-border/10">
			<div
				className="sticky left-0 z-10 flex shrink-0 items-center gap-2 border-r border-border/20 bg-background py-2 pr-4 text-xs text-muted-foreground/70"
				style={{ width: leftColWidth, paddingLeft: 40 + depth * 16 }}
			>
				{text}
			</div>
		</div>
	);

	if (query.isPending) {
		return note(<Loader2 className="size-3 animate-spin" aria-hidden />);
	}
	if (query.isError) {
		return note(
			<>
				{t("board.subtasks.loadError")}
				<button
					type="button"
					onClick={() => query.refetch()}
					className="font-semibold underline underline-offset-2"
				>
					{t("board.subtasks.retry")}
				</button>
			</>,
		);
	}
	return (
		<>
			{children.length === 0 && note(t("board.subtasks.noMatch"))}
			{children.map((child) => (
				<Fragment key={child.id}>
					{renderRow(child, depth)}
					{isExpanded(child.id) && (child.subtask_count ?? 0) > 0 && (
						<RoadmapChildRows
							projectId={projectId}
							parent={child}
							depth={depth + 1}
							filters={filters}
							isExpanded={isExpanded}
							renderRow={renderRow}
							leftColWidth={leftColWidth}
						/>
					)}
				</Fragment>
			))}
			{query.hasNextPage &&
				note(
					<button
						type="button"
						onClick={() => query.fetchNextPage()}
						disabled={query.isFetchingNextPage}
						className="font-semibold hover:text-foreground"
					>
						{t("board.subtasks.loadMore")}
					</button>,
				)}
		</>
	);
}
