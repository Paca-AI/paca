// Hook that joins a project's realtime rooms and invalidates React Query
// caches when events arrive from the server.
//
// Usage: call once per project layout/page:
//
//   useProjectRealtime(projectId);
//
// It's safe to mount this hook more than once for the same projectId at the
// same time (e.g. once in the persistent project layout and again in a
// nested conversations layout) — `joinProject`/`leaveProject` in
// socket-client.ts are reference-counted, so one instance unmounting won't
// evict room membership that another mounted instance still needs.
//
// The hook:
//   1. Joins the project rooms on mount (server grants tasks/docs room
//      access based on the caller's permissions).
//   2. Listens for "event" messages and maps event types to query
//      invalidations using the same query key prefixes as the API helpers.
//   3. Refetches the caller's permissions when the server reports that IAM
//      changed ("permissions:changed"), and resets what is cached for a kind of
//      data the caller could see and no longer can.
//   4. Leaves the project rooms and removes the listeners on unmount.
//
// Query invalidation strategy
// ---------------------------
// task.* events and automation.applied → invalidateTaskViews():
//                  ["projects", projectId, "tasks"], sprint task lists,
//                  view task positions and ["users", "me", "tasks"].
//                  This covers allTasksQueryOptions, taskQueryOptions,
//                  sprintTasksQueryOptions, viewTaskPositions,
//                  epicTasksQueryOptions, assignedTasksQueryOptions, etc.
//                  Also invalidates ["projects", projectId, "agentActivities"]
//                  (agentActivitiesQueryOptions) — task activity changes can
//                  belong to any agent, so every agent's activity feed cache
//                  is invalidated rather than trying to parse which agent
//                  from the payload.
// doc.* events   → invalidate ["projects", projectId, "docs"]
//                  This covers docFoldersQueryOptions, docListQueryOptions,
//                  docQueryOptions, etc. Also invalidates
//                  ["projects", projectId, "agentActivities"], same reasoning
//                  as task.* above.
// sprint.* events → invalidate ["projects", projectId, "sprints"] and
//                  invalidateTaskViews() (sprint completion moves tasks)
//                  This covers sprintsInfiniteQueryOptions and sprintQueryOptions —
//                  replaces the sidebar's old refetchInterval polling.
// view.* events  → invalidate ["projects", projectId, "views"]
//                  viewsByContextQueryOptions keys every context (sprint/
//                  backlog/timeline) under this shared "views" prefix with a
//                  trailing { context, sprintId } filter object, so one
//                  prefix invalidation covers all three regardless of which
//                  context changed.
// workflow.* events → invalidate ["projects", projectId, "workflows"] (the
//                  automation list/graph queries) and ["projects", projectId,
//                  "tasks"] (covers workflowsForTaskQueryOptions, which hangs
//                  off "tasks" rather than "workflows", plus the
//                  workflow.assigned bonus case — the automation engine
//                  reassigning a task should refresh that task's data too).
// automation.*   → (other than automation.applied) invalidate
//                  ["projects", projectId, "automations"] and the
//                  automation dependency map.
// environment.status_changed → invalidate ["projects", projectId,
//                  "environments"] (covers both environmentsQueryOptions,
//                  the list, and environmentQueryOptions, the per-id detail
//                  query, since both hang off this shared prefix). Replaces
//                  environment-api.ts's old fixed-interval polling while an
//                  environment was creating/starting/stopping.
//
// More granular invalidations (e.g. specific taskId) are avoided intentionally:
// the event payload fields are not yet stabilised and broad invalidation is
// safer and simpler.

import { useQueryClient } from "@tanstack/react-query";
import { useEffect } from "react";
import {
	applyRealtimeAgentEvent,
	conversationEventWindowKey,
} from "@/lib/agent-api";
import {
	connectSocket,
	joinProject,
	leaveProject,
	onProjectNamespaces,
	type RealtimeEvent,
	rejoinProject,
} from "@/lib/socket-client";
import { invalidateSprintDirectory } from "@/lib/sprint-directory";

// The first key segment (after ["projects", projectId]) of the queries that
// hold each realtime namespace's data. When the account loses a namespace the
// cached data under these roots must not stay on screen.
const NAMESPACE_QUERY_ROOTS: Record<string, readonly string[]> = {
	tasks: ["tasks", "agentActivities"],
	docs: ["docs", "agentActivities"],
	sprints: ["sprints"],
	views: ["views"],
	workflows: ["workflows", "automations", "automation-dependency-map"],
	environments: ["environments"],
	conversations: ["conversations"],
};

// The project's own permission queries live under "members": never reset
// with the data, they are what tells the UI what is still allowed.
const PERMISSION_QUERY_ROOT = "members";

// holdsDataOf reports whether a cached query holds data of any of the given
// namespaces. A namespace this table does not know errs towards safety:
// every project query except the permissions is treated as holding it.
function holdsDataOf(
	queryKey: readonly unknown[],
	projectId: string,
	namespaces: readonly string[],
): boolean {
	if (queryKey[0] !== "projects" || queryKey[1] !== projectId) return false;
	const root = queryKey[2];
	if (root === PERMISSION_QUERY_ROOT) return false;
	return namespaces.some((namespace) => {
		const roots = NAMESPACE_QUERY_ROOTS[namespace];
		if (!roots) return true;
		if (typeof root === "string" && roots.includes(root)) return true;
		// Task data is also cached under the sprints and views prefixes: a
		// sprint's task list and a view's task positions.
		return (
			namespace === "tasks" &&
			((root === "sprints" && queryKey[4] === "tasks") ||
				(root === "views" && queryKey[4] === "task-positions"))
		);
	});
}

// projectId may be undefined for a component that's reused at both project
// and global scope (e.g. the Conversations page) — the hook is a no-op in
// that case rather than requiring the caller to conditionally call it, which
// would violate the rules of hooks.
// Realtime event types that correspond to a new project activity log entry.
// agent.* is deliberately absent: it carries per-token conversation events,
// and the work an agent does arrives as task.*/doc.* anyway.
const ACTIVITY_LOG_EVENT_PREFIXES = [
	"task.",
	"doc.",
	"sprint.",
	"view.created",
	"view.updated",
	"view.deleted",
	"automation.",
	"environment.",
];
// Realtime-only events under those prefixes, which are never logged.
const ACTIVITY_LOG_SKIPPED_EVENTS = new Set([
	"environment.status_changed",
	"automation.node.updated",
]);
const ACTIVITY_LOG_REFRESH_DELAY_MS = 1500;

export function useProjectRealtime(projectId: string | undefined): void {
	const queryClient = useQueryClient();

	useEffect(() => {
		if (!projectId) return;
		// Narrowed to a plain non-optional const: TS's control-flow narrowing
		// of the guard above doesn't carry into the named function
		// declarations below (handleEvent/handleConnect are hoisted, so TS
		// can't prove they're only ever called after the guard runs).
		const currentProjectId = projectId;
		const socket = connectSocket();

		// Subscribe to the project rooms.
		joinProject(currentProjectId);

		// Task data is cached under several prefixes besides "tasks": sprint
		// task lists (["projects", id, "sprints", sprintId, "tasks"]), per-view
		// task positions (["projects", id, "views", viewId, "task-positions"])
		// and the cross-project "my tasks" list. A task change made by the
		// server (automation, Jev autofill/auto-assign, agents) must refresh
		// all of them, not just the ones the acting client already refetches.
		function invalidateTaskViews() {
			void queryClient.invalidateQueries({
				queryKey: ["projects", currentProjectId, "tasks"],
			});
			void queryClient.invalidateQueries({
				predicate: (query) => {
					const key = query.queryKey;
					return (
						key[0] === "projects" &&
						key[1] === currentProjectId &&
						((key[2] === "sprints" && key[4] === "tasks") ||
							(key[2] === "views" && key[4] === "task-positions"))
					);
				},
			});
			void queryClient.invalidateQueries({
				queryKey: ["users", "me", "tasks"],
			});
			void queryClient.invalidateQueries({
				queryKey: ["projects", currentProjectId, "agentActivities"],
			});
		}

		// The project activity log is written by a worker reading the same
		// event stream, so it lands shortly *after* this event arrives —
		// refetching immediately would miss it. Debounced, which also
		// collapses a burst of events into one refetch.
		let activityRefreshTimer: ReturnType<typeof setTimeout> | undefined;
		function scheduleActivityLogRefresh() {
			clearTimeout(activityRefreshTimer);
			activityRefreshTimer = setTimeout(() => {
				void queryClient.invalidateQueries({
					queryKey: ["projects", currentProjectId, "projectActivities"],
				});
			}, ACTIVITY_LOG_REFRESH_DELAY_MS);
		}

		function handleEvent(event: RealtimeEvent) {
			const { type } = event;

			if (
				ACTIVITY_LOG_EVENT_PREFIXES.some((p) => type.startsWith(p)) &&
				!ACTIVITY_LOG_SKIPPED_EVENTS.has(type)
			) {
				scheduleActivityLogRefresh();
			}

			// task.* and automation.applied are task activities fanned out by
			// the API's ActivitySvc — for user edits and for system-driven
			// ones (Jev autofill/auto-assign record task.updated, the
			// automation engine records automation.applied).
			if (type.startsWith("task.") || type === "automation.applied") {
				invalidateTaskViews();
				return;
			}

			// automation.* graph/lifecycle events (created/updated/node/edge...)
			if (type.startsWith("automation.")) {
				void queryClient.invalidateQueries({
					queryKey: ["projects", currentProjectId, "automations"],
				});
				void queryClient.invalidateQueries({
					queryKey: ["projects", currentProjectId, "automation-dependency-map"],
				});
				return;
			}

			if (type.startsWith("doc.")) {
				void queryClient.invalidateQueries({
					queryKey: ["projects", currentProjectId, "docs"],
				});
				void queryClient.invalidateQueries({
					queryKey: ["projects", currentProjectId, "agentActivities"],
				});
				return;
			}

			// sprint.completed bulk-moves unfinished tasks to another sprint or
			// the backlog, so every sprint event refreshes task views too.
			if (type.startsWith("sprint.")) {
				invalidateSprintDirectory(currentProjectId);
				void queryClient.invalidateQueries({
					queryKey: ["projects", currentProjectId, "sprints"],
				});
				invalidateTaskViews();
				return;
			}

			if (type.startsWith("view.")) {
				void queryClient.invalidateQueries({
					queryKey: ["projects", currentProjectId, "views"],
				});
				return;
			}

			// workflow.* events: covers both graph-structure changes (node/edge/
			// rule/transition/lifecycle edits on the automation builder) and the
			// task-scoped workflow.assigned event (the automation engine
			// reassigning a task) — invalidate both prefixes unconditionally
			// rather than branching per sub-type.
			if (type.startsWith("workflow.")) {
				void queryClient.invalidateQueries({
					queryKey: ["projects", currentProjectId, "workflows"],
				});
				invalidateTaskViews();
				return;
			}

			if (type.startsWith("environment.")) {
				void queryClient.invalidateQueries({
					queryKey: ["projects", currentProjectId, "environments"],
				});
				return;
			}

			// git.task.updated — a task's pull requests or branches changed;
			// refresh its Git panel using the task_id from the payload.
			if (type.startsWith("git.")) {
				const taskId =
					typeof event.payload.task_id === "string"
						? event.payload.task_id
						: null;
				if (taskId) {
					void queryClient.invalidateQueries({
						queryKey: ["git", "projects", currentProjectId, "tasks", taskId],
					});
				}
				return;
			}

			// agent.acp_bridge.status: the Local Bridge panel's connected/
			// disconnected badge. The payload already carries the new state, so
			// this writes it directly into the query cache instead of
			// invalidating + refetching — the whole point of moving off polling.
			if (type === "agent.acp_bridge.status") {
				const agentId =
					typeof event.payload.agent_id === "string"
						? event.payload.agent_id
						: null;
				if (agentId && typeof event.payload.connected === "boolean") {
					queryClient.setQueryData(
						[
							"projects",
							currentProjectId,
							"agents",
							agentId,
							"acp-bridge-status",
						],
						{ connected: event.payload.connected },
					);
				}
				return;
			}

			// agent.* events: conversation caches and task activities
			// (agent.session.started is recorded as a task activity).
			if (type.startsWith("agent.")) {
				const applied = applyRealtimeAgentEvent(queryClient, event.payload);

				if (applied && !applied.isPersistedEvent) {
					// A status-only notification (conversation started/paused/
					// finished/failed/stopped) rather than a discrete event —
					// this is the point to reconcile useConversationEventWindow's
					// paginated cache with the server (its own live-appended
					// events already cover the common case, this is the safety
					// net for anything a dropped realtime message missed).
					void queryClient.invalidateQueries({
						queryKey: conversationEventWindowKey(applied.conversationId),
					});
					void queryClient.invalidateQueries({
						queryKey: ["projects", currentProjectId, "conversations"],
					});
					// agent.session.started shows up in task activity feeds
					if (type === "agent.session.started" || type.startsWith("task.")) {
						invalidateTaskViews();
					}
				}
				return;
			}
		}

		socket.on("event", handleEvent);

		// Socket.IO's "connect" event fires on every automatic reconnect, not
		// just the initial connection. A reconnect starts a new server-side
		// session with no room membership until we "join" again — without
		// this, a network blip silently stops delivering invalidations while
		// the socket still reports connected. Uses `rejoinProject` (not
		// `joinProject`) since this isn't a new subscriber — it must not bump
		// the reference count that `leaveProject` decrements on unmount.
		//
		// Whatever the project's data did while the socket was away (a network
		// drop, or the reconnect that follows a "reauth") never arrived as an
		// event, so after a *re*connect the project's queries are refetched.
		// That waits for the rejoin's ack (see the namespaces listener below),
		// not the connect itself: an event landing between a refetch and the
		// join would be missed by both.
		let hasConnected = socket.connected;
		let refetchAfterRejoin = false;
		function handleConnect() {
			if (hasConnected) refetchAfterRejoin = true;
			hasConnected = true;
			rejoinProject(currentProjectId);
		}
		socket.on("connect", handleConnect);

		// The server reports the project's namespaces on the join ack and
		// whenever IAM changes (a role edit, a detached role, a removed
		// member). The caller's permissions may have changed, so refetch them:
		// that re-renders everything gated on useEffectiveActions. When a kind
		// of data is no longer visible to the caller, what is cached for that
		// kind of data may be stale or now refused, so reset it (the rest of
		// the project's cache is deliberately left alone); a gained kind is
		// invalidated, since its queries may be sitting in the error state.
		const unsubscribeNamespaces = onProjectNamespaces((change) => {
			if (change.projectId !== currentProjectId) return;
			if (refetchAfterRejoin) {
				refetchAfterRejoin = false;
				void queryClient.invalidateQueries({
					queryKey: ["projects", currentProjectId],
				});
				void queryClient.invalidateQueries({
					queryKey: ["users", "me", "tasks"],
				});
			}
			void queryClient.invalidateQueries({
				queryKey: [
					"projects",
					currentProjectId,
					"members",
					"me",
					"permissions",
				],
			});
			void queryClient.invalidateQueries({
				queryKey: ["auth", "me", "permissions"],
			});
			if (change.removed.length > 0) {
				// Reset, not just invalidate: a refetch the server now refuses
				// (403) would leave the previous data on screen, still showing
				// what the account may no longer see. Only what belongs to the
				// lost namespaces is reset: the rest of the project's data is
				// still the member's, and the permission queries invalidated
				// above are being refetched, not emptied.
				void queryClient.resetQueries({
					predicate: (query) =>
						holdsDataOf(query.queryKey, currentProjectId, change.removed),
				});
			}
			if (change.added.length > 0) {
				// A gained namespace's queries may be sitting in the error state
				// from when it was refused; an errored query does not refetch by
				// itself, so ask for it.
				void queryClient.invalidateQueries({
					predicate: (query) =>
						holdsDataOf(query.queryKey, currentProjectId, change.added),
				});
			}
		});

		return () => {
			clearTimeout(activityRefreshTimer);
			unsubscribeNamespaces();
			socket.off("event", handleEvent);
			socket.off("connect", handleConnect);
			leaveProject(currentProjectId);
		};
	}, [projectId, queryClient]);
}
