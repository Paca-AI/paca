import { describe, expect, it } from "vitest";
import { auditEdit, SpliceError, spliceEdit } from "../../tools/doc-edit.js";
import {
	blocknoteToMarkdown,
	markdownToBlocknote,
} from "../../utils/converters.js";

/** Blocks the way the API returns them: parsed from Markdown, then passed
 *  through JSON, with a few props Markdown cannot express. */
function stored(markdown: string, extra?: (blocks: any[]) => void): any[] {
	const blocks = JSON.parse(JSON.stringify(markdownToBlocknote(markdown)));
	extra?.(blocks);
	return blocks;
}

/** Asserts every block outside `changed` is the very object passed in and
 *  serializes to the JSON it had before the edit. */
function expectUntouched(
	before: string[],
	input: any[],
	output: any[],
	changed: number[],
) {
	expect(output.length).toBe(input.length);
	for (let i = 0; i < input.length; i++) {
		if (changed.includes(i)) continue;
		expect(output[i]).toBe(input[i]);
		expect(output[i]).toEqual(JSON.parse(before[i]));
		expect(JSON.stringify(output[i])).toBe(before[i]);
	}
}

const TEN = [
	"# Release plan",
	"Intro paragraph with **bold** text.",
	"## Steps",
	"1. Build the image\n2. Push the image\n3. Deploy",
	"- [ ] write notes\n- [x] tag release",
	"| env | url |\n|---|---|\n| prod | example.com |",
	"```bash\n# not a heading\nmake deploy\n```",
	"> Careful with prod.",
	"## Rollback",
	"Run the previous tag and watch the dashboard.",
].join("\n\n");

describe("spliceEdit", () => {
	it("one-line edit leaves other blocks deep-equal and identical in JSON", () => {
		const blocks = stored(TEN, (b) => {
			b[1].props.textColor = "red";
			b[0].props.backgroundColor = "blue";
		});
		const before = blocks.map((b) => JSON.stringify(b));
		const target = blocks.findIndex(
			(b) => b.type === "paragraph" && /previous tag/.test(JSON.stringify(b)),
		);

		const res = spliceEdit(blocks, "previous tag", "last good tag", false);

		expect(res.replaced).toBe(1);
		expectUntouched(before, blocks, res.blocks, [target]);
		expect(res.blocks[target]).not.toBe(blocks[target]);
		expect(res.blocks[target].id).toBe(blocks[target].id);
		expect(blocknoteToMarkdown([res.blocks[target]])).toBe(
			"Run the last good tag and watch the dashboard.\n",
		);
		expect(blocknoteToMarkdown(res.blocks)).toBe(
			blocknoteToMarkdown(blocks).replace("previous tag", "last good tag"),
		);
	});

	it("keeps Markdown-less props of the edited block", () => {
		const blocks = stored(TEN, (b) => {
			b[1].props.textColor = "red";
		});
		const res = spliceEdit(
			blocks,
			"Intro paragraph",
			"Opening paragraph",
			false,
		);
		expect(res.blocks[1].props.textColor).toBe("red");
		expect(res.blocks[1].id).toBe(blocks[1].id);
	});

	it("edit spanning two blocks rewrites only those two", () => {
		const blocks = stored(TEN);
		const before = blocks.map((b) => JSON.stringify(b));
		const last = blocks.length - 1;

		const res = spliceEdit(
			blocks,
			"## Rollback\n\nRun the previous",
			"## Roll back\n\nRedeploy the previous",
			false,
		);

		expectUntouched(before, blocks, res.blocks, [last - 1, last]);
		expect(blocknoteToMarkdown(res.blocks.slice(last - 1))).toBe(
			"## Roll back\n\nRedeploy the previous tag and watch the dashboard.\n",
		);
	});

	it("edits one item of a numbered list without renumbering or touching the others", () => {
		const blocks = stored(TEN);
		const before = blocks.map((b) => JSON.stringify(b));
		const items = blocks
			.map((b, i) => (b.type === "numberedListItem" ? i : -1))
			.filter((i) => i >= 0);
		expect(items).toHaveLength(3);

		const res = spliceEdit(blocks, "Push the image", "Push the tag", false);

		expectUntouched(before, blocks, res.blocks, [items[1]]);
		expect(res.blocks[items[1]].props).toEqual(blocks[items[1]].props);
		expect(blocknoteToMarkdown(res.blocks)).toContain(
			"1. Build the image\n\n2. Push the tag\n\n3. Deploy\n",
		);
	});

	it("finds text that includes the list number", () => {
		const blocks = stored(TEN);
		const res = spliceEdit(blocks, "3. Deploy", "3. Deploy to prod", false);
		expect(blocknoteToMarkdown(res.blocks)).toContain("3. Deploy to prod\n");
	});

	it("keeps check items and an empty paragraph it did not touch", () => {
		const blocks = stored("- [ ] one\n- [x] two\n- [ ] three\n\nafter", (b) => {
			b.splice(3, 0, {
				id: "empty",
				type: "paragraph",
				props: {},
				content: [],
				children: [],
			});
		});
		const before = blocks.map((b) => JSON.stringify(b));

		const res = spliceEdit(blocks, "three", "3", false);

		expectUntouched(before, blocks, res.blocks, [2]);
		expect(res.blocks[2].props.checked).toBe(false);
	});

	it("keeps a mention chip in a block it did not touch", () => {
		const blocks = stored("first para\n\nsecond para", (b) => {
			b[1].content.push({
				type: "teamMention",
				props: { id: "u1", name: "Ana", avatar: "" },
			});
		});
		const before = blocks.map((b) => JSON.stringify(b));
		const res = spliceEdit(blocks, "first", "1st", false);
		expectUntouched(before, blocks, res.blocks, [0]);
	});

	it("zero matches is an error that says so", () => {
		const blocks = stored(TEN);
		expect(() => spliceEdit(blocks, "nowhere", "x", false)).toThrow(
			SpliceError,
		);
		try {
			spliceEdit(blocks, "nowhere", "x", false);
		} catch (err) {
			expect((err as SpliceError).kind).toBe("not_found");
			expect((err as Error).message).toMatch(/not found/i);
		}
	});

	it("multiple matches without replaceAll is an error listing the lines", () => {
		const blocks = stored(TEN);
		try {
			spliceEdit(blocks, "the image", "it", false);
			expect.unreachable();
		} catch (err) {
			expect(err).toBeInstanceOf(SpliceError);
			expect((err as SpliceError).kind).toBe("ambiguous");
			// Lines of the full Markdown: "1. Build the image" and "2. Push the image".
			const md = blocknoteToMarkdown(blocks).split("\n");
			const want = md
				.map((l, i) => (l.includes("the image") ? i + 1 : 0))
				.filter(Boolean);
			expect((err as SpliceError).lines).toEqual(want);
			expect((err as Error).message).toContain(`lines ${want.join(", ")}`);
		}
	});

	it("replaceAll replaces every match and leaves the rest alone", () => {
		const blocks = stored(TEN);
		const before = blocks.map((b) => JSON.stringify(b));
		const items = blocks
			.map((b, i) => (b.type === "numberedListItem" ? i : -1))
			.filter((i) => i >= 0);

		const res = spliceEdit(blocks, "the image", "the container", true);

		expect(res.replaced).toBe(2);
		expectUntouched(before, blocks, res.blocks, [items[0], items[1]]);
		expect(blocknoteToMarkdown(res.blocks)).toBe(
			blocknoteToMarkdown(blocks).replaceAll("the image", "the container"),
		);
	});

	it("deleting a whole block removes only that block", () => {
		const blocks = stored(TEN);
		const res = spliceEdit(blocks, "> Careful with prod.\n\n", "", false);
		expect(res.blocks.length).toBe(blocks.length - 1);
		const kept = blocks.filter((b) => b.type !== "quote");
		for (const [i, b] of kept.entries()) expect(res.blocks[i]).toBe(b);
	});

	it("does not edit the input array", () => {
		const blocks = stored(TEN);
		const copy = [...blocks];
		const json = JSON.stringify(blocks);
		spliceEdit(blocks, "Deploy", "Ship", false);
		expect(blocks).toEqual(copy);
		for (const [i, b] of blocks.entries()) expect(b).toBe(copy[i]);
		expect(JSON.stringify(blocks)).toBe(json);
	});
});

// ── Blocks Markdown cannot carry ──────────────────────────────────────────────

const t = (text: string, styles: Record<string, unknown> = {}) => ({
	type: "text",
	text,
	styles,
});
const P = {
	textColor: "default",
	backgroundColor: "default",
	textAlignment: "left",
};
let seq = 0;
const para = (text: any, props = {}, children: any[] = []) => ({
	id: `p${++seq}`,
	type: "paragraph",
	props: { ...P, ...props },
	content: typeof text === "string" ? [t(text)] : text,
	children,
});
const item = (
	type: string,
	text: string,
	props = {},
	children: any[] = [],
) => ({
	id: `i${++seq}`,
	type,
	props: { ...P, ...props },
	content: [t(text)],
	children,
});

function expectUnsafe(
	blocks: any[],
	oldString: string,
	line: number,
	why: RegExp,
) {
	try {
		spliceEdit(blocks, oldString, `${oldString}X`, false);
		expect.unreachable("the edit should be refused");
	} catch (err) {
		expect(err).toBeInstanceOf(SpliceError);
		expect((err as SpliceError).kind).toBe("unsafe");
		expect((err as SpliceError).lines).toEqual([line]);
		expect((err as Error).message).toContain(`line ${line}`);
		expect((err as Error).message).toMatch(why);
		expect((err as Error).message).toMatch(/write_doc|web editor/);
	}
}

describe("spliceEdit refuses blocks Markdown cannot carry", () => {
	it("a table without a header row", () => {
		const tbl = {
			id: "tb",
			type: "table",
			props: { textColor: "default" },
			content: {
				type: "tableContent",
				columnWidths: [null, null],
				rows: [
					{ cells: [[t("h1")], [t("h2")]] },
					{ cells: [[t("a")], [t("b")]] },
				],
			},
			children: [],
		};
		expectUnsafe([para("x"), tbl, para("y")], "| a", 3, /table/);
	});

	it("a mermaid diagram", () => {
		const m = {
			id: "m",
			type: "mermaid",
			props: { code: "graph TD; A-->B" },
			children: [],
		};
		expectUnsafe([m, para("x")], "A-->B", 1, /mermaid/);
	});

	it("a toggle list item", () => {
		expectUnsafe(
			[item("toggleListItem", "t", {}, [para("inside")]), para("x")],
			"inside",
			1,
			/toggleListItem/,
		);
	});

	it("a paragraph with nested blocks", () => {
		expectUnsafe(
			[
				para("parent", {}, [para("child", { textColor: "blue" })]),
				para("after"),
			],
			"child",
			1,
			/nested/,
		);
	});

	it("keeps a nested block's colour and id when its parent is edited", () => {
		const sub = item("bulletListItem", "sub", { textColor: "red" });
		const blocks = [item("bulletListItem", "top", {}, [sub]), para("z")];
		const res = spliceEdit(blocks, "top", "TOP", false);
		expect(res.blocks[0].children[0]).toBe(sub);
		expect(res.blocks[0].id).toBe(blocks[0].id);
	});

	it("mention and document reference chips", () => {
		expectUnsafe(
			[
				para([
					t("hi "),
					{ type: "teamMention", props: { id: "u1", name: "Ann", avatar: "" } },
					t(" there"),
				]),
				para("x"),
			],
			"there",
			1,
			/teamMention/,
		);
		expectUnsafe(
			[
				para([
					t("see "),
					{ type: "docReference", props: { id: "d1", title: "Spec" } },
				]),
				para("x"),
			],
			"see",
			1,
			/docReference/,
		);
	});

	it("underline and coloured text", () => {
		expectUnsafe(
			[
				para("x"),
				para([t("under", { underline: true }), t(" bold", { bold: true })]),
			],
			"bold",
			3,
			/underline/,
		);
		expectUnsafe(
			[para([t("red", { textColor: "red" }), t(" plain")])],
			"plain",
			1,
			/textColor/,
		);
	});

	it("still edits a block whose shape Markdown keeps, with its nested blocks reused", () => {
		const child = item("bulletListItem", "sub");
		const blocks = [item("bulletListItem", "top", {}, [child]), para("z")];
		const res = spliceEdit(blocks, "top", "TOP", false);
		expect(res.blocks[0]).not.toBe(blocks[0]);
		expect(res.blocks[0].id).toBe(blocks[0].id);
		expect(res.blocks[0].children[0]).toBe(child);
		expect(res.blocks[1]).toBe(blocks[1]);
	});

	it("does not refuse an unsafe block the edit does not touch", () => {
		const blocks = [para("parent", {}, [para("child")]), para("after")];
		const res = spliceEdit(blocks, "after", "AFTER", false);
		expect(res.blocks[0]).toBe(blocks[0]);
	});
});

describe("spliceEdit edits paragraphs with whitespace Markdown does not show", () => {
	it.each([
		["a trailing space", "has word "],
		["a leading space", " has word"],
		["a double space", "has  word here"],
		["a tab", "has\tword"],
		["a trailing line break", "has word\n"],
		["a soft line break", "has word\nnext"],
	])("%s", (_name, text) => {
		const blocks = [para(text), para("after")];
		const res = spliceEdit(blocks, "word", "WORD", false);
		expect(res.blocks[0].id).toBe(blocks[0].id);
		expect(JSON.stringify(res.blocks[0].content)).toContain("WORD");
		expect(res.blocks[1]).toBe(blocks[1]);
	});

	it("keeps a soft line break without adding a space after it", () => {
		const blocks = [para("l1 word\nl2"), para("after")];
		const res = spliceEdit(blocks, "word", "WORD", false);
		expect(res.blocks[0].content).toEqual([t("l1 WORD\nl2")]);
	});

	it("keeps two line breaks in a row without spaces", () => {
		const blocks = [para("x\n\ny word")];
		const res = spliceEdit(blocks, "word", "WORD", false);
		expect(res.blocks[0].content).toEqual([t("x\n\ny WORD")]);
	});
});

describe("spliceEdit deletes, merges and splits blocks with props", () => {
	it("deletes a red paragraph", () => {
		const blocks = [para("a"), para("red", { textColor: "red" }), para("b")];
		const res = spliceEdit(blocks, "red\n\n", "", false);
		expect(res.blocks).toEqual([blocks[0], blocks[2]]);
		expect(res.blocks[0]).toBe(blocks[0]);
		expect(res.blocks[1]).toBe(blocks[2]);
	});

	it("deletes a paragraph with nested blocks", () => {
		const blocks = [
			para("a"),
			para("parent", { textColor: "red" }, [para("child")]),
			para("b"),
		];
		const md = blocknoteToMarkdown([blocks[1]]);
		const res = spliceEdit(blocks, `${md.trimEnd()}\n\n`, "", false);
		expect(res.blocks).toHaveLength(2);
		expect(res.blocks[0]).toBe(blocks[0]);
		expect(res.blocks[1]).toBe(blocks[2]);
	});

	const differ = (lines: string) =>
		`The edit merges, splits or retypes the blocks on ${lines}, which edit_doc cannot rewrite from Markdown: they have different colours or alignment, so the new blocks cannot keep them. Nothing was changed. Edit those blocks in the web editor, or rewrite the document with write_doc if losing that is acceptable.`;
	function expectRefused(fn: () => unknown, message: string, lines: number[]) {
		try {
			fn();
			expect.unreachable("the edit should be refused");
		} catch (err) {
			expect((err as Error).message).toBe(message);
			expect((err as SpliceError).kind).toBe("unsafe");
			expect((err as SpliceError).lines).toEqual(lines);
		}
	}

	it("refuses to put a red block's colour on another block's text", () => {
		const blocks = [para("A", { textColor: "red" }), para("B"), para("z")];
		expectRefused(
			() => spliceEdit(blocks, "A\n\nB", "B2", false),
			differ("lines 1 and 3"),
			[1, 3],
		);
	});

	it("refuses to merge a red and a blue block", () => {
		const blocks = [
			para("A", { textColor: "red" }),
			para("B", { textColor: "blue" }),
		];
		expectRefused(
			() => spliceEdit(blocks, "A\n\nB", "A B", false),
			differ("lines 1 and 3"),
			[1, 3],
		);
	});

	it("refuses to merge a plain block with a red one", () => {
		const blocks = [para("A"), para("B", { textColor: "red" })];
		expectRefused(
			() => spliceEdit(blocks, "A\n\nB", "A B", false),
			differ("lines 1 and 3"),
			[1, 3],
		);
	});

	it("refuses to turn a red paragraph into list items", () => {
		const blocks = [para("x", { textColor: "red" }), para("z")];
		expectRefused(
			() => spliceEdit(blocks, "x", "* one\n* two", false),
			"The edit touches the paragraph block on line 1, which edit_doc cannot rewrite from Markdown: it would become bulletListItem blocks, which cannot keep its colour or alignment. Nothing was changed. Edit that block in the web editor, or rewrite the document with write_doc if losing that is acceptable.",
			[1],
		);
	});

	it("refuses to turn a blue paragraph into a heading", () => {
		const blocks = [para("Title", { textColor: "blue" }), para("b")];
		expect(() => spliceEdit(blocks, "Title", "# Title", false)).toThrow(
			/it would become heading blocks, which cannot keep its colour or alignment/,
		);
	});

	it("turns a plain paragraph into a heading or list items", () => {
		const blocks = [para("Title"), para("x"), para("b")];
		const res = spliceEdit(blocks, "Title", "# Title", false);
		expect(res.blocks[0].type).toBe("heading");
		const res2 = spliceEdit(blocks, "x", "* one\n* two", false);
		expect(res2.blocks.map((b) => b.type)).toEqual([
			"paragraph",
			"bulletListItem",
			"bulletListItem",
			"paragraph",
		]);
	});

	it("merges blocks that share their props, keeping them", () => {
		const blocks = [
			para("A", { textColor: "red" }),
			para("B", { textColor: "red" }),
			para("C"),
			para("D"),
		];
		const res = spliceEdit(blocks, "A\n\nB", "A B", false);
		expect(res.blocks[0].content).toEqual([t("A B")]);
		expect(res.blocks[0].props.textColor).toBe("red");
		const res2 = spliceEdit(blocks, "C\n\nD", "C D", false);
		expect(res2.blocks[2].props.textColor).toBe("default");
	});

	it("a split gives both halves the props", () => {
		const blocks = [para("one two", { textAlignment: "center" }), para("b")];
		const res = spliceEdit(blocks, "one two", "one\n\ntwo", false);
		expect(res.blocks).toHaveLength(3);
		expect(res.blocks[0].props.textAlignment).toBe("center");
		expect(res.blocks[1].props.textAlignment).toBe("center");
		expect(res.blocks[2]).toBe(blocks[1]);
	});

	it("a new paragraph added after a red one stays default", () => {
		const blocks = [para("red", { textColor: "red" }), para("z")];
		const res = spliceEdit(blocks, "red", "red\n\nnew", false);
		expect(res.blocks[0]).toBe(blocks[0]);
		expect(res.blocks[1].content).toEqual([t("new")]);
		expect(res.blocks[1].props.textColor).toBe("default");
	});

	it("refuses to spread blocks with different props over a different number of blocks", () => {
		const blocks = [
			para("aa", { textColor: "red" }),
			para("bb", { textColor: "blue" }),
			para("z"),
		];
		expectRefused(
			() => spliceEdit(blocks, "aa\n\nbb", "x\n\ny\n\nw", false),
			differ("lines 1 and 3"),
			[1, 3],
		);
	});
});

describe("spliceEdit whitespace in code and tables", () => {
	it("keeps inline code strict: double spaces in code are refused", () => {
		const blocks = [para([t("x  y", { code: true }), t(" end")])];
		expect(() => spliceEdit(blocks, "end", "END", false)).toThrow(SpliceError);
	});

	it("compares table cell text loosely", () => {
		const blocks = stored("| h1 | h2 |\n|---|---|\n| a | b |\n");
		const walk = (v: any): void => {
			if (Array.isArray(v)) v.forEach(walk);
			else if (v && typeof v === "object") {
				if (v.type === "text" && v.text === "a") v.text = "a  x ";
				else Object.values(v).forEach(walk);
			}
		};
		walk(blocks);
		const res = spliceEdit(blocks, "| b", "| B", false);
		expect(JSON.stringify(res.blocks[0])).toContain('"B"');
	});

	it("keeps inline code in a table cell strict", () => {
		const blocks = stored("| h1 | h2 |\n|---|---|\n| `a` | b |\n");
		const walk = (v: any): void => {
			if (Array.isArray(v)) v.forEach(walk);
			else if (v && typeof v === "object") {
				if (v.type === "text" && v.text === "a") v.text = "a  x";
				else Object.values(v).forEach(walk);
			}
		};
		walk(blocks);
		expect(() => spliceEdit(blocks, "| b", "| B", false)).toThrow(SpliceError);
	});
});

describe("spliceEdit refusal messages", () => {
	it("names what is lost in one sentence", () => {
		const blocks = [para([t("under", { underline: true }), t(" text")])];
		expect(() => spliceEdit(blocks, "text", "TEXT", false)).toThrow(
			"The edit touches the paragraph block on line 1, which edit_doc cannot rewrite from Markdown: its underline would be lost. Nothing was changed. Edit that block in the web editor, or rewrite the document with write_doc if losing that is acceptable.",
		);
	});

	it('uses "an" before a vowel and says Markdown once', () => {
		const img = {
			id: "img",
			type: "image",
			props: {
				url: "https://example.com/a.png",
				name: "a.png",
				caption: "cap",
				previewWidth: 300,
				textAlignment: "left",
				backgroundColor: "default",
				showPreview: true,
			},
			children: [],
		};
		let message = "";
		try {
			spliceEdit([img, para("x")], "a.png", "b.png", true);
		} catch (err) {
			message = (err as Error).message;
		}
		expect(message).toBe(
			"The edit touches the image block on line 1, which edit_doc cannot rewrite from Markdown: Markdown has no exact form for an image block. Nothing was changed. Edit that block in the web editor, or rewrite the document with write_doc if losing that is acceptable.",
		);
	});
});

// Seeded fuzz (from review): random documents, one marker edited; every
// block outside the edited list run is the same object, and an edit is
// either applied as asked or refused as unsafe.
// Bot review on Paca-AI/paca#770: a block that renders to nothing (an empty
// paragraph) next to a list detached the edit's insertion from the deletion
// it pairs with, so the edited block lost its id and props, skipped the loss
// check, and the empty block moved.
describe("spliceEdit with empty blocks next to lists", () => {
	const empty = () => para([]);
	const chip = {
		type: "teamMention",
		props: { id: "u1", name: "Ann", avatar: "" },
	};

	/** Every block but `touched` is the same object at the same place; the
	 *  touched one keeps its id and props. */
	function expectInPlace(input: any[], output: any[], touched: number) {
		expect(output).toHaveLength(input.length);
		for (let i = 0; i < input.length; i++) {
			if (i === touched) {
				expect(output[i]).not.toBe(input[i]);
				expect(output[i].id).toBe(input[i].id);
				expect(output[i].props).toEqual(input[i].props);
			} else {
				expect(output[i]).toBe(input[i]);
			}
		}
	}

	it("keeps the id, the colour and the empty block's place (empty before a list)", () => {
		const blocks = [
			empty(),
			item("bulletListItem", "alpha", { textColor: "red" }),
		];
		const res = spliceEdit(blocks, "alpha", "ALPHA", false);
		expectInPlace(blocks, res.blocks, 1);
		expect(res.blocks[1].content).toEqual([t("ALPHA")]);
	});

	it("refuses to drop a mention chip (empty before a list)", () => {
		const blocks = [
			empty(),
			{
				...item("bulletListItem", "x"),
				content: [t("hi "), chip, t(" there")],
			},
		];
		expect(() => spliceEdit(blocks, "there", "THERE", false)).toThrow(
			/teamMention would be lost/,
		);
	});

	it("keeps empties after, between and several in a row", () => {
		const blocks = [
			item("bulletListItem", "one", { textColor: "red" }),
			empty(),
			empty(),
			item("bulletListItem", "two", { textColor: "blue" }),
			empty(),
			item("numberedListItem", "three", { backgroundColor: "yellow" }),
			empty(),
		];
		for (const [word, at] of [
			["one", 0],
			["two", 3],
			["three", 5],
		] as const) {
			const res = spliceEdit(blocks, word, word.toUpperCase(), false);
			expectInPlace(blocks, res.blocks, at);
		}
	});

	it("keeps empties between two items an edit replaces", () => {
		const blocks = [
			item("bulletListItem", "x1", { textColor: "red" }),
			empty(),
			item("bulletListItem", "x2", { textColor: "red" }),
		];
		const res = spliceEdit(blocks, "x", "y", true);
		expect(res.blocks).toHaveLength(3);
		expect(res.blocks[1]).toBe(blocks[1]);
		expect(res.blocks.map((b) => b.id)).toEqual(blocks.map((b) => b.id));
		expect(res.blocks[0].props.textColor).toBe("red");
		expect(res.blocks[2].props.textColor).toBe("red");
	});

	it("checks a block whose text moves past an unchanged one for loss", () => {
		const a = { ...para("x"), content: [t("hi "), chip, t(" there")] };
		const blocks = [a, para("beta"), para("z")];
		const md = blocknoteToMarkdown([a]).trimEnd();
		let res: ReturnType<typeof spliceEdit> | undefined;
		try {
			res = spliceEdit(blocks, `${md}\n\nbeta`, `beta\n\n${md}`, false);
		} catch (err) {
			expect((err as SpliceError).kind).toBe("unsafe");
			return;
		}
		// Applied only if the chip survived (its block kept as stored).
		expect(JSON.stringify(res.blocks)).toContain("teamMention");
	});
});

// Bot review on Paca-AI/paca#770: a refusal named the wrong line for a
// numbered item, whose own render ("1. beta") is not what the list shows.
describe("spliceEdit refusal lines inside lists", () => {
	function refusedLines(blocks: any[], oldString: string, newString: string) {
		try {
			spliceEdit(blocks, oldString, newString, false);
		} catch (err) {
			expect((err as SpliceError).kind).toBe("unsafe");
			return (err as SpliceError).lines;
		}
		return expect.unreachable("the edit should be refused");
	}

	it.each([
		["numberedListItem", [3, 5]],
		["bulletListItem", [3, 5]],
		["checkListItem", [3, 4]],
	])("names each %s's own line", (type, want) => {
		const blocks = [
			para("intro"),
			item(type, "alpha", { textColor: "red" }),
			item(type, "beta", { textColor: "blue" }),
			para("z"),
		];
		const md = blocknoteToMarkdown(blocks);
		const span = md.slice(md.indexOf("alpha"), md.indexOf("beta") + 4);
		expect(refusedLines(blocks, span, "alpha beta")).toEqual(want);
	});

	it("names the line of an item after a multi-line item", () => {
		const blocks = [
			item("numberedListItem", "one\ntwo"),
			item("numberedListItem", "x", { textColor: "red" }),
			para("z"),
		];
		// "1. one\\\n   two\n\n2. x": the red item starts on line 4.
		expect(blocknoteToMarkdown(blocks).split("\n")[3]).toMatch(/x$/);
		expect(refusedLines(blocks, "x", "y\n\nnew paragraph")).toEqual([4]);
	});
});

// Independent review of 161cfaeb: nested children paired by index, a prop
// Markdown expresses (start) not following the edit, colours lost on moved
// or re-nested blocks, and an empty block moved by a merge.
describe("spliceEdit nested children, moves and Markdown props (fourth review)", () => {
	const B = "bulletListItem";
	const kids = () => [
		item(B, "a", {}, [
			item(B, "k1", { textColor: "red" }),
			item(B, "k2", { textColor: "blue" }),
		]),
	];
	const own = (b: any) => [
		b.id,
		b.type,
		b.props.textColor,
		b.content.map((c: any) => c.text).join(""),
	];

	it("deleting the first nested child keeps the second as stored", () => {
		const blocks = kids();
		const res = spliceEdit(blocks, "  * k1\n\n", "", false);
		expect(res.blocks[0].children).toHaveLength(1);
		expect(res.blocks[0].children[0]).toBe(blocks[0].children[1]);
	});

	it("inserting a nested child gives it a new id and moves no props", () => {
		const blocks = kids();
		const res = spliceEdit(blocks, "  * k1", "  * new\n\n  * k1", false);
		const [n, k1, k2] = res.blocks[0].children;
		expect(n.id).not.toBe(blocks[0].children[0].id);
		expect(n.id).not.toBe(blocks[0].children[1].id);
		expect(n.props.textColor).toBe("default");
		expect(k1).toBe(blocks[0].children[0]);
		expect(k2).toBe(blocks[0].children[1]);
	});

	it("deleting a grandchild keeps its sibling as stored", () => {
		const blocks = [
			item(B, "a", {}, [
				item(B, "k1", {}, [
					item(B, "g1", { textColor: "red" }),
					item(B, "g2", { textColor: "blue" }),
				]),
			]),
		];
		const res = spliceEdit(blocks, "    * g1\n\n", "", false);
		const grand = res.blocks[0].children[0].children;
		expect(grand).toHaveLength(1);
		expect(grand[0]).toBe(blocks[0].children[0].children[1]);
	});

	it("refuses to retype a coloured nested child", () => {
		const blocks = [
			item(B, "a", {}, [
				item(B, "k1", { textColor: "red" }),
				item("numberedListItem", "k2", { textColor: "blue" }),
			]),
		];
		expect(() => spliceEdit(blocks, "  * k1", "  1. k1", false)).toThrow(
			SpliceError,
		);
	});

	it.each([
		[
			"3. a",
			"1. a",
			[
				item("numberedListItem", "a", { start: 3 }),
				item("numberedListItem", "b"),
			],
		],
		["3. a", "1. a", [para("x"), item("numberedListItem", "a", { start: 3 })]],
		[
			"3.",
			"1.",
			[
				item("numberedListItem", "a", { start: 3 }),
				item("numberedListItem", "b"),
			],
		],
	])("a list start set in Markdown follows the edit (%s to %s)", (o, n, blocks) => {
		const before = blocknoteToMarkdown(blocks);
		const res = spliceEdit(blocks, o, n, false);
		// What the edited Markdown says once parsed (numbering included).
		expect(blocknoteToMarkdown(res.blocks)).toBe(
			blocknoteToMarkdown(markdownToBlocknote(before.replace(o, n))),
		);
		const num = res.blocks.find((b) => b.type === "numberedListItem");
		expect(num.props.start ?? 1).toBe(1);
	});

	it("an edit Markdown cannot show is refused, not reported as done", () => {
		const blocks = [para("a"), para("b")];
		expect(() => spliceEdit(blocks, "a", "a ", false)).toThrow(SpliceError);
	});

	it("swapping two coloured paragraphs moves the stored blocks", () => {
		const blocks = [
			para("a", { textColor: "red" }),
			para("b", { textColor: "blue" }),
		];
		const res = spliceEdit(blocks, "a\n\nb", "b\n\na", false);
		expect(res.blocks).toEqual([blocks[1], blocks[0]]);
		expect(res.blocks[0]).toBe(blocks[1]);
		expect(res.blocks[1]).toBe(blocks[0]);
	});

	it("swapping two coloured list items moves the stored blocks", () => {
		const blocks = [
			item(B, "x", { textColor: "red" }),
			item(B, "y", { textColor: "blue" }),
		];
		const res = spliceEdit(blocks, "x\n\n* y", "y\n\n* x", false);
		expect(res.blocks[0]).toBe(blocks[1]);
		expect(res.blocks[1]).toBe(blocks[0]);
	});

	it("indenting a coloured item keeps its id and colour", () => {
		const blocks = [
			item(B, "a", { textColor: "red" }),
			item(B, "b", { textColor: "red" }),
		];
		const res = spliceEdit(blocks, "* b", "  * b", false);
		expect(res.blocks).toHaveLength(1);
		expect(own(res.blocks[0])).toEqual(own(blocks[0]));
		expect(res.blocks[0].children).toHaveLength(1);
		expect(res.blocks[0].children[0]).toEqual({ ...blocks[1], children: [] });
	});

	// Rule: in a merge or split (old and new blocks not paired one to one),
	// empty blocks before the first old block stay before the new blocks,
	// empty blocks between old blocks follow the first new block (they
	// followed the first old one), and those after stay after.
	it("an empty block inside a merge or split follows the first new block", () => {
		const e = para([]);
		const blocks = [item(B, "a"), e, item(B, "b")];
		const res = spliceEdit(blocks, "a\n\n- b", "A\n\nmid\n\n- B", false);
		const texts = res.blocks.map((b) =>
			b.content.map((c: any) => c.text).join(""),
		);
		expect(texts).toEqual(["A", "", "mid", "B"]);
		expect(res.blocks[1]).toBe(e);
	});

	it("an empty block keeps its place between items paired one to one", () => {
		const e = para([]);
		const blocks = [item(B, "a", { textColor: "red" }), e, item(B, "b")];
		const res = spliceEdit(blocks, "a\n\n- b", "A\n\n- B", false);
		expect(res.blocks[1]).toBe(e);
		expect(res.blocks.map((b) => b.id)).toEqual(blocks.map((b) => b.id));
	});
});

// The final audit, tested directly on the wrong results the reviews found
// (as if the alignment had produced them), so it holds on its own.
describe("spliceEdit edits whose Markdown reads differently in the whole document", () => {
	it.each([
		[
			"removes a closing fence",
			"```ts\ncode a\n```\n\npara b\n\npara c",
			"a\n```\n\npara b",
			"a b",
		],
		["opens a fence", "para a\n\npara b", "para a", "```\npara a"],
	])("refuses an edit that %s", (_, md, o, n) => {
		const blocks = stored(md);
		let err: unknown;
		try {
			spliceEdit(blocks, o, n, false);
		} catch (e) {
			err = e;
		}
		expect(err).toBeInstanceOf(SpliceError);
		expect((err as SpliceError).kind).toBe("unsafe");
		expect((err as SpliceError).message).toMatch(/blocks after it/);
	});
});

describe("auditEdit", () => {
	const B = "bulletListItem";
	const withId = (b: any, id: string) => ({ ...b, id });

	it("refuses a swap that lost a moved block's colour", () => {
		const a = para("a", { textColor: "red" });
		const b = para("b", { textColor: "blue" });
		const bad = [{ ...para("b"), id: "fresh-1" }, a];
		expect(auditEdit([a, b], bad)?.block).toBe(b);
		expect(auditEdit([a, b], [b, a])).toBeNull();
	});

	it("refuses ids shifted onto a sibling's text (nested insert)", () => {
		const k1 = item(B, "k1", { textColor: "red" });
		const k2 = item(B, "k2", { textColor: "blue" });
		const a = item(B, "a", {}, [k1, k2]);
		const bad = [
			{
				...a,
				children: [
					withId(item(B, "new", { textColor: "red" }), k1.id),
					withId(item(B, "k1", { textColor: "blue" }), k2.id),
					withId(item(B, "k2"), "fresh-2"),
				],
			},
		];
		expect(auditEdit([a], bad)).not.toBeNull();
		const good = [
			{ ...a, children: [withId(item(B, "new"), "fresh-3"), k1, k2] },
		];
		expect(auditEdit([a], good)).toBeNull();
	});

	it("refuses ids shifted onto a sibling's text without colours", () => {
		const k1 = item(B, "k1");
		const k2 = item(B, "k2");
		const a = item(B, "a", {}, [k1, k2]);
		const bad = [
			{
				...a,
				children: [
					withId(item(B, "new"), k1.id),
					withId(item(B, "k1"), k2.id),
					withId(item(B, "k2"), "fresh-4"),
				],
			},
		];
		expect(auditEdit([a], bad)).not.toBeNull();
	});

	it("refuses a nested child that kept its id but lost its colour", () => {
		const k1 = item(B, "k1", { textColor: "red" });
		const a = item(B, "a", {}, [k1]);
		const bad = [{ ...a, children: [withId(item(B, "K1"), k1.id)] }];
		expect(auditEdit([a], bad)?.why).toMatch(/textColor/);
	});

	it("refuses an indented block that lost its colour", () => {
		const a = item(B, "a", { textColor: "red" });
		const b = item(B, "b", { textColor: "red" });
		const bad = [{ ...a, children: [withId(item(B, "b"), "fresh-5")] }];
		expect(auditEdit([a, b], bad)?.block).toBe(b);
	});

	it("refuses a duplicate id", () => {
		const a = para("a");
		expect(auditEdit([a], [a, withId(para("b"), a.id)])?.why).toMatch(/twice/);
	});

	it("allows deleting a coloured block outright, and editing one in place", () => {
		const a = para("a", { textColor: "red" });
		const z = para("z");
		expect(auditEdit([a, z], [z])).toBeNull();
		expect(auditEdit([a], [{ ...a, content: [t("A")] }])).toBeNull();
	});

	it("refuses losing a coloured block while new blocks appear", () => {
		const a = para("a", { textColor: "red" });
		expect(auditEdit([a], [withId(para("x"), "fresh-6")])?.block).toBe(a);
	});

	it("refuses a mention chip lost from a block that kept its id", () => {
		const chip = {
			type: "teamMention",
			props: { id: "u1", name: "Ann", avatar: "" },
		};
		const a = { ...para("x"), content: [t("hi "), chip] };
		expect(auditEdit([a], [{ ...a, content: [t("hi @Ann")] }])?.why).toMatch(
			/teamMention/,
		);
	});
});

describe("spliceEdit fuzz", () => {
	it("keeps every untouched block in place and never loses what Markdown cannot carry", () => {
		let seed = 1;
		const rnd = (k: number) => {
			seed = (seed * 1103515245 + 12345) % 2147483648;
			return seed % k;
		};
		let n = 0;
		const chip = () => ({
			type: "teamMention",
			props: { id: `u${++n}`, name: "Ann", avatar: "" },
		});
		const colour = () =>
			[{}, { textColor: "red" }, { backgroundColor: "blue" }][rnd(3)];
		const mk = (marker: string): any => {
			const kinds = [
				() => para(`para ${marker}`),
				() => para([]),
				() => para([]),
				() => ({
					id: `b${++n}`,
					type: "heading",
					props: { ...P, level: 1 + rnd(3), isToggleable: false, ...colour() },
					content: [t(`head ${marker}`)],
					children: [],
				}),
				() => item("bulletListItem", `bul ${marker}`, colour()),
				() => item("numberedListItem", `num ${marker}`, colour()),
				() =>
					item("checkListItem", `chk ${marker}`, {
						checked: rnd(2) === 1,
						...colour(),
					}),
				() => ({
					id: `b${++n}`,
					type: "codeBlock",
					props: { language: "ts" },
					content: [t(`code ${marker}\nline2`)],
					children: [],
				}),
				() => ({
					id: `b${++n}`,
					type: "quote",
					props: { textColor: "default", backgroundColor: "default" },
					content: [t(`quote ${marker}`)],
					children: [],
				}),
				() => para(`ws ${marker} `),
				() => para(`two  spaces ${marker}`),
				() => para(` lead\t${marker}`),
				() => para(`soft ${marker}\nbreak`),
				() => para(`red ${marker}`, { textColor: "red" }),
				() => para([t("hi "), chip(), t(` chip ${marker}`)]),
				() => ({
					...item("bulletListItem", "x", colour()),
					content: [chip(), t(` lchip ${marker}`)],
				}),
				() => para([t("under", { underline: true }), t(` u ${marker}`)]),
				() => para(`par ${marker}`, {}, [para(`kid ${marker}`)]),
				() =>
					item("bulletListItem", `bp ${marker}`, {}, [
						item("bulletListItem", `bk ${marker}`),
					]),
			];
			return kinds[rnd(kinds.length)]();
		};
		// What Markdown cannot carry: a refusal is allowed only for these.
		const special = (b: any) =>
			b.children.length > 0 ||
			JSON.stringify(b.content).includes("teamMention") ||
			JSON.stringify(b.content).includes("underline");
		const carried = (b: any) => {
			const s = JSON.stringify(b.content ?? []);
			return {
				chips: (s.match(/teamMention/g) ?? []).length,
				underline: (s.match(/underline/g) ?? []).length,
			};
		};
		let applied = 0;
		let refused = 0;
		for (let iter = 0; iter < 400; iter++) {
			const len = 3 + rnd(8);
			const blocks = Array.from({ length: len }, (_, i) => mk(`m${i}x`));
			const json = blocks.map((b) => JSON.stringify(b));
			const target = rnd(len);
			const marker = JSON.stringify(blocks[target]).match(/m\d+x/)?.[0];
			if (!marker) continue;
			const before = blocknoteToMarkdown(blocks);
			let res: ReturnType<typeof spliceEdit>;
			try {
				res = spliceEdit(blocks, marker, `${marker}Z`, false);
			} catch (err) {
				expect(["unsafe", "ambiguous"]).toContain((err as SpliceError).kind);
				expect(special(blocks[target])).toBe(true);
				refused++;
				continue;
			}
			applied++;
			expect(blocknoteToMarkdown(res.blocks)).toBe(
				before.replace(marker, `${marker}Z`),
			);
			// Same blocks in the same order: every other block (empty ones
			// included) is the very object read; the edited one keeps its id
			// and props and loses no chip or underline.
			expect(res.blocks).toHaveLength(len);
			for (let i = 0; i < len; i++) {
				if (i === target) continue;
				expect(res.blocks[i]).toBe(blocks[i]);
				expect(JSON.stringify(res.blocks[i])).toBe(json[i]);
			}
			const edited = res.blocks[target];
			expect(edited.id).toBe(blocks[target].id);
			expect(edited.type).toBe(blocks[target].type);
			for (const [k, v] of Object.entries(blocks[target].props)) {
				expect(edited.props[k], `prop ${k}`).toEqual(v);
			}
			expect(carried(edited)).toEqual(carried(blocks[target]));
			if (edited.type !== "codeBlock") {
				expect(JSON.stringify(edited.content)).not.toContain("\\n ");
			}
		}
		expect(applied).toBeGreaterThan(200);
		expect(refused).toBeGreaterThan(10);
	});
});
