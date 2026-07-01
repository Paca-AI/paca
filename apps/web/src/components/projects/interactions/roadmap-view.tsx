import { CalendarDays } from "lucide-react";
import { useMemo } from "react";
import { useTranslation } from "react-i18next";

import type { Sprint, Task } from "@/lib/interaction-api";
import type {
	CustomFieldDefinition,
	ProjectMember,
	TaskStatus,
	TaskType,
} from "@/lib/project-api";
import { cn } from "@/lib/utils";
import { AddTaskRow } from "./add-task-row";
import {
	type ColumnGroupDef,
	getColumnGroupDefs,
	getTaskColumnKeys,
} from "./view-utils";

// ─── Layout constants ─────────────────────────────────────────────────────────
const LEFT_COL_W = 280; // px — sticky task-name column
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
	tasks: Task[];
	statuses: TaskStatus[];
	taskTypes: TaskType[];
	members?: ProjectMember[];
	sprints?: Sprint[];
	customFields?: CustomFieldDefinition[];
	columnBy?: string;
	canCreate?: boolean;
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
	tasks,
	statuses,
	taskTypes,
	members = [],
	sprints = [],
	customFields = [],
	columnBy = "status",
	canCreate = false,
	onCreateTask,
	onTaskClick,
	pagination,
}: RoadmapViewProps) {
	const { t } = useTranslation();
	// Stable "now" — fixed at mount so all bars are consistent
	const now = useMemo(() => Date.now(), []);

	const viewCtx = useMemo(
		() => ({ statuses, taskTypes, members, customFields, sprints }),
		[statuses, taskTypes, members, customFields, sprints],
	);

	const groupDefs = useMemo(
		() => getColumnGroupDefs(columnBy, viewCtx, t),
		[columnBy, viewCtx, t],
	);

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
		for (const task of tasks) {
			const s = parseDateMs(task.start_date);
			const d = parseDateMs(task.due_date);
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
			tooltip = `${fmtDate(startMs)} → ${fmtDate(dueMs)}`;
		} else if (startMs !== null) {
			tooltip = t("project.interactions.roadmap.startTooltip", {
				date: fmtDate(startMs),
			});
		} else {
			tooltip = t("project.interactions.roadmap.dueTooltip", {
				date: fmtDate(dueMs as number),
			});
		}

		return { leftPx, widthPx, singleDate, overdue, tooltip };
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
						title={t("project.interactions.roadmap.noDatesSet")}
						className="absolute h-5 w-20 rounded border border-dashed border-muted-foreground/20"
						style={{ left: Math.max(0, todayPx - 40) }}
					/>
				)}
			</div>
		);
	}

	// ── Task row ──────────────────────────────────────────────────────────────
	function RoadmapTaskRow({ task }: { task: Task }) {
		const bar = getBar(task);
		const type = taskTypes.find((tt) => tt.id === task.task_type_id) ?? null;

		return (
			<button
				type="button"
				className="group flex w-full cursor-pointer border-b border-border/10 text-left last:border-0"
				onClick={() => onTaskClick(task)}
			>
				{/* Sticky task name */}
				<div
					className={cn(
						"sticky left-0 z-10 flex shrink-0 items-center gap-2",
						"border-r border-border/20 bg-background px-4 py-2.5",
						"group-hover:bg-muted/30 transition-colors duration-100",
					)}
					style={{ width: LEFT_COL_W }}
				>
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
				</div>

				{/* Chart area */}
				<ChartCell bar={bar} />
			</button>
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
				<div style={{ minWidth: LEFT_COL_W + chartWidth }}>
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
							style={{ width: LEFT_COL_W }}
						>
							<CalendarDays className="size-3 text-muted-foreground/45" />
							<span className="text-xs font-semibold uppercase tracking-[0.08em] text-muted-foreground/55">
								{t("project.interactions.roadmap.taskColumn")}
							</span>
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
								{t("project.interactions.roadmap.noTasksToDisplay")}
							</p>
						</div>
					) : (
						groupDefs.map((group) => {
							const groupTasks = tasks.filter((task) =>
								getTaskColumnKeys(task, columnBy, viewCtx).includes(group.key),
							);
							if (groupTasks.length === 0) return null;

							return (
								<div
									key={group.key}
									className="border-b border-border/20 last:border-0"
								>
									<GroupHeader group={group} count={groupTasks.length} />
									{groupTasks.map((task) => (
										<RoadmapTaskRow key={task.id} task={task} />
									))}
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
									? t("project.interactions.board.loadingMore")
									: t("project.interactions.board.viewMore")}
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
