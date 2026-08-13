import { AlertTriangle } from "lucide-react";

/**
 * A conversation's error, rendered as a card inside the message flow itself
 * — not a page footer, which reads as small print easy to miss entirely
 * below the composer. Purely a client-side rendering of the already-fetched
 * `conversation.error_message` field; nothing here is persisted as a
 * conversation event.
 *
 * Meant for `Thread`'s `viewportOverlay` slot, which renders inside the
 * scrollable message column right after the last message — so this reads
 * as part of the conversation, not chrome around it.
 */
export function ConversationErrorBox({ message }: { message: string }) {
	return (
		<div className="mb-4 flex items-start gap-2.5 rounded-xl border border-destructive/20 bg-destructive/5 px-3.5 py-3">
			<AlertTriangle className="size-4 shrink-0 text-destructive mt-0.5" />
			<p className="text-sm text-destructive wrap-break-word">{message}</p>
		</div>
	);
}
