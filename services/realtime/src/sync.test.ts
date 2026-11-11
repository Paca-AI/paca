import { describe, expect, it } from "bun:test";
import type { Logger } from "pino";
import type { AccessDecider } from "./access.ts";
import type { ChangeEntry, ChangeHead, ChangeLogReader } from "./change-log.ts";
import { SyncHub } from "./sync.ts";

const P = "p1";
interface Batch {
	projectId: string;
	epoch: number;
	from: number;
	to: number;
	changes: Array<{
		seq: number;
		type?: string;
		skip?: boolean;
		payload?: unknown;
	}>;
}
const noopLogger = {
	warn() {},
	info() {},
	debug() {},
	error() {},
} as unknown as Logger;

function entry(
	seq: number,
	action = "tasks:read",
	resource = `project/${P}/task/t${seq}`,
): ChangeEntry {
	return {
		seq,
		epoch: 7,
		id: `id${seq}`,
		type: "task.updated",
		op: "update",
		projectId: P,
		ts: "t",
		payload: { n: seq },
		authz: { action, resource },
	};
}

class FakeLog implements ChangeLogReader {
	entries: ChangeEntry[] = [];
	epoch = 7;
	async head(): Promise<ChangeHead | null> {
		const last = this.entries[this.entries.length - 1];
		return last ? { seq: last.seq, epoch: this.epoch } : null;
	}
	async oldest() {
		return this.entries[0]?.seq ?? null;
	}
	async after(_p: string, seq: number, limit: number) {
		return this.entries.filter((e) => e.seq > seq).slice(0, limit);
	}
}

function setup(
	opts: { rooms?: string[]; allow?: (r: string) => boolean } = {},
) {
	const log = new FakeLog();
	const sent: { id: string; event: string; payload: Batch }[] = [];
	let failChecks = false;
	const decider = {
		can: async (_v: unknown, _p: string, q: { resource: string }) => {
			if (failChecks) throw new Error("api down");
			return (opts.allow ?? (() => true))(q.resource);
		},
	} as unknown as AccessDecider;
	const rooms = new Set(opts.rooms ?? [`project:${P}:tasks`]);
	const hub = new SyncHub({
		reader: log,
		decider,
		logger: noopLogger,
		socket: (id) =>
			id === "s1"
				? {
						viewer: { userId: "u1", rawToken: "tok" },
						inRoom: (r) => rooms.has(r),
						inProject: (id) =>
							[...rooms].some((r) => r.startsWith(`project:${id}:`)),
						disconnect() {},
					}
				: undefined,
		emit: (id, event, payload) =>
			sent.push({ id, event, payload: payload as Batch }),
	});
	return {
		log,
		hub,
		sent,
		setFail: (v: boolean) => {
			failChecks = v;
		},
		batches: () =>
			sent.filter((s) => s.event === "sync:changes").map((s) => s.payload),
		resets: () => sent.filter((s) => s.event === "sync:reset"),
	};
}

// Lets queued hub jobs (a KeyedQueue of promises) settle.
const settle = () => new Promise((r) => setTimeout(r, 5));

describe("SyncHub.join", () => {
	it("starts live at the head when the client has no cursor", async () => {
		const t = setup();
		t.log.entries = [entry(1), entry(2)];
		const res = await t.hub.join("s1", P, {});
		expect(res).toEqual({ epoch: 7, head: 2, mode: "live" });
		expect(t.batches()).toEqual([]);
	});

	it("replays what the client missed as one gapless batch", async () => {
		const t = setup();
		t.log.entries = [1, 2, 3, 4, 5].map((n) => entry(n));
		const res = await t.hub.join("s1", P, { cursor: 2, epoch: 7 });
		expect(res.mode).toBe("replay");
		const batch = t.batches()[0] as Batch;
		expect(batch.from).toBe(2);
		expect(batch.to).toBe(5);
		expect(batch.changes.map((c: { seq: number }) => c.seq)).toEqual([3, 4, 5]);
		expect(batch.changes[0]?.payload).toEqual({ n: 3 });
	});

	it("asks for a reset when the epoch changed, the cursor is ahead, or entries were trimmed", async () => {
		const t = setup();
		t.log.entries = [5, 6, 7].map((n) => entry(n));
		expect((await t.hub.join("s1", P, { cursor: 6, epoch: 1 })).mode).toBe(
			"reset",
		);
		expect((await t.hub.join("s1", P, { cursor: 99, epoch: 7 })).mode).toBe(
			"reset",
		);
		// oldest kept is 5, so 3 is not replayable (4 is gone).
		expect((await t.hub.join("s1", P, { cursor: 3, epoch: 7 })).mode).toBe(
			"reset",
		);
		expect(t.batches()).toEqual([]);
	});

	it("is live when the cursor equals the head", async () => {
		const t = setup();
		t.log.entries = [entry(1)];
		expect((await t.hub.join("s1", P, { cursor: 1, epoch: 7 })).mode).toBe(
			"live",
		);
	});
});

describe("SyncHub pump", () => {
	it("delivers a new change after a notify, continuing from the socket's cursor", async () => {
		const t = setup();
		t.log.entries = [entry(1)];
		await t.hub.join("s1", P, {});
		t.log.entries.push(entry(2), entry(3));
		t.hub.notify(P);
		await settle();
		const batch = t.batches()[0] as Batch;
		expect(batch).toMatchObject({ projectId: P, epoch: 7, from: 1, to: 3 });
		t.log.entries.push(entry(4));
		t.hub.notify(P);
		await settle();
		expect(t.batches()[1]).toMatchObject({ from: 3, to: 4 });
	});

	it("passes the entity snapshot on to the socket", async () => {
		const t = setup();
		await t.hub.join("s1", P, {});
		t.log.entries = [{ ...entry(1), entity: { id: "t1", title: "New" } }];
		t.hub.notify(P);
		await settle();
		expect(t.batches()[0]?.changes[0]).toMatchObject({
			entity: { id: "t1", title: "New" },
		});
	});

	it("sends an account that holds nothing in the project nothing at all, not even skips", async () => {
		const t = setup({ rooms: [] });
		await t.hub.join("s1", P, {});
		t.log.entries = [entry(1), entry(2)];
		t.hub.notify(P);
		await settle();
		expect(t.batches()).toEqual([]);
		// Its cursor moved on, so rights gained later do not replay the gap.
		t.log.entries.push(entry(3));
		t.hub.notify(P);
		await settle();
		expect(t.batches()).toEqual([]);
	});

	it("passes the client op id on, so the requesting client can skip its own echo", async () => {
		const t = setup();
		await t.hub.join("s1", P, {});
		t.log.entries = [{ ...entry(1), clientOpId: "op-9" }];
		t.hub.notify(P);
		await settle();
		expect(t.batches()[0]?.changes[0]).toMatchObject({ client_op_id: "op-9" });
	});

	it("sends a skip, not the change, for what the account may not see", async () => {
		const t = setup({ allow: (r) => !r.endsWith("/t2") });
		await t.hub.join("s1", P, {});
		t.log.entries = [entry(1), entry(2), entry(3)];
		t.hub.notify(P);
		await settle();
		const changes = t.batches()[0]?.changes ?? [];
		expect(changes[0]?.type).toBe("task.updated");
		expect(changes[1]).toEqual({ seq: 2, skip: true });
		expect(changes[2]?.seq).toBe(3);
	});

	it("skips changes whose room the socket is not in", async () => {
		const t = setup({ rooms: [`project:${P}:docs`] });
		await t.hub.join("s1", P, {});
		t.log.entries = [entry(1)];
		t.hub.notify(P);
		await settle();
		expect(t.batches()[0]?.changes).toEqual([{ seq: 1, skip: true }]);
	});

	it("never advances past a change it could not check, and retries it", async () => {
		const t = setup();
		await t.hub.join("s1", P, {});
		t.log.entries = [entry(1), entry(2)];
		t.setFail(true);
		t.hub.notify(P);
		await settle();
		expect(t.batches()).toEqual([]);
		t.setFail(false);
		t.hub.notify(P);
		await settle();
		expect(t.batches()[0]).toMatchObject({ from: 0, to: 2 });
	});

	it("fills a hole left by an undecodable entry with a skip", async () => {
		const t = setup();
		await t.hub.join("s1", P, {});
		t.log.entries = [entry(1), entry(3)];
		// head() reads the last entry's seq; the unreadable seq 2 is absent.
		t.hub.notify(P);
		await settle();
		const changes = t.batches()[0]?.changes ?? [];
		expect(changes.map((c: { seq: number }) => c.seq)).toEqual([1, 2, 3]);
		expect(changes[1]).toEqual({ seq: 2, skip: true });
	});

	it("resets a socket whose log was recreated", async () => {
		const t = setup();
		t.log.entries = [entry(1), entry(2)];
		await t.hub.join("s1", P, {});
		t.log.epoch = 8;
		t.log.entries = [entry(1)];
		t.hub.notify(P);
		await settle();
		expect(t.resets()).toHaveLength(1);
		expect(t.resets()[0]?.payload).toMatchObject({ epoch: 8, head: 1 });
	});

	it("adopts the epoch of a log that did not exist at join", async () => {
		const t = setup();
		await t.hub.join("s1", P, {});
		t.log.entries = [entry(1)];
		t.hub.notify(P);
		await settle();
		expect(t.resets()).toEqual([]);
		expect(t.batches()[0]).toMatchObject({ from: 0, to: 1 });
	});

	it("stops delivering after the socket leaves", async () => {
		const t = setup();
		await t.hub.join("s1", P, {});
		t.hub.leave("s1", P);
		t.log.entries = [entry(1)];
		t.hub.notify(P);
		await settle();
		expect(t.batches()).toEqual([]);
	});
});
