// Client side of real-time collaborative editing (see
// services/realtime/src/collab.ts for the protocol and the rules).
//
// A CollabSession owns one Y.Doc + Awareness for one edited resource (a project
// doc or a task description) and keeps them in sync with the realtime service
// over the app's shared Socket.IO connection. BlockNote's `collaboration`
// option is pointed at `session.fragment` / `session.provider`.
//
// Sessions are reference counted per resource: React Strict Mode (and quick
// re-mounts) acquire/release in quick succession and must not tear the room
// down and rejoin, because both joins share one socket id on the server.

import type { Socket } from "socket.io-client";
import {
	Awareness,
	applyAwarenessUpdate,
	encodeAwarenessUpdate,
} from "y-protocols/awareness";
import * as Y from "yjs";
import { connectSocket } from "./socket-client";

export type CollabKind = "doc" | "task";

export interface CollabRef {
	projectId: string;
	kind: CollabKind;
	id: string;
}

export interface CollabUser {
	name: string;
	color: string;
}

export type CollabStatus = "connecting" | "ready" | "offline" | "lost";

/** Must match COLLAB_FRAGMENT on the server (it only inspects this fragment). */
export const COLLAB_FRAGMENT = "document-store";

const JOIN_TIMEOUT_MS = 8_000;
const JOIN_ATTEMPTS = 3;
const TEARDOWN_DELAY_MS = 0;

function normalizeJoin(res: JoinResult): JoinResult {
	if (!res?.ok) return res;
	return {
		...res,
		update: toBytes(res.update) ?? new Uint8Array(),
		stateVector: toBytes(res.stateVector) ?? new Uint8Array(),
		awareness: toBytes(res.awareness) ?? null,
	};
}

type JoinResult =
	| {
			ok: true;
			room: string;
			seed: boolean;
			update: Uint8Array;
			stateVector: Uint8Array;
			awareness: Uint8Array | null;
	  }
	| { ok: false; reason: string };

const PALETTE = [
	"#e11d48",
	"#ea580c",
	"#ca8a04",
	"#16a34a",
	"#0d9488",
	"#0284c7",
	"#4f46e5",
	"#9333ea",
	"#c026d3",
	"#db2777",
];

/** A stable cursor color for an account, so the same person looks the same everywhere. */
export function colorForUser(userId: string): string {
	let h = 0;
	for (let i = 0; i < userId.length; i++) {
		h = (h * 31 + userId.charCodeAt(i)) >>> 0;
	}
	return PALETTE[h % PALETTE.length];
}

/** socket.io hands binary payloads to the browser as ArrayBuffer, Node as Buffer. */
function toBytes(v: unknown): Uint8Array | undefined {
	if (v instanceof Uint8Array) return v;
	if (v instanceof ArrayBuffer) return new Uint8Array(v);
	return undefined;
}

export class CollabSession {
	readonly doc = new Y.Doc();
	readonly awareness = new Awareness(this.doc);
	readonly fragment = this.doc.getXmlFragment(COLLAB_FRAGMENT);
	/** What BlockNote's `collaboration.provider` expects. */
	readonly provider = { awareness: this.awareness };

	status: CollabStatus = "connecting";
	/** True when this client must write the stored content into the empty room. */
	seed = false;
	/** Set when the server moved the seed grant to another client: stop writing. */
	seedRevoked = false;

	private readonly listeners = new Set<() => void>();
	private readonly seedListeners = new Set<() => void>();
	private destroyed = false;

	private readonly socket: Socket;
	readonly ref: CollabRef;
	readonly user: CollabUser;

	constructor(socket: Socket, ref: CollabRef, user: CollabUser) {
		this.socket = socket;
		this.ref = ref;
		this.user = user;
		this.doc.on("update", this.onDocUpdate);
		this.awareness.on("update", this.onAwarenessUpdate);
		socket.on("collab:update", this.onRemoteUpdate);
		socket.on("collab:awareness", this.onRemoteAwareness);
		socket.on("collab:revoked", this.onRevoked);
		socket.on("collab:seed", this.onSeedGranted);
		socket.on("collab:seed-cancel", this.onSeedCancelled);
		socket.on("collab:error", this.onServerError);
		socket.on("connect", this.onConnect);
		socket.on("disconnect", this.onDisconnect);
	}

	/**
	 * Calls `fn` when this client is (or becomes) the one that must write the
	 * stored content into the room: at join, or later when the client that was
	 * picked first left or stalled. Returns an unsubscribe.
	 */
	whenSeeder(fn: () => void): () => void {
		if (this.seed) fn();
		this.seedListeners.add(fn);
		return () => this.seedListeners.delete(fn);
	}

	private grantSeed(): void {
		this.seedRevoked = false;
		if (this.seed) return;
		this.seed = true;
		for (const fn of [...this.seedListeners]) fn();
	}

	subscribe(fn: () => void): () => void {
		this.listeners.add(fn);
		return () => this.listeners.delete(fn);
	}

	/** Joins the room. Resolves true when the session is ready to edit. */
	async join(): Promise<boolean> {
		let res: JoinResult | null = null;
		// The socket can drop while the join is in flight (e.g. the session
		// refresh reconnects it), losing the ack: retry on the new connection
		// instead of giving up and silently falling back to single-user mode.
		// A join that merely times out is NOT retried: the realtime service is
		// unreachable, and the editor must fall back to plain mode promptly.
		for (
			let attempt = 0;
			attempt < JOIN_ATTEMPTS && !this.destroyed;
			attempt++
		) {
			if (!this.socket.connected) {
				await this.waitForConnect();
				if (!this.socket.connected) break;
			}
			let outcome: JoinResult | null | "dropped";
			try {
				outcome = await this.emitJoin();
			} catch {
				outcome = null;
			}
			if (outcome === "dropped") continue;
			res = outcome;
			break;
		}
		if (this.destroyed) return false;
		if (!res?.ok) {
			this.setStatus(res ? "lost" : "offline");
			return false;
		}
		if (res.seed) this.grantSeed();
		try {
			this.applyJoin(res);
		} catch {
			this.setStatus("lost");
			return false;
		}
		this.setStatus("ready");
		return true;
	}

	destroy(): void {
		if (this.destroyed) return;
		this.destroyed = true;
		this.socket.off("collab:update", this.onRemoteUpdate);
		this.socket.off("collab:awareness", this.onRemoteAwareness);
		this.socket.off("collab:revoked", this.onRevoked);
		this.socket.off("collab:seed", this.onSeedGranted);
		this.socket.off("collab:seed-cancel", this.onSeedCancelled);
		this.socket.off("collab:error", this.onServerError);
		this.socket.off("connect", this.onConnect);
		this.socket.off("disconnect", this.onDisconnect);
		this.awareness.setLocalState(null);
		this.awareness.destroy();
		this.doc.destroy();
		this.listeners.clear();
		this.seedListeners.clear();
		if (this.socket.connected)
			this.socket.emit("collab:leave", { room: this.ref });
	}

	// ── internals ────────────────────────────────────────────────────────

	private waitForConnect(): Promise<void> {
		return new Promise((resolve) => {
			const done = () => {
				clearTimeout(timer);
				this.socket.off("connect", done);
				resolve();
			};
			const timer = setTimeout(done, JOIN_TIMEOUT_MS);
			this.socket.on("connect", done);
		});
	}

	private emitJoin(): Promise<JoinResult | null | "dropped"> {
		return new Promise((resolve) => {
			// A disconnect loses the ack: stop waiting for it.
			const onDrop = () => finish("dropped");
			const finish = (res: JoinResult | null | "dropped") => {
				this.socket.off("disconnect", onDrop);
				resolve(res);
			};
			this.socket.on("disconnect", onDrop);
			this.socket
				.timeout(JOIN_TIMEOUT_MS)
				.emit(
					"collab:join",
					{ ...this.ref, stateVector: Y.encodeStateVector(this.doc) },
					(err: Error | null, res: JoinResult) =>
						finish(err ? null : normalizeJoin(res)),
				);
		});
	}

	private applyJoin(res: Extract<JoinResult, { ok: true }>): void {
		Y.applyUpdate(this.doc, res.update, this);
		// What the room lacks (only non-empty when re-joining after a drop).
		const missing = Y.encodeStateAsUpdate(this.doc, res.stateVector);
		if (missing.byteLength > 2) {
			this.socket.emit("collab:update", { room: this.ref, update: missing });
		}
		if (res.awareness) {
			applyAwarenessUpdate(this.awareness, res.awareness, this);
		}
		// Announce our cursor/user state to the room.
		if (this.awareness.getLocalState()) {
			this.socket.emit("collab:awareness", {
				room: this.ref,
				update: encodeAwarenessUpdate(this.awareness, [this.doc.clientID]),
			});
		}
	}

	private setStatus(status: CollabStatus): void {
		if (this.status === status) return;
		this.status = status;
		for (const fn of this.listeners) fn();
	}

	private isMine(room: CollabRef | undefined): boolean {
		return (
			!!room &&
			room.projectId === this.ref.projectId &&
			room.kind === this.ref.kind &&
			room.id === this.ref.id
		);
	}

	private readonly onDocUpdate = (
		update: Uint8Array,
		origin: unknown,
	): void => {
		// Only our own edits (and the seed) go out; updates received from the
		// room carry this session as their origin.
		if (origin === this || this.status === "lost") return;
		if (this.socket.connected) {
			this.socket.emit("collab:update", { room: this.ref, update });
		}
	};

	private readonly onAwarenessUpdate = (
		change: { added: number[]; updated: number[]; removed: number[] },
		origin: unknown,
	): void => {
		if (origin === this || !this.socket.connected) return;
		const ids = [...change.added, ...change.updated, ...change.removed];
		if (!ids.length) return;
		this.socket.emit("collab:awareness", {
			room: this.ref,
			update: encodeAwarenessUpdate(this.awareness, ids),
		});
	};

	private readonly onRemoteUpdate = (msg: {
		room?: CollabRef;
		update?: Uint8Array;
	}): void => {
		const update = toBytes(msg?.update);
		if (!this.isMine(msg?.room) || !update) return;
		Y.applyUpdate(this.doc, update, this);
	};

	private readonly onRemoteAwareness = (msg: {
		room?: CollabRef;
		update?: Uint8Array;
	}): void => {
		const update = toBytes(msg?.update);
		if (!this.isMine(msg?.room) || !update) return;
		applyAwarenessUpdate(this.awareness, update, this);
	};

	private readonly onRevoked = (msg: { room?: string }): void => {
		if (typeof msg?.room !== "string" || !msg.room.endsWith(`:${this.ref.id}`))
			return;
		this.setStatus("lost");
	};

	private readonly onSeedGranted = (msg: { room?: string }): void => {
		if (typeof msg?.room !== "string" || !msg.room.endsWith(`:${this.ref.id}`))
			return;
		this.grantSeed();
	};

	private readonly onSeedCancelled = (msg: { room?: string }): void => {
		if (typeof msg?.room !== "string" || !msg.room.endsWith(`:${this.ref.id}`))
			return;
		this.seed = false;
		this.seedRevoked = true;
	};

	// The server dropped one of our updates (access check unavailable, rate or
	// size limit, backlog). Re-join to exchange whatever either side missed,
	// at most once at a time.
	private resyncing = false;
	private readonly onServerError = (msg: { room?: string }): void => {
		if (typeof msg?.room !== "string" || !msg.room.endsWith(`:${this.ref.id}`))
			return;
		if (this.resyncing || this.destroyed || this.status !== "ready") return;
		this.resyncing = true;
		setTimeout(() => {
			if (this.destroyed) {
				this.resyncing = false;
				return;
			}
			void this.emitJoin()
				.then((res) => {
					if (this.destroyed) return;
					if (res && res !== "dropped" && res.ok) this.applyJoin(res);
				})
				.finally(() => {
					this.resyncing = false;
				});
		}, 1_000);
	};

	private readonly onDisconnect = (): void => {
		if (this.status === "ready") this.setStatus("offline");
	};

	// socket.io reconnected: the server forgot us, so join again and exchange
	// whatever each side missed.
	private readonly onConnect = (): void => {
		if (
			this.destroyed ||
			this.status === "connecting" ||
			this.status === "lost"
		)
			return;
		void this.emitJoin().then((res) => {
			if (this.destroyed) return;
			if (res && res !== "dropped" && res.ok) {
				if (res.seed) this.grantSeed();
				this.applyJoin(res);
				this.setStatus("ready");
			} else if (res && res !== "dropped") {
				this.setStatus("lost");
			}
		});
	};
}

// ── Shared, reference-counted sessions ────────────────────────────────────

interface Entry {
	session: CollabSession;
	ready: Promise<boolean>;
	refs: number;
	teardown?: ReturnType<typeof setTimeout>;
}

const entries = new Map<string, Entry>();

function keyOf(ref: CollabRef): string {
	return `${ref.projectId}:${ref.kind}:${ref.id}`;
}

export interface CollabHandle {
	session: CollabSession;
	/** Resolves true when the room was joined and editing may start. */
	ready: Promise<boolean>;
	release: () => void;
}

export function acquireCollabSession(
	ref: CollabRef,
	user: CollabUser,
): CollabHandle {
	const key = keyOf(ref);
	let entry = entries.get(key);
	if (entry?.teardown) {
		clearTimeout(entry.teardown);
		entry.teardown = undefined;
	}
	if (!entry) {
		const session = new CollabSession(connectSocket(), ref, user);
		entry = { session, ready: session.join(), refs: 0 };
		entries.set(key, entry);
	}
	entry.refs++;
	const held = entry;
	let released = false;
	return {
		session: held.session,
		ready: held.ready,
		release: () => {
			if (released) return;
			released = true;
			held.refs--;
			if (held.refs > 0) return;
			held.teardown = setTimeout(() => {
				if (held.refs > 0) return;
				held.session.destroy();
				if (entries.get(key) === held) entries.delete(key);
			}, TEARDOWN_DELAY_MS);
		},
	};
}

/**
 * Writes the stored blocks into the (empty) shared doc. BlockNote's Yjs sync
 * may not be listening yet when the editor first mounts, in which case the
 * edit shows locally but never reaches the Y.Doc and so never reaches peers.
 * So: seed on the next tick and retry until the write produced a local Yjs
 * update (an empty placeholder block written by the editor does not count).
 * Returns a canceller for effect cleanup.
 */
/**
 * True while the shared doc holds no real content: nothing, or only the empty
 * placeholder paragraph BlockNote may write (mirrors the server's isEmpty).
 */
export function isCollabDocEmpty(session: CollabSession): boolean {
	return !hasContent(session.fragment);
}

const PLACEHOLDER_TAGS = new Set(["blockgroup", "blockcontainer", "paragraph"]);

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
	if (
		node instanceof Y.XmlElement &&
		!PLACEHOLDER_TAGS.has(node.nodeName.toLowerCase())
	) {
		return true;
	}
	if (node instanceof Y.XmlFragment) {
		return node.toArray().some((child) => hasContent(child as XmlNode));
	}
	return node instanceof Y.XmlHook;
}

export function seedCollabDoc(
	session: CollabSession,
	write: () => void,
	{
		attempts = 20,
		delayMs = 50,
		isActive = () => true,
	}: {
		attempts?: number;
		delayMs?: number;
		/** False once the editor is really gone (not merely StrictMode's test unmount). */
		isActive?: () => boolean;
	} = {},
): () => void {
	let cancelled = false;
	let timer: ReturnType<typeof setTimeout> | undefined;
	let tries = 0;
	const attempt = () => {
		if (cancelled || !isActive() || session.seedRevoked) return;
		// Someone else's content is already there (a peer got in first, or an
		// earlier attempt landed): never replace it with the stored copy.
		if (!isCollabDocEmpty(session)) return;
		let wrote = false;
		const onUpdate = (_u: Uint8Array, origin: unknown) => {
			if (origin !== session) wrote = true;
		};
		session.doc.on("update", onUpdate);
		try {
			write();
		} finally {
			session.doc.off("update", onUpdate);
		}
		if (!wrote && ++tries < attempts) timer = setTimeout(attempt, delayMs);
	};
	timer = setTimeout(attempt, 0);
	return () => {
		cancelled = true;
		if (timer) clearTimeout(timer);
	};
}
