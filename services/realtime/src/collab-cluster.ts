// Collaborative editing across several realtime replicas.
//
// A document's Y.Doc lives in one process (collab.ts), which is what keeps seeding
// and the merge of agent writes simple. With more than one replica, two editors
// of the same document could land on different ones and each would build its own
// copy. This layer keeps one authority per room without changing the hub:
//
//   - A room has an OWNER: the replica that holds its lease in Valkey
//     (SET NX with a TTL the owner keeps renewing). The first replica to see a
//     join for a room takes the lease; later joins, from any replica, are
//     answered by it.
//   - A socket on another replica is a REMOTE member: its join, updates,
//     awareness and leave are forwarded to the owner over a per-replica Valkey
//     channel, and what the hub sends to it (peer updates, revoke, seed grants,
//     errors) is sent back the same way and emitted to the socket by the replica
//     that holds it. The owner's hub treats it like any member, so access checks,
//     the seed grant, rate limits and external-write merges are unchanged.
//   - Replicas heartbeat. An owner drops the members of a replica that stopped; a
//     member whose room lost its owner (crash, lease gone) is told
//     `collab:error unavailable`, which makes its client re-join: it finds or
//     takes the lease and re-syncs from the state it holds, exactly as after a
//     restart of a single replica.
//
// If Valkey cannot be reached, a replica behaves as the owner of what it is
// asked about, which is the single-replica behaviour this service always had.

import type { Viewer } from "./access.ts";
import {
	asBytes,
	type CollabErrorReason,
	type CollabHub,
	type CollabJoinRequest,
	type CollabJoinResult,
	type CollabTransport,
	type ContentChangeMeta,
	parseRoomRef,
	roomName,
} from "./collab.ts";

export const LEASE_TTL_MS = 20_000;
export const LEASE_RENEW_MS = 5_000;
export const HEARTBEAT_TTL_MS = 10_000;
export const HEARTBEAT_EVERY_MS = 3_000;
export const SWEEP_EVERY_MS = 5_000;
export const RPC_TIMEOUT_MS = 5_000;

// ── Ports ────────────────────────────────────────────────────────────────

export interface LeaseStore {
	// claim takes the room's lease if nobody holds it and returns the holder.
	claim(room: string, replica: string, ttlMs: number): Promise<string>;
	// renew extends the lease while this replica still holds it.
	renew(room: string, replica: string, ttlMs: number): Promise<boolean>;
	// release drops the lease if this replica holds it.
	release(room: string, replica: string): Promise<void>;
	// takeover removes a lease held by `from` (a replica found dead).
	takeover(room: string, from: string): Promise<void>;
	owner(room: string): Promise<string | null>;
	heartbeat(replica: string, ttlMs: number): Promise<void>;
	alive(replica: string): Promise<boolean>;
}

export interface ClusterBus {
	send(toReplica: string, message: ClusterMessage): Promise<void>;
	// listen delivers every message addressed to this replica.
	listen(replica: string, handler: (m: ClusterMessage) => void): Promise<void>;
	close(): void;
}

// What the replica that holds a socket does for the cluster.
export interface LocalSockets {
	has(socketId: string): boolean;
	emit(socketId: string, event: string, payload: unknown): void;
	emitToRoom(
		room: string,
		exceptSocketId: string | undefined,
		event: string,
		payload: unknown,
	): void;
	leave(socketId: string, room: string): void;
	// socketsIn lists the local sockets in a room.
	socketsIn(room: string): string[];
}

// ── Messages (JSON; binary travels as {"$b64": "…"}) ─────────────────────

export type ClusterMessage =
	| {
			type: "join";
			corr: string;
			from: string;
			socketId: string;
			viewer: Viewer;
			req: unknown;
	  }
	| { type: "joinResult"; corr: string; result: unknown }
	| {
			type: "update" | "awareness";
			from: string;
			socketId: string;
			room: unknown;
			update: unknown;
	  }
	| { type: "leave"; from: string; socketId: string; room: unknown }
	| { type: "leaveAll"; from: string; socketId: string }
	| { type: "deliver"; socketId: string; event: string; payload: unknown }
	| { type: "revoke"; socketId: string; room: string }
	| {
			type: "broadcast";
			room: string;
			except?: string;
			event: string;
			payload: unknown;
	  };

export function encodeBinary(v: unknown): unknown {
	if (v instanceof Uint8Array) {
		return { $b64: Buffer.from(v).toString("base64") };
	}
	if (Array.isArray(v)) return v.map(encodeBinary);
	if (v && typeof v === "object") {
		return Object.fromEntries(
			Object.entries(v).map(([k, x]) => [k, encodeBinary(x)]),
		);
	}
	return v;
}

export function decodeBinary(v: unknown): unknown {
	if (Array.isArray(v)) return v.map(decodeBinary);
	if (v && typeof v === "object") {
		const o = v as Record<string, unknown>;
		if (typeof o.$b64 === "string" && Object.keys(o).length === 1) {
			return new Uint8Array(Buffer.from(o.$b64, "base64"));
		}
		return Object.fromEntries(
			Object.entries(o).map(([k, x]) => [k, decodeBinary(x)]),
		);
	}
	return v;
}

// ── The cluster ──────────────────────────────────────────────────────────

interface Logger {
	warn(obj: object, msg: string): void;
	info(obj: object, msg: string): void;
}

export interface CollabClusterOptions {
	replicaId: string;
	leases: LeaseStore;
	bus: ClusterBus;
	local: LocalSockets;
	logger: Logger;
	// newHub builds the hub on the transport the cluster provides.
	newHub: (transport: CollabTransport) => CollabHub;
	rpcTimeoutMs?: number;
}

// A remote member as the owner sees it, and an owner as a member sees it.
interface RemoteMember {
	replica: string;
	rooms: Set<string>;
}

export interface CollabJoinHooks {
	enter?: (room: string) => void;
	isAlive?: () => boolean;
}

export class CollabCluster {
	readonly hub: CollabHub;
	private readonly id: string;
	private readonly leases: LeaseStore;
	private readonly bus: ClusterBus;
	private readonly local: LocalSockets;
	private readonly logger: Logger;
	private readonly rpcTimeoutMs: number;

	// Owner side: sockets of other replicas that are members of rooms we hold.
	private readonly remoteMembers = new Map<string, RemoteMember>();
	// Member side: local socket -> room -> the replica that owns it.
	private readonly remoteRooms = new Map<string, Map<string, string>>();
	private readonly waiting = new Map<string, (result: unknown) => void>();
	private corr = 0;
	private timers: ReturnType<typeof setInterval>[] = [];
	private closed = false;

	constructor(opts: CollabClusterOptions) {
		this.id = opts.replicaId;
		this.leases = opts.leases;
		this.bus = opts.bus;
		this.local = opts.local;
		this.logger = opts.logger;
		this.rpcTimeoutMs = opts.rpcTimeoutMs ?? RPC_TIMEOUT_MS;
		this.hub = opts.newHub(this.transport());
	}

	async start(): Promise<void> {
		await this.bus.listen(this.id, (m) => this.onMessage(m));
		this.timers.push(
			setInterval(() => void this.renewLeases(), LEASE_RENEW_MS),
			setInterval(() => void this.heartbeat(), HEARTBEAT_EVERY_MS),
			setInterval(() => void this.sweep(), SWEEP_EVERY_MS),
		);
		for (const t of this.timers) t.unref?.();
		await this.heartbeat();
	}

	close(): void {
		this.closed = true;
		for (const t of this.timers) clearInterval(t);
		this.timers = [];
		this.bus.close();
		this.hub.close();
	}

	// ── the hub's face, as the server uses it ───────────────────────────

	async join(
		socketId: string,
		viewer: Viewer,
		req: CollabJoinRequest,
		hooks: CollabJoinHooks = {},
	): Promise<CollabJoinResult> {
		const ref = parseRoomRef(req);
		if (!ref) return this.hub.join(socketId, viewer, req, hooks);
		const name = roomName(ref);
		let owner = await this.ownerFor(name);
		if (owner !== this.id) {
			const result = await this.remoteJoin(owner, socketId, viewer, req);
			if (result) {
				if (result.ok) {
					if (hooks.isAlive && !hooks.isAlive()) {
						this.send(owner, {
							type: "leave",
							from: this.id,
							socketId,
							room: ref,
						});
						return { ok: false, reason: "unavailable" };
					}
					hooks.enter?.(name);
					this.trackRemote(socketId, name, owner);
				}
				return result;
			}
			// The owner did not answer: its lease may be stale. Take it over when
			// the replica is gone, else report the room unavailable.
			if (await this.leases.alive(owner).catch(() => false)) {
				return { ok: false, reason: "unavailable" };
			}
			await this.leases.takeover(name, owner).catch(() => {});
			owner = await this.ownerFor(name);
			if (owner !== this.id) return { ok: false, reason: "unavailable" };
		}
		return this.hub.join(socketId, viewer, req, hooks);
	}

	update(socketId: string, rawRoom: unknown, rawUpdate: unknown): void {
		const owner = this.ownerOfRemote(socketId, rawRoom);
		if (owner) {
			const bytes = asBytes(rawUpdate);
			if (!bytes) return;
			this.send(owner, {
				type: "update",
				from: this.id,
				socketId,
				room: rawRoom,
				update: encodeBinary(bytes),
			});
			return;
		}
		this.hub.update(socketId, rawRoom, rawUpdate);
	}

	awarenessUpdate(
		socketId: string,
		rawRoom: unknown,
		rawUpdate: unknown,
	): void {
		const owner = this.ownerOfRemote(socketId, rawRoom);
		if (owner) {
			const bytes = asBytes(rawUpdate);
			if (!bytes) return;
			this.send(owner, {
				type: "awareness",
				from: this.id,
				socketId,
				room: rawRoom,
				update: encodeBinary(bytes),
			});
			return;
		}
		this.hub.awarenessUpdate(socketId, rawRoom, rawUpdate);
	}

	leave(socketId: string, rawRoom: unknown): void {
		const ref = parseRoomRef(rawRoom);
		const owner = ref && this.ownerOfRemote(socketId, rawRoom);
		if (ref && owner) {
			this.send(owner, {
				type: "leave",
				from: this.id,
				socketId,
				room: rawRoom,
			});
			this.untrackRemote(socketId, roomName(ref));
			return;
		}
		this.hub.leave(socketId, rawRoom);
	}

	leaveAll(socketId: string): void {
		const rooms = this.remoteRooms.get(socketId);
		if (rooms) {
			for (const owner of new Set(rooms.values())) {
				this.send(owner, { type: "leaveAll", from: this.id, socketId });
			}
			this.remoteRooms.delete(socketId);
		}
		this.hub.leaveAll(socketId);
	}

	recheck(): Promise<void> {
		return this.hub.recheck();
	}

	handleContentEvent(
		type: string,
		payload: Record<string, unknown>,
		meta: ContentChangeMeta = {},
	): Promise<void> {
		// Only the owner holds the room: on every other replica this finds none.
		// A remote member is in the owner's room under its own socket id, so the
		// echo check works the same for it.
		return this.hub.handleContentEvent(type, payload, meta);
	}

	// ── ownership ────────────────────────────────────────────────────────

	private async ownerFor(name: string): Promise<string> {
		if (this.hub.hasRoom(name)) return this.id;
		try {
			return await this.leases.claim(name, this.id, LEASE_TTL_MS);
		} catch (err) {
			// Valkey down: be the authority of what we are asked, as a single
			// replica always was.
			this.logger.warn(
				{ err, room: name },
				"collab lease unavailable, hosting locally",
			);
			return this.id;
		}
	}

	private ownerOfRemote(
		socketId: string,
		rawRoom: unknown,
	): string | undefined {
		const ref = parseRoomRef(rawRoom);
		if (!ref) return undefined;
		return this.remoteRooms.get(socketId)?.get(roomName(ref));
	}

	private trackRemote(socketId: string, room: string, owner: string): void {
		let rooms = this.remoteRooms.get(socketId);
		if (!rooms) {
			rooms = new Map();
			this.remoteRooms.set(socketId, rooms);
		}
		rooms.set(room, owner);
	}

	private untrackRemote(socketId: string, room: string): void {
		const rooms = this.remoteRooms.get(socketId);
		if (!rooms) return;
		rooms.delete(room);
		if (rooms.size === 0) this.remoteRooms.delete(socketId);
	}

	private remoteJoin(
		owner: string,
		socketId: string,
		viewer: Viewer,
		req: CollabJoinRequest,
	): Promise<CollabJoinResult | null> {
		const corr = `${this.id}:${++this.corr}`;
		return new Promise((resolve) => {
			const timer = setTimeout(() => {
				this.waiting.delete(corr);
				resolve(null);
			}, this.rpcTimeoutMs);
			this.waiting.set(corr, (result) => {
				clearTimeout(timer);
				resolve(decodeBinary(result) as CollabJoinResult);
			});
			this.send(owner, {
				type: "join",
				corr,
				from: this.id,
				socketId,
				viewer,
				req: encodeBinary(req),
			});
		});
	}

	private send(replica: string, message: ClusterMessage): void {
		void this.bus
			.send(replica, message)
			.catch((err) =>
				this.logger.warn(
					{ err, replica, type: message.type },
					"collab cluster send failed",
				),
			);
	}

	// ── what arrives for this replica ───────────────────────────────────

	private onMessage(m: ClusterMessage): void {
		if (this.closed) return;
		switch (m.type) {
			case "join":
				void this.onRemoteJoin(m);
				return;
			case "joinResult":
				this.waiting.get(m.corr)?.(m.result);
				this.waiting.delete(m.corr);
				return;
			case "update":
				this.hub.update(m.socketId, m.room, decodeBinary(m.update));
				return;
			case "awareness":
				this.hub.awarenessUpdate(m.socketId, m.room, decodeBinary(m.update));
				return;
			case "leave": {
				const ref = parseRoomRef(m.room);
				if (ref) this.dropRemoteRoom(m.socketId, roomName(ref));
				this.hub.leave(m.socketId, m.room);
				return;
			}
			case "leaveAll":
				this.remoteMembers.delete(m.socketId);
				this.hub.leaveAll(m.socketId);
				return;
			case "deliver":
				this.local.emit(m.socketId, m.event, decodeBinary(m.payload));
				return;
			case "revoke":
				this.local.leave(m.socketId, m.room);
				this.untrackRemote(m.socketId, m.room);
				this.local.emit(m.socketId, "collab:revoked", { room: m.room });
				return;
			case "broadcast":
				this.local.emitToRoom(
					m.room,
					m.except,
					m.event,
					decodeBinary(m.payload),
				);
				return;
		}
	}

	private async onRemoteJoin(
		m: Extract<ClusterMessage, { type: "join" }>,
	): Promise<void> {
		const req = decodeBinary(m.req) as CollabJoinRequest;
		const ref = parseRoomRef(req);
		const name = ref ? roomName(ref) : "";
		// Registered before the join so what the hub sends while joining
		// (a seed grant) can reach the socket.
		if (name) this.addRemoteRoom(m.socketId, m.from, name);
		let result: CollabJoinResult;
		try {
			result = await this.hub.join(m.socketId, m.viewer, req, {
				enter: () => {},
				isAlive: () => true,
			});
		} catch (err) {
			this.logger.warn({ err }, "remote collab join failed");
			result = { ok: false, reason: "unavailable" };
		}
		if (!result.ok && name) this.dropRemoteRoom(m.socketId, name);
		this.send(m.from, {
			type: "joinResult",
			corr: m.corr,
			result: encodeBinary(result),
		});
	}

	private addRemoteRoom(socketId: string, replica: string, room: string): void {
		const r = this.remoteMembers.get(socketId) ?? {
			replica,
			rooms: new Set<string>(),
		};
		r.rooms.add(room);
		this.remoteMembers.set(socketId, r);
	}

	private dropRemoteRoom(socketId: string, room: string): void {
		const r = this.remoteMembers.get(socketId);
		if (!r) return;
		r.rooms.delete(room);
		if (r.rooms.size === 0) this.remoteMembers.delete(socketId);
	}

	// ── the hub's output, routed ────────────────────────────────────────

	private transport(): CollabTransport {
		const deliver = (socketId: string, event: string, payload: unknown) => {
			const remote = this.remoteMembers.get(socketId);
			if (remote) {
				this.send(remote.replica, {
					type: "deliver",
					socketId,
					event,
					payload: encodeBinary(payload),
				});
			} else {
				this.local.emit(socketId, event, payload);
			}
		};
		return {
			broadcast: (room, except, event, payload) => {
				this.local.emitToRoom(room, except, event, payload);
				const replicas = new Set<string>();
				for (const r of this.remoteMembers.values()) {
					if (r.rooms.has(room)) replicas.add(r.replica);
				}
				for (const replica of replicas) {
					this.send(replica, {
						type: "broadcast",
						room,
						except,
						event,
						payload: encodeBinary(payload),
					});
				}
			},
			revoke: (socketId, room) => {
				const remote = this.remoteMembers.get(socketId);
				if (remote) {
					this.dropRemoteRoom(socketId, room);
					this.send(remote.replica, { type: "revoke", socketId, room });
					return;
				}
				this.local.leave(socketId, room);
				this.local.emit(socketId, "collab:revoked", { room });
			},
			grantSeed: (socketId, room) => deliver(socketId, "collab:seed", { room }),
			cancelSeed: (socketId, room) =>
				deliver(socketId, "collab:seed-cancel", { room }),
			error: (socketId, room, reason: CollabErrorReason) =>
				deliver(socketId, "collab:error", { room, reason }),
		};
	}

	// ── keeping the cluster honest ──────────────────────────────────────

	private async renewLeases(): Promise<void> {
		for (const room of this.hub.roomNames()) {
			try {
				if (!(await this.leases.renew(room, this.id, LEASE_TTL_MS))) {
					// Someone else holds it now (we were presumed dead): two
					// authorities would diverge, so give the room up.
					this.logger.warn({ room }, "collab lease lost, dropping the room");
					this.evictRoom(room);
				}
			} catch {
				// Valkey unreachable: keep hosting, retry on the next tick.
			}
		}
	}

	private async heartbeat(): Promise<void> {
		await this.leases.heartbeat(this.id, HEARTBEAT_TTL_MS).catch(() => {});
	}

	// sweep (owner side) drops members of replicas that stopped, and (member
	// side) tells sockets whose room lost its owner to re-join.
	private async sweep(): Promise<void> {
		const gone = new Set<string>();
		for (const r of this.remoteMembers.values()) {
			if (gone.has(r.replica)) continue;
			if (!(await this.leases.alive(r.replica).catch(() => true)))
				gone.add(r.replica);
		}
		for (const [socketId, r] of [...this.remoteMembers]) {
			if (gone.has(r.replica)) {
				this.remoteMembers.delete(socketId);
				this.hub.leaveAll(socketId);
			}
		}

		const owners = new Map<string, string>(); // room -> owner, as members hold it
		for (const rooms of this.remoteRooms.values()) {
			for (const [room, owner] of rooms) owners.set(room, owner);
		}
		for (const [room, owner] of owners) {
			const [current, alive] = await Promise.all([
				this.leases.owner(room).catch(() => owner),
				this.leases.alive(owner).catch(() => true),
			]);
			if (current === owner && alive) continue;
			for (const socketId of this.local.socketsIn(room)) {
				this.untrackRemote(socketId, room);
				this.local.leave(socketId, room);
				this.local.emit(socketId, "collab:error", {
					room,
					reason: "unavailable",
				});
			}
		}
	}

	private evictRoom(room: string): void {
		for (const socketId of [...this.remoteMembers.keys()]) {
			const r = this.remoteMembers.get(socketId);
			if (r?.rooms.has(room)) {
				this.send(r.replica, {
					type: "deliver",
					socketId,
					event: "collab:error",
					payload: { room, reason: "unavailable" },
				});
				this.send(r.replica, { type: "revoke", socketId, room });
				this.dropRemoteRoom(socketId, room);
			}
		}
		for (const socketId of this.local.socketsIn(room)) {
			this.local.leave(socketId, room);
			this.local.emit(socketId, "collab:error", {
				room,
				reason: "unavailable",
			});
		}
		// The hub forgets the room's members through leaveAll for each of them.
		this.hub.releaseRoom(room);
	}
}
