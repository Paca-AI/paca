import { describe, expect, it } from "bun:test";
import { IamEpochWatcher } from "./iam-epoch.ts";

function setup(initial: number | null) {
	let value = initial;
	let fail = false;
	let changes = 0;
	const w = new IamEpochWatcher({
		read: async () => {
			if (fail) throw new Error("down");
			return value;
		},
		onChange: () => {
			changes++;
		},
	});
	return {
		w,
		set: (v: number | null) => {
			value = v;
		},
		breakIt: (v: boolean) => {
			fail = v;
		},
		changes: () => changes,
	};
}

describe("IamEpochWatcher", () => {
	it("takes the first reading as a baseline, not as news", async () => {
		const t = setup(5);
		await t.w.poll();
		expect(t.changes()).toBe(0);
	});

	it("reacts once when the counter went up without an announcement being seen", async () => {
		const t = setup(5);
		await t.w.poll();
		t.set(6);
		await t.w.poll();
		await t.w.poll();
		expect(t.changes()).toBe(1);
	});

	it("does not react a second time to a change whose announcement arrived", async () => {
		const t = setup(5);
		await t.w.poll();
		t.w.observe(6);
		t.set(6);
		await t.w.poll();
		expect(t.changes()).toBe(0);
	});

	it("still catches a later change after observing an earlier one", async () => {
		const t = setup(5);
		await t.w.poll();
		t.w.observe(6);
		t.set(8);
		await t.w.poll();
		expect(t.changes()).toBe(1);
	});

	it("ignores a missing counter and an unreachable Valkey, and keeps going", async () => {
		const t = setup(null);
		await t.w.poll();
		t.set(3);
		await t.w.poll(); // baseline
		t.breakIt(true);
		t.set(4);
		await t.w.poll();
		expect(t.changes()).toBe(0);
		t.breakIt(false);
		await t.w.poll();
		expect(t.changes()).toBe(1);
	});

	it("ignores an announcement without a usable epoch", () => {
		const t = setup(1);
		t.w.observe(undefined);
		t.w.observe(Number.NaN);
		expect(t.changes()).toBe(0);
	});
});
