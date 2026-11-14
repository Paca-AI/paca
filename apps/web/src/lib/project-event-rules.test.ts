import type { QueryClient } from "@tanstack/react-query";
import { describe, expect, it, vi } from "vitest";
import { EVENT_RULES, routeProjectEvent } from "./project-event-rules";
import type { RealtimeEvent } from "./socket-client";

function route(
	type: string,
	payload: Record<string, unknown> = {},
	change?: RealtimeEvent["change"],
) {
	const queryClient = {
		invalidateQueries: vi.fn(),
		setQueryData: vi.fn(),
		setQueriesData: vi.fn(),
		removeQueries: vi.fn(),
	} as unknown as QueryClient;
	const rule = routeProjectEvent(
		{ queryClient, projectId: "p" },
		{ type, payload, change },
	);
	return { rule, queryClient };
}

describe("EVENT_RULES", () => {
	it("has unique rule names", () => {
		const names = EVENT_RULES.map((r) => r.name);
		expect(new Set(names).size).toBe(names.length);
	});

	// The routing table, as documentation: which rule takes which event.
	it.each([
		["task.updated", {}, "task"],
		["task.created", {}, "task"],
		["automation.applied", {}, "task"],
		["task.comment.added", { task_id: "t" }, "task-subresources"],
		["task.link.removed", { task_id: "t" }, "task-subresources"],
		["automation.node.added", {}, "automation"],
		["doc.updated", {}, "doc"],
		["doc.comment.added", { document_id: "d" }, "doc-comment"],
		["sprint.completed", {}, "sprint"],
		["view.task_moved", { view_id: "v" }, "view-task-moved"],
		["view.reordered", {}, "view-reordered"],
		["view.updated", {}, "view"],
		["workflow.assigned", {}, "workflow"],
		["environment.status_changed", {}, "environment"],
		["environment.created", {}, "environment"],
		["project.task_status.updated", {}, "task-schema"],
		["project.custom_field.deleted", {}, "task-schema"],
		["project.updated", {}, "project"],
		["member.added", {}, "member"],
		["project_agent.updated", {}, "project-agent"],
		["git.task.updated", { task_id: "t" }, "git"],
		["agent.acp_bridge.status", {}, "acp-bridge-status"],
	])("%s is handled by the %s rule", (type, payload, expected) => {
		expect(route(type, payload as Record<string, unknown>).rule).toBe(expected);
	});

	it("refreshes only the part of the task that a comment, attachment or link changed", () => {
		const refreshed = (type: string) => {
			const { queryClient } = route(type, { task_id: "t" });
			const calls = (
				queryClient.invalidateQueries as unknown as {
					mock: {
						calls: Array<
							[{ predicate?: (q: { queryKey: unknown[] }) => boolean }]
						>;
					};
				}
			).mock.calls;
			return (segment: string) =>
				calls.some(([a]) =>
					a.predicate?.({ queryKey: ["projects", "p", "tasks", "t", segment] }),
				);
		};
		const comment = refreshed("task.comment.added");
		expect(comment("activities")).toBe(true);
		expect(comment("links")).toBe(false);
		expect(comment("attachments")).toBe(false);
		const link = refreshed("task.link.added");
		expect(link("links")).toBe(true);
		expect(link("attachments")).toBe(false);
		const attachment = refreshed("task.attachment.removed");
		expect(attachment("attachments")).toBe(true);
		expect(attachment("links")).toBe(false);
	});

	it("refreshes the project-wide link listings on a link change only", () => {
		const listingsRefreshed = (type: string) =>
			(
				route(type, { task_id: "t" }).queryClient
					.invalidateQueries as unknown as {
					mock: { calls: Array<[{ queryKey?: unknown[] }]> };
				}
			).mock.calls.some(
				([a]) => JSON.stringify(a.queryKey) === '["projects","p","task-links"]',
			);
		expect(listingsRefreshed("task.link.added")).toBe(true);
		expect(listingsRefreshed("task.link.removed")).toBe(true);
		expect(listingsRefreshed("task.comment.added")).toBe(false);
		expect(listingsRefreshed("task.attachment.added")).toBe(false);
	});

	it("falls through to the broad rule when the narrow one has nothing to act on", () => {
		expect(route("task.comment.added", {}).rule).toBe("task");
		expect(route("doc.comment.added", {}).rule).toBe("doc");
		expect(route("view.task_moved", {}).rule).toBe("view");
	});

	it("uses a patching rule when the change carries the entity", () => {
		expect(
			route(
				"task.updated",
				{},
				{ entityType: "task", taskId: "t", entityId: "t", entity: { id: "t" } },
			).rule,
		).toBe("task-patch");
		expect(
			route(
				"sprint.updated",
				{},
				{ entityType: "sprint", entityId: "s", entity: { id: "s" } },
			).rule,
		).toBe("sprint-patch");
		expect(
			route("environment.status_changed", {
				environment_id: "e",
				status: "running",
			}).rule,
		).toBe("environment-status");
	});

	it("ignores an event type no rule knows", () => {
		const { rule, queryClient } = route("something.new");
		expect(rule).toBeNull();
		expect(queryClient.invalidateQueries).not.toHaveBeenCalled();
	});
});
