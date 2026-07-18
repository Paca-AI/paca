// Shared data-fetch module — the ONLY place that talks to the SDD read API.
//
// Everything goes through the same-origin /sdd-api/* proxy with the caller's
// own Paca session cookie (`credentials: "include"`); the plugin holds no
// credential of its own and the proxy injects the SDD service token server-side
// (deploy/galaxy/sdd-proxy). Responses are RAW JSON from central/index.js — NOT
// the Paca { success, data } envelope — so we parse the body directly.
//
// Each endpoint is memoized for 60 s (per path), and concurrent callers share
// one in-flight request. A failed fetch evicts itself so the next caller
// retries instead of being stuck with a cached rejection for the whole TTL.
// Mirrors com.galaxy.analytics/frontend/src/paca-api.ts.
import { CACHE_TTL_MS, SDD_API_BASE } from "./config";
import type {
	EventsResult,
	FleetResult,
	SddFlagsResult,
	SddOverview,
	SddSpecVersionsResult,
	SessionsResult,
	TasksResult,
	TeamAnalytics,
	TeamCoordination,
	TeamOverview,
} from "./types";

export class SddApiError extends Error {
	status: number;
	constructor(message: string, status: number) {
		super(message);
		this.name = "SddApiError";
		this.status = status;
	}
	/** Session missing/expired — the fix is to reload the SPA, not retry. */
	get isAuthError(): boolean {
		return this.status === 401 || this.status === 403;
	}
}

async function rawGet<T>(path: string): Promise<T> {
	let res: Response;
	try {
		res = await fetch(`${SDD_API_BASE}${path}`, {
			method: "GET",
			credentials: "include",
			headers: { Accept: "application/json" },
		});
	} catch (e) {
		throw new SddApiError(e instanceof Error ? e.message : "network error", 0);
	}
	let body: unknown = null;
	try {
		body = await res.json();
	} catch {
		body = null;
	}
	if (!res.ok) {
		const msg =
			(body &&
				typeof body === "object" &&
				"error" in body &&
				(body as { error?: { message?: string } }).error?.message) ||
			`HTTP ${res.status} on ${path}`;
		throw new SddApiError(String(msg), res.status);
	}
	return body as T;
}

// ── 60 s per-path cache (module-level: shared across every view) ─────────────
interface CacheEntry {
	at: number;
	promise: Promise<unknown>;
}
const cache = new Map<string, CacheEntry>();

function getCached<T>(path: string, opts?: { force?: boolean }): Promise<T> {
	const now = Date.now();
	const hit = cache.get(path);
	if (!opts?.force && hit && now - hit.at < CACHE_TTL_MS) {
		return hit.promise as Promise<T>;
	}
	const promise = rawGet<T>(path);
	cache.set(path, { at: now, promise });
	promise.catch(() => {
		const cur = cache.get(path);
		if (cur && cur.promise === promise) cache.delete(path);
	});
	return promise;
}

/** Drop every cached response (used by the global Refresh button). */
export function clearSddCache(): void {
	cache.clear();
}

type Opts = { force?: boolean };

export const sddApi = {
	teamOverview: (o?: Opts) => getCached<TeamOverview>("/team/overview", o),
	teamAnalytics: (o?: Opts) => getCached<TeamAnalytics>("/team/analytics", o),
	teamFleet: (o?: Opts) => getCached<FleetResult>("/team/fleet", o),
	teamCoordination: (o?: Opts) => getCached<TeamCoordination>("/team/coordination", o),
	sdd: (o?: Opts) => getCached<SddOverview>("/sdd", o),
	specVersions: (o?: Opts) => getCached<SddSpecVersionsResult>("/sdd/spec-versions", o),
	flags: (o?: Opts) => getCached<SddFlagsResult>("/sdd/flags", o),
	sessions: (o?: Opts) => getCached<SessionsResult>("/sessions", o),
	events: (o?: Opts) => getCached<EventsResult>("/events?limit=100", o),
	tasks: (o?: Opts) => getCached<TasksResult>("/tasks", o),
};
