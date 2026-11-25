import { describe, expect, it } from "vitest";
import type { Task } from "@/lib/interaction-api";
import { daysUntilDue, summarizeDueDates, upcomingDueTasks } from "./due-dates";

// Local noon, so the local calendar date is Nov 25 in any test timezone.
const NOW = new Date(2026, 10, 25, 12, 0, 0);

function task(id: string, due_date: string | null): Task {
	return {
		id,
		project_id: "p",
		title: id,
		task_number: 1,
		importance: 0,
		custom_fields: {},
		created_at: "2026-01-01T00:00:00Z",
		updated_at: "2026-01-01T00:00:00Z",
		assignment_mode: "manual",
		due_date,
	};
}

describe("daysUntilDue", () => {
	it("reads the stored calendar date, not the instant", () => {
		expect(daysUntilDue("2026-11-25T00:00:00Z", NOW)).toBe(0);
		expect(daysUntilDue("2026-11-26T00:00:00Z", NOW)).toBe(1);
		expect(daysUntilDue("2026-11-23T00:00:00Z", NOW)).toBe(-2);
	});

	it("crosses month and year boundaries", () => {
		expect(daysUntilDue("2026-12-02T00:00:00Z", NOW)).toBe(7);
		expect(daysUntilDue("2027-01-01T00:00:00Z", NOW)).toBe(37);
	});

	it("returns null for missing or invalid dates", () => {
		expect(daysUntilDue(null, NOW)).toBeNull();
		expect(daysUntilDue(undefined, NOW)).toBeNull();
		expect(daysUntilDue("not a date", NOW)).toBeNull();
	});
});

describe("upcomingDueTasks", () => {
	it("keeps overdue and in-window tasks, soonest first", () => {
		const result = upcomingDueTasks(
			[
				task("later", "2026-12-20T00:00:00Z"),
				task("soon", "2026-11-28T00:00:00Z"),
				task("none", null),
				task("late", "2026-11-20T00:00:00Z"),
				task("today", "2026-11-25T00:00:00Z"),
			],
			14,
			NOW,
		);
		expect(result.map((r) => [r.task.id, r.days])).toEqual([
			["late", -5],
			["today", 0],
			["soon", 3],
		]);
	});
});

describe("summarizeDueDates", () => {
	it("counts overdue tasks and tasks due within seven days", () => {
		expect(
			summarizeDueDates(
				[
					task("late", "2026-11-24T00:00:00Z"),
					task("today", "2026-11-25T00:00:00Z"),
					task("week", "2026-12-02T00:00:00Z"),
					task("next", "2026-12-03T00:00:00Z"),
					task("none", null),
				],
				NOW,
			),
		).toEqual({ overdue: 1, dueThisWeek: 2 });
	});
});
