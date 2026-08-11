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
//   3. Leaves the project rooms and removes the listener on unmount.
//
// Query invalidation strategy
// ---------------------------
// task.* events  → invalidate ["projects", projectId, "tasks"]
//                  This covers allTasksQueryOptions, taskQueryOptions,
//                  sprintTasksQueryOptions, epicTasksQueryOptions, etc.
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
// sprint.* events → invalidate ["projects", projectId, "sprints"]
//                  This covers sprintsQueryOptions and sprintQueryOptions —
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
	type RealtimeEvent,
	rejoinProject,
} from "@/lib/socket-client";

// projectId may be undefined for a component that's reused at both project
// and global scope (e.g. the Conversations page) — the hook is a no-op in
// that case rather than requiring the caller to conditionally call it, which
// would violate the rules of hooks.
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

		function handleEvent(event: RealtimeEvent) {
			const { type } = event;

			if (type.startsWith("task.")) {
				void queryClient.invalidateQueries({
					queryKey: ["projects", currentProjectId, "tasks"],
				});
				void queryClient.invalidateQueries({
					queryKey: ["projects", currentProjectId, "agentActivities"],
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

			if (type.startsWith("sprint.")) {
				void queryClient.invalidateQueries({
					queryKey: ["projects", currentProjectId, "sprints"],
				});
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
				void queryClient.invalidateQueries({
					queryKey: ["projects", currentProjectId, "tasks"],
				});
				return;
			}

			// github.branch.linked / github.pr.linked — refresh the affected
			// task's branch and PR lists using the task_id from the payload.
			if (type.startsWith("github.")) {
				const taskId =
					typeof event.payload.task_id === "string"
						? event.payload.task_id
						: null;
				if (taskId) {
					void queryClient.invalidateQueries({
						queryKey: ["projects", currentProjectId, "tasks", taskId, "github"],
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
						void queryClient.invalidateQueries({
							queryKey: ["projects", currentProjectId, "tasks"],
						});
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
		function handleConnect() {
			rejoinProject(currentProjectId);
		}
		socket.on("connect", handleConnect);

		return () => {
			socket.off("event", handleEvent);
			socket.off("connect", handleConnect);
			leaveProject(currentProjectId);
		};
	}, [projectId, queryClient]);
}
