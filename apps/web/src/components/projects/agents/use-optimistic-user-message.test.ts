import { act, renderHook } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import type { AgentConversationEvent } from "@/lib/agent-api";
import { useOptimisticUserMessage } from "./use-optimistic-user-message";

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

	it("discards on a failed send", () => {
		const { result } = renderHook(() => useOptimisticUserMessage([], "c1"));
		act(() => result.current.begin({ text: "x", contextItems: [] }));
		act(() => result.current.discard());
		expect(result.current.events).toHaveLength(0);
	});
});
