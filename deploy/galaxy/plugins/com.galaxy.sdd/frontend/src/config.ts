// Plugin-wide constants for the NATIVE SDD Fleet plugin (ADR-038).
//
// The plugin reads SDD fleet telemetry SAME-ORIGIN through the Paca gateway's
// /sdd-api/* proxy (deploy/galaxy/sdd-proxy) with the caller's own Paca session
// cookie (`credentials: "include"`). It ships NO secret and no absolute host,
// so the one bundle works on any deployment (tasks.skyplatform.net, a tenant
// instance, dev). The proxy gates on the Paca session and injects the SDD
// service token — the browser never sees it. No iframe.
export const SDD_API_BASE = "/sdd-api";

/** In-memory cache TTL for every SDD endpoint (matches com.galaxy.analytics). */
export const CACHE_TTL_MS = 60_000;

/** The eight fleet views, in sub-rail order (mirrors the standalone app nav). */
export type ViewKey =
	| "overview"
	| "tasks"
	| "sessions"
	| "activity"
	| "analytics"
	| "coordination"
	| "sdd"
	| "fleet";

export const VIEW_ORDER: ViewKey[] = [
	"overview",
	"tasks",
	"sessions",
	"activity",
	"analytics",
	"coordination",
	"sdd",
	"fleet",
];

/** localStorage keys (namespaced) for the remembered view + language. */
export const LS_VIEW = "galaxy.sdd.view";
export const LS_LANG = "galaxy.sdd.lang";
