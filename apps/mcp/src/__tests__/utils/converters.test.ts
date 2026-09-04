import { describe, expect, it } from "vitest";
import {
	blocknoteToMarkdown,
	markdownToBlocknote,
} from "../../utils/converters.js";

// ---------------------------------------------------------------------------
// blocknoteToMarkdown
// ---------------------------------------------------------------------------

describe("blocknoteToMarkdown", () => {
	it("returns empty string for null input", () => {
		expect(blocknoteToMarkdown(null)).toBe("");
	});

	it("returns empty string for an empty array", () => {
		expect(blocknoteToMarkdown([])).toBe("");
	});

	it("returns .text value when input is an object with a string text property", () => {
		const input = { text: "plain text content", content: [] };
		expect(blocknoteToMarkdown(input)).toBe("plain text content");
	});

	it("returns empty string for an object without .text and with non-array .content", () => {
		const input = { content: "not-an-array" as any };
		expect(blocknoteToMarkdown(input)).toBe("");
	});

	it("converts simple paragraph blocks to markdown text", () => {
		const blocks = [
			{
				id: "1",
				type: "paragraph",
				props: {},
				content: [{ type: "text", text: "Hello world", styles: {} }],
				children: [],
			},
		];
		const result = blocknoteToMarkdown(blocks);
		expect(result).toContain("Hello world");
	});

	it("converts heading blocks to markdown headings", () => {
		const blocks = [
			{
				id: "1",
				type: "heading",
				props: { level: 1 },
				content: [{ type: "text", text: "My Heading", styles: {} }],
				children: [],
			},
		];
		const result = blocknoteToMarkdown(blocks);
		expect(result).toContain("My Heading");
	});

	it("handles teamMention inline content by converting to @name (id: id) text", () => {
		const blocks = [
			{
				id: "1",
				type: "paragraph",
				props: {},
				content: [
					{
						type: "teamMention",
						props: { name: "Alice", id: "user-1" },
					},
				],
				children: [],
			},
		];
		const result = blocknoteToMarkdown(blocks);
		expect(result).toContain("@Alice (id: user-1)");
	});

	it("handles taskReference by converting to #title (id: id) text", () => {
		const blocks = [
			{
				id: "1",
				type: "paragraph",
				props: {},
				content: [
					{
						type: "taskReference",
						props: { title: "Fix bug", id: "task-42" },
					},
				],
				children: [],
			},
		];
		const result = blocknoteToMarkdown(blocks);
		expect(result).toContain("#Fix bug (id: task-42)");
	});

	it("handles docReference by converting to 📄 title (id: id) text", () => {
		const blocks = [
			{
				id: "1",
				type: "paragraph",
				props: {},
				content: [
					{
						type: "docReference",
						props: { title: "Design Doc", id: "doc-1" },
					},
				],
				children: [],
			},
		];
		const result = blocknoteToMarkdown(blocks);
		expect(result).toContain("📄 Design Doc (id: doc-1)");
	});

	it("handles teamMention with no id gracefully", () => {
		const blocks = [
			{
				id: "1",
				type: "paragraph",
				props: {},
				content: [
					{
						type: "teamMention",
						props: { name: "Bob", id: "" },
					},
				],
				children: [],
			},
		];
		const result = blocknoteToMarkdown(blocks);
		expect(result).toContain("@Bob");
	});

	// Regression test: mermaid/annotationCard are custom block types (see
	// apps/web/src/components/shared/blocknote-schema.tsx) the bare
	// BlockNoteEditor this file builds has never heard of. Left unconverted,
	// blocksToMarkdownLossy looks up the block's type in its own schema and
	// throws "Cannot read properties of undefined (reading 'propSchema')" —
	// this broke every list_tasks call for a project with a mermaid diagram
	// or a comment-reference card in any task's description (conversation
	// 269da86a-05c8-4e12-9619-49397a85949e).
	it("handles mermaid blocks by degrading to a fenced mermaid code block", () => {
		const blocks = [
			{
				id: "1",
				type: "mermaid",
				props: { code: "graph TD; A-->B;" },
				content: [],
				children: [],
			},
		];
		const result = blocknoteToMarkdown(blocks);
		expect(result).toContain("```mermaid");
		expect(result).toContain("graph TD; A-->B;");
	});

	it("handles annotationCard blocks by degrading to a plain reference line", () => {
		const blocks = [
			{
				id: "1",
				type: "annotationCard",
				props: {
					id: "annotation-1",
					projectId: "p1",
					environmentId: "e1",
					portForwardId: "pf1",
				},
				content: [],
				children: [],
			},
		];
		const result = blocknoteToMarkdown(blocks);
		expect(result).toContain("[Comment reference (id: annotation-1)]");
	});

	it("does not crash when mermaid/annotationCard blocks are mixed with ordinary content", () => {
		const blocks = [
			{
				id: "1",
				type: "paragraph",
				props: {},
				content: [{ type: "text", text: "Before", styles: {} }],
				children: [],
			},
			{
				id: "2",
				type: "mermaid",
				props: { code: "graph TD; A-->B;" },
				content: [],
				children: [],
			},
			{
				id: "3",
				type: "annotationCard",
				props: {
					id: "annotation-1",
					projectId: "p1",
					environmentId: "e1",
					portForwardId: "pf1",
				},
				content: [],
				children: [],
			},
			{
				id: "4",
				type: "paragraph",
				props: {},
				content: [{ type: "text", text: "After", styles: {} }],
				children: [],
			},
		];
		const result = blocknoteToMarkdown(blocks);
		expect(result).toContain("Before");
		expect(result).toContain("```mermaid");
		expect(result).toContain("[Comment reference (id: annotation-1)]");
		expect(result).toContain("After");
	});
});

// ---------------------------------------------------------------------------
// markdownToBlocknote
// ---------------------------------------------------------------------------

describe("markdownToBlocknote", () => {
	it("returns empty array for empty string", () => {
		expect(markdownToBlocknote("")).toEqual([]);
	});

	it("returns empty array for whitespace-only string", () => {
		expect(markdownToBlocknote("   ")).toEqual([]);
	});

	it("returns an array of blocks for non-empty markdown", () => {
		const result = markdownToBlocknote("Hello world");
		expect(Array.isArray(result)).toBe(true);
		expect(result.length).toBeGreaterThan(0);
	});

	it("parses a simple paragraph into blocks", () => {
		const result = markdownToBlocknote("Some text");
		expect(result[0]).toMatchObject({ type: "paragraph" });
	});

	it("round-trips back to markdown with the same content", () => {
		const markdown = "Simple paragraph";
		const blocks = markdownToBlocknote(markdown);
		const back = blocknoteToMarkdown(blocks);
		expect(back.trim()).toContain("Simple paragraph");
	});
});
