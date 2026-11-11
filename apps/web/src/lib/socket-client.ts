// Singleton Socket.IO client.
//
// A single socket connection is shared across the whole app.  Consumers call
// `connectSocket()` once (authenticated layout) and `disconnectSocket()` on
// logout.  Project pages call `joinProject` / `leaveProject` to subscribe to
// namespace-scoped rooms — these are reference-counted per projectId, since
// more than one mounted component can subscribe to the same project
// concurrently (see the comment above `projectSubscriberCounts` below).
//
// The realtime service keeps one room per project and kind of event (tasks,
// docs, sprints, views, conversations, ...). The server places each socket into
// only the rooms its account may possibly see and then checks every event
// against IAM on the resource it is about, so clients always emit join/leave
// and let the server decide. Three server messages shape the client:
//
//   join ack            { projectId, namespaces }: which rooms were joined.
//                       Recorded here; a join that is never acked (the server
//                       could not reach the API) is retried a few times.
//   permissions:changed { projectId, namespaces }: IAM changed, or the rooms
//                       were re-synced. Reported to onProjectNamespaces
//                       listeners with the namespaces that were lost.
//   reauth              the access token is about to expire: refresh the
//                       session and reconnect so the handshake carries the new
//                       cookie, before the server disconnects the socket.

import { io, type Socket } from "socket.io-client";
import { apiClient } from "./api-client";
import {
	clearChangeHeads,
	earliestChangeHead,
	markJoined,
	markLeft,
} from "./change-heads";
import { setCollabSocketProvider } from "./client-ops";
import {
	isSyncEnabled,
	ProjectSync,
	type SyncBatch,
	type SyncHeartbeat,
	type SyncJoinAck,
} from "./sync-engine";

const REALTIME_URL = window.location.origin;

// Mutations name the connected socket (X-Collab-Socket); see client-ops.ts.
setCollabSocketProvider(() =>
	socket?.connected ? (socket.id ?? undefined) : undefined,
);

const SOCKET_PATH = "/ws/socket.io";

let socket: Socket | null = null;

/** Connect (or return the existing) socket.  Safe to call multiple times. */
export function connectSocket(): Socket {
	if (socket?.connected) return socket;

	if (socket) {
		socket.connect();
		return socket;
	}

	socket = io(REALTIME_URL, {
		path: SOCKET_PATH,
		// Cookies are sent automatically by the browser (HttpOnly access_token).
		withCredentials: true,
		// Prefer WebSocket; fall back to polling only when necessary.
		transports: ["websocket", "polling"],
		autoConnect: true,
	});

	// socket.io-client auto-reconnects after most disconnects, but NOT after
	// one the server itself initiated (reason "io server disconnect") — per
	// https://socket.io/docs/v4/client-socket-instance/#disconnect, that case
	// requires an explicit reconnect call. The realtime service disconnects a
	// socket exactly this way when a "join" 401s because its captured
	// access-token snapshot has expired (see services/realtime/src/server.ts)
	// — without this handler that socket would sit disconnected forever,
	// joining no project rooms and receiving nothing, until something else
	// (e.g. a full page reload) established a new connection.
	socket.on("disconnect", (reason) => {
		if (reason === "io server disconnect") {
			socket?.connect();
		}
	});

	// A handshake the server refuses (expired/missing cookie) arrives as
	// "connect_error", and socket.io does NOT retry after it. That is how a
	// tab left idle past its token's expiry ended up with a dead socket until
	// a reload: refresh the session and reconnect ourselves.
	socket.on("connect", () => {
		recoveryAttempt = 0;
	});

	// Only when socket.io has given up (`active` false, i.e. the handshake was
	// refused). For transient failures (server down, network blip) it keeps
	// retrying by itself; recovering as well would duplicate those attempts and
	// hit the refresh endpoint on every one of them.
	socket.on("connect_error", () => {
		if (socket && !socket.active) scheduleRecovery();
	});

	// A background tab may have slept through "reauth" and the expiry
	// disconnect: check the connection as soon as the tab is visible again.
	document.addEventListener("visibilitychange", handleVisibilityChange);

	// The server asks for this shortly before the access token expires and
	// disconnects at expiry. Refresh the session (shared with any refresh the
	// HTTP client is already running) and reconnect on the new cookie; if the
	// refresh fails the session is over and the HTTP client's own 401 handling
	// takes the user to the login page.
	socket.on("reauth", () => {
		void reauthenticate();
	});

	// A project's namespaces changed on the server (see
	// onProjectNamespaces): keep the record and tell the listeners.
	socket.on("permissions:changed", (msg: NamespacesMessage) => {
		recordNamespaces(msg);
	});

	// The resumable change feed (see sync-engine.ts); only a project that
	// joined through "sync:join" has a ProjectSync to receive these.
	socket.on("sync:changes", (batch: SyncBatch) => {
		syncStates.get(batch?.projectId)?.handleBatch(batch);
	});
	socket.on(
		"sync:reset",
		(msg: { projectId: string; epoch: number; head: number }) => {
			syncStates.get(msg?.projectId)?.handleReset(msg);
		},
	);
	socket.on("sync:heartbeat", (beat: SyncHeartbeat) => {
		syncStates.get(beat?.projectId)?.handleHeartbeat(beat);
	});

	return socket;
}

/** Disconnect and destroy the socket.  Call on logout. */
export function disconnectSocket(): void {
	document.removeEventListener("visibilitychange", handleVisibilityChange);
	if (recoveryTimer !== undefined) clearTimeout(recoveryTimer);
	recoveryTimer = undefined;
	recoveryAttempt = 0;
	recoveringSocket = null;
	if (socket) {
		socket.disconnect();
		socket = null;
	}
	projectSubscriberCounts.clear();
	projectNamespaces.clear();
	syncStates.clear();
	clearChangeHeads();
	for (const timer of joinRetryTimers.values()) clearTimeout(timer);
	joinRetryTimers.clear();
}

// ── Recovery of a socket that cannot reconnect by itself ─────────────────────

const RECOVERY_BASE_DELAY_MS = 1_000;
const RECOVERY_MAX_DELAY_MS = 30_000;
let recoveryTimer: ReturnType<typeof setTimeout> | undefined;
let recoveryAttempt = 0;
/** The socket a refresh+connect attempt is currently running for. */
let recoveringSocket: Socket | null = null;

function handleVisibilityChange(): void {
	if (document.visibilityState !== "visible") return;
	if (isSocketDown()) recoverNow();
}

/** Not connected and not about to retry by itself. */
function isSocketDown(): boolean {
	return !!socket && !socket.connected && !socket.active;
}

/** Refresh the session, then reconnect, retrying with backoff. */
function scheduleRecovery(): void {
	if (!socket || recoveryTimer !== undefined) return;
	const delay = Math.min(
		RECOVERY_BASE_DELAY_MS * 2 ** recoveryAttempt,
		RECOVERY_MAX_DELAY_MS,
	);
	recoveryAttempt++;
	recoveryTimer = setTimeout(runRecovery, delay);
}

/** Attempt recovery immediately, preempting any pending backoff timer. The
 *  attempt counter is untouched, so the backoff resumes where it left off if
 *  this attempt fails too. */
function recoverNow(): void {
	if (!socket || recoveringSocket === socket) return;
	if (recoveryTimer !== undefined) clearTimeout(recoveryTimer);
	recoveryTimer = setTimeout(runRecovery, 0);
}

function runRecovery(): void {
	recoveryTimer = undefined;
	void recover();
}

async function recover(): Promise<void> {
	const current = socket;
	if (!current || current.connected) return;
	recoveringSocket = current;
	try {
		await apiClient.refreshSession();
	} catch (error) {
		if (recoveringSocket === current) recoveringSocket = null;
		// The session is really over: stop retrying. The HTTP client's own 401
		// handling takes the user to login, and logout/login recreates the
		// socket. Anything else (offline, 5xx) is retried with backoff.
		if (!isAuthRejection(error)) scheduleRecovery();
		return;
	}
	if (recoveringSocket === current) recoveringSocket = null;
	if (socket !== current) return;
	// recoveryAttempt is reset by the "connect" listener, not here: a refresh
	// that works while the handshake keeps failing must keep backing off.
	current.connect();
}

function isAuthRejection(error: unknown): boolean {
	const status = (error as { response?: { status?: number } })?.response
		?.status;
	return status === 401 || status === 403;
}

/** Reconnect a socket that is down; no-op while connected or retrying. */
export function reconnectSocketIfDown(): void {
	if (isSocketDown()) recoverNow();
}

/** Refresh the session, then reconnect so the handshake uses the new cookie. */
async function reauthenticate(): Promise<void> {
	try {
		await apiClient.refreshSession();
	} catch {
		return;
	}
	const current = socket;
	if (current?.connected) {
		current.disconnect().connect();
	}
}

/** Returns the current socket instance, or null if not connected. */
export function getSocket(): Socket | null {
	return socket;
}

// Reference counts of active `joinProject` callers per projectId. Multiple
// independent components can be subscribed to the same project at once
// (e.g. the persistent project layout that owns the floating AI chat, and a
// nested conversations layout) — without this, one of them unmounting and
// calling `leaveProject` would evict the shared socket from every namespace
// room for that project, silently starving the others of live events until
// something happened to re-join (see `leaveProject` below).
const projectSubscriberCounts = new Map<string, number>();

/**
 * Ask the server to place this socket into the project's namespace rooms.
 * Reference-counted per projectId: call once per mounted subscriber,
 * paired with a matching `leaveProject` on cleanup.
 */
export function joinProject(projectId: string): void {
	const count = projectSubscriberCounts.get(projectId) ?? 0;
	projectSubscriberCounts.set(projectId, count + 1);
	emitJoin(projectId);
}

/**
 * Remove this socket from the project's namespace rooms — but only once
 * every subscriber registered via `joinProject` has also left. This
 * prevents one subscriber's unmount from evicting room membership that a
 * sibling subscriber (e.g. the floating AI chat widget's parent layout)
 * still relies on.
 */
export function leaveProject(projectId: string): void {
	const count = projectSubscriberCounts.get(projectId) ?? 0;
	if (count <= 1) {
		projectSubscriberCounts.delete(projectId);
		projectNamespaces.delete(projectId);
		// The sync state (epoch and cursor) is kept: coming back to the project
		// joins with it, so what changed in between is replayed, or answered
		// with a reset when it cannot be. It is dropped on disconnect/sign-out.
		markLeft(projectId);
		clearJoinRetry(projectId);
		socket?.emit("leave", { projectId });
		return;
	}
	projectSubscriberCounts.set(projectId, count - 1);
}

/**
 * Re-emit "join" for a project after the socket reconnects, without
 * affecting the subscriber count used by `joinProject`/`leaveProject`. A
 * reconnect starts a new server-side session with no room membership, so
 * every project the socket cares about needs to be re-joined — but this
 * isn't a new subscriber, so it must not be counted as one (or a later
 * `leaveProject` would never bring the count back down to zero).
 */
export function rejoinProject(projectId: string): void {
	emitJoin(projectId);
}

// ── Join acknowledgements ────────────────────────────────────────────────────

/** The server's account of which rooms of a project the socket is in. */
export interface NamespacesMessage {
	projectId: string;
	namespaces: string[];
}

/** A change to the namespaces the socket is in for one project. */
export interface NamespacesChange extends NamespacesMessage {
	/** True when the join resumed the change feed from the client's cursor
	 *  (sync protocol): nothing was missed while away, so the project's data
	 *  needs no refetch. False or absent otherwise. */
	resumed?: boolean;
	/** Namespaces the socket was in before and no longer is: the account lost
	 *  access to that kind of data, so what the client cached may be stale. */
	removed: string[];
	/** Namespaces the socket was not in before and now is: the account gained
	 *  access to that kind of data, which the client may hold only as a refused
	 *  (errored) fetch. Empty on the first report for a project, where the
	 *  initial load already fetches everything. */
	added: string[];
}

// The server acks a join with the namespaces it granted. No ack within
// JOIN_ACK_TIMEOUT_MS means the join failed on the server (it could not reach
// the API) and left the socket in no rooms, which used to go unnoticed until
// the next reconnect: retry a few times.
const JOIN_ACK_TIMEOUT_MS = 10_000;
const JOIN_RETRY_DELAY_MS = 5_000;
const JOIN_MAX_ATTEMPTS = 3;

const projectNamespaces = new Map<string, string[]>();
const joinRetryTimers = new Map<string, ReturnType<typeof setTimeout>>();
const namespaceListeners = new Set<(change: NamespacesChange) => void>();

// ── Sync protocol ────────────────────────────────────────────────────────────

// One ProjectSync per project joined through "sync:join".
const syncStates = new Map<string, ProjectSync>();
const syncChangeListeners = new Set<
	(projectId: string, event: RealtimeEvent) => void
>();
const syncResetListeners = new Set<(projectId: string) => void>();

/** Listen for changes delivered by the sync protocol, in order. */
export function onProjectChange(
	listener: (projectId: string, event: RealtimeEvent) => void,
): () => void {
	syncChangeListeners.add(listener);
	return () => {
		syncChangeListeners.delete(listener);
	};
}

/** Listen for a project whose missed changes cannot be replayed: refetch. */
export function onProjectReset(
	listener: (projectId: string) => void,
): () => void {
	syncResetListeners.add(listener);
	return () => {
		syncResetListeners.delete(listener);
	};
}

function syncStateFor(projectId: string): ProjectSync {
	let state = syncStates.get(projectId);
	if (!state) {
		state = new ProjectSync({
			onChange: (event) => {
				for (const l of syncChangeListeners) l(projectId, event);
			},
			onReset: () => {
				for (const l of syncResetListeners) l(projectId);
			},
			requestResync: () => {
				if (projectSubscriberCounts.has(projectId)) emitJoin(projectId);
			},
		});
		syncStates.set(projectId, state);
	}
	return state;
}

type SyncJoinResponse =
	| ({ ok: true; projectId: string; namespaces: string[] } & SyncJoinAck)
	| { ok: false; reason?: string };

function clearJoinRetry(projectId: string): void {
	const timer = joinRetryTimers.get(projectId);
	if (timer !== undefined) clearTimeout(timer);
	joinRetryTimers.delete(projectId);
}

function emitJoin(projectId: string, attempt = 1): void {
	if (!socket) return;
	clearJoinRetry(projectId);
	if (isSyncEnabled()) {
		emitSyncJoin(projectId, attempt);
		return;
	}
	emitLegacyJoin(projectId, attempt);
}

// sync:join does what "join" does and also returns the change feed from the
// client's cursor. Two kinds of failure are told apart:
//
//  - The server answered that it cannot serve the feed (disabled, an older
//    server, an error): fall back to the plain join, which still works and is
//    followed by a refetch on reconnect, as before.
//  - No answer (the socket dropped or the server was unreachable): the cursor
//    is kept and the join retried, so a client that was away resumes from where
//    it was instead of losing what it missed. Only after the retries run out is
//    the plain join used, with the cursor still kept for the next attempt.
function emitSyncJoin(projectId: string, attempt: number): void {
	if (!socket) return;
	const state = syncStateFor(projectId);
	// A first join starts where the page's data was read, not at the head, so a
	// change made between its first requests and this join is replayed.
	const hadCursor = state.cursor !== null;
	const seen = earliestChangeHead(projectId);
	if (seen) state.seed(seen);
	socket
		.timeout(JOIN_ACK_TIMEOUT_MS)
		.emit(
			"sync:join",
			{ projectId, ...state.joinRequest() },
			(err: Error | null, res?: SyncJoinResponse) => {
				if (!err && res?.ok) {
					markJoined(projectId);
					const resumed = state.applyJoinAck(res);
					recordNamespaces({
						projectId,
						namespaces: res.namespaces ?? [],
						resumed,
					});
					return;
				}
				if (!err) {
					// Refused: this server does not serve the feed. A client that was
					// following it has no way to replay what it missed any more, so
					// it refetches, as after a reset.
					syncStates.delete(projectId);
					if (hadCursor) for (const l of syncResetListeners) l(projectId);
					emitLegacyJoin(projectId, attempt);
					return;
				}
				if (!projectSubscriberCounts.has(projectId)) return;
				if (attempt >= JOIN_MAX_ATTEMPTS) {
					emitLegacyJoin(projectId, 1);
					return;
				}
				joinRetryTimers.set(
					projectId,
					setTimeout(() => {
						joinRetryTimers.delete(projectId);
						if (socket?.connected && projectSubscriberCounts.has(projectId)) {
							emitJoin(projectId, attempt + 1);
						}
					}, JOIN_RETRY_DELAY_MS),
				);
			},
		);
}

function emitLegacyJoin(projectId: string, attempt = 1): void {
	if (!socket) return;
	socket
		.timeout(JOIN_ACK_TIMEOUT_MS)
		.emit(
			"join",
			{ projectId },
			(err: Error | null, ack?: NamespacesMessage) => {
				if (!err && ack) {
					recordNamespaces({
						projectId,
						namespaces: ack.namespaces ?? [],
					});
					return;
				}
				if (
					attempt >= JOIN_MAX_ATTEMPTS ||
					!projectSubscriberCounts.has(projectId)
				) {
					return;
				}
				joinRetryTimers.set(
					projectId,
					setTimeout(() => {
						joinRetryTimers.delete(projectId);
						if (socket?.connected && projectSubscriberCounts.has(projectId)) {
							emitJoin(projectId, attempt + 1);
						}
					}, JOIN_RETRY_DELAY_MS),
				);
			},
		);
}

function recordNamespaces({
	projectId,
	namespaces,
	resumed,
}: NamespacesMessage & { resumed?: boolean }): void {
	// Ignore a message for a project nobody on this client subscribes to (a
	// late ack after leaveProject).
	if (!projectSubscriberCounts.has(projectId)) return;
	const before = projectNamespaces.get(projectId);
	projectNamespaces.set(projectId, namespaces);
	const removed = (before ?? []).filter((ns) => !namespaces.includes(ns));
	const added = before ? namespaces.filter((ns) => !before.includes(ns)) : [];
	for (const listener of namespaceListeners) {
		listener({ projectId, namespaces, removed, added, resumed });
	}
}

/** The namespaces the server last said this socket is in for a project, or
 *  undefined before the first ack. */
export function getProjectNamespaces(
	projectId: string,
): readonly string[] | undefined {
	return projectNamespaces.get(projectId);
}

/**
 * Listen for the server reporting a project's namespaces: the join ack and
 * every `permissions:changed` (IAM changed, or the rooms were re-synced).
 * Returns the unsubscribe function.
 */
export function onProjectNamespaces(
	listener: (change: NamespacesChange) => void,
): () => void {
	namespaceListeners.add(listener);
	return () => {
		namespaceListeners.delete(listener);
	};
}

// ── Typed event payloads ─────────────────────────────────────────────────────

export interface RealtimeEvent {
	type: string;
	payload: Record<string, unknown>;
	/** Present on events delivered by the sync protocol: what the change did
	 *  and, when the server attached it, the entity after the change, so the
	 *  cache can be patched instead of refetched (see cache-patch.ts). */
	change?: {
		op?: string;
		entity?: Record<string, unknown>;
		entityType?: string;
		entityId?: string;
		taskId?: string;
		/** Set when this tab's own mutation caused the change (its echo). */
		clientOpId?: string;
	};
}
