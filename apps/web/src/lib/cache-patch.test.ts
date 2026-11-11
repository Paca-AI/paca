import { QueryClient } from "@tanstack/react-query";
import { beforeEach, describe, expect, it } from "vitest";
import {
	applyEnvironmentStatus,
	applySprintChange,
	applyTaskChange,
	isSprintListKey,
	isTaskListKey,
} from "./cache-patch";
import type { RealtimeEvent } from "./socket-client";

const P = "p1";
// A task's own query key carries its uuid; the list-key rule relies on it.
const T1 = "9c18da4b-18b9-4d66-8d82-7a65202b91a4";
const T2 = "0a4fe3b2-6f43-4a07-9f6a-2d1f0c6a8b11";
const task = (over: Record<string, unknown> = {}) => ({
	id: T1,
	title: "Old",
	status_id: "s1",
	assignee_ids: ["m1"],
	assignees: [{ id: "m1" }],
	description: [{ type: "paragraph" }],
	...over,
});

function event(
	type: string,
	entity: Record<string, unknown> | undefined,
	fields: string[] = [],
): RealtimeEvent {
	return {
		type,
		payload: {
			task_id: T1,
			content: JSON.stringify({ changes: fields.map((field) => ({ field })) }),
		},
		change: { entityType: "task", entityId: T1, taskId: T1, entity },
	};
}

let qc: QueryClient;
type InvalidateArg = {
	refetchType?: string;
	predicate?: (q: { queryKey: unknown[] }) => boolean;
};
let calls: InvalidateArg[] = [];

beforeEach(() => {
	qc = new QueryClient();
	calls = [];
	qc.invalidateQueries = ((arg: InvalidateArg) => {
		calls.push(arg);
		return Promise.resolve();
	}) as unknown as QueryClient["invalidateQueries"];
});

const listsInvalidated = () => calls.some((c) => c.refetchType === "active");

describe("isTaskListKey", () => {
	it("recognises task lists and counts but not a task's own query", () => {
		expect(isTaskListKey(["projects", P, "tasks", { sprintId: "s" }], P)).toBe(
			true,
		);
		expect(isTaskListKey(["projects", P, "tasks", "epics", "e"], P)).toBe(true);
		expect(isTaskListKey(["projects", P, "tasks", T1, "subtasks"], P)).toBe(
			true,
		);
		expect(
			isTaskListKey(["projects", P, "sprints", "s", "tasks", "count", null], P),
		).toBe(true);
		expect(isTaskListKey(["users", "me", "tasks"], P)).toBe(true);
		expect(isTaskListKey(["projects", P, "tasks", "col", "todo", {}], P)).toBe(
			true,
		);
		expect(
			isTaskListKey(["projects", P, "tasks", "columns", "status", {}], P),
		).toBe(true);
		expect(isTaskListKey(["projects", P, "tasks", "link-picker", "q"], P)).toBe(
			true,
		);
		expect(isTaskListKey(["projects", P, "tasks"], P)).toBe(false);
		expect(isTaskListKey(["projects", P, "tasks", T1, "activities"], P)).toBe(
			false,
		);
		expect(isTaskListKey(["projects", P, "tasks", T1], P)).toBe(false);
		expect(isTaskListKey(["projects", P, "tasks", T1, "links"], P)).toBe(false);
		expect(isTaskListKey(["projects", "other", "tasks", {}], P)).toBe(false);
	});
});

describe("applyTaskChange: task.updated", () => {
	it("patches the detail and every list row in place, keeping the description", () => {
		qc.setQueryData(["projects", P, "tasks", T1], task());
		qc.setQueryData(["projects", P, "tasks", { sprintId: "s" }], {
			items: [task(), task({ id: T2 })],
		});
		qc.setQueryData(["users", "me", "tasks"], {
			pages: [{ items: [task()] }],
			pageParams: [undefined],
		});

		const res = applyTaskChange(
			qc,
			P,
			event("task.updated", { id: T1, title: "New", assignee_ids: ["m1"] }, [
				"title",
			]),
		);

		expect(res).toBe("patched");
		const detail = qc.getQueryData<Record<string, unknown>>([
			"projects",
			P,
			"tasks",
			T1,
		]);
		expect(detail?.title).toBe("New");
		expect(detail?.description).toEqual([{ type: "paragraph" }]);
		const list = qc.getQueryData<{ items: Array<Record<string, unknown>> }>([
			"projects",
			P,
			"tasks",
			{ sprintId: "s" },
		]);
		expect(list?.items.map((t) => t.title)).toEqual(["New", "Old"]);
		const mine = qc.getQueryData<{
			pages: Array<{ items: Array<Record<string, unknown>> }>;
		}>(["users", "me", "tasks"]);
		expect(mine?.pages[0]?.items[0]?.title).toBe("New");
	});

	it("does not refetch any list for a title-only change", () => {
		qc.setQueryData(["projects", P, "tasks", T1], task());
		applyTaskChange(
			qc,
			P,
			event("task.updated", { id: T1, title: "New" }, ["title"]),
		);
		expect(listsInvalidated()).toBe(false);
	});

	it.each([
		"status",
		"assignee",
		"sprint",
		"due_date",
		"importance",
		"tags",
	])("refetches the active lists when %s changes, since a list may filter or sort on it", (field) => {
		applyTaskChange(qc, P, event("task.updated", { id: T1 }, [field]));
		expect(listsInvalidated()).toBe(true);
	});

	it("refetches the lists when the payload carries no diff", () => {
		applyTaskChange(qc, P, {
			type: "task.updated",
			payload: { task_id: T1 },
			change: {
				entityType: "task",
				entityId: T1,
				taskId: T1,
				entity: { id: T1 },
			},
		});
		expect(listsInvalidated()).toBe(true);
	});

	it("drops the embedded people when the ids they belong to changed", () => {
		qc.setQueryData(["projects", P, "tasks", { a: 1 }], { items: [task()] });
		applyTaskChange(
			qc,
			P,
			event("task.updated", { id: T1, assignee_ids: ["m2"] }, ["assignee"]),
		);
		const row = qc.getQueryData<{ items: Array<Record<string, unknown>> }>([
			"projects",
			P,
			"tasks",
			{ a: 1 },
		])?.items[0];
		expect(row?.assignee_ids).toEqual(["m2"]);
		expect(row?.assignees).toBeUndefined();
	});

	it("does not create a detail query nobody asked for", () => {
		applyTaskChange(
			qc,
			P,
			event("task.updated", { id: T1, title: "New" }, ["title"]),
		);
		expect(qc.getQueryData(["projects", P, "tasks", T1])).toBeUndefined();
	});

	it("refreshes only the task's timeline after an update, not its links, attachments or subtasks", () => {
		applyTaskChange(qc, P, event("task.updated", { id: T1 }, ["title"]));
		const keyRefreshed = (...key: unknown[]) =>
			calls.some((c) => c.predicate?.({ queryKey: key }));
		expect(keyRefreshed("projects", P, "tasks", T1, "activities")).toBe(true);
		expect(keyRefreshed("projects", P, "tasks", T1, "links")).toBe(false);
		expect(keyRefreshed("projects", P, "tasks", T1, "attachments")).toBe(false);
		expect(keyRefreshed("projects", P, "tasks", T1, "subtasks")).toBe(false);
	});

	it("refreshes the task's own timeline and links, but not its detail", () => {
		applyTaskChange(qc, P, event("task.updated", { id: T1 }, ["title"]));
		const predicate = calls
			.map((c) => c.predicate)
			.find((p) =>
				p?.({ queryKey: ["projects", P, "tasks", T1, "activities"] }),
			);
		expect(predicate).toBeDefined();
		expect(predicate?.({ queryKey: ["projects", P, "tasks", T1] })).toBe(false);
	});
});

describe("applyTaskChange: created and deleted", () => {
	it("seeds the new task's detail and refetches the lists it may belong to", () => {
		expect(
			applyTaskChange(qc, P, event("task.created", { id: T1, title: "Hi" })),
		).toBe("patched");
		expect(qc.getQueryData(["projects", P, "tasks", T1])).toMatchObject({
			title: "Hi",
		});
		expect(listsInvalidated()).toBe(true);
	});

	it("removes a deleted task from the detail and from every list", () => {
		qc.setQueryData(["projects", P, "tasks", T1], task());
		qc.setQueryData(["projects", P, "tasks", { a: 1 }], {
			items: [task(), task({ id: T2 })],
		});
		expect(applyTaskChange(qc, P, event("task.deleted", undefined))).toBe(
			"patched",
		);
		expect(qc.getQueryData(["projects", P, "tasks", T1])).toBeUndefined();
		const list = qc.getQueryData<{ items: Array<{ id: string }> }>([
			"projects",
			P,
			"tasks",
			{ a: 1 },
		]);
		expect(list?.items.map((t) => t.id)).toEqual([T2]);
	});
});

describe("applyTaskChange: what it leaves to the caller", () => {
	it("is unhandled without a snapshot, for another entity, or for a legacy event", () => {
		expect(applyTaskChange(qc, P, event("task.updated", undefined))).toBe(
			"unhandled",
		);
		expect(applyTaskChange(qc, P, event("task.updated", { id: "other" }))).toBe(
			"unhandled",
		);
		expect(applyTaskChange(qc, P, { type: "task.updated", payload: {} })).toBe(
			"unhandled",
		);
		expect(
			applyTaskChange(qc, P, {
				type: "task.updated",
				payload: {},
				change: { entityType: "doc", entityId: "d", entity: { id: "d" } },
			}),
		).toBe("unhandled");
		expect(calls).toEqual([]);
	});
});

describe("applySprintChange", () => {
	const sprint = (over: Record<string, unknown> = {}) => ({
		id: "s1",
		name: "Sprint 1",
		status: "planned",
		goal: "g",
		...over,
	});
	const sprintEvent = (
		type: string,
		entity: Record<string, unknown> | undefined,
	): RealtimeEvent => ({
		type,
		payload: { sprint_id: "s1" },
		change: { entityType: "sprint", entityId: "s1", entity },
	});
	const listKey = ["projects", P, "sprints", "list", { sort: "name" }];

	it("recognises sprint lists only", () => {
		expect(isSprintListKey(listKey, P)).toBe(true);
		expect(isSprintListKey(["projects", P, "sprints", "s1"], P)).toBe(false);
		expect(isSprintListKey(["projects", P, "sprints", "s1", "tasks"], P)).toBe(
			false,
		);
	});

	it("patches the detail and the list row, and refetches nothing when no list field moved", () => {
		qc.setQueryData(["projects", P, "sprints", "s1"], sprint());
		qc.setQueryData(listKey, {
			pages: [{ items: [sprint()] }],
			pageParams: [undefined],
		});
		const res = applySprintChange(
			qc,
			P,
			sprintEvent("sprint.updated", sprint({ goal: "new goal" })),
		);
		expect(res).toBe("patched");
		expect(
			qc.getQueryData<{ goal: string }>(["projects", P, "sprints", "s1"])?.goal,
		).toBe("new goal");
		const row = qc.getQueryData<{
			pages: Array<{ items: Array<{ goal: string }> }>;
		}>(listKey)?.pages[0]?.items[0];
		expect(row?.goal).toBe("new goal");
		expect(listsInvalidated()).toBe(false);
	});

	it("refetches the lists when the name or status changed, since a list filters and sorts on them", () => {
		qc.setQueryData(listKey, { items: [sprint()] });
		applySprintChange(
			qc,
			P,
			sprintEvent("sprint.completed", sprint({ status: "completed" })),
		);
		expect(listsInvalidated()).toBe(true);
	});

	it("refetches the lists when the sprint is in none of them", () => {
		applySprintChange(qc, P, sprintEvent("sprint.updated", sprint()));
		expect(listsInvalidated()).toBe(true);
	});

	it("seeds a created sprint and refetches the lists", () => {
		expect(
			applySprintChange(qc, P, sprintEvent("sprint.created", sprint())),
		).toBe("patched");
		expect(qc.getQueryData(["projects", P, "sprints", "s1"])).toMatchObject({
			name: "Sprint 1",
		});
		expect(listsInvalidated()).toBe(true);
	});

	it("removes a deleted sprint", () => {
		qc.setQueryData(["projects", P, "sprints", "s1"], sprint());
		qc.setQueryData(listKey, { items: [sprint(), sprint({ id: "s2" })] });
		expect(
			applySprintChange(qc, P, sprintEvent("sprint.deleted", undefined)),
		).toBe("patched");
		expect(qc.getQueryData(["projects", P, "sprints", "s1"])).toBeUndefined();
		expect(
			qc
				.getQueryData<{ items: Array<{ id: string }> }>(listKey)
				?.items.map((r) => r.id),
		).toEqual(["s2"]);
	});

	it("leaves a change without a snapshot, or of another entity, to the caller", () => {
		expect(
			applySprintChange(qc, P, sprintEvent("sprint.updated", undefined)),
		).toBe("unhandled");
		expect(
			applySprintChange(qc, P, { type: "sprint.updated", payload: {} }),
		).toBe("unhandled");
		expect(calls).toEqual([]);
	});
});

describe("applyEnvironmentStatus", () => {
	const env = (over: Record<string, unknown> = {}) => ({
		id: "e1",
		name: "dev",
		status: "starting",
		error_message: "old failure",
		...over,
	});
	const listKey = ["projects", P, "environments", "list", ""];

	it("writes the status into the environment and its list row, clearing a stale error", () => {
		qc.setQueryData(["projects", P, "environments", "e1"], env());
		qc.setQueryData(listKey, {
			pages: [{ items: [env(), env({ id: "e2" })] }],
		});
		const res = applyEnvironmentStatus(qc, P, {
			environment_id: "e1",
			status: "running",
		});
		expect(res).toBe("patched");
		expect(
			qc.getQueryData(["projects", P, "environments", "e1"]),
		).toMatchObject({
			status: "running",
			error_message: null,
		});
		const rows = qc.getQueryData<{
			pages: Array<{ items: Array<{ id: string; status: string }> }>;
		}>(listKey)?.pages[0]?.items;
		expect(rows?.map((r) => r.status)).toEqual(["running", "starting"]);
	});

	it("records an error message, and refetches only the one environment, never a list", () => {
		qc.setQueryData(listKey, { items: [env()] });
		applyEnvironmentStatus(qc, P, {
			environment_id: "e1",
			status: "error",
			error_message: "boom",
		});
		expect(
			qc.getQueryData<{ items: Array<{ error_message: string }> }>(listKey)
				?.items[0]?.error_message,
		).toBe("boom");
		expect(calls).toEqual([
			{
				queryKey: ["projects", P, "environments", "e1"],
				exact: true,
				refetchType: "active",
			},
		]);
	});

	it("does not create a query nobody asked for, and leaves a malformed event to the caller", () => {
		applyEnvironmentStatus(qc, P, { environment_id: "e1", status: "running" });
		expect(
			qc.getQueryData(["projects", P, "environments", "e1"]),
		).toBeUndefined();
		expect(applyEnvironmentStatus(qc, P, { status: "running" })).toBe(
			"unhandled",
		);
		expect(applyEnvironmentStatus(qc, P, { environment_id: "e1" })).toBe(
			"unhandled",
		);
	});
});
