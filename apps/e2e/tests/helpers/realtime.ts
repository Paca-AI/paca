// Helpers for the realtime specs: a socket "probe" that records everything the
// realtime service sends it, and the pieces around it (cookies and tokens of an
// API session, minted short-lived tokens, per-test cleanup).
//
// A probe is a plain socket.io client, the same protocol the web app speaks, so
// the specs check what a client would actually receive: which rooms a join
// puts it in, which events are delivered and which are held back, and when the
// server asks it to reauthenticate or ends the connection.

import { createHmac, randomUUID } from "node:crypto";
import { type APIRequestContext, expect } from "@playwright/test";
import { io, type Socket } from "socket.io-client";
import { BASE_URL } from "./e2e-api";
import { loginContext } from "./profile-users";

export const SOCKET_PATH = "/ws/socket.io";

/** JWT_SECRET of deploy/docker-compose.e2e.yml, to mint tokens the API accepts. */
export const JWT_SECRET =
	process.env.E2E_JWT_SECRET ?? "e2e-jwt-secret-not-for-production";

/**
 * The minted-token specs only make sense against the isolated e2e stack, whose
 * secret is known. Refuse to run them against anything but a local base URL
 * unless E2E_JWT_SECRET was set on purpose.
 */
export function assertE2eStack(): void {
	const local = /^https?:\/\/(localhost|127\.0\.0\.1|\[::1\])(:\d+)?(\/|$)/;
	if (!local.test(BASE_URL) && !process.env.E2E_JWT_SECRET) {
		throw new Error(
			`refusing to mint tokens against ${BASE_URL}: set E2E_JWT_SECRET for a non-local stack`,
		);
	}
}

/**
 * How long a spec waits before it calls something "not delivered". Delivery is
 * asynchronous (a per-event access check), so a negative assertion needs a
 * quiet window; a positive one polls and returns as soon as the event arrives.
 */
export const QUIET_MS = 1_200;

export const sleep = (ms: number) =>
	new Promise<void>((resolve) => setTimeout(resolve, ms));

// ─── What a probe records ────────────────────────────────────────────────────

export interface ReceivedEvent {
	type: string;
	payload: Record<string, unknown>;
	at: number;
}

export interface NamespacesChange {
	projectId: string;
	namespaces: string[];
	at: number;
}

/** One change of a `sync:changes` batch (see services/realtime/src/sync.ts). */
export interface SyncChange {
	seq: number;
	skip?: true;
	type?: string;
	op?: string;
	payload?: Record<string, unknown>;
	entity?: Record<string, unknown>;
	entity_type?: string;
	entity_id?: string;
}

export interface SyncBatch {
	projectId: string;
	epoch: number;
	from: number;
	to: number;
	changes: SyncChange[];
	at: number;
}

export interface SyncJoinAck {
	ok: boolean;
	namespaces: string[];
	epoch: number;
	head: number;
	mode: "live" | "replay" | "reset";
}

export interface SocketAuth {
	/** A Cookie header value, as a browser would send. */
	cookie?: string;
	/** A token in the handshake `auth` payload. */
	token?: string;
}

export class Probe {
	/** Project events delivered as "event". */
	readonly events: ReceivedEvent[] = [];
	/** Notifications delivered as "notification". */
	readonly notifications: ReceivedEvent[] = [];
	/** `permissions:changed` messages. */
	readonly changes: NamespacesChange[] = [];
	/** Times the server sent "reauth". */
	readonly reauths: number[] = [];
	readonly disconnects: Array<{ reason: string; at: number }> = [];
	readonly connectedAt = Date.now();
	/** `sync:changes` batches, in arrival order. */
	readonly batches: SyncBatch[] = [];
	/** `sync:reset` messages. */
	readonly resets: Array<{ epoch: number; head: number; at: number }> = [];
	/** `sync:heartbeat` messages. */
	readonly heartbeats: Array<{ epoch: number; head: number; at: number }> = [];

	private constructor(readonly socket: Socket) {
		socket.on("event", (e: Omit<ReceivedEvent, "at">) =>
			this.events.push({ ...e, at: Date.now() }),
		);
		socket.on("notification", (e: Omit<ReceivedEvent, "at">) =>
			this.notifications.push({ ...e, at: Date.now() }),
		);
		socket.on("permissions:changed", (m: Omit<NamespacesChange, "at">) =>
			this.changes.push({ ...m, at: Date.now() }),
		);
		socket.on("sync:changes", (b: Omit<SyncBatch, "at">) =>
			this.batches.push({ ...b, at: Date.now() }),
		);
		socket.on("sync:reset", (m: { epoch: number; head: number }) =>
			this.resets.push({ ...m, at: Date.now() }),
		);
		socket.on("sync:heartbeat", (m: { epoch: number; head: number }) =>
			this.heartbeats.push({ ...m, at: Date.now() }),
		);
		socket.on("reauth", () => this.reauths.push(Date.now()));
		socket.on("disconnect", (reason: string) =>
			this.disconnects.push({ reason, at: Date.now() }),
		);
	}

	/** Connects, or rejects with the server's refusal message. */
	static async connect(auth: SocketAuth): Promise<Probe> {
		const outcome = await Probe.tryConnect(auth);
		if (!outcome.probe) throw new Error(`connection refused: ${outcome.error}`);
		return outcome.probe;
	}

	/** Connects and reports the refusal instead of throwing. */
	static tryConnect(
		auth: SocketAuth,
	): Promise<{ probe?: Probe; error?: string }> {
		const socket = io(BASE_URL, {
			path: SOCKET_PATH,
			transports: ["websocket"],
			extraHeaders: auth.cookie ? { Cookie: auth.cookie } : undefined,
			auth: auth.token ? { token: auth.token } : undefined,
			reconnection: false,
			forceNew: true,
		});
		return new Promise((resolve) => {
			socket.once("connect", () => resolve({ probe: new Probe(socket) }));
			socket.once("connect_error", (err: Error) => {
				socket.close();
				resolve({ error: err.message });
			});
		});
	}

	get connected(): boolean {
		return this.socket.connected;
	}

	/** Emits "join" and resolves with the namespaces the server granted. */
	join(projectId: string, timeoutMs = 10_000): Promise<string[]> {
		return new Promise((resolve, reject) => {
			this.socket
				.timeout(timeoutMs)
				.emit(
					"join",
					{ projectId },
					(err: Error | null, ack?: { namespaces: string[] }) =>
						err || !ack
							? reject(err ?? new Error("no ack"))
							: resolve(ack.namespaces),
				);
		});
	}

	/** Emits "sync:join" and resolves with the server's answer. */
	syncJoin(
		projectId: string,
		cursor: number | null = null,
		epoch: number | null = null,
		timeoutMs = 10_000,
	): Promise<SyncJoinAck> {
		return new Promise((resolve, reject) => {
			this.socket
				.timeout(timeoutMs)
				.emit(
					"sync:join",
					{ projectId, cursor, epoch },
					(err: Error | null, ack?: SyncJoinAck) =>
						err || !ack ? reject(err ?? new Error("no ack")) : resolve(ack),
				);
		});
	}

	/** Every non-skipped change received so far, in order. */
	syncedChanges(since = 0): SyncChange[] {
		return this.batches
			.filter((b) => b.at >= since)
			.flatMap((b) => b.changes)
			.filter((c) => !c.skip);
	}

	/** Emits "join" with an arbitrary payload and reports whether it was acked. */
	async joinRaw(payload: unknown, waitMs = 1_000): Promise<boolean> {
		return new Promise((resolve) => {
			this.socket
				.timeout(waitMs)
				.emit("join", payload, (err: Error | null) => resolve(!err));
		});
	}

	leave(projectId: string): void {
		this.socket.emit("leave", { projectId });
	}

	/**
	 * Events received since `since` that mention `id` as a whole JSON string
	 * value (not as a substring of another value).
	 */
	eventsAbout(id: string, since = 0): ReceivedEvent[] {
		const needle = JSON.stringify(id);
		return this.events.filter(
			(e) => e.at >= since && JSON.stringify(e.payload).includes(needle),
		);
	}

	eventsOfType(type: string, since = 0): ReceivedEvent[] {
		return this.events.filter((e) => e.type === type && e.at >= since);
	}

	/** The newest `permissions:changed` received since `since`. */
	lastChange(since = 0): NamespacesChange | undefined {
		return this.changes.filter((c) => c.at >= since).at(-1);
	}

	/**
	 * Resolves with the first event at or after `since` (a `Date.now()` mark,
	 * millisecond resolution: take it before the action under test, and treat a
	 * same-millisecond event as after it) that satisfies `match`.
	 */
	async waitForEvent(
		match: (e: ReceivedEvent) => boolean,
		since = 0,
		timeoutMs = 10_000,
	): Promise<ReceivedEvent> {
		const deadline = Date.now() + timeoutMs;
		for (;;) {
			const found = this.events.find((e) => e.at >= since && match(e));
			if (found) return found;
			if (Date.now() > deadline)
				throw new Error("timed out waiting for an event");
			await sleep(50);
		}
	}

	close(): void {
		this.socket.close();
	}
}

// ─── Cleanup ─────────────────────────────────────────────────────────────────

const open: Array<{ close(): unknown }> = [];

/** Registers a probe or API context to be closed by `closeAll`. */
export function track<T extends { close(): unknown }>(thing: T): T {
	open.push(thing);
	return thing;
}

/** Closes every tracked socket and context. Await it from `afterEach`. */
export async function closeAll(): Promise<void> {
	await Promise.allSettled(
		open.splice(0).map(async (thing) => {
			try {
				await thing.close();
			} catch {
				// already closed
			}
		}),
	);
}

// ─── Sessions and tokens ─────────────────────────────────────────────────────

/** The Cookie header an API context would send, for a socket handshake. */
export async function cookieHeaderOf(api: APIRequestContext): Promise<string> {
	const state = await api.storageState();
	return state.cookies.map((c) => `${c.name}=${c.value}`).join("; ");
}

/** The access token an API context holds. */
export async function accessTokenOf(api: APIRequestContext): Promise<string> {
	const state = await api.storageState();
	const cookie = state.cookies.find((c) => c.name === "access_token");
	expect(cookie, "the session has an access_token cookie").toBeTruthy();
	return (cookie as { value: string }).value;
}

const base64url = (value: unknown) =>
	Buffer.from(JSON.stringify(value)).toString("base64url");

/**
 * A copy of a real access token with its own lifetime, signed with the stack's
 * JWT secret: how a spec gets a token that expires in seconds instead of
 * fifteen minutes. `patch` overrides claims (`kind`, `sub`, ...); a negative
 * `ttlSec` gives an already expired token.
 */
export function mintToken(
	realToken: string,
	ttlSec: number,
	patch: Record<string, unknown> = {},
	secret = JWT_SECRET,
): string {
	assertE2eStack();
	const claims = JSON.parse(
		Buffer.from(realToken.split(".")[1] as string, "base64url").toString(),
	) as Record<string, unknown>;
	const now = Math.floor(Date.now() / 1000);
	const body = {
		...claims,
		iat: now - 5,
		nbf: now - 5,
		exp: now + ttlSec,
		jti: randomUUID(),
		...patch,
	};
	const head = base64url({ alg: "HS256", typ: "JWT" });
	const payload = base64url(body);
	const signature = createHmac("sha256", secret)
		.update(`${head}.${payload}`)
		.digest("base64url");
	return `${head}.${payload}.${signature}`;
}

// ─── A signed-in member with a joined socket ─────────────────────────────────

export interface Connected {
	api: APIRequestContext;
	probe: Probe;
	/** The namespaces the join granted. */
	namespaces: string[];
}

/**
 * Signs `username` in, opens a socket on that session's cookie and joins
 * `projectId`. Both are closed by `closeAll`.
 */
export async function connectAs(
	playwright: Parameters<typeof loginContext>[0],
	username: string,
	projectId?: string,
): Promise<Connected> {
	const api = await loginContext(playwright, username);
	track({ close: () => api.dispose() });
	const probe = track(
		await Probe.connect({ cookie: await cookieHeaderOf(api) }),
	);
	const namespaces = projectId ? await probe.join(projectId) : [];
	return { api, probe, namespaces };
}
