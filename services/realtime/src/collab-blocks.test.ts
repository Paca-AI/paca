import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { BlockNoteEditor } from "@blocknote/core";
import { blocksToYDoc, yDocToBlocks } from "@blocknote/core/yjs";
import * as Y from "yjs";
import { COLLAB_FRAGMENT } from "./collab.ts";
import {
	CUSTOM_BLOCKS,
	CUSTOM_INLINES,
	mergeExternalChange,
} from "./collab-blocks.ts";

const ed = BlockNoteEditor.create();
const para = (
	id: string | undefined,
	text: string,
	children: unknown[] = [],
) => ({
	...(id ? { id } : {}),
	type: "paragraph",
	content: text,
	children,
});

function room(blocks: unknown[]): Y.Doc {
	return blocksToYDoc(ed, blocks as never, COLLAB_FRAGMENT);
}

// texts lists every block's plain text, depth first.
function texts(doc: Y.Doc): string[] {
	const out: string[] = [];
	const walk = (bs: { content?: unknown; children: unknown[] }[]) => {
		for (const b of bs) {
			const c = b.content as { text: string }[] | undefined;
			out.push(c?.map((x) => x.text).join("") ?? "");
			walk(b.children as never);
		}
	};
	walk(yDocToBlocks(ed, doc, COLLAB_FRAGMENT) as never);
	return out;
}

// typeInto simulates an editor typing at the start of a block.
function typeInto(doc: Y.Doc, id: string, text: string) {
	const find = (n: Y.XmlFragment | Y.XmlElement): Y.XmlText | undefined => {
		for (const c of n.toArray()) {
			if (c instanceof Y.XmlElement && c.getAttribute("id") === id) {
				const content = c.get(0) as Y.XmlElement;
				return content.get(0) as Y.XmlText;
			}
			if (c instanceof Y.XmlElement) {
				const hit = find(c);
				if (hit) return hit;
			}
		}
		return undefined;
	};
	const t = find(doc.getXmlFragment(COLLAB_FRAGMENT));
	if (!t) throw new Error(`no block ${id}`);
	t.insert(0, text);
}

const base = [para("a", "alpha"), para("b", "bravo"), para("c", "charlie")];

describe("mergeExternalChange", () => {
	test("replaces a block nobody is editing and keeps live edits elsewhere", () => {
		const doc = room(base);
		typeInto(doc, "a", "LIVE ");
		const next = [
			para("a", "alpha"),
			para("b", "bravo by agent"),
			para("c", "charlie"),
		];
		const res = mergeExternalChange(doc, COLLAB_FRAGMENT, base, next);
		expect(res.applied).toBe(1);
		expect(res.conflicts).toBe(0);
		expect(texts(doc)).toEqual(["LIVE alpha", "bravo by agent", "charlie"]);
	});

	test("keeps the live version of a block edited on both sides", () => {
		const doc = room(base);
		typeInto(doc, "b", "LIVE ");
		const next = [
			para("a", "alpha"),
			para("b", "bravo by agent"),
			para("c", "charlie"),
		];
		const res = mergeExternalChange(doc, COLLAB_FRAGMENT, base, next);
		expect(res.applied).toBe(0);
		expect(res.conflicts).toBe(1);
		expect(texts(doc)).toEqual(["alpha", "LIVE bravo", "charlie"]);
	});

	test("is a no-op for an editor's own save coming back", () => {
		const doc = room(base);
		typeInto(doc, "b", "LIVE ");
		const saved = [
			para("a", "alpha"),
			para("b", "LIVE bravo"),
			para("c", "charlie"),
		];
		const res = mergeExternalChange(doc, COLLAB_FRAGMENT, base, saved);
		expect(res.applied).toBe(0);
		expect(texts(doc)).toEqual(["alpha", "LIVE bravo", "charlie"]);
	});

	test("inserts added blocks after their previous sibling", () => {
		const doc = room(base);
		const next = [
			para("a", "alpha"),
			para("n1", "new one"),
			para("b", "bravo"),
			para("c", "charlie"),
			para("n2", "new two"),
		];
		const res = mergeExternalChange(doc, COLLAB_FRAGMENT, base, next);
		expect(res.applied).toBe(2);
		expect(texts(doc)).toEqual([
			"alpha",
			"new one",
			"bravo",
			"charlie",
			"new two",
		]);
	});

	test("deletes removed blocks only while untouched", () => {
		const doc = room(base);
		typeInto(doc, "c", "LIVE ");
		const next = [para("a", "alpha")];
		const res = mergeExternalChange(doc, COLLAB_FRAGMENT, base, next);
		expect(res.applied).toBe(1);
		expect(res.conflicts).toBe(1);
		expect(texts(doc)).toEqual(["alpha", "LIVE charlie"]);
	});

	test("recovers ids when the writer sends blocks without any", () => {
		const doc = room(base);
		typeInto(doc, "a", "LIVE ");
		const next = [
			para(undefined, "alpha"),
			para(undefined, "bravo by agent"),
			para(undefined, "charlie"),
			para(undefined, "delta"),
		];
		const res = mergeExternalChange(doc, COLLAB_FRAGMENT, base, next);
		expect(res.applied).toBe(2);
		expect(texts(doc)).toEqual([
			"LIVE alpha",
			"bravo by agent",
			"charlie",
			"delta",
		]);
	});

	test("ignores ids the writer invented", () => {
		const doc = room(base);
		const next = [
			para("1", "alpha"),
			para("2", "bravo!"),
			para("3", "charlie"),
		];
		mergeExternalChange(doc, COLLAB_FRAGMENT, base, next);
		expect(texts(doc)).toEqual(["alpha", "bravo!", "charlie"]);
	});

	test("changes a nested block without touching its parent's live edits", () => {
		const tree = [para("p", "parent", [para("k", "kid")])];
		const doc = room(tree);
		typeInto(doc, "p", "LIVE ");
		const next = [
			para("p", "parent", [para("k", "kid by agent"), para("k2", "kid two")]),
		];
		const res = mergeExternalChange(doc, COLLAB_FRAGMENT, tree, next);
		expect(res.applied).toBe(2);
		expect(texts(doc)).toEqual(["LIVE parent", "kid by agent", "kid two"]);
	});

	test("an agent that wrote from an empty document fills the room", () => {
		const doc = room([para("a", "")]);
		const next = [para(undefined, "written by agent")];
		mergeExternalChange(doc, COLLAB_FRAGMENT, [], next);
		expect(texts(doc).join("|")).toContain("written by agent");
	});

	test("an inserted block next to an edited one is not mistaken for the edit", () => {
		const two = [para("a", "alpha"), para("b", "bravo")];
		const doc = room(two);
		typeInto(doc, "a", "LIVE ");
		// Ids stripped, as every web save does: X prepended and alpha edited.
		const next = [
			para(undefined, "X new"),
			para(undefined, "alpha edited"),
			para(undefined, "bravo"),
		];
		mergeExternalChange(
			doc,
			COLLAB_FRAGMENT,
			two.map(({ id: _, ...b }) => b),
			next,
		);
		const got = texts(doc);
		expect(got).toContain("LIVE alpha");
		expect(got).toContain("X new");
		expect(got).toContain("bravo");
	});

	test("keeps a live edit when the stored content has no ids", () => {
		const doc = room(base);
		typeInto(doc, "b", "LIVE ");
		const noIds = (bs: typeof base) => bs.map(({ id: _, ...b }) => b);
		const next = [
			para(undefined, "alpha"),
			para(undefined, "agent"),
			para(undefined, "charlie"),
		];
		const res = mergeExternalChange(doc, COLLAB_FRAGMENT, noIds(base), next);
		expect(texts(doc)).toEqual(["alpha", "LIVE bravo", "charlie"]);
		expect(res.conflicts).toBe(1);
	});

	test("keeps a live edit next to the editor's trailing empty paragraph", () => {
		// BlockNote always keeps an empty block at the end of the document.
		const withTrailing = [...base, para("t", "")];
		const doc = room(withTrailing);
		typeInto(doc, "c", "LIVE ");
		const noIds = base.map(({ id: _, ...b }) => b);
		const next = [
			para(undefined, "alpha"),
			para(undefined, "bravo"),
			para(undefined, "agent"),
		];
		const res = mergeExternalChange(doc, COLLAB_FRAGMENT, noIds, next);
		expect(texts(doc).slice(0, 3)).toEqual(["alpha", "bravo", "LIVE charlie"]);
		expect(res.conflicts).toBe(1);
	});

	test("does not insert twice when the room was seeded after the write", () => {
		const noIds = base.map(({ id: _, ...b }) => b);
		const next = [
			para(undefined, "alpha"),
			para(undefined, "bravo"),
			para(undefined, "charlie"),
			para(undefined, "agent"),
		];
		// The room already holds the written content.
		const doc = room(next.map((b, i) => ({ ...b, id: `s${i}` })));
		const res = mergeExternalChange(doc, COLLAB_FRAGMENT, noIds, next);
		expect(res.applied).toBe(0);
		expect(texts(doc)).toEqual(["alpha", "bravo", "charlie", "agent"]);
	});

	test("does not duplicate an added block already present beside live edits", () => {
		const noIds = base.map(({ id: _, ...b }) => b);
		const next = [...noIds, para(undefined, "agent")];
		const doc = room([...base, para("n", "agent")]);
		typeInto(doc, "a", "LIVE ");
		mergeExternalChange(doc, COLLAB_FRAGMENT, noIds, next);
		expect(texts(doc).filter((t) => t === "agent")).toHaveLength(1);
	});

	test("an identical save changes nothing", () => {
		const doc = room(base);
		const res = mergeExternalChange(doc, COLLAB_FRAGMENT, base, base);
		expect(res).toEqual({ applied: 0, conflicts: 0 });
	});

	test("short unrelated blocks are not taken for edits of each other", () => {
		const doc = room([para("x", "A."), para("y", "keep")]);
		mergeExternalChange(
			doc,
			COLLAB_FRAGMENT,
			[para("x", "A."), para("y", "keep")],
			[para(undefined, "B."), para(undefined, "keep")],
		);
		expect(texts(doc)).toEqual(["B.", "keep"]);
	});
});

describe("schema parity with the web editor", () => {
	test("custom blocks and inline contents match apps/web", () => {
		const src = readFileSync(
			new URL(
				"../../../apps/web/src/components/shared/blocknote-schema.tsx",
				import.meta.url,
			),
			"utf8",
		);
		const section = (name: string) => {
			const m = src.match(new RegExp(`${name}: \\{([\\s\\S]*?)\\n\\t\\},`));
			return [...(m?.[1] ?? "").matchAll(/^\t\t(\w+):/gm)].map((x) => x[1]);
		};
		expect(section("blockSpecs").sort()).toEqual([...CUSTOM_BLOCKS].sort());
		expect(section("inlineContentSpecs").sort()).toEqual(
			[...CUSTOM_INLINES].sort(),
		);
	});
});

describe("mergeExternalChange dedupe guard", () => {
	const noIds = (bs: object[]) =>
		bs.map((b) => {
			const { id: _, ...rest } = b as Record<string, unknown>;
			return rest;
		});

	test("keeps an added blank paragraph next to the editor's trailing one", () => {
		const doc = room([...base, para("t", "")]);
		const old = noIds(base);
		const res = mergeExternalChange(doc, COLLAB_FRAGMENT, old, [
			old[0] as never,
			para(undefined, ""),
			old[1] as never,
			old[2] as never,
		]);
		expect(res.applied).toBe(1);
		expect(texts(doc)).toEqual(["alpha", "", "bravo", "charlie", ""]);
	});

	test("inserts every one of several identical additions", () => {
		const doc = room([para("a", "alpha")]);
		const old = noIds([para("a", "alpha")]);
		mergeExternalChange(doc, COLLAB_FRAGMENT, old, [
			old[0] as never,
			para(undefined, "TODO"),
			para(undefined, "TODO"),
		]);
		expect(texts(doc)).toEqual(["alpha", "TODO", "TODO"]);
	});

	test("collapses each addition once when seeded after the write", () => {
		const old = noIds([para("a", "alpha")]);
		const next = [
			old[0] as never,
			para(undefined, "TODO"),
			para(undefined, "TODO"),
		];
		// Same second block retyped live in the seed, so shapes differ overall.
		const doc = room([
			para("s0", "alpha"),
			para("s1", "TODO"),
			para("s2", "TODO"),
			para("s3", "extra"),
		]);
		const res = mergeExternalChange(doc, COLLAB_FRAGMENT, old, next);
		expect(texts(doc)).toEqual(["alpha", "TODO", "TODO", "extra"]);
		expect(res.duplicates).toBe(2);
	});

	test("does not report conflicts for the children of a skipped duplicate", () => {
		const old = noIds([para("a", "alpha")]);
		const next = [
			old[0] as never,
			para(undefined, "parent", [para(undefined, "child")]),
		];
		const doc = room([
			para("s0", "alpha"),
			para("s1", "parent", [para("s2", "child")]),
			para("s3", "extra"),
		]);
		const res = mergeExternalChange(doc, COLLAB_FRAGMENT, old, next);
		expect(res.conflicts).toBe(0);
		expect(res.duplicates).toBe(1);
	});

	test("a short block edited live is not paired with an unrelated addition", () => {
		const anchor = para("k", "anchor text");
		const doc = room([anchor, para("s", "ab")]);
		typeInto(doc, "s", "LIVE ");
		mergeExternalChange(
			doc,
			COLLAB_FRAGMENT,
			noIds([anchor, para("s", "ab")]),
			[
				...noIds([anchor]),
				para(undefined, "abc"),
				para(undefined, "extra"),
			] as never,
		);
		expect(texts(doc)).toContain("LIVE ab");
		expect(texts(doc)).toContain("extra");
		// "abc" is an addition, not a modified "ab": it is not swallowed by the
		// live edit on "ab".
		expect(texts(doc)).toContain("abc");
	});
});
