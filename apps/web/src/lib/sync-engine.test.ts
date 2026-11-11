import { describe, expect, it, vi } from "vitest";
import { ProjectSync, type SyncBatch } from "./sync-engine";

function setup() {
	const cb = {
		onChange: vi.fn(),
		onReset: vi.fn(),
		requestResync: vi.fn(),
	};
	return { sync: new ProjectSync(cb), cb };
}

const change = (seq: number) => ({
	seq,
	type: "task.updated",
	payload: { n: seq },
});
const batch = (from: number, to: number, epoch = 7): SyncBatch => ({
	projectId: "p",
	epoch,
	from,
	to,
	changes: Array.from({ length: to - from }, (_, i) => change(from + i + 1)),
});

describe("ProjectSync", () => {
	it("starts a first join from the head its data was read at, and keeps a cursor it already has", () => {
		const { sync } = setup();
		sync.seed({ seq: 4, epoch: 7 });
		expect(sync.joinRequest()).toEqual({ cursor: 4, epoch: 7 });
		sync.seed({ seq: 99, epoch: 7 });
		expect(sync.cursor).toBe(4);
	});

	it("starts at the head on a first join, which needs no refetch", () => {
		const { sync, cb } = setup();
		expect(sync.applyJoinAck({ epoch: 7, head: 10, mode: "live" })).toBe(false);
		expect(sync.cursor).toBe(10);
		expect(cb.onReset).not.toHaveBeenCalled();
	});

	it("applies a batch that follows the cursor, in order, and advances it", () => {
		const { sync, cb } = setup();
		sync.applyJoinAck({ epoch: 7, head: 10, mode: "live" });
		sync.handleBatch(batch(10, 12));
		expect(cb.onChange.mock.calls.map(([e]) => e.payload.n)).toEqual([11, 12]);
		expect(sync.cursor).toBe(12);
	});

	it("does not apply skipped changes but still advances past them", () => {
		const { sync, cb } = setup();
		sync.applyJoinAck({ epoch: 7, head: 0, mode: "live" });
		sync.handleBatch({
			projectId: "p",
			epoch: 7,
			from: 0,
			to: 2,
			changes: [{ seq: 1, skip: true }, change(2)],
		});
		expect(cb.onChange).toHaveBeenCalledTimes(1);
		expect(sync.cursor).toBe(2);
	});

	it("drops a batch it already applied and applies only the new tail of an overlap", () => {
		const { sync, cb } = setup();
		sync.applyJoinAck({ epoch: 7, head: 0, mode: "live" });
		sync.handleBatch(batch(0, 3));
		cb.onChange.mockClear();
		sync.handleBatch(batch(0, 3));
		expect(cb.onChange).not.toHaveBeenCalled();
		sync.handleBatch(batch(2, 5));
		expect(cb.onChange.mock.calls.map(([e]) => e.payload.n)).toEqual([4, 5]);
	});

	it("asks to re-join, applying nothing, when a batch was missed", () => {
		const { sync, cb } = setup();
		sync.applyJoinAck({ epoch: 7, head: 3, mode: "live" });
		sync.handleBatch(batch(5, 6));
		expect(cb.requestResync).toHaveBeenCalledTimes(1);
		expect(cb.onChange).not.toHaveBeenCalled();
		expect(sync.cursor).toBe(3);
		// The re-join carries the cursor so the server replays from it.
		expect(sync.joinRequest()).toEqual({ cursor: 3, epoch: 7 });
	});

	it("asks to re-join when the log's epoch changed", () => {
		const { sync, cb } = setup();
		sync.applyJoinAck({ epoch: 7, head: 3, mode: "live" });
		sync.handleBatch(batch(3, 4, 8));
		expect(cb.requestResync).toHaveBeenCalled();
		expect(cb.onChange).not.toHaveBeenCalled();
	});

	it("adopts a batch that beat the join ack on a first join", () => {
		const { sync, cb } = setup();
		sync.handleBatch(batch(4, 5));
		expect(cb.onChange).toHaveBeenCalledTimes(1);
		expect(sync.cursor).toBe(5);
	});

	describe("rejoining with a cursor", () => {
		function joined() {
			const t = setup();
			t.sync.applyJoinAck({ epoch: 7, head: 3, mode: "live" });
			return t;
		}

		it("is resumed, with no refetch, when the server replays", () => {
			const { sync, cb } = joined();
			sync.handleBatch(batch(3, 6)); // the replay arrives before the ack
			expect(sync.applyJoinAck({ epoch: 7, head: 6, mode: "replay" })).toBe(
				true,
			);
			expect(sync.cursor).toBe(6);
			expect(cb.onReset).not.toHaveBeenCalled();
		});

		it("keeps its own cursor when a replay could not be completed", () => {
			const { sync } = joined();
			sync.applyJoinAck({ epoch: 7, head: 9, mode: "replay" });
			expect(sync.cursor).toBe(3);
		});

		it("asks the app to refetch on a reset and continues from the head", () => {
			const { sync, cb } = joined();
			expect(sync.applyJoinAck({ epoch: 8, head: 40, mode: "reset" })).toBe(
				false,
			);
			expect(cb.onReset).toHaveBeenCalledTimes(1);
			expect(sync.cursor).toBe(40);
			expect(sync.epoch).toBe(8);
		});
	});

	it("handles a reset the server sends while connected", () => {
		const { sync, cb } = setup();
		sync.applyJoinAck({ epoch: 7, head: 3, mode: "live" });
		sync.handleReset({ epoch: 9, head: 1 });
		expect(cb.onReset).toHaveBeenCalledTimes(1);
		expect([sync.epoch, sync.cursor]).toEqual([9, 1]);
	});

	describe("heartbeat", () => {
		it("re-joins after being behind the head on two beats in a row", () => {
			const { sync, cb } = setup();
			sync.applyJoinAck({ epoch: 7, head: 3, mode: "live" });
			sync.handleHeartbeat({ projectId: "p", epoch: 7, head: 5 });
			expect(cb.requestResync).not.toHaveBeenCalled();
			sync.handleHeartbeat({ projectId: "p", epoch: 7, head: 5 });
			expect(cb.requestResync).toHaveBeenCalledTimes(1);
		});

		it("is patient with a batch that is merely in flight", () => {
			const { sync, cb } = setup();
			sync.applyJoinAck({ epoch: 7, head: 3, mode: "live" });
			sync.handleHeartbeat({ projectId: "p", epoch: 7, head: 4 });
			sync.handleBatch(batch(3, 4));
			sync.handleHeartbeat({ projectId: "p", epoch: 7, head: 4 });
			expect(cb.requestResync).not.toHaveBeenCalled();
		});

		it("re-joins at once when the epoch differs", () => {
			const { sync, cb } = setup();
			sync.applyJoinAck({ epoch: 7, head: 3, mode: "live" });
			sync.handleHeartbeat({ projectId: "p", epoch: 8, head: 1 });
			expect(cb.requestResync).toHaveBeenCalledTimes(1);
		});
	});
});
