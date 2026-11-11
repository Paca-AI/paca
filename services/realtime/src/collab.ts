// Real-time collaborative editing (Yjs) for BlockNote editors.
//
// One in-memory Y.Doc per edited resource (a project doc or a task
// description). Clients speak a tiny protocol over the existing Socket.IO
// connection instead of a second WebSocket server, so authentication and IAM
// reuse everything this service already has:
//
//   collab:join   { projectId, kind, id, stateVector }  (ack)
//       -> { ok: true, seed, update, stateVector, awareness }
//          or { ok: false, reason }
//   collab:update { room, update }     client -> server, then relayed to peers
//   collab:awareness { room, update }  cursors / presence, relayed to peers
//   collab:leave  { room }
//   collab:revoked { room }            server -> client: access was lost
//
// Authorization: joining and every update require `docs:write` / `tasks:write`
// on the exact resource, asked of the API's IAM engine through the shared
// AccessDecider (cached for a few seconds, flushed when IAM changes). Only
// editors join, so everyone in a room may read and write it.
//
// Persistence: none here. The room is a live relay; the REST API stays the
// source of truth. A client saves the block JSON through the normal REST route
// (debounced / on blur) exactly as before, so MCP, search, exports and every
// other consumer of the stored content keep working. When the last editor
// leaves, the room is dropped after a short grace period and the next editor
// re-seeds it from the stored content.
//
// Writes by others: an agent / MCP / script saving through REST while editors
// are connected is not in the room, and the editors' next save would overwrite
// it. The API announces such writes (doc.updated / task.updated carry the
// field's old and new content) and applyExternal merges them into the live
// Y.Doc block by block (see collab-blocks.ts), broadcasting the result to every
// editor. A save by a browser editor of the room is its own content coming
// back and is ignored: the browser names its realtime socket on the request
// (X-Collab-Socket) and the API passes it on as the envelope's collab_socket.
// The account is not enough: an agent writing with the editor's own API key
// is someone else's write and is merged.
//
// Seeding: a fresh room has no content. The first joiner is told `seed: true`
// and is the only one that writes the stored blocks into the Y.Doc, which
// avoids two clients both inserting the initial content.

import * as decoding from "lib0/decoding";
import {
	Awareness,
	applyAwarenessUpdate,
	encodeAwarenessUpdate,
	removeAwarenessStates,
} from "y-protocols/awareness";
import * as Y from "yjs";
import type { AccessDecider, Viewer } from "./access.ts";
import { type MergeResult, mergeExternalChange } from "./collab-blocks.ts";

export type CollabKind = "doc" | "task";

export interface CollabRoomRef {
	projectId: string;
	kind: CollabKind;
	id: string;
}

export interface CollabJoinRequest extends CollabRoomRef {
	stateVector?: Uint8Array;
}

export type CollabJoinResult =
	| {
			ok: true;
			room: string;
			seed: boolean;
			// What the client lacks, relative to the state vector it sent.
			update: Uint8Array;
			// The room's state vector, so the client can send back what the room lacks.
			stateVector: Uint8Array;
			awareness: Uint8Array | null;
	  }
	| { ok: false; reason: "invalid" | "forbidden" | "unavailable" | "full" };

// Why the hub dropped a client's update. The client reacts by re-syncing.
export type CollabErrorReason =
	| "unavailable" // the access check could not be answered; try again
	| "rate" // too many messages
	| "too_large" // the room reached its size budget
	| "backlog"; // too many updates still waiting on their access check

// The fragment name BlockNote's collaboration option is pointed at.
export const COLLAB_FRAGMENT = "document-store";

const ACTIONS: Record<CollabKind, { write: string; segment: string }> = {
	doc: { write: "docs:write", segment: "doc" },
	task: { write: "tasks:write", segment: "task" },
};

const UUID_RE =
	/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

export const MAX_UPDATE_BYTES = 5 * 1024 * 1024;
export const MAX_ROOMS = 2_000;
// Total update bytes one room accepts over its lifetime. Y.Doc memory cannot be
// bounded exactly, but the sum of applied updates is an upper-bound proxy that
// stops an editor growing a room without limit.
export const MAX_ROOM_BYTES = 64 * 1024 * 1024;
// Updates from one socket+room that may wait on an access answer at once.
export const MAX_PENDING_UPDATES = 100;
// Per-socket message budget (updates + awareness): a burst, then a steady rate.
export const RATE_BURST = 200;
export const RATE_PER_SEC = 100;
const DESTROY_GRACE_MS = 30_000;
// One account may hold this many rooms open at once, so a single user with a
// project-wide write grant cannot use up the global cap for everyone else.
export const MAX_ROOMS_PER_USER = 10;
// A seed grant that has not produced content by then moves to another member.
const SEED_DEADLINE_MS = 10_000;

export function parseRoomRef(raw: unknown): CollabRoomRef | undefined {
	if (!raw || typeof raw !== "object") return undefined;
	const { projectId, kind, id } = raw as Record<string, unknown>;
	if (kind !== "doc" && kind !== "task") return undefined;
	if (typeof projectId !== "string" || !UUID_RE.test(projectId))
		return undefined;
	if (typeof id !== "string" || !UUID_RE.test(id)) return undefined;
	return { projectId, kind, id };
}

export interface ContentChange {
	ref: CollabRoomRef;
	actorId?: string;
	// The socket of the browser editor that saved this content, when the write
	// came from one (X-Collab-Socket). See applyExternal.
	collabSocket?: string;
	old: unknown[];
	new: unknown[];
}

// What the realtime envelope says about a content write beyond its payload.
export interface ContentChangeMeta {
	collabSocket?: string;
}

const CONTENT_EVENTS: Record<
	string,
	{ kind: CollabKind; idKey: string; field: string }
> = {
	"doc.updated": { kind: "doc", idKey: "document_id", field: "content" },
	"task.updated": { kind: "task", idKey: "task_id", field: "description" },
};

// parseContentChange reads the old and new content out of a doc.updated /
// task.updated payload: `content` is a JSON string `{"changes":[{field, old,
// new}]}`. Undefined when the event is about something else.
export function parseContentChange(
	type: string,
	payload: Record<string, unknown>,
	meta: ContentChangeMeta = {},
): ContentChange | undefined {
	const spec = CONTENT_EVENTS[type];
	if (!spec) return undefined;
	const ref = parseRoomRef({
		projectId: payload.project_id,
		kind: spec.kind,
		id: payload[spec.idKey],
	});
	if (!ref || typeof payload.content !== "string") return undefined;
	let changes: unknown;
	try {
		changes = (JSON.parse(payload.content) as { changes?: unknown }).changes;
	} catch {
		return undefined;
	}
	if (!Array.isArray(changes)) return undefined;
	const change = changes.find(
		(c) =>
			c &&
			typeof c === "object" &&
			(c as { field?: unknown }).field === spec.field,
	) as { old?: unknown; new?: unknown } | undefined;
	if (!change) return undefined;
	const asBlocks = (v: unknown): unknown[] | undefined =>
		v === null || v === undefined ? [] : Array.isArray(v) ? v : undefined;
	const oldBlocks = asBlocks(change.old);
	const newBlocks = asBlocks(change.new);
	if (!oldBlocks || !newBlocks) return undefined;
	return {
		ref,
		actorId:
			typeof payload.actor_id === "string" ? payload.actor_id : undefined,
		collabSocket: meta.collabSocket,
		old: oldBlocks,
		new: newBlocks,
	};
}

export function roomName(ref: CollabRoomRef): string {
	return `collab:${ref.projectId}:${ref.kind}:${ref.id}`;
}

function resourceOf(ref: CollabRoomRef): string {
	return `project/${ref.projectId}/${ACTIONS[ref.kind].segment}/${ref.id}`;
}

export function asBytes(v: unknown): Uint8Array | undefined {
	return v instanceof Uint8Array && v.byteLength <= MAX_UPDATE_BYTES
		? v
		: undefined;
}

interface Member {
	viewer: Viewer;
	// Awareness client ids this socket announced, removed again on leave.
	awarenessIds: Set<number>;
}

interface Bucket {
	tokens: number;
	at: number;
}

interface Room {
	ref: CollabRoomRef;
	name: string;
	doc: Y.Doc;
	awareness: Awareness;
	members: Map<string, Member>;
	seeder: string | null;
	seeded: boolean;
	seedTimer?: ReturnType<typeof setTimeout>;
	// Members already given the seed grant while the room stayed unseeded, so
	// idle members are not cycled through forever.
	seedTried: Set<string>;
	// Awareness client id -> the socket that announced it, so one member cannot
	// overwrite or remove another member's cursor.
	awarenessOwners: Map<number, string>;
	bytes: number;
	destroyTimer?: ReturnType<typeof setTimeout>;
}

export interface CollabTransport {
	// Send to every member of the room except `exceptSocketId`.
	broadcast(
		room: string,
		exceptSocketId: string | undefined,
		event: string,
		payload: unknown,
	): void;
	// Tell one socket it was removed from the room.
	revoke(socketId: string, room: string): void;
	// Tell one socket it must now write the stored content into the room.
	grantSeed(socketId: string, room: string): void;
	// Tell one socket its seed grant moved to someone else: stop writing.
	cancelSeed(socketId: string, room: string): void;
	// Tell one socket an update of its was dropped; it should re-sync.
	error(socketId: string, room: string, reason: CollabErrorReason): void;
}

export class CollabHub {
	private readonly rooms = new Map<string, Room>();
	// Per socket+room chain, so updates are applied in the order they arrived
	// even though each one waits for an access answer.
	private readonly chains = new Map<string, Promise<void>>();
	private readonly pending = new Map<string, number>();
	private readonly buckets = new Map<string, Bucket>();
	// Per room: external writes waiting to be merged, in arrival order.
	private readonly externalChains = new Map<string, Promise<void>>();

	constructor(
		private readonly decider: AccessDecider,
		private readonly transport: CollabTransport,
		private readonly onError: (err: unknown, ctx: string) => void = () => {},
		private readonly seedDeadlineMs: number = SEED_DEADLINE_MS,
		// Expected outcomes worth knowing about that are not failures: an
		// external write left partly or wholly unmerged.
		private readonly onNotice: (info: object, msg: string) => void = () => {},
	) {}

	get roomCount(): number {
		return this.rooms.size;
	}

	// hasRoom: the hub holds this room (it is the room's authority).
	hasRoom(name: string): boolean {
		return this.rooms.has(name);
	}

	// releaseRoom destroys a room and its state without telling its members
	// (the caller already has): used when the hub is no longer the room's
	// authority.
	releaseRoom(name: string): void {
		const room = this.rooms.get(name);
		if (!room) return;
		this.destroy(room);
		this.rooms.delete(name);
	}

	// roomNames lists the rooms the hub holds, for a cluster to keep their
	// leases alive.
	roomNames(): string[] {
		return [...this.rooms.keys()];
	}

	async join(
		socketId: string,
		viewer: Viewer,
		req: CollabJoinRequest,
		hooks: {
			// Called synchronously, before the state snapshot is taken, so the
			// socket is already in the broadcast room for every later update
			// and none can fall between the snapshot and the room join.
			enter?: (room: string) => void;
			// False once the socket is gone: the access check below is async,
			// and a member added for a dead socket would never be removed.
			isAlive?: () => boolean;
		} = {},
	): Promise<CollabJoinResult> {
		const ref = parseRoomRef(req);
		if (!ref) return { ok: false, reason: "invalid" };
		const allowed = await this.canWrite(viewer, ref);
		if (allowed === undefined) return { ok: false, reason: "unavailable" };
		if (!allowed) return { ok: false, reason: "forbidden" };
		if (hooks.isAlive && !hooks.isAlive()) {
			return { ok: false, reason: "unavailable" };
		}
		const name = roomName(ref);
		let room = this.rooms.get(name);
		if (this.roomsOf(viewer.userId, name) >= MAX_ROOMS_PER_USER) {
			return { ok: false, reason: "full" };
		}
		if (!room) {
			if (this.rooms.size >= MAX_ROOMS) return { ok: false, reason: "full" };
			room = this.createRoom(ref, name);
		}
		if (room.destroyTimer) {
			clearTimeout(room.destroyTimer);
			room.destroyTimer = undefined;
		}
		const rejoin = room.members.has(socketId);
		room.members.set(socketId, {
			viewer,
			awarenessIds: room.members.get(socketId)?.awarenessIds ?? new Set(),
		});

		hooks.enter?.(name);
		const clientSv = asBytes(req.stateVector);
		let seed = false;
		if (
			!rejoin &&
			!room.seeded &&
			room.seeder === null &&
			isEmpty(room.doc) &&
			(!clientSv || clientSv.byteLength <= 1)
		) {
			this.grantSeed(room, socketId, false);
			seed = true;
		} else if (room.seeder === socketId && !room.seeded) {
			seed = true;
		}

		let update: Uint8Array;
		try {
			update = Y.encodeStateAsUpdate(room.doc, clientSv);
		} catch {
			update = Y.encodeStateAsUpdate(room.doc);
		}
		const states = [...room.awareness.getStates().keys()];
		return {
			ok: true,
			room: name,
			seed,
			update,
			stateVector: Y.encodeStateVector(room.doc),
			awareness: states.length
				? encodeAwarenessUpdate(room.awareness, states)
				: null,
		};
	}

	// update applies a client's document update and relays it to the peers.
	update(socketId: string, rawRoom: unknown, rawUpdate: unknown): void {
		const ref = parseRoomRef(rawRoom);
		const update = asBytes(rawUpdate);
		if (!ref || !update) return;
		const name = roomName(ref);
		if (!this.takeToken(socketId)) {
			this.transport.error(socketId, name, "rate");
			return;
		}
		const accepted = this.enqueue(socketId, name, async () => {
			const room = this.rooms.get(name);
			const member = room?.members.get(socketId);
			if (!room || !member) return;
			const allowed = await this.canWrite(member.viewer, ref);
			if (allowed === undefined) {
				// Not a denial: keep the editor in the room, drop only this
				// update, and let the client re-sync it.
				this.transport.error(socketId, name, "unavailable");
				return;
			}
			if (!allowed) {
				this.evict(room, socketId);
				return;
			}
			if (!room.members.has(socketId)) return;
			if (room.bytes + update.byteLength > MAX_ROOM_BYTES) {
				this.transport.error(socketId, name, "too_large");
				return;
			}
			room.bytes += update.byteLength;
			try {
				Y.applyUpdate(room.doc, update, socketId);
			} catch (err) {
				this.onError(err, "apply update");
				return;
			}
			// Only while unseeded: isEmpty serializes the whole document.
			if (!room.seeded && !isEmpty(room.doc)) this.markSeeded(room);
			this.transport.broadcast(name, socketId, "collab:update", {
				room: ref,
				update,
			});
		});
		if (!accepted) this.transport.error(socketId, name, "backlog");
	}

	awarenessUpdate(
		socketId: string,
		rawRoom: unknown,
		rawUpdate: unknown,
	): void {
		const ref = parseRoomRef(rawRoom);
		const update = asBytes(rawUpdate);
		if (!ref || !update) return;
		const name = roomName(ref);
		const room = this.rooms.get(name);
		const member = room?.members.get(socketId);
		if (!room || !member) return;
		if (!this.takeToken(socketId)) return;
		const ids = awarenessClientIds(update);
		if (!ids) return;
		// A socket may only speak for awareness ids it announced itself.
		for (const id of ids) {
			const owner = room.awarenessOwners.get(id);
			if (owner !== undefined && owner !== socketId) return;
		}
		for (const id of ids) {
			room.awarenessOwners.set(id, socketId);
			member.awarenessIds.add(id);
		}
		try {
			applyAwarenessUpdate(room.awareness, update, socketId);
		} catch (err) {
			this.onError(err, "apply awareness");
			return;
		}
		this.transport.broadcast(name, socketId, "collab:awareness", {
			room: ref,
			update,
		});
	}

	// handleContentEvent merges a doc.updated / task.updated announcement into the
	// matching live room, if there is one. Never throws.
	handleContentEvent(
		type: string,
		payload: Record<string, unknown>,
		meta: ContentChangeMeta = {},
	): Promise<void> {
		const change = parseContentChange(type, payload, meta);
		if (!change) return Promise.resolve();
		// One chain per room, so two writes merge in the order they arrived.
		const key = roomName(change.ref);
		const prev = this.externalChains.get(key) ?? Promise.resolve();
		const next = prev
			.then(() =>
				this.applyExternal(
					change.ref,
					change.collabSocket,
					change.old,
					change.new,
				),
			)
			.then(() => {})
			.catch((err) => this.onError(err, "apply external change"));
		this.externalChains.set(key, next);
		void next.then(() => {
			if (this.externalChains.get(key) === next)
				this.externalChains.delete(key);
		});
		return next;
	}

	// applyExternal merges a write that did not come through the room into its
	// Y.Doc and relays the result to all members. A no-op when nobody has the
	// resource open (the next editor seeds from the stored content), when the
	// room is not seeded yet, or when the write is the save of one of the
	// room's editor sockets.
	async applyExternal(
		ref: CollabRoomRef,
		collabSocket: string | undefined,
		oldContent: unknown[],
		newContent: unknown[],
	): Promise<MergeResult | undefined> {
		const name = roomName(ref);
		let room = this.rooms.get(name);
		if (!room?.seeded || room.members.size === 0) return undefined;
		// An editor's own save comes back as this event: its content is already
		// the room's, and merging it again could revive blocks deleted since.
		// It is recognised by the socket that saved, not by the account: an
		// agent writing with the same user's API key is someone else's write.
		if (collabSocket && room.members.has(collabSocket)) return undefined;
		// Editor updates already received but still waiting on their access
		// check have not reached the Y.Doc yet. Merging first would replace a
		// block that such an update is about to edit, and the update would land
		// on a deleted node. Let them apply first so the merge sees them.
		const waiting = [...room.members.keys()]
			.map((id) => this.chains.get(`${id}\u0000${name}`))
			.filter((p): p is Promise<void> => p !== undefined);
		if (waiting.length) {
			await Promise.all(waiting);
			room = this.rooms.get(name);
			if (!room?.seeded || room.members.size === 0) return undefined;
		}
		// The merge's size is only known afterwards, so the budget is checked
		// before it and charged with the real update after it. One write can
		// overshoot by its own size at most.
		if (room.bytes >= MAX_ROOM_BYTES) {
			this.onNotice({ room: name }, "external change not merged (too_large)");
			return { applied: 0, conflicts: 0, skipped: "too_large" };
		}
		const before = Y.encodeStateVector(room.doc);
		const result = mergeExternalChange(
			room.doc,
			COLLAB_FRAGMENT,
			oldContent,
			newContent,
		);
		if (result.applied > 0) {
			const update = Y.encodeStateAsUpdate(room.doc, before);
			room.bytes += update.byteLength;
			this.transport.broadcast(room.name, undefined, "collab:update", {
				room: ref,
				update,
			});
		}
		if (result.conflicts > 0 || result.skipped || result.duplicates) {
			this.onNotice(
				{ room: name, ...result },
				result.skipped
					? `external change not merged (${result.skipped})`
					: result.conflicts > 0
						? "external change kept blocks as edited live"
						: "external change blocks already present in the room",
			);
		}
		return result;
	}

	leave(socketId: string, rawRoom: unknown): void {
		const ref = parseRoomRef(rawRoom);
		if (!ref) return;
		const room = this.rooms.get(roomName(ref));
		if (room) this.evict(room, socketId, false);
	}

	// leaveAll removes a socket from every room (disconnect).
	leaveAll(socketId: string): void {
		this.buckets.delete(socketId);
		for (const room of [...this.rooms.values()]) {
			if (room.members.has(socketId)) this.evict(room, socketId, false);
		}
	}

	// recheck re-asks IAM for every member; run after the API announced an IAM
	// change (the decider cache has been flushed by then).
	async recheck(): Promise<void> {
		const jobs: Promise<void>[] = [];
		for (const room of [...this.rooms.values()]) {
			for (const [socketId, member] of [...room.members]) {
				jobs.push(
					this.canWrite(member.viewer, room.ref).then((ok) => {
						// undefined: the check failed, which proves nothing. The
						// next pass (or the next update) asks again.
						if (ok === false) this.evict(room, socketId);
					}),
				);
			}
		}
		await Promise.all(jobs);
	}

	close(): void {
		for (const room of this.rooms.values()) this.destroy(room);
		this.rooms.clear();
	}

	// ── internals ──────────────────────────────────────────────────────────

	// canWrite answers true / false, or undefined when the access check itself
	// failed (API unreachable). Callers that grant access treat undefined as
	// "no"; callers that would revoke it must not, or one API blip would evict
	// every editor.
	private async canWrite(
		viewer: Viewer,
		ref: CollabRoomRef,
	): Promise<boolean | undefined> {
		try {
			return await this.decider.can(viewer, ref.projectId, {
				action: ACTIONS[ref.kind].write,
				resource: resourceOf(ref),
			});
		} catch (err) {
			this.onError(err, "access check");
			return undefined;
		}
	}

	private takeToken(socketId: string): boolean {
		const now = Date.now();
		const b = this.buckets.get(socketId) ?? { tokens: RATE_BURST, at: now };
		b.tokens = Math.min(
			RATE_BURST,
			b.tokens + ((now - b.at) / 1000) * RATE_PER_SEC,
		);
		b.at = now;
		this.buckets.set(socketId, b);
		if (b.tokens < 1) return false;
		b.tokens -= 1;
		return true;
	}

	// enqueue returns false when the socket already has too many updates
	// waiting on their access check.
	private enqueue(
		socketId: string,
		name: string,
		job: () => Promise<void>,
	): boolean {
		const key = `${socketId}\u0000${name}`;
		const waiting = this.pending.get(key) ?? 0;
		if (waiting >= MAX_PENDING_UPDATES) return false;
		this.pending.set(key, waiting + 1);
		const prev = this.chains.get(key) ?? Promise.resolve();
		const next = prev.then(job).catch((err) => this.onError(err, "update"));
		this.chains.set(key, next);
		void next.then(() => {
			const left = (this.pending.get(key) ?? 1) - 1;
			if (left <= 0) this.pending.delete(key);
			else this.pending.set(key, left);
			if (this.chains.get(key) === next) this.chains.delete(key);
		});
		return true;
	}

	private createRoom(ref: CollabRoomRef, name: string): Room {
		const doc = new Y.Doc();
		const awareness = new Awareness(doc);
		awareness.setLocalState(null);
		const room: Room = {
			ref,
			name,
			doc,
			awareness,
			members: new Map(),
			seeder: null,
			seeded: false,
			seedTried: new Set(),
			awarenessOwners: new Map(),
			bytes: 0,
		};
		this.rooms.set(name, room);
		return room;
	}

	private evict(room: Room, socketId: string, notify = true): void {
		const member = room.members.get(socketId);
		if (!member) return;
		room.members.delete(socketId);
		if (notify) this.transport.revoke(socketId, room.name);
		if (room.seeder === socketId && !room.seeded) {
			// Do not strand the room unseeded: hand the grant to someone else.
			this.reassignSeed(room, socketId);
		}
		const ids = [...member.awarenessIds].filter((id) =>
			room.awareness.getStates().has(id),
		);
		for (const id of member.awarenessIds) {
			if (room.awarenessOwners.get(id) === socketId) {
				room.awarenessOwners.delete(id);
			}
		}
		if (ids.length) {
			removeAwarenessStates(room.awareness, ids, socketId);
			this.transport.broadcast(room.name, socketId, "collab:awareness", {
				room: room.ref,
				update: encodeAwarenessUpdate(room.awareness, ids),
			});
		}
		if (room.members.size === 0) {
			room.destroyTimer = setTimeout(() => {
				if (room.members.size === 0) {
					this.destroy(room);
					this.rooms.delete(room.name);
				}
			}, DESTROY_GRACE_MS);
			room.destroyTimer.unref?.();
		}
	}

	private roomsOf(userId: string, except: string): number {
		let n = 0;
		for (const [name, room] of this.rooms) {
			if (name === except) continue;
			for (const m of room.members.values()) {
				if (m.viewer.userId === userId) {
					n++;
					break;
				}
			}
		}
		return n;
	}

	private markSeeded(room: Room): void {
		room.seeded = true;
		room.seedTried.clear();
		if (room.seedTimer) clearTimeout(room.seedTimer);
		room.seedTimer = undefined;
	}

	private grantSeed(room: Room, socketId: string, notify: boolean): void {
		room.seeder = socketId;
		room.seedTried.add(socketId);
		if (notify) this.transport.grantSeed(socketId, room.name);
		if (room.seedTimer) clearTimeout(room.seedTimer);
		room.seedTimer = setTimeout(() => {
			room.seedTimer = undefined;
			if (room.seeded || room.seeder === null) return;
			this.reassignSeed(room, room.seeder);
		}, this.seedDeadlineMs);
		room.seedTimer.unref?.();
	}

	// reassignSeed moves an unfinished seed grant from `from` to another member,
	// or frees it when nobody untried is left (a later joiner can still take it).
	private reassignSeed(room: Room, from: string): void {
		const next = [...room.members.keys()].find(
			(id) => id !== from && !room.seedTried.has(id),
		);
		if (next) {
			// The previous holder may only be slow: tell it to stop, or both
			// would write the stored content and it would appear twice.
			if (room.members.has(from)) this.transport.cancelSeed(from, room.name);
			this.grantSeed(room, next, true);
			return;
		}
		room.seeder = null;
		if (room.seedTimer) clearTimeout(room.seedTimer);
		room.seedTimer = undefined;
	}

	private destroy(room: Room): void {
		if (room.seedTimer) clearTimeout(room.seedTimer);
		if (room.destroyTimer) clearTimeout(room.destroyTimer);
		room.awareness.destroy();
		room.doc.destroy();
	}
}

// A room is "empty" until it holds real content. BlockNote may write a bare
// placeholder paragraph into the shared doc as soon as an editor mounts; that
// must not count as content, or the stored document would never be seeded and
// every editor would show a blank page.
const PLACEHOLDER_TAGS = new Set(["blockgroup", "blockcontainer", "paragraph"]);

export function isEmpty(doc: Y.Doc): boolean {
	return !hasContent(doc.getXmlFragment(COLLAB_FRAGMENT));
}

type XmlNode = Y.XmlFragment | Y.XmlElement | Y.XmlText | Y.XmlHook;

function hasContent(node: XmlNode): boolean {
	if (node instanceof Y.XmlText) {
		return node
			.toDelta()
			.some(
				(op: { insert?: unknown }) =>
					typeof op.insert !== "string" || op.insert.trim() !== "",
			);
	}
	if (node instanceof Y.XmlElement) {
		if (!PLACEHOLDER_TAGS.has(node.nodeName.toLowerCase())) return true;
	}
	if (node instanceof Y.XmlFragment) {
		return node.toArray().some((child) => hasContent(child as XmlNode));
	}
	return node instanceof Y.XmlHook;
}

// awarenessClientIds reads the client ids out of an awareness update without
// applying it (y-protocols' wire format: count, then id/clock/state per entry).
function awarenessClientIds(update: Uint8Array): number[] | undefined {
	try {
		const d = decoding.createDecoder(update);
		const n = decoding.readVarUint(d);
		if (n > 1_000) return undefined;
		const ids: number[] = [];
		for (let i = 0; i < n; i++) {
			ids.push(decoding.readVarUint(d));
			decoding.readVarUint(d);
			decoding.readVarString(d);
		}
		return ids;
	} catch {
		return undefined;
	}
}
