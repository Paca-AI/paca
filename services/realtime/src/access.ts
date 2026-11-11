// Per-event authorization.
//
// A project room only says an account may possibly receive a kind of event
// (some role gives it the namespace's read action, maybe on a single sprint's
// tasks). IAM can still exclude one task or one conversation from such an
// account, and the REST routes honour that, so an event is delivered to a
// socket only after the API confirms the socket's account holds the event's
// action on the event's resource. This module owns that question: it asks the
// API's IAM engine in batches, caches the answers briefly, and keeps the
// events of one resource in order.
//
// Nothing here decides anything itself — no policy, no wildcard matching. A
// cached answer is only ever the API's, and it is dropped when the API
// announces an IAM change (see AccessDecider.flush).

import type { DefaultEventsMap, RemoteSocket } from "socket.io";
import {
	type AccessQuery,
	ApiRequestError,
	checkAccess,
} from "./api-client.ts";

const DEFAULT_TTL_MS = 30_000;
// The API refuses more than this many checks per request.
const MAX_BATCH = 200;
const SWEEP_THRESHOLD = 10_000;

export interface Viewer {
	userId: string;
	rawToken: string;
	// Projects this socket receives through the sync protocol (see sync.ts).
	// For those, a logged event reaches the socket as a "sync:changes" batch,
	// not as a legacy "event".
	syncProjects?: Set<string>;
}

interface Entry {
	expires: number;
	allowed: Promise<boolean>;
}

interface Waiter {
	query: AccessQuery;
	resolve: (allowed: boolean) => void;
	reject: (err: unknown) => void;
}

interface Batch {
	viewer: Viewer;
	projectId: string;
	waiters: Waiter[];
}

export interface AccessDeciderOptions {
	ttlMs?: number;
	check?: typeof checkAccess;
}

export class AccessDecider {
	private readonly cache = new Map<string, Entry>();
	private readonly batches = new Map<string, Batch>();
	private readonly ttlMs: number;
	private readonly check: typeof checkAccess;

	constructor(
		private readonly apiUrl: string,
		{ ttlMs, check }: AccessDeciderOptions = {},
	) {
		this.ttlMs = ttlMs ?? DEFAULT_TTL_MS;
		this.check = check ?? checkAccess;
	}

	// can resolves true only on a positive answer from the API. A failure is
	// not cached, so the next event retries; it rejects so the caller can log
	// it and fail closed.
	can(viewer: Viewer, projectId: string, query: AccessQuery): Promise<boolean> {
		const key = `${viewer.userId}\u0000${query.action}\u0000${query.resource}`;
		const now = Date.now();
		const hit = this.cache.get(key);
		if (hit && hit.expires > now) return hit.allowed;

		if (this.cache.size >= SWEEP_THRESHOLD) {
			for (const [k, e] of this.cache) {
				if (e.expires <= now) this.cache.delete(k);
			}
		}
		const allowed = new Promise<boolean>((resolve, reject) => {
			this.enqueue(viewer, projectId, { query, resolve, reject });
		});
		const entry = { expires: now + this.ttlMs, allowed };
		this.cache.set(key, entry);
		allowed.catch(() => {
			if (this.cache.get(key) === entry) this.cache.delete(key);
		});
		return allowed;
	}

	// flush forgets every cached answer. Called when the API announces that a
	// role or an attachment changed: the next event is checked afresh. A check
	// already in flight still answers its own waiters, but is no longer cached.
	flush(): void {
		this.cache.clear();
	}

	private enqueue(viewer: Viewer, projectId: string, waiter: Waiter): void {
		const key = `${viewer.userId}\u0000${projectId}`;
		let batch = this.batches.get(key);
		if (!batch) {
			batch = { viewer, projectId, waiters: [] };
			this.batches.set(key, batch);
			// A microtask, not a timer: everything asked in the same turn of
			// the event loop (one event fanning out to a room's sockets) shares
			// the request, and nothing waits on a delay.
			queueMicrotask(() => void this.send(key));
		}
		batch.waiters.push(waiter);
	}

	private async send(key: string): Promise<void> {
		const batch = this.batches.get(key);
		this.batches.delete(key);
		if (!batch) return;
		for (let i = 0; i < batch.waiters.length; i += MAX_BATCH) {
			const chunk = batch.waiters.slice(i, i + MAX_BATCH);
			try {
				const answers = await this.check(
					this.apiUrl,
					batch.viewer.rawToken,
					batch.projectId,
					chunk.map((w) => w.query),
				);
				chunk.forEach((w, j) => {
					w.resolve(answers[j] === true);
				});
			} catch (err) {
				for (const w of chunk) w.reject(err);
			}
		}
	}
}

// KeyedQueue runs jobs with the same key one after another, so the events of
// one resource keep their order even when an earlier one waits on an access
// check. Jobs of different keys run concurrently.
export class KeyedQueue {
	private readonly tails = new Map<string, Promise<void>>();

	run(key: string, job: () => Promise<void>): void {
		const prev = this.tails.get(key) ?? Promise.resolve();
		const next = prev.then(job, job).then(
			() => undefined,
			() => undefined,
		);
		this.tails.set(key, next);
		void next.then(() => {
			if (this.tails.get(key) === next) {
				this.tails.delete(key);
			}
		});
	}
}

// deliverToReaders emits the event to each socket of `sockets` whose account
// may perform query.action on query.resource. A socket whose token the API
// rejects (401) is disconnected so it reconnects with a fresh one; any other
// failure skips that socket for this event.
export async function deliverToReaders(
	sockets: RemoteSocket<DefaultEventsMap, Viewer>[],
	decider: AccessDecider,
	projectId: string,
	query: AccessQuery,
	event: { type: string; payload: Record<string, unknown> },
	onError: (err: unknown, userId: string) => void,
	// logged: the event is in the project's change log, so sockets that sync
	// the project get it from there.
	logged = false,
): Promise<void> {
	await Promise.all(
		sockets.map(async (s) => {
			if (logged && s.data.syncProjects?.has(projectId)) return;
			try {
				if (await decider.can(s.data, projectId, query)) {
					s.emit("event", event);
				}
			} catch (err) {
				onError(err, s.data.userId);
				if (err instanceof ApiRequestError && err.status === 401) {
					s.disconnect(true);
				}
			}
		}),
	);
}
