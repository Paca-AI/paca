import { describe, expect, it } from "vitest";
import { createLimiter, mapPool } from "./concurrency";

const tick = (ms = 1) => new Promise((r) => setTimeout(r, ms));

describe("mapPool", () => {
	it("returns results in input order", async () => {
		const out = await mapPool([30, 1, 10], 3, async (ms, i) => {
			await tick(ms);
			return i;
		});
		expect(out).toEqual([0, 1, 2]);
	});

	it("never exceeds the limit", async () => {
		let active = 0;
		let peak = 0;
		await mapPool(
			Array.from({ length: 10 }, (_, i) => i),
			3,
			async () => {
				active++;
				peak = Math.max(peak, active);
				await tick(2);
				active--;
			},
		);
		expect(peak).toBe(3);
	});

	it("rejects with the first error, not a later one, and stops starting items", async () => {
		const started: number[] = [];
		const err = new Error("first");
		await expect(
			mapPool([0, 1, 2, 3, 4, 5], 2, async (n) => {
				started.push(n);
				if (n === 0) throw err;
				await tick(5);
				throw new Error("later");
			}),
		).rejects.toBe(err);
		expect(started.length).toBeLessThan(6);
	});

	it("aborts the signal of in-flight siblings but still reports the first error", async () => {
		const err = new Error("first");
		let siblingAborted = false;
		await expect(
			mapPool([0, 1], 2, async (n, _i, signal) => {
				if (n === 0) {
					await tick(1);
					throw err;
				}
				await new Promise<void>((_, reject) =>
					signal.addEventListener("abort", () => {
						siblingAborted = true;
						reject(new Error("aborted"));
					}),
				);
			}),
		).rejects.toBe(err);
		expect(siblingAborted).toBe(true);
	});

	it("aborts the item signal when the outer signal aborts", async () => {
		const outer = new AbortController();
		const p = mapPool(
			[0],
			1,
			(_n, _i, signal) =>
				new Promise<void>((_, reject) =>
					signal.addEventListener("abort", () => reject(new Error("stop"))),
				),
			outer.signal,
		);
		outer.abort();
		await expect(p).rejects.toThrow("stop");
	});

	it("treats a non-positive limit as 1", async () => {
		expect(await mapPool([1, 2, 3], 0, async (n) => n * 2)).toEqual([2, 4, 6]);
	});

	it("handles an empty list", async () => {
		expect(await mapPool([], 3, async () => 1)).toEqual([]);
	});
});

describe("createLimiter", () => {
	it("caps in-flight tasks and runs every queued one", async () => {
		const limit = createLimiter(2);
		let active = 0;
		let peak = 0;
		const results = await Promise.all(
			Array.from({ length: 6 }, (_, i) =>
				limit(async () => {
					active++;
					peak = Math.max(peak, active);
					await tick(2);
					active--;
					return i;
				}),
			),
		);
		expect(peak).toBe(2);
		expect(results).toEqual([0, 1, 2, 3, 4, 5]);
	});

	it("frees its slot when a task fails", async () => {
		const limit = createLimiter(1);
		await expect(limit(() => Promise.reject(new Error("x")))).rejects.toThrow();
		expect(await limit(async () => "ok")).toBe("ok");
	});

	it("does not exceed the limit when a call arrives as a slot is released", async () => {
		const limit = createLimiter(1);
		let active = 0;
		let peak = 0;
		const run = async () => {
			active++;
			peak = Math.max(peak, active);
			await tick(2);
			active--;
		};
		const first = limit(run);
		const second = limit(run);
		await first;
		// `first` has released; `second` is being woken. A new call must queue.
		const third = limit(run);
		await Promise.all([second, third]);
		expect(peak).toBe(1);
	});
});
