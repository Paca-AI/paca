import { useCallback, useEffect, useMemo, useState } from "react";
import type { AgentConversationEvent } from "@/lib/agent-api";
import { type ContextItem, toWireContextItems } from "@/lib/context-items";

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
			return (typeof text === "string" ? text : "") === pending.text;
		});
	}, [events, pending]);

	useEffect(() => {
		if (persisted) setPendingMessage(null);
	}, [persisted]);

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

	const begin = useCallback<UseOptimisticUserMessage["begin"]>(
		({ text, contextItems }) => {
			setPendingMessage({
				conversationId,
				text,
				contextItems,
				baseIndex: events.at(-1)?.event_index ?? -1,
				createdAt: new Date().toISOString(),
			});
		},
		[conversationId, events],
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
			event_index: lastIndex + 1,
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
