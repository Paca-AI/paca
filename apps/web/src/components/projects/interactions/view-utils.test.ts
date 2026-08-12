// Regression tests for the "load more" pagination / task-detail-modal fixes
// in interaction-layout.tsx (see the fix's commit for the full root-cause
// write-up). These exercise the extracted pure logic directly rather than
// mounting the full InteractionLayout component, which requires a large
// amount of router/query/plugin-registry context.

import { describe, expect, it } from "vitest";
import type {
	ListTasksOptions,
	Task,
	TaskListResult,
} from "@/lib/interaction-api";
import {
	keepPreviousDataOnPageSizeChangeOnly,
	resolveSelectedTask,
	shouldClearColumnExtras,
} from "./view-utils";

const PROJECT_ID = "proj-1";

const makeTask = (overrides: Partial<Task> = {}): Task => ({
	id: "task-1",
	project_id: PROJECT_ID,
	title: "Do the thing",
	task_number: 0,
	sprint_id: null,
	status_id: "status-todo",
	task_type_id: null,
	parent_task_id: null,
	description: null,
	importance: 0,
	assignee_ids: [],
	reporter_id: null,
	custom_fields: {},
	view_position: null,
	view_group_key: null,
	created_at: "2026-01-01T00:00:00Z",
	updated_at: "2026-01-01T00:00:00Z",
	...overrides,
});

const makeResult = (
	overrides: Partial<TaskListResult> = {},
): TaskListResult => ({
	items: [],
	page_size: 20,
	next_cursor: null,
	...overrides,
});

describe("shouldClearColumnExtras", () => {
	it("does not clear while the base query has no data yet", () => {
		expect(shouldClearColumnExtras(undefined, 35)).toBe(false);
	});

	it("does not clear while the base query hasn't caught up and more pages remain", () => {
		const data = makeResult({
			items: [makeTask(), makeTask({ id: "task-2" })],
			next_cursor: "cursor-1",
		});
		expect(shouldClearColumnExtras(data, 35)).toBe(false);
	});

	it("clears once the base query has reached the expected depth", () => {
		const items = Array.from({ length: 35 }, (_, i) =>
			makeTask({ id: `task-${i}` }),
		);
		const data = makeResult({ items, next_cursor: "cursor-more" });
		expect(shouldClearColumnExtras(data, 35)).toBe(true);
	});

	it("clears when the base query is exhausted even if shorter than expected depth (regression: deleted/moved task)", () => {
		// A column expanded to depth 35 via "load more", then one of those
		// tasks got deleted (or moved out of the column) by someone else —
		// the true total is now 34, which the base query can never re-reach.
		// The base query reporting `next_cursor: null` (nothing left to
		// fetch) must be treated as authoritative so extras don't linger
		// forever holding the deleted/moved task.
		const items = Array.from({ length: 34 }, (_, i) =>
			makeTask({ id: `task-${i}` }),
		);
		const data = makeResult({ items, next_cursor: null });
		expect(shouldClearColumnExtras(data, 35)).toBe(true);
	});

	it("treats an undefined next_cursor the same as null (exhausted)", () => {
		const items = [makeTask()];
		const data: Pick<TaskListResult, "items" | "next_cursor"> = { items };
		expect(shouldClearColumnExtras(data, 35)).toBe(true);
	});
});

describe("resolveSelectedTask", () => {
	it("resolves to null and clears the cache when nothing is selected", () => {
		const cached = makeTask({ id: "task-cached" });
		expect(resolveSelectedTask([], null, cached)).toEqual({
			resolved: null,
			nextLastKnown: null,
		});
	});

	it("resolves the task straight from the list and caches it when present", () => {
		const task = makeTask({ id: "task-1", title: "Fresh title" });
		const result = resolveSelectedTask([task], "task-1", null);
		expect(result.resolved).toBe(task);
		expect(result.nextLastKnown).toBe(task);
	});

	it("falls back to the cached task when the selection transiently drops out of the list (regression: modal flash-close)", () => {
		// Simulates the reported bug: the task detail dialog is open for
		// "task-1", a background refetch (triggered by the dialog itself
		// re-observing sprints/customFields) briefly empties `tasks` before
		// the expanded page re-resolves.
		const cached = makeTask({ id: "task-1", title: "Cached title" });
		const result = resolveSelectedTask([], "task-1", cached);
		expect(result.resolved).toBe(cached);
		// Untouched — still available if the next lookup is also a miss.
		expect(result.nextLastKnown).toBe(cached);
	});

	it("does not fall back to a cached task belonging to a different id", () => {
		const cached = makeTask({ id: "task-other" });
		const result = resolveSelectedTask([], "task-1", cached);
		expect(result.resolved).toBeNull();
		// The stale cache for the other id is left untouched rather than
		// wiped, so it can still resolve if that id is re-selected later.
		expect(result.nextLastKnown).toBe(cached);
	});

	it("prefers a freshly-found task over the cached one for the same id", () => {
		const cached = makeTask({ id: "task-1", title: "Stale title" });
		const fresh = makeTask({ id: "task-1", title: "Fresh title" });
		const result = resolveSelectedTask([fresh], "task-1", cached);
		expect(result.resolved).toBe(fresh);
		expect(result.nextLastKnown).toBe(fresh);
	});
});

describe("keepPreviousDataOnPageSizeChangeOnly", () => {
	const prevData = makeResult({ items: [makeTask()] });

	it("returns undefined when there is no previous query (first-ever fetch)", () => {
		const placeholderFn = keepPreviousDataOnPageSizeChangeOnly({
			pageSize: 20,
		});
		expect(placeholderFn(undefined, undefined)).toBeUndefined();
	});

	it("returns undefined when the previous query never resolved (no data to reuse)", () => {
		const placeholderFn = keepPreviousDataOnPageSizeChangeOnly({
			pageSize: 20,
		});
		const previousQuery = { queryKey: ["col", { pageSize: 5 }] as const };
		expect(placeholderFn(undefined, previousQuery)).toBeUndefined();
	});

	it("keeps the previous page's data when only pageSize changed (regression: 'load more')", () => {
		const opts: ListTasksOptions = { statusIds: ["status-todo"], pageSize: 5 };
		const grownOpts: ListTasksOptions = { ...opts, pageSize: 20 };
		const placeholderFn = keepPreviousDataOnPageSizeChangeOnly(grownOpts);
		const previousQuery = { queryKey: ["col", opts] as const };
		expect(placeholderFn(prevData, previousQuery)).toBe(prevData);
	});

	it("drops previous data when a filter changed alongside pageSize (regression: stale filter results)", () => {
		// Reported issue: because these queries key on the full options object,
		// blindly reusing previous data across *any* key change (the stock
		// `keepPreviousData` helper) would keep showing a stale, non-matching
		// result set when a user changes a filter — with no loading indicator.
		const opts: ListTasksOptions = { statusIds: ["status-todo"], pageSize: 5 };
		const filterChanged: ListTasksOptions = {
			statusIds: ["status-done"],
			pageSize: 5,
		};
		const placeholderFn = keepPreviousDataOnPageSizeChangeOnly(filterChanged);
		const previousQuery = { queryKey: ["col", opts] as const };
		expect(placeholderFn(prevData, previousQuery)).toBeUndefined();
	});

	it("drops previous data when the search term changed", () => {
		const opts: ListTasksOptions = { search: "foo", pageSize: 5 };
		const searchChanged: ListTasksOptions = { search: "bar", pageSize: 5 };
		const placeholderFn = keepPreviousDataOnPageSizeChangeOnly(searchChanged);
		const previousQuery = { queryKey: ["col", opts] as const };
		expect(placeholderFn(prevData, previousQuery)).toBeUndefined();
	});
});
