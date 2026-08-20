import type { ConversationStatus } from "@/lib/agent-api";

const TERMINAL_STATUSES = new Set<ConversationStatus>([
	"finished",
	"failed",
	"stopped",
]);

// The permanent stop action is available for every non-terminal conversation,
// including ACP conversations whose composer Cancel action only interrupts the
// current turn.
export function shouldShowPermanentStop(status: ConversationStatus): boolean {
	return !TERMINAL_STATUSES.has(status);
}
