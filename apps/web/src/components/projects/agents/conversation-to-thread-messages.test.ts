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

function fileEditorObservationEvent(opts: {
	toolCallId: string;
	path: string;
	command: string;
	oldContent?: string | null;
	newContent?: string | null;
	prevExist?: boolean;
	isError?: boolean;
}): AgentConversationEvent {
	return {
		id: `evt-${nextIndex}`,
		conversation_id: "conv-1",
		event_index: nextIndex++,
		event_type: "ObservationEvent",
		event_source: "agent",
		payload: {
			tool_call_id: opts.toolCallId,
			tool_name: "file_editor",
			observation: {
				kind: "FileEditorObservation",
				command: opts.command,
				path: opts.path,
				prev_exist: opts.prevExist ?? true,
				old_content: opts.oldContent ?? null,
				new_content: opts.newContent ?? null,
				is_error: opts.isError ?? false,
				content: "edited",
			},
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
	content?: unknown;
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
			content: opts.content ?? null,
			is_error: opts.isError ?? false,
		},
		created_at: "2026-01-01T00:00:06.000Z",
	};
}

function agentMessageChunk(text: string): AgentConversationEvent {
	return {
		id: `evt-${nextIndex}`,
		conversation_id: "conv-1",
		event_index: nextIndex++,
		event_type: "agent_message_chunk",
		event_source: "agent",
		payload: {
			content: { type: "text", text },
			sessionUpdate: "agent_message_chunk",
		},
		created_at: "2026-01-01T00:00:07.000Z",
	};
}

function agentThoughtChunk(text: string): AgentConversationEvent {
	return {
		id: `evt-${nextIndex}`,
		conversation_id: "conv-1",
		event_index: nextIndex++,
		event_type: "agent_thought_chunk",
		event_source: "agent",
		payload: {
			content: { type: "text", text },
			sessionUpdate: "agent_thought_chunk",
		},
		created_at: "2026-01-01T00:00:07.500Z",
	};
}

function acpToolCall(opts: {
	toolCallId: string;
	title: string;
}): AgentConversationEvent {
	return {
		id: `evt-${nextIndex}`,
		conversation_id: "conv-1",
		event_index: nextIndex++,
		event_type: "tool_call",
		event_source: "agent",
		payload: {
			toolCallId: opts.toolCallId,
			title: opts.title,
			sessionUpdate: "tool_call",
		},
		created_at: "2026-01-01T00:00:08.000Z",
	};
}

function acpToolCallUpdate(opts: {
	toolCallId: string;
	status?: string;
	text?: string;
}): AgentConversationEvent {
	return {
		id: `evt-${nextIndex}`,
		conversation_id: "conv-1",
		event_index: nextIndex++,
		event_type: "tool_call_update",
		event_source: "agent",
		payload: {
			toolCallId: opts.toolCallId,
			status: opts.status ?? null,
			content: opts.text
				? [{ type: "content", content: { type: "text", text: opts.text } }]
				: [],
			sessionUpdate: "tool_call_update",
		},
		created_at: "2026-01-01T00:00:09.000Z",
	};
}

function turnEnd(stopReason: string): AgentConversationEvent {
	return {
		id: `evt-${nextIndex}`,
		conversation_id: "conv-1",
		event_index: nextIndex++,
		event_type: "turn_end",
		event_source: "system",
		payload: { stopReason },
		created_at: "2026-01-01T00:00:10.000Z",
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

	it("extracts a patch-edit diff block into the tool-call's artifact", () => {
		const events = [
			acpToolCallEvent({
				toolCallId: "acp-edit-1",
				title: "Edit: src/foo.ts",
				status: "completed",
				content: [
					{
						type: "diff",
						path: "src/foo.ts",
						old_text: "const a = 1;",
						new_text: "const a = 2;",
					},
				],
			}),
		];

		const messages = eventsToThreadMessages(events, false);

		const parts = messages[0].content as unknown as Array<
			Record<string, unknown>
		>;
		expect(parts[0]).toMatchObject({
			type: "tool-call",
			toolCallId: "acp-edit-1",
			artifact: {
				diffs: [
					{
						path: "src/foo.ts",
						oldText: "const a = 1;",
						newText: "const a = 2;",
					},
				],
			},
		});
	});

	it("extracts a full-file write diff block with a null oldText", () => {
		const events = [
			acpToolCallEvent({
				toolCallId: "acp-write-1",
				title: "Write: src/new-file.ts",
				status: "completed",
				content: [
					{
						type: "diff",
						path: "src/new-file.ts",
						old_text: null,
						new_text: "export const x = 1;",
					},
				],
			}),
		];

		const messages = eventsToThreadMessages(events, false);

		const parts = messages[0].content as unknown as Array<
			Record<string, unknown>
		>;
		expect(parts[0]).toMatchObject({
			type: "tool-call",
			toolCallId: "acp-write-1",
			artifact: {
				diffs: [
					{
						path: "src/new-file.ts",
						oldText: null,
						newText: "export const x = 1;",
					},
				],
			},
		});
	});

	it("keeps a diff captured on an earlier update when a later update carries no content", () => {
		const events = [
			acpToolCallEvent({
				toolCallId: "acp-edit-2",
				title: "Edit: src/bar.ts",
				status: "pending",
				content: [
					{
						type: "diff",
						path: "src/bar.ts",
						old_text: "old",
						new_text: "new",
					},
				],
			}),
			acpToolCallEvent({
				toolCallId: "acp-edit-2",
				title: "Edit: src/bar.ts",
				status: "completed",
			}),
		];

		const messages = eventsToThreadMessages(events, false);

		const parts = messages[0].content as unknown as Array<
			Record<string, unknown>
		>;
		expect(parts[0]).toMatchObject({
			type: "tool-call",
			toolCallId: "acp-edit-2",
			artifact: {
				diffs: [{ path: "src/bar.ts", oldText: "old", newText: "new" }],
			},
		});
	});

	it("does not attach an artifact for non-edit ACP tool calls", () => {
		const events = [
			acpToolCallEvent({
				toolCallId: "acp-1",
				title: "Bash: ls -la",
				status: "completed",
				rawOutput: "file1\nfile2",
			}),
		];

		const messages = eventsToThreadMessages(events, false);

		const parts = messages[0].content as unknown as Array<
			Record<string, unknown>
		>;
		expect(parts[0]).not.toHaveProperty("artifact");
	});

	it("extracts a diff for a native file_editor str_replace call", () => {
		const events = [
			actionEvent({ toolCallId: "fe-1", toolName: "file_editor" }),
			fileEditorObservationEvent({
				toolCallId: "fe-1",
				path: "/workspace/repo/app/models/user.py",
				command: "str_replace",
				oldContent: "old body",
				newContent: "new body",
			}),
		];

		const messages = eventsToThreadMessages(events, false);

		const parts = messages[0].content as unknown as Array<
			Record<string, unknown>
		>;
		expect(parts[0]).toMatchObject({
			type: "tool-call",
			toolCallId: "fe-1",
			artifact: {
				diffs: [
					{
						path: "/workspace/repo/app/models/user.py",
						oldText: "old body",
						newText: "new body",
					},
				],
			},
		});
	});

	it("extracts a diff with a null oldText for a native file_editor create call", () => {
		const events = [
			actionEvent({ toolCallId: "fe-2", toolName: "file_editor" }),
			fileEditorObservationEvent({
				toolCallId: "fe-2",
				path: "/workspace/repo/app/new_file.py",
				command: "create",
				newContent: "file body",
				prevExist: false,
			}),
		];

		const messages = eventsToThreadMessages(events, false);

		const parts = messages[0].content as unknown as Array<
			Record<string, unknown>
		>;
		expect(parts[0]).toMatchObject({
			type: "tool-call",
			toolCallId: "fe-2",
			artifact: {
				diffs: [
					{
						path: "/workspace/repo/app/new_file.py",
						oldText: null,
						newText: "file body",
					},
				],
			},
		});
	});

	it("does not attach an artifact for a native file_editor view call", () => {
		const events = [
			actionEvent({ toolCallId: "fe-3", toolName: "file_editor" }),
			fileEditorObservationEvent({
				toolCallId: "fe-3",
				path: "/workspace/repo",
				command: "view",
			}),
		];

		const messages = eventsToThreadMessages(events, false);

		const parts = messages[0].content as unknown as Array<
			Record<string, unknown>
		>;
		expect(parts[0]).not.toHaveProperty("artifact");
	});

	it("does not attach an artifact for a failed file_editor edit", () => {
		const events = [
			actionEvent({ toolCallId: "fe-4", toolName: "file_editor" }),
			fileEditorObservationEvent({
				toolCallId: "fe-4",
				path: "/workspace/repo/app/models/user.py",
				command: "str_replace",
				oldContent: "old body",
				newContent: "new body",
				isError: true,
			}),
		];

		const messages = eventsToThreadMessages(events, false);

		const parts = messages[0].content as unknown as Array<
			Record<string, unknown>
		>;
		expect(parts[0]).not.toHaveProperty("artifact");
	});

	describe("services/agent-runner's ACP event types", () => {
		it("joins consecutive agent_message_chunk events into one text part, not one part per chunk", () => {
			const events = [
				userMessage("hi"),
				agentMessageChunk("Hello"),
				agentMessageChunk("!"),
				agentMessageChunk(" How"),
				agentMessageChunk(" can I help?"),
			];

			const messages = eventsToThreadMessages(events, false);

			expect(messages).toHaveLength(2);
			const parts = messages[1].content as unknown as Array<
				Record<string, unknown>
			>;
			expect(parts).toHaveLength(1);
			expect(parts[0]).toMatchObject({
				type: "text",
				text: "Hello! How can I help?",
			});
		});

		it("joins consecutive agent_thought_chunk events into one reasoning part, separate from reply text", () => {
			const events = [
				userMessage("what's 2+2?"),
				agentThoughtChunk("Let me "),
				agentThoughtChunk("think about this."),
				agentMessageChunk("It's 4."),
			];

			const messages = eventsToThreadMessages(events, false);

			expect(messages).toHaveLength(2);
			const parts = messages[1].content as unknown as Array<
				Record<string, unknown>
			>;
			expect(parts).toHaveLength(2);
			expect(parts[0]).toMatchObject({
				type: "reasoning",
				text: "Let me think about this.",
			});
			expect(parts[1]).toMatchObject({ type: "text", text: "It's 4." });
		});

		it("renders a tool_call/tool_call_update pair as one resolved tool-call part", () => {
			const events = [
				acpToolCall({ toolCallId: "tc-1", title: "Running ls" }),
				acpToolCallUpdate({
					toolCallId: "tc-1",
					status: "completed",
					text: "file1.txt\nfile2.txt",
				}),
			];

			const messages = eventsToThreadMessages(events, false);

			const parts = messages[0].content as unknown as Array<
				Record<string, unknown>
			>;
			expect(parts[0]).toMatchObject({
				type: "tool-call",
				toolCallId: "tc-1",
				toolName: "Running ls",
				result: "file1.txt\nfile2.txt",
			});
			expect(parts[0]).not.toHaveProperty("isError");
		});

		it("marks a tool_call_update with status=failed as an error", () => {
			const events = [
				acpToolCall({ toolCallId: "tc-2", title: "Running rm" }),
				acpToolCallUpdate({
					toolCallId: "tc-2",
					status: "failed",
					text: "permission denied",
				}),
			];

			const messages = eventsToThreadMessages(events, false);

			const parts = messages[0].content as unknown as Array<
				Record<string, unknown>
			>;
			expect(parts[0]).toMatchObject({
				toolCallId: "tc-2",
				result: "permission denied",
				isError: true,
			});
		});

		it("ignores turn_end — it carries no user-visible content", () => {
			const events = [
				userMessage("hi"),
				agentMessageChunk("hello!"),
				turnEnd("end_turn"),
			];

			const messages = eventsToThreadMessages(events, false);

			expect(messages).toHaveLength(2);
			const parts = messages[1].content as unknown as Array<
				Record<string, unknown>
			>;
			expect(parts).toHaveLength(1);
			expect(parts[0]).toMatchObject({ type: "text", text: "hello!" });
		});

		it("renders a full chat turn: reply chunks, a tool call, then more reply chunks", () => {
			const events = [
				userMessage("what files are here?"),
				agentMessageChunk("Let me check."),
				acpToolCall({ toolCallId: "tc-3", title: "Running ls" }),
				acpToolCallUpdate({
					toolCallId: "tc-3",
					status: "completed",
					text: "a.txt",
				}),
				agentMessageChunk("There's one file: a.txt"),
				turnEnd("end_turn"),
			];

			const messages = eventsToThreadMessages(events, false);

			expect(messages).toHaveLength(2);
			const parts = messages[1].content as unknown as Array<
				Record<string, unknown>
			>;
			expect(parts).toHaveLength(3);
			expect(parts[0]).toMatchObject({ type: "text", text: "Let me check." });
			expect(parts[1]).toMatchObject({
				type: "tool-call",
				toolCallId: "tc-3",
				result: "a.txt",
			});
			expect(parts[2]).toMatchObject({
				type: "text",
				text: "There's one file: a.txt",
			});
		});
	});
});
