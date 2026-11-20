// Ephemeral presence: who is viewing a thing and who is typing in it.
//
// A client joins a "live room" for what it is looking at (today `task:<id>`;
// channels and docs later). Access is checked once, at join, through the same
// IAM decision the REST routes use; nothing is checked per event. Typing is a
// short lease: the client says "typing" on start and every few seconds while
// it continues, and the lease lapses on its own after TYPING_TTL_MS. Nothing
// here touches the API's database or Valkey's change log, and nothing is
// persisted.
//
// Across replicas each replica holds only its own sockets and publishes a
// small summary per room (who views, who types) on every change and as a
// heartbeat; every replica merges the summaries it hears with its own and
// fans the union out to its sockets. A replica that falls silent ages out.

import type { Logger } from "pino";
import type { AccessDecider, Viewer } from "./access.ts";

export type LiveKind = "task";

export interface LiveRef {
	kind: LiveKind;
	projectId: string;
	id: string;
}

// What a client receives, and what a join acks with.
export interface PresenceSnapshot {
	kind: LiveKind;
	id: string;
	// viewers and typing are account ids (the client resolves names and
	// avatars from the project's member list), sorted for stable output.
	viewers: string[];
	typing: string[];
}

// A replica's view of one room, as published on the bus.
export interface PresenceSummary {
	replica: string;
	room: string;
	viewers: string[];
	typing: string[];
	// hello asks every replica that has the room to publish its own summary,
	// so a socket that just joined sees remote viewers without waiting for the
	// next heartbeat.
	hello?: boolean;
}

export interface PresenceBus {
	publish(summary: PresenceSummary): void;
}

export interface PresenceTransport {
	// emit sends a snapshot to every local socket in the room.
	emit(room: string, snapshot: PresenceSnapshot): void;
}

export const TYPING_TTL_MS = 5_000;
// A replica's summaries are trusted this long without a refresh.
export const REMOTE_TTL_MS = 25_000;
export const HEARTBEAT_MS = 10_000;
// One socket may hold this many live rooms, and may send this many typing
// messages per second (a client throttles to one every few seconds).
export const MAX_LIVE_ROOMS = 20;
const MIN_TYPING_GAP_MS = 500;

const UUID_RE =
	/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

const ACTIONS: Record<LiveKind, { read: string; write: string; seg: string }> =
	{ task: { read: "tasks:read", write: "tasks:write", seg: "task" } };

export function liveRoomName(kind: LiveKind, id: string): string {
	return `live:${kind}:${id}`;
}

export function parseLiveRef(v: unknown): LiveRef | undefined {
	if (!v || typeof v !== "object") return undefined;
	const { kind, projectId, id } = v as Record<string, unknown>;
	if (kind !== "task") return undefined;
	if (typeof projectId !== "string" || !UUID_RE.test(projectId))
		return undefined;
	if (typeof id !== "string" || !UUID_RE.test(id)) return undefined;
	return { kind, projectId, id };
}

interface Member {
	viewer: Viewer;
	canType: boolean;
	lastTypingAt: number;
}

interface Room {
	ref: LiveRef;
	name: string;
	members: Map<string, Member>; // socket id -> member
	// typingUntil is per account: any of its sockets typing keeps it set.
	typingUntil: Map<string, number>;
	remote: Map<string, { at: number; summary: PresenceSummary }>;
	lastKey: string;
}

export type JoinResult =
	| { ok: true; snapshot: PresenceSnapshot; canType: boolean }
	| { ok: false; reason: "forbidden" | "unavailable" | "limit" };

export class PresenceHub {
	private readonly rooms = new Map<string, Room>();
	private readonly byRoom = new Map<string, Set<string>>(); // socket -> room names
	private timer?: ReturnType<typeof setInterval>;
	private lastBeat = 0;

	constructor(
		private readonly opts: {
			decider: AccessDecider;
			transport: PresenceTransport;
			replica: string;
			bus?: PresenceBus;
			logger?: Logger;
			now?: () => number;
		},
	) {}

	private now(): number {
		return (this.opts.now ?? Date.now)();
	}

	start(): void {
		this.timer = setInterval(() => this.tick(), 1_000);
		this.timer.unref?.();
	}

	stop(): void {
		if (this.timer) clearInterval(this.timer);
		this.timer = undefined;
	}

	async join(
		socketId: string,
		viewer: Viewer,
		ref: LiveRef,
		// stillConnected guards the await below: a socket that left while the
		// access check ran must not be added back.
		stillConnected: () => boolean = () => true,
	): Promise<JoinResult> {
		const name = liveRoomName(ref.kind, ref.id);
		const joined = this.byRoom.get(socketId);
		if (!joined?.has(name) && (joined?.size ?? 0) >= MAX_LIVE_ROOMS) {
			return { ok: false, reason: "limit" };
		}
		const a = ACTIONS[ref.kind];
		const resource = `project/${ref.projectId}/${a.seg}/${ref.id}`;
		let canRead: boolean;
		let canType: boolean;
		try {
			[canRead, canType] = await Promise.all([
				this.opts.decider.can(viewer, ref.projectId, {
					action: a.read,
					resource,
				}),
				this.opts.decider.can(viewer, ref.projectId, {
					action: a.write,
					resource,
				}),
			]);
		} catch (err) {
			this.opts.logger?.warn({ err }, "presence access check failed");
			return { ok: false, reason: "unavailable" };
		}
		if (!canRead) return { ok: false, reason: "forbidden" };
		if (!stillConnected()) return { ok: false, reason: "unavailable" };

		let room = this.rooms.get(name);
		let fresh = false;
		if (!room) {
			room = {
				ref,
				name,
				members: new Map(),
				typingUntil: new Map(),
				remote: new Map(),
				lastKey: "",
			};
			this.rooms.set(name, room);
			fresh = true;
		}
		room.members.set(socketId, { viewer, canType, lastTypingAt: 0 });
		const set = this.byRoom.get(socketId) ?? new Set();
		set.add(name);
		this.byRoom.set(socketId, set);
		this.changed(room, fresh);
		return { ok: true, snapshot: this.snapshot(room), canType };
	}

	leave(socketId: string, ref: LiveRef): void {
		const name = liveRoomName(ref.kind, ref.id);
		this.remove(socketId, name);
	}

	leaveAll(socketId: string): void {
		for (const name of [...(this.byRoom.get(socketId) ?? [])]) {
			this.remove(socketId, name);
		}
		this.byRoom.delete(socketId);
	}

	// typing records or clears the lease of the socket's account.
	typing(socketId: string, kind: unknown, id: unknown, on: unknown): void {
		if (kind !== "task" || typeof id !== "string") return;
		const room = this.rooms.get(liveRoomName(kind, id));
		const member = room?.members.get(socketId);
		if (!room || !member || !member.canType) return;
		const now = this.now();
		if (on === true) {
			if (now - member.lastTypingAt < MIN_TYPING_GAP_MS) return;
			member.lastTypingAt = now;
			room.typingUntil.set(member.viewer.userId, now + TYPING_TTL_MS);
		} else {
			room.typingUntil.delete(member.viewer.userId);
		}
		this.changed(room);
	}

	// recheck drops sockets that no longer may read their room (an IAM change).
	// A failed check keeps the socket: one API blip must not evict everyone.
	async recheck(onRevoked: (socketId: string, ref: LiveRef) => void) {
		for (const room of [...this.rooms.values()]) {
			for (const [socketId, m] of [...room.members]) {
				try {
					const ok = await this.opts.decider.can(m.viewer, room.ref.projectId, {
						action: ACTIONS[room.ref.kind].read,
						resource: `project/${room.ref.projectId}/${ACTIONS[room.ref.kind].seg}/${room.ref.id}`,
					});
					if (!ok) {
						this.remove(socketId, room.name);
						onRevoked(socketId, room.ref);
					}
				} catch {
					// keep
				}
			}
		}
	}

	// hear merges a summary published by another replica.
	hear(summary: PresenceSummary): void {
		if (summary.replica === this.opts.replica) return;
		const room = this.rooms.get(summary.room);
		// A room with no local socket has nobody to tell.
		if (!room) return;
		room.remote.set(summary.replica, { at: this.now(), summary });
		if (summary.hello) this.publish(room);
		this.emitIfChanged(room);
	}

	private remove(socketId: string, name: string): void {
		this.byRoom.get(socketId)?.delete(name);
		const room = this.rooms.get(name);
		const m = room?.members.get(socketId);
		if (!room || !m) return;
		room.members.delete(socketId);
		const uid = m.viewer.userId;
		const stillHere = [...room.members.values()].some(
			(x) => x.viewer.userId === uid,
		);
		if (!stillHere) room.typingUntil.delete(uid);
		if (room.members.size === 0) {
			this.rooms.delete(name);
			// Tell the other replicas this one has left the room.
			this.publish(room, true);
			return;
		}
		this.changed(room);
	}

	private tick(): void {
		const now = this.now();
		const beat = now - this.lastBeat >= HEARTBEAT_MS;
		if (beat) this.lastBeat = now;
		for (const room of this.rooms.values()) {
			let lapsed = false;
			for (const [uid, until] of room.typingUntil) {
				if (until <= now) {
					room.typingUntil.delete(uid);
					lapsed = true;
				}
			}
			for (const [replica, r] of room.remote) {
				if (now - r.at > REMOTE_TTL_MS) room.remote.delete(replica);
			}
			// A lapsed lease is published at once, not on the next heartbeat, so
			// other replicas stop showing "typing" when this one does.
			if (beat || lapsed) this.publish(room);
			this.emitIfChanged(room);
		}
	}

	private local(room: Room): { viewers: string[]; typing: string[] } {
		const now = this.now();
		const viewers = new Set<string>();
		for (const m of room.members.values()) viewers.add(m.viewer.userId);
		const typing = [...room.typingUntil]
			.filter(([, until]) => until > now)
			.map(([uid]) => uid);
		return { viewers: [...viewers], typing };
	}

	private snapshot(room: Room): PresenceSnapshot {
		const { viewers, typing } = this.local(room);
		const v = new Set(viewers);
		const t = new Set(typing);
		for (const { summary } of room.remote.values()) {
			for (const u of summary.viewers) v.add(u);
			for (const u of summary.typing) t.add(u);
		}
		return {
			kind: room.ref.kind,
			id: room.ref.id,
			viewers: [...v].sort(),
			typing: [...t].sort(),
		};
	}

	private changed(room: Room, hello = false): void {
		this.publish(room, false, hello);
		this.emitIfChanged(room);
	}

	private emitIfChanged(room: Room): void {
		const snap = this.snapshot(room);
		const key = JSON.stringify([snap.viewers, snap.typing]);
		if (key === room.lastKey) return;
		room.lastKey = key;
		this.opts.transport.emit(room.name, snap);
	}

	private publish(room: Room, empty = false, hello = false): void {
		if (!this.opts.bus) return;
		const { viewers, typing } = empty
			? { viewers: [], typing: [] }
			: this.local(room);
		this.opts.bus.publish({
			replica: this.opts.replica,
			room: room.name,
			viewers,
			typing,
			...(hello ? { hello: true } : {}),
		});
	}
}
