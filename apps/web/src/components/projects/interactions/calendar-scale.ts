// Geometry of the Calendar view: the grid each mode shows, the first day of
// the week, where a task lands and how the bars of a week row stack. Kept
// free of React so it can be tested on its own.
//
// Like the Gantt view (see gantt-scale.ts) it works in whole UTC days ("day
// numbers"), because Paca stores task dates as UTC midnight.

import type {
	CalendarDateField,
	CalendarMode,
	Task,
} from "@/lib/interaction-api";
import {
	dayToISO,
	MS_PER_DAY,
	nextUnit,
	type TaskSpan,
	taskSpan,
	unitStart,
} from "./gantt-scale";

/** Modes, widest first. */
export const CALENDAR_MODES: readonly CalendarMode[] = ["month", "week", "day"];

export const DEFAULT_CALENDAR_MODE: CalendarMode = "month";

export const CALENDAR_DATE_FIELDS: readonly CalendarDateField[] = [
	"start_and_due",
	"start_date",
	"due_date",
];

export const DEFAULT_CALENDAR_DATE_FIELD: CalendarDateField = "start_and_due";

/** The saved mode of a view, or the default when it is unset or unknown. */
export function resolveCalendarMode(
	mode: string | null | undefined,
): CalendarMode {
	return CALENDAR_MODES.includes(mode as CalendarMode)
		? (mode as CalendarMode)
		: DEFAULT_CALENDAR_MODE;
}

/** The saved date field of a view, or the default when unset or unknown. */
export function resolveCalendarDateField(
	field: string | null | undefined,
): CalendarDateField {
	return CALENDAR_DATE_FIELDS.includes(field as CalendarDateField)
		? (field as CalendarDateField)
		: DEFAULT_CALENDAR_DATE_FIELD;
}

/** "YYYY-MM-DD" of a day number, the form the API's date filters take. */
export function dayToDateParam(day: number): string {
	return dayToISO(day).slice(0, 10);
}

// ─── Weeks ────────────────────────────────────────────────────────────────────

/** Day of the week of a day number: 0 is Sunday, 6 is Saturday. */
export function weekdayOf(day: number): number {
	// 1970-01-01, day 0, was a Thursday.
	return (((day + 4) % 7) + 7) % 7;
}

// Regions whose week starts on Sunday (CLDR week data), for browsers without
// Intl.Locale week info. Everywhere else not listed starts on Monday, apart
// from the few that start on Saturday.
const SUNDAY_FIRST = new Set(
	(
		"AG AS BD BR BS BT BW BZ CA CO DM DO ET GT GU HK HN ID IL IN JM JP KE " +
		"KH KR LA MH MM MO MT MX MZ NI NP PA PE PH PK PR PT PY SA SG SV TH TT " +
		"TW UM US VE VI WS YE ZA ZW"
	).split(" "),
);
const SATURDAY_FIRST = new Set(
	"AE AF BH DJ DZ EG IQ IR JO KW LY OM QA SD SY".split(" "),
);

interface WeekInfoLocale extends Intl.Locale {
	getWeekInfo?: () => { firstDay: number };
	weekInfo?: { firstDay: number };
}

/**
 * The first day of the week in a locale (0 Sunday … 6 Saturday), so the grid
 * starts its rows where the person's calendars do. Unknown locales start on
 * Monday.
 */
export function firstDayOfWeek(locale: string | undefined): number {
	if (!locale) return 1;
	let loc: WeekInfoLocale;
	try {
		loc = new Intl.Locale(locale) as WeekInfoLocale;
	} catch {
		return 1;
	}
	const info = loc.getWeekInfo?.() ?? loc.weekInfo;
	// Intl numbers the days 1 (Monday) to 7 (Sunday).
	if (info && info.firstDay >= 1 && info.firstDay <= 7) {
		return info.firstDay % 7;
	}
	const region = loc.region ?? loc.maximize().region;
	if (region && SUNDAY_FIRST.has(region)) return 0;
	if (region && SATURDAY_FIRST.has(region)) return 6;
	return 1;
}

/** The first day of the week that contains `day`. */
export function weekStartOf(day: number, firstDay: number): number {
	return day - ((weekdayOf(day) - firstDay + 7) % 7);
}

// ─── Grid ─────────────────────────────────────────────────────────────────────

/**
 * The days a mode shows around an anchor day: `start` inclusive, `end`
 * exclusive, in rows of `columns` days.
 */
export interface CalendarGrid {
	start: number;
	end: number;
	/** Days per row: 7, or 1 in the day mode. */
	columns: number;
	/** The first day of each row. */
	rows: number[];
}

/**
 * The grid of a mode: the whole weeks covering the anchor's month, the week
 * of the anchor, or the anchor alone.
 */
export function buildGrid(
	anchor: number,
	mode: CalendarMode,
	firstDay: number,
): CalendarGrid {
	if (mode === "day") {
		return { start: anchor, end: anchor + 1, columns: 1, rows: [anchor] };
	}
	let start: number;
	let end: number;
	if (mode === "week") {
		start = weekStartOf(anchor, firstDay);
		end = start + 7;
	} else {
		const first = unitStart(anchor, "month");
		const last = nextUnit(first, "month") - 1;
		start = weekStartOf(first, firstDay);
		end = weekStartOf(last, firstDay) + 7;
	}
	const rows: number[] = [];
	for (let d = start; d < end; d += 7) rows.push(d);
	return { start, end, columns: 7, rows };
}

/** The anchor one step back (-1) or forward (1) in a mode. */
export function shiftAnchor(
	anchor: number,
	mode: CalendarMode,
	direction: -1 | 1,
): number {
	switch (mode) {
		case "day":
			return anchor + direction;
		case "week":
			return anchor + 7 * direction;
		case "month": {
			const first = unitStart(anchor, "month");
			return direction > 0
				? nextUnit(first, "month")
				: unitStart(first - 1, "month");
		}
	}
}

/** Whether `day` falls in the month the month grid is built around. */
export function inAnchorMonth(day: number, anchor: number): boolean {
	return unitStart(day, "month") === unitStart(anchor, "month");
}

// ─── Placement ────────────────────────────────────────────────────────────────

/**
 * The days a task is shown on for a date field, or null when it has none of
 * the dates the field uses. With start_and_due a task spans from its start to
 * its due date (one of the two is enough); otherwise it sits on that one date.
 */
export function placementSpan(
	task: Pick<Task, "start_date" | "due_date">,
	field: CalendarDateField,
): TaskSpan | null {
	switch (field) {
		case "start_date":
			return taskSpan(task.start_date, null);
		case "due_date":
			return taskSpan(null, task.due_date);
		default:
			return taskSpan(task.start_date, task.due_date);
	}
}

/** A task's span with both ends moved by `days`. */
export function shiftSpan(span: TaskSpan, days: number): TaskSpan {
	return { ...span, start: span.start + days, end: span.end + days };
}

/**
 * The update that gives an undated task (for the date field) a date: the day
 * it was dropped on or created for. With start_and_due it becomes a one-day
 * task, starting and due that day.
 */
export function scheduleOnDay(
	day: number,
	field: CalendarDateField,
): { start_date?: string; due_date?: string } {
	const iso = dayToISO(day);
	if (field === "start_date") return { start_date: iso };
	if (field === "due_date") return { due_date: iso };
	return { start_date: iso, due_date: iso };
}

// ─── Week rows ────────────────────────────────────────────────────────────────

/** The part of a task's span that falls in one row, and the lane it sits in. */
export interface RowSegment<T> {
	item: T;
	/** First and last day of the segment, inside the row. */
	start: number;
	end: number;
	/** The span goes on before the row's first day / after its last. */
	continuesBefore: boolean;
	continuesAfter: boolean;
	/** Stacking position from the top, 0 first. */
	lane: number;
}

export interface RowLayout<T> {
	segments: RowSegment<T>[];
	/** Lanes the row needs to show every segment. */
	laneCount: number;
}

/**
 * Cuts the spans that reach into a row (`rowStart`, `columns` days) to it and
 * stacks them: earlier starts first, longer spans first among those, then the
 * order given; each takes the topmost lane free on all of its days.
 */
export function layoutRow<T>(
	items: readonly { item: T; span: TaskSpan }[],
	rowStart: number,
	columns: number,
): RowLayout<T> {
	const rowEnd = rowStart + columns - 1;
	const cut = items
		.map(({ item, span }, order) => ({ item, span, order }))
		.filter(({ span }) => span.end >= rowStart && span.start <= rowEnd)
		.map(({ item, span, order }) => ({
			item,
			order,
			start: Math.max(span.start, rowStart),
			end: Math.min(span.end, rowEnd),
			continuesBefore: span.start < rowStart,
			continuesAfter: span.end > rowEnd,
		}))
		.sort(
			(a, b) =>
				a.start - b.start ||
				b.end - b.start - (a.end - a.start) ||
				a.order - b.order,
		);
	// The last day taken in each lane.
	const laneEnds: number[] = [];
	const segments: RowSegment<T>[] = cut.map(({ order: _order, ...seg }) => {
		let lane = laneEnds.findIndex((end) => end < seg.start);
		if (lane === -1) lane = laneEnds.length;
		laneEnds[lane] = seg.end;
		return { ...seg, lane };
	});
	return { segments, laneCount: laneEnds.length };
}

/**
 * How many lanes of a row to draw when only `fit` lanes have room: all of
 * them when they fit, otherwise one fewer than fit, keeping a line for the
 * "+N more" links.
 */
export function visibleLanes(laneCount: number, fit: number): number {
	if (laneCount <= fit) return laneCount;
	return Math.max(0, fit - 1);
}

/** Segments covering `day` that sit at or below lane `limit`, i.e. hidden. */
export function hiddenOnDay<T>(
	segments: readonly RowSegment<T>[],
	day: number,
	limit: number,
): RowSegment<T>[] {
	return segments.filter(
		(s) => s.lane >= limit && s.start <= day && s.end >= day,
	);
}

/** Segments covering `day`, top lane first. */
export function segmentsOnDay<T>(
	segments: readonly RowSegment<T>[],
	day: number,
): RowSegment<T>[] {
	return segments
		.filter((s) => s.start <= day && s.end >= day)
		.sort((a, b) => a.lane - b.lane);
}

// ─── Labels ───────────────────────────────────────────────────────────────────

function formatter(
	locale: string | undefined,
	opts: Intl.DateTimeFormatOptions,
) {
	const f = new Intl.DateTimeFormat(locale, { ...opts, timeZone: "UTC" });
	return (day: number) => f.format(new Date(day * MS_PER_DAY));
}

/** The days of one row with their short weekday names. */
export function weekdayLabels(
	rowStart: number,
	columns: number,
	locale: string | undefined,
): { day: number; label: string }[] {
	const short = formatter(locale, { weekday: "short" });
	return Array.from({ length: columns }, (_, i) => ({
		day: rowStart + i,
		label: short(rowStart + i),
	}));
}

/** The heading of a grid: its month, its week's days, or its day. */
export function gridTitle(
	grid: CalendarGrid,
	anchor: number,
	mode: CalendarMode,
	locale: string | undefined,
): string {
	if (mode === "month") {
		return formatter(locale, { month: "long", year: "numeric" })(anchor);
	}
	if (mode === "day") {
		return formatter(locale, {
			weekday: "long",
			month: "long",
			day: "numeric",
			year: "numeric",
		})(anchor);
	}
	const f = new Intl.DateTimeFormat(locale, {
		month: "short",
		day: "numeric",
		year: "numeric",
		timeZone: "UTC",
	});
	return f.formatRange(
		new Date(grid.start * MS_PER_DAY),
		new Date((grid.end - 1) * MS_PER_DAY),
	);
}

/** A day's full date, for screen readers and tooltips. */
export function fullDateLabel(day: number, locale: string | undefined): string {
	return formatter(locale, {
		weekday: "long",
		month: "long",
		day: "numeric",
		year: "numeric",
	})(day);
}
