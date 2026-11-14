import { useInfiniteQuery } from "@tanstack/react-query";
import { CalendarDays, ChevronRight, LocateFixed } from "lucide-react";
import {
	Fragment,
	useCallback,
	useEffect,
	useLayoutEffect,
	useMemo,
	useRef,
	useState,
} from "react";
import { useTranslation } from "react-i18next";
import { PaneResizeHandle } from "@/components/shared/pane-resize-handle";
import { Spinner } from "@/components/ui/spinner";
import { toast } from "@/components/ui/toast";
import {
	type ChildTaskFilters,
	childTasksInfiniteQueryOptions,
	type GanttZoom,
	type ProjectTaskLink,
	type Sprint,
	type Task,
	type ViewConfig,
} from "@/lib/interaction-api";
import { notifyError } from "@/lib/notify";
import { GANTT_TASK_PANE, usePaneWidth } from "@/lib/pane-width";
import type {
	CustomFieldDefinition,
	ProjectMember,
	TaskStatus,
	TaskType,
} from "@/lib/project-api";
import { cn } from "@/lib/utils";
import { AddTaskRow } from "./add-task-row";
import {
	barBox,
	buildScale,
	chartWidth as chartWidthOf,
	computeRange,
	type DateUpdate,
	type DragMode,
	dayToPx,
	dependencyPath,
	dragDays,
	dragSpan,
	GANTT_ZOOMS,
	localToday,
	MS_PER_DAY,
	PX_PER_DAY,
	rescheduleUpdate,
	resolveGanttZoom,
	type TaskSpan,
	taskSpan,
	violatesBlock,
} from "./gantt-scale";
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
const ROW_HEIGHT = 40;
/** Bars narrower than this get no resize handles: there is no room to aim. */
const MIN_RESIZABLE_PX = 20;

const ZOOM_LABEL_KEYS = {
	day: "layout.gantt.zoom.day",
	week: "layout.gantt.zoom.week",
	month: "layout.gantt.zoom.month",
	quarter: "layout.gantt.zoom.quarter",
	year: "layout.gantt.zoom.year",
} as const satisfies Record<GanttZoom, string>;

// ─── Types ────────────────────────────────────────────────────────────────────
interface GanttViewProps {
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
	/** Saves new dates after a bar is dragged; rejects when the save fails.
	 *  Absent when the user may not edit tasks, which makes the chart read-only. */
	onRescheduleTask?: (taskId: string, update: DateUpdate) => Promise<unknown>;
	/** The time scale; unset means the default zoom. */
	zoom?: GanttZoom;
	/** Remembers a zoom the user picked for this view. */
	onZoomChange?: (zoom: GanttZoom) => void;
	/** Whether "blocks" links are drawn as arrows. */
	showDependencies?: boolean;
	/** The project's "blocks" links; only those between shown bars are drawn. */
	blockLinks?: ProjectTaskLink[];
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

/** An arrow between two shown bars, in chart-body coordinates. */
interface DependencyArrow {
	id: string;
	path: string;
	violated: boolean;
}

interface DependencyLayout {
	arrows: DependencyArrow[];
	/** Blocked task id → titles of the blockers it starts before. */
	violations: Map<string, string[]>;
}

const EMPTY_LAYOUT: DependencyLayout = { arrows: [], violations: new Map() };

/** A bar drag in progress. */
interface BarDrag {
	task: Task;
	span: TaskSpan;
	mode: DragMode;
	originX: number;
	pointerId: number;
	days: number;
	/** Set by Escape: the drag saves nothing, but its click is still eaten. */
	cancelled?: boolean;
}

/** Dates saved optimistically, shown until the task itself catches up. */
interface PendingDates {
	span: TaskSpan;
	/** The task's version when the save started; a newer one wins. */
	version: string;
}

/** Changes whenever the server changes the task. */
function versionOf(task: Task): string {
	return `${task.version ?? ""}|${task.updated_at}`;
}

function fmtDay(day: number, locale: string): string {
	return new Date(day * MS_PER_DAY).toLocaleDateString(locale, {
		month: "short",
		day: "numeric",
		year: "numeric",
		timeZone: "UTC",
	});
}

// ─── Component ────────────────────────────────────────────────────────────────
export function GanttView({
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
	onRescheduleTask,
	zoom: zoomProp,
	onZoomChange,
	showDependencies = true,
	blockLinks,
	onCreateTask,
	onTaskClick,
	pagination,
}: GanttViewProps) {
	const { t, i18n } = useTranslation("projects");
	const { t: tShared } = useTranslation("shared");
	const locale = i18n.language;
	const taskPane = usePaneWidth(GANTT_TASK_PANE);
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
	const canReschedule = canEdit && !!onRescheduleTask;

	const dragStartTimer = useRef<ReturnType<typeof setTimeout> | null>(null);

	// ── Zoom ──────────────────────────────────────────────────────────────────
	// Held locally so a click re-renders at once; the saved value catches up.
	const [zoom, setZoom] = useState<GanttZoom>(() => resolveGanttZoom(zoomProp));
	useEffect(() => setZoom(resolveGanttZoom(zoomProp)), [zoomProp]);
	const pxPerDay = PX_PER_DAY[zoom];

	// Stable "today" — fixed at mount so all bars agree.
	const today = useMemo(() => localToday(), []);

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

	// ── Optimistic reschedules ────────────────────────────────────────────────
	const [pending, setPending] = useState<Record<string, PendingDates>>({});

	/** The span a task is drawn with: a pending save wins until the task
	 *  changes on the server (its version moves on). */
	const spanOf = useCallback(
		(task: Task): TaskSpan | null => {
			const p = pending[task.id];
			if (p && p.version === versionOf(task)) return p.span;
			return taskSpan(task.start_date, task.due_date);
		},
		[pending],
	);

	// ── Timeline geometry ─────────────────────────────────────────────────────
	const range = useMemo(() => {
		const spans: TaskSpan[] = [];
		for (const task of tasks) {
			const s = taskSpan(task.start_date, task.due_date);
			if (s) spans.push(s);
		}
		return computeRange(spans, today, zoom);
	}, [tasks, today, zoom]);
	const chartWidth = chartWidthOf(range, zoom);
	const scale = useMemo(
		() =>
			buildScale(range, zoom, {
				locale,
				quarterLabel: (n) => t("layout.gantt.quarterLabel", { n }),
			}),
		[range, zoom, locale, t],
	);
	const todayPx = dayToPx(today, range, zoom);
	const todayInRange = today >= range.start && today < range.end;

	// ── Scrolling: keep a date in view across scale changes ───────────────────
	const scrollRef = useRef<HTMLDivElement>(null);
	/** A day to center on the next layout: today when the chart first opens. */
	const centerDayRef = useRef<number | null>(today);
	/** The scale the scroll position was last laid out for. */
	const laidOutRef = useRef({ start: range.start, pxPerDay });

	// biome-ignore lint/correctness/useExhaustiveDependencies: runs when the scale changes; everything else it reads is current at that point.
	useLayoutEffect(() => {
		const el = scrollRef.current;
		const prev = laidOutRef.current;
		laidOutRef.current = { start: range.start, pxPerDay };
		if (!el) return;
		const half = (el.clientWidth - leftColW) / 2;
		let day = centerDayRef.current;
		centerDayRef.current = null;
		if (day === null) {
			if (prev.start === range.start && prev.pxPerDay === pxPerDay) return;
			// A new zoom or a wider range: keep the date that was in the middle
			// of the chart there, instead of whatever now sits at the old offset.
			day = prev.start + (el.scrollLeft + half) / prev.pxPerDay;
		}
		el.scrollLeft = Math.max(0, (day - range.start) * pxPerDay - half);
	}, [range.start, pxPerDay]);

	const changeZoom = (next: GanttZoom) => {
		if (next === zoom) return;
		setZoom(next);
		onZoomChange?.(next);
	};

	const jumpToToday = () => {
		const el = scrollRef.current;
		if (!el) return;
		const x = todayPx + pxPerDay / 2;
		const left = Math.max(0, x - (el.clientWidth - leftColW) / 2);
		if (typeof el.scrollTo === "function") {
			el.scrollTo({ left, behavior: "smooth" });
		} else {
			el.scrollLeft = left;
		}
	};

	// ── Bar dragging (reschedule) ─────────────────────────────────────────────
	const barDragRef = useRef<BarDrag | null>(null);
	const [liveDrag, setLiveDrag] = useState<{
		taskId: string;
		span: TaskSpan;
	} | null>(null);
	/** Swallows the click that ends a bar drag, so it doesn't open the task. */
	const suppressClickRef = useRef(false);
	/** Latest task object per id (top-level and nested), for undo. */
	const latestTasks = useRef(new Map<string, Task>());

	const cancelBarDrag = useCallback(() => {
		barDragRef.current = null;
		setLiveDrag(null);
	}, []);

	const isBarDragging = liveDrag !== null;
	useEffect(() => {
		if (!isBarDragging) return;
		const onKey = (e: KeyboardEvent) => {
			if (e.key !== "Escape" || !barDragRef.current) return;
			barDragRef.current.cancelled = true;
			setLiveDrag(null);
		};
		window.addEventListener("keydown", onKey);
		return () => window.removeEventListener("keydown", onKey);
	}, [isBarDragging]);

	/** Saves `update` for `task`, drawing `span` until the task catches up. */
	const saveDates = (task: Task, span: TaskSpan, update: DateUpdate) => {
		if (!onRescheduleTask) return Promise.resolve(false);
		const version = versionOf(task);
		setPending((prev) => ({ ...prev, [task.id]: { span, version } }));
		const dropPending = () =>
			setPending((prev) => {
				if (prev[task.id]?.version !== version) return prev;
				const { [task.id]: _gone, ...rest } = prev;
				return rest;
			});
		return onRescheduleTask(task.id, update).then(
			() => true,
			(err: unknown) => {
				dropPending();
				notifyError(err, t("layout.gantt.rescheduleFailed"));
				return false;
			},
		);
	};

	const commitDrag = (drag: BarDrag) => {
		const next = dragSpan(drag.span, drag.mode, drag.days);
		const update = rescheduleUpdate(drag.span, next, drag.mode);
		if (Object.keys(update).length === 0) return;
		const { task } = drag;
		const nextSpan: TaskSpan = {
			...next,
			hasStart: drag.span.hasStart || "start_date" in update,
			hasDue: drag.span.hasDue || "due_date" in update,
		};
		void saveDates(task, nextSpan, update).then((saved) => {
			if (!saved) return;
			// Undo puts back exactly what the drag changed.
			const undo: DateUpdate = {};
			if ("start_date" in update) undo.start_date = task.start_date ?? null;
			if ("due_date" in update) undo.due_date = task.due_date ?? null;
			toast.add({
				type: "success",
				title: t("layout.gantt.rescheduled", { title: task.title }),
				actionProps: {
					children: t("layout.gantt.undo"),
					onClick: () => {
						const current = latestTasks.current.get(task.id) ?? task;
						void saveDates(current, drag.span, undo);
					},
				},
			});
		});
	};

	const startBarDrag = (
		e: React.PointerEvent,
		task: Task,
		span: TaskSpan,
		mode: DragMode,
	) => {
		if (!canReschedule || e.button !== 0) return;
		e.stopPropagation();
		e.preventDefault();
		(e.currentTarget as Element).setPointerCapture?.(e.pointerId);
		barDragRef.current = {
			task,
			span,
			mode,
			originX: e.clientX,
			pointerId: e.pointerId,
			days: 0,
		};
	};

	const moveBarDrag = (e: React.PointerEvent) => {
		const drag = barDragRef.current;
		if (!drag || drag.cancelled || e.pointerId !== drag.pointerId) return;
		const days = dragDays(e.clientX - drag.originX, zoom);
		if (days === drag.days) return;
		drag.days = days;
		setLiveDrag({
			taskId: drag.task.id,
			span: dragSpan(drag.span, drag.mode, days),
		});
	};

	const endBarDrag = (e: React.PointerEvent) => {
		const drag = barDragRef.current;
		if (!drag || e.pointerId !== drag.pointerId) return;
		cancelBarDrag();
		if (drag.days === 0 && !drag.cancelled) return;
		suppressClickRef.current = true;
		// The click that follows pointerup fires before any timer.
		setTimeout(() => {
			suppressClickRef.current = false;
		}, 0);
		if (!drag.cancelled) commitDrag(drag);
	};

	// ── Dependency arrows ─────────────────────────────────────────────────────
	// Bars are measured in the DOM: nested rows load on their own, so only the
	// rendered chart knows where every bar ended up.
	const bodyRef = useRef<HTMLDivElement>(null);
	const [deps, setDeps] = useState<DependencyLayout>(EMPTY_LAYOUT);
	const depsKey = useRef("");
	const links = useMemo(
		() => (showDependencies ? (blockLinks ?? []) : []),
		[showDependencies, blockLinks],
	);

	const measureDependencies = useCallback(() => {
		const root = bodyRef.current;
		let layout = EMPTY_LAYOUT;
		if (root && links.length > 0) {
			const rootRect = root.getBoundingClientRect();
			const bars = new Map<
				string,
				{ rect: DOMRect; span: TaskSpan; title: string }
			>();
			for (const el of root.querySelectorAll<HTMLElement>("[data-gantt-bar]")) {
				const id = el.dataset.ganttBar as string;
				if (bars.has(id)) continue; // a task shown in two groups: use the first
				bars.set(id, {
					rect: el.getBoundingClientRect(),
					span: {
						start: Number(el.dataset.start),
						end: Number(el.dataset.end),
						hasStart: true,
						hasDue: true,
					},
					title: el.dataset.title ?? "",
				});
			}
			const arrows: DependencyArrow[] = [];
			const violations = new Map<string, string[]>();
			for (const link of links) {
				const from = bars.get(link.source_task_id);
				const to = bars.get(link.target_task_id);
				if (!from || !to) continue;
				const violated = violatesBlock(from.span, to.span);
				if (violated) {
					const list = violations.get(link.target_task_id) ?? [];
					list.push(from.title);
					violations.set(link.target_task_id, list);
				}
				arrows.push({
					id: link.id,
					violated,
					path: dependencyPath(
						{
							x: from.rect.right - rootRect.left,
							y: from.rect.top + from.rect.height / 2 - rootRect.top,
						},
						{
							x: to.rect.left - rootRect.left,
							y: to.rect.top + to.rect.height / 2 - rootRect.top,
						},
					),
				});
			}
			layout = { arrows, violations };
		}
		const key = JSON.stringify([layout.arrows, [...layout.violations]]);
		if (key === depsKey.current) return;
		depsKey.current = key;
		setDeps(layout);
	}, [links]);

	// Every render can move a bar (a drag, a zoom, rows opening), so measure
	// after each; the key check above stops it from looping.
	useLayoutEffect(() => {
		measureDependencies();
	});

	// Nested rows render on their own when their children load: watch the size.
	useEffect(() => {
		const root = bodyRef.current;
		if (!root || typeof ResizeObserver === "undefined") return;
		const observer = new ResizeObserver(() => measureDependencies());
		observer.observe(root);
		return () => observer.disconnect();
	}, [measureDependencies]);

	// ── Bar cell ──────────────────────────────────────────────────────────────
	/** The chart part of a row. A plain function, not a component, so a bar
	 *  being dragged is never remounted (which would drop pointer capture). */
	function renderChartCell(task: Task) {
		const drawn = liveDrag?.taskId === task.id ? liveDrag.span : spanOf(task);
		if (!drawn) {
			return (
				<div
					className="relative shrink-0"
					style={{ width: chartWidth, height: ROW_HEIGHT }}
				>
					<div
						title={t("layout.gantt.noDatesSet")}
						className="absolute top-1/2 h-5 w-20 -translate-y-1/2 rounded border border-dashed border-muted-foreground/20"
						style={{ left: Math.max(0, todayPx - 40) }}
					/>
				</div>
			);
		}

		const box = barBox(drawn, range, zoom);
		const singleDate = !drawn.hasStart || !drawn.hasDue;
		const overdue = drawn.hasDue && drawn.end < today;
		const blockers = deps.violations.get(task.id);
		const isDragging = liveDrag?.taskId === task.id;
		const rangeLabel = t("layout.gantt.dateRangeTooltip", {
			start: fmtDay(drawn.start, locale),
			end: fmtDay(drawn.end, locale),
		});
		let tooltip: string;
		if (drawn.hasStart && drawn.hasDue) {
			tooltip = rangeLabel;
		} else if (drawn.hasStart) {
			tooltip = t("layout.gantt.startDateTooltip", {
				date: fmtDay(drawn.start, locale),
			});
		} else {
			tooltip = t("layout.gantt.dueDateTooltip", {
				date: fmtDay(drawn.end, locale),
			});
		}
		if (blockers) {
			tooltip = [
				tooltip,
				...blockers.map((title) =>
					t("layout.gantt.startsBeforeBlocker", { title }),
				),
			].join("\n");
		}
		// Keep the handles mounted while dragging: unmounting the element that
		// holds pointer capture would drop the drag when the bar gets narrow.
		const resizable =
			canReschedule && (isDragging || box.width >= MIN_RESIZABLE_PX);

		return (
			<div
				className="relative shrink-0"
				style={{ width: chartWidth, height: ROW_HEIGHT }}
			>
				<div
					data-gantt-bar={task.id}
					data-start={drawn.start}
					data-end={drawn.end}
					data-title={task.title}
					data-violation={blockers ? "true" : undefined}
					title={isDragging ? undefined : tooltip}
					className={cn(
						"absolute top-1/2 h-6 -translate-y-1/2 rounded-full shadow-sm",
						"opacity-80 group-hover:opacity-100 transition-opacity duration-150",
						overdue ? "bg-destructive/55" : "bg-primary/55",
						singleDate && "border border-primary/50 bg-primary/30",
						blockers && "ring-2 ring-destructive/70",
						canReschedule &&
							"cursor-grab touch-none select-none active:cursor-grabbing",
						isDragging && "opacity-100 ring-2 ring-primary/60",
					)}
					style={{ left: box.left, width: box.width }}
					onPointerDown={(e) => startBarDrag(e, task, drawn, "move")}
					onPointerMove={moveBarDrag}
					onPointerUp={endBarDrag}
					onPointerCancel={cancelBarDrag}
				>
					{resizable && (
						<>
							<span
								aria-hidden
								data-gantt-handle="start"
								title={t("layout.gantt.resizeStart")}
								className="absolute inset-y-0 left-0 w-2 cursor-ew-resize rounded-l-full hover:bg-primary/40"
								onPointerDown={(e) => startBarDrag(e, task, drawn, "start")}
							/>
							<span
								aria-hidden
								data-gantt-handle="end"
								title={t("layout.gantt.resizeEnd")}
								className="absolute inset-y-0 right-0 w-2 cursor-ew-resize rounded-r-full hover:bg-primary/40"
								onPointerDown={(e) => startBarDrag(e, task, drawn, "end")}
							/>
						</>
					)}
					{isDragging && (
						<span
							role="status"
							className="pointer-events-none absolute bottom-full left-0 mb-1.5 whitespace-nowrap rounded-md border border-border/40 bg-popover px-2 py-0.5 text-[11px] font-medium text-popover-foreground shadow-sm"
						>
							{rangeLabel}
						</span>
					)}
				</div>
			</div>
		);
	}

	// ── Task row ──────────────────────────────────────────────────────────────
	/** The clickable row (name column + bar). `depth` > 0 marks a nested child. */
	function renderRowBody(task: Task, depth: number) {
		latestTasks.current.set(task.id, task);
		const type = taskTypes.find((tt) => tt.id === task.task_type_id) ?? null;
		const count = task.subtask_count ?? 0;
		const isOpen = expansion.isExpanded(task.id);
		return (
			// biome-ignore lint/a11y/useSemanticElements: the chevron is a real button and cannot nest inside one
			<div
				role="button"
				tabIndex={0}
				className="group flex w-full cursor-pointer border-b border-border/10 text-left last:border-0"
				onClick={() => {
					if (suppressClickRef.current) {
						suppressClickRef.current = false;
						return;
					}
					onTaskClick(task);
				}}
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
				{renderChartCell(task)}
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
						// A bar drag reschedules; it must not also pick up the row.
						if (barDragRef.current) {
							e.preventDefault();
							return;
						}
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
						<GanttChildRows
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
	function renderGroupHeader(group: ColumnGroupDef, count: number) {
		return (
			// Opaque, so the gridlines behind the rows don't run through it.
			<div className="relative bg-background">
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
			</div>
		);
	}

	// ─── Render ────────────────────────────────────────────────────────────────
	const hasMajorBand = scale.major.length > 0;
	return (
		<div className="flex h-full flex-col overflow-hidden">
			{/* ── Toolbar: zoom and jump to today ─────────────────────── */}
			<div className="flex shrink-0 items-center gap-2 border-b border-border/20 px-4 py-1.5">
				<fieldset
					aria-label={t("layout.gantt.zoomLabel")}
					className="flex items-center rounded-lg border border-border/40 p-0.5"
				>
					{GANTT_ZOOMS.map((z) => (
						<button
							key={z}
							type="button"
							aria-pressed={zoom === z}
							onClick={() => changeZoom(z)}
							className={cn(
								"rounded-md px-2.5 py-0.5 text-xs font-medium transition-colors duration-100",
								zoom === z
									? "bg-primary/12 text-primary"
									: "text-muted-foreground hover:bg-muted/50 hover:text-foreground",
							)}
						>
							{t(ZOOM_LABEL_KEYS[z])}
						</button>
					))}
				</fieldset>
				<button
					type="button"
					onClick={jumpToToday}
					title={t("layout.gantt.jumpToToday")}
					className="flex items-center gap-1.5 rounded-lg border border-border/40 px-2.5 py-1 text-xs font-medium text-muted-foreground transition-colors duration-100 hover:border-primary/50 hover:text-primary"
				>
					<LocateFixed className="size-3.5" />
					{t("layout.gantt.today")}
				</button>
			</div>

			<div ref={scrollRef} className="flex-1 overflow-auto">
				{/*
				 * Single scrollable container. minWidth triggers horizontal scroll.
				 * Left column cells are sticky left-0; header row is sticky top-0.
				 */}
				<div
					ref={bodyRef}
					className="relative"
					style={{ minWidth: leftColW + chartWidth }}
				>
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
								{t("layout.gantt.taskColumnLabel")}
							</span>
							<PaneResizeHandle
								config={GANTT_TASK_PANE}
								pane={taskPane}
								label={tShared("paneResize.taskColumn")}
								edge="right"
								showGrip
								testId="gantt-task-column-resize-handle"
							/>
						</div>

						{/* Scale: a coarse band above one cell per step */}
						<div
							className="relative overflow-hidden"
							style={{ width: chartWidth }}
						>
							{hasMajorBand &&
								scale.major.map((c) => (
									<div
										key={c.key}
										className="absolute top-0 flex h-1/2 items-center overflow-hidden border-r border-border/25 px-2"
										style={{ left: c.left, width: c.width }}
									>
										<span className="whitespace-nowrap text-[11px] font-semibold text-muted-foreground/60">
											{c.label}
										</span>
									</div>
								))}
							{scale.minor.map((c) => (
								<div
									key={c.key}
									className={cn(
										"absolute flex items-center justify-center overflow-hidden border-r border-border/15",
										hasMajorBand ? "bottom-0 h-1/2" : "inset-y-0",
									)}
									style={{ left: c.left, width: c.width }}
								>
									<span className="whitespace-nowrap text-[11px] font-medium text-muted-foreground/50">
										{c.label}
									</span>
								</div>
							))}

							{/* Today indicator: ring dot + vertical line */}
							{todayInRange && (
								<div
									className="absolute inset-y-0 z-10 flex flex-col items-center"
									style={{
										left: todayPx + pxPerDay / 2,
										transform: "translateX(-50%)",
									}}
								>
									<div className="mt-0.5 size-2 shrink-0 rounded-full bg-primary ring-2 ring-primary/25" />
									<div className="w-px flex-1 bg-primary/70" />
								</div>
							)}
						</div>
					</div>

					{/* ── Rows, over gridlines and a today line drawn once ───── */}
					<div className="relative">
						<div
							aria-hidden
							className="pointer-events-none absolute inset-y-0"
							style={{ left: leftColW, width: chartWidth }}
						>
							{scale.minor.map((c) => (
								<div
									key={c.key}
									className="absolute inset-y-0 w-px bg-border/12"
									style={{ left: c.left }}
								/>
							))}
							{todayInRange && (
								<div
									className="absolute inset-y-0 w-0.5 bg-primary/18"
									style={{
										left: todayPx + pxPerDay / 2,
										transform: "translateX(-50%)",
									}}
								/>
							)}
						</div>

						{/* ── Content ────────────────────────────────────────────── */}
						{tasks.length === 0 ? (
							<div className="flex flex-col items-center py-20 text-muted-foreground/40">
								<CalendarDays className="mb-2 size-7" />
								<p className="text-sm font-medium">
									{t("layout.gantt.noTasksToDisplay")}
								</p>
							</div>
						) : (
							groupDefs.map((group) => {
								const groupTasks = tasks.filter((task) =>
									getTaskColumnKeys(task, columnBy, viewCtx).includes(
										group.key,
									),
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
										{renderGroupHeader(group, groupTasks.length)}
										{groupTasks.map((task) =>
											renderTaskRow(task, group, groupTasks),
										)}
									</div>
								);
							})
						)}
					</div>

					{/* ── Dependency arrows ("blocks" links) ─────────────── */}
					{deps.arrows.length > 0 && (
						<svg
							aria-hidden="true"
							data-testid="gantt-dependencies"
							className="pointer-events-none absolute inset-0 z-[5] overflow-visible"
							width="100%"
							height="100%"
						>
							<defs>
								<marker
									id="gantt-arrow"
									viewBox="0 0 8 8"
									refX="7"
									refY="4"
									markerWidth="7"
									markerHeight="7"
									orient="auto"
								>
									<path
										d="M0,0 L8,4 L0,8 z"
										className="fill-muted-foreground/70"
									/>
								</marker>
								<marker
									id="gantt-arrow-violated"
									viewBox="0 0 8 8"
									refX="7"
									refY="4"
									markerWidth="7"
									markerHeight="7"
									orient="auto"
								>
									<path d="M0,0 L8,4 L0,8 z" className="fill-destructive" />
								</marker>
							</defs>
							{deps.arrows.map((a) => (
								<path
									key={a.id}
									d={a.path}
									data-violation={a.violated ? "true" : undefined}
									fill="none"
									strokeWidth={1.5}
									className={
										a.violated
											? "stroke-destructive"
											: "stroke-muted-foreground/60"
									}
									markerEnd={`url(#${a.violated ? "gantt-arrow-violated" : "gantt-arrow"})`}
								/>
							))}
						</svg>
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
									? t("layout.gantt.loading")
									: t("layout.gantt.viewMore")}
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
interface GanttChildRowsProps {
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
function GanttChildRows({
	projectId,
	parent,
	depth,
	filters,
	isExpanded,
	renderRow,
	leftColWidth,
}: GanttChildRowsProps) {
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
		return note(<Spinner aria-hidden="true" className="size-3" />);
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
						<GanttChildRows
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
