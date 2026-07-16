import type { ThreadMessageLike } from "@assistant-ui/react";
import type { AgentConversationEvent } from "@/lib/agent-api";

// Extract plain text from a content block array [{type:"text", text:"..."}] or a bare string.
export function extractContentText(content: unknown): string | null {
	if (Array.isArray(content)) {
		const parts = (content as Array<unknown>)
			.map((c) => {
				if (typeof c === "object" && c !== null && "text" in c) {
					const t = (c as { text: unknown }).text;
					return typeof t === "string" ? t : null;
				}
				return null;
			})
			.filter((t): t is string => t !== null && t.trim().length > 0);
		return parts.length > 0 ? parts.join("\n\n").trim() : null;
	}
	if (typeof content === "string" && content.trim().length > 0) {
		return content.trim();
	}
	return null;
}

type MutableToolCallPart = {
	type: "tool-call";
	toolCallId: string;
	toolName: string;
	argsText: string;
	result?: unknown;
	isError?: boolean;
};

type MutablePart =
	| { type: "text"; text: string }
	| { type: "reasoning"; text: string }
	| MutableToolCallPart;

interface InProgressMessage {
	id: string;
	createdAt: Date;
	parts: MutablePart[];
	// Keyed by tool_call_id so a later ObservationEvent can attach its result
	// to the ActionEvent's tool-call part within the same turn.
	openToolCalls: Map<string, MutableToolCallPart>;
}

function startAssistantMessage(id: string, createdAt: Date): InProgressMessage {
	return { id, createdAt, parts: [], openToolCalls: new Map() };
}

function toThreadMessage(
	msg: InProgressMessage,
	isTrailingAndRunning: boolean,
): ThreadMessageLike {
	return {
		id: msg.id,
		role: "assistant",
		createdAt: msg.createdAt,
		status: isTrailingAndRunning
			? { type: "running" }
			: { type: "complete", reason: "stop" },
		content: msg.parts,
	};
}

/**
 * Converts raw conversation events into assistant-ui's ThreadMessageLike[],
 * grouping each agent turn's thought/tool-calls/reply into one assistant
 * message's content parts (rather than one bubble per raw event) — the
 * `reasoning`/`tool-call` part types are auto-grouped into collapsible
 * sections by the Thread component's `MessagePrimitive.GroupedParts`.
 *
 * `isThreadRunning` marks the trailing in-progress assistant message (if
 * any) as still running so the Thread shows its "working" indicator.
 */
export function eventsToThreadMessages(
	events: AgentConversationEvent[],
	isThreadRunning: boolean,
): ThreadMessageLike[] {
	const messages: ThreadMessageLike[] = [];
	let current: InProgressMessage | null = null;

	const flushCurrent = () => {
		if (current && current.parts.length > 0) {
			messages.push(toThreadMessage(current, false));
		}
		current = null;
	};

	for (const ev of events) {
		const p = ev.payload;
		const t = ev.event_type;

		// Non-user-visible bookkeeping events — already filtered server-side,
		// skipped here too as a defensive guard against legacy/unfiltered data.
		if (
			t === "ConversationStateUpdateEvent" ||
			t === "SystemPromptEvent" ||
			t === "StreamingDeltaEvent"
		) {
			continue;
		}

		if (t === "MessageEvent") {
			const llmMsg = p.llm_message as { content?: unknown } | undefined;
			const text =
				extractContentText(llmMsg?.content) ?? extractContentText(p.content);
			if (!text) continue;

			if (ev.event_source === "user") {
				flushCurrent();
				messages.push({
					id: ev.id,
					role: "user",
					createdAt: new Date(ev.created_at),
					content: [{ type: "text", text }],
				});
				continue;
			}

			// Agent's natural-language reply — part of the current turn.
			if (!current)
				current = startAssistantMessage(ev.id, new Date(ev.created_at));
			current.parts.push({ type: "text", text });
			continue;
		}

		if (t === "ActionEvent") {
			if (!current)
				current = startAssistantMessage(ev.id, new Date(ev.created_at));

			const thoughtText = extractContentText(p.thought);
			if (thoughtText)
				current.parts.push({ type: "reasoning", text: thoughtText });

			const toolCall = p.tool_call as
				| { name?: string; arguments?: unknown }
				| undefined;
			const toolCallId =
				typeof p.tool_call_id === "string" ? p.tool_call_id : ev.id;
			const toolName =
				(typeof p.tool_name === "string" ? p.tool_name : undefined) ??
				toolCall?.name ??
				"tool";

			// Every turn (both regular Agent and ACPAgent conversations) ends
			// with a synthetic "finish" tool call whose action carries the
			// agent's actual natural-language reply in `message` — render it
			// as reply text, not a tool-call card, or it shows up as an
			// opaque "finish" bubble instead of the agent's answer. The
			// matching ObservationEvent is skipped below since this already
			// surfaces the same text.
			if (toolName === "finish") {
				const action = p.action as { message?: unknown } | undefined;
				const finishText =
					typeof action?.message === "string" ? action.message : null;
				if (finishText) current.parts.push({ type: "text", text: finishText });
				continue;
			}

			const argsText =
				(typeof toolCall?.arguments === "string" ? toolCall.arguments : null) ??
				JSON.stringify(toolCall?.arguments ?? toolCall ?? {}, null, 2);

			const part: MutableToolCallPart = {
				type: "tool-call",
				toolCallId,
				toolName,
				argsText,
			};
			current.parts.push(part);
			current.openToolCalls.set(toolCallId, part);
			continue;
		}

		// ObservationEvent (tool succeeded), AgentErrorEvent (tool/scaffold
		// error), and UserRejectObservation (user/hook rejected the call) are
		// all responses to a tool call — each carries tool_call_id and must
		// resolve the matching open tool-call part, or it's left "running"
		// forever with no indication anything happened.
		if (
			t === "ObservationEvent" ||
			t === "AgentErrorEvent" ||
			t === "UserRejectObservation"
		) {
			// The matching ActionEvent already rendered the "finish" tool
			// call's message as reply text — its observation just repeats
			// the same text, so skip it rather than emitting a duplicate
			// tool-call card.
			if (p.tool_name === "finish") continue;

			const isError = t !== "ObservationEvent";
			const obs = p.observation as Record<string, unknown> | undefined;
			const resultText =
				(obs &&
					(extractContentText(obs.content) ??
						(typeof obs.message === "string" ? obs.message : null))) ??
				extractContentText(p.content) ??
				(typeof p.error === "string" ? p.error : null) ??
				(typeof p.rejection_reason === "string" ? p.rejection_reason : null) ??
				(typeof p.message === "string" ? p.message : null) ??
				(typeof p.output === "string" ? p.output : null) ??
				"";

			const toolCallId =
				typeof p.tool_call_id === "string" ? p.tool_call_id : undefined;
			const openPart =
				toolCallId && current
					? current.openToolCalls.get(toolCallId)
					: undefined;

			if (openPart) {
				openPart.result = resultText;
				if (isError) openPart.isError = true;
			} else {
				// No matching open tool-call in this turn (history gap) — append
				// a standalone, already-complete tool-call part.
				if (!current)
					current = startAssistantMessage(ev.id, new Date(ev.created_at));
				const toolName = typeof p.tool_name === "string" ? p.tool_name : "tool";
				current.parts.push({
					type: "tool-call",
					toolCallId: toolCallId ?? ev.id,
					toolName,
					argsText: "",
					result: resultText,
					...(isError ? { isError: true } : {}),
				});
			}
			continue;
		}

		// ACPToolCallEvent — emitted by ACP-type agents in place of the
		// ActionEvent/ObservationEvent pair for every tool call the local
		// ACP CLI (Claude Code/Codex/Gemini CLI) makes. Unlike that pair,
		// ACP streams multiple updates (start, progress, terminal) for the
		// *same* tool_call_id as one call progresses, so the part is
		// created once and mutated in place on each subsequent update
		// rather than matched against a separately-typed observation event.
		if (t === "ACPToolCallEvent") {
			if (!current)
				current = startAssistantMessage(ev.id, new Date(ev.created_at));

			const toolCallId =
				typeof p.tool_call_id === "string" ? p.tool_call_id : ev.id;
			const toolName =
				(typeof p.title === "string" ? p.title : null) ??
				(typeof p.tool_kind === "string" ? p.tool_kind : null) ??
				"tool";
			const rawInput = p.raw_input;
			const argsText =
				typeof rawInput === "string"
					? rawInput
					: rawInput !== undefined && rawInput !== null
						? JSON.stringify(rawInput, null, 2)
						: "";

			let part = current.openToolCalls.get(toolCallId);
			if (!part) {
				part = { type: "tool-call", toolCallId, toolName, argsText };
				current.parts.push(part);
				current.openToolCalls.set(toolCallId, part);
			} else {
				part.toolName = toolName;
				if (argsText) part.argsText = argsText;
			}

			const status = typeof p.status === "string" ? p.status : null;
			if (status === "completed" || status === "failed") {
				const rawOutput = p.raw_output;
				const resultText =
					extractContentText(p.content) ??
					(typeof rawOutput === "string"
						? rawOutput
						: rawOutput !== undefined && rawOutput !== null
							? JSON.stringify(rawOutput)
							: null) ??
					"";
				part.result = resultText;
				if (p.is_error === true) part.isError = true;
			}
			continue;
		}

		// Fallback for other/legacy event types: surface as plain text so
		// nothing silently disappears from the transcript.
		const fallbackText =
			extractContentText(p.content) ??
			extractContentText(p.thought) ??
			(typeof p.message === "string" ? p.message : null);
		if (fallbackText) {
			if (ev.event_source === "user") {
				flushCurrent();
				messages.push({
					id: ev.id,
					role: "user",
					createdAt: new Date(ev.created_at),
					content: [{ type: "text", text: fallbackText }],
				});
			} else {
				if (!current)
					current = startAssistantMessage(ev.id, new Date(ev.created_at));
				current.parts.push({ type: "text", text: fallbackText });
			}
		}
	}

	if (current && current.parts.length > 0) {
		messages.push(toThreadMessage(current, isThreadRunning));
	}

	return messages;
}
