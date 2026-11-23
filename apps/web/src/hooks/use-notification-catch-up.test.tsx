import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { renderHook } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

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

import {
	NOTIFICATIONS_POLL_INTERVAL_MS,
	useNotificationCatchUp,
} from "./use-notification-catch-up";

function connectHandler() {
	const call = mockSocket.on.mock.calls.find(([name]) => name === "connect");
	if (!call) throw new Error("no connect handler");
	return call[1] as () => void;
}

function setup(enabled = true) {
	const queryClient = new QueryClient();
	const invalidate = vi.spyOn(queryClient, "invalidateQueries");
	const wrapper = ({ children }: { children: ReactNode }) => (
		<QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
	);
	const view = renderHook(() => useNotificationCatchUp(enabled), { wrapper });
	return { invalidate, view };
}

describe("useNotificationCatchUp", () => {
	beforeEach(() => {
		vi.clearAllMocks();
		mockSocket.connected = false;
		mockSocket.active = true;
		state.exists = true;
	});

	it("does not refetch on the initial connect", () => {
		const { invalidate } = setup();
		connectHandler()();
		expect(invalidate).not.toHaveBeenCalled();
	});

	it("refetches the inbox after a reconnect", () => {
		const { invalidate } = setup();
		connectHandler()();
		connectHandler()();
		expect(invalidate).toHaveBeenCalledWith({ queryKey: ["notifications"] });
	});

	it("refetches on the first connect of a socket that mounted down", () => {
		mockSocket.active = false;
		const { invalidate } = setup();
		connectHandler()();
		expect(invalidate).toHaveBeenCalledWith({ queryKey: ["notifications"] });
	});

	it("refetches when the socket was already connected at mount and reconnects", () => {
		mockSocket.connected = true;
		const { invalidate } = setup();
		connectHandler()();
		expect(invalidate).toHaveBeenCalledWith({ queryKey: ["notifications"] });
	});

	it("does nothing while disabled", () => {
		setup(false);
		expect(mockSocket.on).not.toHaveBeenCalled();
	});

	it("stops listening on unmount", () => {
		const { view } = setup();
		const handler = connectHandler();
		view.unmount();
		expect(mockSocket.off).toHaveBeenCalledWith("connect", handler);
	});
});

describe("useNotificationCatchUp polling", () => {
	beforeEach(() => {
		vi.clearAllMocks();
		vi.useFakeTimers();
	});
	afterEach(() => {
		vi.useRealTimers();
		vi.restoreAllMocks();
	});

	function visibility(state: DocumentVisibilityState) {
		vi.spyOn(document, "visibilityState", "get").mockReturnValue(state);
	}

	it("re-reads the inbox every interval while the tab is visible", () => {
		visibility("visible");
		const { invalidate } = setup();
		vi.advanceTimersByTime(NOTIFICATIONS_POLL_INTERVAL_MS * 2);
		expect(invalidate).toHaveBeenCalledTimes(2);
		expect(invalidate).toHaveBeenCalledWith({ queryKey: ["notifications"] });
	});

	it("does not poll a hidden tab", () => {
		visibility("hidden");
		const { invalidate } = setup();
		vi.advanceTimersByTime(NOTIFICATIONS_POLL_INTERVAL_MS * 3);
		expect(invalidate).not.toHaveBeenCalled();
	});

	it("stops polling on unmount", () => {
		visibility("visible");
		const { invalidate, view } = setup();
		view.unmount();
		vi.advanceTimersByTime(NOTIFICATIONS_POLL_INTERVAL_MS * 2);
		expect(invalidate).not.toHaveBeenCalled();
	});
});
