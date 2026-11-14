// Geometry of the Gantt view: dates as day numbers, the time scale at each
// zoom level, bar placement, drag-to-reschedule arithmetic and the routing of
// dependency arrows. Kept free of React so it can be tested on its own.
//
// Paca stores task dates as UTC midnight ("2026-03-05T00:00:00Z"), so the UTC
// calendar date of a stored value is the date the person picked. Everything
// here works in whole UTC days ("day numbers": days since the Unix epoch),
// which keeps the chart free of time-zone and daylight-saving drift.

import type { GanttZoom } from "@/lib/interaction-api";

export const MS_PER_DAY = 86_400_000;

/** Zoom levels, finest first. */
export const GANTT_ZOOMS: readonly GanttZoom[] = [
	"day",
	"week",
	"month",
	"quarter",
	"year",
];

export const DEFAULT_GANTT_ZOOM: GanttZoom = "week";

/** Chart pixels per calendar day at each zoom level. */
export const PX_PER_DAY: Record<GanttZoom, number> = {
	day: 40,
	week: 20,
	month: 6,
	quarter: 2.5,
	year: 0.75,
};

/** Narrowest a bar is drawn, so a one-day task stays visible at any zoom. */
export const MIN_BAR_PX = 8;

/** The saved zoom of a view, or the default when it is unset or unknown. */
export function resolveGanttZoom(zoom: string | null | undefined): GanttZoom {
	return GANTT_ZOOMS.includes(zoom as GanttZoom)
		? (zoom as GanttZoom)
		: DEFAULT_GANTT_ZOOM;
}

/** Day number of a stored date, or null when it is unset or unparsable. */
export function toDay(iso: string | null | undefined): number | null {
	if (!iso) return null;
	const ms = Date.parse(iso);
	return Number.isNaN(ms) ? null : Math.floor(ms / MS_PER_DAY);
}

/** The stored form of a day number: UTC midnight of that date. */
export function dayToISO(day: number): string {
	return `${new Date(day * MS_PER_DAY).toISOString().slice(0, 10)}T00:00:00Z`;
}

/** Day number of the local calendar date, so "today" matches the clock. */
export function localToday(now: Date = new Date()): number {
	return Math.floor(
		Date.UTC(now.getFullYear(), now.getMonth(), now.getDate()) / MS_PER_DAY,
	);
}

/**
 * The days a task occupies, both ends inclusive. A task with one date
 * occupies that day; the flags record which dates are really set, so a
 * reschedule only writes what the task has.
 */
export interface TaskSpan {
	start: number;
	end: number;
	hasStart: boolean;
	hasDue: boolean;
}

export function taskSpan(
	startDate: string | null | undefined,
	dueDate: string | null | undefined,
): TaskSpan | null {
	const start = toDay(startDate);
	const due = toDay(dueDate);
	if (start === null && due === null) return null;
	const a = start ?? (due as number);
	const b = due ?? (start as number);
	return {
		start: Math.min(a, b),
		end: Math.max(a, b),
		hasStart: start !== null,
		hasDue: due !== null,
	};
}

// ─── Time units ───────────────────────────────────────────────────────────────

type Unit = "day" | "week" | "month" | "quarter" | "year";

function dayOf(year: number, month: number, date: number): number {
	// Date.UTC carries an out-of-range month into the year.
	return Math.floor(Date.UTC(year, month, date) / MS_PER_DAY);
}

function partsOf(day: number) {
	const d = new Date(day * MS_PER_DAY);
	return {
		year: d.getUTCFullYear(),
		month: d.getUTCMonth(),
		weekday: d.getUTCDay(),
	};
}

/** First day of the unit that contains `day` (weeks start on Monday). */
export function unitStart(day: number, unit: Unit): number {
	const { year, month, weekday } = partsOf(day);
	switch (unit) {
		case "day":
			return day;
		case "week":
			return day - ((weekday + 6) % 7);
		case "month":
			return dayOf(year, month, 1);
		case "quarter":
			return dayOf(year, month - (month % 3), 1);
		case "year":
			return dayOf(year, 0, 1);
	}
}

/** First day of the unit after the one starting on `start`. */
export function nextUnit(start: number, unit: Unit): number {
	const { year, month } = partsOf(start);
	switch (unit) {
		case "day":
			return start + 1;
		case "week":
			return start + 7;
		case "month":
			return dayOf(year, month + 1, 1);
		case "quarter":
			return dayOf(year, month + 3, 1);
		case "year":
			return dayOf(year + 1, 0, 1);
	}
}

/** The step of each zoom's scale, and the coarser band shown above it. */
const TIERS: Record<GanttZoom, { minor: Unit; major: Unit | null }> = {
	day: { minor: "day", major: "month" },
	week: { minor: "week", major: "month" },
	month: { minor: "month", major: "year" },
	quarter: { minor: "quarter", major: "year" },
	year: { minor: "year", major: null },
};

/** Empty steps kept on each side of the tasks, so bars never touch the edge. */
const PAD_STEPS: Record<GanttZoom, number> = {
	day: 7,
	week: 2,
	month: 1,
	quarter: 1,
	year: 1,
};

// ─── Range and scale ──────────────────────────────────────────────────────────

/** The days the chart shows: `start` inclusive, `end` exclusive. */
export interface GanttRange {
	start: number;
	end: number;
}

/**
 * The chart's range at a zoom level: every span and today, widened to whole
 * steps of the scale plus some padding on each side.
 */
export function computeRange(
	spans: readonly TaskSpan[],
	today: number,
	zoom: GanttZoom,
): GanttRange {
	let lo = today;
	let hi = today;
	for (const s of spans) {
		if (s.start < lo) lo = s.start;
		if (s.end > hi) hi = s.end;
	}
	const unit = TIERS[zoom].minor;
	let start = unitStart(lo, unit);
	let end = nextUnit(unitStart(hi, unit), unit);
	for (let i = 0; i < PAD_STEPS[zoom]; i++) {
		start = unitStart(start - 1, unit);
		end = nextUnit(end, unit);
	}
	return { start, end };
}

export function chartWidth(range: GanttRange, zoom: GanttZoom): number {
	return (range.end - range.start) * PX_PER_DAY[zoom];
}

/** Horizontal offset of the start of `day` from the chart's left edge. */
export function dayToPx(day: number, range: GanttRange, zoom: GanttZoom) {
	return (day - range.start) * PX_PER_DAY[zoom];
}

export interface ScaleCell {
	key: string;
	label: string;
	left: number;
	width: number;
}

export interface GanttScale {
	/** The coarser band (months or years); empty at the year zoom. */
	major: ScaleCell[];
	/** One cell per step of the scale. */
	minor: ScaleCell[];
}

function formatter(
	locale: string | undefined,
	opts: Intl.DateTimeFormatOptions,
) {
	const f = new Intl.DateTimeFormat(locale, { ...opts, timeZone: "UTC" });
	return (day: number) => f.format(new Date(day * MS_PER_DAY));
}

/** Header cells of the chart's two bands at a zoom level. */
export function buildScale(
	range: GanttRange,
	zoom: GanttZoom,
	options: { locale?: string; quarterLabel?: (quarter: number) => string } = {},
): GanttScale {
	const { locale, quarterLabel = (q) => `Q${q}` } = options;
	const px = PX_PER_DAY[zoom];
	const labels: Record<Unit, (day: number, band: "major" | "minor") => string> =
		{
			day: (day) => String(new Date(day * MS_PER_DAY).getUTCDate()),
			week: formatter(locale, { month: "short", day: "numeric" }),
			month: (() => {
				const long = formatter(locale, { month: "long", year: "numeric" });
				const short = formatter(locale, { month: "short" });
				return (day, band) => (band === "major" ? long(day) : short(day));
			})(),
			quarter: (day) => quarterLabel(Math.floor(partsOf(day).month / 3) + 1),
			year: (day) => String(partsOf(day).year),
		};
	const cells = (unit: Unit, band: "major" | "minor"): ScaleCell[] => {
		const out: ScaleCell[] = [];
		for (let s = unitStart(range.start, unit); s < range.end; ) {
			const next = nextUnit(s, unit);
			const from = Math.max(s, range.start);
			const to = Math.min(next, range.end);
			out.push({
				key: `${unit}-${s}`,
				label: labels[unit](s, band),
				left: (from - range.start) * px,
				width: (to - from) * px,
			});
			s = next;
		}
		return out;
	};
	const { minor, major } = TIERS[zoom];
	return {
		major: major ? cells(major, "major") : [],
		minor: cells(minor, "minor"),
	};
}

// ─── Bars and dragging ────────────────────────────────────────────────────────

export interface BarBox {
	left: number;
	width: number;
}

/**
 * Where a span's bar sits: from the start of its first day to the end of its
 * last, kept inside the chart (a nested task can fall outside the range,
 * which is computed from the top-level tasks).
 */
export function barBox(
	span: TaskSpan,
	range: GanttRange,
	zoom: GanttZoom,
): BarBox {
	const width = chartWidth(range, zoom);
	const rawLeft = dayToPx(span.start, range, zoom);
	const rawRight = dayToPx(span.end + 1, range, zoom);
	const left = Math.min(Math.max(0, rawLeft), Math.max(0, width - MIN_BAR_PX));
	const right = Math.min(width, Math.max(rawRight, left + MIN_BAR_PX));
	return { left, width: Math.max(MIN_BAR_PX, right - left) };
}

/** What a drag changes: the whole bar, or one of its edges. */
export type DragMode = "move" | "start" | "end";

/** Whole days a horizontal drag of `dx` pixels covers, snapped to the day. */
export function dragDays(dx: number, zoom: GanttZoom): number {
	return Math.round(dx / PX_PER_DAY[zoom]) || 0;
}

/** The span after dragging `mode` by `days`; an edge never passes the other. */
export function dragSpan(span: TaskSpan, mode: DragMode, days: number) {
	switch (mode) {
		case "move":
			return { ...span, start: span.start + days, end: span.end + days };
		case "start":
			return { ...span, start: Math.min(span.start + days, span.end) };
		case "end":
			return { ...span, end: Math.max(span.end + days, span.start) };
	}
}

export interface DateUpdate {
	start_date?: string | null;
	due_date?: string | null;
}

/**
 * The task update a drag makes. Moving shifts only the dates the task has;
 * dragging an edge sets that date, and also fills in the other one when the
 * task had a single date, so the bar keeps the range the person drew.
 * Empty when the drag changed nothing.
 */
export function rescheduleUpdate(
	before: TaskSpan,
	after: Pick<TaskSpan, "start" | "end">,
	mode: DragMode,
): DateUpdate {
	if (after.start === before.start && after.end === before.end) return {};
	const out: DateUpdate = {};
	if (mode === "move") {
		if (before.hasStart) out.start_date = dayToISO(after.start);
		if (before.hasDue) out.due_date = dayToISO(after.end);
	} else if (mode === "start") {
		out.start_date = dayToISO(after.start);
		if (!before.hasDue) out.due_date = dayToISO(after.end);
	} else {
		out.due_date = dayToISO(after.end);
		if (!before.hasStart) out.start_date = dayToISO(after.start);
	}
	return out;
}

// ─── Dependencies ─────────────────────────────────────────────────────────────

/**
 * A "blocks" link is violated when the blocked task starts before its blocker
 * ends, i.e. on or before the blocker's last day.
 */
export function violatesBlock(blocker: TaskSpan, blocked: TaskSpan): boolean {
	return blocked.start <= blocker.end;
}

export interface Point {
	x: number;
	y: number;
}

/**
 * SVG path of a dependency arrow from the blocker's end (`from`) to the
 * blocked task's start (`to`), drawn with right angles. When the blocked task
 * starts far enough to the right the line steps down once; otherwise it
 * leaves to the right, doubles back between the two rows and enters from the
 * left, so it never runs through either bar.
 */
export function dependencyPath(from: Point, to: Point, gap = 8): string {
	if (to.x - from.x >= 2 * gap) {
		const midX = from.x + gap;
		return `M${from.x},${from.y} H${midX} V${to.y} H${to.x}`;
	}
	const midY = from.y === to.y ? from.y + 14 : from.y + (to.y - from.y) / 2;
	return `M${from.x},${from.y} H${from.x + gap} V${midY} H${to.x - gap} V${to.y} H${to.x}`;
}
