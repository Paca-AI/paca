// What a project event does to the cache, as an ordered table of rules.
//
// Both delivery paths end here: the legacy "event" message and a change from
// the sync protocol are the same { type, payload } (a sync change also carries
// `change`, which the patching rules use). The first rule that matches the type
// and handles the event wins; a rule that finds it cannot act (a comment that
// names no task) returns false and the next matching rule gets its turn, which
// is how the broad rules at the end stay the fallback for the narrow ones.
//
// Adding an event type means adding a rule here, and, on the API side, a
// routing rule in events.AuthzFor; TestEveryTopicIsClassified fails until that
// exists. Order matters: put a narrow rule before the broad rule for its prefix.

import type { QueryClient } from "@tanstack/react-query";
import {
	applyRealtimeAgentEvent,
	conversationEventWindowKey,
} from "@/lib/agent-api";
import { invalidateAgentDirectory } from "@/lib/agent-directory";
import {
	applyEnvironmentStatus,
	applySprintChange,
	applyTaskChange,
	refreshTaskSubQueries,
} from "@/lib/cache-patch";
import { invalidateMemberDirectory } from "@/lib/member-directory";
import { projectRolesKey } from "@/lib/role-api";
import type { RealtimeEvent } from "@/lib/socket-client";
import { invalidateSprintDirectory } from "@/lib/sprint-directory";

export interface EventContext {
	queryClient: QueryClient;
	projectId: string;
}

export interface EventRule {
	/** Shown in tests and logs. */
	name: string;
	matches: (type: string) => boolean;
	/** Returns false when the event cannot be handled here (the next rule runs). */
	handle: (ctx: EventContext, event: RealtimeEvent) => boolean | undefined;
}

const startsWith =
	(...prefixes: string[]) =>
	(type: string) =>
		prefixes.some((p) => type.startsWith(p));

const str = (v: unknown): string | null =>
	typeof v === "string" && v ? v : null;

/**
 * Task data is cached under several prefixes besides "tasks": sprint task
 * lists (["projects", id, "sprints", sprintId, "tasks"]), per-view task
 * positions (["projects", id, "views", viewId, "task-positions"]) and the
 * cross-project "my tasks" list. A task change made by the server (automation,
 * Jev autofill/auto-assign, agents) must refresh all of them, not just the ones
 * the acting client already refetches.
 */
export function invalidateTaskViews({
	queryClient,
	projectId,
}: EventContext): void {
	void queryClient.invalidateQueries({
		queryKey: ["projects", projectId, "tasks"],
	});
	void queryClient.invalidateQueries({
		predicate: (query) => {
			const key = query.queryKey;
			return (
				key[0] === "projects" &&
				key[1] === projectId &&
				((key[2] === "sprints" && key[4] === "tasks") ||
					(key[2] === "views" && key[4] === "task-positions"))
			);
		},
	});
	void queryClient.invalidateQueries({ queryKey: ["users", "me", "tasks"] });
	invalidateAgentActivities({ queryClient, projectId });
}

// Task and doc activity can belong to any agent, so every agent's feed is
// refreshed rather than working out which one from the payload.
function invalidateAgentActivities({
	queryClient,
	projectId,
}: EventContext): void {
	void queryClient.invalidateQueries({
		queryKey: ["projects", projectId, "agentActivities"],
	});
}

export const EVENT_RULES: EventRule[] = [
	{
		// A task's comments, attachments and links hang off the task: refresh
		// those, not every task list.
		name: "task-subresources",
		matches: startsWith("task.comment.", "task.attachment.", "task.link."),
		handle: (ctx, event) => {
			const taskId = str(event.payload.task_id);
			if (!taskId) return false;
			// Only the part that changed, and the timeline that records it.
			const part = event.type.startsWith("task.attachment.")
				? "attachments"
				: event.type.startsWith("task.link.")
					? "links"
					: "activities";
			refreshTaskSubQueries(ctx.queryClient, ctx.projectId, taskId, [
				part,
				"activities",
			]);
			invalidateAgentActivities(ctx);
			return true;
		},
	},
	{
		// A change from the sync protocol that carries the task after the change
		// is patched into the cache; only what it cannot answer is refetched
		// (see cache-patch.ts). Anything else falls through to "task".
		name: "task-patch",
		matches: (t) =>
			t === "task.created" || t === "task.updated" || t === "task.deleted",
		handle: (ctx, event) => {
			if (
				applyTaskChange(ctx.queryClient, ctx.projectId, event) !== "patched"
			) {
				return false;
			}
			invalidateAgentActivities(ctx);
			return true;
		},
	},
	{
		// task.* and automation.applied are task activities fanned out by the
		// API's ActivitySvc, for user edits and system-driven ones alike.
		name: "task",
		matches: (t) => t.startsWith("task.") || t === "automation.applied",
		handle: (ctx) => {
			invalidateTaskViews(ctx);
			return true;
		},
	},
	{
		// automation.* graph/lifecycle events (created/updated/node/edge...).
		name: "automation",
		matches: startsWith("automation."),
		handle: ({ queryClient, projectId }) => {
			void queryClient.invalidateQueries({
				queryKey: ["projects", projectId, "automations"],
			});
			void queryClient.invalidateQueries({
				queryKey: ["projects", projectId, "automation-dependency-map"],
			});
			return true;
		},
	},
	{
		// A doc's comments live in its own timeline only.
		name: "doc-comment",
		matches: startsWith("doc.comment."),
		handle: (ctx, event) => {
			const docId = str(event.payload.document_id);
			if (!docId) return false;
			void ctx.queryClient.invalidateQueries({
				queryKey: ["projects", ctx.projectId, "docs", docId, "activities"],
			});
			invalidateAgentActivities(ctx);
			return true;
		},
	},
	{
		name: "doc",
		matches: startsWith("doc."),
		handle: (ctx) => {
			void ctx.queryClient.invalidateQueries({
				queryKey: ["projects", ctx.projectId, "docs"],
			});
			invalidateAgentActivities(ctx);
			return true;
		},
	},
	{
		// A sprint change that carries the sprint (sync protocol) is patched
		// into the cache; completing or deleting one also moves its tasks.
		name: "sprint-patch",
		matches: startsWith("sprint."),
		handle: (ctx, event) => {
			if (
				applySprintChange(ctx.queryClient, ctx.projectId, event) !== "patched"
			) {
				return false;
			}
			invalidateSprintDirectory(ctx.projectId);
			if (
				event.type === "sprint.completed" ||
				event.type === "sprint.deleted"
			) {
				invalidateTaskViews(ctx);
			}
			return true;
		},
	},
	{
		// sprint.completed bulk-moves unfinished tasks to another sprint or the
		// backlog, so every sprint event refreshes task views too.
		name: "sprint",
		matches: startsWith("sprint."),
		handle: (ctx) => {
			invalidateSprintDirectory(ctx.projectId);
			void ctx.queryClient.invalidateQueries({
				queryKey: ["projects", ctx.projectId, "sprints"],
			});
			invalidateTaskViews(ctx);
			return true;
		},
	},
	{
		// A drag-and-drop moved tasks within one view: only that view's
		// positions changed.
		name: "view-task-moved",
		matches: (t) => t === "view.task_moved",
		handle: (ctx, event) => {
			const viewId = str(event.payload.view_id);
			if (!viewId) return false;
			void ctx.queryClient.invalidateQueries({
				queryKey: [
					"projects",
					ctx.projectId,
					"views",
					viewId,
					"task-positions",
				],
			});
			return true;
		},
	},
	{
		// A reorder changes the order of the view lists, nothing inside a view.
		name: "view-reordered",
		matches: (t) => t === "view.reordered",
		handle: ({ queryClient, projectId }) => {
			void queryClient.invalidateQueries({
				predicate: (query) =>
					query.queryKey[0] === "projects" &&
					query.queryKey[1] === projectId &&
					query.queryKey[2] === "views" &&
					typeof query.queryKey[3] === "object",
			});
			return true;
		},
	},
	{
		// A view's config is the caller's personal one, so no snapshot: refetch.
		name: "view",
		matches: startsWith("view."),
		handle: ({ queryClient, projectId }) => {
			void queryClient.invalidateQueries({
				queryKey: ["projects", projectId, "views"],
			});
			return true;
		},
	},
	{
		// Graph-structure changes on the automation builder and the task-scoped
		// workflow.assigned (the engine reassigning a task): both prefixes,
		// unconditionally, rather than branching per sub-type.
		name: "workflow",
		matches: startsWith("workflow."),
		handle: (ctx) => {
			void ctx.queryClient.invalidateQueries({
				queryKey: ["projects", ctx.projectId, "workflows"],
			});
			invalidateTaskViews(ctx);
			return true;
		},
	},
	{
		// The event carries the new status: patch it in instead of refetching.
		name: "environment-status",
		matches: (t) => t === "environment.status_changed",
		handle: (ctx, event) =>
			applyEnvironmentStatus(ctx.queryClient, ctx.projectId, event.payload) ===
			"patched",
	},
	{
		name: "environment",
		matches: startsWith("environment."),
		handle: ({ queryClient, projectId }) => {
			void queryClient.invalidateQueries({
				queryKey: ["projects", projectId, "environments"],
			});
			return true;
		},
	},
	{
		// The task schema: types, statuses and custom fields. Tasks hold ids and
		// names come from the schema queries, so a rename, a reorder or a new
		// default changes no task row. A deleted type/status/field is cleared
		// from the tasks that used it, and a status edit can change what counts
		// as done in a parent's subtask counts: those task views refetch.
		name: "task-schema",
		matches: startsWith(
			"project.task_type.",
			"project.task_status.",
			"project.custom_field.",
		),
		handle: (ctx, event) => {
			const { type } = event;
			const root = type.startsWith("project.task_type.")
				? "task-types"
				: type.startsWith("project.task_status.")
					? "task-statuses"
					: "custom-fields";
			void ctx.queryClient.invalidateQueries({
				queryKey: ["projects", ctx.projectId, root],
			});
			if (type.endsWith(".deleted") || type === "project.task_status.updated") {
				invalidateTaskViews(ctx);
			}
			return true;
		},
	},
	{
		// name, settings, avatar: the project's own query and the project lists,
		// not everything under it.
		name: "project",
		matches: (t) => t === "project.updated",
		handle: ({ queryClient, projectId }) => {
			void queryClient.invalidateQueries({
				queryKey: ["projects", projectId],
				exact: true,
			});
			void queryClient.invalidateQueries({
				predicate: (query) =>
					query.queryKey[0] === "projects" &&
					(query.queryKey[1] === "all" ||
						(typeof query.queryKey[1] === "object" &&
							query.queryKey[1] !== null)),
			});
			return true;
		},
	},
	{
		// The team page and every member picker. The prefix also covers the
		// caller's own permissions, which a role change moves.
		name: "member",
		matches: startsWith("member."),
		handle: ({ queryClient, projectId }) => {
			// Names, avatars and roles other places look up by id.
			invalidateMemberDirectory(projectId);
			void queryClient.invalidateQueries({
				queryKey: ["projects", projectId, "members"],
			});
			void queryClient.invalidateQueries({
				queryKey: projectRolesKey(projectId),
			});
			return true;
		},
	},
	{
		// An agent's configuration, and its membership.
		name: "project-agent",
		matches: startsWith("project_agent."),
		handle: ({ queryClient, projectId }) => {
			invalidateAgentDirectory(projectId);
			invalidateMemberDirectory(projectId);
			void queryClient.invalidateQueries({
				queryKey: ["projects", projectId, "agents"],
			});
			void queryClient.invalidateQueries({
				queryKey: ["projects", projectId, "members"],
			});
			return true;
		},
	},
	{
		// git.task.updated: a task's pull requests or branches changed; refresh
		// its Git panel.
		name: "git",
		matches: startsWith("git."),
		handle: ({ queryClient, projectId }, event) => {
			const taskId = str(event.payload.task_id);
			if (taskId) {
				void queryClient.invalidateQueries({
					queryKey: ["git", "projects", projectId, "tasks", taskId],
				});
			}
			return true;
		},
	},
	{
		// The Local Bridge panel's connected/disconnected badge. The payload
		// already carries the new state, so it is written straight into the
		// cache rather than refetched.
		name: "acp-bridge-status",
		matches: (t) => t === "agent.acp_bridge.status",
		handle: ({ queryClient, projectId }, event) => {
			const agentId = str(event.payload.agent_id);
			if (agentId && typeof event.payload.connected === "boolean") {
				queryClient.setQueryData(
					["projects", projectId, "agents", agentId, "acp-bridge-status"],
					{ connected: event.payload.connected },
				);
			}
			return true;
		},
	},
	{
		// Conversation caches and task activities (agent.session.started is
		// recorded as a task activity).
		name: "agent",
		matches: startsWith("agent."),
		handle: (ctx, event) => {
			const applied = applyRealtimeAgentEvent(ctx.queryClient, event.payload);
			if (applied && !applied.isPersistedEvent) {
				// A status-only notification (conversation started/paused/
				// finished/failed/stopped) rather than a discrete event: the point
				// to reconcile useConversationEventWindow's paginated cache with
				// the server, the safety net for a dropped realtime message.
				void ctx.queryClient.invalidateQueries({
					queryKey: conversationEventWindowKey(applied.conversationId),
				});
				void ctx.queryClient.invalidateQueries({
					queryKey: ["projects", ctx.projectId, "conversations"],
				});
				if (event.type === "agent.session.started") invalidateTaskViews(ctx);
			}
			return true;
		},
	},
];

/**
 * Runs an event through the rules and returns the name of the one that
 * handled it, or null when none did (an event type nothing here knows).
 */
export function routeProjectEvent(
	ctx: EventContext,
	event: RealtimeEvent,
): string | null {
	for (const rule of EVENT_RULES) {
		if (!rule.matches(event.type)) continue;
		if (rule.handle(ctx, event) !== false) return rule.name;
	}
	return null;
}
