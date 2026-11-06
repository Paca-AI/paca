import { act, renderHook } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { AgentConversationEvent } from "@/lib/agent-api";
import {
	OPTIMISTIC_MESSAGE_TTL_MS,
	useOptimisticUserMessage,
} from "./use-optimistic-user-message";

const userEvent = (index: number, text: string): AgentConversationEvent => ({
	id: `e${index}`,
	conversation_id: "c1",
	event_index: index,
	event_type: "user_message",
	event_source: "user",
	payload: { content: { type: "text", text } },
	created_at: "2026-01-01T00:00:00Z",
});

describe("useOptimisticUserMessage", () => {
	afterEach(() => vi.useRealTimers());

	it("shows the message at once and drops it when the persisted event arrives", () => {
		const old = [userEvent(0, "hi")];
		const { result, rerender } = renderHook(
			({ events }) => useOptimisticUserMessage(events, "c1"),
			{ initialProps: { events: old } },
		);
		expect(result.current.pending).toBe(false);

		act(() => result.current.begin({ text: "hi", contextItems: [] }));
		expect(result.current.pending).toBe(true);
		// Same text as an older turn must not count as the echo.
		expect(result.current.events).toHaveLength(2);

		rerender({ events: [...old, userEvent(1, "hi")] });
		expect(result.current.pending).toBe(false);
		expect(result.current.events).toHaveLength(2);
	});

	it("matches the persisted echo of a whitespace-padded message", () => {
		const { result, rerender } = renderHook(
			({ events }) => useOptimisticUserMessage(events, "c1"),
			{ initialProps: { events: [] as AgentConversationEvent[] } },
		);
		act(() => result.current.begin({ text: "  hello\n", contextItems: [] }));
		expect(result.current.pending).toBe(true);
		rerender({ events: [userEvent(0, "hello")] });
		expect(result.current.pending).toBe(false);
		expect(result.current.events).toHaveLength(1);
	});

	it("discards on a failed send", () => {
		const { result } = renderHook(() => useOptimisticUserMessage([], "c1"));
		act(() => result.current.begin({ text: "x", contextItems: [] }));
		act(() => result.current.discard());
		expect(result.current.events).toHaveLength(0);
	});

	it("drops the overlay after the TTL if no echo ever matches", () => {
		vi.useFakeTimers();
		const { result } = renderHook(() => useOptimisticUserMessage([], "c1"));
		act(() => result.current.begin({ text: "x", contextItems: [] }));
		expect(result.current.pending).toBe(true);
		act(() => {
			vi.advanceTimersByTime(OPTIMISTIC_MESSAGE_TTL_MS + 1);
		});
		expect(result.current.pending).toBe(false);
		expect(result.current.events).toHaveLength(0);
	});

	it("re-scopes to the new conversation on follow and drops on a foreign one", () => {
		const { result, rerender } = renderHook(
			({ id }) => useOptimisticUserMessage([], id),
			{ initialProps: { id: "c1" } },
		);
		act(() => result.current.begin({ text: "x", contextItems: [] }));
		act(() => result.current.follow("c2"));
		rerender({ id: "c2" });
		expect(result.current.pending).toBe(true);
		rerender({ id: "c3" });
		expect(result.current.pending).toBe(false);
	});

	it("matches an echo whose inner whitespace differs", () => {
		const { result, rerender } = renderHook(
			({ events }) => useOptimisticUserMessage(events, "c1"),
			{ initialProps: { events: [] as AgentConversationEvent[] } },
		);
		act(() => result.current.begin({ text: "a\n\nb", contextItems: [] }));
		rerender({ events: [userEvent(0, "a b")] });
		expect(result.current.pending).toBe(false);
	});

	it("keeps the overlay past the TTL while the conversation is queued", () => {
		vi.useFakeTimers();
		const { result, rerender } = renderHook(
			({ hold }) => useOptimisticUserMessage([], "c1", hold),
			{ initialProps: { hold: true } },
		);
		act(() => result.current.begin({ text: "x", contextItems: [] }));
		act(() => {
			vi.advanceTimersByTime(OPTIMISTIC_MESSAGE_TTL_MS * 2);
		});
		expect(result.current.pending).toBe(true);
		rerender({ hold: false });
		// A full TTL starts when the hold lifts, so the echo still has time.
		act(() => {
			vi.advanceTimersByTime(OPTIMISTIC_MESSAGE_TTL_MS - 1);
		});
		expect(result.current.pending).toBe(true);
		act(() => {
			vi.advanceTimersByTime(1);
		});
		expect(result.current.pending).toBe(false);
	});
});
