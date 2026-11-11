import { renderHook } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

// ── Hoisted mocks ─────────────────────────────────────────────────────────────

const mocks = vi.hoisted(() => {
	const socket = {
		connected: false,
		on: vi.fn(),
		off: vi.fn(),
	};

	return {
		socket,
		connectSocket: vi.fn(() => socket),
		joinProject: vi.fn(),
		leaveProject: vi.fn(),
		rejoinProject: vi.fn(),
		onProjectNamespaces: vi.fn(),
		onProjectChange: vi.fn((_listener: unknown) => () => {}),
		onProjectReset: vi.fn((_listener: unknown) => () => {}),
		unsubscribeNamespaces: vi.fn(),
		invalidateQueries: vi.fn(),
		resetQueries: vi.fn(),
		setQueryData: vi.fn(),
		setQueriesData: vi.fn(),
		removeQueries: vi.fn(),
		invalidateMemberDirectory: vi.fn(),
		invalidateAgentDirectory: vi.fn(),
	};
});

vi.mock("@/lib/member-directory", () => ({
	invalidateMemberDirectory: mocks.invalidateMemberDirectory,
}));
vi.mock("@/lib/agent-directory", () => ({
	invalidateAgentDirectory: mocks.invalidateAgentDirectory,
}));

vi.mock("@/lib/socket-client", () => ({
	connectSocket: mocks.connectSocket,
	joinProject: mocks.joinProject,
	leaveProject: mocks.leaveProject,
	rejoinProject: mocks.rejoinProject,
	onProjectNamespaces: mocks.onProjectNamespaces,
	onProjectChange: mocks.onProjectChange,
	onProjectReset: mocks.onProjectReset,
}));

vi.mock("@tanstack/react-query", async () => {
	const actual = await vi.importActual<typeof import("@tanstack/react-query")>(
		"@tanstack/react-query",
	);
	return {
		...actual,
		useQueryClient: () => ({
			invalidateQueries: mocks.invalidateQueries,
			resetQueries: mocks.resetQueries,
			setQueryData: mocks.setQueryData,
			setQueriesData: mocks.setQueriesData,
			removeQueries: mocks.removeQueries,
		}),
	};
});

import { useProjectRealtime } from "./use-project-realtime";

// ── Tests ─────────────────────────────────────────────────────────────────────

describe("useProjectRealtime", () => {
	beforeEach(() => {
		vi.clearAllMocks();
		mocks.socket.connected = false;
		mocks.connectSocket.mockReturnValue(mocks.socket);
		mocks.onProjectNamespaces.mockReturnValue(mocks.unsubscribeNamespaces);
	});

	it("joins the project rooms on mount", () => {
		renderHook(() => useProjectRealtime("proj-abc"));

		expect(mocks.connectSocket).toHaveBeenCalledTimes(1);
		expect(mocks.joinProject).toHaveBeenCalledWith("proj-abc");
	});

	it("registers an event listener on mount", () => {
		renderHook(() => useProjectRealtime("proj-abc"));

		expect(mocks.socket.on).toHaveBeenCalledWith("event", expect.any(Function));
	});

	it("removes the event listener and leaves project rooms on unmount", () => {
		const { unmount } = renderHook(() => useProjectRealtime("proj-abc"));

		const [, listener] = mocks.socket.on.mock.calls[0] as [
			string,
			(...args: unknown[]) => void,
		];

		unmount();

		expect(mocks.socket.off).toHaveBeenCalledWith("event", listener);
		expect(mocks.leaveProject).toHaveBeenCalledWith("proj-abc");
	});

	it("invalidates tasks query key on task.* events", () => {
		renderHook(() => useProjectRealtime("proj-abc"));

		const [, listener] = mocks.socket.on.mock.calls[0] as [
			string,
			(event: { type: string; payload: Record<string, unknown> }) => void,
		];

		listener({ type: "task.created", payload: {} });

		expect(mocks.invalidateQueries).toHaveBeenCalledWith({
			queryKey: ["projects", "proj-abc", "tasks"],
		});
	});

	it("invalidates docs query key on doc.* events", () => {
		renderHook(() => useProjectRealtime("proj-abc"));

		const [, listener] = mocks.socket.on.mock.calls[0] as [
			string,
			(event: { type: string; payload: Record<string, unknown> }) => void,
		];

		listener({ type: "doc.updated", payload: {} });

		expect(mocks.invalidateQueries).toHaveBeenCalledWith({
			queryKey: ["projects", "proj-abc", "docs"],
		});
	});

	it("invalidates views query key on view.* events", () => {
		renderHook(() => useProjectRealtime("proj-abc"));

		const [, listener] = mocks.socket.on.mock.calls[0] as [
			string,
			(event: { type: string; payload: Record<string, unknown> }) => void,
		];

		listener({ type: "view.created", payload: {} });

		expect(mocks.invalidateQueries).toHaveBeenCalledWith({
			queryKey: ["projects", "proj-abc", "views"],
		});
	});

	it("invalidates environments query key on environment.status_changed events", () => {
		renderHook(() => useProjectRealtime("proj-abc"));

		const [, listener] = mocks.socket.on.mock.calls[0] as [
			string,
			(event: { type: string; payload: Record<string, unknown> }) => void,
		];

		listener({ type: "environment.status_changed", payload: {} });

		expect(mocks.invalidateQueries).toHaveBeenCalledWith({
			queryKey: ["projects", "proj-abc", "environments"],
		});
	});

	it("invalidates workflows and tasks query keys on workflow.* events", () => {
		renderHook(() => useProjectRealtime("proj-abc"));

		const [, listener] = mocks.socket.on.mock.calls[0] as [
			string,
			(event: { type: string; payload: Record<string, unknown> }) => void,
		];

		listener({ type: "workflow.node.added", payload: {} });

		expect(mocks.invalidateQueries).toHaveBeenCalledWith({
			queryKey: ["projects", "proj-abc", "workflows"],
		});
		expect(mocks.invalidateQueries).toHaveBeenCalledWith({
			queryKey: ["projects", "proj-abc", "tasks"],
		});
	});

	it("invalidates workflows and tasks query keys on workflow.assigned events", () => {
		renderHook(() => useProjectRealtime("proj-abc"));

		const [, listener] = mocks.socket.on.mock.calls[0] as [
			string,
			(event: { type: string; payload: Record<string, unknown> }) => void,
		];

		listener({
			type: "workflow.assigned",
			payload: { project_id: "proj-abc", task_id: "task-1" },
		});

		expect(mocks.invalidateQueries).toHaveBeenCalledWith({
			queryKey: ["projects", "proj-abc", "workflows"],
		});
		expect(mocks.invalidateQueries).toHaveBeenCalledWith({
			queryKey: ["projects", "proj-abc", "tasks"],
		});
	});

	it("invalidates the task Git query key on git.task.updated events", () => {
		renderHook(() => useProjectRealtime("proj-abc"));

		const [, listener] = mocks.socket.on.mock.calls[0] as [
			string,
			(event: { type: string; payload: Record<string, unknown> }) => void,
		];

		listener({
			type: "git.task.updated",
			payload: { task_id: "task-42" },
		});

		expect(mocks.invalidateQueries).toHaveBeenCalledWith({
			queryKey: ["git", "projects", "proj-abc", "tasks", "task-42"],
		});
	});

	it("does not invalidate when git.* event has no task_id", () => {
		renderHook(() => useProjectRealtime("proj-abc"));

		const [, listener] = mocks.socket.on.mock.calls[0] as [
			string,
			(event: { type: string; payload: Record<string, unknown> }) => void,
		];

		listener({ type: "git.task.updated", payload: {} });

		expect(mocks.invalidateQueries).not.toHaveBeenCalled();
	});

	it("does not invalidate queries for unrecognised event types", () => {
		renderHook(() => useProjectRealtime("proj-abc"));

		const [, listener] = mocks.socket.on.mock.calls[0] as [
			string,
			(event: { type: string; payload: Record<string, unknown> }) => void,
		];

		listener({ type: "unknown.event", payload: {} });

		expect(mocks.invalidateQueries).not.toHaveBeenCalled();
	});

	describe("comments, sprints and views", () => {
		function send(
			type: string,
			payload: Record<string, unknown> = {},
			change?: unknown,
		) {
			renderHook(() => useProjectRealtime("proj-abc"));
			const [, listener] = mocks.socket.on.mock.calls[0] as [
				string,
				(event: unknown) => void,
			];
			listener({ type, payload, change });
		}
		const keys = () =>
			mocks.invalidateQueries.mock.calls.map(
				(c) => (c[0] as { queryKey?: unknown[] }).queryKey,
			);

		it("refreshes only the task's own queries on a task comment, not every task list", () => {
			send("task.comment.added", { task_id: "t1" });
			expect(keys()).not.toContainEqual(["projects", "proj-abc", "tasks"]);
			const predicates = mocks.invalidateQueries.mock.calls
				.map(
					(c) =>
						(c[0] as { predicate?: (q: { queryKey: unknown[] }) => boolean })
							.predicate,
				)
				.filter(Boolean);
			expect(
				predicates.some((p) =>
					p?.({
						queryKey: ["projects", "proj-abc", "tasks", "t1", "activities"],
					}),
				),
			).toBe(true);
			expect(
				predicates.some((p) =>
					p?.({
						queryKey: ["projects", "proj-abc", "tasks", "t2", "activities"],
					}),
				),
			).toBe(false);
		});

		it("refreshes only that doc's timeline on a doc comment", () => {
			send("doc.comment.added", { document_id: "d1" });
			expect(keys()).toContainEqual([
				"projects",
				"proj-abc",
				"docs",
				"d1",
				"activities",
			]);
			expect(keys()).not.toContainEqual(["projects", "proj-abc", "docs"]);
		});

		it("falls back to the broad refresh when a comment names no task", () => {
			send("task.comment.added", {});
			expect(keys()).toContainEqual(["projects", "proj-abc", "tasks"]);
		});

		it("patches a sprint change that carries the sprint, and moves tasks only when it completes", () => {
			const change = (id = "s1") => ({
				entityType: "sprint",
				entityId: id,
				entity: { id, name: "S", status: "active" },
			});
			send("sprint.updated", {}, change());
			expect(mocks.setQueriesData).toHaveBeenCalled();
			expect(keys()).not.toContainEqual(["projects", "proj-abc", "sprints"]);
			expect(keys()).not.toContainEqual(["projects", "proj-abc", "tasks"]);
		});

		it("still refreshes everything for a sprint change with no snapshot", () => {
			send("sprint.updated", {});
			expect(keys()).toContainEqual(["projects", "proj-abc", "sprints"]);
		});

		it("refreshes only the moved view's positions on view.task_moved", () => {
			send("view.task_moved", { view_id: "v1" });
			expect(keys()).toContainEqual([
				"projects",
				"proj-abc",
				"views",
				"v1",
				"task-positions",
			]);
			expect(keys()).not.toContainEqual(["projects", "proj-abc", "views"]);
		});

		it("refreshes only the view lists on view.reordered", () => {
			send("view.reordered", { view_context: "backlog" });
			const predicate = mocks.invalidateQueries.mock.calls
				.map(
					(c) =>
						(c[0] as { predicate?: (q: { queryKey: unknown[] }) => boolean })
							.predicate,
				)
				.find(Boolean);
			expect(
				predicate?.({
					queryKey: ["projects", "proj-abc", "views", { context: "backlog" }],
				}),
			).toBe(true);
			expect(
				predicate?.({
					queryKey: ["projects", "proj-abc", "views", "v1", "task-positions"],
				}),
			).toBe(false);
		});
	});

	describe("project schema, settings, members and agents", () => {
		function send(type: string, payload: Record<string, unknown> = {}) {
			renderHook(() => useProjectRealtime("proj-abc"));
			const [, listener] = mocks.socket.on.mock.calls[0] as [
				string,
				(event: { type: string; payload: Record<string, unknown> }) => void,
			];
			listener({ type, payload });
		}
		const keys = () =>
			mocks.invalidateQueries.mock.calls.map(
				(c) => (c[0] as { queryKey?: unknown[] }).queryKey,
			);

		it("refreshes the task statuses on project.task_status.*", () => {
			send("project.task_status.updated");
			expect(keys()).toContainEqual(["projects", "proj-abc", "task-statuses"]);
			// A status edit can change what counts as done in subtask counts.
			expect(keys()).toContainEqual(["projects", "proj-abc", "tasks"]);
		});

		it("does not refetch tasks for a reorder or a rename of a task type", () => {
			send("project.task_status.reordered");
			send("project.task_type.updated");
			expect(keys()).not.toContainEqual(["projects", "proj-abc", "tasks"]);
		});

		it("refreshes only the custom fields when one is created", () => {
			send("project.custom_field.created");
			expect(keys()).toContainEqual(["projects", "proj-abc", "custom-fields"]);
			expect(keys()).not.toContainEqual(["projects", "proj-abc", "tasks"]);
		});

		it("refreshes the task types on project.task_type.*", () => {
			send("project.task_type.deleted");
			expect(keys()).toContainEqual(["projects", "proj-abc", "task-types"]);
		});

		it("refreshes the project itself, not everything under it, on project.updated", () => {
			send("project.updated");
			expect(mocks.invalidateQueries).toHaveBeenCalledWith({
				queryKey: ["projects", "proj-abc"],
				exact: true,
			});
			expect(keys()).not.toContainEqual(["projects", "proj-abc", "tasks"]);
		});

		it("refreshes the member and agent lookups other clients' changes would leave stale", () => {
			send("member.updated");
			expect(mocks.invalidateMemberDirectory).toHaveBeenCalledWith("proj-abc");
			mocks.invalidateMemberDirectory.mockClear();
			send("project_agent.updated");
			expect(mocks.invalidateAgentDirectory).toHaveBeenCalledWith("proj-abc");
			expect(mocks.invalidateMemberDirectory).toHaveBeenCalledWith("proj-abc");
		});

		it("patches an environment's status from the event, refetching only the one being viewed", () => {
			send("environment.status_changed", {
				environment_id: "e1",
				status: "running",
			});
			expect(mocks.setQueryData).toHaveBeenCalled();
			expect(mocks.setQueriesData).toHaveBeenCalled();
			expect(keys()).not.toContainEqual([
				"projects",
				"proj-abc",
				"environments",
			]);
			expect(mocks.invalidateQueries).toHaveBeenCalledWith({
				queryKey: ["projects", "proj-abc", "environments", "e1"],
				exact: true,
				refetchType: "active",
			});
		});

		it("falls back to the broad refresh for an environment event with no id", () => {
			send("environment.status_changed", { status: "running" });
			expect(keys()).toContainEqual(["projects", "proj-abc", "environments"]);
		});

		it("refreshes members and roles on member.*", () => {
			send("member.role_changed");
			expect(keys()).toContainEqual(["projects", "proj-abc", "members"]);
			expect(keys()).toContainEqual(["projects", "proj-abc", "roles"]);
		});

		it("refreshes agents and members on project_agent.*", () => {
			send("project_agent.created");
			expect(keys()).toContainEqual(["projects", "proj-abc", "agents"]);
			expect(keys()).toContainEqual(["projects", "proj-abc", "members"]);
		});
	});

	it("registers a connect listener on mount", () => {
		renderHook(() => useProjectRealtime("proj-abc"));

		expect(mocks.socket.on).toHaveBeenCalledWith(
			"connect",
			expect.any(Function),
		);
	});

	it("re-joins the project room when the socket reconnects, without re-registering as a subscriber", () => {
		renderHook(() => useProjectRealtime("proj-abc"));

		mocks.joinProject.mockClear();

		const [, onConnect] = mocks.socket.on.mock.calls.find(
			([event]) => event === "connect",
		) as [string, () => void];
		onConnect();

		expect(mocks.rejoinProject).toHaveBeenCalledWith("proj-abc");
		// Must not call joinProject again — that would bump the reference
		// count that leaveProject decrements on unmount, since a reconnect
		// isn't a new subscriber.
		expect(mocks.joinProject).not.toHaveBeenCalled();
	});

	it("removes the connect listener on unmount", () => {
		const { unmount } = renderHook(() => useProjectRealtime("proj-abc"));

		const [, onConnect] = mocks.socket.on.mock.calls.find(
			([event]) => event === "connect",
		) as [string, () => void];

		unmount();

		expect(mocks.socket.off).toHaveBeenCalledWith("connect", onConnect);
	});

	it("re-joins and re-registers when projectId changes", () => {
		const { rerender } = renderHook(
			({ projectId }) => useProjectRealtime(projectId),
			{ initialProps: { projectId: "proj-1" } },
		);

		expect(mocks.joinProject).toHaveBeenCalledWith("proj-1");

		rerender({ projectId: "proj-2" });

		// Old project should have been left.
		expect(mocks.leaveProject).toHaveBeenCalledWith("proj-1");
		// New project joined.
		expect(mocks.joinProject).toHaveBeenCalledWith("proj-2");
	});
	it("appends a full persisted event to the tail cache instead of refetching", () => {
		renderHook(() => useProjectRealtime("proj-abc"));
		const [, listener] = mocks.socket.on.mock.calls[0] as [
			string,
			(event: { type: string; payload: Record<string, unknown> }) => void,
		];

		listener({
			type: "agent.tool_call",
			payload: {
				id: "evt-1",
				conversation_id: "conv-1",
				event_index: 42,
				event_type: "tool_call",
				event_source: "agent",
				payload: { toolCallId: "tc-1", title: "Running ls" },
				created_at: "2026-01-01T00:00:00Z",
			},
		});

		// persistAndPublish (services/agent-runner) puts the full row on the
		// realtime message now — appended straight into the tail cache, no
		// GET .../events round trip needed.
		expect(mocks.setQueryData).toHaveBeenCalled();
		expect(mocks.invalidateQueries).not.toHaveBeenCalled();
	});

	it("does not refetch the conversation list or detail for a legacy index-only event", () => {
		renderHook(() => useProjectRealtime("proj-abc"));
		const [, listener] = mocks.socket.on.mock.calls[0] as [
			string,
			(event: { type: string; payload: Record<string, unknown> }) => void,
		];

		// No `id` — an event_index alone still counts as "persisted" (so the
		// reconciling-fetch path below stays reserved for genuine status
		// notifications), it just has nothing to append.
		listener({
			type: "agent.acptoolcallevent",
			payload: { conversation_id: "conv-1", event_index: "42" },
		});

		expect(mocks.setQueryData).toHaveBeenCalled();
		expect(mocks.invalidateQueries).not.toHaveBeenCalled();
	});

	it("refetches the conversations prefix for a lifecycle event", () => {
		renderHook(() => useProjectRealtime("proj-abc"));
		const [, listener] = mocks.socket.on.mock.calls[0] as [
			string,
			(event: { type: string; payload: Record<string, unknown> }) => void,
		];

		listener({
			type: "agent.conversation.finished",
			payload: { conversation_id: "conv-1" },
		});

		expect(mocks.invalidateQueries).toHaveBeenCalledWith({
			queryKey: ["projects", "proj-abc", "conversations"],
		});
	});

	describe("permission changes reported by the server", () => {
		function namespaceListener() {
			const [listener] = mocks.onProjectNamespaces.mock.calls[0] as [
				(change: {
					projectId: string;
					namespaces: string[];
					removed: string[];
					added: string[];
				}) => void,
			];
			return listener;
		}

		it("refetches the caller's permissions, and nothing else, when no namespace was lost", () => {
			renderHook(() => useProjectRealtime("proj-abc"));
			mocks.invalidateQueries.mockClear();

			namespaceListener()({
				projectId: "proj-abc",
				namespaces: ["tasks", "docs"],
				removed: [],
				added: [],
			});

			const keys = mocks.invalidateQueries.mock.calls.map(
				([arg]) => arg.queryKey,
			);
			expect(keys).toEqual([
				["projects", "proj-abc", "members", "me", "permissions"],
				["auth", "me", "permissions"],
			]);
		});

		// The predicate resetQueries was called with, for a given loss.
		function lostPredicate(
			removed: string[],
			namespaces: string[] = ["tasks"],
		) {
			renderHook(() => useProjectRealtime("proj-abc"));
			mocks.resetQueries.mockClear();
			namespaceListener()({
				projectId: "proj-abc",
				namespaces,
				removed,
				added: [],
			});
			expect(mocks.resetQueries).toHaveBeenCalledTimes(1);
			const [arg] = mocks.resetQueries.mock.calls[0] as [
				{ predicate: (q: { queryKey: unknown[] }) => boolean },
			];
			return (...key: unknown[]) => arg.predicate({ queryKey: key });
		}

		it("resets only the data of the namespace that was lost, so refused data does not stay on screen", () => {
			const lost = lostPredicate(["docs"]);
			expect(lost("projects", "proj-abc", "docs", "list")).toBe(true);
			expect(lost("projects", "proj-abc", "agentActivities")).toBe(true);
			// What the member still has is left alone.
			expect(lost("projects", "proj-abc", "tasks", "list")).toBe(false);
			expect(lost("projects", "proj-abc", "sprints", "s1")).toBe(false);
			expect(lost("projects", "proj-abc", "environments")).toBe(false);
		});

		it("never resets the permission queries, which were just invalidated", () => {
			const lost = lostPredicate(["tasks", "docs"]);
			expect(lost("projects", "proj-abc", "members", "me", "permissions")).toBe(
				false,
			);
			expect(lost("auth", "me", "permissions")).toBe(false);
		});

		it("resets the task data cached under sprints and views when tasks are lost", () => {
			const lost = lostPredicate(["tasks"], ["docs"]);
			expect(lost("projects", "proj-abc", "tasks", "t1")).toBe(true);
			expect(lost("projects", "proj-abc", "sprints", "s1", "tasks")).toBe(true);
			expect(
				lost("projects", "proj-abc", "views", "v1", "task-positions"),
			).toBe(true);
			// A sprint or a view itself is not task data.
			expect(lost("projects", "proj-abc", "sprints", "s1")).toBe(false);
			expect(lost("projects", "proj-abc", "views", "v1", "config")).toBe(false);
		});

		it("covers every namespace's own data, and several lost at once", () => {
			const lost = lostPredicate([
				"workflows",
				"environments",
				"conversations",
			]);
			expect(lost("projects", "proj-abc", "automations")).toBe(true);
			expect(lost("projects", "proj-abc", "automation-dependency-map")).toBe(
				true,
			);
			expect(lost("projects", "proj-abc", "environments", "e1")).toBe(true);
			expect(lost("projects", "proj-abc", "conversations", "c1")).toBe(true);
			expect(lost("projects", "proj-abc", "docs")).toBe(false);
		});

		it("leaves project queries outside the lost namespace's roots alone", () => {
			const lost = lostPredicate(["tasks"], ["docs"]);
			expect(lost("projects", "proj-abc", "settings")).toBe(false);
			expect(lost("projects", "proj-abc", "labels")).toBe(false);
			expect(lost("projects", "proj-abc")).toBe(false);
		});

		it("matches the keys the real query options build, so a renamed key fails here", async () => {
			const { viewTaskPositionsQueryOptions } = await import(
				"@/lib/interaction-api"
			);
			const lost = lostPredicate(["tasks"], ["docs"]);
			const { queryKey } = viewTaskPositionsQueryOptions("proj-abc", "v1");
			expect(lost(...queryKey)).toBe(true);
		});

		it("does not touch another project's queries", () => {
			const lost = lostPredicate(["tasks"], ["docs"]);
			expect(lost("projects", "other", "tasks", "t1")).toBe(false);
		});

		it("errs towards safety for a namespace it does not know, still sparing the permissions", () => {
			const lost = lostPredicate(["somethingNew"]);
			expect(lost("projects", "proj-abc", "tasks", "t1")).toBe(true);
			expect(lost("projects", "proj-abc", "docs")).toBe(true);
			expect(lost("projects", "proj-abc", "members", "me", "permissions")).toBe(
				false,
			);
		});

		it("refetches a gained namespace's data, which may be sitting in the error state, and nothing else", () => {
			renderHook(() => useProjectRealtime("proj-abc"));
			mocks.invalidateQueries.mockClear();
			mocks.resetQueries.mockClear();

			namespaceListener()({
				projectId: "proj-abc",
				namespaces: ["tasks", "docs"],
				removed: [],
				added: ["tasks"],
			});

			expect(mocks.resetQueries).not.toHaveBeenCalled();
			const withPredicate = mocks.invalidateQueries.mock.calls
				.map(([arg]) => arg)
				.find((arg) => typeof arg.predicate === "function");
			expect(withPredicate).toBeDefined();
			const gained = (...key: unknown[]) =>
				withPredicate.predicate({ queryKey: key });
			expect(gained("projects", "proj-abc", "tasks", "t1")).toBe(true);
			expect(gained("projects", "proj-abc", "sprints", "s1", "tasks")).toBe(
				true,
			);
			expect(gained("projects", "proj-abc", "docs")).toBe(false);
			expect(
				gained("projects", "proj-abc", "members", "me", "permissions"),
			).toBe(false);
		});

		it("does not refetch data when nothing was gained or lost", () => {
			renderHook(() => useProjectRealtime("proj-abc"));
			mocks.invalidateQueries.mockClear();

			namespaceListener()({
				projectId: "proj-abc",
				namespaces: ["tasks"],
				removed: [],
				added: [],
			});

			expect(
				mocks.invalidateQueries.mock.calls.filter(
					([arg]) => typeof arg.predicate === "function",
				),
			).toEqual([]);
		});

		it("only invalidates, never resets, when nothing was lost", () => {
			renderHook(() => useProjectRealtime("proj-abc"));
			mocks.resetQueries.mockClear();

			namespaceListener()({
				projectId: "proj-abc",
				namespaces: ["tasks", "docs"],
				removed: [],
				added: [],
			});

			expect(mocks.resetQueries).not.toHaveBeenCalled();
		});

		it("ignores a report about another project", () => {
			renderHook(() => useProjectRealtime("proj-abc"));
			mocks.invalidateQueries.mockClear();

			namespaceListener()({
				projectId: "other",
				namespaces: [],
				removed: ["tasks"],
				added: [],
			});

			expect(mocks.invalidateQueries).not.toHaveBeenCalled();
		});

		it("stops listening on unmount", () => {
			const { unmount } = renderHook(() => useProjectRealtime("proj-abc"));
			unmount();
			expect(mocks.unsubscribeNamespaces).toHaveBeenCalled();
		});
	});

	describe("after a reconnect", () => {
		function handlerFor(event: string) {
			const call = mocks.socket.on.mock.calls.find(([name]) => name === event);
			return call?.[1] as (...args: unknown[]) => void;
		}
		function ack(projectId = "proj-abc") {
			const [listener] = mocks.onProjectNamespaces.mock.calls[0] as [
				(c: {
					projectId: string;
					namespaces: string[];
					removed: string[];
					added: string[];
				}) => void,
			];
			listener({ projectId, namespaces: ["tasks"], removed: [], added: [] });
		}
		const projectRefetches = () =>
			mocks.invalidateQueries.mock.calls.filter(
				([arg]) =>
					arg.queryKey.length === 2 &&
					arg.queryKey[0] === "projects" &&
					arg.queryKey[1] === "proj-abc",
			).length;

		it("does not refetch when the rejoin resumed the change feed", () => {
			mocks.socket.connected = true;
			renderHook(() => useProjectRealtime("proj-abc"));
			handlerFor("connect")();
			mocks.invalidateQueries.mockClear();
			const [listener] = mocks.onProjectNamespaces.mock.calls[0] as [
				(c: unknown) => void,
			];
			listener({
				projectId: "proj-abc",
				namespaces: ["tasks"],
				removed: [],
				added: [],
				resumed: true,
			});
			expect(projectRefetches()).toBe(0);
		});

		it("handles changes the sync feed delivers for this project only", () => {
			renderHook(() => useProjectRealtime("proj-abc"));
			const [feed] = mocks.onProjectChange.mock.calls[0] as [
				(projectId: string, e: unknown) => void,
			];
			feed("other", { type: "task.created", payload: {} });
			expect(mocks.invalidateQueries).not.toHaveBeenCalled();
			feed("proj-abc", { type: "task.created", payload: {} });
			expect(mocks.invalidateQueries).toHaveBeenCalledWith({
				queryKey: ["projects", "proj-abc", "tasks"],
			});
		});

		it("patches a task change that carries the task instead of invalidating the task prefix", () => {
			renderHook(() => useProjectRealtime("proj-abc"));
			const [feed] = mocks.onProjectChange.mock.calls[0] as [
				(projectId: string, e: unknown) => void,
			];
			feed("proj-abc", {
				type: "task.updated",
				payload: { content: JSON.stringify({ changes: [{ field: "title" }] }) },
				change: {
					entityType: "task",
					entityId: "t1",
					taskId: "t1",
					entity: { id: "t1", title: "New" },
				},
			});
			expect(mocks.invalidateQueries).not.toHaveBeenCalledWith({
				queryKey: ["projects", "proj-abc", "tasks"],
			});
			expect(mocks.setQueryData).toHaveBeenCalled();
		});

		it("refetches the project when the feed reports a reset", () => {
			renderHook(() => useProjectRealtime("proj-abc"));
			const [onReset] = mocks.onProjectReset.mock.calls[0] as [
				(projectId: string) => void,
			];
			onReset("proj-abc");
			expect(projectRefetches()).toBe(1);
		});

		it("rejoins and refetches the project only once the rejoin is acked", () => {
			mocks.socket.connected = true;
			renderHook(() => useProjectRealtime("proj-abc"));
			mocks.invalidateQueries.mockClear();
			mocks.rejoinProject.mockClear();

			handlerFor("connect")();
			expect(mocks.rejoinProject).toHaveBeenCalledWith("proj-abc");
			expect(projectRefetches()).toBe(0); // not before the join is acked

			ack();
			expect(projectRefetches()).toBe(1);
			ack(); // a later permissions report must not refetch again
			expect(projectRefetches()).toBe(1);
		});

		it("does not refetch on the first connect", () => {
			mocks.socket.connected = false;
			renderHook(() => useProjectRealtime("proj-abc"));
			mocks.invalidateQueries.mockClear();

			handlerFor("connect")();
			ack();

			expect(projectRefetches()).toBe(0);
		});
	});
});
