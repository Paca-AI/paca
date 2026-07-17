// Shared data-fetch module — the ONLY place that talks to the Paca REST API.
//
// Endpoints used (verified against docs/api/http-design.md and the Go
// handlers in services/api/internal/transport/http/):
//
//   GET /api/v1/projects/{projectId}/sprints        -> { items: Sprint[] }
//   GET /api/v1/projects/{projectId}/task-statuses  -> { items: TaskStatus[] }
//   GET /api/v1/projects/{projectId}/tasks          -> { items, page_size,
//                                                        next_cursor, total_count }
//
// Every response uses the platform envelope { success, data, request_id }
// (error shape: { success: false, error, request_id }).
//
// Pagination note (differs from the http-design.md *recommendation*): the
// tasks endpoint is CURSOR-paginated in the actual implementation
// (task_handler.go: `page_size` clamped 1..200 + `cursor` -> `next_cursor`),
// not page/page_size. We therefore loop page_size=100 batches following
// next_cursor until it comes back null.
//
// Auth: the caller's own session cookie, `credentials: "include"` — the
// plugin sees exactly what the signed-in user can see (tasks.read /
// sprints.read enforced server-side) and holds no credentials of its own.
import { API_BASE, CACHE_TTL_MS, TASKS_MAX_PAGES, TASKS_PAGE_SIZE } from "./config";

// ── API shapes (subset the charts need; field names match the Go DTOs) ───────

export type SprintStatus = "planned" | "active" | "completed";

export interface Sprint {
	id: string;
	project_id: string;
	name: string;
	start_date?: string | null;
	end_date?: string | null;
	goal?: string | null;
	status: SprintStatus;
	created_at: string;
	updated_at: string;
}

export type StatusCategory =
	| "backlog"
	| "refinement"
	| "ready"
	| "todo"
	| "inprogress"
	| "done";

export interface TaskStatus {
	id: string;
	project_id: string;
	name: string;
	color?: string | null;
	position: number;
	category: StatusCategory;
	is_default?: boolean;
}

export interface Task {
	id: string;
	project_id: string;
	task_number: number;
	title: string;
	task_type_id?: string | null;
	status_id?: string | null;
	sprint_id?: string | null;
	parent_task_id?: string | null;
	importance: number;
	story_points?: number | null;
	assignee_ids?: string[];
	tags?: string[];
	start_date?: string | null;
	due_date?: string | null;
	created_at: string;
	updated_at: string;
}

interface Envelope<T> {
	success: boolean;
	data?: T;
	error?: string;
	request_id?: string;
}

interface TaskListResult {
	items: Task[];
	page_size: number;
	next_cursor?: string | null;
	total_count?: number;
}

/** Everything the four analytics panels need, fetched once per project. */
export interface ProjectAnalyticsData {
	sprints: Sprint[];
	statuses: TaskStatus[];
	tasks: Task[];
	/** When this snapshot was taken (drives the "as of" footnote). */
	fetchedAt: number;
}

// ── Errors ────────────────────────────────────────────────────────────────────

export class PacaApiError extends Error {
	status: number;
	requestId?: string;

	constructor(message: string, status: number, requestId?: string) {
		super(message);
		this.name = "PacaApiError";
		this.status = status;
		this.requestId = requestId;
	}

	/** Session missing/expired — the fix is to reload the SPA, not retry. */
	get isAuthError(): boolean {
		return this.status === 401 || this.status === 403;
	}
}

async function fetchEnvelope<T>(path: string): Promise<T> {
	let res: Response;
	try {
		res = await fetch(`${API_BASE}${path}`, {
			method: "GET",
			credentials: "include",
			headers: { Accept: "application/json" },
		});
	} catch (e) {
		throw new PacaApiError(
			e instanceof Error ? e.message : "network error",
			0,
		);
	}

	let body: Envelope<T> | null = null;
	try {
		body = (await res.json()) as Envelope<T>;
	} catch {
		body = null;
	}

	if (!res.ok || !body || body.success !== true || body.data === undefined) {
		throw new PacaApiError(
			(body && body.error) || `HTTP ${res.status} on ${path}`,
			res.status,
			body?.request_id,
		);
	}
	return body.data;
}

// ── Endpoint wrappers ─────────────────────────────────────────────────────────

export function listSprints(projectId: string): Promise<Sprint[]> {
	return fetchEnvelope<{ items: Sprint[] }>(
		`/projects/${encodeURIComponent(projectId)}/sprints`,
	).then((d) => d.items ?? []);
}

export function listTaskStatuses(projectId: string): Promise<TaskStatus[]> {
	return fetchEnvelope<{ items: TaskStatus[] }>(
		`/projects/${encodeURIComponent(projectId)}/task-statuses`,
	).then((d) => d.items ?? []);
}

/**
 * Fetch ALL tasks of the project in page_size=100 batches, following the
 * cursor until the API stops returning one. Analytics needs the full task
 * set (all sprints + backlog), so no filters are applied server-side.
 */
export async function listAllTasks(projectId: string): Promise<Task[]> {
	const all: Task[] = [];
	let cursor: string | null = null;

	for (let page = 0; page < TASKS_MAX_PAGES; page++) {
		const params = new URLSearchParams({ page_size: String(TASKS_PAGE_SIZE) });
		if (cursor) params.set("cursor", cursor);
		const result: TaskListResult = await fetchEnvelope<TaskListResult>(
			`/projects/${encodeURIComponent(projectId)}/tasks?${params.toString()}`,
		);
		const items = result.items ?? [];
		all.push(...items);
		cursor = result.next_cursor ?? null;
		// Belt and braces: stop on missing cursor OR a short page, so a server
		// that echoes a stale cursor can never loop us.
		if (!cursor || items.length === 0) break;
	}
	return all;
}

// ── 60s in-memory cache (module-level: shared by every panel + surface) ──────

interface CacheEntry {
	at: number;
	promise: Promise<ProjectAnalyticsData>;
}

const cache = new Map<string, CacheEntry>();

/**
 * Fetch sprints + statuses + all tasks for a project, memoized for 60s.
 *
 * The entry stores the in-flight promise, so concurrent mounts (e.g. the
 * board view and the project page both rendering) share ONE network round
 * trip. A failed fetch evicts itself so the next caller retries instead of
 * being stuck with a cached rejection for the rest of the TTL.
 */
export function getProjectAnalyticsData(
	projectId: string,
	opts?: { force?: boolean },
): Promise<ProjectAnalyticsData> {
	const now = Date.now();
	const hit = cache.get(projectId);
	if (!opts?.force && hit && now - hit.at < CACHE_TTL_MS) {
		return hit.promise;
	}

	const promise = Promise.all([
		listSprints(projectId),
		listTaskStatuses(projectId),
		listAllTasks(projectId),
	]).then(
		([sprints, statuses, tasks]): ProjectAnalyticsData => ({
			sprints,
			statuses,
			tasks,
			fetchedAt: Date.now(),
		}),
	);

	cache.set(projectId, { at: now, promise });
	promise.catch(() => {
		// Only evict if this promise is still the cached one.
		const cur = cache.get(projectId);
		if (cur && cur.promise === promise) cache.delete(projectId);
	});
	return promise;
}
