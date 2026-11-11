// Patches the React Query cache from a change instead of refetching.
//
// A change delivered by the sync protocol may carry the entity as it is after
// the change (see events.Event.Snapshot in the API). Where it does, the cache
// is updated in place and only what the entity cannot answer is refetched:
//
//   - the entity's own query and every cached list that already contains it
//     are updated in place, so a card or a row changes at once;
//   - a list is refetched only when the change could move the entity in or out
//     of it (a field a list can filter or group on changed, or the entity was
//     created or deleted). Lists are filtered on the server, so the client
//     cannot tell on its own.
//
// A change without a snapshot (an automation, a worker, an older server) is
// "unhandled": the caller falls back to invalidating, as before.

import type { QueryClient, QueryKey } from "@tanstack/react-query";
import type { RealtimeEvent } from "./socket-client";

export type PatchResult = "patched" | "unhandled";

// The fields of a task update that a list can filter, group or sort on, so
// the task may now belong elsewhere in it, or to another list altogether.
// Names are those of the API's task.updated diff. Only a title (or an edit
// that records no field) leaves every list as it was.
const TASK_MEMBERSHIP_FIELDS = new Set([
	"importance",
	"story_points",
	"start_date",
	"due_date",
	"status",
	"task_type",
	"sprint",
	"parent",
	"parent_task",
	"assignee",
	"assignment_mode",
	"reporter",
	"tags",
	"custom_fields",
]);

const UUID_RE =
	/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

// A query that holds lists of tasks (or counts of them): task lists keyed by
// their options, the per-column lists and the column definitions of a board,
// epics, pickers, a parent's subtasks and children, a sprint's tasks and their
// counts, and the signed-in user's assigned tasks.
//
// Under ["projects", p, "tasks"] a key is a single task's own (a uuid) or
// something that hangs off it (links, attachments, its timeline) unless it is
// a parent's subtasks/children; anything else there (an options object or a
// word such as "col", "columns", "epics", "picker") is a list. Deciding by
// what a task's own key looks like, rather than by listing the words, keeps a
// list added later from being missed.
export function isTaskListKey(key: QueryKey, projectId: string): boolean {
	if (key[0] === "users" && key[1] === "me" && key[2] === "tasks") return true;
	if (key[0] !== "projects" || key[1] !== projectId) return false;
	if (key[2] === "sprints") return key[4] === "tasks";
	if (key[2] !== "tasks") return false;
	const third = key[3];
	if (third === undefined || third === null) return false;
	if (typeof third === "string" && UUID_RE.test(third)) {
		return key[4] === "subtasks" || key[4] === "children";
	}
	return true;
}

function changedFields(payload: Record<string, unknown>): string[] | null {
	const content = payload.content;
	if (typeof content !== "string") return null;
	try {
		const parsed = JSON.parse(content) as {
			changes?: Array<{ field?: unknown }>;
		};
		if (!Array.isArray(parsed.changes)) return null;
		return parsed.changes.flatMap((c) =>
			typeof c.field === "string" ? [c.field] : [],
		);
	} catch {
		return null;
	}
}

// What the snapshot may overwrite: everything but the description (the
// snapshot omits it; collaborative editing owns it) and the embedded people
// (lists embed them, a snapshot does not).
function mergeTask(
	existing: Record<string, unknown>,
	snapshot: Record<string, unknown>,
): Record<string, unknown> {
	const { description: _d, assignees: _a, reporter: _r, ...fields } = snapshot;
	const next = { ...existing, ...fields };
	// The embedded people follow their ids: drop them when the ids changed.
	if (
		JSON.stringify(existing.assignee_ids) !==
		JSON.stringify(fields.assignee_ids)
	) {
		delete next.assignees;
	}
	if (existing.reporter_id !== fields.reporter_id) delete next.reporter;
	return next;
}

function isRecord(v: unknown): v is Record<string, unknown> {
	return !!v && typeof v === "object" && !Array.isArray(v);
}

// mapRows rewrites every row (an object with a string id) of a cached list value (a result with
// items, an infinite query's pages, or a bare array) with `fn`, returning the
// same object when nothing changed so React Query keeps the reference.
function mapRows(
	data: unknown,
	fn: (task: Record<string, unknown>) => Record<string, unknown> | null,
): unknown {
	const mapList = (items: unknown[]): unknown[] => {
		let changed = false;
		const out: unknown[] = [];
		for (const item of items) {
			if (!isRecord(item) || typeof item.id !== "string") {
				out.push(item);
				continue;
			}
			const next = fn(item);
			if (next === item) {
				out.push(item);
				continue;
			}
			changed = true;
			if (next !== null) out.push(next);
		}
		return changed ? out : items;
	};
	if (Array.isArray(data)) return mapList(data);
	if (!isRecord(data)) return data;
	if (Array.isArray(data.items)) {
		const items = mapList(data.items);
		return items === data.items ? data : { ...data, items };
	}
	if (Array.isArray(data.pages)) {
		let changed = false;
		const pages = data.pages.map((page: unknown) => {
			const next = mapRows(page, fn);
			if (next !== page) changed = true;
			return next;
		});
		return changed ? { ...data, pages } : data;
	}
	return data;
}

/**
 * Refetches what hangs off one task, without touching its own query or any
 * list. `segments` names which parts ("activities", "links", "attachments",
 * "subtasks", ...): a comment only changes the timeline, so refreshing the
 * rest would be requests for nothing. Omitted, all of them.
 */
export function refreshTaskSubQueries(
	queryClient: QueryClient,
	projectId: string,
	taskId: string,
	segments?: readonly string[],
): void {
	void queryClient.invalidateQueries({
		predicate: (q) =>
			q.queryKey[0] === "projects" &&
			q.queryKey[1] === projectId &&
			q.queryKey[2] === "tasks" &&
			q.queryKey[3] === taskId &&
			q.queryKey.length > 4 &&
			(segments === undefined || segments.includes(String(q.queryKey[4]))),
	});
}

/** Applies a task.created / task.updated / task.deleted change. */
export function applyTaskChange(
	queryClient: QueryClient,
	projectId: string,
	event: RealtimeEvent,
): PatchResult {
	const { change } = event;
	if (!change || change.entityType !== "task") return "unhandled";
	const taskId = change.taskId ?? change.entityId;
	if (!taskId) return "unhandled";
	const detailKey = ["projects", projectId, "tasks", taskId] as const;
	const lists = {
		predicate: (q: { queryKey: QueryKey }) =>
			isTaskListKey(q.queryKey, projectId),
	};
	const refetchLists = () =>
		void queryClient.invalidateQueries({ ...lists, refetchType: "active" });
	if (event.type === "task.deleted") {
		queryClient.removeQueries({ queryKey: detailKey, exact: true });
		queryClient.setQueriesData(lists, (data: unknown) =>
			mapRows(data, (t) => (t.id === taskId ? null : t)),
		);
		// Totals, paging cursors and parents' subtask counts.
		refetchLists();
		return "patched";
	}

	const entity = change.entity;
	if (!isRecord(entity) || entity.id !== taskId) return "unhandled";

	if (event.type === "task.created") {
		queryClient.setQueryData(detailKey, (old: unknown) =>
			isRecord(old) ? mergeTask(old, entity) : entity,
		);
		// A new task may belong to any list.
		refetchLists();
		return "patched";
	}

	if (event.type === "task.updated") {
		queryClient.setQueryData(detailKey, (old: unknown) =>
			isRecord(old) ? mergeTask(old, entity) : old,
		);
		queryClient.setQueriesData(lists, (data: unknown) =>
			mapRows(data, (t) => (t.id === taskId ? mergeTask(t, entity) : t)),
		);
		// An update adds an entry to the task's timeline; the rest of what hangs
		// off it (links, attachments, subtasks) is unchanged, and a changed child
		// is patched in the subtask lists above.
		refreshTaskSubQueries(queryClient, projectId, taskId, ["activities"]);
		const fields = changedFields(event.payload);
		// Unknown fields (no diff in the payload) are treated as moving the task.
		if (!fields || fields.some((f) => TASK_MEMBERSHIP_FIELDS.has(f))) {
			refetchLists();
		}
		return "patched";
	}

	return "unhandled";
}

// ── Sprints ──────────────────────────────────────────────────────────────────

// A sprint list is cursor-paged, filtered by status, searched and sorted by
// name: these are the fields that can move a sprint in or out of one.
const SPRINT_LIST_FIELDS = [
	"name",
	"status",
	"start_date",
	"end_date",
] as const;

export function isSprintListKey(key: QueryKey, projectId: string): boolean {
	return (
		key[0] === "projects" &&
		key[1] === projectId &&
		key[2] === "sprints" &&
		key[3] === "list"
	);
}

/** Applies a sprint.created / updated / completed / deleted change. */
export function applySprintChange(
	queryClient: QueryClient,
	projectId: string,
	event: RealtimeEvent,
): PatchResult {
	const { change } = event;
	if (!change || change.entityType !== "sprint") return "unhandled";
	const sprintId = change.entityId;
	if (!sprintId) return "unhandled";
	const detailKey = ["projects", projectId, "sprints", sprintId] as const;
	const lists = {
		predicate: (q: { queryKey: QueryKey }) =>
			isSprintListKey(q.queryKey, projectId),
	};
	const refetchLists = () =>
		void queryClient.invalidateQueries({ ...lists, refetchType: "active" });

	if (event.type === "sprint.deleted") {
		queryClient.removeQueries({ queryKey: detailKey, exact: true });
		queryClient.setQueriesData(lists, (data: unknown) =>
			mapRows(data, (r) => (r.id === sprintId ? null : r)),
		);
		refetchLists();
		return "patched";
	}

	const entity = change.entity;
	if (!isRecord(entity) || entity.id !== sprintId) return "unhandled";

	if (event.type === "sprint.created") {
		queryClient.setQueryData(detailKey, (old: unknown) =>
			isRecord(old) ? { ...old, ...entity } : entity,
		);
		refetchLists();
		return "patched";
	}

	if (event.type === "sprint.updated" || event.type === "sprint.completed") {
		queryClient.setQueryData(detailKey, (old: unknown) =>
			isRecord(old) ? { ...old, ...entity } : old,
		);
		let found = false;
		let moved = false;
		queryClient.setQueriesData(lists, (data: unknown) =>
			mapRows(data, (r) => {
				if (r.id !== sprintId) return r;
				found = true;
				if (SPRINT_LIST_FIELDS.some((f) => r[f] !== entity[f])) moved = true;
				return { ...r, ...entity };
			}),
		);
		// A sprint in no cached list may belong in one; one whose filter fields
		// changed may have to leave or move.
		if (!found || moved) refetchLists();
		return "patched";
	}

	return "unhandled";
}

// ── Environments ─────────────────────────────────────────────────────────────

/**
 * Applies environment.status_changed. The event already says what changed
 * (the new status and any error), so the environment's query and its row in
 * every cached list are updated in place. A transition can also move other
 * fields (a port, the last activity), so the one environment being viewed is
 * refetched; no list is.
 */
export function applyEnvironmentStatus(
	queryClient: QueryClient,
	projectId: string,
	payload: Record<string, unknown>,
): PatchResult {
	const id = payload.environment_id;
	const status = payload.status;
	if (typeof id !== "string" || !id || typeof status !== "string") {
		return "unhandled";
	}
	const fields = {
		status,
		error_message:
			typeof payload.error_message === "string" ? payload.error_message : null,
	};
	const detailKey = ["projects", projectId, "environments", id] as const;
	queryClient.setQueryData(detailKey, (old: unknown) =>
		isRecord(old) ? { ...old, ...fields } : old,
	);
	queryClient.setQueriesData(
		{
			predicate: (q) =>
				q.queryKey[0] === "projects" &&
				q.queryKey[1] === projectId &&
				q.queryKey[2] === "environments" &&
				q.queryKey[3] === "list",
		},
		(data: unknown) =>
			mapRows(data, (r) => (r.id === id ? { ...r, ...fields } : r)),
	);
	void queryClient.invalidateQueries({
		queryKey: detailKey,
		exact: true,
		refetchType: "active",
	});
	return "patched";
}
