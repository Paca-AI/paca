// Client side of the resumable sync protocol (see
// services/realtime/src/sync.ts for the server and the rules).
//
// One ProjectSync tracks one project: the epoch of the server's change log and
// a cursor, the seq of the last change applied. The server sends changes as
// batches that say which seq they follow:
//
//   { projectId, epoch, from, to, changes: [{seq, type, payload, ...} | {seq, skip}] }
//
//  - from === cursor: apply, move the cursor to `to`.
//  - to <= cursor:    already applied (a replay overlapping live delivery): drop.
//  - from > cursor:   a batch was missed: ask to re-join. The join carries the
//                     cursor, so the server replays exactly what is missing.
//  - epoch differs:   the log was recreated; re-join, the ack says "reset".
//
// A `reset` (log recreated, cursor too old, entries trimmed) means individual
// changes cannot be replayed: onReset lets the app refetch what it shows.
//
// This class is pure bookkeeping, with no socket, so its rules are testable on
// their own; socket-client.ts feeds it.

import type { RealtimeEvent } from "./socket-client";

export interface SyncChange {
	seq: number;
	skip?: true;
	type?: string;
	op?: string;
	payload?: Record<string, unknown>;
	/** The entity after the change, when the server attached one. */
	entity?: Record<string, unknown>;
	entity_type?: string;
	entity_id?: string;
	task_id?: string;
	/** The X-Client-Op-Id of the mutation that caused the change, if any. */
	client_op_id?: string;
}

export interface SyncBatch {
	projectId: string;
	epoch: number;
	from: number;
	to: number;
	changes: SyncChange[];
}

export interface SyncHeartbeat {
	projectId: string;
	epoch: number;
	head: number;
}

export interface SyncJoinAck {
	epoch: number;
	head: number;
	mode: "live" | "replay" | "reset";
}

export interface SyncCallbacks {
	/** One change, in order, to apply (the same shape as a legacy event). */
	onChange(event: RealtimeEvent): void;
	/** Changes could not be replayed: refetch what is on screen. */
	onReset(): void;
	/** Re-join with the current cursor to be replayed what is missing. */
	requestResync(): void;
}

// Heartbeats come every ~15 s; a client that is behind the log head on two in
// a row has lost a batch the gap rule could not see (it was the last one).
const BEHIND_BEATS_BEFORE_RESYNC = 2;

export class ProjectSync {
	cursor: number | null = null;
	epoch: number | null = null;
	private behindBeats = 0;
	private readonly cb: SyncCallbacks;

	constructor(cb: SyncCallbacks) {
		this.cb = cb;
	}

	/**
	 * Starts the feed from a head the client's data was read at, for a project
	 * it has not joined yet (see change-heads.ts). No effect once it has a cursor.
	 */
	seed(head: { seq: number; epoch: number }): void {
		if (this.cursor !== null) return;
		this.cursor = head.seq;
		this.epoch = head.epoch;
	}

	/** The join the client sends: where it is. */
	joinRequest(): { cursor: number | null; epoch: number | null } {
		return { cursor: this.cursor, epoch: this.epoch };
	}

	/**
	 * Applies the server's answer to a join. Returns whether the project's data
	 * is known to be current ("resumed"): the client already had a cursor and
	 * the server could continue from it, so nothing needs refetching.
	 */
	applyJoinAck(ack: SyncJoinAck): boolean {
		const hadCursor = this.cursor !== null;
		this.behindBeats = 0;
		if (ack.mode === "reset") {
			this.epoch = ack.epoch;
			this.cursor = ack.head;
			if (hadCursor) this.cb.onReset();
			return false;
		}
		this.epoch = ack.epoch;
		// A replay's batches arrive before the ack and have moved the cursor
		// already; one the server could not complete leaves it for the next
		// batch to pick up. Only a client with no cursor starts at the head.
		if (this.cursor === null || ack.mode === "live") this.cursor = ack.head;
		return hadCursor;
	}

	handleBatch(batch: SyncBatch): void {
		if (this.epoch !== null && batch.epoch !== this.epoch) {
			this.cb.requestResync();
			return;
		}
		if (this.epoch === null) this.epoch = batch.epoch;
		// Joined without a cursor and a batch beat the ack: it starts the feed.
		if (this.cursor === null) this.cursor = batch.from;
		if (batch.to <= this.cursor) return;
		if (batch.from > this.cursor) {
			this.cb.requestResync();
			return;
		}
		for (const change of batch.changes) {
			if (change.seq <= this.cursor || change.skip) continue;
			if (!change.type || !change.payload) continue;
			this.cb.onChange({
				type: change.type,
				payload: change.payload,
				change: {
					op: change.op,
					entity: change.entity,
					entityType: change.entity_type,
					entityId: change.entity_id,
					taskId: change.task_id,
					clientOpId: change.client_op_id,
				},
			});
		}
		this.cursor = batch.to;
		this.behindBeats = 0;
	}

	handleReset(msg: { epoch: number; head: number }): void {
		this.epoch = msg.epoch;
		this.cursor = msg.head;
		this.behindBeats = 0;
		this.cb.onReset();
	}

	handleHeartbeat(beat: SyncHeartbeat): void {
		if (this.cursor === null || this.epoch === null) return;
		if (beat.epoch !== this.epoch) {
			this.cb.requestResync();
			return;
		}
		if (beat.head > this.cursor) {
			this.behindBeats++;
			if (this.behindBeats >= BEHIND_BEATS_BEFORE_RESYNC) {
				this.behindBeats = 0;
				this.cb.requestResync();
			}
		} else {
			this.behindBeats = 0;
		}
	}
}

/** Whether this client asks the server for the sync protocol. Off by default. */
export function isSyncEnabled(): boolean {
	try {
		if (window.localStorage.getItem("paca.realtime.sync") === "1") return true;
		if (window.localStorage.getItem("paca.realtime.sync") === "0") return false;
	} catch {
		// Storage can be blocked; fall through to the build-time setting.
	}
	return import.meta.env.VITE_REALTIME_SYNC === "true";
}
