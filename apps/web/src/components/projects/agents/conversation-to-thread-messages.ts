import type { AppendMessage, ThreadMessageLike } from "@assistant-ui/react";
import type {
	AgentConversation,
	AgentConversationEvent,
} from "@/lib/agent-api";
import { parseContextItems } from "@/lib/context-items";

// Our chat runtimes (conversation-view.tsx / ai-chat-float.tsx / the
// Conversations page's new-conversation composer) only ever send a single
// text part — there's no attachment UI wired up to any of them (see
// thread.tsx's Composer comment) — so reject anything else instead of
// silently dropping it. Returns null rather than throwing so each call site
// can raise its own translated error message.
export function extractTextOnlyContent(message: AppendMessage): string | null {
	if (message.content.length !== 1 || message.content[0]?.type !== "text") {
		return null;
	}
	return message.content[0].text;
}

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

// Extracts text from a single ACP ContentBlock object ({type, text} — not
// an array, unlike extractContentText's inputs). agent-runner (services/
// agent-runner/internal/acp/types.go) writes this shape directly as
// agent_message_chunk's `content` field and, wrapped one level deeper, as
// each entry of tool_call_update's `content` array.
function extractAcpBlockText(block: unknown): string | null {
	if (typeof block !== "object" || block === null) return null;
	const text = (block as Record<string, unknown>).text;
	return typeof text === "string" && text.length > 0 ? text : null;
}

// tool_call_update's `content` is an array of {type, content: ContentBlock}
// wrapper objects (ToolCallUpdate.Content in the Go source) — mirrors that
// struct's own Text() helper.
function extractToolCallUpdateText(content: unknown): string | null {
	if (!Array.isArray(content)) return null;
	const parts = (content as Array<unknown>)
		.map((c) => {
			if (typeof c !== "object" || c === null) return null;
			return extractAcpBlockText((c as Record<string, unknown>).content);
		})
		.filter((t): t is string => t !== null);
	return parts.length > 0 ? parts.join("") : null;
}

export interface ToolDiffBlock {
	path?: string;
	oldText: string | null;
	newText: string;
}

// ACP-spec edit tools (Claude Code / Codex / Gemini CLI) report file changes as
// `{type: "diff", path, old_text, new_text}` content blocks rather than
// exposing before/after text through `raw_input` directly — see
// ACPToolCallEvent.is_patch_edit in the SDK. `old_text` is snake_case as
// written by the backend's `model_dump(mode="json")` (no alias), but ACP's own
// JSON wire format uses camelCase, so both are checked for robustness.
function extractDiffBlocks(content: unknown): ToolDiffBlock[] | null {
	if (!Array.isArray(content)) return null;
	const diffs: ToolDiffBlock[] = [];
	for (const block of content) {
		if (typeof block !== "object" || block === null) continue;
		const b = block as Record<string, unknown>;
		if (b.type !== "diff") continue;
		const newText =
			typeof b.new_text === "string"
				? b.new_text
				: typeof b.newText === "string"
					? b.newText
					: null;
		if (newText === null) continue;
		const oldText =
			typeof b.old_text === "string"
				? b.old_text
				: typeof b.oldText === "string"
					? b.oldText
					: null;
		diffs.push({
			path: typeof b.path === "string" ? b.path : undefined,
			oldText,
			newText,
		});
	}
	return diffs.length > 0 ? diffs : null;
}

// Native OpenHands SDK `file_editor` tool (regular, non-ACP agent
// conversations) reports edits via `old_content`/`new_content` on the
// FileEditorObservation that follows the ActionEvent, not through a
// diff-type content block the way ACPToolCallEvent does. Mirrors the SDK's
// own FileEditorObservation._has_meaningful_diff so `view` calls, errors,
// and no-op edits don't render an empty/misleading diff.
function extractFileEditorDiff(
	obs: Record<string, unknown> | undefined,
): ToolDiffBlock | null {
	if (!obs || obs.kind !== "FileEditorObservation") return null;
	if (obs.is_error === true) return null;
	const path = typeof obs.path === "string" ? obs.path : null;
	if (!path) return null;

	const oldContent =
		typeof obs.old_content === "string" ? obs.old_content : null;
	const newContent =
		typeof obs.new_content === "string" ? obs.new_content : null;

	if (obs.command === "create") {
		if (!newContent || obs.prev_exist === true) return null;
		return { path, oldText: null, newText: newContent };
	}

	if (
		obs.command === "str_replace" ||
		obs.command === "insert" ||
		obs.command === "undo_edit"
	) {
		if (oldContent === null || newContent === null) return null;
		if (oldContent === newContent) return null;
		return { path, oldText: oldContent, newText: newContent };
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
	// UI-only: lets ToolFallback render a real diff instead of the raw args
	// JSON for ACP edit/write tool calls.
	artifact?: { diffs: ToolDiffBlock[] };
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
 * Whether the *current* turn's environment is ready — services/agent-runner
 * persists an `environment_ready` marker right before every session/prompt
 * call (see executor.Run's `onReady`), once the sandbox/ACP session is
 * actually ready to run a turn. Callers use this to switch the Thread's
 * empty-message indicator from "setting up your environment" to "thinking":
 * before this event, any wait is container/handshake setup; after it, any
 * further wait is the LLM itself.
 *
 * Compares the latest `environment_ready` against the latest `user_message`
 * rather than just checking presence anywhere in the window: a chat
 * conversation whose sandbox got idle-reaped and needs a fresh cold start
 * still has an *older* turn's `environment_ready` sitting in the loaded
 * window, which a bare "has one ever appeared" check would misread as
 * ready. Every trigger with real message text gets its own `user_message`
 * (see handler.Handle's persistAndPublish call), so its index marks where
 * the current turn began.
 */
export function hasEnvironmentReadyEvent(
	events: AgentConversationEvent[],
): boolean {
	const lastReadyIndex =
		events.findLast((e) => e.event_type === "environment_ready")?.event_index ??
		-1;
	const lastUserMessageIndex =
		events.findLast((e) => e.event_type === "user_message")?.event_index ?? -1;
	return lastReadyIndex > lastUserMessageIndex;
}

/**
 * Whether the Thread's empty-message indicator should read "thinking"
 * instead of "setting up your environment" — the single computation all
 * three chat surfaces (conversation-view, ai-chat-float,
 * ai-chat-float-global) need, so it lives here once instead of being
 * re-derived at each call site. ACP (local bridge) conversations have no
 * sandbox to provision at all (see conversation-view.tsx's isACP doc
 * comment), so they're always ready; everything else defers to
 * hasEnvironmentReadyEvent's turn-boundary check.
 */
export function isEnvironmentReady(
	isACP: boolean,
	events: AgentConversationEvent[],
): boolean {
	return isACP || hasEnvironmentReadyEvent(events);
}

/**
 * Whether the composer should accept a new message right now — the single
 * computation all three chat surfaces (conversation-view, ai-chat-float,
 * ai-chat-float-global) need, so it lives here once instead of being
 * re-derived (each slightly differently, and easy to drift out of sync) at
 * each call site.
 *
 * ACP conversations and conversations attached to a static environment are
 * always replyable, full stop — regardless of status or trigger type. An
 * ACP conversation's local bridge daemon keeps it alive by conversation_id
 * independent of Paca's own status bookkeeping (see
 * SendConversationMessage's ACP branch in services/api), and an
 * environment-attached conversation's reply carries the environment/folder
 * over onto a fresh conversation instead of dead-ending, since the
 * environment outlives any one conversation (see SendChatMessage's and
 * resumeConversationMessage's terminal branches in services/api) — both
 * reached the same way whether or not this conversation has a
 * chat_session_id, via onNew's own fallback to sendConversationMessage
 * where there's no chat session to reply through.
 *
 * Everything else is a plain LLM chat_message conversation with no
 * environment attached: only replyable through a live chat session that
 * hasn't yet reached a terminal status.
 */
export function canReplyToConversation(
	conversation: AgentConversation | null | undefined,
	isACP: boolean,
): boolean {
	if (!conversation) return false;
	if (isACP || !!conversation.environment_id) return true;
	if (conversation.trigger_type !== "chat_message") return false;
	const isTerminal =
		conversation.status === "finished" ||
		conversation.status === "failed" ||
		conversation.status === "stopped";
	return !!conversation.chat_session_id && !isTerminal;
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
		// environment_ready never renders as a bubble either — it's a marker
		// consumed separately via hasEnvironmentReadyEvent below, not part of
		// the transcript.
		if (
			t === "ConversationStateUpdateEvent" ||
			t === "SystemPromptEvent" ||
			t === "StreamingDeltaEvent" ||
			t === "environment_ready"
		) {
			continue;
		}

		// user_message / agent_message_chunk / agent_thought_chunk / tool_call
		// / tool_call_update / turn_end are the shared ACP event vocabulary
		// services/agent-runner's internal/handler established for `llm`-type
		// (Goose-in-sandbox) agents and apps/acp-bridge's Go daemon reuses
		// as-is for `acp`-type (local bridge) agents — both ultimately relay
		// the same raw ACP session/update notifications, so there was never a
		// reason for the local-bridge path to invent a second, OpenHands-
		// shaped vocabulary of its own. Handled separately from the
		// OpenHands-style types below rather than folded in, since a
		// conversation from before both of those migrations can still have
		// OpenHands-typed rows in its history that must keep rendering
		// correctly.
		//
		// user_message records what the user actually sent, written once at
		// the start of a turn (internal/handler/handler.go) — ACP itself has
		// no equivalent event (session/prompt never echoes back what it was
		// asked), so without this a chat conversation's own messages
		// wouldn't render at all, only the agent's replies.
		if (t === "user_message") {
			const text = extractAcpBlockText(p.content);
			if (!text) continue;
			flushCurrent();
			// context_items (snake_case, as persisted by
			// services/agent-runner's handler.go) round-trips back into the
			// camelCase ContextItem[] shape here so thread.tsx's UserMessage
			// can render a read-only badge row via metadata.custom.contextItems.
			const contextItems = parseContextItems(p.context_items);
			messages.push({
				id: ev.id,
				role: "user",
				createdAt: new Date(ev.created_at),
				content: [{ type: "text", text }],
				...(contextItems.length > 0
					? { metadata: { custom: { contextItems } } }
					: {}),
			});
			continue;
		}

		if (t === "agent_message_chunk") {
			const text = extractAcpBlockText(p.content);
			if (!text) continue;
			if (!current)
				current = startAssistantMessage(ev.id, new Date(ev.created_at));
			// Chunks stream one turn's reply piece by piece — append to the
			// already-open text part instead of pushing a new one each time,
			// or the reply renders as many separate fragments/paragraphs
			// instead of one continuous message.
			const lastPart = current.parts.at(-1);
			if (lastPart && lastPart.type === "text") {
				lastPart.text += text;
			} else {
				current.parts.push({ type: "text", text });
			}
			continue;
		}

		if (t === "agent_thought_chunk") {
			const text = extractAcpBlockText(p.content);
			if (!text) continue;
			if (!current)
				current = startAssistantMessage(ev.id, new Date(ev.created_at));
			// Same streaming-append shape as agent_message_chunk above, but
			// into a "reasoning" part instead of a "text" one — the model's
			// thinking, kept visually distinct (and collapsible) from its
			// actual reply.
			const lastPart = current.parts.at(-1);
			if (lastPart && lastPart.type === "reasoning") {
				lastPart.text += text;
			} else {
				current.parts.push({ type: "reasoning", text });
			}
			continue;
		}

		if (t === "tool_call") {
			if (!current)
				current = startAssistantMessage(ev.id, new Date(ev.created_at));
			const toolCallId =
				typeof p.toolCallId === "string" ? p.toolCallId : ev.id;
			const toolName = typeof p.title === "string" ? p.title : "tool";
			const part: MutableToolCallPart = {
				type: "tool-call",
				toolCallId,
				toolName,
				argsText: "",
			};
			current.parts.push(part);
			current.openToolCalls.set(toolCallId, part);
			continue;
		}

		if (t === "tool_call_update") {
			const toolCallId =
				typeof p.toolCallId === "string" ? p.toolCallId : undefined;
			const status = typeof p.status === "string" ? p.status : null;
			const resultText = extractToolCallUpdateText(p.content);
			// agent-runner appends {type:"diff",...} content blocks for
			// completed edit-type tool calls itself — see
			// services/agent-runner/internal/executor/diff.go — since Goose's
			// ACP-over-HTTP mode has no fs-delegation callback to report them
			// through natively the way Claude Code/Codex ACP sessions do.
			const diffs = extractDiffBlocks(p.content);
			const openPart =
				toolCallId && current
					? current.openToolCalls.get(toolCallId)
					: undefined;
			if (openPart) {
				if (resultText) openPart.result = resultText;
				if (status === "failed") openPart.isError = true;
				if (diffs) openPart.artifact = { diffs };
			} else if (resultText) {
				// No matching open tool-call in this turn (history gap) —
				// append a standalone, already-complete tool-call part.
				if (!current)
					current = startAssistantMessage(ev.id, new Date(ev.created_at));
				current.parts.push({
					type: "tool-call",
					toolCallId: toolCallId ?? ev.id,
					toolName: "tool",
					argsText: "",
					result: resultText,
					...(status === "failed" ? { isError: true } : {}),
					...(diffs ? { artifact: { diffs } } : {}),
				});
			}
			continue;
		}

		// turn_end carries only a machine-readable stop reason, never
		// user-visible content.
		if (t === "turn_end") continue;

		// turn_usage carries only token/cost accounting (see
		// services/agent-runner/internal/handler/handler.go) — surfaced via
		// AgentConversation.total_tokens/cost_usd (the usage pill on each
		// conversation list item, see conversations-layout.tsx), never as a
		// transcript bubble. Currently falls through harmlessly to the
		// fallback case below anyway (its payload has no content/thought/
		// message key), but skipped explicitly rather than relying on that.
		if (t === "turn_usage") continue;

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

			const fileEditorDiff =
				p.tool_name === "file_editor" ? extractFileEditorDiff(obs) : null;

			if (openPart) {
				openPart.result = resultText;
				if (isError) openPart.isError = true;
				if (fileEditorDiff) openPart.artifact = { diffs: [fileEditorDiff] };
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
					...(fileEditorDiff ? { artifact: { diffs: [fileEditorDiff] } } : {}),
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

			// Diff content can arrive on the initial notification (edit tools
			// typically propose the full diff upfront) or only once the call
			// completes, depending on provider — check on every update but
			// never clear a diff already captured from an earlier update.
			const diffs = extractDiffBlocks(p.content);

			let part = current.openToolCalls.get(toolCallId);
			if (!part) {
				part = { type: "tool-call", toolCallId, toolName, argsText };
				if (diffs) part.artifact = { diffs };
				current.parts.push(part);
				current.openToolCalls.set(toolCallId, part);
			} else {
				part.toolName = toolName;
				if (argsText) part.argsText = argsText;
				if (diffs) part.artifact = { diffs };
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
