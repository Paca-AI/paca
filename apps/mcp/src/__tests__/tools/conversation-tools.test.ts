import { describe, expect, it, vi } from "vitest";
import {
	formatConversationTranscript,
	formatTranscriptLines,
	getConversationTools,
	handleConversationTool,
} from "../../tools/conversation-tools.js";
import type {
	AgentConversation,
	AgentConversationEvent,
	ConversationEventPage,
} from "../../types/index.js";

// ── Shared fixtures ───────────────────────────────────────────────────────────

const CONVERSATION: AgentConversation = {
	id: "conv-1",
	agent_id: "agent-1",
	project_id: "p1",
	trigger_type: "chat_message",
	status: "running",
	iteration_count: 1,
	input_tokens: 10,
	output_tokens: 20,
	total_tokens: 30,
	created_at: "2024-01-01T00:00:00Z",
	started_at: "2024-01-01T00:00:01Z",
	agent_name: "Ada",
	agent_handle: "ada-bot",
};

function makeClient(
	opts: {
		conversation?: AgentConversation;
		page?: Partial<ConversationEventPage>;
	} = {},
) {
	const page: ConversationEventPage = {
		items: [],
		total: 0,
		next_cursor: null,
		prev_cursor: null,
		...opts.page,
	};
	return {
		getConversation: vi
			.fn()
			.mockResolvedValue(opts.conversation ?? CONVERSATION),
		listConversationEvents: vi.fn().mockResolvedValue(page),
	} as any;
}

function ev(
	overrides: Partial<AgentConversationEvent> & { event_type: string },
): AgentConversationEvent {
	return {
		id: `ev-${Math.random()}`,
		conversation_id: "conv-1",
		event_index: 0,
		event_source: "agent",
		payload: {},
		created_at: "2024-01-01T00:00:00Z",
		...overrides,
	};
}

// ── getConversationTools ─────────────────────────────────────────────────────

describe("getConversationTools", () => {
	it("returns exactly one tool: read_conversation", () => {
		const tools = getConversationTools();
		expect(tools).toHaveLength(1);
		expect(tools[0].name).toBe("read_conversation");
	});

	it("only requires conversationId", () => {
		const tools = getConversationTools();
		expect(tools[0].inputSchema.required).toEqual(["conversationId"]);
	});
});

// ── handleConversationTool ────────────────────────────────────────────────────

describe("handleConversationTool – read_conversation", () => {
	it("fetches the conversation and its events by ID alone — no projectId needed", async () => {
		const client = makeClient();
		await handleConversationTool(
			"read_conversation",
			{ conversationId: "conv-1" },
			client,
		);
		expect(client.getConversation).toHaveBeenCalledWith("conv-1");
		expect(client.listConversationEvents).toHaveBeenCalledWith("conv-1", {
			after: undefined,
			before: undefined,
			limit: undefined,
		});
	});

	it("passes after/before/limit through to the events call", async () => {
		const client = makeClient();
		await handleConversationTool(
			"read_conversation",
			{ conversationId: "conv-1", after: "cur1", limit: 25 },
			client,
		);
		expect(client.listConversationEvents).toHaveBeenCalledWith("conv-1", {
			after: "cur1",
			before: undefined,
			limit: 25,
		});
	});

	it("rejects when both after and before are provided", async () => {
		const client = makeClient();
		await expect(
			handleConversationTool(
				"read_conversation",
				{ conversationId: "conv-1", after: "a", before: "b" },
				client,
			),
		).rejects.toThrow();
	});

	it("returns the rendered transcript as text content", async () => {
		const client = makeClient({
			page: {
				items: [
					ev({
						event_type: "user_message",
						event_source: "user",
						payload: { content: { type: "text", text: "hello" } },
					}),
				],
				total: 1,
			},
		});
		const result = await handleConversationTool(
			"read_conversation",
			{ conversationId: "conv-1" },
			client,
		);
		expect(result.content[0].type).toBe("text");
		expect(result.content[0].text).toContain("User: hello");
		expect(result.content[0].text).toContain("Ada");
	});

	it("throws for an unknown tool name", async () => {
		const client = makeClient();
		await expect(handleConversationTool("unknown", {}, client)).rejects.toThrow(
			"Unknown conversation tool",
		);
	});
});

// ── formatTranscriptLines ──────────────────────────────────────────────────────

describe("formatTranscriptLines", () => {
	it("renders a user_message event", () => {
		const lines = formatTranscriptLines([
			ev({
				event_type: "user_message",
				event_source: "user",
				payload: { content: { type: "text", text: "Hi there" } },
			}),
		]);
		expect(lines).toEqual(["User: Hi there"]);
	});

	it("merges consecutive agent_message_chunk events into one Agent line", () => {
		const lines = formatTranscriptLines([
			ev({
				event_type: "agent_message_chunk",
				payload: { content: { text: "Hel" } },
			}),
			ev({
				event_type: "agent_message_chunk",
				payload: { content: { text: "lo!" } },
			}),
		]);
		expect(lines).toEqual(["Agent: Hello!"]);
	});

	it("renders agent_thought_chunk as a distinct thinking line", () => {
		const lines = formatTranscriptLines([
			ev({
				event_type: "agent_thought_chunk",
				payload: { content: { text: "considering options" } },
			}),
		]);
		expect(lines).toEqual(["Agent (thinking): considering options"]);
	});

	it("flushes the agent buffer before a new user_message", () => {
		const lines = formatTranscriptLines([
			ev({
				event_type: "agent_message_chunk",
				payload: { content: { text: "Reply" } },
			}),
			ev({
				event_type: "user_message",
				event_source: "user",
				payload: { content: { text: "Follow-up" } },
			}),
		]);
		expect(lines).toEqual(["Agent: Reply", "User: Follow-up"]);
	});

	it("skips non-user-visible bookkeeping events", () => {
		for (const type of [
			"ConversationStateUpdateEvent",
			"SystemPromptEvent",
			"StreamingDeltaEvent",
			"environment_ready",
			"turn_end",
			"turn_usage",
		]) {
			expect(formatTranscriptLines([ev({ event_type: type })])).toEqual([]);
		}
	});

	it("renders a tool_call then merges its tool_call_update result onto the same line", () => {
		const lines = formatTranscriptLines([
			ev({
				event_type: "tool_call",
				payload: { toolCallId: "tc1", title: "get_task" },
			}),
			ev({
				event_type: "tool_call_update",
				payload: {
					toolCallId: "tc1",
					status: "completed",
					content: [{ content: { text: "Task #1" } }],
				},
			}),
		]);
		expect(lines).toEqual(["[tool: get_task] called -> Task #1"]);
	});

	it("marks a failed tool_call_update as an error", () => {
		const lines = formatTranscriptLines([
			ev({
				event_type: "tool_call",
				payload: { toolCallId: "tc1", title: "read_doc" },
			}),
			ev({
				event_type: "tool_call_update",
				payload: { toolCallId: "tc1", status: "failed" },
			}),
		]);
		expect(lines).toEqual(["[tool: read_doc] called -> ERROR: failed"]);
	});

	it("renders a legacy MessageEvent from the user and from the agent", () => {
		const userLines = formatTranscriptLines([
			ev({
				event_type: "MessageEvent",
				event_source: "user",
				payload: { content: [{ text: "hi" }] },
			}),
		]);
		expect(userLines).toEqual(["User: hi"]);

		const agentLines = formatTranscriptLines([
			ev({
				event_type: "MessageEvent",
				event_source: "agent",
				payload: { llm_message: { content: [{ text: "hello back" }] } },
			}),
		]);
		expect(agentLines).toEqual(["Agent: hello back"]);
	});

	it("renders a legacy ActionEvent/ObservationEvent tool-call pair", () => {
		const lines = formatTranscriptLines([
			ev({
				event_type: "ActionEvent",
				payload: { tool_call_id: "tc1", tool_name: "read_doc" },
			}),
			ev({
				event_type: "ObservationEvent",
				payload: {
					tool_call_id: "tc1",
					tool_name: "read_doc",
					observation: { message: "Doc content" },
				},
			}),
		]);
		expect(lines).toEqual(["[tool: read_doc] called -> Doc content"]);
	});

	it("renders the synthetic 'finish' ActionEvent as the agent's reply, skipping its ObservationEvent", () => {
		const lines = formatTranscriptLines([
			ev({
				event_type: "ActionEvent",
				payload: {
					tool_call_id: "tc1",
					tool_name: "finish",
					action: { message: "All done!" },
				},
			}),
			ev({
				event_type: "ObservationEvent",
				payload: { tool_call_id: "tc1", tool_name: "finish" },
			}),
		]);
		expect(lines).toEqual(["Agent: All done!"]);
	});

	it("marks AgentErrorEvent and UserRejectObservation as errors", () => {
		const errorLines = formatTranscriptLines([
			ev({
				event_type: "ActionEvent",
				payload: { tool_call_id: "tc1", tool_name: "run" },
			}),
			ev({
				event_type: "AgentErrorEvent",
				payload: { tool_call_id: "tc1", error: "boom" },
			}),
		]);
		expect(errorLines).toEqual(["[tool: run] called -> ERROR: boom"]);

		const rejectLines = formatTranscriptLines([
			ev({
				event_type: "ActionEvent",
				payload: { tool_call_id: "tc2", tool_name: "run" },
			}),
			ev({
				event_type: "UserRejectObservation",
				payload: { tool_call_id: "tc2", rejection_reason: "not allowed" },
			}),
		]);
		expect(rejectLines).toEqual(["[tool: run] called -> ERROR: not allowed"]);
	});

	it("renders an ACPToolCallEvent, updating the same line once completed", () => {
		const lines = formatTranscriptLines([
			ev({
				event_type: "ACPToolCallEvent",
				payload: { tool_call_id: "tc1", title: "bash", status: "pending" },
			}),
			ev({
				event_type: "ACPToolCallEvent",
				payload: {
					tool_call_id: "tc1",
					title: "bash",
					status: "completed",
					raw_output: "done",
				},
			}),
		]);
		expect(lines).toEqual(["[tool: bash] -> done"]);
	});

	it("marks a failed ACPToolCallEvent as an error", () => {
		const lines = formatTranscriptLines([
			ev({
				event_type: "ACPToolCallEvent",
				payload: {
					tool_call_id: "tc1",
					title: "bash",
					status: "failed",
					is_error: true,
				},
			}),
		]);
		expect(lines).toEqual(["[tool: bash] ERROR: -> failed"]);
	});

	it("falls back to plain text for an unrecognized event type", () => {
		const lines = formatTranscriptLines([
			ev({
				event_type: "SomeFutureEvent",
				event_source: "agent",
				payload: { message: "note" },
			}),
		]);
		expect(lines).toEqual(["Agent: note"]);
	});

	it("returns an empty array for no events", () => {
		expect(formatTranscriptLines([])).toEqual([]);
	});
});

// ── formatConversationTranscript ───────────────────────────────────────────────

describe("formatConversationTranscript", () => {
	it("includes agent name/handle, status, and trigger type in the header", () => {
		const text = formatConversationTranscript(CONVERSATION, {
			items: [],
			total: 0,
			next_cursor: null,
			prev_cursor: null,
		});
		expect(text).toContain("Ada (@ada-bot)");
		expect(text).toContain("Status: running");
		expect(text).toContain("Trigger: chat_message");
	});

	it("shows a placeholder when the event window is empty", () => {
		const text = formatConversationTranscript(CONVERSATION, {
			items: [],
			total: 0,
			next_cursor: null,
			prev_cursor: null,
		});
		expect(text).toContain("(no events in this window)");
	});

	it("reports the shown/total event counts", () => {
		const items = [
			ev({ event_type: "user_message", payload: { content: { text: "hi" } } }),
		];
		const text = formatConversationTranscript(CONVERSATION, {
			items,
			total: 42,
			next_cursor: null,
			prev_cursor: null,
		});
		expect(text).toContain("Showing 1 of 42 total event(s)");
	});

	it("mentions the after cursor when next_cursor is present", () => {
		const text = formatConversationTranscript(CONVERSATION, {
			items: [],
			total: 0,
			next_cursor: "next-abc",
			prev_cursor: null,
		});
		expect(text).toContain('after: "next-abc"');
	});

	it("mentions the before cursor when prev_cursor is present", () => {
		const text = formatConversationTranscript(CONVERSATION, {
			items: [],
			total: 0,
			next_cursor: null,
			prev_cursor: "prev-abc",
		});
		expect(text).toContain('before: "prev-abc"');
	});

	it("omits cursor hints entirely when both cursors are null", () => {
		const text = formatConversationTranscript(CONVERSATION, {
			items: [],
			total: 0,
			next_cursor: null,
			prev_cursor: null,
		});
		expect(text).not.toContain("Call read_conversation again");
		expect(text).not.toContain("Older events exist");
	});
});
