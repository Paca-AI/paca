// Helpers for the sync-protocol specs (tests/realtime/sync-*.spec.ts): API
// calls that change a project the way a user, an agent or a script would, and
// the assertions every spec about the feed shares.

import { type APIRequestContext, expect, test } from "@playwright/test";
import {
	API_URL,
	authRequest,
	cleanupProjectsByPrefix,
	cleanupUsersByPrefix,
	newRunId,
} from "./e2e-api";
import { cleanupEnvironmentsInProjectsByPrefix } from "./environments";
import {
	closeAll,
	cookieHeaderOf,
	type Probe,
	Probe as ProbeClass,
	type SyncChange,
	sleep,
	track,
} from "./realtime";

/**
 * Registers the hooks every sync spec needs: a clean slate of the spec's own
 * data before and after each test, and every socket closed. Returns `name`,
 * which gives a label a prefix and a run id no other spec shares.
 */
export function useSyncScaffold(prefix: string) {
	const runId = newRunId();
	const name = (label: string) => `${prefix}${label}_${runId}`;
	const cleanup = async (request: APIRequestContext) => {
		await cleanupEnvironmentsInProjectsByPrefix(request, prefix);
		await cleanupProjectsByPrefix(request, prefix);
		await cleanupUsersByPrefix(request, prefix);
	};
	test.beforeEach(async ({ request, context }) => {
		await authRequest(request);
		await cleanup(request);
		await context.clearCookies();
	});
	test.afterEach(async ({ request }) => {
		await closeAll();
		await authRequest(request);
		await cleanup(request);
	});
	return { name, runId };
}

/** A BlockNote document with one paragraph. */
export const blocks = (text: string) => [
	{
		type: "paragraph",
		content: [{ type: "text", text, styles: {} }],
	},
];

/** Fails the spec with the server's answer unless the request succeeded. */
export async function data<T = Record<string, unknown>>(
	response: Awaited<ReturnType<APIRequestContext["get"]>>,
): Promise<T> {
	const text = await response.text();
	expect(response.ok(), text).toBeTruthy();
	// A delete may answer with no body at all.
	if (!text) return {} as T;
	return (JSON.parse(text) as { data: T }).data;
}

/** Calls that change a project, as whoever `request` is signed in as. */
export function changes(request: APIRequestContext, projectId: string) {
	const base = `${API_URL}/projects/${projectId}`;
	return {
		patchTask: async (taskId: string, body: Record<string, unknown>) =>
			data(await request.patch(`${base}/tasks/${taskId}`, { data: body })),
		deleteTask: async (taskId: string) =>
			data(await request.delete(`${base}/tasks/${taskId}`)),
		comment: async (taskId: string, text: string) =>
			data<{ id: string }>(
				await request.post(`${base}/tasks/${taskId}/activities/comments`, {
					data: { content: blocks(text) },
				}),
			),
		editComment: async (taskId: string, commentId: string, text: string) =>
			data(
				await request.patch(
					`${base}/tasks/${taskId}/activities/comments/${commentId}`,
					{ data: { content: blocks(text) } },
				),
			),
		deleteComment: async (taskId: string, commentId: string) =>
			data(
				await request.delete(
					`${base}/tasks/${taskId}/activities/comments/${commentId}`,
				),
			),
		link: async (taskId: string, targetTaskId: string) =>
			data<{ id: string }>(
				await request.post(`${base}/tasks/${taskId}/links`, {
					data: { target_task_id: targetTaskId, link_type: "relates_to" },
				}),
			),
		unlink: async (taskId: string, linkId: string) =>
			data(await request.delete(`${base}/tasks/${taskId}/links/${linkId}`)),
		patchSprint: async (sprintId: string, body: Record<string, unknown>) =>
			data(await request.patch(`${base}/sprints/${sprintId}`, { data: body })),
		completeSprint: async (sprintId: string) =>
			data(
				await request.post(`${base}/sprints/${sprintId}/complete`, {
					data: {},
				}),
			),
		deleteSprint: async (sprintId: string) =>
			data(await request.delete(`${base}/sprints/${sprintId}`)),
		createView: async (name: string, context = "backlog") =>
			data<{ id: string }>(
				await request.post(`${base}/views?context=${context}`, {
					data: { name, view_type: "table" },
				}),
			),
		patchView: async (viewId: string, body: Record<string, unknown>) =>
			data(await request.patch(`${base}/views/${viewId}`, { data: body })),
		deleteView: async (viewId: string) =>
			data(await request.delete(`${base}/views/${viewId}`)),
		reorderViews: async (viewIds: string[], context = "backlog") =>
			data(
				await request.put(`${base}/views/positions?context=${context}`, {
					data: { view_ids: viewIds },
				}),
			),
		moveTask: async (viewId: string, taskId: string, position: number) =>
			data(
				await request.put(`${base}/views/${viewId}/task-positions/${taskId}`, {
					data: { position },
				}),
			),
		createStatus: async (name: string, category = "todo") =>
			data<{ id: string }>(
				await request.post(`${base}/task-statuses`, {
					data: { name, category, position: 50 },
				}),
			),
		patchStatus: async (id: string, body: Record<string, unknown>) =>
			data(await request.patch(`${base}/task-statuses/${id}`, { data: body })),
		deleteStatus: async (id: string) =>
			data(await request.delete(`${base}/task-statuses/${id}`)),
		createType: async (name: string) =>
			data<{ id: string }>(
				await request.post(`${base}/task-types`, { data: { name } }),
			),
		patchType: async (id: string, body: Record<string, unknown>) =>
			data(await request.patch(`${base}/task-types/${id}`, { data: body })),
		deleteType: async (id: string) =>
			data(await request.delete(`${base}/task-types/${id}`)),
		createField: async (key: string, display: string, type = "text") =>
			data<{ id: string }>(
				await request.post(`${base}/custom-fields`, {
					data: { field_key: key, display_name: display, field_type: type },
				}),
			),
		patchField: async (id: string, body: Record<string, unknown>) =>
			data(await request.patch(`${base}/custom-fields/${id}`, { data: body })),
		deleteField: async (id: string) =>
			data(await request.delete(`${base}/custom-fields/${id}`)),
		patchProject: async (body: Record<string, unknown>) =>
			data(await request.patch(base, { data: body })),
		patchDoc: async (docId: string, body: Record<string, unknown>) =>
			data(await request.patch(`${base}/docs/${docId}`, { data: body })),
		deleteDoc: async (docId: string) =>
			data(await request.delete(`${base}/docs/${docId}`)),
		docComment: async (docId: string, text: string) =>
			data<{ id: string }>(
				await request.post(`${base}/docs/${docId}/comments`, {
					data: { content: blocks(text) },
				}),
			),
		patchFolder: async (folderId: string, body: Record<string, unknown>) =>
			data(
				await request.patch(`${base}/docs/folders/${folderId}`, { data: body }),
			),
		deleteFolder: async (folderId: string) =>
			data(await request.delete(`${base}/docs/folders/${folderId}`)),
	};
}

// ─── Probes ──────────────────────────────────────────────────────────────────

/** A signed-in socket (the admin of `request`) that joined `projectId` with sync. */
export async function syncProbe(
	request: APIRequestContext,
	projectId: string,
	cursor: number | null = null,
	epoch: number | null = null,
) {
	const probe = track(
		await ProbeClass.connect({ cookie: await cookieHeaderOf(request) }),
	);
	const ack = await probe.syncJoin(projectId, cursor, epoch);
	return { probe, ack };
}

/** Polls until a change satisfying `match` arrived; returns it. */
export async function waitForChange(
	probe: Probe,
	match: (c: SyncChange) => boolean,
	since = 0,
	timeoutMs = 10_000,
): Promise<SyncChange> {
	const deadline = Date.now() + timeoutMs;
	for (;;) {
		const found = probe.syncedChanges(since).find(match);
		if (found) return found;
		if (Date.now() > deadline) {
			const seen = probe
				.syncedChanges(since)
				.map((c) => c.type)
				.join(", ");
			throw new Error(
				`timed out waiting for a change (saw: ${seen || "none"})`,
			);
		}
		await sleep(50);
	}
}

/** Waits for a change of `type` (optionally about `entityId`). */
export function waitForType(
	probe: Probe,
	type: string,
	opts: { entityId?: string; since?: number; timeoutMs?: number } = {},
) {
	return waitForChange(
		probe,
		(c) =>
			c.type === type &&
			(opts.entityId === undefined ||
				c.entity_id === opts.entityId ||
				c.entity?.id === opts.entityId ||
				c.payload?.task_id === opts.entityId),
		opts.since ?? 0,
		opts.timeoutMs,
	);
}

/** Every change (skips included) in arrival order. */
export function allChanges(probe: Probe, since = 0): SyncChange[] {
	return probe.batches.filter((b) => b.at >= since).flatMap((b) => b.changes);
}

/**
 * What a socket's batches must satisfy whatever happened: each starts where the
 * previous one ended, and covers from+1..to with no hole and no repeat.
 * Returns the problems found (empty when sound).
 */
export function chainProblems(probe: Probe, startAt: number): string[] {
	const problems: string[] = [];
	let cursor = startAt;
	for (const [i, b] of probe.batches.entries()) {
		if (b.from !== cursor) {
			problems.push(`batch ${i} starts at ${b.from}, expected ${cursor}`);
		}
		const seqs = b.changes.map((c) => c.seq);
		const want = Array.from(
			{ length: b.to - b.from },
			(_, k) => b.from + k + 1,
		);
		if (JSON.stringify(seqs) !== JSON.stringify(want)) {
			problems.push(
				`batch ${i} holds ${seqs.join(",")}, expected ${want.join(",")}`,
			);
		}
		cursor = b.to;
	}
	return problems;
}

/** Waits for the socket's cursor (the last batch's `to`) to reach `seq`. */
export async function waitForCursor(
	probe: Probe,
	seq: number,
	timeoutMs = 20_000,
) {
	await expect
		.poll(() => probe.batches.at(-1)?.to ?? 0, { timeout: timeoutMs })
		.toBeGreaterThanOrEqual(seq);
}

/** Runs `fn(i)` for i in [0, n) with at most `limit` in flight. */
export async function inParallel(
	n: number,
	limit: number,
	fn: (i: number) => Promise<unknown>,
) {
	let next = 0;
	await Promise.all(
		Array.from({ length: limit }, async () => {
			for (let i = next++; i < n; i = next++) await fn(i);
		}),
	);
}
