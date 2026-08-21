import { renderHook } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

// ── Hoisted mocks ─────────────────────────────────────────────────────────────

const mocks = vi.hoisted(() => {
	const socket = {
		on: vi.fn(),
		off: vi.fn(),
	};

	return {
		socket,
		connectSocket: vi.fn(() => socket),
		joinProject: vi.fn(),
		leaveProject: vi.fn(),
		rejoinProject: vi.fn(),
		invalidateQueries: vi.fn(),
		setQueryData: vi.fn(),
	};
});

vi.mock("@/lib/socket-client", () => ({
	connectSocket: mocks.connectSocket,
	joinProject: mocks.joinProject,
	leaveProject: mocks.leaveProject,
	rejoinProject: mocks.rejoinProject,
}));

vi.mock("@tanstack/react-query", async () => {
	const actual = await vi.importActual<typeof import("@tanstack/react-query")>(
		"@tanstack/react-query",
	);
	return {
		...actual,
		useQueryClient: () => ({
			invalidateQueries: mocks.invalidateQueries,
			setQueryData: mocks.setQueryData,
		}),
	};
});

import { useProjectRealtime } from "./use-project-realtime";

// ── Tests ─────────────────────────────────────────────────────────────────────

describe("useProjectRealtime", () => {
	beforeEach(() => {
		vi.clearAllMocks();
		mocks.connectSocket.mockReturnValue(mocks.socket);
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

	it("invalidates task github query key on github.branch.linked events", () => {
		renderHook(() => useProjectRealtime("proj-abc"));

		const [, listener] = mocks.socket.on.mock.calls[0] as [
			string,
			(event: { type: string; payload: Record<string, unknown> }) => void,
		];

		listener({
			type: "github.branch.linked",
			payload: { task_id: "task-42", project_id: "proj-abc" },
		});

		expect(mocks.invalidateQueries).toHaveBeenCalledWith({
			queryKey: ["projects", "proj-abc", "tasks", "task-42", "github"],
		});
	});

	it("invalidates task github query key on github.pr.linked events", () => {
		renderHook(() => useProjectRealtime("proj-abc"));

		const [, listener] = mocks.socket.on.mock.calls[0] as [
			string,
			(event: { type: string; payload: Record<string, unknown> }) => void,
		];

		listener({
			type: "github.pr.linked",
			payload: { task_id: "task-42", project_id: "proj-abc", pr_number: 7 },
		});

		expect(mocks.invalidateQueries).toHaveBeenCalledWith({
			queryKey: ["projects", "proj-abc", "tasks", "task-42", "github"],
		});
	});

	it("does not invalidate when github.* event has no task_id", () => {
		renderHook(() => useProjectRealtime("proj-abc"));

		const [, listener] = mocks.socket.on.mock.calls[0] as [
			string,
			(event: { type: string; payload: Record<string, unknown> }) => void,
		];

		listener({ type: "github.branch.linked", payload: {} });

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

	it("refetches authoritative session and turn results on agent.turn.finished", () => {
		renderHook(() => useProjectRealtime("proj-abc"));
		const [, listener] = mocks.socket.on.mock.calls[0] as [
			string,
			(event: { type: string; payload: Record<string, unknown> }) => void,
		];

		listener({
			type: "agent.turn.finished",
			payload: {
				project_id: "proj-abc",
				session_id: "session-1",
				turn_id: "turn-1",
				status: "succeeded",
				stable_output: "must never be accepted from realtime",
			},
		});

		expect(mocks.invalidateQueries).toHaveBeenCalledWith({
			queryKey: ["projects", "proj-abc", "chat-sessions"],
		});
		expect(mocks.invalidateQueries).toHaveBeenCalledWith({
			queryKey: ["projects", "proj-abc", "chat-sessions", "session-1", "turns"],
		});
		expect(mocks.setQueryData).not.toHaveBeenCalled();
	});

	it("ignores authoritative Chat events for another project", () => {
		renderHook(() => useProjectRealtime("proj-abc"));
		const [, listener] = mocks.socket.on.mock.calls[0] as [
			string,
			(event: { type: string; payload: Record<string, unknown> }) => void,
		];

		listener({
			type: "agent.turn.finished",
			payload: { project_id: "proj-other", session_id: "session-1" },
		});

		expect(mocks.invalidateQueries).not.toHaveBeenCalled();
		expect(mocks.setQueryData).not.toHaveBeenCalled();
	});

	it("refetches a task's publication and activity projection", () => {
		renderHook(() => useProjectRealtime("proj-abc"));
		const [, listener] = mocks.socket.on.mock.calls[0] as [
			string,
			(event: { type: string; payload: Record<string, unknown> }) => void,
		];

		listener({
			type: "agent.conclusion.revised",
			payload: {
				project_id: "proj-abc",
				target_task_id: "task-1",
				publication_id: "publication-2",
			},
		});

		expect(mocks.invalidateQueries).toHaveBeenCalledWith({
			queryKey: ["projects", "proj-abc", "tasks", "task-1"],
		});
		expect(mocks.setQueryData).not.toHaveBeenCalled();
	});
});
