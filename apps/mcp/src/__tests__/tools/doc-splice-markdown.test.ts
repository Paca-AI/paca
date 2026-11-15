import { describe, expect, it } from "vitest";
import { SpliceError, spliceMarkdown } from "../../tools/doc-edit.js";
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

const t = (text: string, styles: Record<string, unknown> = {}) => ({
	type: "text",
	text,
	styles,
});

/** The file pull writes for `blocks`. */
const pulled = (blocks: any[]) => blocknoteToMarkdown(blocks);

/** Asserts every block outside `changed` (indices in `input`) is the very
 *  object passed in, in order, and serializes as before. */
function expectKept(
	input: any[],
	json: string[],
	output: any[],
	changed: number[],
) {
	const kept = input.filter((_, i) => !changed.includes(i));
	const found = output.filter((b) => input.includes(b));
	expect(found).toEqual(kept);
	for (let k = 0; k < found.length; k++) expect(found[k]).toBe(kept[k]);
	input.forEach((b, i) => {
		if (!changed.includes(i)) expect(JSON.stringify(b)).toBe(json[i]);
	});
}

function refusal(fn: () => unknown): SpliceError {
	try {
		fn();
	} catch (err) {
		expect(err).toBeInstanceOf(SpliceError);
		return err as SpliceError;
	}
	throw new Error("expected a refusal");
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

/** Every block type Markdown writes, and the formatting it cannot write:
 *  colours, alignment, a mention chip, underline, a toggle heading, a table
 *  with column widths and a coloured cell, nested blocks under a paragraph,
 *  a numbered list starting at 3. */
function everyType(): any[] {
	return stored(
		[
			"# Title",
			"## Sub title",
			"### Third level",
			"Plain paragraph with **bold**, *italic*, `code`, ~~strike~~ and a [link](https://example.com).",
			"Coloured paragraph.",
			"Centred paragraph.",
			"- bullet one\n- bullet two\n  - nested bullet",
			"3. third\n4. fourth",
			"- [ ] open task\n- [x] done task",
			"```ts\nconst x = 1;\n```",
			"> A quote.",
			"| a | b |\n|---|---|\n| 1 | 2 |\n| 3 | 4 |",
			"Chip paragraph.",
			"Under paragraph.",
			"Toggle heading",
			"Parent paragraph",
			"Last paragraph.",
		].join("\n\n"),
		(b) => {
			const find = (text: string) =>
				b.find((x: any) => JSON.stringify(x.content ?? "").includes(text));
			find("Coloured paragraph.").props.textColor = "red";
			find("Centred paragraph.").props.textAlignment = "center";
			find("Chip paragraph.").content = [
				t("Chip "),
				{
					type: "teamMention",
					props: { id: "u1", name: "Ann", avatar: "" },
				},
				t(" paragraph."),
			];
			find("Under paragraph.").content = [
				t("Under", { underline: true }),
				t(" paragraph."),
			];
			const toggle = find("Toggle heading");
			toggle.type = "heading";
			toggle.props = { ...toggle.props, level: 2, isToggleable: true };
			const table = b.find((x: any) => x.type === "table");
			table.content.columnWidths = [180, null];
			table.content.rows[1].cells[0].props.backgroundColor = "blue";
			const parent = find("Parent paragraph");
			parent.children = [
				{
					id: "kid1",
					type: "paragraph",
					props: {
						textColor: "default",
						backgroundColor: "default",
						textAlignment: "left",
					},
					content: [t("Nested under a paragraph")],
					children: [],
				},
			];
		},
	);
}

describe("spliceMarkdown", () => {
	it("identical file returns the same objects and changed 0", () => {
		const blocks = stored(TEN);
		const res = spliceMarkdown(blocks, pulled(blocks));
		expect(res.blocks).toBe(blocks);
		expect(res.changed).toBe(0);
		expect(res.lines).toEqual([]);
	});

	it("pull output of a fixture with every block type is identical to the render", () => {
		const blocks = everyType();
		const md = pulled(blocks);
		// Untouched blocks must render, alone and in place, exactly as the
		// whole document does: that is what lets an edit leave them alone.
		const res = spliceMarkdown(blocks, md);
		expect(res.blocks).toBe(blocks);
		expect(res.changed).toBe(0);
		// The same through JSON again (what the server returns next time).
		const again = JSON.parse(JSON.stringify(blocks));
		expect(pulled(again)).toBe(md);
		// One edited paragraph in it changes that block only.
		const json = blocks.map((b) => JSON.stringify(b));
		const edited = md.replace("Last paragraph.", "Last paragraph, edited.");
		const one = spliceMarkdown(blocks, edited);
		expect(one.changed).toBe(1);
		const k = blocks.findIndex((b) =>
			JSON.stringify(b.content).includes("Last paragraph."),
		);
		expectKept(blocks, json, one.blocks, [k]);
		expect(one.blocks[k].id).toBe(blocks[k].id);
		expect(pulled(one.blocks)).toBe(edited);
	});

	it("one edited paragraph changes one block", () => {
		const blocks = stored(TEN);
		const json = blocks.map((b) => JSON.stringify(b));
		const md = pulled(blocks).replace("Intro paragraph", "Opening paragraph");
		const res = spliceMarkdown(blocks, md);
		expect(res.changed).toBe(1);
		expectKept(blocks, json, res.blocks, [1]);
		expect(res.blocks[1].id).toBe(blocks[1].id);
		expect(res.blocks[1].content[0].text).toBe("Opening paragraph with ");
		expect(res.lines).toEqual([3]);
		expect(pulled(res.blocks)).toBe(md);
	});

	it("edits in three separate places change three blocks, all others identical", () => {
		const blocks = stored(TEN);
		const json = blocks.map((b) => JSON.stringify(b));
		const md = pulled(blocks)
			.replace("# Release plan", "# Release plan v2")
			.replace("Careful with prod.", "Careful with production.")
			.replace("watch the dashboard", "watch the dashboards");
		const res = spliceMarkdown(blocks, md);
		expect(res.changed).toBe(3);
		const at = (s: string) =>
			blocks.findIndex((b) => JSON.stringify(b.content).includes(s));
		const changed = [0, at("Careful with prod."), at("watch the dashboard")];
		expectKept(blocks, json, res.blocks, changed);
		for (const k of changed) expect(res.blocks[k].id).toBe(blocks[k].id);
		expect(res.lines).toHaveLength(3);
		expect(pulled(res.blocks)).toBe(md);
	});

	it("inserted and removed plain blocks", () => {
		const blocks = stored("one\n\ntwo\n\nthree\n\nfour\n\nfive");
		const json = blocks.map((b) => JSON.stringify(b));
		const md = "one\n\nnew one\n\ntwo\n\nthree\n\nfive\n";
		expect(pulled(blocks)).toBe("one\n\ntwo\n\nthree\n\nfour\n\nfive\n");
		const res = spliceMarkdown(blocks, md);
		// One block added, one removed.
		expect(res.changed).toBe(2);
		expect(res.blocks).toHaveLength(5);
		expectKept(blocks, json, res.blocks, [3]);
		expect(res.blocks.includes(blocks[3])).toBe(false);
		expect(pulled(res.blocks)).toBe(md);
	});

	it("edit next to a coloured block keeps it as stored", () => {
		const blocks = stored("before\n\nred one\n\nafter", (b) => {
			b[1].props.textColor = "red";
		});
		const json = blocks.map((b) => JSON.stringify(b));
		const md = "before, edited\n\nred one\n\nafter, edited\n";
		const res = spliceMarkdown(blocks, md);
		expect(res.changed).toBe(2);
		expectKept(blocks, json, res.blocks, [0, 2]);
		expect(res.blocks[1]).toBe(blocks[1]);
		// And a block inserted right after it, and right before it.
		const res2 = spliceMarkdown(
			blocks,
			"before\n\nnew a\n\nred one\n\nnew b\n\nafter\n",
		);
		expect(res2.changed).toBe(2);
		expect(res2.blocks[2]).toBe(blocks[1]);
		expect(res2.blocks[0]).toBe(blocks[0]);
		expect(res2.blocks[4]).toBe(blocks[2]);
	});

	it("reordering coloured blocks is refused and names the lines", () => {
		const blocks = stored("intro\n\nred one\n\nblue two\n\nend", (b) => {
			b[1].props.textColor = "red";
			b[2].props.backgroundColor = "blue";
		});
		const md = pulled(blocks);
		expect(md).toBe("intro\n\nred one\n\nblue two\n\nend\n");
		const err = refusal(() =>
			spliceMarkdown(blocks, "intro\n\nblue two\n\nred one\n\nend\n"),
		);
		expect(err.kind).toBe("unsafe");
		expect(err.lines.length).toBeGreaterThan(0);
		for (const l of err.lines) expect([3, 5]).toContain(l);
		expect(err.message).toMatch(/line [35]/);
	});

	it("names the lines of the edited file when lines were added above", () => {
		const blocks = stored("intro\n\nred one\n\nblue two\n\nend", (b) => {
			b[1].props.textColor = "red";
			b[2].props.backgroundColor = "blue";
		});
		// Two new paragraphs at the top move everything down four lines.
		const err = refusal(() =>
			spliceMarkdown(
				blocks,
				"top a\n\ntop b\n\nintro\n\nblue two\n\nred one\n\nend\n",
			),
		);
		expect(err.kind, err.message).toBe("unsafe");
		for (const l of err.lines) expect([7, 9], err.message).toContain(l);
		expect(err.message).toMatch(/in the edited file/);
	});

	it("refuses moving a block with a mention chip across the file (a fuzz finding)", () => {
		// The chip paragraph is deleted at the top and its text written again
		// further down, where another block's id would take it as plain
		// text: each part alone passes, the document as a whole does not.
		const plain = {
			textColor: "default",
			backgroundColor: "default",
			textAlignment: "left",
		};
		const blocks = [
			{
				id: "c0",
				type: "paragraph",
				props: { ...plain },
				content: [
					t("hi "),
					{
						type: "teamMention",
						props: { id: "u9", name: "Ann", avatar: "" },
					},
					t(" chip"),
				],
				children: [],
			},
			...["para", "dup line", "dup line"].map((text, k) => ({
				id: `b${k}`,
				type: "paragraph",
				props: { ...plain, ...(k === 0 ? { backgroundColor: "blue" } : {}) },
				content: [t(text)],
				children: [],
			})),
			{
				id: "b9",
				type: "bulletListItem",
				props: { ...plain },
				content: [t("dup line")],
				children: [],
			},
		];
		const md = pulled(blocks);
		expect(md).toBe(
			"hi @Ann (id: u9) chip\n\npara\n\ndup line\n\ndup line\n\n* dup line\n",
		);
		const err = refusal(() =>
			spliceMarkdown(
				blocks,
				"para\n\nhi @Ann (id: u9) chip\n\ndup line\n\n## dup line\n\n* dup line\n",
			),
		);
		expect(err.kind).toBe("unsafe");
		expect(err.message).toMatch(/teamMention/);
		// The chip paragraph was on line 1, where the file now has "para".
		expect(err.lines).toEqual([1]);
		expect(err.message).toMatch(/line 1\b/);
	});

	describe("whole-document audit: changes in several places that each pass alone", () => {
		// The document is audited as a whole after its parts, so some
		// changes in separate places are refused together although each
		// applies alone (two pushes would take them). Recorded here so the
		// cost of that rule is visible.
		const DOC =
			"Intro text here.\n\nRed warning about deploys.\n\nMiddle paragraph one.\n\nMiddle paragraph two.\n\nClosing words.";
		const doc = () =>
			stored(DOC, (b) => {
				b[1].props.textColor = "red";
			});
		const md = () => pulled(doc());
		const RED = "Red warning about deploys.";

		it("deleting a coloured paragraph and adding one elsewhere", () => {
			const del = md().replace(`${RED}\n\n`, "");
			const add = md().replace(
				"Closing words.",
				"Closing words.\n\nA new one.",
			);
			expect(spliceMarkdown(doc(), del).changed).toBe(1);
			expect(spliceMarkdown(doc(), add).changed).toBe(1);
			const both = del.replace(
				"Closing words.",
				"Closing words.\n\nA new one.",
			);
			const err = refusal(() => spliceMarkdown(doc(), both));
			expect(err.message).toContain(
				"it would be replaced by new blocks without its textColor",
			);
		});

		it("rewording a coloured paragraph and editing another one", () => {
			const reword = md().replace(
				RED,
				"Careful: releases on Friday need sign-off.",
			);
			expect(spliceMarkdown(doc(), reword).changed).toBe(1);
			const both = reword.replace("Closing words.", "Closing words!");
			const err = refusal(() => spliceMarkdown(doc(), both));
			expect(err.message).toContain(
				"its id would end up on text that came from another block",
			);
			// A smaller rewording next to another edit applies.
			const small = md()
				.replace(RED, "Red warning about all deploys.")
				.replace("Middle paragraph two.", "Middle paragraph 2.");
			expect(spliceMarkdown(doc(), small).changed).toBe(2);
		});

		it("editing a coloured paragraph and adding one that reads like it", () => {
			const edit = md().replace(RED, "Red warning about deploys, updated.");
			expect(spliceMarkdown(doc(), edit).changed).toBe(1);
			const both = edit.replace(
				"Closing words.",
				"Closing words.\n\nRed warning about deploys again.",
			);
			const err = refusal(() => spliceMarkdown(doc(), both));
			expect(err.message).toContain("cannot tell which new block it is");
		});
	});

	it("a reorder of plain blocks is a change", () => {
		const blocks = stored("alpha one\n\nbeta two\n\nend");
		const json = blocks.map((b) => JSON.stringify(b));
		const swapped = "beta two\n\nalpha one\n\nend\n";
		const res = spliceMarkdown(blocks, swapped);
		expect(res.blocks).not.toBe(blocks);
		expect(res.changed).toBe(1);
		expect(pulled(res.blocks)).toBe(swapped);
		// The moved blocks are the objects read, in their new order.
		expect(res.blocks).toHaveLength(3);
		expect(res.blocks[0]).toBe(blocks[1]);
		expect(res.blocks[1]).toBe(blocks[0]);
		expect(res.blocks[2]).toBe(blocks[2]);
		blocks.forEach((b, i) => expect(JSON.stringify(b)).toBe(json[i]));
		// List items swapped, and paragraphs swapped next to a list.
		const list = stored("alpha one\n\nbeta two\n\n* li one\n\n* li two\n\nend");
		const both = pulled(list)
			.replace("alpha one\n\nbeta two", "beta two\n\nalpha one")
			.replace("* li one\n\n* li two", "* li two\n\n* li one");
		const res2 = spliceMarkdown(list, both);
		expect(res2.changed).toBeGreaterThan(0);
		expect(pulled(res2.blocks)).toBe(both);
	});

	it("formatted table row swap is refused", () => {
		const blocks = stored(
			"| name | state |\n|---|---|\n| alpha | done |\n| beta | late |",
			(b) => {
				b[0].content.columnWidths = [200, null];
			},
		);
		const md = pulled(blocks);
		const lines = md.split("\n");
		const swapped = [lines[0], lines[1], lines[3], lines[2], ...lines.slice(4)];
		const err = refusal(() => spliceMarkdown(blocks, swapped.join("\n")));
		expect(err.kind).toBe("unsafe");
		expect(err.message).toMatch(/row/);
	});

	it("file with CRLF line endings", () => {
		const blocks = stored(TEN);
		const json = blocks.map((b) => JSON.stringify(b));
		const crlf = pulled(blocks).replace(/\n/g, "\r\n");
		const same = spliceMarkdown(blocks, crlf);
		expect(same.blocks).toBe(blocks);
		expect(same.changed).toBe(0);
		const res = spliceMarkdown(blocks, crlf.replace("Intro", "Opening"));
		expect(res.changed).toBe(1);
		expectKept(blocks, json, res.blocks, [1]);
	});

	it("file with a byte order mark", () => {
		const blocks = stored(TEN);
		const res = spliceMarkdown(blocks, `﻿${pulled(blocks)}`);
		expect(res.blocks).toBe(blocks);
		expect(res.changed).toBe(0);
	});

	it("file without trailing newline", () => {
		const blocks = stored(TEN);
		const json = blocks.map((b) => JSON.stringify(b));
		const bare = pulled(blocks).replace(/\n+$/, "");
		const same = spliceMarkdown(blocks, bare);
		expect(same.blocks).toBe(blocks);
		expect(same.changed).toBe(0);
		// Extra newlines at the end are not an edit either.
		expect(spliceMarkdown(blocks, `${bare}\n\n\n`).blocks).toBe(blocks);
		const res = spliceMarkdown(blocks, bare.replace("dashboard.", "graphs."));
		expect(res.changed).toBe(1);
		expectKept(blocks, json, res.blocks, [blocks.length - 1]);
	});

	it("edits the first and the last block", () => {
		const blocks = stored(TEN);
		const json = blocks.map((b) => JSON.stringify(b));
		const md = pulled(blocks)
			.replace("# Release plan", "# Plan")
			.replace("the dashboard.", "the logs.");
		const res = spliceMarkdown(blocks, md);
		expect(res.changed).toBe(2);
		expectKept(blocks, json, res.blocks, [0, blocks.length - 1]);
		expect(pulled(res.blocks)).toBe(md);
	});

	it("merges two paragraphs by deleting the blank line between them", () => {
		const blocks = stored("one\n\ntwo\n\nthree");
		const json = blocks.map((b) => JSON.stringify(b));
		const md = "one two\n\nthree\n";
		const res = spliceMarkdown(blocks, md);
		expect(res.blocks).toHaveLength(2);
		expect(res.blocks[1]).toBe(blocks[2]);
		expect(JSON.stringify(res.blocks[1])).toBe(json[2]);
		expect(pulled(res.blocks)).toBe(md);
	});

	it("splits a paragraph by inserting a blank line", () => {
		const blocks = stored("one\n\nfirst half second half\n\nthree");
		const md = pulled(blocks);
		expect(md).toBe("one\n\nfirst half second half\n\nthree\n");
		const next = "one\n\nfirst half\n\nsecond half\n\nthree\n";
		const res = spliceMarkdown(blocks, next);
		expect(res.blocks).toHaveLength(4);
		expect(res.blocks[0]).toBe(blocks[0]);
		expect(res.blocks[3]).toBe(blocks[2]);
		expect(pulled(res.blocks)).toBe(next);
	});

	it("fills an empty document and empties a document", () => {
		const filled = spliceMarkdown([], "# New\n\nText.\n");
		expect(filled.changed).toBe(2);
		expect(pulled(filled.blocks)).toBe("# New\n\nText.\n");
		const blocks = stored("one\n\ntwo");
		const emptied = spliceMarkdown(blocks, "");
		expect(emptied.blocks).toEqual([]);
		expect(emptied.changed).toBe(2);
		expect(emptied.lines).toEqual([1]);
	});

	it("refuses Markdown that Paca would write differently, naming the line", () => {
		const blocks = stored("intro\n\n* a\n\n* b");
		expect(pulled(blocks)).toBe("intro\n\n* a\n\n* b\n");
		const err = refusal(() =>
			spliceMarkdown(blocks, "intro\n\n* a\n\n* b\n\n- c\n"),
		);
		expect(err.kind).toBe("unsafe");
		expect(err.message).toMatch(/line 7/);
		expect(err.message).toContain("* c");
	});

	it("refuses an edit that only changes what Markdown does not show", () => {
		const blocks = stored(TEN);
		const md = pulled(blocks).replace(
			"Careful with prod.",
			"Careful with prod. ",
		);
		const err = refusal(() => spliceMarkdown(blocks, md));
		expect(["no_change", "unsafe"]).toContain(err.kind);
	});
});

// ── Fuzz ──────────────────────────────────────────────────────────────────────
//
// The generator is copied from the edit_doc fuzz ("spliceEdit fuzz by edit
// kind" in doc-edit.test.ts, which stays as it is). The checker below is
// written here and calls neither spliceMarkdown nor spliceEdit.

/** What of a block Markdown cannot write (copied from doc-edit.test.ts). */
function lostByMarkdown(b: any): string {
	const neutral: Record<string, unknown> = {
		textColor: "default",
		backgroundColor: "default",
		textAlignment: "left",
		isToggleable: false,
	};
	const written = new Set(["start", "checked", "level", "language"]);
	const props = Object.entries(b.props ?? {})
		.filter(([k, v]) => !written.has(k) && v !== undefined && neutral[k] !== v)
		.map(([k, v]) => `${k}=${String(v)}`)
		.sort();
	const inline: string[] = [];
	for (const node of Array.isArray(b.content) ? b.content : []) {
		if (node.type === "text") {
			for (const [s, v] of Object.entries(node.styles ?? {})) {
				if (!["bold", "italic", "code", "strike"].includes(s)) {
					inline.push(`${s}=${String(v)}:${node.text}`);
				}
			}
		} else if (node.type !== "link") {
			inline.push(JSON.stringify(node));
		}
	}
	const c = b.content;
	if (c?.type === "tableContent") {
		if ((c.columnWidths ?? []).some((w: unknown) => w != null)) {
			props.push(`widths=${JSON.stringify(c.columnWidths)}`);
		}
		if ((c.headerRows ?? 0) !== 1) props.push(`headerRows=${c.headerRows}`);
		if (c.headerCols) props.push(`headerCols=${c.headerCols}`);
		(c.rows ?? []).forEach((r: any, y: number) => {
			(r.cells ?? []).forEach((cell: any, x: number) => {
				for (const [k, v] of Object.entries(cell?.props ?? {})) {
					const plain = { ...neutral, colspan: 1, rowspan: 1 } as any;
					if (v !== undefined && plain[k] !== v)
						props.push(`${y},${x}:${k}=${v}`);
				}
			});
		});
	}
	const mdTypes = [
		"paragraph",
		"heading",
		"bulletListItem",
		"numberedListItem",
		"checkListItem",
		"codeBlock",
		"quote",
		"table",
	];
	if (b.type && !mdTypes.includes(b.type)) props.push(`type=${b.type}`);
	return JSON.stringify([props, inline.sort()]);
}
const NOTHING_LOST = lostByMarkdown({ props: {}, content: [] });
const everyBlock = (bs: any[]): any[] =>
	bs.flatMap((b) => [b, ...everyBlock(b.children ?? [])]);
const ownText = (b: any) =>
	(Array.isArray(b.content) ? b.content : [])
		.map((n: any) => n.text ?? "")
		.join("");
const MARKER = /m\d+(?:c\d+)*x/g;
const PLAIN = {
	textColor: "default",
	backgroundColor: "default",
	textAlignment: "left",
};

describe("spliceMarkdown fuzz", () => {
	it("keeps untouched blocks, never moves or drops what Markdown cannot carry, and renders to the file", () => {
		let seed = 11;
		const rnd = (k: number) => {
			seed = (seed * 1103515245 + 12345) % 2147483648;
			return Math.floor((seed / 2147483648) * k);
		};
		let n = 0;
		const id = () => `f${++n}`;
		const look = () =>
			[
				{},
				{},
				{},
				{},
				{},
				{ textColor: "red" },
				{ backgroundColor: "blue" },
				{ textAlignment: "center" },
			][rnd(8)];
		const blk = (
			type: string,
			text: string,
			props: Record<string, unknown> = {},
			children: any[] = [],
		) => ({
			id: id(),
			type,
			props: { ...PLAIN, ...props },
			content: text === "" ? [] : [t(text)],
			children,
		});
		const LIST = ["bulletListItem", "numberedListItem", "checkListItem"];
		const listItem = (type: string, text: string, extra = {}) =>
			blk(type, text, {
				...(type === "checkListItem" ? { checked: rnd(2) === 1 } : {}),
				...look(),
				...extra,
			});
		const table = (m: string) => {
			const [tb] = JSON.parse(
				JSON.stringify(
					markdownToBlocknote(
						`| name ${m} | state |\n|---|---|\n| alpha one | done |\n| beta two | late |`,
					),
				),
			);
			tb.id = id();
			if (rnd(3) === 0) tb.content.columnWidths = [120 + rnd(200), null];
			else if (rnd(2) === 0)
				tb.content.rows[1 + rnd(2)].cells[rnd(2)].props.backgroundColor = "red";
			return tb;
		};
		const mk = (p: string): any => {
			const m = `${p}x`;
			const kinds = [
				() => blk("paragraph", `para ${m}`, look()),
				() => blk("paragraph", `para ${m}`),
				() => blk("paragraph", ""),
				() =>
					blk("heading", `head ${m}`, {
						level: 1 + rnd(3),
						isToggleable: false,
						...look(),
					}),
				() => listItem(LIST[rnd(3)], `item ${m}`),
				() => listItem("numberedListItem", `num ${m}`, { start: 3 }),
				() => {
					const kid = listItem(LIST[rnd(3)], `kid ${p}c0x`);
					return {
						...listItem(LIST[rnd(3)], `top ${m}`),
						children: [kid],
					};
				},
				() =>
					blk("paragraph", `par ${m}`, look(), [
						blk("paragraph", `kid ${p}c0x`),
					]),
				() => ({
					...blk("codeBlock", `code ${m}\nline2`),
					props: { language: "ts" },
				}),
				() => ({
					...blk("quote", `quote ${m}`),
					props: { textColor: "default", backgroundColor: "default" },
				}),
				() => ({
					...blk("paragraph", ""),
					content: [
						t("hi "),
						{
							type: "teamMention",
							props: { id: id(), name: "Ann", avatar: "" },
						},
						t(` chip ${m}`),
					],
				}),
				() => ({
					...blk("paragraph", ""),
					content: [t("under", { underline: true }), t(` u ${m}`)],
				}),
				() => blk("paragraph", "dup line", look()),
				() => blk("bulletListItem", "dup line", look()),
				() => table(m),
			];
			return kinds[rnd(kinds.length)]();
		};

		// Line edits on the pulled file. Each returns old lines [lo, hi) and
		// what replaces them, or null.
		type LineEdit = { lo: number; hi: number; repl: string[] };
		const isText = (l: string | undefined) =>
			!!l && l.trim() !== "" && !l.trimStart().startsWith("```");
		const PREFIX =
			/^(\s*)((?:[-*+]|\d+[.)])\s+(?:\[[ xX]\]\s+)?|#{1,6}\s+|>\s+)?/;
		const EDITS: Array<(ls: string[]) => LineEdit | null> = [
			// Reword a line.
			(ls) => {
				const k = rnd(ls.length);
				const m = ls[k].match(MARKER)?.[0];
				return m
					? { lo: k, hi: k + 1, repl: [ls[k].replace(m, `${m}Z`)] }
					: null;
			},
			// Delete a line (and the blank line after it).
			(ls) => {
				const k = rnd(ls.length);
				if (!isText(ls[k])) return null;
				return { lo: k, hi: ls[k + 1] === "" ? k + 2 : k + 1, repl: [] };
			},
			// Add a paragraph before a line.
			(ls) => {
				const k = rnd(ls.length);
				if (ls[k] !== "" || !isText(ls[k + 1])) return null;
				return { lo: k, hi: k + 1, repl: ["", `new para mN${k}x`, ""] };
			},
			// Add a list item after an item.
			(ls) => {
				const k = rnd(ls.length);
				const [, indent, prefix] = ls[k].match(PREFIX) ?? [];
				if (!prefix || /^[#>]/.test(prefix)) return null;
				return {
					lo: k,
					hi: k + 1,
					repl: [
						ls[k],
						"",
						`${indent}${prefix.replace(/\d+/, "9")}fresh mF${k}x`,
					],
				};
			},
			// Swap two neighbouring text lines.
			(ls) => {
				const k = rnd(ls.length - 1);
				if (!isText(ls[k]) || !isText(ls[k + 1]) || ls[k] === ls[k + 1])
					return null;
				return { lo: k, hi: k + 2, repl: [ls[k + 1], ls[k]] };
			},
			// Swap two blocks a blank line apart.
			(ls) => {
				const k = rnd(ls.length - 2);
				if (!isText(ls[k]) || ls[k + 1] !== "" || !isText(ls[k + 2]))
					return null;
				if (ls[k] === ls[k + 2]) return null;
				return { lo: k, hi: k + 3, repl: [ls[k + 2], "", ls[k]] };
			},
			// Change a line's type.
			(ls) => {
				const k = rnd(ls.length);
				if (!isText(ls[k]) || ls[k].startsWith("|")) return null;
				const [whole, indent] = ls[k].match(PREFIX) ?? ["", ""];
				const to = ["", "## ", "* ", "1. ", "* [ ] ", "> "][rnd(6)];
				const next = `${indent}${to}${ls[k].slice(whole.length)}`;
				return next === ls[k] ? null : { lo: k, hi: k + 1, repl: [next] };
			},
			// Join two paragraphs.
			(ls) => {
				const k = rnd(ls.length - 2);
				if (!isText(ls[k]) || ls[k + 1] !== "" || !isText(ls[k + 2]))
					return null;
				return {
					lo: k,
					hi: k + 3,
					repl: [`${ls[k]} ${ls[k + 2].replace(PREFIX, "")}`],
				};
			},
		];

		const squash = (x: string) => x.replace(/\n+/g, "\n");
		let applied = 0;
		let refused = 0;
		let multi = 0;
		const iterations = Number(process.env.FUZZ_ITER) || 500;
		for (let iter = 0; iter < iterations; iter++) {
			const len = 3 + rnd(7);
			const blocks: any[] = Array.from({ length: len }, (_, i) => mk(`m${i}`));
			const json = blocks.map((b) => JSON.stringify(b));
			const md = blocknoteToMarkdown(blocks);
			const ls = md.replace(/\n$/, "").split("\n");

			// One to three edits on lines at least two apart, applied at once.
			const edits: LineEdit[] = [];
			const want = 1 + rnd(3);
			for (let tries = 0; tries < 20 && edits.length < want; tries++) {
				const e = EDITS[rnd(EDITS.length)](ls);
				if (!e) continue;
				if (edits.some((x) => e.lo < x.hi + 2 && x.lo < e.hi + 2)) continue;
				edits.push(e);
			}
			if (edits.length === 0) continue;
			edits.sort((x, y) => x.lo - y.lo);
			const out: string[] = [];
			let pos = 0;
			for (const e of edits) {
				out.push(...ls.slice(pos, e.lo), ...e.repl);
				pos = e.hi;
			}
			out.push(...ls.slice(pos));
			const edited = `${out.join("\n")}\n`;
			if (edited === md) continue;
			// Saved by various editors.
			const file = [
				(x: string) => x,
				(x: string) => x.replace(/\n/g, "\r\n"),
				(x: string) => x.replace(/\n+$/, ""),
				(x: string) => `﻿${x}`,
			][rnd(4)](edited);
			const ctx = `#${iter} ${JSON.stringify(edits)}\n${md}\n---\n${edited}`;

			let res: ReturnType<typeof spliceMarkdown>;
			try {
				res = spliceMarkdown(blocks, file);
			} catch (err) {
				expect(err, ctx).toBeInstanceOf(SpliceError);
				expect((err as SpliceError).lines.length, ctx).toBeGreaterThan(0);
				refused++;
				continue;
			}
			applied++;
			if (edits.length > 1) multi++;
			const where = `${ctx}\nRES ${JSON.stringify(res.blocks)}`;

			// A. It renders to the file (newlines at its end aside).
			const body = edited.replace(/\n+$/, "");
			expect(blocknoteToMarkdown(res.blocks), where).toBe(
				body === "" ? "" : `${body}\n`,
			);

			// B. Blocks the edits do not reach are the very objects read, in
			// order. A block is reached when an edited line falls in its part
			// of the Markdown (blank lines squashed: a list's spacing depends
			// on all of it), or in a neighbour's; then the rest of a list it
			// is in, empty blocks next to it, and numbered items after it.
			const flat = squash(md);
			const ends = blocks.map((_, i) => {
				const upto = squash(
					blocknoteToMarkdown(blocks.slice(0, i + 1)),
				).trimEnd();
				expect(flat.startsWith(upto), `${ctx}\nprefix ${i}`).toBe(true);
				return upto.length;
			});
			const offs: number[] = [0];
			for (const l of ls) offs.push(offs[offs.length - 1] + l.length + 1);
			const touched = blocks.map((_, i) => {
				const a = i === 0 ? 0 : ends[i - 1];
				return edits.some((e) => {
					const s = squash(md.slice(0, offs[e.lo])).length;
					const z = squash(md.slice(0, offs[e.hi])).length;
					return s < ends[i] && Math.max(z, s + 1) > a;
				});
			});
			const reached = touched.map(
				(x, i) => x || !!touched[i - 1] || !!touched[i + 1],
			);
			const LISTS = [...LIST, "toggleListItem"];
			const shows = (b: any) =>
				blocknoteToMarkdown([{ ...b, children: [] }]).trim() !== "";
			for (let grew = true; grew; ) {
				grew = false;
				for (let i = 0; i < len; i++) {
					if (reached[i]) continue;
					const near = [i - 1, i + 1].filter((q) => reached[q]);
					if (
						near.length > 0 &&
						(!shows(blocks[i]) ||
							(LISTS.includes(blocks[i].type) &&
								near.some((q) => LISTS.includes(blocks[q].type))))
					) {
						reached[i] = true;
						grew = true;
					}
				}
			}
			for (let i = 1; i < len; i++) {
				if (reached[i - 1] && blocks[i].type === "numberedListItem")
					reached[i] = true;
			}
			let at = 0;
			for (let i = 0; i < len; i++) {
				if (reached[i]) continue;
				const found = res.blocks.indexOf(blocks[i], at);
				expect(
					found,
					`${where}\nblock ${i} missing or out of order`,
				).toBeGreaterThanOrEqual(0);
				expect(JSON.stringify(res.blocks[found]), where).toBe(json[i]);
				at = found + 1;
			}

			// C. Every block read that holds what Markdown cannot write is
			// returned as read, or its id is on a block of its type with the
			// same such data under the same parent, data blocks keeping their
			// order; or it is gone and so is its text (it was deleted, not
			// merged into a block without its data). No id twice.
			const outs = everyBlock(res.blocks);
			const ids = outs.map((b) => b.id);
			expect(new Set(ids).size, where).toBe(ids.length);
			const parentOf = new Map<any, string | null>();
			const walk = (bs: any[], parent: string | null) => {
				for (const b of bs) {
					parentOf.set(b, parent);
					walk(b.children ?? [], b.id);
				}
			};
			walk(blocks, null);
			walk(res.blocks, null);
			let lastAt = -1;
			for (const o of everyBlock(blocks)) {
				const lost = lostByMarkdown(o);
				if (lost === NOTHING_LOST) continue;
				const k = outs.findIndex((b) => b.id === o.id);
				if (k >= 0) {
					const same = outs[k];
					expect(same.type, `${where}\n${o.id} retyped`).toBe(o.type);
					expect(lostByMarkdown(same), `${where}\n${o.id} lost data`).toBe(
						lost,
					);
					expect(parentOf.get(same), `${where}\n${o.id} moved`).toBe(
						parentOf.get(o),
					);
					expect(k, `${where}\n${o.id} out of order`).toBeGreaterThan(lastAt);
					lastAt = k;
					continue;
				}
				// Its unique marker, if it has one, may only be on a block with
				// its data (it was merged with blocks like it).
				const m = ownText(o).match(MARKER)?.[0];
				if (!m) continue;
				const holder = outs.find(
					(b) => ownText(b).includes(m) && lostByMarkdown(b) !== lost,
				);
				expect(
					holder,
					`${where}\n${o.id}'s text moved without its data`,
				).toBeUndefined();
			}
		}
		expect(applied, "applied too rarely").toBeGreaterThan(iterations / 5);
		expect(refused, "never refused").toBeGreaterThan(10);
		expect(multi, "few edits with several changes").toBeGreaterThan(
			iterations / 10,
		);
	}, 180_000);
});
