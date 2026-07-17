import { describe, expect, it } from "vitest";
import type { AgentConversationEvent } from "@/lib/agent-api";
import { eventsToThreadMessages } from "./conversation-to-thread-messages";

let nextIndex = 0;

function userMessage(text: string): AgentConversationEvent {
	return {
		id: `evt-${nextIndex}`,
		conversation_id: "conv-1",
		event_index: nextIndex++,
		event_type: "MessageEvent",
		event_source: "user",
		payload: { content: text },
		created_at: "2026-01-01T00:00:00.000Z",
	};
}

function agentReply(text: string): AgentConversationEvent {
	return {
		id: `evt-${nextIndex}`,
		conversation_id: "conv-1",
		event_index: nextIndex++,
		event_type: "MessageEvent",
		event_source: "agent",
		payload: { content: text },
		created_at: "2026-01-01T00:00:01.000Z",
	};
}

function actionEvent(opts: {
	thought?: string;
	toolCallId: string;
	toolName: string;
	args?: string;
}): AgentConversationEvent {
	return {
		id: `evt-${nextIndex}`,
		conversation_id: "conv-1",
		event_index: nextIndex++,
		event_type: "ActionEvent",
		event_source: "agent",
		payload: {
			thought: opts.thought ? [{ type: "text", text: opts.thought }] : [],
			tool_call_id: opts.toolCallId,
			tool_name: opts.toolName,
			tool_call: { name: opts.toolName, arguments: opts.args ?? "{}" },
		},
		created_at: "2026-01-01T00:00:02.000Z",
	};
}

function observationEvent(opts: {
	toolCallId: string;
	toolName: string;
	result: string;
}): AgentConversationEvent {
	return {
		id: `evt-${nextIndex}`,
		conversation_id: "conv-1",
		event_index: nextIndex++,
		event_type: "ObservationEvent",
		event_source: "agent",
		payload: {
			tool_call_id: opts.toolCallId,
			tool_name: opts.toolName,
			observation: { content: opts.result },
		},
		created_at: "2026-01-01T00:00:03.000Z",
	};
}

function agentErrorEvent(opts: {
	toolCallId: string;
	toolName: string;
	error: string;
}): AgentConversationEvent {
	return {
		id: `evt-${nextIndex}`,
		conversation_id: "conv-1",
		event_index: nextIndex++,
		event_type: "AgentErrorEvent",
		event_source: "agent",
		payload: {
			tool_call_id: opts.toolCallId,
			tool_name: opts.toolName,
			error: opts.error,
		},
		created_at: "2026-01-01T00:00:03.000Z",
	};
}

function userRejectObservation(opts: {
	toolCallId: string;
	toolName: string;
	rejectionReason: string;
}): AgentConversationEvent {
	return {
		id: `evt-${nextIndex}`,
		conversation_id: "conv-1",
		event_index: nextIndex++,
		event_type: "UserRejectObservation",
		event_source: "agent",
		payload: {
			tool_call_id: opts.toolCallId,
			tool_name: opts.toolName,
			rejection_reason: opts.rejectionReason,
		},
		created_at: "2026-01-01T00:00:03.000Z",
	};
}

function finishActionEvent(
	toolCallId: string,
	message: string,
): AgentConversationEvent {
	return {
		id: `evt-${nextIndex}`,
		conversation_id: "conv-1",
		event_index: nextIndex++,
		event_type: "ActionEvent",
		event_source: "agent",
		payload: {
			thought: [],
			tool_call_id: toolCallId,
			tool_name: "finish",
			tool_call: { name: "finish", arguments: JSON.stringify({ message }) },
			action: { message },
		},
		created_at: "2026-01-01T00:00:04.000Z",
	};
}

function finishObservationEvent(
	toolCallId: string,
	message: string,
): AgentConversationEvent {
	return {
		id: `evt-${nextIndex}`,
		conversation_id: "conv-1",
		event_index: nextIndex++,
		event_type: "ObservationEvent",
		event_source: "agent",
		payload: {
			tool_call_id: toolCallId,
			tool_name: "finish",
			observation: { content: message },
		},
		created_at: "2026-01-01T00:00:05.000Z",
	};
}

function acpToolCallEvent(opts: {
	toolCallId: string;
	title: string;
	status?: string;
	rawInput?: unknown;
	rawOutput?: unknown;
	isError?: boolean;
}): AgentConversationEvent {
	return {
		id: `evt-${nextIndex}`,
		conversation_id: "conv-1",
		event_index: nextIndex++,
		event_type: "ACPToolCallEvent",
		event_source: "agent",
		payload: {
			tool_call_id: opts.toolCallId,
			title: opts.title,
			status: opts.status ?? null,
			raw_input: opts.rawInput ?? null,
			raw_output: opts.rawOutput ?? null,
			content: null,
			is_error: opts.isError ?? false,
		},
		created_at: "2026-01-01T00:00:06.000Z",
	};
}

describe("eventsToThreadMessages", () => {
	it("converts a text-only turn into user + assistant messages", () => {
		const events = [userMessage("hi"), agentReply("hello!")];

		const messages = eventsToThreadMessages(events, false);

		expect(messages).toHaveLength(2);
		expect(messages[0]).toMatchObject({
			role: "user",
			content: [{ type: "text", text: "hi" }],
		});
		expect(messages[1]).toMatchObject({
			role: "assistant",
			content: [{ type: "text", text: "hello!" }],
		});
	});

	it("groups thought + tool-call + observation + reply into one assistant message", () => {
		const events = [
			userMessage("list the repos"),
			actionEvent({
				thought: "I should list repositories first",
				toolCallId: "call-1",
				toolName: "list_repositories",
			}),
			observationEvent({
				toolCallId: "call-1",
				toolName: "list_repositories",
				result: "repo-a, repo-b",
			}),
			agentReply("You have two repos: repo-a and repo-b."),
		];

		const messages = eventsToThreadMessages(events, false);

		expect(messages).toHaveLength(2);
		const assistant = messages[1];
		expect(assistant.role).toBe("assistant");
		const parts = assistant.content as unknown as Array<
			Record<string, unknown>
		>;
		expect(parts).toHaveLength(3);
		expect(parts[0]).toMatchObject({
			type: "reasoning",
			text: "I should list repositories first",
		});
		expect(parts[1]).toMatchObject({
			type: "tool-call",
			toolCallId: "call-1",
			toolName: "list_repositories",
			result: "repo-a, repo-b",
		});
		expect(parts[2]).toMatchObject({
			type: "text",
			text: "You have two repos: repo-a and repo-b.",
		});
	});

	it("keeps multiple tool calls in one turn as separate correlated parts", () => {
		const events = [
			userMessage("do two things"),
			actionEvent({ toolCallId: "call-1", toolName: "tool_a" }),
			observationEvent({
				toolCallId: "call-1",
				toolName: "tool_a",
				result: "a-done",
			}),
			actionEvent({ toolCallId: "call-2", toolName: "tool_b" }),
			observationEvent({
				toolCallId: "call-2",
				toolName: "tool_b",
				result: "b-done",
			}),
			agentReply("Both done."),
		];

		const messages = eventsToThreadMessages(events, false);

		const assistant = messages[1];
		const parts = assistant.content as unknown as Array<
			Record<string, unknown>
		>;
		const toolCalls = parts.filter((p) => p.type === "tool-call");
		expect(toolCalls).toHaveLength(2);
		expect(toolCalls[0]).toMatchObject({
			toolCallId: "call-1",
			result: "a-done",
		});
		expect(toolCalls[1]).toMatchObject({
			toolCallId: "call-2",
			result: "b-done",
		});
	});

	it("appends a standalone completed tool-call part when an observation has no matching open call", () => {
		const events = [
			observationEvent({
				toolCallId: "orphan-1",
				toolName: "mystery_tool",
				result: "done",
			}),
		];

		const messages = eventsToThreadMessages(events, false);

		expect(messages).toHaveLength(1);
		const parts = messages[0].content as unknown as Array<
			Record<string, unknown>
		>;
		expect(parts).toHaveLength(1);
		expect(parts[0]).toMatchObject({
			type: "tool-call",
			toolCallId: "orphan-1",
			result: "done",
		});
	});

	it("marks the trailing assistant message as running when the thread is still running", () => {
		const events = [
			userMessage("hi"),
			actionEvent({ toolCallId: "call-1", toolName: "tool_a" }),
		];

		const messages = eventsToThreadMessages(events, true);

		expect(messages[1].status).toEqual({ type: "running" });
	});

	it("marks a completed trailing assistant message as complete", () => {
		const events = [userMessage("hi"), agentReply("done")];

		const messages = eventsToThreadMessages(events, false);

		expect(messages[1].status).toEqual({ type: "complete", reason: "stop" });
	});

	it("resolves an open tool-call with an error when the tool fails (AgentErrorEvent)", () => {
		const events = [
			userMessage("run the broken tool"),
			actionEvent({ toolCallId: "call-1", toolName: "flaky_tool" }),
			agentErrorEvent({
				toolCallId: "call-1",
				toolName: "flaky_tool",
				error: "connection reset",
			}),
		];

		const messages = eventsToThreadMessages(events, false);

		const assistant = messages[1];
		const parts = assistant.content as unknown as Array<
			Record<string, unknown>
		>;
		expect(parts).toHaveLength(1);
		expect(parts[0]).toMatchObject({
			type: "tool-call",
			toolCallId: "call-1",
			result: "connection reset",
			isError: true,
		});
	});

	it("resolves an open tool-call with an error when the user rejects it (UserRejectObservation)", () => {
		const events = [
			userMessage("delete everything"),
			actionEvent({ toolCallId: "call-1", toolName: "delete_repo" }),
			userRejectObservation({
				toolCallId: "call-1",
				toolName: "delete_repo",
				rejectionReason: "User rejected the action",
			}),
		];

		const messages = eventsToThreadMessages(events, false);

		const assistant = messages[1];
		const parts = assistant.content as unknown as Array<
			Record<string, unknown>
		>;
		expect(parts).toHaveLength(1);
		expect(parts[0]).toMatchObject({
			type: "tool-call",
			toolCallId: "call-1",
			result: "User rejected the action",
			isError: true,
		});
	});

	it("appends a standalone errored tool-call part when an AgentErrorEvent has no matching open call", () => {
		const events = [
			agentErrorEvent({
				toolCallId: "orphan-1",
				toolName: "mystery_tool",
				error: "boom",
			}),
		];

		const messages = eventsToThreadMessages(events, false);

		expect(messages).toHaveLength(1);
		const parts = messages[0].content as unknown as Array<
			Record<string, unknown>
		>;
		expect(parts).toHaveLength(1);
		expect(parts[0]).toMatchObject({
			type: "tool-call",
			toolCallId: "orphan-1",
			result: "boom",
			isError: true,
		});
	});

	it("does not mark a successful ObservationEvent's tool-call as an error", () => {
		const events = [
			actionEvent({ toolCallId: "call-1", toolName: "list_repositories" }),
			observationEvent({
				toolCallId: "call-1",
				toolName: "list_repositories",
				result: "repo-a",
			}),
		];

		const messages = eventsToThreadMessages(events, false);

		const parts = messages[0].content as unknown as Array<
			Record<string, unknown>
		>;
		expect(parts[0]).not.toHaveProperty("isError");
	});

	it("renders the synthetic 'finish' tool call as reply text, not a tool-call card", () => {
		const events = [
			userMessage("do the task"),
			finishActionEvent("call-finish", "All done, here's the summary."),
			finishObservationEvent("call-finish", "All done, here's the summary."),
		];

		const messages = eventsToThreadMessages(events, false);

		expect(messages).toHaveLength(2);
		const parts = messages[1].content as unknown as Array<
			Record<string, unknown>
		>;
		expect(parts).toHaveLength(1);
		expect(parts[0]).toMatchObject({
			type: "text",
			text: "All done, here's the summary.",
		});
	});

	it("renders an ACPToolCallEvent as a tool-call part keyed by tool_call_id", () => {
		const events = [
			userMessage("run ls"),
			acpToolCallEvent({
				toolCallId: "acp-1",
				title: "Bash: ls -la",
				status: "in_progress",
				rawInput: { command: "ls -la" },
			}),
		];

		const messages = eventsToThreadMessages(events, true);

		const parts = messages[1].content as unknown as Array<
			Record<string, unknown>
		>;
		expect(parts).toHaveLength(1);
		expect(parts[0]).toMatchObject({
			type: "tool-call",
			toolCallId: "acp-1",
			toolName: "Bash: ls -la",
		});
		expect(parts[0]).not.toHaveProperty("result");
	});

	it("updates the same ACPToolCallEvent part in place across start/progress/terminal updates", () => {
		const events = [
			userMessage("run ls"),
			acpToolCallEvent({
				toolCallId: "acp-1",
				title: "Bash: ls -la",
				status: "pending",
				rawInput: { command: "ls -la" },
			}),
			acpToolCallEvent({
				toolCallId: "acp-1",
				title: "Bash: ls -la",
				status: "completed",
				rawInput: { command: "ls -la" },
				rawOutput: "file1\nfile2",
			}),
		];

		const messages = eventsToThreadMessages(events, false);

		const parts = messages[1].content as unknown as Array<
			Record<string, unknown>
		>;
		expect(parts).toHaveLength(1);
		expect(parts[0]).toMatchObject({
			type: "tool-call",
			toolCallId: "acp-1",
			result: "file1\nfile2",
		});
	});

	it("marks a failed ACPToolCallEvent's terminal update as an error", () => {
		const events = [
			acpToolCallEvent({
				toolCallId: "acp-1",
				title: "Bash: rm nonexistent",
				status: "failed",
				rawOutput: "No such file or directory",
				isError: true,
			}),
		];

		const messages = eventsToThreadMessages(events, false);

		const parts = messages[0].content as unknown as Array<
			Record<string, unknown>
		>;
		expect(parts[0]).toMatchObject({
			type: "tool-call",
			toolCallId: "acp-1",
			result: "No such file or directory",
			isError: true,
		});
	});
});
