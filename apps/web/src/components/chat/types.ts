/**
 * Message model rendered by the chat Thread. Deliberately small: the agent
 * conversation is event-sourced and converted to these by
 * `projects/agents/conversation-to-thread-messages.ts`.
 */

/** Diff blocks attached to a tool call (ACP edit/write calls). */
export type ToolCallArtifact = { diffs: unknown[] };

export type ChatTextPart = { type: "text"; text: string };
export type ChatReasoningPart = { type: "reasoning"; text: string };
export type ChatToolCallPart = {
	type: "tool-call";
	toolCallId: string;
	toolName: string;
	argsText: string;
	result?: unknown;
	isError?: boolean;
	artifact?: ToolCallArtifact;
};

export type ChatPart = ChatTextPart | ChatReasoningPart | ChatToolCallPart;

export type ChatMessage = {
	id: string;
	role: "user" | "assistant";
	createdAt?: Date;
	/** Only meaningful on assistant messages: still being produced. */
	status?: "running" | "complete";
	content: readonly ChatPart[];
	/** Free-form app data (attached context items on user messages). */
	metadata?: { custom?: Record<string, unknown> };
};

/** Lifecycle of a tool call as the cards render it. */
export type ToolCallStatus =
	| { type: "running" }
	| { type: "complete" }
	| { type: "incomplete"; reason: "cancelled" | "error"; error?: unknown };

/**
 * A tool call is running while its message is and no result has arrived. A
 * result-less call in a finished message was cut short (run stopped/failed).
 */
export function toolCallStatus(
	part: ChatToolCallPart,
	messageRunning: boolean,
): ToolCallStatus {
	if (part.result === undefined) {
		return messageRunning
			? { type: "running" }
			: { type: "incomplete", reason: "cancelled" };
	}
	return { type: "complete" };
}
