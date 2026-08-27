// Namespace-based permission model.
//
// Instead of checking permissions on every received message, sockets are placed
// into namespace-scoped rooms at join time:
//
//   project:<projectId>:tasks        — receives all task.* events
//   project:<projectId>:docs         — receives all doc.* events
//   project:<projectId>:workflows    — receives all workflow.* graph events
//   project:<projectId>:sprints      — receives all sprint.* and view.* events
//   project:<projectId>:environments — receives all environment.* events
//
// Room membership is determined once when the client emits "join": the server
// fetches the user's project permissions, then joins only the rooms the user is
// allowed to see.  The Pub/Sub subscriber routes events directly to the correct
// room with a plain io.to(room).emit() — no per-socket checks needed.

// EventNamespace identifies the permission-gated sub-domains.
export type EventNamespace =
	| "tasks"
	| "docs"
	| "workflows"
	| "sprints"
	| "environments";

// NAMESPACE_PERMISSIONS maps each namespace to the project permission required
// to receive events in that namespace.
export const NAMESPACE_PERMISSIONS: Record<EventNamespace, string> = {
	tasks: "tasks.read",
	docs: "docs.read",
	workflows: "workflows.read",
	sprints: "sprints.read",
	environments: "environments.read",
};

// projectRoomName returns the Socket.IO room name for a project + namespace pair.
export function projectRoomName(
	projectId: string,
	namespace: EventNamespace,
): string {
	return `project:${projectId}:${namespace}`;
}

// agentChatRoomName returns the Socket.IO room name for a user's global
// agent chat events — agent.* events with no project_id (a conversation with
// a global agent from the home page / admin pages, see services/ai-agent's
// core.streams.publish_realtime) route here instead of a project room, keyed
// on payload.actor_user_id. Mirrors the existing
// `user:${userId}:notifications` per-user room, auto-joined at connect time
// (see server.ts) rather than via an explicit "join" — there's no
// project-membership permission check to run first, since this is just the
// caller's own identity.
export function agentChatRoomName(userId: string): string {
	return `user:${userId}:agent-chat`;
}

// eventNamespace infers the namespace from the event type prefix.
// Returns undefined for unknown event types.
export function eventNamespace(type: string): EventNamespace | undefined {
	if (type.startsWith("task.")) return "tasks";
	if (type.startsWith("doc.")) return "docs";
	// github.branch.* and github.pr.* events are delivered to the tasks room
	// because they are task-scoped and require the same tasks.read permission.
	if (type.startsWith("github.")) return "tasks";
	// agent.* events are task-scoped (conversation started/finished/events) and
	// require the same tasks.read permission.
	if (type.startsWith("agent.")) return "tasks";
	// workflow.assigned is task-scoped (the automation engine reassigning a
	// task) — special-cased ahead of the generic workflow.* rule below so it
	// routes on tasks.read rather than workflows.read.
	if (type === "workflow.assigned") return "tasks";
	// All other workflow.* events describe changes to the workflow graph
	// itself (nodes, edges, rules, transitions, lifecycle) and require
	// workflows.read — a permission distinct from tasks.read.
	if (type.startsWith("workflow.")) return "workflows";
	if (type.startsWith("sprint.")) return "sprints";
	// view.* events cover sprint/backlog/timeline interaction views. They
	// require the same sprints.read permission as sprint.* (see
	// view_handler.go's route registration), so they share the room.
	if (type.startsWith("view.")) return "sprints";
	// environment.* events (currently just environment.status_changed)
	// require environments.read, the same permission GET
	// .../environments/:id is gated on.
	if (type.startsWith("environment.")) return "environments";
	return undefined;
}

// hasProjectPermission checks whether a permission map grants the requested
// permission.  Wildcard permissions are honoured:
//
//   "*"        → grants everything
//   "tasks.*"  → grants "tasks.read", "tasks.write", etc.
export function hasProjectPermission(
	permissions: Record<string, boolean>,
	required: string,
): boolean {
	if (permissions["*"] === true) return true;
	if (permissions[required] === true) return true;

	// Check the namespace wildcard (e.g. "tasks.*" covers "tasks.read").
	const dotIdx = required.lastIndexOf(".");
	if (dotIdx !== -1) {
		const wildcard = `${required.slice(0, dotIdx)}.*`;
		if (permissions[wildcard] === true) return true;
	}

	return false;
}
