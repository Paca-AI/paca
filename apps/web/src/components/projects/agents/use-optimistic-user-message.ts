import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import type { AgentConversationEvent } from "@/lib/agent-api";
import { type ContextItem, toWireContextItems } from "@/lib/context-items";

/** Longest the overlay may outlive a send whose echo never matches (the
 *  server rewrote the text, or never persisted the turn). */
export const OPTIMISTIC_MESSAGE_TTL_MS = 60_000;

const normalize = (text: string) => text.replace(/\s+/g, " ").trim();

type PendingMessage = {
	conversationId: string | null;
	text: string;
	contextItems: ContextItem[];
	/** Highest event_index on screen when the message was sent: only a
	 *  `user_message` past it can be this message's persisted echo. */
	baseIndex: number;
	createdAt: string;
};

export type UseOptimisticUserMessage = {
	/** `events` plus the not-yet-persisted message, if any. */
	events: AgentConversationEvent[];
	/** True from send until the server's own `user_message` event arrives. */
	pending: boolean;
	begin: (message: { text: string; contextItems: ContextItem[] }) => void;
	/** Drops the optimistic message (the send failed or was cancelled). */
	discard: () => void;
	/** Re-scopes the overlay to the conversation the reply actually landed in
	 *  (a reply to an ended chat starts a fresh one server-side). */
	follow: (conversationId: string) => void;
};

/**
 * The server only persists a turn's `user_message` once agent-runner picks the
 * turn up, which can be seconds after the send request returned. Without this
 * the sender sees nothing happen in the meantime. This overlays the message
 * on the event list at send time, and drops the overlay as soon as the real
 * event (same text, past the index the list had at send time) shows up, so
 * the transcript never shows it twice.
 *
 * `conversationId` scopes the overlay to the conversation it was sent in.
 */
export function useOptimisticUserMessage(
	events: AgentConversationEvent[],
	conversationId: string | null,
	/** True while the echo is legitimately delayed (a queued conversation
	 *  waiting for a slot): the safety-net timeout is held off meanwhile. */
	holdTimeout = false,
): UseOptimisticUserMessage {
	const [pendingMessage, setPendingMessage] = useState<PendingMessage | null>(
		null,
	);

	const pending =
		pendingMessage &&
		(pendingMessage.conversationId === conversationId ||
			pendingMessage.conversationId === null)
			? pendingMessage
			: null;

	const persisted = useMemo(() => {
		if (!pending) return false;
		return events.some((e) => {
			if (e.event_type !== "user_message") return false;
			if (e.event_index <= pending.baseIndex) return false;
			const content = e.payload.content;
			const text =
				typeof content === "object" && content !== null
					? (content as Record<string, unknown>).text
					: undefined;
			return (
				normalize(typeof text === "string" ? text : "") ===
				normalize(pending.text)
			);
		});
	}, [events, pending]);

	useEffect(() => {
		if (persisted) setPendingMessage(null);
	}, [persisted]);

	// Safety net: without it a mismatched echo would leave a duplicate message
	// and a permanent "thinking" state.
	const startedAt = pendingMessage?.createdAt;
	useEffect(() => {
		if (!startedAt || holdTimeout) return;
		// Counted from when the hold lifts, not from the send: the echo is only
		// persisted shortly after a queued conversation starts running, so a
		// long queue wait must not leave the timer already expired.
		const timer = setTimeout(
			() => setPendingMessage(null),
			OPTIMISTIC_MESSAGE_TTL_MS,
		);
		return () => clearTimeout(timer);
	}, [startedAt, holdTimeout]);

	// A different conversation's overlay must not linger.
	useEffect(() => {
		setPendingMessage((prev) =>
			prev &&
			prev.conversationId !== null &&
			prev.conversationId !== conversationId
				? null
				: prev,
		);
	}, [conversationId]);

	// Read through a ref so `begin` keeps one identity across event updates.
	const lastIndexRef = useRef(-1);
	lastIndexRef.current = events.at(-1)?.event_index ?? -1;

	const begin = useCallback<UseOptimisticUserMessage["begin"]>(
		({ text, contextItems }) => {
			setPendingMessage({
				conversationId,
				// The server persists the trimmed text; match and render that.
				text: text.trim(),
				contextItems,
				baseIndex: lastIndexRef.current,
				createdAt: new Date().toISOString(),
			});
		},
		[conversationId],
	);

	const discard = useCallback(() => setPendingMessage(null), []);
	const follow = useCallback(
		(id: string) =>
			setPendingMessage((prev) =>
				prev ? { ...prev, conversationId: id, baseIndex: -1 } : prev,
			),
		[],
	);

	const withPending = useMemo(() => {
		if (!pending || persisted) return events;
		const lastIndex = events.at(-1)?.event_index ?? -1;
		const optimistic: AgentConversationEvent = {
			id: "optimistic-user-message",
			conversation_id: conversationId ?? "",
			// Fractional so it can't collide with the real event that lands next.
			event_index: lastIndex + 0.5,
			event_type: "user_message",
			event_source: "user",
			payload: {
				content: { type: "text", text: pending.text },
				context_items: toWireContextItems(pending.contextItems),
			},
			created_at: pending.createdAt,
		};
		return [...events, optimistic];
	}, [events, pending, persisted, conversationId]);

	return {
		events: withPending,
		pending: pending !== null && !persisted,
		begin,
		discard,
		follow,
	};
}
