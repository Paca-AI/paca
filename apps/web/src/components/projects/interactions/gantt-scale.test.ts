import { describe, expect, it } from "vitest";
import {
	barBox,
	buildScale,
	chartWidth,
	computeRange,
	DEFAULT_GANTT_ZOOM,
	dayToISO,
	dayToPx,
	dependencyPath,
	dragDays,
	dragSpan,
	localToday,
	MIN_BAR_PX,
	nextUnit,
	PX_PER_DAY,
	rescheduleUpdate,
	resolveGanttZoom,
	type TaskSpan,
	taskSpan,
	toDay,
	unitStart,
	violatesBlock,
} from "./gantt-scale";

const day = (iso: string) => toDay(`${iso}T00:00:00Z`) as number;
const span = (start: string, end: string): TaskSpan => ({
	start: day(start),
	end: day(end),
	hasStart: true,
	hasDue: true,
});

describe("dates as day numbers", () => {
	it("reads the UTC calendar date of a stored value", () => {
		expect(toDay("2026-03-02T00:00:00Z")).toBe(20514);
		// Any time on that UTC date is the same day.
		expect(toDay("2026-03-02T23:59:59Z")).toBe(20514);
		expect(toDay("2026-03-02")).toBe(20514);
		expect(toDay(null)).toBeNull();
		expect(toDay("")).toBeNull();
		expect(toDay("not a date")).toBeNull();
	});

	it("writes a day back as UTC midnight", () => {
		expect(dayToISO(20514)).toBe("2026-03-02T00:00:00Z");
		expect(dayToISO(day("2024-02-29"))).toBe("2024-02-29T00:00:00Z");
	});

	it("takes today from the local calendar date", () => {
		expect(localToday(new Date(2026, 2, 2, 23, 30))).toBe(20514);
		expect(localToday(new Date(2026, 2, 2, 0, 5))).toBe(20514);
	});
});

describe("taskSpan", () => {
	it("spans start to due, both inclusive", () => {
		expect(taskSpan("2026-03-02T00:00:00Z", "2026-03-04T00:00:00Z")).toEqual({
			start: 20514,
			end: 20516,
			hasStart: true,
			hasDue: true,
		});
	});

	it("occupies the one day of a task with a single date", () => {
		expect(taskSpan(null, "2026-03-04T00:00:00Z")).toEqual({
			start: 20516,
			end: 20516,
			hasStart: false,
			hasDue: true,
		});
		expect(taskSpan("2026-03-02T00:00:00Z", undefined)).toEqual({
			start: 20514,
			end: 20514,
			hasStart: true,
			hasDue: false,
		});
	});

	it("is null without dates and tolerates a due date before the start", () => {
		expect(taskSpan(null, null)).toBeNull();
		expect(taskSpan("2026-03-04", "2026-03-02")).toMatchObject({
			start: 20514,
			end: 20516,
		});
	});
});

describe("zoom", () => {
	it("falls back to the default for an unset or unknown zoom", () => {
		expect(resolveGanttZoom("quarter")).toBe("quarter");
		expect(resolveGanttZoom(undefined)).toBe(DEFAULT_GANTT_ZOOM);
		expect(resolveGanttZoom("fortnight")).toBe(DEFAULT_GANTT_ZOOM);
	});

	it("gets coarser from day to year", () => {
		expect(PX_PER_DAY.day).toBeGreaterThan(PX_PER_DAY.week);
		expect(PX_PER_DAY.week).toBeGreaterThan(PX_PER_DAY.month);
		expect(PX_PER_DAY.month).toBeGreaterThan(PX_PER_DAY.quarter);
		expect(PX_PER_DAY.quarter).toBeGreaterThan(PX_PER_DAY.year);
	});
});

describe("units", () => {
	it("finds the start of the week, month, quarter and year", () => {
		const wed = day("2026-03-04");
		expect(unitStart(wed, "day")).toBe(wed);
		expect(unitStart(wed, "week")).toBe(day("2026-03-02")); // Monday
		expect(unitStart(day("2026-03-02"), "week")).toBe(day("2026-03-02"));
		expect(unitStart(day("2026-03-08"), "week")).toBe(day("2026-03-02")); // Sunday
		expect(unitStart(wed, "month")).toBe(day("2026-03-01"));
		expect(unitStart(wed, "quarter")).toBe(day("2026-01-01"));
		expect(unitStart(day("2026-12-31"), "quarter")).toBe(day("2026-10-01"));
		expect(unitStart(wed, "year")).toBe(day("2026-01-01"));
	});

	it("steps to the next unit across month and year ends", () => {
		expect(nextUnit(day("2026-03-02"), "week")).toBe(day("2026-03-09"));
		expect(nextUnit(day("2026-01-01"), "month")).toBe(day("2026-02-01"));
		expect(nextUnit(day("2026-12-01"), "month")).toBe(day("2027-01-01"));
		expect(nextUnit(day("2026-10-01"), "quarter")).toBe(day("2027-01-01"));
		expect(nextUnit(day("2026-01-01"), "year")).toBe(day("2027-01-01"));
	});
});

describe("computeRange", () => {
	const spans = [
		span("2026-03-04", "2026-03-10"),
		span("2026-03-20", "2026-04-02"),
	];
	const today = day("2026-03-15");

	it("covers every span in whole weeks plus two weeks each side", () => {
		const r = computeRange(spans, today, "week");
		expect(r.start).toBe(day("2026-02-16")); // Monday two weeks before Mar 2
		expect(r.end).toBe(day("2026-04-20")); // two weeks after the week of Apr 2
		expect((r.end - r.start) % 7).toBe(0);
	});

	it("pads a day zoom by a week and a month zoom by a month", () => {
		expect(computeRange(spans, today, "day")).toEqual({
			start: day("2026-02-25"),
			end: day("2026-04-10"),
		});
		expect(computeRange(spans, today, "month")).toEqual({
			start: day("2026-02-01"),
			end: day("2026-06-01"),
		});
	});

	it("aligns to quarters and years", () => {
		expect(computeRange(spans, today, "quarter")).toEqual({
			start: day("2025-10-01"),
			end: day("2026-10-01"),
		});
		expect(computeRange(spans, today, "year")).toEqual({
			start: day("2025-01-01"),
			end: day("2028-01-01"),
		});
	});

	it("always includes today, even with no dated tasks", () => {
		const far = day("2027-06-01");
		const r = computeRange(spans, far, "month");
		expect(r.start).toBeLessThanOrEqual(day("2026-03-04"));
		expect(r.end).toBeGreaterThan(far);
		const empty = computeRange([], today, "week");
		expect(empty.start).toBeLessThan(today);
		expect(empty.end).toBeGreaterThan(today);
	});
});

describe("buildScale", () => {
	it("shows days under months at the day zoom", () => {
		const r = { start: day("2026-02-27"), end: day("2026-03-03") };
		const s = buildScale(r, "day", { locale: "en-US" });
		expect(s.minor.map((c) => c.label)).toEqual(["27", "28", "1", "2"]);
		expect(s.minor.map((c) => c.left)).toEqual([0, 40, 80, 120]);
		expect(s.major.map((c) => c.label)).toEqual([
			"February 2026",
			"March 2026",
		]);
		// Cells are clipped to the range.
		expect(s.major.map((c) => [c.left, c.width])).toEqual([
			[0, 80],
			[80, 80],
		]);
	});

	it("labels weeks by their Monday", () => {
		const r = { start: day("2026-03-02"), end: day("2026-03-16") };
		const s = buildScale(r, "week", { locale: "en-US" });
		expect(s.minor.map((c) => c.label)).toEqual(["Mar 2", "Mar 9"]);
		expect(s.minor.every((c) => c.width === 7 * PX_PER_DAY.week)).toBe(true);
	});

	it("shows quarters under years, with a translatable label", () => {
		const r = { start: day("2026-01-01"), end: day("2027-01-01") };
		const s = buildScale(r, "quarter", {
			quarterLabel: (n) => `T${n}`,
		});
		expect(s.minor.map((c) => c.label)).toEqual(["T1", "T2", "T3", "T4"]);
		expect(s.major.map((c) => c.label)).toEqual(["2026"]);
		const total = s.minor.reduce((sum, c) => sum + c.width, 0);
		expect(total).toBe(chartWidth(r, "quarter"));
	});

	it("has a single band at the year zoom", () => {
		const r = { start: day("2025-01-01"), end: day("2027-01-01") };
		const s = buildScale(r, "year");
		expect(s.major).toEqual([]);
		expect(s.minor.map((c) => c.label)).toEqual(["2025", "2026"]);
	});
});

describe("bars", () => {
	const r = { start: day("2026-03-01"), end: day("2026-04-01") };

	it("runs from the start of the first day to the end of the last", () => {
		expect(barBox(span("2026-03-03", "2026-03-05"), r, "day")).toEqual({
			left: 2 * 40,
			width: 3 * 40,
		});
	});

	it("keeps a one-day bar visible at a coarse zoom", () => {
		const box = barBox(span("2026-03-03", "2026-03-03"), r, "year");
		expect(box.width).toBe(MIN_BAR_PX);
	});

	it("stays inside the chart when the span falls outside the range", () => {
		const width = chartWidth(r, "day");
		const before = barBox(span("2026-02-01", "2026-02-03"), r, "day");
		expect(before.left).toBe(0);
		expect(before.width).toBe(MIN_BAR_PX);
		const after = barBox(span("2026-05-01", "2026-05-03"), r, "day");
		expect(after.left + after.width).toBeLessThanOrEqual(width);
	});

	it("places a day at its offset from the start of the chart", () => {
		expect(dayToPx(day("2026-03-11"), r, "day")).toBe(400);
		expect(dayToPx(day("2026-03-11"), r, "week")).toBe(200);
	});
});

describe("dragging", () => {
	const s = span("2026-03-10", "2026-03-12");

	it("snaps a drag to whole days at the current zoom", () => {
		expect(dragDays(45, "day")).toBe(1);
		expect(dragDays(19, "day")).toBe(0);
		expect(dragDays(-61, "day")).toBe(-2);
		expect(dragDays(61, "week")).toBe(3);
		expect(dragDays(-1, "day")).toBe(0);
	});

	it("moves both ends, or one edge without passing the other", () => {
		expect(dragSpan(s, "move", 3)).toMatchObject({
			start: day("2026-03-13"),
			end: day("2026-03-15"),
		});
		expect(dragSpan(s, "start", -2)).toMatchObject({
			start: day("2026-03-08"),
			end: day("2026-03-12"),
		});
		expect(dragSpan(s, "start", 10)).toMatchObject({
			start: day("2026-03-12"),
			end: day("2026-03-12"),
		});
		expect(dragSpan(s, "end", -10)).toMatchObject({
			start: day("2026-03-10"),
			end: day("2026-03-10"),
		});
	});

	it("writes both dates when moving a task that has both", () => {
		expect(rescheduleUpdate(s, dragSpan(s, "move", 2), "move")).toEqual({
			start_date: "2026-03-12T00:00:00Z",
			due_date: "2026-03-14T00:00:00Z",
		});
	});

	it("only shifts the date a single-date task has", () => {
		const dueOnly = { ...span("2026-03-10", "2026-03-10"), hasStart: false };
		expect(
			rescheduleUpdate(dueOnly, dragSpan(dueOnly, "move", 1), "move"),
		).toEqual({ due_date: "2026-03-11T00:00:00Z" });
		const startOnly = { ...span("2026-03-10", "2026-03-10"), hasDue: false };
		expect(
			rescheduleUpdate(startOnly, dragSpan(startOnly, "move", -1), "move"),
		).toEqual({ start_date: "2026-03-09T00:00:00Z" });
	});

	it("sets the dragged edge, filling in the other date of a single-date task", () => {
		expect(rescheduleUpdate(s, dragSpan(s, "end", 2), "end")).toEqual({
			due_date: "2026-03-14T00:00:00Z",
		});
		const dueOnly = { ...span("2026-03-10", "2026-03-10"), hasStart: false };
		expect(
			rescheduleUpdate(dueOnly, dragSpan(dueOnly, "start", -3), "start"),
		).toEqual({ start_date: "2026-03-07T00:00:00Z" });
		const startOnly = { ...span("2026-03-10", "2026-03-10"), hasDue: false };
		expect(
			rescheduleUpdate(startOnly, dragSpan(startOnly, "end", 4), "end"),
		).toEqual({ due_date: "2026-03-14T00:00:00Z" });
		expect(
			rescheduleUpdate(startOnly, dragSpan(startOnly, "start", -2), "start"),
		).toEqual({
			start_date: "2026-03-08T00:00:00Z",
			due_date: "2026-03-10T00:00:00Z",
		});
	});

	it("changes nothing when the drag ends where it started", () => {
		expect(rescheduleUpdate(s, dragSpan(s, "move", 0), "move")).toEqual({});
		const oneDay = span("2026-03-10", "2026-03-10");
		expect(
			rescheduleUpdate(oneDay, dragSpan(oneDay, "start", 3), "start"),
		).toEqual({});
	});
});

describe("dependencies", () => {
	const blocker = span("2026-03-02", "2026-03-06");

	it("flags a blocked task that starts on or before the blocker's last day", () => {
		expect(violatesBlock(blocker, span("2026-03-07", "2026-03-09"))).toBe(
			false,
		);
		expect(violatesBlock(blocker, span("2026-03-06", "2026-03-09"))).toBe(true);
		expect(violatesBlock(blocker, span("2026-03-01", "2026-03-09"))).toBe(true);
	});

	it("steps down once when the blocked task starts well to the right", () => {
		expect(dependencyPath({ x: 100, y: 20 }, { x: 160, y: 60 })).toBe(
			"M100,20 H108 V60 H160",
		);
	});

	it("doubles back between the rows when the blocked task starts earlier", () => {
		expect(dependencyPath({ x: 100, y: 20 }, { x: 60, y: 100 })).toBe(
			"M100,20 H108 V60 H52 V100 H60",
		);
		// Going up works the same way.
		expect(dependencyPath({ x: 100, y: 100 }, { x: 90, y: 20 })).toBe(
			"M100,100 H108 V60 H82 V20 H90",
		);
	});
});
