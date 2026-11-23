import { describe, expect, it } from "vitest";

import { dndEndTimes, isDoNotDisturbActive } from "./do-not-disturb";
import type { DoNotDisturb } from "./push-api";

const off: DoNotDisturb = { active: false, until: null, indefinite: false };

describe("isDoNotDisturbActive", () => {
	const now = new Date("2026-11-23T12:00:00Z").getTime();

	it("is off when unknown or off", () => {
		expect(isDoNotDisturbActive(undefined, now)).toBe(false);
		expect(isDoNotDisturbActive(off, now)).toBe(false);
	});

	it("is on until the end time, and off after it even if the server's flag is stale", () => {
		const dnd = {
			active: true,
			until: "2026-11-23T13:00:00Z",
			indefinite: false,
		};
		expect(isDoNotDisturbActive(dnd, now)).toBe(true);
		expect(
			isDoNotDisturbActive(dnd, new Date("2026-11-23T13:00:01Z").getTime()),
		).toBe(false);
	});

	it("is on indefinitely when asked", () => {
		expect(
			isDoNotDisturbActive(
				{ active: true, until: null, indefinite: true },
				now,
			),
		).toBe(true);
	});

	it("is not on for an active flag with neither an end nor indefinite", () => {
		expect(
			isDoNotDisturbActive(
				{ active: true, until: null, indefinite: false },
				now,
			),
		).toBe(false);
	});
});

describe("dndEndTimes", () => {
	it("offers one and eight hours ahead", () => {
		const now = new Date(2026, 10, 23, 12, 0, 0);
		const t = dndEndTimes(now);
		expect(t.oneHour.getTime() - now.getTime()).toBe(3_600_000);
		expect(t.eightHours.getTime() - now.getTime()).toBe(8 * 3_600_000);
	});

	it("means the next 08:00: tomorrow's in the afternoon, today's before it", () => {
		const afternoon = dndEndTimes(new Date(2026, 10, 23, 15, 0, 0)).nextMorning;
		expect([
			afternoon.getDate(),
			afternoon.getHours(),
			afternoon.getMinutes(),
		]).toEqual([24, 8, 0]);

		const small = dndEndTimes(new Date(2026, 10, 23, 3, 30, 0)).nextMorning;
		expect([small.getDate(), small.getHours()]).toEqual([23, 8]);

		const exactly = dndEndTimes(new Date(2026, 10, 23, 8, 0, 0)).nextMorning;
		expect(exactly.getDate()).toBe(24);
	});
});
