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
// What each event type does to the cache is an ordered table of rules in
// lib/project-event-rules.ts; the first matching rule that can handle the event
// wins. Events delivered by the sync protocol and legacy events take the same
// path, and a sync change that carries its entity is patched into the cache
// instead of refetched (lib/cache-patch.ts).

import { useQueryClient } from "@tanstack/react-query";
import { useEffect } from "react";
import { routeProjectEvent } from "@/lib/project-event-rules";
import {
	connectSocket,
	joinProject,
	leaveProject,
	onProjectChange,
	onProjectNamespaces,
	onProjectReset,
	type RealtimeEvent,
	rejoinProject,
} from "@/lib/socket-client";

// The first key segment (after ["projects", projectId]) of the queries that
// hold each realtime namespace's data. When the account loses a namespace the
// cached data under these roots must not stay on screen.
const NAMESPACE_QUERY_ROOTS: Record<string, readonly string[]> = {
	tasks: [
		"tasks",
		"agentActivities",
		// The task schema is read through tasks:read.
		"task-types",
		"task-statuses",
		"custom-fields",
	],
	docs: ["docs", "agentActivities"],
	sprints: ["sprints"],
	views: ["views"],
	workflows: ["workflows", "automations", "automation-dependency-map"],
	environments: ["environments"],
	conversations: ["conversations"],
	// ["projects", id] itself has no third segment, see holdsDataOf.
	project: [],
	members: ["members"],
	agents: ["agents"],
};

// The project's own permission queries live under ["members", "me"]: never
// reset with the data, they are what tells the UI what is still allowed.
const PERMISSION_QUERY_ROOT = "members";
const PERMISSION_QUERY_SUBROOT = "me";

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
	if (
		root === PERMISSION_QUERY_ROOT &&
		queryKey[3] === PERMISSION_QUERY_SUBROOT
	) {
		return false;
	}
	return namespaces.some((namespace) => {
		const roots = NAMESPACE_QUERY_ROOTS[namespace];
		if (!roots) return true;
		// The project's own detail query: ["projects", projectId].
		if (namespace === "project" && root === undefined) return true;
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
	"member.",
	"project.",
	"project_agent.",
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

		const ctx = { queryClient, projectId: currentProjectId };

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

			routeProjectEvent(ctx, event);
		}

		socket.on("event", handleEvent);

		// Changes from the sync protocol (a client that joined with
		// "sync:join") take the same path as legacy events, in order, and
		// resumed from the last applied change after a reconnect.
		const unsubscribeChanges = onProjectChange((changedProjectId, event) => {
			if (changedProjectId === currentProjectId) handleEvent(event);
		});

		// Everything a project shows may have changed while the client could not
		// be replayed what it missed (the feed was trimmed or recreated), or the
		// socket reconnected without the feed: refetch it.
		function refetchProject() {
			void queryClient.invalidateQueries({
				queryKey: ["conversation-event-window"],
			});
			void queryClient.invalidateQueries({
				queryKey: ["projects", currentProjectId],
			});
			void queryClient.invalidateQueries({
				queryKey: ["users", "me", "tasks"],
			});
		}
		const unsubscribeResets = onProjectReset((resetProjectId) => {
			if (resetProjectId === currentProjectId) refetchProject();
		});

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
				// A join that resumed the change feed was replayed everything it
				// missed: nothing to refetch.
				if (!change.resumed) refetchProject();
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
			unsubscribeChanges();
			unsubscribeResets();
			socket.off("event", handleEvent);
			socket.off("connect", handleConnect);
			leaveProject(currentProjectId);
		};
	}, [projectId, queryClient]);
}
