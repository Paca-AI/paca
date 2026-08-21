import { describe, expect, it } from "vitest";
import type {
	ProjectChatTurnHistoryItem,
	ProjectChatTurnStatus,
} from "@/lib/agent-api";
import {
	canPublishProjectChatConclusion,
	projectChatTurnsToThreadMessages,
} from "./project-chat-to-thread-messages";

function item(
	status: ProjectChatTurnStatus,
	options: { index?: number; stableOutput?: string; withResult?: boolean } = {},
): ProjectChatTurnHistoryItem {
	const index = options.index ?? 1;
	const turnId = `turn-${index}`;
	return {
		turn: {
			id: turnId,
			turn_index: index,
			input_text: `question ${index}`,
			status,
			created_at: `2026-08-18T00:0${index}:00Z`,
		},
		result:
			options.withResult === false ||
			status === "queued" ||
			status === "running"
				? null
				: {
						turn_id: turnId,
						run_id: `run-${index}`,
						generated_by_agent_id: "agent-1",
						terminal_status: status,
						stable_output: options.stableOutput,
						runtime_disposition: "retired",
						created_at: `2026-08-18T00:0${index}:30Z`,
					},
	} as ProjectChatTurnHistoryItem;
}

const terminalFallback = (value: ProjectChatTurnHistoryItem) =>
	`terminal:${value.turn.status}`;

describe("projectChatTurnsToThreadMessages", () => {
	it("orders turns and renders one authoritative stable answer", () => {
		const messages = projectChatTurnsToThreadMessages(
			[
				item("succeeded", { index: 2, stableOutput: "second answer" }),
				item("succeeded", { index: 1, stableOutput: "first answer" }),
			],
			{ terminalFallback },
		);

		expect(messages.map((message) => message.id)).toEqual([
			"turn-1:user",
			"turn-1:assistant",
			"turn-2:user",
			"turn-2:assistant",
		]);
		expect(messages[1]?.content).toEqual([
			{ type: "text", text: "first answer" },
		]);
		expect(
			canPublishProjectChatConclusion(
				item("succeeded", {
					stableOutput: "stable",
				}),
			),
		).toBe(true);
	});

	it.each([
		"failed",
		"stopped",
		"cancelled",
		"timed_out",
		"no_output",
	] as const)("never treats %s execution text as a successful answer", (status) => {
		const hostile = item(status, { stableOutput: "partial private execution" });
		const messages = projectChatTurnsToThreadMessages([hostile], {
			terminalFallback,
		});

		expect(messages).toHaveLength(2);
		expect(messages[1]?.content).toEqual([
			{ type: "text", text: `terminal:${status}` },
		]);
		expect(messages[1]?.status?.type).toBe("incomplete");
		expect(JSON.stringify(messages)).not.toContain("partial private execution");
		expect(canPublishProjectChatConclusion(hostile)).toBe(false);
	});

	it("does not invent an assistant answer before a result exists", () => {
		const messages = projectChatTurnsToThreadMessages(
			[item("running", { withResult: false })],
			{ terminalFallback },
		);

		expect(messages).toHaveLength(1);
		expect(messages[0]?.role).toBe("user");
	});

	it("requires non-empty stable output before publishing", () => {
		expect(
			canPublishProjectChatConclusion(
				item("succeeded", { stableOutput: "   " }),
			),
		).toBe(false);
	});
});
