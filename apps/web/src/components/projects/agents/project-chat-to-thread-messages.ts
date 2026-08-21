import type { ThreadMessageLike } from "@assistant-ui/react";
import type { ProjectChatTurnHistoryItem } from "@/lib/agent-api";

export interface ProjectChatMessageOptions {
	terminalFallback: (item: ProjectChatTurnHistoryItem) => string;
}

/**
 * Builds the user-visible session transcript exclusively from authoritative
 * turns and turn results. Runs/events are deliberately ignored here: they are
 * diagnostic execution records and can contain partial or superseded output.
 */
export function projectChatTurnsToThreadMessages(
	items: ProjectChatTurnHistoryItem[],
	options: ProjectChatMessageOptions,
): ThreadMessageLike[] {
	const messages: ThreadMessageLike[] = [];
	const ordered = [...items].sort(
		(a, b) => a.turn.turn_index - b.turn.turn_index,
	);

	for (const item of ordered) {
		messages.push({
			id: `${item.turn.id}:user`,
			role: "user",
			createdAt: new Date(item.turn.created_at),
			content: [{ type: "text", text: item.turn.input_text }],
		});

		const result = item.result;
		if (!result) continue;

		const text =
			result.terminal_status === "succeeded" && result.stable_output
				? result.stable_output
				: options.terminalFallback(item);
		if (!text.trim()) continue;

		messages.push({
			id: `${item.turn.id}:assistant`,
			role: "assistant",
			createdAt: new Date(result.created_at),
			status:
				result.terminal_status === "succeeded"
					? { type: "complete", reason: "stop" }
					: {
							type: "incomplete",
							reason:
								result.terminal_status === "stopped" ||
								result.terminal_status === "cancelled"
									? "cancelled"
									: "error",
							error: {
								terminal_status: result.terminal_status,
								error_code: result.error_code ?? null,
							},
						},
			content: [{ type: "text", text }],
		});
	}

	return messages;
}

export function isProjectChatTurnActive(
	item: ProjectChatTurnHistoryItem | undefined,
): boolean {
	return item?.turn.status === "queued" || item?.turn.status === "running";
}

export function canPublishProjectChatConclusion(
	item: ProjectChatTurnHistoryItem,
): boolean {
	return (
		item.turn.status === "succeeded" &&
		item.result?.terminal_status === "succeeded" &&
		!!item.result.stable_output?.trim()
	);
}
