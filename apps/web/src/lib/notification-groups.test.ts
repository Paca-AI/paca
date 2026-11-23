import { describe, expect, it } from "vitest";

import { dayGroup, groupByDay } from "./notification-groups";

// Noon, local time, on 23 November 2026.
const now = new Date(2026, 10, 23, 12, 0, 0);
const at = (day: number, hour = 9) =>
	new Date(2026, 10, day, hour).toISOString();

describe("dayGroup", () => {
	it("buckets by the local calendar day", () => {
		expect(dayGroup(at(23, 0), now)).toBe("today");
		expect(dayGroup(at(23, 11), now)).toBe("today");
		expect(dayGroup(at(22, 23), now)).toBe("yesterday");
		expect(dayGroup(at(22, 0), now)).toBe("yesterday");
		expect(dayGroup(at(21, 23), now)).toBe("earlier");
		expect(dayGroup(at(1), now)).toBe("earlier");
	});

	it("treats a time slightly in the future (clock skew) as today", () => {
		expect(dayGroup(new Date(2026, 10, 23, 12, 5).toISOString(), now)).toBe(
			"today",
		);
	});
});

describe("groupByDay", () => {
	it("keeps order and starts a new group where the day changes", () => {
		const items = [
			{ id: 1, created_at: at(23, 10) },
			{ id: 2, created_at: at(23, 8) },
			{ id: 3, created_at: at(22, 20) },
			{ id: 4, created_at: at(10) },
			{ id: 5, created_at: at(9) },
		];
		const groups = groupByDay(items, now);
		expect(groups.map((g) => [g.group, g.items.map((i) => i.id)])).toEqual([
			["today", [1, 2]],
			["yesterday", [3]],
			["earlier", [4, 5]],
		]);
	});

	it("omits empty groups and handles an empty list", () => {
		expect(groupByDay([], now)).toEqual([]);
		expect(
			groupByDay([{ created_at: at(1) }], now).map((g) => g.group),
		).toEqual(["earlier"]);
	});
});
