// Namespace-based rooms.
//
// A socket is placed into namespace-scoped rooms at join time:
//
//   project:<projectId>:tasks        — task.* events
//   project:<projectId>:conversations — agent.* conversation events
//   project:<projectId>:docs         — doc.* events
//   project:<projectId>:workflows    — workflow.* graph events
//   project:<projectId>:sprints      — sprint.* events
//   project:<projectId>:views        — view.* events
//   project:<projectId>:environments — environment.* events
//
// Room membership is a prefilter, not the authorization: the server fetches
// the account's effective project actions, which include an action allowed
// only on some resource inside the project (one sprint's tasks), and joins the
// rooms whose read action is in that list. Who actually receives an event is
// decided per event, on the resource the event names, by the API's IAM engine
// (see access.ts). The rooms are re-synced when the API announces an IAM change
// (see resync.ts), so a revoked grant stops delivering without a rejoin.

// EventNamespace identifies the permission-gated sub-domains.
export type EventNamespace =
	| "tasks"
	| "conversations"
	| "docs"
	| "workflows"
	| "sprints"
	| "views"
	| "environments";

// NAMESPACE_PERMISSIONS maps each namespace to the project permission required
// to receive events in that namespace.
export const NAMESPACE_PERMISSIONS: Record<EventNamespace, string> = {
	tasks: "tasks:read",
	conversations: "conversations:read",
	docs: "docs:read",
	workflows: "workflows:read",
	sprints: "sprints:read",
	views: "views:read",
	environments: "environments:read",
};

// projectRoomName returns the Socket.IO room name for a project + namespace pair.
export function projectRoomName(
	projectId: string,
	namespace: EventNamespace,
): string {
	return `project:${projectId}:${namespace}`;
}

// namespaceForAction returns the room namespace whose read action is `action`,
// or undefined when no room carries it.
export function namespaceForAction(action: string): EventNamespace | undefined {
	for (const [ns, required] of Object.entries(NAMESPACE_PERMISSIONS)) {
		if (required === action) return ns as EventNamespace;
	}
	return undefined;
}

// agentChatRoomName returns the Socket.IO room name for a user's global
// agent chat events — agent.* events with no project_id (a conversation with
// a global agent from the home page / admin pages, see services/ai-agent's
// core.streams.publish_realtime) route here instead of a project room, keyed
// on payload.actor_account_id. Mirrors the existing
// `user:${userId}:notifications` per-user room, auto-joined at connect time
// (see server.ts) rather than via an explicit "join" — there's no
// project-membership permission check to run first, since this is just the
// caller's own identity.
export function agentChatRoomName(userId: string): string {
	return `user:${userId}:agent-chat`;
}

// eventNamespace infers the namespace from the event type prefix. It is the
// fallback for an event whose publisher attached no `authz` descriptor (the
// agent.* events services/ai-agent publishes): the API's descriptor, when
// present, names the action and resource and wins (see subscriber.ts).
// Returns undefined for unknown event types.
export function eventNamespace(type: string): EventNamespace | undefined {
	if (type.startsWith("task.")) return "tasks";
	if (type.startsWith("doc.")) return "docs";
	// git.task.* events (a task's pull requests or branches changed) are
	// task-scoped and require the same tasks:read permission.
	if (type.startsWith("git.")) return "tasks";
	// The ACP bridge's connected/disconnected status carries no conversation
	// content, only an agent id and a flag.
	if (type === "agent.acp_bridge.status") return "tasks";
	// Other agent.* events carry conversation content (messages, reasoning,
	// tool actions), so they require conversations:read — the permission the
	// REST conversation routes are gated on — not tasks:read. The room only
	// says who may read conversations project-wide; the subscriber checks each
	// conversation individually before delivering (see conversation-access.ts).
	if (type.startsWith("agent.")) return "conversations";
	// workflow.assigned is task-scoped (the automation engine reassigning a
	// task) — special-cased ahead of the generic workflow.* rule below so it
	// routes on tasks:read rather than workflows:read.
	if (type === "workflow.assigned") return "tasks";
	// All other workflow.* events describe changes to the workflow graph
	// itself (nodes, edges, rules, transitions, lifecycle) and require
	// workflows:read — a permission distinct from tasks:read.
	if (type.startsWith("workflow.")) return "workflows";
	// automation.applied is the task activity ActivitySvc fans out when the
	// automation engine changes a task — task-scoped, so it routes on
	// tasks:read ahead of the generic automation.* rule below.
	if (type === "automation.applied") return "tasks";
	// Other automation.* events are automation graph/lifecycle changes,
	// gated on workflows:read (the same permission the automation routes use).
	if (type.startsWith("automation.")) return "workflows";
	if (type.startsWith("sprint.")) return "sprints";
	// view.* events cover sprint/backlog/timeline interaction views, whose
	// routes are gated on views:read (not sprints:read) on view/<id>.
	if (type.startsWith("view.")) return "views";
	// environment.* events (currently just environment.status_changed)
	// require environments:read, the same permission GET
	// .../environments/:id is gated on.
	if (type.startsWith("environment.")) return "environments";
	return undefined;
}

// hasAction reports whether the account's effective project actions (action ->
// true, as fetchProjectPermissions returns them) include `required`. The API
// lists concrete actions, already expanded from any wildcard in a policy, so
// this is a plain lookup: the policy language lives in the API alone.
export function hasAction(
	permissions: Record<string, boolean>,
	required: string,
): boolean {
	return permissions[required] === true;
}
