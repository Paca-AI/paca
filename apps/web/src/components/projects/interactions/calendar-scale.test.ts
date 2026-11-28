import { describe, expect, it } from "vitest";

import {
	buildGrid,
	dayToDateParam,
	firstDayOfWeek,
	gridTitle,
	hiddenOnDay,
	inAnchorMonth,
	layoutRow,
	placementSpan,
	resolveCalendarDateField,
	resolveCalendarMode,
	scheduleOnDay,
	segmentsOnDay,
	shiftAnchor,
	shiftSpan,
	visibleLanes,
	weekdayLabels,
	weekdayOf,
	weekStartOf,
} from "./calendar-scale";
import { type TaskSpan, toDay } from "./gantt-scale";

/** Day number of a "YYYY-MM-DD" date. */
const d = (date: string) => toDay(`${date}T00:00:00Z`) as number;

const span = (start: string, end: string): TaskSpan => ({
	start: d(start),
	end: d(end),
	hasStart: true,
	hasDue: true,
});

describe("resolveCalendarMode / resolveCalendarDateField", () => {
	it("keeps a known value and falls back to the default otherwise", () => {
		expect(resolveCalendarMode("week")).toBe("week");
		expect(resolveCalendarMode("day")).toBe("day");
		expect(resolveCalendarMode(undefined)).toBe("month");
		expect(resolveCalendarMode("fortnight")).toBe("month");
		expect(resolveCalendarDateField("due_date")).toBe("due_date");
		expect(resolveCalendarDateField(null)).toBe("start_and_due");
		expect(resolveCalendarDateField("created_at")).toBe("start_and_due");
	});
});

describe("weeks", () => {
	it("numbers weekdays from Sunday", () => {
		expect(weekdayOf(d("1970-01-01"))).toBe(4); // Thursday
		expect(weekdayOf(d("2026-03-01"))).toBe(0); // Sunday
		expect(weekdayOf(d("2026-03-07"))).toBe(6); // Saturday
		expect(weekdayOf(d("1969-12-31"))).toBe(3); // Wednesday, a negative day
	});

	it("finds the first day of a week for either week start", () => {
		// Wednesday 2026-03-04.
		expect(weekStartOf(d("2026-03-04"), 1)).toBe(d("2026-03-02"));
		expect(weekStartOf(d("2026-03-04"), 0)).toBe(d("2026-03-01"));
		expect(weekStartOf(d("2026-03-01"), 1)).toBe(d("2026-02-23"));
		expect(weekStartOf(d("2026-03-02"), 1)).toBe(d("2026-03-02"));
	});

	it("takes the first day of the week from the locale", () => {
		expect(firstDayOfWeek("en-US")).toBe(0);
		expect(firstDayOfWeek("en")).toBe(0);
		expect(firstDayOfWeek("ja")).toBe(0);
		expect(firstDayOfWeek("pt-BR")).toBe(0);
		expect(firstDayOfWeek("en-GB")).toBe(1);
		expect(firstDayOfWeek("fr")).toBe(1);
		expect(firstDayOfWeek("ru")).toBe(1);
		expect(firstDayOfWeek("vi")).toBe(1);
		expect(firstDayOfWeek(undefined)).toBe(1);
		expect(firstDayOfWeek("not a locale!")).toBe(1);
	});
});

describe("buildGrid", () => {
	it("covers the anchor's month with whole weeks", () => {
		// March 2026 starts on a Sunday and ends on a Tuesday.
		const monday = buildGrid(d("2026-03-18"), "month", 1);
		expect(monday.start).toBe(d("2026-02-23"));
		expect(monday.end).toBe(d("2026-04-06"));
		expect(monday.columns).toBe(7);
		expect(monday.rows).toHaveLength(6);
		expect(monday.rows[1]).toBe(d("2026-03-02"));

		const sunday = buildGrid(d("2026-03-18"), "month", 0);
		expect(sunday.start).toBe(d("2026-03-01"));
		expect(sunday.end).toBe(d("2026-04-05"));
		expect(sunday.rows).toHaveLength(5);
	});

	it("needs only four rows for a February that fits them", () => {
		// February 2026 runs Sunday the 1st to Saturday the 28th.
		const grid = buildGrid(d("2026-02-10"), "month", 0);
		expect(grid.rows).toHaveLength(4);
		expect(grid.start).toBe(d("2026-02-01"));
		expect(grid.end).toBe(d("2026-03-01"));
	});

	it("shows the anchor's week, or the anchor alone", () => {
		const week = buildGrid(d("2026-03-04"), "week", 1);
		expect(week).toEqual({
			start: d("2026-03-02"),
			end: d("2026-03-09"),
			columns: 7,
			rows: [d("2026-03-02")],
		});
		const day = buildGrid(d("2026-03-04"), "day", 1);
		expect(day).toEqual({
			start: d("2026-03-04"),
			end: d("2026-03-05"),
			columns: 1,
			rows: [d("2026-03-04")],
		});
	});
});

describe("shiftAnchor", () => {
	it("steps a day, a week or a month at a time", () => {
		expect(shiftAnchor(d("2026-03-04"), "day", 1)).toBe(d("2026-03-05"));
		expect(shiftAnchor(d("2026-03-04"), "week", -1)).toBe(d("2026-02-25"));
		expect(shiftAnchor(d("2026-01-31"), "month", 1)).toBe(d("2026-02-01"));
		expect(shiftAnchor(d("2026-03-31"), "month", -1)).toBe(d("2026-02-01"));
		expect(shiftAnchor(d("2026-01-15"), "month", -1)).toBe(d("2025-12-01"));
	});

	it("tells the anchor's month from the days around it", () => {
		expect(inAnchorMonth(d("2026-03-31"), d("2026-03-01"))).toBe(true);
		expect(inAnchorMonth(d("2026-02-28"), d("2026-03-01"))).toBe(false);
	});
});

describe("placement", () => {
	const task = {
		start_date: "2026-03-03T00:00:00Z",
		due_date: "2026-03-06T00:00:00Z",
	};

	it("spans start to due, or sits on the one date the field uses", () => {
		expect(placementSpan(task, "start_and_due")).toEqual(
			span("2026-03-03", "2026-03-06"),
		);
		expect(placementSpan(task, "start_date")).toEqual({
			start: d("2026-03-03"),
			end: d("2026-03-03"),
			hasStart: true,
			hasDue: false,
		});
		expect(placementSpan(task, "due_date")).toEqual({
			start: d("2026-03-06"),
			end: d("2026-03-06"),
			hasStart: false,
			hasDue: true,
		});
	});

	it("places a task with one date on that day, and none without", () => {
		const dueOnly = { start_date: null, due_date: "2026-03-06T00:00:00Z" };
		expect(placementSpan(dueOnly, "start_and_due")?.start).toBe(
			d("2026-03-06"),
		);
		expect(placementSpan(dueOnly, "start_date")).toBeNull();
		expect(
			placementSpan({ start_date: null, due_date: null }, "start_and_due"),
		).toBeNull();
	});

	it("moves a span and gives an undated task the day's date", () => {
		expect(shiftSpan(span("2026-03-03", "2026-03-06"), 2)).toEqual(
			span("2026-03-05", "2026-03-08"),
		);
		const day = d("2026-03-09");
		expect(scheduleOnDay(day, "start_and_due")).toEqual({
			start_date: "2026-03-09T00:00:00Z",
			due_date: "2026-03-09T00:00:00Z",
		});
		expect(scheduleOnDay(day, "start_date")).toEqual({
			start_date: "2026-03-09T00:00:00Z",
		});
		expect(scheduleOnDay(day, "due_date")).toEqual({
			due_date: "2026-03-09T00:00:00Z",
		});
		expect(dayToDateParam(day)).toBe("2026-03-09");
	});
});

describe("layoutRow", () => {
	const row = d("2026-03-02"); // a Monday-first week row

	it("cuts spans to the row and records what goes on past it", () => {
		const { segments, laneCount } = layoutRow(
			[
				{ item: "long", span: span("2026-02-25", "2026-03-11") },
				{ item: "inside", span: span("2026-03-04", "2026-03-05") },
				{ item: "outside", span: span("2026-03-09", "2026-03-10") },
			],
			row,
			7,
		);
		expect(laneCount).toBe(2);
		expect(segments).toEqual([
			{
				item: "long",
				start: row,
				end: row + 6,
				continuesBefore: true,
				continuesAfter: true,
				lane: 0,
			},
			{
				item: "inside",
				start: d("2026-03-04"),
				end: d("2026-03-05"),
				continuesBefore: false,
				continuesAfter: false,
				lane: 1,
			},
		]);
	});

	it("reuses a lane once it is free and puts longer spans first", () => {
		const { segments, laneCount } = layoutRow(
			[
				{ item: "a", span: span("2026-03-02", "2026-03-03") },
				{ item: "b", span: span("2026-03-04", "2026-03-04") },
				{ item: "c", span: span("2026-03-02", "2026-03-05") },
				{ item: "d", span: span("2026-03-03", "2026-03-03") },
			],
			row,
			7,
		);
		const lanes = Object.fromEntries(segments.map((s) => [s.item, s.lane]));
		// c is longest of the Monday starts, so it goes on top.
		expect(lanes).toEqual({ c: 0, a: 1, d: 2, b: 1 });
		expect(laneCount).toBe(3);
	});

	it("keeps the given order for equal spans", () => {
		const { segments } = layoutRow(
			[
				{ item: "first", span: span("2026-03-04", "2026-03-04") },
				{ item: "second", span: span("2026-03-04", "2026-03-04") },
			],
			row,
			7,
		);
		expect(segments.map((s) => s.item)).toEqual(["first", "second"]);
	});
});

describe("overflow", () => {
	it("keeps a lane for the +N more links when lanes don't fit", () => {
		expect(visibleLanes(2, 3)).toBe(2);
		expect(visibleLanes(3, 3)).toBe(3);
		expect(visibleLanes(4, 3)).toBe(2);
		expect(visibleLanes(4, 0)).toBe(0);
	});

	it("counts the hidden segments of a day and lists every one", () => {
		const row = d("2026-03-02");
		const { segments } = layoutRow(
			[
				{ item: "a", span: span("2026-03-02", "2026-03-04") },
				{ item: "b", span: span("2026-03-03", "2026-03-03") },
				{ item: "c", span: span("2026-03-03", "2026-03-05") },
			],
			row,
			7,
		);
		const tuesday = d("2026-03-03");
		expect(hiddenOnDay(segments, tuesday, 1).map((s) => s.item)).toEqual([
			"c",
			"b",
		]);
		expect(
			hiddenOnDay(segments, d("2026-03-05"), 1).map((s) => s.item),
		).toEqual(["c"]);
		expect(segmentsOnDay(segments, tuesday).map((s) => s.item)).toEqual([
			"a",
			"c",
			"b",
		]);
	});
});

describe("labels", () => {
	it("names the weekdays from the row's first day", () => {
		const labels = weekdayLabels(d("2026-03-02"), 7, "en-US");
		expect(labels.map((l) => l.label)).toEqual([
			"Mon",
			"Tue",
			"Wed",
			"Thu",
			"Fri",
			"Sat",
			"Sun",
		]);
		expect(labels[0].day).toBe(d("2026-03-02"));
		expect(labels[6].day).toBe(d("2026-03-08"));
	});

	it("titles a grid by its month, week or day", () => {
		const anchor = d("2026-03-04");
		expect(
			gridTitle(buildGrid(anchor, "month", 1), anchor, "month", "en-US"),
		).toBe("March 2026");
		expect(gridTitle(buildGrid(anchor, "day", 1), anchor, "day", "en-US")).toBe(
			"Wednesday, March 4, 2026",
		);
		expect(
			gridTitle(buildGrid(anchor, "week", 1), anchor, "week", "en-US"),
		).toMatch(/Mar 2\s*–\s*8, 2026/);
	});
});
