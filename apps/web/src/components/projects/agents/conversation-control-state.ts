import type { ConversationStatus } from "@/lib/agent-api";

const TERMINAL_STATUSES = new Set<ConversationStatus>([
	"finished",
	"failed",
	"stopped",
]);

// The durable stop action is available for every non-terminal conversation,
// including ACP conversations whose composer Cancel action only stops the
// current response.
export function shouldShowConversationStop(
	status: ConversationStatus,
): boolean {
	return !TERMINAL_STATUSES.has(status);
}
