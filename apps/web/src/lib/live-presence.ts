// Client side of the ephemeral presence layer (services/realtime/src/presence.ts).
//
// A view joins a "live room" for what it shows (a task, a doc or a chat channel) and learns who
// is viewing and who is typing there. Joins are reference-counted per room,
// re-sent after a reconnect, and nothing is persisted. Typing is throttled:
// one message when it starts, one every TYPING_REFRESH_MS while it goes on,
// one when it stops.

import type { Socket } from "socket.io-client";
import { getSocket } from "./socket-client";

export type LiveKind = "task" | "doc" | "channel";

export interface LiveRef {
	kind: LiveKind;
	projectId: string;
	id: string;
}

export interface LivePresence {
	/** Account ids viewing the room (including your own). */
	viewers: string[];
	/** Account ids typing right now (including your own). */
	typing: string[];
	/** Usernames by account id: what to show when the project's member list
	 *  cannot name someone (no members permission, or a direct message). */
	names: Record<string, string>;
	/** False for a read-only viewer: typing is not sent. */
	canType: boolean;
	/** True once the server accepted the join. */
	joined: boolean;
}

export const EMPTY_PRESENCE: LivePresence = {
	viewers: [],
	typing: [],
	names: {},
	canType: false,
	joined: false,
};

export const TYPING_REFRESH_MS = 3_000;
const JOIN_TIMEOUT_MS = 8_000;
const RETRY_MS = 3_000;
const TYPING_PREF_KEY = "paca:typing-broadcast";

/** Whether this browser announces that you are typing (default on). */
export function isTypingBroadcastEnabled(): boolean {
	try {
		return localStorage.getItem(TYPING_PREF_KEY) !== "off";
	} catch {
		return true;
	}
}

export function setTypingBroadcastEnabled(on: boolean): void {
	try {
		if (on) localStorage.removeItem(TYPING_PREF_KEY);
		else localStorage.setItem(TYPING_PREF_KEY, "off");
	} catch {
		// the preference just does not persist
	}
}

interface Entry {
	ref: LiveRef;
	count: number;
	state: LivePresence;
	listeners: Set<() => void>;
	retry?: ReturnType<typeof setTimeout>;
	typingSince: number;
	typingOn: boolean;
	// joinSeq invalidates a join answer that arrives after a leave or retry.
	joinSeq: number;
}

const entries = new Map<string, Entry>();
let bound: Socket | null = null;

const keyOf = (ref: Pick<LiveRef, "kind" | "id">) => `${ref.kind}:${ref.id}`;

function notify(e: Entry): void {
	for (const fn of e.listeners) fn();
}

function bind(socket: Socket): void {
	if (bound === socket) return;
	bound = socket;
	socket.on("live:presence", (msg: unknown) => {
		const m = msg as {
			kind?: LiveKind;
			id?: string;
			viewers?: string[];
			typing?: string[];
			names?: Record<string, string>;
		};
		if (!m?.kind || !m.id) return;
		const e = entries.get(keyOf({ kind: m.kind, id: m.id }));
		if (!e) return;
		e.state = {
			...e.state,
			viewers: Array.isArray(m.viewers) ? m.viewers : [],
			typing: Array.isArray(m.typing) ? m.typing : [],
			names: m.names && typeof m.names === "object" ? m.names : {},
		};
		notify(e);
	});
	socket.on("live:revoked", (msg: { kind?: LiveKind; id?: string }) => {
		if (!msg?.kind || !msg.id) return;
		const e = entries.get(keyOf({ kind: msg.kind, id: msg.id }));
		if (!e) return;
		e.state = { ...EMPTY_PRESENCE };
		notify(e);
	});
	// A reconnect is a new server session with no rooms: join them all again.
	socket.on("connect", () => {
		for (const e of entries.values()) emitJoin(e);
	});
}

function emitJoin(e: Entry): void {
	const socket = getSocket();
	if (!socket) {
		scheduleRetry(e);
		return;
	}
	bind(socket);
	if (!socket.connected) return; // the "connect" handler joins
	const seq = ++e.joinSeq;
	e.typingOn = false;
	socket
		.timeout(JOIN_TIMEOUT_MS)
		.emit("live:join", e.ref, (err: unknown, res: unknown) => {
			if (seq !== e.joinSeq || !entries.has(keyOf(e.ref))) return;
			const r = res as
				| {
						ok: true;
						snapshot: {
							viewers: string[];
							typing: string[];
							names?: Record<string, string>;
						};
						canType: boolean;
				  }
				| { ok: false; reason: string }
				| undefined;
			if (err || !r) {
				scheduleRetry(e);
				return;
			}
			if (!r.ok) {
				// forbidden or invalid: nothing to show. "unavailable" is worth a retry.
				if (r.reason === "unavailable") scheduleRetry(e);
				return;
			}
			e.state = {
				viewers: r.snapshot.viewers,
				typing: r.snapshot.typing,
				names: r.snapshot.names ?? {},
				canType: r.canType,
				joined: true,
			};
			notify(e);
		});
}

function scheduleRetry(e: Entry): void {
	if (e.retry) return;
	e.retry = setTimeout(() => {
		e.retry = undefined;
		if (entries.get(keyOf(e.ref)) === e) emitJoin(e);
	}, RETRY_MS);
}

/** Join a live room; returns the matching leave. Reference-counted. */
export function joinLive(ref: LiveRef): () => void {
	const key = keyOf(ref);
	let e = entries.get(key);
	if (e) {
		e.count++;
	} else {
		e = {
			ref,
			count: 1,
			state: { ...EMPTY_PRESENCE },
			listeners: new Set(),
			typingSince: 0,
			typingOn: false,
			joinSeq: 0,
		};
		entries.set(key, e);
		emitJoin(e);
	}
	let left = false;
	return () => {
		if (left) return;
		left = true;
		const cur = entries.get(key);
		if (!cur) return;
		cur.count--;
		if (cur.count > 0) return;
		entries.delete(key);
		cur.joinSeq++;
		if (cur.retry) clearTimeout(cur.retry);
		getSocket()?.emit("live:leave", cur.ref);
	};
}

export function subscribeLive(ref: LiveRef, fn: () => void): () => void {
	const e = entries.get(keyOf(ref));
	if (!e) return () => {};
	e.listeners.add(fn);
	return () => e.listeners.delete(fn);
}

export function getLiveState(ref: Pick<LiveRef, "kind" | "id">): LivePresence {
	return entries.get(keyOf(ref))?.state ?? EMPTY_PRESENCE;
}

/** Report typing for a room: true while the composer has text and keys are
 *  being pressed, false when it is cleared or sent. Throttled here. */
export function setLiveTyping(
	ref: Pick<LiveRef, "kind" | "id">,
	typing: boolean,
	now: number = Date.now(),
): void {
	const e = entries.get(keyOf(ref));
	const socket = getSocket();
	if (!e || !socket?.connected || !e.state.canType) return;
	if (!isTypingBroadcastEnabled()) return;
	if (typing) {
		if (e.typingOn && now - e.typingSince < TYPING_REFRESH_MS) return;
		e.typingOn = true;
		e.typingSince = now;
		socket.emit("live:typing", { kind: ref.kind, id: ref.id, typing: true });
	} else if (e.typingOn) {
		e.typingOn = false;
		socket.emit("live:typing", { kind: ref.kind, id: ref.id, typing: false });
	}
}

/** Test hook. */
export function resetLivePresence(): void {
	for (const e of entries.values()) if (e.retry) clearTimeout(e.retry);
	entries.clear();
	bound = null;
}
