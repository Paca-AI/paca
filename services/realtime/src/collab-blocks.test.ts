import { describe, expect, test } from "bun:test";
import { BlockNoteEditor } from "@blocknote/core";
import { blocksToYDoc, yDocToBlocks } from "@blocknote/core/yjs";
import * as Y from "yjs";
import { COLLAB_FRAGMENT } from "./collab.ts";
import { mergeExternalChange } from "./collab-blocks.ts";

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
});
