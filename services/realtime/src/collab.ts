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
// Seeding: a fresh room has no content. The first joiner is told `seed: true`
// and is the only one that writes the stored blocks into the Y.Doc, which
// avoids two clients both inserting the initial content.

import {
	Awareness,
	applyAwarenessUpdate,
	encodeAwarenessUpdate,
	removeAwarenessStates,
} from "y-protocols/awareness";
import * as Y from "yjs";
import type { AccessDecider, Viewer } from "./access.ts";

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

export function roomName(ref: CollabRoomRef): string {
	return `collab:${ref.projectId}:${ref.kind}:${ref.id}`;
}

function resourceOf(ref: CollabRoomRef): string {
	return `project/${ref.projectId}/${ACTIONS[ref.kind].segment}/${ref.id}`;
}

function asBytes(v: unknown): Uint8Array | undefined {
	return v instanceof Uint8Array && v.byteLength <= MAX_UPDATE_BYTES
		? v
		: undefined;
}

interface Member {
	viewer: Viewer;
	// Awareness client ids this socket announced, removed again on leave.
	awarenessIds: Set<number>;
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
}

export class CollabHub {
	private readonly rooms = new Map<string, Room>();
	// Per socket+room chain, so updates are applied in the order they arrived
	// even though each one waits for an access answer.
	private readonly chains = new Map<string, Promise<void>>();

	constructor(
		private readonly decider: AccessDecider,
		private readonly transport: CollabTransport,
		private readonly onError: (err: unknown, ctx: string) => void = () => {},
		private readonly seedDeadlineMs: number = SEED_DEADLINE_MS,
	) {}

	get roomCount(): number {
		return this.rooms.size;
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
		if (!(await this.canWrite(viewer, ref))) {
			return { ok: false, reason: "forbidden" };
		}
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
		this.enqueue(socketId, name, async () => {
			const room = this.rooms.get(name);
			const member = room?.members.get(socketId);
			if (!room || !member) return;
			if (!(await this.canWrite(member.viewer, ref))) {
				this.evict(room, socketId);
				return;
			}
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
		const onUpdate = (
			change: { added: number[]; updated: number[]; removed: number[] },
			origin: unknown,
		) => {
			if (origin !== socketId) return;
			for (const id of [...change.added, ...change.updated]) {
				member.awarenessIds.add(id);
			}
		};
		room.awareness.on("update", onUpdate);
		try {
			applyAwarenessUpdate(room.awareness, update, socketId);
		} catch (err) {
			this.onError(err, "apply awareness");
			return;
		} finally {
			room.awareness.off("update", onUpdate);
		}
		this.transport.broadcast(name, socketId, "collab:awareness", {
			room: ref,
			update,
		});
	}

	leave(socketId: string, rawRoom: unknown): void {
		const ref = parseRoomRef(rawRoom);
		if (!ref) return;
		const room = this.rooms.get(roomName(ref));
		if (room) this.evict(room, socketId, false);
	}

	// leaveAll removes a socket from every room (disconnect).
	leaveAll(socketId: string): void {
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
						if (!ok) this.evict(room, socketId);
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

	private async canWrite(viewer: Viewer, ref: CollabRoomRef): Promise<boolean> {
		try {
			return await this.decider.can(viewer, ref.projectId, {
				action: ACTIONS[ref.kind].write,
				resource: resourceOf(ref),
			});
		} catch (err) {
			// Fail closed.
			this.onError(err, "access check");
			return false;
		}
	}

	private enqueue(
		socketId: string,
		name: string,
		job: () => Promise<void>,
	): void {
		const key = `${socketId}\u0000${name}`;
		const prev = this.chains.get(key) ?? Promise.resolve();
		const next = prev.then(job).catch((err) => this.onError(err, "update"));
		this.chains.set(key, next);
		void next.then(() => {
			if (this.chains.get(key) === next) this.chains.delete(key);
		});
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
		if (room.seedTimer) clearTimeout(room.seedTimer);
		room.seedTimer = undefined;
	}

	private grantSeed(room: Room, socketId: string, notify: boolean): void {
		room.seeder = socketId;
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
	// or frees it when nobody else is in the room.
	private reassignSeed(room: Room, from: string): void {
		const next = [...room.members.keys()].find((id) => id !== from);
		if (next) {
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
// (Y.XmlFragment.toString() lower-cases tag names.)
const PLACEHOLDER_TAGS = new Set(["blockgroup", "blockcontainer", "paragraph"]);

export function isEmpty(doc: Y.Doc): boolean {
	const fragment = doc.getXmlFragment(COLLAB_FRAGMENT);
	if (fragment.length === 0) return true;
	const xml = fragment.toString();
	for (const m of xml.matchAll(/<([A-Za-z][\w-]*)/g)) {
		if (!PLACEHOLDER_TAGS.has((m[1] ?? "").toLowerCase())) return false;
	}
	return xml.replace(/<[^>]*>/g, "").trim() === "";
}
