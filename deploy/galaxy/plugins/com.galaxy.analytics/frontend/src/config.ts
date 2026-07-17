// Plugin-wide constants.
//
// The plugin calls the SAME-ORIGIN Paca REST API with the caller's own
// session cookie (`credentials: "include"`), exactly like the SPA does — it
// carries no secrets and no absolute host, so the same bundle works on any
// deployment (tasks.skyplatform.net, a tenant instance, dev). Routes verified
// against docs/api/http-design.md and the Go handlers (see paca-api.ts).
export const API_BASE = "/api/v1";

/**
 * Tasks page size for the pagination loop. The Go handler
 * (services/api .../task_handler.go ListTasks) clamps page_size to 1..200
 * with default 20; 100 keeps each response comfortably sized.
 */
export const TASKS_PAGE_SIZE = 100;

/** Safety valve for the cursor loop (100 pages x 100 tasks = 10k tasks). */
export const TASKS_MAX_PAGES = 100;

/** In-memory cache TTL for project analytics data. */
export const CACHE_TTL_MS = 60_000;
