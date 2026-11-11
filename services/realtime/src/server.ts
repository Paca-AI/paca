// HTTP + Socket.IO server.
//
// Authentication flow
// -------------------
// Every Socket.IO connection must present a valid access JWT.  The auth
// middleware resolves the token from two locations in order:
//   1. socket.handshake.auth.token  — preferred for programmatic clients.
//   2. Cookie header "access_token" — automatically sent by browsers that
//      already have an active API session.
//
// The token is NOT verified locally.  Instead the middleware calls
// GET /api/v1/users/me/global-permissions on services/api.  A successful 200
// response proves the token is valid and provides the caller's global
// permissions.  On any non-200 response the connection is refused.
//
// Nothing about the session is persisted: the token stays in memory for the
// socket's lifetime, and a socket never outlives it (see token-expiry.ts).
//
// Room management
// ---------------
// Events are routed through namespace-scoped rooms per project:
//
//   project:<projectId>:tasks        — all task.* events
//   project:<projectId>:docs         — all doc.* events
//   project:<projectId>:workflows    — all workflow.* graph events
//   project:<projectId>:sprints      — all sprint.* events
//   project:<projectId>:views        — all view.* events
//   project:<projectId>:environments — all environment.* events
//
// When a client emits "join" with { projectId }, the server fetches the user's
// effective project actions via the API (which include actions allowed only on
// some resource inside the project) and joins the namespace rooms the user may
// possibly see; the ack lists them. A room is a prefilter: each event is
// checked against the API on its own resource before it is emitted (see
// access.ts), and rooms are re-synced when IAM changes (see resync.ts).
//
// A "leave" event removes the socket from all namespace rooms for that project.
//
// Two more rooms are auto-joined at connect time, keyed purely on the
// socket's own identity rather than any project permission check:
//
//   user:<userId>:notifications  — all notification.* events
//   user:<userId>:agent-chat     — agent.* events with no project_id, i.e. a
//                                  conversation with a global agent from the
//                                  home page / admin pages
//
// Client-side example (using socket.io-client):
//
//   const socket = io("http://localhost", {
//     path: "/ws/socket.io",          // Caddy strips the /ws prefix
//     withCredentials: true,          // send access_token cookie automatically
//   });
//   socket.emit("join", { projectId: "<uuid>" });
//   socket.on("event", ({ type, payload }) => { ... });

import { createServer } from "node:http";
import type { Logger } from "pino";
import { Server } from "socket.io";
import type { AccessDecider } from "./access.ts";
import {
	ApiRequestError,
	fetchProjectPermissions,
	verifyTokenWithAPI,
} from "./api-client.ts";
import { extractTokenFromCookieHeader } from "./auth.ts";
import type { ChangeLogReader } from "./change-log.ts";
import {
	CollabHub,
	type CollabJoinRequest,
	type CollabJoinResult,
	MAX_UPDATE_BYTES,
	parseRoomRef,
	roomName,
} from "./collab.ts";
import type { Config } from "./config.ts";
import { agentChatRoomName } from "./permissions.ts";
import { createResyncer, type Resyncer } from "./resync.ts";
import { syncProjectRooms } from "./rooms.ts";
import { SyncHub } from "./sync.ts";
import { ExpiryWatcher } from "./token-expiry.ts";

// Augment Socket.IO's socket data type.
declare module "socket.io" {
	interface SocketData {
		userId: string;
		username: string;
		// rawToken is kept in memory only (never persisted) so the socket can
		// call the API as its account: joining rooms, re-syncing them and
		// checking each event.
		rawToken: string;
		// expiresAt is the token's exp in epoch milliseconds, when it has one.
		expiresAt?: number;
		// projects are the projects this socket joined and not yet left.
		projects: Set<string>;
		// syncProjects are the projects whose changes this socket receives
		// through the sync protocol (see sync.ts) instead of legacy events.
		syncProjects: Set<string>;
	}
}

export interface SocketServer {
	httpServer: ReturnType<typeof createServer>;
	io: Server;
	resyncer: Resyncer;
	expiry: ExpiryWatcher;
	collab: CollabHub;
	// sync is undefined when the protocol is off (no change log reader, or
	// SYNC_ENABLED=false).
	sync?: SyncHub;
}

export function createSocketServer(
	config: Config,
	logger: Logger,
	decider: AccessDecider,
	changeLog?: ChangeLogReader,
): SocketServer {
	const httpServer = createServer((req, res) => {
		if (req.url === "/healthz" && req.method === "GET") {
			res.writeHead(200, { "Content-Type": "application/json" });
			res.end(JSON.stringify({ status: "ok" }));
			return;
		}
		res.writeHead(404);
		res.end();
	});

	const io = new Server(httpServer, {
		cors: {
			origin: config.cors.origins,
			credentials: true,
		},
		// Collaborative-editing updates (Yjs) travel as binary messages; the
		// default 1 MB limit would drop the connection on a large first sync.
		maxHttpBufferSize: MAX_UPDATE_BYTES + 64 * 1024,
		connectionStateRecovery: {
			maxDisconnectionDuration: 2 * 60 * 1000,
		},
	});

	// ── Auth middleware ────────────────────────────────────────────────────────

	io.use(async (socket, next) => {
		// 1. Prefer explicit token in handshake auth payload.
		//    Runtime-validate that it's a non-empty string — clients could send
		//    a non-string value which would produce a malformed Authorization header.
		const rawAuthToken = socket.handshake.auth?.token;
		let token: string | undefined =
			typeof rawAuthToken === "string" && rawAuthToken
				? rawAuthToken
				: undefined;

		// 2. Fall back to the HttpOnly cookie sent by browsers.
		if (!token) {
			token = extractTokenFromCookieHeader(socket.handshake.headers.cookie);
		}

		if (!token) {
			return next(new Error("missing authentication"));
		}

		try {
			const authResult = await verifyTokenWithAPI(config.apiUrl, token);

			// Keep token and identity in in-memory socket data for later use
			// (joins, resyncs, per-event checks). The raw token is NEVER
			// written to Valkey or logs.
			socket.data.userId = authResult.userId;
			socket.data.username = authResult.username;
			socket.data.rawToken = token;
			socket.data.expiresAt = authResult.expiresAt;
			socket.data.projects = new Set();
			socket.data.syncProjects = new Set();

			next();
		} catch (err) {
			logger.warn({ socketId: socket.id, err }, "socket auth failed");
			next(new Error("invalid or expired token"));
		}
	});

	const expiry = new ExpiryWatcher();

	// Collaborative editing relay (see collab.ts).
	const collab = new CollabHub(
		decider,
		{
			broadcast: (room, except, event, payload) => {
				const target = io.to(room);
				(except ? target.except(except) : target).emit(event, payload);
			},
			grantSeed: (socketId, room) => {
				io.to(socketId).emit("collab:seed", { room });
			},
			cancelSeed: (socketId, room) => {
				io.to(socketId).emit("collab:seed-cancel", { room });
			},
			error: (socketId, room, reason) => {
				io.to(socketId).emit("collab:error", { room, reason });
			},
			revoke: (socketId, room) => {
				logger.info({ socketId, room }, "collab access revoked");
				io.in(socketId).socketsLeave(room);
				io.to(socketId).emit("collab:revoked", { room });
			},
		},
		(err, ctx) => logger.warn({ err, ctx }, "collab error"),
		undefined,
		(info, msg) => logger.info(info, msg),
	);

	// Resumable change delivery (see sync.ts).
	const sync =
		config.syncEnabled && changeLog
			? new SyncHub({
					reader: changeLog,
					decider,
					logger,
					socket: (id) => {
						const s = io.sockets.sockets.get(id);
						if (!s) return undefined;
						return {
							viewer: s.data,
							inRoom: (room) => s.rooms.has(room),
							inProject: (id) =>
								[...s.rooms].some((r) => r.startsWith(`project:${id}:`)),
							disconnect: () => void s.disconnect(true),
						};
					},
					emit: (id, event, payload) => {
						io.to(id).emit(event, payload);
					},
				})
			: undefined;
	sync?.start();

	// ── Connection lifecycle ───────────────────────────────────────────────────

	io.on("connection", (socket) => {
		const { userId, username } = socket.data;
		logger.info({ userId, username, socketId: socket.id }, "client connected");

		// The socket ends with its token: "reauth" a minute before, a
		// disconnect at expiry (the client reconnects with a fresh handshake).
		const cancelExpiry = expiry.watch(socket, socket.data.expiresAt);

		// Auto-join the user's personal notification room so notification.* events
		// can be delivered without an explicit join from the client.
		socket.join(`user:${userId}:notifications`);
		logger.debug(
			{ userId, room: `user:${userId}:notifications` },
			"joined user notification room",
		);

		// Auto-join the user's personal global-agent-chat room, same reasoning
		// as notifications above: a global agent conversation (home page /
		// admin pages, no project) has no project room to place the socket in
		// via the permission-gated "join" flow below, so this is keyed purely
		// on the user's own identity instead.
		socket.join(agentChatRoomName(userId));
		logger.debug(
			{ userId, room: agentChatRoomName(userId) },
			"joined user agent-chat room",
		);

		// Join namespace-scoped rooms for a project. Fetches the account's
		// effective project actions once and joins the rooms whose read action
		// is among them. The optional ack receives { projectId, namespaces } so
		// the client can tell "not allowed" from "failed" (it is never called
		// on failure).
		// joinRooms places the socket in the project's namespace rooms: it
		// fetches the account's effective project actions once and joins the
		// rooms whose read action is among them. null when the account is not
		// a member. Throws when the API cannot be reached; a 401 also
		// disconnects the socket.
		//
		// socket.data.rawToken is captured once at connect time and reused for
		// as long as the WebSocket stays up. Once the access token expires
		// every join 401s and the socket would be left in zero rooms with no
		// recovery. Disconnecting trips socket.io-client's reconnection, which
		// re-runs the handshake against whatever access_token cookie the
		// browser holds by then.
		async function joinRooms(projectId: string): Promise<string[] | null> {
			try {
				const perms = await fetchProjectPermissions(
					config.apiUrl,
					socket.data.rawToken,
					projectId,
				);
				if (perms === null) {
					logger.debug(
						{ userId, projectId },
						"join rejected: not a project member",
					);
					return null;
				}
				const joined = syncProjectRooms(socket, projectId, perms);
				socket.data.projects.add(projectId);
				logger.debug({ userId, projectId, joined }, "joined project rooms");
				return joined;
			} catch (err) {
				logger.warn({ userId, projectId, err }, "project join failed");
				if (err instanceof ApiRequestError && err.status === 401) {
					socket.disconnect(true);
				}
				throw err;
			}
		}

		// Join namespace-scoped rooms for a project. The optional ack receives
		// { projectId, namespaces } so the client can tell "not allowed" from
		// "failed" (it is never called on failure).
		socket.on(
			"join",
			async (
				data: { projectId?: string },
				ack?: (res: { projectId: string; namespaces: string[] }) => void,
			) => {
				const projectId = data?.projectId;
				if (!projectId || typeof projectId !== "string") return;
				try {
					const namespaces = await joinRooms(projectId);
					if (typeof ack === "function") {
						ack({ projectId, namespaces: namespaces ?? [] });
					}
				} catch {
					// Logged in joinRooms; the client retries an unacked join.
				}
			},
		);

		// sync:join is "join" plus the resumable change feed: the same rooms,
		// then everything the project logged since the client's cursor (see
		// sync.ts). { ok: false } makes the client fall back to "join".
		socket.on(
			"sync:join",
			async (
				data: { projectId?: unknown; cursor?: unknown; epoch?: unknown },
				ack?: (res: unknown) => void,
			) => {
				if (typeof ack !== "function") return;
				const projectId = data?.projectId;
				if (!projectId || typeof projectId !== "string") return;
				if (!sync) {
					ack({ ok: false, reason: "disabled" });
					return;
				}
				try {
					const namespaces = await joinRooms(projectId);
					// No rooms means no right to see anything of the project: a
					// non-member, or a member whose roles hold nothing in it. They get
					// no feed, not even the bare skips, which would still tell them how
					// active somebody else's project is.
					if (namespaces === null || namespaces.length === 0) {
						sync.leave(socket.id, projectId);
						socket.data.syncProjects.delete(projectId);
						ack({
							ok: true,
							projectId,
							namespaces: [],
							epoch: 0,
							head: 0,
							mode: "live",
						});
						return;
					}
					// Marked before the join so no legacy event for the project
					// slips through between the hub registering the socket and
					// the mark.
					socket.data.syncProjects.add(projectId);
					const res = await sync.join(socket.id, projectId, {
						cursor: data.cursor,
						epoch: data.epoch,
					});
					ack({ ok: true, projectId, namespaces, ...res });
				} catch (err) {
					socket.data.syncProjects.delete(projectId);
					sync.leave(socket.id, projectId);
					logger.warn({ userId, projectId, err }, "sync join failed");
					ack({ ok: false, reason: "unavailable" });
				}
			},
		);

		// Leave all namespace rooms for a project.
		socket.on("leave", (data: { projectId?: string }) => {
			const projectId = data?.projectId;
			if (!projectId || typeof projectId !== "string") return;
			syncProjectRooms(socket, projectId, null);
			socket.data.projects.delete(projectId);
			socket.data.syncProjects.delete(projectId);
			sync?.leave(socket.id, projectId);
			logger.debug({ userId, projectId }, "left project rooms");
		});

		// ── Collaborative editing ──────────────────────────────────────────
		socket.on(
			"collab:join",
			async (req: CollabJoinRequest, ack?: (res: CollabJoinResult) => void) => {
				if (typeof ack !== "function") return;
				try {
					// socket.join is synchronous with the in-memory adapter, and
					// runs before the hub snapshots the room's state.
					const res = await collab.join(socket.id, socket.data, req, {
						enter: (room) => {
							void socket.join(room);
						},
						isAlive: () => socket.connected,
					});
					ack(res);
				} catch (err) {
					logger.warn({ userId, err }, "collab join failed");
					ack({ ok: false, reason: "unavailable" });
				}
			},
		);
		socket.on("collab:update", (msg: { room?: unknown; update?: unknown }) => {
			collab.update(socket.id, msg?.room, msg?.update);
		});
		socket.on(
			"collab:awareness",
			(msg: { room?: unknown; update?: unknown }) => {
				collab.awarenessUpdate(socket.id, msg?.room, msg?.update);
			},
		);
		socket.on("collab:leave", (msg: { room?: unknown }) => {
			collab.leave(socket.id, msg?.room);
			const ref = parseRoomRef(msg?.room);
			if (ref) void socket.leave(roomName(ref));
		});

		socket.on("disconnect", (reason) => {
			collab.leaveAll(socket.id);
			sync?.dropSocket(socket.id);
			cancelExpiry();
			logger.info(
				{ userId, socketId: socket.id, reason },
				"client disconnected",
			);
		});
	});

	const resyncer = createResyncer(io, config.apiUrl, decider, logger);
	resyncer.start();
	expiry.start();

	return { httpServer, io, resyncer, expiry, collab, sync };
}
