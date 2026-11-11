// Sync hub: resumable, ordered delivery of a project's change log.
//
// A socket that joins with "sync:join" gets the project's changes as
// "sync:changes" batches, each saying which sequence number it follows:
//
//   { projectId, epoch, from, to, changes: [{seq, type, payload, ...} | {seq, skip}] }
//
// `from` is the seq the client must already have applied; the batch covers
// from+1 .. to with no holes. A change the socket's account may not see (or
// whose room it is not in) arrives as `{seq, skip: true}`: it keeps the
// sequence gapless for that socket, and says nothing but that something
// happened. A client that finds `from` ahead of its cursor re-joins with its
// cursor and is replayed what it missed.
//
// Where changes come from: the API appends them to a per-project stream (see
// change-log.ts) and also publishes them on Pub/Sub with their seq. The
// Pub/Sub message is only a nudge here (`notify`); the stream is the data, read
// from each socket's cursor. A nudge that is lost costs at most one poll
// interval, never a change. Joins, replays and pumps of one project run one
// after another, so a socket never sees batches out of order.
//
// Authorization is the same question the legacy path asks: the entry names an
// action and a resource, and the API's IAM engine, through the AccessDecider,
// says whether this account holds it. If the API cannot be reached the socket's
// cursor stays where it was and the next pump retries: a change is never
// dropped because a check failed.

import type { Logger } from "pino";
import { type AccessDecider, KeyedQueue, type Viewer } from "./access.ts";
import { ApiRequestError } from "./api-client.ts";
import type { ChangeEntry, ChangeLogReader } from "./change-log.ts";
import { namespaceForAction, projectRoomName } from "./permissions.ts";

// A join further behind than this is answered with a reset instead of a replay.
export const REPLAY_LIMIT = 1_000;
const PUMP_BATCH = 200;
export const POLL_INTERVAL_MS = 5_000;
export const HEARTBEAT_INTERVAL_MS = 15_000;

export interface SyncSocket {
	viewer: Viewer;
	inRoom(room: string): boolean;
	// inProject: the socket is in at least one room of the project, i.e. its
	// account may read something there.
	inProject(projectId: string): boolean;
	disconnect(): void;
}

export interface SyncDeps {
	reader: ChangeLogReader;
	decider: AccessDecider;
	logger: Logger;
	socket(socketId: string): SyncSocket | undefined;
	emit(socketId: string, event: string, payload: unknown): void;
}

export type SyncMode = "live" | "replay" | "reset";

export interface SyncJoinResult {
	epoch: number;
	head: number;
	mode: SyncMode;
}

type WireChange =
	| { seq: number; skip: true }
	| {
			seq: number;
			id: string;
			type: string;
			op: string;
			ts: string;
			payload: Record<string, unknown>;
			entity?: Record<string, unknown>;
			entity_type?: string;
			entity_id?: string;
			task_id?: string;
			actor_id?: string;
			origin?: string;
			client_op_id?: string;
	  };

interface Sub {
	socketId: string;
	cursor: number;
	epoch: number;
}

class CheckFailed extends Error {
	constructor(readonly cause: unknown) {
		super("access check failed");
	}
}

function toWire(e: ChangeEntry): WireChange {
	return {
		seq: e.seq,
		id: e.id,
		type: e.type,
		op: e.op,
		ts: e.ts,
		payload: e.payload,
		entity: e.entity,
		entity_type: e.entityType,
		entity_id: e.entityId,
		task_id: e.taskId,
		actor_id: e.actorId,
		origin: e.origin,
		client_op_id: e.clientOpId,
	};
}

export class SyncHub {
	private readonly subs = new Map<string, Map<string, Sub>>();
	private readonly queue = new KeyedQueue();
	private readonly pending = new Set<string>();
	private timers: ReturnType<typeof setInterval>[] = [];

	constructor(private readonly deps: SyncDeps) {}

	start(): void {
		this.timers.push(
			setInterval(() => this.pollAll(), POLL_INTERVAL_MS),
			setInterval(() => void this.heartbeatAll(), HEARTBEAT_INTERVAL_MS),
		);
		for (const t of this.timers) t.unref?.();
	}

	stop(): void {
		for (const t of this.timers) clearInterval(t);
		this.timers = [];
	}

	// join registers the socket for a project and brings it up to date from
	// the cursor it holds. Resolves when any replay has been emitted, so the
	// caller's ack follows it.
	join(
		socketId: string,
		projectId: string,
		req: { cursor?: unknown; epoch?: unknown },
	): Promise<SyncJoinResult> {
		return new Promise((resolve, reject) => {
			this.queue.run(this.key(projectId), async () => {
				try {
					resolve(await this.doJoin(socketId, projectId, req));
				} catch (err) {
					reject(err);
				}
			});
		});
	}

	private async doJoin(
		socketId: string,
		projectId: string,
		req: { cursor?: unknown; epoch?: unknown },
	): Promise<SyncJoinResult> {
		const { reader } = this.deps;
		const head = (await reader.head(projectId)) ?? { seq: 0, epoch: 0 };
		const cursor =
			typeof req.cursor === "number" && Number.isSafeInteger(req.cursor)
				? req.cursor
				: null;
		const sub: Sub = { socketId, cursor: head.seq, epoch: head.epoch };
		const result: SyncJoinResult = {
			epoch: head.epoch,
			head: head.seq,
			mode: "live",
		};
		let replay: ChangeEntry[] = [];

		if (cursor !== null && cursor !== head.seq) {
			const sameLog = req.epoch === head.epoch || head.epoch === 0;
			if (!sameLog || cursor > head.seq || cursor < 0) {
				result.mode = "reset";
			} else if (head.seq - cursor > REPLAY_LIMIT) {
				result.mode = "reset";
			} else {
				const oldest = await reader.oldest(projectId);
				if (oldest === null || oldest > cursor + 1) {
					result.mode = "reset";
				} else {
					replay = await reader.after(projectId, cursor, REPLAY_LIMIT);
					result.mode = "replay";
					sub.cursor = cursor;
				}
			}
		}

		this.register(projectId, sub);
		if (result.mode === "replay") {
			try {
				await this.deliver(projectId, sub, head.epoch, replay, head.seq);
			} catch {
				// Could not check access: leave the cursor where the client
				// said; the next pump retries from it.
			}
		}
		return result;
	}

	leave(socketId: string, projectId: string): void {
		const subs = this.subs.get(projectId);
		if (!subs) return;
		subs.delete(socketId);
		if (subs.size === 0) this.subs.delete(projectId);
	}

	dropSocket(socketId: string): void {
		for (const projectId of [...this.subs.keys()]) {
			this.leave(socketId, projectId);
		}
	}

	// notify nudges the pump of a project: a change was logged.
	notify(projectId: string): void {
		if (!this.subs.has(projectId) || this.pending.has(projectId)) return;
		this.pending.add(projectId);
		this.queue.run(this.key(projectId), async () => {
			this.pending.delete(projectId);
			try {
				await this.pump(projectId);
			} catch (err) {
				this.deps.logger.warn({ projectId, err }, "sync pump failed");
			}
		});
	}

	private key(projectId: string): string {
		return `sync:${projectId}`;
	}

	private register(projectId: string, sub: Sub): void {
		let subs = this.subs.get(projectId);
		if (!subs) {
			subs = new Map();
			this.subs.set(projectId, subs);
		}
		subs.set(sub.socketId, sub);
	}

	private pollAll(): void {
		for (const projectId of this.subs.keys()) this.notify(projectId);
	}

	private async heartbeatAll(): Promise<void> {
		for (const [projectId, subs] of [...this.subs]) {
			try {
				const head = await this.deps.reader.head(projectId);
				if (!head) continue;
				for (const sub of subs.values()) {
					this.deps.emit(sub.socketId, "sync:heartbeat", {
						projectId,
						epoch: head.epoch,
						head: head.seq,
					});
				}
			} catch (err) {
				this.deps.logger.warn({ projectId, err }, "sync heartbeat failed");
			}
		}
	}

	private async pump(projectId: string): Promise<void> {
		const { reader } = this.deps;
		for (;;) {
			const subs = this.subs.get(projectId);
			if (!subs || subs.size === 0) return;
			const head = await reader.head(projectId);
			if (!head) return;

			const behind: Sub[] = [];
			for (const sub of subs.values()) {
				if (sub.epoch !== head.epoch) {
					if (sub.epoch === 0 && sub.cursor === 0) {
						// Joined before the project's first change.
						sub.epoch = head.epoch;
					} else {
						// The log was recreated: the cursor means nothing now.
						sub.epoch = head.epoch;
						sub.cursor = head.seq;
						this.deps.emit(sub.socketId, "sync:reset", {
							projectId,
							epoch: head.epoch,
							head: head.seq,
						});
						continue;
					}
				}
				if (sub.cursor < head.seq) behind.push(sub);
			}
			if (behind.length === 0) return;

			const cursorsBefore = behind.map((s) => s.cursor);
			const from = Math.min(...cursorsBefore);
			const entries = await reader.after(projectId, from, PUMP_BATCH);
			const first = entries[0];
			if (!first) {
				// Everything after the cursor was trimmed away (or unreadable).
				for (const sub of behind) this.reset(projectId, sub, head);
				return;
			}
			await Promise.all(
				behind.map(async (sub) => {
					if (first.seq > sub.cursor + 1) {
						// The entries this socket needs were trimmed.
						this.reset(projectId, sub, head);
						return;
					}
					try {
						await this.deliver(
							projectId,
							sub,
							head.epoch,
							entries.filter((e) => e.seq > sub.cursor),
							head.seq,
						);
					} catch {
						// Cursor unchanged; retried on the next pump.
					}
				}),
			);
			const last = entries[entries.length - 1];
			if (!last || entries.length < PUMP_BATCH || last.seq >= head.seq) return;
			// Nobody moved (every delivery failed, for example an access check
			// that could not be answered): stop here and let the next nudge or
			// poll retry, instead of re-reading the same range without end while
			// holding this project's queue.
			if (behind.every((s, i) => s.cursor === cursorsBefore[i])) return;
		}
	}

	private reset(
		projectId: string,
		sub: Sub,
		head: { seq: number; epoch: number },
	) {
		sub.cursor = head.seq;
		sub.epoch = head.epoch;
		this.deps.emit(sub.socketId, "sync:reset", {
			projectId,
			epoch: head.epoch,
			head: head.seq,
		});
	}

	// deliver emits entries (all with seq > sub.cursor, ascending) as one batch.
	// A seq missing between them (an entry that could not be decoded) becomes a
	// skip so the batch stays gapless. Throws, leaving the cursor alone, when
	// an access check fails.
	private async deliver(
		projectId: string,
		sub: Sub,
		epoch: number,
		entries: ChangeEntry[],
		upTo: number,
	): Promise<void> {
		const target = this.deps.socket(sub.socketId);
		if (!target) {
			this.leave(sub.socketId, projectId);
			return;
		}
		const last = entries[entries.length - 1];
		if (!last) return;
		const end = Math.min(last.seq, upTo);
		// An account that holds nothing in the project (a member who lost every
		// role) gets nothing, not even skips: the sequence numbers alone would
		// tell it how active the project is. Its cursor moves on silently; what
		// it gains later is announced by permissions:changed, which makes the
		// client refetch what it can now read.
		if (!target.inProject(projectId)) {
			sub.cursor = end;
			return;
		}
		const bySeq = new Map(entries.map((e) => [e.seq, e]));

		const checks: Promise<WireChange>[] = [];
		for (let seq = sub.cursor + 1; seq <= end; seq++) {
			const entry = bySeq.get(seq);
			checks.push(
				entry
					? this.authorize(target, projectId, entry)
					: Promise.resolve({ seq, skip: true as const }),
			);
		}
		let changes: WireChange[];
		try {
			changes = await Promise.all(checks);
		} catch (err) {
			const cause = err instanceof CheckFailed ? err.cause : err;
			this.deps.logger.warn(
				{ projectId, userId: target.viewer.userId, err: cause },
				"sync access check failed — will retry",
			);
			if (cause instanceof ApiRequestError && cause.status === 401) {
				target.disconnect();
			}
			throw err;
		}
		// The socket may have left or re-joined while the checks ran.
		if (this.subs.get(projectId)?.get(sub.socketId) !== sub) return;

		this.deps.emit(sub.socketId, "sync:changes", {
			projectId,
			epoch,
			from: sub.cursor,
			to: end,
			changes,
		});
		sub.cursor = end;
	}

	private async authorize(
		target: SyncSocket,
		projectId: string,
		entry: ChangeEntry,
	): Promise<WireChange> {
		const ns = namespaceForAction(entry.authz.action);
		if (!ns || !target.inRoom(projectRoomName(projectId, ns))) {
			return { seq: entry.seq, skip: true };
		}
		try {
			const allowed = await this.deps.decider.can(
				target.viewer,
				projectId,
				entry.authz,
			);
			return allowed ? toWire(entry) : { seq: entry.seq, skip: true };
		} catch (err) {
			throw new CheckFailed(err);
		}
	}
}
