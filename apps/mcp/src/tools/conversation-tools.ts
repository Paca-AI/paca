import type { Tool } from "@modelcontextprotocol/sdk/types.js";
import { z } from "zod";
import type { PacaAPIConversationClient } from "../api/index.js";
import type {
	AgentConversation,
	AgentConversationEvent,
	ConversationEventPage,
	ConversationEventWindow,
} from "../types/index.js";

// ── Zod schema ────────────────────────────────────────────────────────────────

const ReadConversationSchema = z
	.object({
		conversationId: z.string(),
		after: z.string().optional(),
		before: z.string().optional(),
		limit: z.number().int().min(1).max(200).optional(),
	})
	.refine((data) => !(data.after && data.before), {
		message: "after and before are mutually exclusive",
		path: ["before"],
	});

// ── Tool definition ────────────────────────────────────────────────────────────

export function getConversationTools(): Tool[] {
	return [
		{
			name: "read_conversation",
			description:
				"Read another conversation's transcript — for example one attached as chat context (an '## Attached Context' section naming a Conversation ID). " +
				"Works for any conversation you (this agent) participated in, whether it happened in a project or as a global chat — you don't need to know or specify which. " +
				"Returns a condensed, skimmable transcript ('User: ...' / 'Agent: ...' lines, plus one-line '[tool: name] ...' summaries for tool calls — not full arguments or results) rather than a pixel-perfect replay of the UI. " +
				"Events are paginated: the response reports how many of the conversation's total events are shown, and — if more exist — which cursor to pass back (after/before) on a follow-up call to read further.",
			inputSchema: {
				type: "object",
				properties: {
					conversationId: {
						type: "string",
						description: "The technical UUID of the conversation to read.",
					},
					after: {
						type: "string",
						description:
							"Opaque cursor from a previous call's response — resumes forward toward newer events from that point. Mutually exclusive with before. Omit both to start at the most recent events.",
					},
					before: {
						type: "string",
						description:
							"Opaque cursor from a previous call's response — resumes backward toward older events from that point. Mutually exclusive with after.",
					},
					limit: {
						type: "number",
						description:
							"Max events to return in this page (1-200, default 50).",
					},
				},
				required: ["conversationId"],
			},
		},
	];
}

// ── Event → text rendering ──────────────────────────────────────────────────────
//
// A simplified re-derivation of apps/web's
// components/projects/agents/conversation-to-thread-messages.ts for plain-text
// output: that file groups a turn's parts into rich ThreadMessageLike objects
// for the UI (with diff artifacts, streaming state, etc.); this just needs
// flat lines of text an LLM can skim, so tool calls are condensed to one-line
// summaries instead of full args/results. Covers the current ACP-based event
// vocabulary (user_message/agent_message_chunk/agent_thought_chunk/tool_call/
// tool_call_update) plus the legacy OpenHands-style shapes
// (MessageEvent/ActionEvent/ObservationEvent/AgentErrorEvent/
// UserRejectObservation/ACPToolCallEvent) older conversation history may
// still contain.

function oneLine(text: string): string {
	return text.replace(/\s+/g, " ").trim();
}

function truncate(text: string, max = 200): string {
	return text.length > max ? `${text.slice(0, max)}…` : text;
}

function condense(text: string, max = 200): string {
	return truncate(oneLine(text), max);
}

function extractBlockText(block: unknown): string | null {
	if (typeof block !== "object" || block === null) return null;
	const text = (block as Record<string, unknown>).text;
	return typeof text === "string" && text.length > 0 ? text : null;
}

function extractContentText(content: unknown): string | null {
	if (Array.isArray(content)) {
		const parts = content
			.map((c) => {
				if (typeof c === "object" && c !== null && "text" in c) {
					const t = (c as { text: unknown }).text;
					return typeof t === "string" ? t : null;
				}
				return null;
			})
			.filter((t): t is string => t !== null && t.trim().length > 0);
		return parts.length > 0 ? parts.join(" ").trim() : null;
	}
	if (typeof content === "string" && content.trim().length > 0) {
		return content.trim();
	}
	return null;
}

function extractToolCallUpdateText(content: unknown): string | null {
	if (!Array.isArray(content)) return null;
	const parts = content
		.map((c) => {
			if (typeof c !== "object" || c === null) return null;
			return extractBlockText((c as Record<string, unknown>).content);
		})
		.filter((t): t is string => t !== null);
	return parts.length > 0 ? parts.join("") : null;
}

// Non-user-visible bookkeeping events, never rendered as a transcript line.
const SKIPPED_EVENT_TYPES = new Set([
	"ConversationStateUpdateEvent",
	"SystemPromptEvent",
	"StreamingDeltaEvent",
	"environment_ready",
	"turn_end",
	"turn_usage",
]);

/**
 * Renders a page of conversation events into flat transcript lines, oldest
 * first. Exported for testing.
 */
export function formatTranscriptLines(
	events: AgentConversationEvent[],
): string[] {
	const lines: string[] = [];
	let agentBuf = "";
	let thoughtBuf = "";
	// Maps an open tool call's id to the line it's rendered on, so a later
	// update/observation event can append its result to that same line
	// instead of appearing as an unrelated entry further down.
	const openToolCalls = new Map<string, number>();

	const flushAgent = () => {
		if (agentBuf) {
			lines.push(`Agent: ${oneLine(agentBuf)}`);
			agentBuf = "";
		}
	};
	const flushThought = () => {
		if (thoughtBuf) {
			lines.push(`Agent (thinking): ${condense(thoughtBuf)}`);
			thoughtBuf = "";
		}
	};
	const flushAll = () => {
		flushAgent();
		flushThought();
	};

	for (const ev of events) {
		const p = ev.payload ?? {};
		const t = ev.event_type;

		if (SKIPPED_EVENT_TYPES.has(t)) continue;

		// user_message is written once per turn with the user's actual message
		// (see services/agent-runner's handler.go) — ACP itself has no
		// equivalent event.
		if (t === "user_message") {
			const text = extractBlockText(p.content);
			if (!text) continue;
			flushAll();
			lines.push(`User: ${oneLine(text)}`);
			continue;
		}

		// Chunks stream one reply/thought piece by piece — buffered and only
		// flushed once a different kind of event interrupts the run, so a
		// streamed reply renders as one line instead of dozens of fragments.
		if (t === "agent_message_chunk") {
			const text = extractBlockText(p.content);
			if (text) agentBuf += text;
			continue;
		}
		if (t === "agent_thought_chunk") {
			const text = extractBlockText(p.content);
			if (text) thoughtBuf += text;
			continue;
		}

		if (t === "tool_call") {
			flushAll();
			const toolCallId =
				typeof p.toolCallId === "string" ? p.toolCallId : ev.id;
			const toolName = typeof p.title === "string" ? p.title : "tool";
			lines.push(`[tool: ${toolName}] called`);
			openToolCalls.set(toolCallId, lines.length - 1);
			continue;
		}

		if (t === "tool_call_update") {
			const toolCallId =
				typeof p.toolCallId === "string" ? p.toolCallId : undefined;
			const status = typeof p.status === "string" ? p.status : null;
			const resultText = extractToolCallUpdateText(p.content);
			const idx = toolCallId ? openToolCalls.get(toolCallId) : undefined;
			const failed = status === "failed";
			const summary = resultText
				? condense(resultText, 160)
				: (status ?? "done");
			if (idx !== undefined) {
				lines[idx] = `${lines[idx]} -> ${failed ? "ERROR: " : ""}${summary}`;
			} else {
				lines.push(`[tool] ${failed ? "ERROR: " : ""}${summary}`);
			}
			continue;
		}

		// --- Legacy (pre-ACP) OpenHands-style events -----------------------

		if (t === "MessageEvent") {
			const llmMsg = p.llm_message as { content?: unknown } | undefined;
			const text =
				extractContentText(llmMsg?.content) ?? extractContentText(p.content);
			if (!text) continue;
			flushAll();
			lines.push(
				ev.event_source === "user"
					? `User: ${oneLine(text)}`
					: `Agent: ${oneLine(text)}`,
			);
			continue;
		}

		if (t === "ActionEvent") {
			const toolCall = p.tool_call as { name?: string } | undefined;
			const toolCallId =
				typeof p.tool_call_id === "string" ? p.tool_call_id : ev.id;
			const toolName =
				(typeof p.tool_name === "string" ? p.tool_name : undefined) ??
				toolCall?.name ??
				"tool";

			// Every turn ends with a synthetic "finish" tool call whose action
			// carries the agent's actual natural-language reply — render it as
			// reply text, not a tool-call line.
			if (toolName === "finish") {
				const action = p.action as { message?: unknown } | undefined;
				const finishText =
					typeof action?.message === "string" ? action.message : null;
				flushAll();
				if (finishText) lines.push(`Agent: ${oneLine(finishText)}`);
				continue;
			}

			flushAll();
			lines.push(`[tool: ${toolName}] called`);
			openToolCalls.set(toolCallId, lines.length - 1);
			continue;
		}

		if (
			t === "ObservationEvent" ||
			t === "AgentErrorEvent" ||
			t === "UserRejectObservation"
		) {
			// The matching ActionEvent already rendered the "finish" call's
			// message as reply text; its observation just repeats it.
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
			const idx = toolCallId ? openToolCalls.get(toolCallId) : undefined;
			const summary =
				condense(resultText, 160) || (isError ? "failed" : "done");
			if (idx !== undefined) {
				lines[idx] = `${lines[idx]} -> ${isError ? "ERROR: " : ""}${summary}`;
			} else {
				const toolName = typeof p.tool_name === "string" ? p.tool_name : "tool";
				lines.push(`[tool: ${toolName}] ${isError ? "ERROR: " : ""}${summary}`);
			}
			continue;
		}

		// ACPToolCallEvent — emitted by ACP-type agents (Claude Code/Codex/
		// Gemini CLI local bridge) in place of the ActionEvent/ObservationEvent
		// pair. Streams multiple updates for the same tool_call_id, so the
		// line is created once and updated in place.
		if (t === "ACPToolCallEvent") {
			const toolCallId =
				typeof p.tool_call_id === "string" ? p.tool_call_id : ev.id;
			const toolName =
				(typeof p.title === "string" ? p.title : null) ??
				(typeof p.tool_kind === "string" ? p.tool_kind : null) ??
				"tool";
			let idx = openToolCalls.get(toolCallId);
			if (idx === undefined) {
				flushAll();
				lines.push(`[tool: ${toolName}] called`);
				idx = lines.length - 1;
				openToolCalls.set(toolCallId, idx);
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
							: "");
				const summary = condense(resultText, 160) || status;
				const failed = status === "failed" || p.is_error === true;
				lines[idx] =
					`[tool: ${toolName}] ${failed ? "ERROR: " : ""}-> ${summary}`;
			}
			continue;
		}

		// Fallback for any other/unrecognized event type: surface as plain
		// text so nothing silently disappears from the transcript.
		const fallbackText =
			extractContentText(p.content) ??
			extractContentText(p.thought) ??
			(typeof p.message === "string" ? p.message : null);
		if (fallbackText) {
			flushAll();
			lines.push(
				ev.event_source === "user"
					? `User: ${oneLine(fallbackText)}`
					: `Agent: ${oneLine(fallbackText)}`,
			);
		}
	}

	flushAll();
	return lines;
}

function formatHeader(conversation: AgentConversation): string {
	const who = conversation.agent_name
		? `${conversation.agent_name}${conversation.agent_handle ? ` (@${conversation.agent_handle})` : ""}`
		: conversation.agent_id;
	return (
		`Conversation ${conversation.id} with ${who}\n` +
		`Status: ${conversation.status} | Trigger: ${conversation.trigger_type} | Started: ${conversation.started_at ?? conversation.created_at}`
	);
}

function formatPaginationFooter(page: ConversationEventPage): string {
	const lines = [
		`--- Showing ${page.items.length} of ${page.total} total event(s) ---`,
	];
	if (page.prev_cursor) {
		lines.push(
			`Older events exist. Call read_conversation again with before: "${page.prev_cursor}" to read further back.`,
		);
	}
	if (page.next_cursor) {
		lines.push(
			`Call read_conversation again with after: "${page.next_cursor}" to continue reading forward (useful if this conversation is still running).`,
		);
	}
	return lines.join("\n");
}

/** Exported for testing. */
export function formatConversationTranscript(
	conversation: AgentConversation,
	page: ConversationEventPage,
): string {
	const transcriptLines = formatTranscriptLines(page.items);
	const body =
		transcriptLines.length > 0
			? transcriptLines.join("\n")
			: "(no events in this window)";
	return [
		formatHeader(conversation),
		"",
		body,
		"",
		formatPaginationFooter(page),
	].join("\n");
}

// ── Tool handler ──────────────────────────────────────────────────────────────

export async function handleConversationTool(
	toolName: string,
	args: unknown,
	client: PacaAPIConversationClient,
): Promise<any> {
	switch (toolName) {
		case "read_conversation": {
			const { conversationId, after, before, limit } =
				ReadConversationSchema.parse(args);
			const window: ConversationEventWindow = { after, before, limit };

			const [conversation, page] = await Promise.all([
				client.getConversation(conversationId),
				client.listConversationEvents(conversationId, window),
			]);

			return {
				content: [
					{
						type: "text",
						text: formatConversationTranscript(conversation, page),
					},
				],
			};
		}

		default:
			throw new Error(`Unknown conversation tool: ${toolName}`);
	}
}
