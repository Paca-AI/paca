import { keepPreviousData, useInfiniteQuery } from "@tanstack/react-query";
import {
	CalendarOff,
	ChevronLeft,
	ChevronRight,
	LocateFixed,
	Plus,
	RotateCw,
} from "lucide-react";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import {
	Popover,
	PopoverContent,
	PopoverTrigger,
} from "@/components/ui/popover";
import { Spinner } from "@/components/ui/spinner";
import { toast } from "@/components/ui/toast";
import {
	type CalendarMode,
	type ListTasksOptions,
	listAllTasks,
	type Task,
	type ViewConfig,
} from "@/lib/interaction-api";
import { notifyError } from "@/lib/notify";
import type { TaskStatus, TaskType } from "@/lib/project-api";
import { cn } from "@/lib/utils";
import {
	buildGrid,
	CALENDAR_MODES,
	dayToDateParam,
	firstDayOfWeek,
	fullDateLabel,
	gridTitle,
	hiddenOnDay,
	inAnchorMonth,
	layoutRow,
	placementSpan,
	type RowSegment,
	resolveCalendarDateField,
	resolveCalendarMode,
	scheduleOnDay,
	segmentsOnDay,
	shiftAnchor,
	shiftSpan,
	visibleLanes,
	weekdayLabels,
} from "./calendar-scale";
import {
	type DateUpdate,
	dragSpan,
	localToday,
	MS_PER_DAY,
	rescheduleUpdate,
	type TaskSpan,
	taskSpan,
} from "./gantt-scale";
import { TaskTypeSelector } from "./task-type-selector";
import type { TaskFieldUpdate } from "./view-utils";

// ─── Layout constants ─────────────────────────────────────────────────────────
/** Height of a day's header (its date and "+" button). */
const DAY_HEADER_PX = 28;
/** Height of one lane of task bars, gap included. */
const LANE_PX = 22;
/** Lanes a month row is assumed to fit before it has been measured. */
const UNMEASURED_MONTH_LANES = 3;
/** A month row's least height (min-h-24), when the grid has no room left. */
const MIN_MONTH_ROW_PX = 96;
/** The API's largest page: the calendar loads every task of its window. */
const WINDOW_PAGE_SIZE = 200;
const UNDATED_PAGE_SIZE = 50;

const MODE_LABEL_KEYS = {
	month: "layout.calendar.mode.month",
	week: "layout.calendar.mode.week",
	day: "layout.calendar.mode.day",
} as const satisfies Record<CalendarMode, string>;

// ─── Types ────────────────────────────────────────────────────────────────────
interface CalendarViewProps {
	projectId: string;
	/** The project's task ID prefix (e.g. "PACA"); IDs fall back to "#n". */
	taskIdPrefix?: string;
	/** The view's filters, without paging or sorting; the calendar narrows
	 *  them to the days on screen. */
	taskFilters: ListTasksOptions;
	statuses: TaskStatus[];
	/** The task types a new task can have. */
	taskTypes: TaskType[];
	viewConfig?: ViewConfig;
	canCreate?: boolean;
	canEdit?: boolean;
	/** Saves new dates after a drag; rejects when the save fails. Absent when
	 *  the user may not edit tasks, which makes the calendar read-only. */
	onRescheduleTask?: (taskId: string, update: DateUpdate) => Promise<unknown>;
	/** The grid; unset means the default mode. */
	mode?: CalendarMode;
	/** Remembers a mode the user picked for this view. */
	onModeChange?: (mode: CalendarMode) => void;
	onCreateTask?: (
		statusId: string,
		title: string,
		taskTypeId?: string | null,
		extraFields?: TaskFieldUpdate,
	) => Promise<void>;
	onTaskClick: (task: Task) => void;
}

/** What a drag does: move the task, move one of its ends, or give an undated
 *  task a date. */
type DragKind = "move" | "start" | "end" | "schedule";

interface CalendarDrag {
	task: Task;
	kind: DragKind;
	/** The days the task is shown on when the drag starts (null: undated). */
	span: TaskSpan | null;
	/** The day under the pointer when the drag started. */
	grabDay: number;
}

type TaskDates = Pick<Task, "start_date" | "due_date">;

/** Dates saved optimistically, shown until the task itself catches up. */
interface PendingDates {
	task: Task;
	dates: TaskDates;
	/** The task's version when the save started; a newer one wins. */
	version: string;
	/** When the save succeeded; the entry goes once the lists refetched. */
	savedAt?: number;
}

/** Changes whenever the server changes the task. */
function versionOf(task: Task): string {
	return `${task.version ?? ""}|${task.updated_at}`;
}

function taskKey(task: Task, prefix: string): string {
	return prefix ? `${prefix}-${task.task_number}` : `#${task.task_number}`;
}

function fmtDay(day: number, locale: string): string {
	return new Date(day * MS_PER_DAY).toLocaleDateString(locale, {
		month: "short",
		day: "numeric",
		year: "numeric",
		timeZone: "UTC",
	});
}

/** The days a drag would put the task on if dropped on `day`. */
function previewSpan(drag: CalendarDrag, day: number): TaskSpan | null {
	switch (drag.kind) {
		case "schedule":
			return { start: day, end: day, hasStart: true, hasDue: true };
		case "move":
			return drag.span ? shiftSpan(drag.span, day - drag.grabDay) : null;
		case "start":
			return drag.span
				? dragSpan(drag.span, "start", day - drag.span.start)
				: null;
		case "end":
			return drag.span ? dragSpan(drag.span, "end", day - drag.span.end) : null;
	}
}

/** Loads every page of a task list, so nothing on screen is missing. */
function useAllPages(query: {
	hasNextPage: boolean;
	isFetchingNextPage: boolean;
	isFetchNextPageError: boolean;
	isPlaceholderData: boolean;
	fetchNextPage: () => unknown;
}) {
	const {
		hasNextPage,
		isFetchingNextPage,
		isFetchNextPageError,
		isPlaceholderData,
		fetchNextPage,
	} = query;
	useEffect(() => {
		if (
			hasNextPage &&
			!isFetchingNextPage &&
			!isFetchNextPageError &&
			!isPlaceholderData
		) {
			void fetchNextPage();
		}
	}, [
		hasNextPage,
		isFetchingNextPage,
		isFetchNextPageError,
		isPlaceholderData,
		fetchNextPage,
	]);
}

// ─── Component ────────────────────────────────────────────────────────────────
export function CalendarView({
	projectId,
	taskIdPrefix = "",
	taskFilters,
	statuses,
	taskTypes,
	viewConfig,
	canCreate = false,
	canEdit = false,
	onRescheduleTask,
	mode: modeProp,
	onModeChange,
	onCreateTask,
	onTaskClick,
}: CalendarViewProps) {
	const { t, i18n } = useTranslation("projects");
	const locale = i18n.language;
	const dateField = resolveCalendarDateField(viewConfig?.calendar_date_field);
	const canReschedule = canEdit && !!onRescheduleTask;
	const canAdd = canCreate && !!onCreateTask;

	// ── Mode and position ─────────────────────────────────────────────────────
	// Held locally so a click re-renders at once; the saved value catches up.
	const [mode, setMode] = useState<CalendarMode>(() =>
		resolveCalendarMode(modeProp),
	);
	useEffect(() => setMode(resolveCalendarMode(modeProp)), [modeProp]);
	// Stable "today" — fixed at mount so every cell agrees.
	const today = useMemo(() => localToday(), []);
	const [anchor, setAnchor] = useState(today);
	const firstDay = useMemo(() => firstDayOfWeek(locale), [locale]);
	const grid = useMemo(
		() => buildGrid(anchor, mode, firstDay),
		[anchor, mode, firstDay],
	);
	const title = gridTitle(grid, anchor, mode, locale);

	/** The day that takes the grid's keyboard focus. */
	const [focusedDay, setFocusedDay] = useState(today);
	const activeDay =
		focusedDay >= grid.start && focusedDay < grid.end ? focusedDay : anchor;

	const changeMode = (next: CalendarMode) => {
		if (next === mode) return;
		setMode(next);
		// Keep the day in focus on screen in the new mode.
		setAnchor(activeDay);
		onModeChange?.(next);
	};
	const step = (direction: -1 | 1) => {
		const next = shiftAnchor(anchor, mode, direction);
		setAnchor(next);
		setFocusedDay(next);
	};
	const goToday = () => {
		setAnchor(today);
		setFocusedDay(today);
	};

	// ── Data: the tasks of the window, and the undated ones ───────────────────
	const scheduledBy = dateField === "start_and_due" ? undefined : dateField;
	const windowOpts = useMemo(
		(): ListTasksOptions => ({
			...taskFilters,
			scheduledFrom: dayToDateParam(grid.start),
			scheduledTo: dayToDateParam(grid.end - 1),
			scheduledBy,
			pageSize: WINDOW_PAGE_SIZE,
		}),
		[taskFilters, grid.start, grid.end, scheduledBy],
	);
	const windowQuery = useInfiniteQuery({
		queryKey: ["projects", projectId, "tasks", "calendar", windowOpts],
		queryFn: ({ pageParam }: { pageParam: string | undefined }) =>
			listAllTasks(projectId, { ...windowOpts, cursor: pageParam }),
		initialPageParam: undefined as string | undefined,
		getNextPageParam: (last) => last.next_cursor ?? undefined,
		// Moving to the next month keeps the tasks already loaded on screen
		// (they sit on their own dates) until the new window arrives.
		placeholderData: keepPreviousData,
		staleTime: 15_000,
	});
	useAllPages(windowQuery);

	const undatedOpts = useMemo(
		(): ListTasksOptions => ({
			...taskFilters,
			unscheduled: true,
			scheduledBy,
			pageSize: UNDATED_PAGE_SIZE,
		}),
		[taskFilters, scheduledBy],
	);
	const undatedQuery = useInfiniteQuery({
		queryKey: ["projects", projectId, "tasks", "calendar-undated", undatedOpts],
		queryFn: ({ pageParam }: { pageParam: string | undefined }) =>
			listAllTasks(projectId, { ...undatedOpts, cursor: pageParam }),
		initialPageParam: undefined as string | undefined,
		getNextPageParam: (last) => last.next_cursor ?? undefined,
		staleTime: 15_000,
	});
	const undatedTotal = undatedQuery.data?.pages[0]?.total_count;

	// ── Optimistic dates ──────────────────────────────────────────────────────
	const [pending, setPending] = useState<Record<string, PendingDates>>({});

	// A saved entry goes once both lists have refetched after the save: from
	// then on they show the task where the server has it.
	const windowUpdatedAt = windowQuery.dataUpdatedAt;
	const undatedUpdatedAt = undatedQuery.dataUpdatedAt;
	useEffect(() => {
		setPending((prev) => {
			let changed = false;
			const next: Record<string, PendingDates> = {};
			for (const [id, p] of Object.entries(prev)) {
				if (
					p.savedAt !== undefined &&
					windowUpdatedAt > p.savedAt &&
					undatedUpdatedAt > p.savedAt
				) {
					changed = true;
					continue;
				}
				next[id] = p;
			}
			return changed ? next : prev;
		});
	}, [windowUpdatedAt, undatedUpdatedAt]);

	/** A loaded task as it should be shown: a pending save wins until the
	 *  server changes the task (its version moves on). */
	const withPending = useCallback(
		(task: Task): Task => {
			const p = pending[task.id];
			return p && p.version === versionOf(task)
				? { ...task, ...p.dates }
				: task;
		},
		[pending],
	);

	const windowTasks = useMemo(() => {
		const out: Task[] = [];
		const seen = new Set<string>();
		for (const page of windowQuery.data?.pages ?? []) {
			for (const task of page.items) {
				if (seen.has(task.id)) continue;
				seen.add(task.id);
				out.push(withPending(task));
			}
		}
		// A task given a date here shows before the window refetches.
		for (const p of Object.values(pending)) {
			if (!seen.has(p.task.id)) out.push({ ...p.task, ...p.dates });
		}
		return out;
	}, [windowQuery.data, pending, withPending]);

	const undatedTasks = useMemo(() => {
		const out: Task[] = [];
		const seen = new Set<string>();
		for (const page of undatedQuery.data?.pages ?? []) {
			for (const task of page.items) {
				if (seen.has(task.id)) continue;
				seen.add(task.id);
				const shown = withPending(task);
				if (!placementSpan(shown, dateField)) out.push(shown);
			}
		}
		// A task whose date was taken away here (an undo) shows at once.
		for (const p of Object.values(pending)) {
			const shown = { ...p.task, ...p.dates };
			if (!seen.has(p.task.id) && !placementSpan(shown, dateField)) {
				out.push(shown);
			}
		}
		return out;
	}, [undatedQuery.data, pending, withPending, dateField]);

	/** Latest copy of each task, so an undo starts from what is shown. */
	const latestTasks = useRef(new Map<string, Task>());
	for (const task of windowTasks) latestTasks.current.set(task.id, task);
	for (const task of undatedTasks) latestTasks.current.set(task.id, task);

	/** Saves `update` for `task`, showing the new dates until it catches up. */
	const saveDates = (task: Task, update: DateUpdate) => {
		if (!onRescheduleTask) return Promise.resolve(false);
		const version = versionOf(task);
		const dates: TaskDates = {
			start_date: task.start_date,
			due_date: task.due_date,
			...update,
		};
		setPending((prev) => ({ ...prev, [task.id]: { task, dates, version } }));
		return onRescheduleTask(task.id, update).then(
			() => {
				setPending((prev) => {
					const p = prev[task.id];
					if (p?.version !== version) return prev;
					return { ...prev, [task.id]: { ...p, savedAt: Date.now() } };
				});
				return true;
			},
			(err: unknown) => {
				setPending((prev) => {
					if (prev[task.id]?.version !== version) return prev;
					const { [task.id]: _gone, ...rest } = prev;
					return rest;
				});
				notifyError(err, t("layout.calendar.rescheduleFailed"));
				return false;
			},
		);
	};

	/** Saves `update` and offers to undo exactly what it changed. */
	const reschedule = (task: Task, update: DateUpdate) => {
		if (Object.keys(update).length === 0) return;
		void saveDates(task, update).then((saved) => {
			if (!saved) return;
			const undo: DateUpdate = {};
			if ("start_date" in update) undo.start_date = task.start_date ?? null;
			if ("due_date" in update) undo.due_date = task.due_date ?? null;
			toast.add({
				type: "success",
				title: t("layout.calendar.rescheduled", { title: task.title }),
				actionProps: {
					children: t("layout.calendar.undo"),
					onClick: () => {
						const current = latestTasks.current.get(task.id) ?? task;
						void saveDates(current, undo);
					},
				},
			});
		});
	};

	// ── Dragging ──────────────────────────────────────────────────────────────
	const [drag, setDrag] = useState<CalendarDrag | null>(null);
	const dragRef = useRef<CalendarDrag | null>(null);
	const [overDay, setOverDay] = useState<number | null>(null);
	const preview = drag && overDay !== null ? previewSpan(drag, overDay) : null;

	const startDrag = (e: React.DragEvent, next: CalendarDrag) => {
		if (!canReschedule) return;
		e.stopPropagation();
		e.dataTransfer.effectAllowed = "move";
		e.dataTransfer.setData("text/plain", next.task.id);
		dragRef.current = next;
		// Deferred: changing the DOM inside dragstart can cancel the drag.
		setTimeout(() => {
			if (dragRef.current === next) setDrag(next);
		}, 0);
	};

	const endDrag = () => {
		dragRef.current = null;
		setDrag(null);
		setOverDay(null);
	};

	const dropOnDay = (day: number) => {
		const current = dragRef.current;
		endDrag();
		if (!current) return;
		const { task, kind, span } = current;
		if (kind === "schedule") {
			reschedule(task, scheduleOnDay(day, dateField));
			return;
		}
		const full = taskSpan(task.start_date, task.due_date);
		if (!full || !span) return;
		if (kind === "move") {
			// Every date the task has moves by the same number of days, so it
			// keeps its length whichever date places it.
			const days = day - current.grabDay;
			reschedule(task, rescheduleUpdate(full, shiftSpan(full, days), "move"));
			return;
		}
		// Edge drags exist only where a task spans its two dates.
		const after = previewSpan(current, day);
		if (after) reschedule(task, rescheduleUpdate(full, after, kind));
	};

	// ── Month rows: how many lanes fit ────────────────────────────────────────
	const bodyRef = useRef<HTMLDivElement>(null);
	const [bodyHeight, setBodyHeight] = useState(0);
	useEffect(() => {
		const el = bodyRef.current;
		if (!el || typeof ResizeObserver === "undefined") return;
		const observer = new ResizeObserver(() => setBodyHeight(el.clientHeight));
		observer.observe(el);
		setBodyHeight(el.clientHeight);
		return () => observer.disconnect();
	}, []);
	const laneFit =
		mode !== "month"
			? Number.POSITIVE_INFINITY
			: bodyHeight > 0
				? Math.floor(
						(Math.max(bodyHeight / grid.rows.length, MIN_MONTH_ROW_PX) -
							DAY_HEADER_PX -
							4) /
							LANE_PX,
					)
				: UNMEASURED_MONTH_LANES;

	// ── Placement ─────────────────────────────────────────────────────────────
	const placed = useMemo(() => {
		const out: { item: Task; span: TaskSpan }[] = [];
		for (const task of windowTasks) {
			const span = placementSpan(task, dateField);
			if (span) out.push({ item: task, span });
		}
		return out;
	}, [windowTasks, dateField]);
	const rows = useMemo(
		() =>
			grid.rows.map((rowStart) => ({
				rowStart,
				...layoutRow(placed, rowStart, grid.columns),
			})),
		[grid, placed],
	);

	const statusById = useMemo(
		() => new Map(statuses.map((s) => [s.id, s])),
		[statuses],
	);
	const typeById = useMemo(
		() => new Map(taskTypes.map((tt) => [tt.id, tt])),
		[taskTypes],
	);
	const defaultStatusId = useMemo(
		() =>
			statuses.find((s) => s.is_default)?.id ??
			[...statuses].sort((a, b) => a.position - b.position)[0]?.id ??
			"",
		[statuses],
	);

	// ── Keyboard navigation ───────────────────────────────────────────────────
	const gridRef = useRef<HTMLDivElement>(null);
	const focusAfterMove = useRef(false);
	const [quickAddDay, setQuickAddDay] = useState<number | null>(null);

	useEffect(() => {
		if (!focusAfterMove.current) return;
		focusAfterMove.current = false;
		gridRef.current
			?.querySelector<HTMLElement>(`[data-calendar-day="${activeDay}"]`)
			?.focus();
	}, [activeDay]);

	const onGridKeyDown = (e: React.KeyboardEvent, day: number) => {
		if (e.target !== e.currentTarget) return;
		const rowStart = day - ((day - grid.start) % grid.columns);
		let next: number | null = null;
		switch (e.key) {
			case "ArrowLeft":
				next = day - 1;
				break;
			case "ArrowRight":
				next = day + 1;
				break;
			case "ArrowUp":
				next = day - (grid.columns === 1 ? 1 : 7);
				break;
			case "ArrowDown":
				next = day + (grid.columns === 1 ? 1 : 7);
				break;
			case "Home":
				next = rowStart;
				break;
			case "End":
				next = rowStart + grid.columns - 1;
				break;
			case "Enter":
			case " ":
				if (canAdd) {
					e.preventDefault();
					setQuickAddDay(day);
				}
				return;
			default:
				return;
		}
		e.preventDefault();
		focusAfterMove.current = true;
		setFocusedDay(next);
		// Walking off the grid turns the page.
		if (next < grid.start || next >= grid.end) setAnchor(next);
	};

	// ── Render pieces ─────────────────────────────────────────────────────────
	function chipTooltip(task: Task): string {
		const span = taskSpan(task.start_date, task.due_date);
		const head = `${taskKey(task, taskIdPrefix)} ${task.title}`;
		if (!span) return head;
		const dates =
			span.hasStart && span.hasDue
				? t("layout.calendar.dateRange", {
						start: fmtDay(span.start, locale),
						end: fmtDay(span.end, locale),
					})
				: span.hasStart
					? t("layout.calendar.startDate", { date: fmtDay(span.start, locale) })
					: t("layout.calendar.dueDate", { date: fmtDay(span.end, locale) });
		return `${head}\n${dates}`;
	}

	function renderChipBody(task: Task) {
		const status = task.status_id ? statusById.get(task.status_id) : undefined;
		const type = task.task_type_id
			? typeById.get(task.task_type_id)
			: undefined;
		const done = status?.category === "done";
		return (
			<>
				<span
					aria-hidden
					className="size-1.5 shrink-0 rounded-full"
					style={{
						background: type?.color ?? "oklch(var(--muted-foreground) / 0.35)",
					}}
				/>
				<span
					className={cn(
						"min-w-0 truncate",
						done && "text-muted-foreground line-through",
					)}
				>
					{task.title}
				</span>
			</>
		);
	}

	function renderSegment(seg: RowSegment<Task>, rowStart: number) {
		const task = seg.item;
		const status = task.status_id ? statusById.get(task.status_id) : undefined;
		const days = seg.end - seg.start + 1;
		const edgesAvailable =
			canReschedule && dateField === "start_and_due" && drag === null;
		const span = placementSpan(task, dateField);
		const grabbed = drag?.task.id === task.id;
		return (
			<div
				key={`${task.id}-${seg.start}`}
				data-calendar-task={task.id}
				data-start={seg.start}
				data-end={seg.end}
				className={cn(
					"group/chip absolute flex items-center",
					// Let drops reach the day under a bar while something is dragged.
					drag ? "pointer-events-none" : "pointer-events-auto",
					grabbed && "opacity-40",
				)}
				style={{
					left: `calc(${((seg.start - rowStart) / grid.columns) * 100}% + 3px)`,
					width: `calc(${(days / grid.columns) * 100}% - 6px)`,
					top: DAY_HEADER_PX + seg.lane * LANE_PX,
					height: LANE_PX - 2,
				}}
			>
				<button
					type="button"
					draggable={canReschedule}
					title={chipTooltip(task)}
					onClick={() => onTaskClick(task)}
					onDragStart={(e) => {
						const rect = e.currentTarget.getBoundingClientRect();
						const index =
							rect.width > 0
								? Math.floor(((e.clientX - rect.left) / rect.width) * days)
								: 0;
						startDrag(e, {
							task,
							kind: "move",
							span,
							grabDay: seg.start + Math.min(days - 1, Math.max(0, index)),
						});
					}}
					onDragEnd={endDrag}
					className={cn(
						"flex h-full w-full min-w-0 items-center gap-1.5 border-l-2 bg-primary/10 px-1.5 text-left text-xs font-medium text-foreground/85",
						"transition-colors duration-100 hover:bg-primary/18 focus-visible:outline-2 focus-visible:outline-primary",
						seg.continuesBefore ? "rounded-l-none" : "rounded-l-md",
						seg.continuesAfter ? "rounded-r-none" : "rounded-r-md",
						canReschedule && "cursor-grab active:cursor-grabbing",
					)}
					style={{
						borderLeftColor: seg.continuesBefore
							? "transparent"
							: (status?.color ?? "var(--color-primary)"),
					}}
				>
					{renderChipBody(task)}
				</button>
				{edgesAvailable && !seg.continuesBefore && (
					<div
						draggable
						aria-hidden
						title={t("layout.calendar.resizeStart")}
						onDragStart={(e) =>
							startDrag(e, { task, kind: "start", span, grabDay: seg.start })
						}
						onDragEnd={endDrag}
						data-calendar-edge="start"
						className="absolute inset-y-0 left-0 w-1.5 cursor-ew-resize opacity-0 group-hover/chip:opacity-100"
					/>
				)}
				{edgesAvailable && !seg.continuesAfter && (
					<div
						draggable
						aria-hidden
						title={t("layout.calendar.resizeEnd")}
						onDragStart={(e) =>
							startDrag(e, { task, kind: "end", span, grabDay: seg.end })
						}
						onDragEnd={endDrag}
						data-calendar-edge="end"
						className="absolute inset-y-0 right-0 w-1.5 cursor-ew-resize rounded-r-md opacity-0 group-hover/chip:opacity-100 group-hover/chip:bg-primary/40"
					/>
				)}
			</div>
		);
	}

	function renderTaskList(tasks: Task[]) {
		return (
			<ul className="flex flex-col gap-0.5">
				{tasks.map((task) => (
					<li key={task.id}>
						<button
							type="button"
							title={chipTooltip(task)}
							onClick={() => onTaskClick(task)}
							className="flex w-full min-w-0 items-center gap-1.5 rounded-md px-2 py-1 text-left text-xs font-medium hover:bg-muted/60 focus-visible:outline-2 focus-visible:outline-primary"
						>
							{renderChipBody(task)}
						</button>
					</li>
				))}
			</ul>
		);
	}

	function renderDay(day: number, row: (typeof rows)[number], limit: number) {
		const date = new Date(day * MS_PER_DAY);
		const isToday = day === today;
		const outside = mode === "month" && !inAnchorMonth(day, anchor);
		const onDay = segmentsOnDay(row.segments, day);
		const hidden = hiddenOnDay(row.segments, day, limit);
		const inPreview = preview && day >= preview.start && day <= preview.end;
		const label = [
			fullDateLabel(day, locale),
			isToday ? t("layout.calendar.today") : null,
			t("layout.calendar.taskCount", { count: onDay.length }),
		]
			.filter(Boolean)
			.join(", ");
		return (
			// biome-ignore lint/a11y/useSemanticElements: ARIA grid of CSS-grid days
			<div
				key={day}
				role="gridcell"
				tabIndex={day === activeDay ? 0 : -1}
				aria-label={label}
				aria-current={isToday ? "date" : undefined}
				data-calendar-day={day}
				data-drop-target={inPreview ? "true" : undefined}
				onKeyDown={(e) => onGridKeyDown(e, day)}
				onFocus={(e) => {
					if (e.target === e.currentTarget) setFocusedDay(day);
				}}
				onClick={(e) => {
					if (e.target === e.currentTarget && canAdd) setQuickAddDay(day);
				}}
				onDragOver={(e) => {
					if (!dragRef.current) return;
					e.preventDefault();
					e.dataTransfer.dropEffect = "move";
					if (overDay !== day) setOverDay(day);
				}}
				onDrop={(e) => {
					if (!dragRef.current) return;
					e.preventDefault();
					dropOnDay(day);
				}}
				className={cn(
					"group/day relative min-w-0 flex-1 border-r border-border/40 last:border-r-0",
					"focus-visible:outline-2 focus-visible:-outline-offset-2 focus-visible:outline-primary",
					outside && "bg-muted/25",
					inPreview && "bg-primary/8",
				)}
			>
				<div
					className="pointer-events-none flex items-center justify-between px-1.5"
					style={{ height: DAY_HEADER_PX }}
				>
					<span
						className={cn(
							"flex h-5 min-w-5 items-center justify-center rounded-full px-1 text-xs tabular-nums",
							isToday
								? "bg-primary font-semibold text-primary-foreground"
								: outside
									? "text-muted-foreground/60"
									: "text-muted-foreground",
						)}
					>
						{date.getUTCDate()}
					</span>
					{canAdd && (
						<QuickAdd
							open={quickAddDay === day}
							onOpenChange={(open) => setQuickAddDay(open ? day : null)}
							label={t("layout.calendar.addTaskOn", {
								date: fullDateLabel(day, locale),
							})}
							taskTypes={taskTypes}
							onAdd={(title, taskTypeId) =>
								onCreateTask?.(
									defaultStatusId,
									title,
									taskTypeId,
									scheduleOnDay(day, dateField),
								)
							}
						/>
					)}
				</div>
				{hidden.length > 0 && (
					<Popover>
						<PopoverTrigger
							render={
								<button
									type="button"
									className="pointer-events-auto absolute left-1 rounded px-1.5 text-xs font-semibold text-muted-foreground hover:bg-muted/60 hover:text-foreground focus-visible:outline-2 focus-visible:outline-primary"
									style={{ top: DAY_HEADER_PX + limit * LANE_PX }}
								/>
							}
						>
							{t("layout.calendar.more", { count: hidden.length })}
						</PopoverTrigger>
						<PopoverContent align="start" className="w-64 gap-1.5">
							<p className="px-2 text-xs font-semibold text-muted-foreground">
								{fullDateLabel(day, locale)}
							</p>
							{renderTaskList(onDay.map((s) => s.item))}
						</PopoverContent>
					</Popover>
				)}
			</div>
		);
	}

	function renderRow(row: (typeof rows)[number]) {
		const limit = visibleLanes(row.laneCount, laneFit);
		const days = Array.from(
			{ length: grid.columns },
			(_, i) => row.rowStart + i,
		);
		// Week and day rows grow to show every lane and scroll as one.
		const minHeight =
			mode === "month"
				? undefined
				: DAY_HEADER_PX + row.laneCount * LANE_PX + LANE_PX;
		return (
			// biome-ignore lint/a11y/useSemanticElements: ARIA grid of CSS-grid days
			// biome-ignore lint/a11y/useFocusableInteractive: its days take the focus
			<div
				key={row.rowStart}
				role="row"
				className={cn(
					"relative flex border-b border-border/40 last:border-b-0",
					mode === "month" ? "min-h-24 flex-1" : "flex-1",
				)}
				style={{ minHeight }}
			>
				{days.map((day) => renderDay(day, row, limit))}
				<div className="pointer-events-none absolute inset-0">
					{row.segments
						.filter((seg) => seg.lane < limit)
						.map((seg) => renderSegment(seg, row.rowStart))}
				</div>
			</div>
		);
	}

	// ── Render ────────────────────────────────────────────────────────────────
	const [undatedOpen, setUndatedOpen] = useState(false);
	const loadError = windowQuery.isError || windowQuery.isFetchNextPageError;
	const headerLabels = weekdayLabels(grid.rows[0], grid.columns, locale);

	return (
		<div className="flex h-full flex-col overflow-hidden">
			{/* ── Toolbar: navigation, mode and the undated tasks ─────────── */}
			<div className="flex shrink-0 flex-wrap items-center gap-2 border-b border-border/50 px-4 py-1.5">
				<div className="flex items-center gap-1">
					<button
						type="button"
						onClick={() => step(-1)}
						aria-label={t("layout.calendar.previous")}
						title={t("layout.calendar.previous")}
						className="flex size-7 items-center justify-center rounded-md text-muted-foreground transition-colors hover:bg-muted/60 hover:text-foreground"
					>
						<ChevronLeft className="size-4" />
					</button>
					<button
						type="button"
						onClick={() => step(1)}
						aria-label={t("layout.calendar.next")}
						title={t("layout.calendar.next")}
						className="flex size-7 items-center justify-center rounded-md text-muted-foreground transition-colors hover:bg-muted/60 hover:text-foreground"
					>
						<ChevronRight className="size-4" />
					</button>
					<button
						type="button"
						onClick={goToday}
						title={t("layout.calendar.jumpToToday")}
						className="flex items-center gap-1.5 rounded-lg border border-border/60 px-2.5 py-1 text-xs font-medium text-muted-foreground transition-colors duration-100 hover:border-primary/50 hover:text-primary"
					>
						<LocateFixed className="size-3.5" />
						{t("layout.calendar.today")}
					</button>
				</div>
				<h2 aria-live="polite" className="text-sm font-semibold">
					{title}
				</h2>
				{windowQuery.isFetching && <Spinner className="size-3.5" />}
				{loadError && (
					<button
						type="button"
						onClick={() =>
							windowQuery.isFetchNextPageError
								? void windowQuery.fetchNextPage()
								: void windowQuery.refetch()
						}
						className="flex items-center gap-1 rounded-md px-2 py-0.5 text-xs font-medium text-destructive hover:bg-destructive/10"
					>
						<RotateCw className="size-3" />
						{t("layout.calendar.loadFailed")}
					</button>
				)}
				<div className="ml-auto flex items-center gap-2">
					<fieldset
						aria-label={t("layout.calendar.modeLabel")}
						className="flex items-center rounded-lg border border-border/60 p-0.5"
					>
						{CALENDAR_MODES.map((m) => (
							<button
								key={m}
								type="button"
								aria-pressed={mode === m}
								onClick={() => changeMode(m)}
								className={cn(
									"rounded-md px-2.5 py-0.5 text-xs font-medium transition-colors duration-100",
									mode === m
										? "bg-primary/12 text-primary"
										: "text-muted-foreground hover:bg-muted/50 hover:text-foreground",
								)}
							>
								{t(MODE_LABEL_KEYS[m])}
							</button>
						))}
					</fieldset>
					<button
						type="button"
						aria-pressed={undatedOpen}
						onClick={() => setUndatedOpen((o) => !o)}
						className={cn(
							"flex items-center gap-1.5 rounded-lg border px-2.5 py-1 text-xs font-medium transition-colors duration-100",
							undatedOpen
								? "border-primary/40 bg-primary/10 text-primary"
								: "border-border/60 text-muted-foreground hover:border-primary/50 hover:text-primary",
						)}
					>
						<CalendarOff className="size-3.5" />
						{t("layout.calendar.noDate")}
						{undatedTotal !== undefined && (
							<span className="rounded-full bg-muted/70 px-1.5 text-[11px] font-bold tabular-nums text-muted-foreground">
								{undatedTotal}
							</span>
						)}
					</button>
				</div>
			</div>

			<div className="flex min-h-0 flex-1">
				{/* ── Grid ────────────────────────────────────────────── */}
				{/* biome-ignore lint/a11y/useSemanticElements: ARIA grid of CSS-grid days */}
				<div
					ref={gridRef}
					role="grid"
					aria-label={title}
					className="flex min-w-0 flex-1 flex-col"
				>
					{/* biome-ignore lint/a11y/useSemanticElements: ARIA grid of CSS-grid days */}
					{/* biome-ignore lint/a11y/useFocusableInteractive: header row only labels the days */}
					<div role="row" className="flex shrink-0 border-b border-border/50">
						{headerLabels.map(({ day, label }) => (
							// biome-ignore lint/a11y/useSemanticElements: ARIA grid of CSS-grid days
							// biome-ignore lint/a11y/useFocusableInteractive: only labels a column of days
							<div
								key={day}
								role="columnheader"
								className="min-w-0 flex-1 px-2 py-1.5 text-xs font-semibold uppercase tracking-wide text-muted-foreground"
							>
								{label}
							</div>
						))}
					</div>
					<div
						ref={bodyRef}
						className="flex min-h-0 flex-1 flex-col overflow-y-auto"
					>
						{rows.map(renderRow)}
					</div>
				</div>

				{/* ── Undated tasks ───────────────────────────────────── */}
				{undatedOpen && (
					<aside
						aria-label={t("layout.calendar.noDate")}
						className="flex w-64 shrink-0 flex-col border-l border-border/50"
					>
						<div className="border-b border-border/50 px-3 py-2">
							<p className="text-xs font-semibold">
								{t("layout.calendar.noDate")}
							</p>
							<p className="text-xs text-muted-foreground">
								{canReschedule
									? t("layout.calendar.noDateHintDrag")
									: t("layout.calendar.noDateHint")}
							</p>
						</div>
						<div className="min-h-0 flex-1 overflow-y-auto p-2">
							{undatedQuery.isPending ? (
								<div className="flex justify-center py-4">
									<Spinner className="size-4" />
								</div>
							) : undatedTasks.length === 0 ? (
								<p className="px-2 py-4 text-center text-xs text-muted-foreground">
									{t("layout.calendar.noUndatedTasks")}
								</p>
							) : (
								<ul className="flex flex-col gap-1">
									{undatedTasks.map((task) => (
										<li key={task.id}>
											<button
												type="button"
												draggable={canReschedule}
												title={chipTooltip(task)}
												data-calendar-undated={task.id}
												onClick={() => onTaskClick(task)}
												onDragStart={(e) =>
													startDrag(e, {
														task,
														kind: "schedule",
														span: null,
														grabDay: 0,
													})
												}
												onDragEnd={endDrag}
												className={cn(
													"flex w-full min-w-0 items-center gap-1.5 rounded-md border border-border/50 px-2 py-1.5 text-left text-xs font-medium hover:bg-muted/50 focus-visible:outline-2 focus-visible:outline-primary",
													canReschedule && "cursor-grab active:cursor-grabbing",
												)}
											>
												{renderChipBody(task)}
											</button>
										</li>
									))}
								</ul>
							)}
							{undatedQuery.hasNextPage && (
								<button
									type="button"
									disabled={undatedQuery.isFetchingNextPage}
									onClick={() => void undatedQuery.fetchNextPage()}
									className="mt-2 w-full rounded-md py-1.5 text-xs font-medium text-muted-foreground hover:bg-muted/50 hover:text-foreground disabled:opacity-60"
								>
									{undatedQuery.isFetchingNextPage
										? t("layout.calendar.loading")
										: t("layout.calendar.viewMore")}
								</button>
							)}
						</div>
					</aside>
				)}
			</div>
		</div>
	);
}

// ─── Quick add ────────────────────────────────────────────────────────────────

interface QuickAddProps {
	open: boolean;
	onOpenChange: (open: boolean) => void;
	label: string;
	taskTypes: TaskType[];
	onAdd: (
		title: string,
		taskTypeId: string | null,
	) => Promise<void> | undefined;
}

/** The "+" of a day: a small form that creates a task dated that day. */
function QuickAdd({
	open,
	onOpenChange,
	label,
	taskTypes,
	onAdd,
}: QuickAddProps) {
	const { t } = useTranslation("projects");
	const [title, setTitle] = useState("");
	const [typeId, setTypeId] = useState<string | null>(null);
	const [saving, setSaving] = useState(false);
	const defaultType =
		taskTypes.find((tt) => tt.is_default) ?? taskTypes[0] ?? null;
	const selectedType = taskTypes.find((tt) => tt.id === typeId) ?? defaultType;

	const close = () => {
		setTitle("");
		setTypeId(null);
		onOpenChange(false);
	};

	const submit = async () => {
		const value = title.trim();
		if (!value || saving) return;
		setSaving(true);
		try {
			await onAdd(value, selectedType?.id ?? null);
			close();
		} catch {
			// The create mutation reports the failure; keep the text to retry.
		} finally {
			setSaving(false);
		}
	};

	return (
		<Popover
			open={open}
			onOpenChange={(next) => (next ? onOpenChange(true) : close())}
		>
			<PopoverTrigger
				render={
					<button
						type="button"
						aria-label={label}
						title={label}
						className={cn(
							"pointer-events-auto flex size-5 items-center justify-center rounded text-muted-foreground opacity-0 transition-opacity hover:bg-muted/70 hover:text-foreground focus-visible:opacity-100 group-hover/day:opacity-100",
							open && "opacity-100",
						)}
					/>
				}
			>
				<Plus className="size-3.5" />
			</PopoverTrigger>
			<PopoverContent align="end" className="w-72 gap-2">
				<form
					className="flex flex-col gap-2"
					onSubmit={(e) => {
						e.preventDefault();
						void submit();
					}}
				>
					<p className="text-xs font-semibold text-muted-foreground">{label}</p>
					<input
						// biome-ignore lint/a11y/noAutofocus: the form opens to type a title
						autoFocus
						value={title}
						onChange={(e) => setTitle(e.target.value)}
						onKeyDown={(e) => {
							if (e.key === "Escape") close();
						}}
						aria-label={t("layout.calendar.taskTitle")}
						placeholder={t("board.addTaskRow.placeholder")}
						className="rounded-md border border-input bg-transparent px-2.5 py-1.5 text-sm outline-none focus:border-ring focus:ring-2 focus:ring-ring/20"
					/>
					<div className="flex items-center justify-between gap-2">
						{taskTypes.length > 0 && selectedType ? (
							<TaskTypeSelector
								taskTypes={taskTypes}
								value={selectedType.id}
								onChange={setTypeId}
							/>
						) : (
							<span />
						)}
						<div className="flex items-center gap-1.5">
							<button
								type="button"
								onClick={close}
								className="rounded-lg bg-muted/40 px-2.5 py-1.5 text-xs font-semibold text-muted-foreground hover:bg-muted/60 hover:text-foreground"
							>
								{t("board.addTaskRow.cancel")}
							</button>
							<button
								type="submit"
								disabled={!title.trim() || saving}
								className="rounded-lg bg-primary px-2.5 py-1.5 text-xs font-semibold text-primary-foreground hover:bg-primary/90 disabled:opacity-50"
							>
								{t("layout.calendar.add")}
							</button>
						</div>
					</div>
				</form>
			</PopoverContent>
		</Popover>
	);
}
