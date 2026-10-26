import { describe, expect, it, vi } from "vitest";
import { getAskTools, handleAskTool } from "../../tools/ask-tools.js";

function makeClients() {
	return {
		task: { addTaskComment: vi.fn().mockResolvedValue({}) } as any,
		doc: { addDocumentComment: vi.fn().mockResolvedValue({}) } as any,
	};
}

describe("ask_user_question", () => {
	it("is registered", () => {
		expect(getAskTools().map((t) => t.name)).toEqual(["ask_user_question"]);
	});

	it("posts nothing in chat mode", async () => {
		const { task, doc } = makeClients();
		const res = await handleAskTool(
			"ask_user_question",
			{ question: "Which?", options: ["A", "B"] },
			task,
			doc,
		);
		expect(res.content[0].text).toMatch(/next message/);
		expect(res.content[0].text).toMatch(/```paca-ask\n.*"Which\?"/);
		expect(task.addTaskComment).not.toHaveBeenCalled();
		expect(doc.addDocumentComment).not.toHaveBeenCalled();
	});

	it("posts a task comment carrying the question payload", async () => {
		const { task, doc } = makeClients();
		await handleAskTool(
			"ask_user_question",
			{
				question: "Which?",
				options: ["A", { label: "B", description: "slower" }],
				multiSelect: true,
				projectId: "p1",
				taskId: "t1",
			},
			task,
			doc,
		);
		const [projectId, taskId, input] = task.addTaskComment.mock.calls[0];
		expect([projectId, taskId]).toEqual(["p1", "t1"]);
		expect(input.content).toMatch(/^```paca-ask\n/);
		const json = JSON.parse(input.content.split("\n").slice(1, -1).join("\n"));
		expect(json).toEqual({
			type: "paca-ask-question",
			question: "Which?",
			options: [{ label: "A" }, { label: "B", description: "slower" }],
			multiSelect: true,
		});
	});

	it("prepends an @mention when asked", async () => {
		const { task, doc } = makeClients();
		await handleAskTool(
			"ask_user_question",
			{
				question: "Q?",
				options: ["A"],
				projectId: "p1",
				taskId: "t1",
				mention: "@alice",
			},
			task,
			doc,
		);
		expect(task.addTaskComment.mock.calls[0][2].content).toMatch(
			/^@alice\n\n```paca-ask\n/,
		);
	});

	it("posts a doc comment for docId", async () => {
		const { task, doc } = makeClients();
		await handleAskTool(
			"ask_user_question",
			{ question: "Which?", options: ["A"], projectId: "p1", docId: "d1" },
			task,
			doc,
		);
		expect(doc.addDocumentComment).toHaveBeenCalledWith(
			"p1",
			"d1",
			expect.stringContaining("paca-ask-question"),
		);
	});

	it("requires projectId with taskId", async () => {
		const { task, doc } = makeClients();
		await expect(
			handleAskTool(
				"ask_user_question",
				{ question: "Q?", options: ["A"], taskId: "t1" },
				task,
				doc,
			),
		).rejects.toThrow(/projectId/);
	});
});
