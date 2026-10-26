import { describe, expect, it } from "vitest";
import {
	extractAskQuestion,
	isAskQuestionTool,
	parseAskQuestion,
	parseAskQuestionText,
} from "./ask-question";

describe("ask-question", () => {
	it("recognises prefixed tool names", () => {
		expect(isAskQuestionTool("mcp__paca__ask_user_question")).toBe(true);
		expect(isAskQuestionTool("Bash")).toBe(false);
	});

	it("parses string and object options", () => {
		expect(
			parseAskQuestion({
				question: " Q? ",
				options: ["A", { label: "B", description: "d" }, 3, { label: "" }],
				multiSelect: true,
			}),
		).toEqual({
			question: "Q?",
			options: [{ label: "A" }, { label: "B", description: "d" }],
			multiSelect: true,
		});
		expect(parseAskQuestion({ question: "Q?", options: [] })).toBeNull();
		expect(parseAskQuestionText("not json")).toBeNull();
	});

	it("extracts the question from comment blocks", () => {
		const payload = JSON.stringify({
			type: "paca-ask-question",
			question: "Q?",
			options: [{ label: "A" }],
		});
		const blocks = [
			{ type: "paragraph", content: [{ type: "text", text: "hi" }] },
			{
				type: "codeBlock",
				props: { language: "paca-ask" },
				content: [{ type: "text", text: payload }],
			},
		];
		const found = extractAskQuestion(blocks);
		expect(found?.ask.question).toBe("Q?");
		expect(found?.rest).toEqual([blocks[0]]);
		expect(
			extractAskQuestion([
				{ type: "codeBlock", content: [{ type: "text", text: "{}" }] },
			]),
		).toBeNull();
	});
});

describe("ask-question from a tool call", () => {
	it("matches the Goose-style title", () => {
		expect(isAskQuestionTool("mcp: paca  ask user question")).toBe(true);
	});

	it("falls back to the payload echoed in the tool result", async () => {
		const { parseAskQuestionFromCall } = await import("./ask-question");
		const result = `Question shown.\n\n\`\`\`paca-ask\n${JSON.stringify({
			type: "paca-ask-question",
			question: "Test 123?",
			options: [{ label: "Test 1" }],
			multiSelect: false,
		})}\n\`\`\``;
		expect(parseAskQuestionFromCall("{}", result)?.question).toBe("Test 123?");
		expect(parseAskQuestionFromCall(undefined, "nothing")).toBeNull();
	});
});
