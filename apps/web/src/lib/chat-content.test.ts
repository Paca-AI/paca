import { describe, expect, it } from "vitest";
import type { ChatBlock, ChatInline } from "./chat-api";
import { blocksToText, safeHref, sameText } from "./chat-content";

const textToBlocks = (text: string): ChatBlock[] =>
	text.split("\n").map((line) => ({
		type: "paragraph",
		content: [{ type: "text", text: line, styles: {} }],
	}));

describe("chat content", () => {
	it("reads one line per block", () => {
		expect(blocksToText(textToBlocks("a\nb"))).toBe("a\nb");
	});

	it("reads nested inline content and children", () => {
		expect(
			blocksToText([
				{
					type: "paragraph",
					content: [
						{
							type: "link",
							href: "https://x.test",
							content: [{ type: "text", text: "docs" }],
						},
					],
					children: [
						{ type: "paragraph", content: [{ type: "text", text: "child" }] },
					],
				},
			]),
		).toBe("docs\nchild");
	});

	it("allows only http(s) and mailto links", () => {
		expect(safeHref("https://paca.test/x")).toBe("https://paca.test/x");
		expect(safeHref("mailto:a@b.test")).toBe("mailto:a@b.test");
		expect(safeHref("javascript:alert(1)")).toBeUndefined();
		expect(safeHref("  JaVaScRiPt:alert(1)")).toBeUndefined();
		expect(safeHref("data:text/html,x")).toBeUndefined();
		expect(safeHref(undefined)).toBeUndefined();
	});

	it("compares edits by text", () => {
		expect(sameText(textToBlocks("x"), textToBlocks("x"))).toBe(true);
		expect(sameText(textToBlocks("x"), textToBlocks("y"))).toBe(false);
	});
});

describe("references in chat content", () => {
	const para = (...content: ChatInline[]): ChatBlock => ({
		type: "paragraph",
		content,
	});

	it("shows a mention as @name and a task or document as #label", () => {
		const blocks = [
			para(
				{ type: "text", text: "hi " },
				{ type: "teamMention", props: { id: "u1", name: "Mina" } },
				{ type: "text", text: " see " },
				{ type: "taskReference", props: { id: "t1", number: 42 } },
				{ type: "docReference", props: { id: "d1234567-aaaa" } },
			),
		];
		expect(blocksToText(blocks)).toBe("hi @Mina see #42#doc:d1234567");
	});
});
