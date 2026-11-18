import { describe, expect, it } from "vitest";
import {
	blocksToText,
	mentionsOf,
	safeHref,
	sameText,
	textToBlocks,
} from "./chat-content";

describe("chat content", () => {
	it("turns lines into paragraphs and drops blank ones", () => {
		const blocks = textToBlocks("one\n\n  \ntwo  ");
		expect(blocks).toHaveLength(2);
		expect(blocks[1]).toEqual({
			type: "paragraph",
			content: [{ type: "text", text: "two", styles: {} }],
		});
		expect(textToBlocks("   \n ")).toEqual([]);
	});

	it("round-trips text through blocks", () => {
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

describe("mentions in chat content", () => {
	const mina = { id: "u-mina", name: "Mina" };
	const ann = { id: "u-ann", name: "Ann" };
	const annLee = { id: "u-annlee", name: "Ann Lee" };

	it("turns @Name into a mention node and keeps the rest as text", () => {
		const [p] = textToBlocks("hey @Mina, look", [mina]);
		expect(p?.content).toEqual([
			{ type: "text", text: "hey ", styles: {} },
			{ type: "teamMention", props: { id: "u-mina", name: "Mina" } },
			{ type: "text", text: ", look", styles: {} },
		]);
	});

	it("prefers the longest matching name and respects word boundaries", () => {
		const [p] = textToBlocks("@Ann Lee and @Ann and @Annabel", [ann, annLee]);
		const ids = (p?.content ?? [])
			.filter((n) => n.type === "teamMention")
			.map((n) => n.props?.id);
		expect(ids).toEqual(["u-annlee", "u-ann"]);
		// "@Annabel" is not a mention of Ann.
		expect(blocksToText([p as never])).toBe("@Ann Lee and @Ann and @Annabel");
	});

	it("drops a mention whose text was deleted", () => {
		const [p] = textToBlocks("no names here", [mina]);
		expect(p?.content).toEqual([
			{ type: "text", text: "no names here", styles: {} },
		]);
	});

	it("shows a mention as @name in plain text and finds it again for an edit", () => {
		const blocks = textToBlocks("ping @Mina", [mina]);
		expect(blocksToText(blocks)).toBe("ping @Mina");
		expect(mentionsOf(blocks)).toEqual([mina]);
		// Edited text keeps the link when the name is kept.
		const edited = textToBlocks("ping again @Mina", mentionsOf(blocks));
		expect(mentionsOf(edited)).toEqual([mina]);
	});
});
