import { beforeEach, describe, expect, it, vi } from "vitest";

const mocks = vi.hoisted(() => ({
	createTask: vi.fn(),
	updateTask: vi.fn(),
	addComment: vi.fn(),
	createDocument: vi.fn(),
}));

vi.mock("@/lib/interaction-api", () => ({
	createTask: mocks.createTask,
	updateTask: mocks.updateTask,
	addComment: mocks.addComment,
}));
vi.mock("./doc-api", () => ({ createDocument: mocks.createDocument }));

import {
	executeProposal,
	markdownBlocks,
	textBlocks,
} from "./proposal-actions";

beforeEach(() => vi.resetAllMocks());

describe("markdownBlocks", () => {
	it("maps headings, bullets and paragraphs and skips blank lines", () => {
		const blocks = markdownBlocks("# Notes\n\n- one\n* two\nplain text");
		expect(blocks).toEqual([
			{
				type: "heading",
				props: { level: 1 },
				content: [{ type: "text", text: "Notes", styles: {} }],
			},
			{
				type: "bulletListItem",
				content: [{ type: "text", text: "one", styles: {} }],
			},
			{
				type: "bulletListItem",
				content: [{ type: "text", text: "two", styles: {} }],
			},
			{
				type: "paragraph",
				content: [{ type: "text", text: "plain text", styles: {} }],
			},
		]);
	});

	it("never returns an empty document", () => {
		expect(markdownBlocks("  \n ")).toHaveLength(1);
		expect(textBlocks("")).toHaveLength(1);
	});
});

describe("executeProposal", () => {
	it("creates a task with the edited title, then sets description and priority", async () => {
		mocks.createTask.mockResolvedValue({ id: "t1", task_number: 7 });
		const result = await executeProposal("p1", "task.create", {
			title: "  Fix login ",
			description: "Safari only",
			priority: "High",
		});
		expect(mocks.createTask).toHaveBeenCalledWith("p1", { title: "Fix login" });
		expect(mocks.updateTask).toHaveBeenCalledWith("p1", "t1", {
			description: textBlocks("Safari only"),
			importance: 3,
		});
		expect(result).toEqual({ task_id: "t1", task_number: 7 });
	});

	it("skips the second request when there is nothing else to set", async () => {
		mocks.createTask.mockResolvedValue({ id: "t2", task_number: 8 });
		await executeProposal("p1", "task.create", { title: "Only a title" });
		expect(mocks.updateTask).not.toHaveBeenCalled();
	});

	it("passes sprint and assignee to the create call", async () => {
		mocks.createTask.mockResolvedValue({ id: "t3", task_number: 9 });
		await executeProposal("p1", "task.create", {
			title: "x",
			sprint_id: "s1",
			assignee_id: "u1",
		});
		expect(mocks.createTask).toHaveBeenCalledWith("p1", {
			title: "x",
			sprint_id: "s1",
			assignee_ids: ["u1"],
		});
	});

	it("updates only the fields the proposal carries, and clears with an empty string", async () => {
		await executeProposal("p1", "task.update", {
			task_id: "t1",
			status_id: "st",
			sprint_id: "",
			priority: "low",
		});
		expect(mocks.updateTask).toHaveBeenCalledWith("p1", "t1", {
			status_id: "st",
			sprint_id: null,
			importance: 1,
		});
	});

	it("posts a comment as blocks", async () => {
		await executeProposal("p1", "task.comment", { task_id: "t1", body: "hi" });
		expect(mocks.addComment).toHaveBeenCalledWith("p1", "t1", textBlocks("hi"));
	});

	it("creates a doc from the notes", async () => {
		mocks.createDocument.mockResolvedValue({ id: "d1" });
		const result = await executeProposal("p1", "doc.create_from_notes", {
			title: " Notes ",
			markdown: "- a",
		});
		expect(mocks.createDocument).toHaveBeenCalledWith("p1", {
			title: "Notes",
			content: markdownBlocks("- a"),
		});
		expect(result).toEqual({ doc_id: "d1" });
	});

	it("lets a write error through, so the card reports a failure", async () => {
		mocks.createTask.mockRejectedValue(new Error("403"));
		await expect(
			executeProposal("p1", "task.create", { title: "x" }),
		).rejects.toThrow("403");
	});
});
