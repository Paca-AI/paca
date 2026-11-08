import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

// Build a minimal mock socket that tracks calls.
function createMockSocket() {
	return {
		connected: false,
		connect: vi.fn(),
		disconnect: vi.fn(),
		emit: vi.fn(),
		on: vi.fn(),
		off: vi.fn(),
		// socket.timeout(ms) returns an emitter whose ack callback takes
		// (err, response); the mock hands the emit through unchanged.
		timeout: vi.fn(),
	};
}

const { mockSocketFactory, mockSocket } = vi.hoisted(() => {
	const socket = createMockSocket();
	return {
		mockSocketFactory: vi.fn(() => socket),
		mockSocket: socket,
	};
});

vi.mock("socket.io-client", () => ({
	io: mockSocketFactory,
}));

const { refreshSession } = vi.hoisted(() => ({ refreshSession: vi.fn() }));
vi.mock("./api-client", () => ({ apiClient: { refreshSession } }));

// Import after mocking so the module-level `io` call uses our mock.
import {
	connectSocket,
	disconnectSocket,
	getProjectNamespaces,
	getSocket,
	joinProject,
	leaveProject,
	onProjectNamespaces,
	rejoinProject,
} from "./socket-client";

type Ack = (err: Error | null, res?: unknown) => void;

/** The handler the module registered for a server message. */
function handlerFor(event: string) {
	const call = mockSocket.on.mock.calls.find(([name]) => name === event);
	if (!call) throw new Error(`no ${event} handler registered`);
	return call[1] as (...args: unknown[]) => void;
}

/** The ack callback of the most recent join emit. */
function lastJoinAck(): Ack {
	const calls = mockSocket.emit.mock.calls.filter(([name]) => name === "join");
	const last = calls[calls.length - 1];
	if (!last) throw new Error("no join emitted");
	return last[2] as Ack;
}

describe("socket-client", () => {
	beforeEach(() => {
		// Reset mock state before each test.
		vi.clearAllMocks();
		mockSocket.connected = false;
		mockSocket.timeout.mockReturnValue({ emit: mockSocket.emit });
		// Ensure internal singleton is cleared between tests.
		disconnectSocket();
	});

	afterEach(() => {
		disconnectSocket();
	});

	describe("connectSocket", () => {
		it("creates a new socket on first call", () => {
			const socket = connectSocket();

			expect(mockSocketFactory).toHaveBeenCalledTimes(1);
			expect(socket).toBe(mockSocket);
		});

		it("reuses the existing socket when already connected", () => {
			mockSocket.connected = true;

			connectSocket();
			const second = connectSocket();

			expect(mockSocketFactory).toHaveBeenCalledTimes(1);
			expect(second).toBe(mockSocket);
		});

		it("calls connect() on an existing disconnected socket instead of creating a new one", () => {
			// First call creates the socket.
			connectSocket();
			expect(mockSocketFactory).toHaveBeenCalledTimes(1);

			// Simulate disconnected state (connected = false, socket exists).
			mockSocket.connected = false;

			// Second call should reuse and call connect().
			connectSocket();
			expect(mockSocketFactory).toHaveBeenCalledTimes(1);
			expect(mockSocket.connect).toHaveBeenCalledTimes(1);
		});
	});

	describe("disconnectSocket", () => {
		it("disconnects and nullifies the socket", () => {
			connectSocket();
			disconnectSocket();

			expect(mockSocket.disconnect).toHaveBeenCalledTimes(1);
			expect(getSocket()).toBeNull();
		});

		it("is a no-op when no socket exists", () => {
			// Should not throw.
			expect(() => disconnectSocket()).not.toThrow();
		});
	});

	describe("getSocket", () => {
		it("returns null when no socket is created", () => {
			expect(getSocket()).toBeNull();
		});

		it("returns the current socket after connecting", () => {
			connectSocket();
			expect(getSocket()).toBe(mockSocket);
		});
	});

	describe("joinProject", () => {
		it("emits a join event with the projectId", () => {
			connectSocket();
			joinProject("proj-123");

			expect(mockSocket.emit).toHaveBeenCalledWith(
				"join",
				{ projectId: "proj-123" },
				expect.any(Function),
			);
		});

		it("is a no-op when no socket is connected", () => {
			// No connectSocket() call — socket is null.
			joinProject("proj-123");
			expect(mockSocket.emit).not.toHaveBeenCalled();
		});
	});

	describe("leaveProject", () => {
		it("emits a leave event with the projectId", () => {
			connectSocket();
			leaveProject("proj-123");

			expect(mockSocket.emit).toHaveBeenCalledWith("leave", {
				projectId: "proj-123",
			});
		});

		it("is a no-op when no socket is connected", () => {
			leaveProject("proj-123");
			expect(mockSocket.emit).not.toHaveBeenCalled();
		});

		it("does not emit leave while another subscriber is still joined (reference counting)", () => {
			connectSocket();
			// Two independent callers join the same project (e.g. the project
			// layout and a nested conversations layout).
			joinProject("proj-123");
			joinProject("proj-123");
			mockSocket.emit.mockClear();

			// First caller unmounts and leaves — the second is still joined,
			// so the socket must stay in the project's rooms.
			leaveProject("proj-123");
			expect(mockSocket.emit).not.toHaveBeenCalledWith(
				"leave",
				expect.anything(),
			);

			// Second caller leaves too — now it should actually emit leave.
			leaveProject("proj-123");
			expect(mockSocket.emit).toHaveBeenCalledWith("leave", {
				projectId: "proj-123",
			});
		});

		it("tracks subscriber counts independently per projectId", () => {
			connectSocket();
			joinProject("proj-a");
			joinProject("proj-b");
			mockSocket.emit.mockClear();

			leaveProject("proj-a");
			expect(mockSocket.emit).toHaveBeenCalledWith("leave", {
				projectId: "proj-a",
			});
			expect(mockSocket.emit).not.toHaveBeenCalledWith("leave", {
				projectId: "proj-b",
			});
		});
	});

	describe("rejoinProject", () => {
		it("emits a join event without affecting the subscriber count", () => {
			connectSocket();
			joinProject("proj-123");
			mockSocket.emit.mockClear();

			// Simulate a socket reconnect re-joining an already-subscribed
			// project — this must not require a second leaveProject call to
			// actually release the room.
			rejoinProject("proj-123");
			expect(mockSocket.emit).toHaveBeenCalledWith(
				"join",
				{ projectId: "proj-123" },
				expect.any(Function),
			);

			mockSocket.emit.mockClear();
			leaveProject("proj-123");
			expect(mockSocket.emit).toHaveBeenCalledWith("leave", {
				projectId: "proj-123",
			});
		});

		it("is a no-op when no socket is connected", () => {
			rejoinProject("proj-123");
			expect(mockSocket.emit).not.toHaveBeenCalled();
		});
	});

	describe("join acknowledgement", () => {
		it("records the namespaces the server granted and tells listeners", () => {
			connectSocket();
			const changes: unknown[] = [];
			const off = onProjectNamespaces((c) => changes.push(c));

			joinProject("proj-1");
			expect(getProjectNamespaces("proj-1")).toBeUndefined();
			lastJoinAck()(null, {
				projectId: "proj-1",
				namespaces: ["tasks", "docs"],
			});

			expect(getProjectNamespaces("proj-1")).toEqual(["tasks", "docs"]);
			expect(changes).toEqual([
				{
					projectId: "proj-1",
					namespaces: ["tasks", "docs"],
					removed: [],
					added: [],
				},
			]);
			off();
		});

		it("treats a non-member ack as no namespaces rather than a failure", () => {
			connectSocket();
			joinProject("proj-1");
			lastJoinAck()(null, { projectId: "proj-1", namespaces: [] });
			expect(getProjectNamespaces("proj-1")).toEqual([]);
		});

		it("ignores an ack that arrives after the last subscriber left", () => {
			connectSocket();
			joinProject("proj-1");
			const ack = lastJoinAck();
			leaveProject("proj-1");
			ack(null, { projectId: "proj-1", namespaces: ["tasks"] });
			expect(getProjectNamespaces("proj-1")).toBeUndefined();
		});

		describe("a join the server never acks", () => {
			beforeEach(() => vi.useFakeTimers());
			afterEach(() => vi.useRealTimers());

			it("is retried, a bounded number of times", () => {
				connectSocket();
				mockSocket.connected = true;
				joinProject("proj-1");
				const joins = () =>
					mockSocket.emit.mock.calls.filter(([n]) => n === "join").length;
				expect(joins()).toBe(1);

				lastJoinAck()(new Error("operation has timed out"));
				vi.advanceTimersByTime(5_000);
				expect(joins()).toBe(2);

				lastJoinAck()(new Error("operation has timed out"));
				vi.advanceTimersByTime(5_000);
				expect(joins()).toBe(3);

				lastJoinAck()(new Error("operation has timed out"));
				vi.advanceTimersByTime(60_000);
				expect(joins()).toBe(3);
			});

			it("is not retried once the subscriber has left", () => {
				connectSocket();
				mockSocket.connected = true;
				joinProject("proj-1");
				lastJoinAck()(new Error("operation has timed out"));
				leaveProject("proj-1");
				vi.advanceTimersByTime(60_000);
				expect(
					mockSocket.emit.mock.calls.filter(([n]) => n === "join").length,
				).toBe(1);
			});
		});
	});

	describe("permissions:changed", () => {
		it("reports the namespaces the socket lost", () => {
			connectSocket();
			const changes: Array<{ removed: string[]; added: string[] }> = [];
			const off = onProjectNamespaces((c) => changes.push(c));
			joinProject("proj-1");
			lastJoinAck()(null, {
				projectId: "proj-1",
				namespaces: ["tasks", "docs"],
			});

			handlerFor("permissions:changed")({
				projectId: "proj-1",
				namespaces: ["tasks"],
			});

			expect(getProjectNamespaces("proj-1")).toEqual(["tasks"]);
			expect(changes.at(-1)?.removed).toEqual(["docs"]);
			off();
		});

		it("reports the namespaces the socket gained, but not on the first report", () => {
			connectSocket();
			const changes: Array<{ removed: string[]; added: string[] }> = [];
			const off = onProjectNamespaces((c) => changes.push(c));
			joinProject("proj-1");
			lastJoinAck()(null, { projectId: "proj-1", namespaces: ["tasks"] });
			// The first report is the initial load: nothing to refetch.
			expect(changes.at(-1)?.added).toEqual([]);

			handlerFor("permissions:changed")({
				projectId: "proj-1",
				namespaces: ["tasks", "docs"],
			});
			expect(changes.at(-1)?.added).toEqual(["docs"]);
			expect(changes.at(-1)?.removed).toEqual([]);

			// Losing and regaining: the regain is reported as gained.
			handlerFor("permissions:changed")({
				projectId: "proj-1",
				namespaces: ["docs"],
			});
			handlerFor("permissions:changed")({
				projectId: "proj-1",
				namespaces: ["docs", "tasks"],
			});
			expect(changes.at(-1)?.added).toEqual(["tasks"]);
			off();
		});

		it("stops notifying an unsubscribed listener", () => {
			connectSocket();
			const listener = vi.fn();
			onProjectNamespaces(listener)();
			joinProject("proj-1");
			lastJoinAck()(null, { projectId: "proj-1", namespaces: ["tasks"] });
			expect(listener).not.toHaveBeenCalled();
		});
	});

	describe("reauth", () => {
		it("refreshes the session, then reconnects so the handshake carries the new cookie", async () => {
			connectSocket();
			mockSocket.connected = true;
			mockSocket.disconnect.mockReturnValue(mockSocket);
			let done: (() => void) | undefined;
			refreshSession.mockReturnValue(
				new Promise<void>((resolve) => {
					done = resolve;
				}),
			);

			handlerFor("reauth")();
			expect(mockSocket.disconnect).not.toHaveBeenCalled(); // not before the refresh
			done?.();
			await vi.waitFor(() => expect(mockSocket.connect).toHaveBeenCalled());
			expect(mockSocket.disconnect).toHaveBeenCalledTimes(1);
		});

		it("leaves the socket alone when the refresh fails (the session is over)", async () => {
			connectSocket();
			mockSocket.connected = true;
			refreshSession.mockRejectedValue(new Error("expired"));

			handlerFor("reauth")();
			await Promise.resolve();
			await Promise.resolve();

			expect(mockSocket.disconnect).not.toHaveBeenCalled();
			expect(mockSocket.connect).not.toHaveBeenCalled();
		});
	});

	describe("recovery from a refused handshake", () => {
		it("refreshes the session and reconnects after connect_error", async () => {
			vi.useFakeTimers();
			try {
				refreshSession.mockResolvedValue(undefined);
				connectSocket();
				mockSocket.connect.mockClear();

				handlerFor("connect_error")(new Error("invalid or expired token"));
				await vi.advanceTimersByTimeAsync(1_000);

				expect(refreshSession).toHaveBeenCalledTimes(1);
				expect(mockSocket.connect).toHaveBeenCalledTimes(1);
			} finally {
				vi.useRealTimers();
			}
		});
	});
});
