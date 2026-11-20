import { describe, expect, it } from "vitest";
import { isFollowUp, layout, type TimelineEntry } from "./timeline";

const at = (h: number, m = 0, day = 10) =>
	new Date(2026, 10, day, h, m).toISOString();
const e = (
	id: string,
	author: string,
	time: string,
	seq?: number,
): TimelineEntry => ({
	id,
	authorId: author,
	at: time,
	seq,
});

describe("isFollowUp", () => {
	it("groups the same author within five minutes", () => {
		expect(isFollowUp(e("1", "a", at(9, 0)), e("2", "a", at(9, 4)))).toBe(true);
		expect(isFollowUp(e("1", "a", at(9, 0)), e("2", "a", at(9, 6)))).toBe(
			false,
		);
		expect(isFollowUp(e("1", "a", at(9, 0)), e("2", "b", at(9, 1)))).toBe(
			false,
		);
		expect(isFollowUp(undefined, e("2", "a", at(9, 1)))).toBe(false);
	});

	it("does not group after a deleted message", () => {
		expect(
			isFollowUp(
				{ ...e("1", "a", at(9)), deleted: true },
				e("2", "a", at(9, 1)),
			),
		).toBe(false);
	});
});

describe("layout", () => {
	it("marks the first entry of every day", () => {
		const slots = layout([
			e("1", "a", at(23, 50, 10)),
			e("2", "a", at(0, 5, 11)),
			e("3", "a", at(0, 6, 11)),
		]);
		expect(slots.map((s) => s.dayStart)).toEqual([true, true, false]);
		// A day break also ends a run of one author.
		expect(slots.map((s) => s.compact)).toEqual([false, false, true]);
	});

	it("marks where the unread begin, once, and breaks the run there", () => {
		const slots = layout(
			[
				e("1", "a", at(9, 0), 1),
				e("2", "a", at(9, 1), 2),
				e("3", "a", at(9, 2), 3),
				e("4", "a", at(9, 3), 4),
			],
			2,
		);
		expect(slots.map((s) => s.newStart)).toEqual([false, false, true, false]);
		expect(slots[2]?.compact).toBe(false);
		expect(slots[3]?.compact).toBe(true);
	});

	it("shows no divider when everything is read or nothing was read before", () => {
		const entries = [e("1", "a", at(9), 1), e("2", "b", at(9, 1), 2)];
		expect(layout(entries, 2).some((s) => s.newStart)).toBe(false);
		expect(layout(entries, 0).some((s) => s.newStart)).toBe(false);
		expect(layout(entries).some((s) => s.newStart)).toBe(false);
	});
});
