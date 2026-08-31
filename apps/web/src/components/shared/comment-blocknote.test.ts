import { describe, expect, it } from "vitest";
import {
	convertMermaidCodeBlocks,
	normalizeBlockContent,
} from "./comment-blocknote";

describe("normalizeBlockContent", () => {
	it("returns the array unchanged when content is already a block array", () => {
		const blocks = [{ type: "paragraph", content: [] }];
		expect(normalizeBlockContent(blocks)).toBe(blocks);
	});

	it("wraps a plain string into a paragraph block instead of dropping it", () => {
		// This is the exact shape reported in GitHub issue #233: a plain string
		// stored as description/comment content, which used to crash BlockNote's
		// `.map()` calls over blocks.
		const result = normalizeBlockContent("just a plain string") as Array<{
			type: string;
			content: Array<{ text: string }>;
		}>;
		expect(result).toHaveLength(1);
		expect(result[0].type).toBe("paragraph");
		expect(result[0].content[0].text).toBe("just a plain string");
	});

	it("returns an empty array for null, undefined, empty string, or other scalars", () => {
		expect(normalizeBlockContent(null)).toEqual([]);
		expect(normalizeBlockContent(undefined)).toEqual([]);
		expect(normalizeBlockContent("")).toEqual([]);
		expect(normalizeBlockContent(42)).toEqual([]);
		expect(normalizeBlockContent({ type: "paragraph" })).toEqual([]);
	});
});

describe("convertMermaidCodeBlocks", () => {
	it("rewrites a mermaid code block into a mermaid block", () => {
		const input = [
			{
				type: "codeBlock",
				props: { language: "mermaid" },
				content: [{ type: "text", text: "erDiagram\n  A ||--o{ B : has" }],
			},
		];
		expect(convertMermaidCodeBlocks(input)).toEqual([
			{
				type: "mermaid",
				props: { code: "erDiagram\n  A ||--o{ B : has" },
				content: [],
			},
		]);
	});

	it("matches the language case-insensitively", () => {
		const input = [
			{
				type: "codeBlock",
				props: { language: "Mermaid" },
				content: [{ type: "text", text: "graph TD\n A-->B" }],
			},
		];
		const out = convertMermaidCodeBlocks(input) as Array<{ type: string }>;
		expect(out[0].type).toBe("mermaid");
	});

	it("joins multiple inline text runs into the code prop", () => {
		const input = [
			{
				type: "codeBlock",
				props: { language: "mermaid" },
				content: [
					{ type: "text", text: "graph TD\n" },
					{ type: "text", text: "  A-->B" },
				],
			},
		];
		const out = convertMermaidCodeBlocks(input) as Array<{
			props: { code: string };
		}>;
		expect(out[0].props.code).toBe("graph TD\n  A-->B");
	});

	it("leaves non-mermaid code blocks and other blocks untouched", () => {
		const input = [
			{
				type: "codeBlock",
				props: { language: "javascript" },
				content: [{ type: "text", text: "const x = 1;" }],
			},
			{ type: "paragraph", content: [{ type: "text", text: "hi" }] },
		];
		// unchanged → returns the SAME array reference (no needless churn)
		expect(convertMermaidCodeBlocks(input)).toBe(input);
	});

	it("handles an empty mermaid code block", () => {
		const input = [
			{ type: "codeBlock", props: { language: "mermaid" }, content: [] },
		];
		const out = convertMermaidCodeBlocks(input) as Array<{
			props: { code: string };
		}>;
		expect(out[0].props.code).toBe("");
	});

	it("tolerates malformed / non-object entries", () => {
		const input = [null, "x", 42, { type: "paragraph" }];
		expect(convertMermaidCodeBlocks(input)).toBe(input);
	});
});
