// Singleton Socket.IO client.
//
// A single socket connection is shared across the whole app.  Consumers call
// `connectSocket()` once (authenticated layout) and `disconnectSocket()` on
// logout.  Project pages call `joinProject` / `leaveProject` to subscribe to
// namespace-scoped rooms.
//
// The realtime service uses two rooms per project:
//   project:<projectId>:tasks  — task.* events
//   project:<projectId>:docs   — doc.* events
//
// The server places each socket into only the rooms it has permission for, so
// clients always emit join/leave for both namespaces and let the server decide.

import { io, type Socket } from "socket.io-client";

const REALTIME_URL = window.location.origin;

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

	return socket;
}

/** Disconnect and destroy the socket.  Call on logout. */
export function disconnectSocket(): void {
	if (socket) {
		socket.disconnect();
		socket = null;
	}
}

/** Returns the current socket instance, or null if not connected. */
export function getSocket(): Socket | null {
	return socket;
}

/** Ask the server to place this socket into the project's namespace rooms. */
export function joinProject(projectId: string): void {
	socket?.emit("join", { projectId });
}

/** Remove this socket from the project's namespace rooms. */
export function leaveProject(projectId: string): void {
	socket?.emit("leave", { projectId });
}

// ── Typed event payloads ─────────────────────────────────────────────────────

export interface RealtimeEvent {
	type: string;
	payload: Record<string, unknown>;
}
