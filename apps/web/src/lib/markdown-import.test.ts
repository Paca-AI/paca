import { describe, expect, it } from "vitest";
import { isMarkdownFile, markdownToDocument } from "./markdown-import";

describe("isMarkdownFile", () => {
	it("recognizes Markdown by extension or type", () => {
		expect(isMarkdownFile({ name: "Notes.MD" })).toBe(true);
		expect(isMarkdownFile({ name: "a.markdown" })).toBe(true);
		expect(isMarkdownFile({ name: "a.txt", type: "text/markdown" })).toBe(true);
		expect(isMarkdownFile({ name: "a.txt", type: "text/plain" })).toBe(false);
		expect(isMarkdownFile({ name: "mdx.pdf" })).toBe(false);
	});
});

describe("markdownToDocument", () => {
	it("takes a leading level-1 heading as the title and drops it", () => {
		const { title, blocks } = markdownToDocument(
			"# Evaluation criteria\n\nFirst paragraph.\n",
			"process.md",
		);
		expect(title).toBe("Evaluation criteria");
		expect(blocks).toHaveLength(1);
		expect((blocks[0] as { type: string }).type).toBe("paragraph");
	});

	it("titles a file without a leading heading by its name", () => {
		const { title, blocks } = markdownToDocument(
			"Just text\n\n## Sub\n",
			"Meeting notes.md",
		);
		expect(title).toBe("Meeting notes");
		expect(blocks).toHaveLength(2);
	});

	it("keeps a heading that is not the first block, and a deeper first heading", () => {
		expect(markdownToDocument("## Only\n", "x.md").title).toBe("x");
		expect(markdownToDocument("text\n\n# Later\n", "y.md").title).toBe("y");
	});

	it("handles an empty file and a byte order mark", () => {
		expect(markdownToDocument("", "Empty.md")).toEqual({
			title: "Empty",
			blocks: [],
		});
		expect(markdownToDocument("﻿# Titled\n", "z.md").title).toBe("Titled");
	});
});
