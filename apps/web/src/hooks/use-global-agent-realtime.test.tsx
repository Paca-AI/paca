import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { renderHook } from "@testing-library/react";
import type { ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";

const { mockSocket, state } = vi.hoisted(() => ({
	mockSocket: {
		connected: false,
		active: true,
		on: vi.fn(),
		off: vi.fn(),
	},
	state: { exists: true },
}));

vi.mock("@/lib/socket-client", () => ({
	getSocket: () => (state.exists ? mockSocket : null),
	// Like the real one, re-subscribes an existing socket (active -> true).
	connectSocket: () => {
		mockSocket.active = true;
		state.exists = true;
		return mockSocket;
	},
}));

import { useGlobalAgentRealtime } from "./use-global-agent-realtime";

function handlerFor(event: string) {
	const call = mockSocket.on.mock.calls.find(([name]) => name === event);
	if (!call) throw new Error(`no ${event} handler`);
	return call[1] as () => void;
}

function setup() {
	const queryClient = new QueryClient();
	const invalidate = vi.spyOn(queryClient, "invalidateQueries");
	const wrapper = ({ children }: { children: ReactNode }) => (
		<QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
	);
	renderHook(() => useGlobalAgentRealtime(), { wrapper });
	return invalidate;
}

describe("useGlobalAgentRealtime reconnect", () => {
	beforeEach(() => {
		vi.clearAllMocks();
		mockSocket.connected = false;
		mockSocket.active = true;
		state.exists = true;
	});

	it("does not refetch on the initial connect", () => {
		const invalidate = setup();
		handlerFor("connect")();
		expect(invalidate).not.toHaveBeenCalled();
	});

	it("refetches conversation events after a reconnect", () => {
		const invalidate = setup();
		handlerFor("connect")();
		handlerFor("connect")();
		expect(invalidate).toHaveBeenCalledWith({
			queryKey: ["conversation-event-window"],
		});
		expect(invalidate).toHaveBeenCalledWith({
			queryKey: ["global-chat", "conversations"],
		});
	});

	it("treats the first connect of a mounted-down socket as a reconnect", () => {
		mockSocket.active = false;
		const invalidate = setup();
		expect(mockSocket.active).toBe(true); // connectSocket() re-subscribed it
		handlerFor("connect")();
		expect(invalidate).toHaveBeenCalledWith({
			queryKey: ["conversation-event-window"],
		});
	});
});
