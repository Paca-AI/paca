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
// Session data (userId, username, role, globalPermissions, projectPermissions)
// is stored in Valkey for auditing and recovery purposes.
//
// Room management
// ---------------
// Events are routed through three namespace-scoped rooms per project:
//
//   project:<projectId>:tasks      — all task.* events
//   project:<projectId>:docs       — all doc.* events
//   project:<projectId>:workflows  — all workflow.* graph events
//
// When a client emits "join" with { projectId }, the server fetches the user's
// project permissions via the API, then joins only the namespace rooms the user
// is permitted to see.  The Pub/Sub subscriber routes events directly to the
// correct room — no per-message permission checks are needed.
//
// A "leave" event removes the socket from all namespace rooms for that project.
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
import type Redis from "ioredis";
import type { Logger } from "pino";
import { Server } from "socket.io";
import {
	ApiRequestError,
	fetchProjectPermissions,
	verifyTokenWithAPI,
} from "./api-client.ts";
import { extractTokenFromCookieHeader } from "./auth.ts";
import type { Config } from "./config.ts";
import {
	type EventNamespace,
	hasProjectPermission,
	NAMESPACE_PERMISSIONS,
	projectRoomName,
} from "./permissions.ts";
import {
	deleteSession,
	type Session,
	saveSession,
	setProjectPermissions,
} from "./session.ts";

// Augment Socket.IO's socket data type.
declare module "socket.io" {
	interface SocketData {
		userId: string;
		username: string;
		// rawToken is kept in memory only (never persisted) so the socket can
		// re-call the API when joining a project room.
		rawToken: string;
	}
}

export interface SocketServer {
	httpServer: ReturnType<typeof createServer>;
	io: Server;
}

export function createSocketServer(
	config: Config,
	sessionRedis: Redis,
	logger: Logger,
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

			// Persist the session in Valkey for auditing and recovery purposes.
			const session: Session = {
				userId: authResult.userId,
				username: authResult.username,
				role: authResult.role,
				globalPermissions: authResult.globalPermissions,
				projectPermissions: {},
			};
			await saveSession(sessionRedis, socket.id, session);

			// Keep token and identity in in-memory socket data for later use
			// (e.g. project join calls).  The raw token is NEVER written to Valkey.
			socket.data.userId = authResult.userId;
			socket.data.username = authResult.username;
			socket.data.rawToken = token;

			next();
		} catch (err) {
			logger.warn({ socketId: socket.id, err }, "socket auth failed");
			next(new Error("invalid or expired token"));
		}
	});

	// ── Connection lifecycle ───────────────────────────────────────────────────

	io.on("connection", (socket) => {
		const { userId, username } = socket.data;
		logger.info({ userId, username, socketId: socket.id }, "client connected");

		// Auto-join the user's personal notification room so notification.* events
		// can be delivered without an explicit join from the client.
		socket.join(`user:${userId}:notifications`);
		logger.debug(
			{ userId, room: `user:${userId}:notifications` },
			"joined user notification room",
		);

		// Join namespace-scoped rooms for a project.  Fetches project permissions
		// once and joins only the rooms the user is allowed to see:
		//   project:<projectId>:tasks  (if user has tasks.read)
		//   project:<projectId>:docs   (if user has docs.read)
		socket.on("join", async (data: { projectId?: string }) => {
			const projectId = data?.projectId;
			if (!projectId || typeof projectId !== "string") return;

			try {
				const perms = await fetchProjectPermissions(
					config.apiUrl,
					socket.data.rawToken,
					projectId,
				);

				if (perms === null) {
					// User is not a member of this project — silently ignore.
					logger.debug(
						{ userId, projectId },
						"join rejected: not a project member",
					);
					return;
				}

				await setProjectPermissions(sessionRedis, socket.id, projectId, perms);

				const joinedNamespaces: string[] = [];
				for (const [ns, requiredPerm] of Object.entries(
					NAMESPACE_PERMISSIONS,
				)) {
					if (hasProjectPermission(perms, requiredPerm)) {
						socket.join(projectRoomName(projectId, ns as EventNamespace));
						joinedNamespaces.push(ns);
					}
				}
				logger.debug(
					{ userId, projectId, joinedNamespaces },
					"joined project rooms",
				);
			} catch (err) {
				logger.warn({ userId, projectId, err }, "project join failed");
				// socket.data.rawToken is captured once at connect time and reused
				// for every "join" on this socket for as long as the WebSocket
				// stays up — unlike a plain HTTP request, it has no natural
				// expiry that would otherwise force a fresh handshake. Once the
				// access token expires, every join from then on 401s the same
				// way and the socket is left in zero rooms with no recovery.
				// Disconnecting trips socket.io-client's built-in automatic
				// reconnection, which re-runs this connection's auth handshake
				// against whatever access_token cookie the browser holds at that
				// point — fresh, if the client has refreshed it by then.
				if (err instanceof ApiRequestError && err.status === 401) {
					socket.disconnect(true);
				}
			}
		});

		// Leave all namespace rooms for a project.
		socket.on("leave", (data: { projectId?: string }) => {
			const projectId = data?.projectId;
			if (!projectId || typeof projectId !== "string") return;
			for (const ns of Object.keys(NAMESPACE_PERMISSIONS)) {
				socket.leave(projectRoomName(projectId, ns as EventNamespace));
			}
			logger.debug({ userId, projectId }, "left project rooms");
		});

		socket.on("disconnect", async (reason) => {
			logger.info(
				{ userId, socketId: socket.id, reason },
				"client disconnected",
			);
			try {
				await deleteSession(sessionRedis, socket.id);
			} catch (err) {
				logger.warn(
					{ userId, socketId: socket.id, reason, err },
					"failed to delete session on disconnect",
				);
			}
		});
	});

	return { httpServer, io };
}
