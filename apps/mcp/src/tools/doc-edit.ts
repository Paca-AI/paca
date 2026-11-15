// Exact-string edits on a document's blocks that leave every block outside
// the edit as the very object the API returned, so it serializes to the same
// JSON and the stored copy of it does not change.
//
// Text positions are positions in blocknoteToMarkdown(blocks), the Markdown
// read_doc returns, so `old_string` is matched against what the agent read
// and line numbers agree with read_doc and grep_docs.

import { PacaAPIError } from "../api/doc-client.js";
import type { Document } from "../types/index.js";
import { blocknoteToMarkdown, markdownToBlocknote } from "../utils/index.js";

export type Block = Record<string, any>;

/** Why an edit could not be applied. */
export class SpliceError extends Error {
	constructor(
		message: string,
		readonly kind: "not_found" | "ambiguous" | "empty" | "unsafe" | "no_change",
		/** The 1-based Markdown lines where `old_string` starts. */
		readonly lines: number[] = [],
	) {
		super(message);
		this.name = "SpliceError";
	}
}

// List items render as one Markdown list, so how one is written depends on
// its neighbours (numbering, list markers, loose or tight spacing); a block
// that renders to nothing (an empty paragraph) can split a list. Such runs
// are rendered and re-parsed as a whole.
const LIST_TYPES = new Set([
	"bulletListItem",
	"numberedListItem",
	"checkListItem",
	"toggleListItem",
]);

interface Unit {
	/** Block indices [from, to). */
	from: number;
	to: number;
	/** Character range [start, end) in the full Markdown. The newline that
	 *  separates a unit from the previous one belongs to it. */
	start: number;
	end: number;
}

/** Renders of blocks by object, for one edit: the exporter is slow, and a
 *  block is never changed once read or parsed. spliceEdit starts afresh. */
const newKeys = () => ({
	own: new WeakMap<Block, string>(),
	sub: new WeakMap<Block, string>(),
	text: new WeakMap<Block, string>(),
	canon: new WeakMap<Block, string>(),
	words: new Map<string, Map<string, number>>(),
});
let keys = newKeys();

/** A block's Markdown with its nested blocks (cached per object). */
function subKey(b: Block): string {
	if (!Array.isArray(b.children) || b.children.length === 0) return ownKey(b);
	let k = keys.sub.get(b);
	if (k === undefined) {
		k = render([b]);
		keys.sub.set(b, k);
	}
	return k;
}

/** Whether a block shows nothing in Markdown (an empty paragraph), without
 *  rendering the blocks that obviously show something. */
function blank(b: Block): boolean {
	// Headings, list items, quotes and code always write a marker; a
	// paragraph with visible text writes it.
	if (MARKER_TYPES.has(b.type)) return false;
	if (
		b.type === "paragraph" &&
		Array.isArray(b.content) &&
		b.content.some(
			(c: Block) => typeof c?.text === "string" && c.text.trim() !== "",
		)
	) {
		return false;
	}
	return subKey(b) === "";
}
const MARKER_TYPES = new Set([
	"heading",
	"bulletListItem",
	"numberedListItem",
	"checkListItem",
	"codeBlock",
	"quote",
]);

/** A block's own fields as Markdown sees them (type, props without neutral
 *  values, text): blocks equal here render the same, without the exporter. */
function canonOwn(b: Block): string {
	let k = keys.canon.get(b);
	if (k === undefined) {
		k = stable([b.type, canonProps(b.props), canonContent(b.content)]);
		keys.canon.set(b, k);
	}
	return k;
}

function render(blocks: Block[]): string {
	return blocknoteToMarkdown(blocks);
}

/**
 * Groups `blocks` into units whose Markdown, rendered on its own, is exactly
 * its part of `full`, and records where each part is. When that does not
 * hold (a rendering this does not foresee), the whole document is one unit:
 * the edit is still correct, it just re-parses more.
 */
function unitsOf(blocks: Block[], renders: string[], full: string): Unit[] {
	const groups: Array<[number, number]> = [];
	for (let i = 0; i < blocks.length; ) {
		const joinable = (k: number) =>
			LIST_TYPES.has(blocks[k]?.type) || renders[k] === "";
		let j = i + 1;
		if (joinable(i)) while (j < blocks.length && joinable(j)) j++;
		groups.push([i, j]);
		i = j;
	}

	const units: Unit[] = [];
	let pos = 0;
	for (const [from, to] of groups) {
		const md = to - from === 1 ? renders[from] : render(blocks.slice(from, to));
		if (md === "") {
			units.push({ from, to, start: pos, end: pos });
			continue;
		}
		const part = (pos === 0 ? "" : "\n") + md;
		if (!full.startsWith(part, pos)) {
			return [{ from: 0, to: blocks.length, start: 0, end: full.length }];
		}
		units.push({ from, to, start: pos, end: pos + part.length });
		pos += part.length;
	}
	if (pos !== full.length) {
		return [{ from: 0, to: blocks.length, start: 0, end: full.length }];
	}
	return units;
}

// A list marker at the start of a block's own render ("1. ", "* ", "- [ ] "):
// in a list the number differs ("2. "), so a block is found by its text.
const LIST_MARKER = /^\s*(?:[*+-]|\d+[.)])\s+(?:\[[ xX]\]\s+)?/;

/**
 * Where each block starts in `full` (the offset of its first line), or -1
 * when that is not found. Inside a unit the blocks are found in order, each
 * after the one before it, by the first line of its own render without its
 * list marker: a numbered item renders "1. beta" alone but "2. beta" in
 * its list.
 */
function blockStarts(renders: string[], units: Unit[], full: string): number[] {
	const out = renders.map(() => -1);
	for (const u of units) {
		let cursor = u.start;
		for (let k = u.from; k < u.to; k++) {
			const first = renders[k].split("\n")[0].replace(LIST_MARKER, "");
			if (first === "") continue;
			const found = full.indexOf(first, cursor);
			if (found === -1 || found >= u.end) continue;
			out[k] = full.lastIndexOf("\n", found - 1) + 1;
			cursor = found + first.length;
		}
	}
	return out;
}

/** Where `text` next starts a line of `full` at or after `from`, after
 *  only indentation, quote marks and a list marker; -1 if nowhere. */
function lineStartOf(full: string, text: string, from: number): number {
	for (
		let at = full.indexOf(text, from);
		at >= 0;
		at = full.indexOf(text, at + 1)
	) {
		const head = full.slice(full.lastIndexOf("\n", at - 1) + 1, at);
		if (/^[\s>]*(?:(?:[*+-]|\d+[.)])\s+(?:\[[ xX]\]\s+)?)?$/.test(head))
			return at;
	}
	return -1;
}

function occurrences(text: string, needle: string): number[] {
	const out: number[] = [];
	for (
		let i = text.indexOf(needle);
		i !== -1;
		i = text.indexOf(needle, i + needle.length)
	) {
		out.push(i);
	}
	return out;
}

function lineAt(text: string, offset: number): number {
	let line = 1;
	for (let i = 0; i < offset; i++) if (text.charCodeAt(i) === 10) line++;
	return line;
}

function touches(u: Unit, ms: number, me: number): boolean {
	if (u.start === u.end) return ms < u.start && u.start < me;
	return u.end > ms && u.start < me;
}

const DEFAULT_PROPS: Record<string, unknown> = {
	textColor: "default",
	backgroundColor: "default",
	textAlignment: "left",
};

// ── Can Markdown carry this block? ────────────────────────────────────────────
//
// A block the edit touches is rewritten from Markdown. That is only safe when
// rendering the block to Markdown and parsing it back gives the same block
// (ids aside); otherwise the edit would silently drop what Markdown cannot
// say (a mention chip, underline, a colour on a nested block, a toggle, a
// table without a header row, a diagram).

/** Prop values that mean "not set": a missing key equals one of these. */
const NEUTRAL_PROPS: Record<string, unknown> = {
	...DEFAULT_PROPS,
	isToggleable: false,
	checked: false,
	colspan: 1,
	rowspan: 1,
	caption: "",
	showPreview: true,
	language: "text",
};

function canonProps(props: unknown): Record<string, unknown> {
	const out: Record<string, unknown> = {};
	for (const [k, v] of Object.entries(
		(props ?? {}) as Record<string, unknown>,
	)) {
		if (v === undefined || NEUTRAL_PROPS[k] === v) continue;
		out[k] = v;
	}
	return out;
}

/** Inline content with false styles dropped and equal-styled text joined.
 *  `loose`: whitespace Markdown does not show is normalised too (runs of
 *  spaces and tabs, spaces around a line break, and the text's two ends),
 *  so a paragraph typed with a trailing or double space still compares
 *  equal to what Markdown gives back. */
function canonInline(content: unknown, loose = false): unknown[] {
	if (!Array.isArray(content)) return [];
	const out = canonRuns(content, loose);
	if (!loose) return out;
	const texts = out.filter((n) => n.type === "text");
	if (texts.length > 0) {
		const first = texts[0];
		if (!first.styles.code) first.text = first.text.replace(/^\s+/, "");
		const last = texts[texts.length - 1];
		if (!last.styles.code) last.text = last.text.replace(/\s+$/, "");
	}
	return out.filter((n) => !(n.type === "text" && n.text === ""));
}

function canonRuns(content: unknown[], loose: boolean): Block[] {
	const out: Block[] = [];
	for (const node of content as Block[]) {
		if (node?.type === "text" && typeof node.text === "string") {
			if (node.text === "") continue;
			const styles = Object.fromEntries(
				Object.entries((node.styles ?? {}) as Record<string, unknown>)
					.filter(([, v]) => v !== false && v !== undefined)
					.sort(([a], [b]) => a.localeCompare(b)),
			);
			const prev = out[out.length - 1];
			if (prev?.type === "text" && stable(prev.styles) === stable(styles)) {
				prev.text += node.text;
			} else {
				out.push({ type: "text", text: node.text, styles });
			}
			// Inline code keeps its spaces in Markdown: compared strictly.
			if (loose && !out[out.length - 1].styles.code) {
				const last = out[out.length - 1];
				last.text = last.text.replace(/[ \t]+/g, " ").replace(/ ?\n ?/g, "\n");
			}
		} else if (node?.type === "link") {
			out.push({
				type: "link",
				href: node.href,
				content: canonInline(node.content, loose),
			});
		} else {
			out.push(node);
		}
	}
	return out;
}

function canonContent(content: unknown, loose = false): unknown {
	if (content == null) return null;
	if (Array.isArray(content)) return canonInline(content, loose);
	const c = content as Block;
	if (c.type === "tableContent") {
		return {
			type: c.type,
			headerRows: c.headerRows ?? null,
			headerCols: c.headerCols ?? null,
			columnWidths: (c.columnWidths ?? []).map((w: unknown) => w ?? null),
			rows: (c.rows ?? []).map((r: Block) => ({
				cells: (r.cells ?? []).map((cell: unknown) =>
					Array.isArray(cell)
						? { props: {}, content: canonInline(cell, loose) }
						: {
								props: canonProps((cell as Block).props),
								content: canonInline((cell as Block).content, loose),
							},
				),
			})),
		};
	}
	return c;
}

/** `b` without ids and neutral values, for comparing. `loose`: also
 *  without whitespace Markdown does not show (never in code blocks). */
function canonBlock(b: Block, loose = false): unknown {
	return {
		type: b.type,
		props: canonProps(b.props),
		content: canonContent(b.content, loose && b.type !== "codeBlock"),
		children: (Array.isArray(b.children) ? b.children : []).map((c: Block) =>
			canonBlock(c, loose),
		),
	};
}

/** What the Markdown round trip adds to a soft line break: the parser keeps
 *  the indent of the continuation line, so "l1\nl2" comes back as
 *  "l1\n l2". Removes it from parsed blocks (not from code). */
function fixSoftBreaks(blocks: Block[]): Block[] {
	const inline = (nodes: unknown): unknown =>
		Array.isArray(nodes)
			? nodes.map((n: Block) =>
					n?.type === "text" && typeof n.text === "string"
						? { ...n, text: n.text.replace(/\n +/g, "\n") }
						: n?.type === "link"
							? { ...n, content: inline(n.content) }
							: n,
				)
			: nodes;
	return blocks.map((b) => ({
		...b,
		...(b.type !== "codeBlock" && Array.isArray(b.content)
			? { content: inline(b.content) }
			: {}),
		...(Array.isArray(b.children)
			? { children: fixSoftBreaks(b.children) }
			: {}),
	}));
}

/** "a table block", "an image block". */
function aBlock(type: string): string {
	return `${/^[aeiou]/i.test(type) ? "an" : "a"} ${type} block`;
}

/** JSON with object keys sorted, for comparing values. */
function stable(v: unknown): string {
	return JSON.stringify(v, (_k, x) =>
		x && typeof x === "object" && !Array.isArray(x)
			? Object.fromEntries(
					Object.entries(x).sort(([a], [b]) => a.localeCompare(b)),
				)
			: x,
	);
}

/** How an old block's props reach the block that replaces it:
 *  "paired": one block of its type stands in for it, inheriting its id and
 *  the props Markdown cannot carry (colours, alignment);
 *  "carried": it is merged into another block, which keeps its own. */
type Carry = "paired" | "carried";

/** Why `b`'s own part (its type, props and text; its nested blocks are
 *  aligned on their own) cannot be rewritten from Markdown, or null. */
function markdownLoss(b: Block, carry: Carry): string | null {
	const own = { ...b, children: [] };
	const parsed = fixSoftBreaks(markdownToBlocknote(render([own])));
	let back: Block[] = parsed;
	if (carry === "paired" && parsed.length === 1 && parsed[0].type === b.type) {
		back = [inherit(b, parsed[0])];
	}
	if (back.length !== 1)
		return `Markdown has no exact form for ${aBlock(b.type)}`;
	const a = canonBlock(own, true) as Block;
	const z = canonBlock({ ...back[0], children: [] }, true) as Block;
	if (carry === "carried") {
		for (const k of Object.keys(DEFAULT_PROPS)) {
			delete a.props[k];
			delete z.props[k];
		}
	}
	if (a.type !== z.type) return `it would become ${aBlock(z.type)}`;
	if (stable(a.props) !== stable(z.props)) {
		const keys = [
			...new Set([...Object.keys(a.props), ...Object.keys(z.props)]),
		]
			.filter((k) => stable(a.props[k]) !== stable(z.props[k]))
			.join(", ");
		return `its ${keys} would be lost`;
	}
	if (stable(a.content) !== stable(z.content)) {
		const what = new Set<string>();
		const walk = (nodes: unknown) => {
			if (!Array.isArray(nodes)) return;
			for (const n of nodes as Block[]) {
				if (n?.type === "text") {
					for (const s of Object.keys(n.styles ?? {})) {
						if (!["bold", "italic", "code", "strike"].includes(s)) what.add(s);
					}
				} else if (n?.type === "link") walk(n.content);
				else if (n?.type) what.add(n.type);
			}
		};
		if (b.type === "table")
			return "its header row or cell formatting would change";
		walk(b.content);
		return what.size > 0
			? `its ${[...what].join(", ")} would be lost`
			: "its text would not come back the same";
	}
	return null;
}

function subtree(b: Block): Block[] {
	return [b, ...(Array.isArray(b.children) ? b.children : []).flatMap(subtree)];
}

const kidsOf = (b: Block): Block[] =>
	Array.isArray(b.children) ? b.children : [];

/** How blocks nest, types aside. */
const shape = (b: Block): string => `(${kidsOf(b).map(shape).join("")})`;

/** Whether Markdown writes `b`'s nested blocks as nested (a list item's
 *  are; a paragraph's come back as blocks after it). */
function nestsInMarkdown(b: Block): boolean {
	if (kidsOf(b).every(blank)) return true;
	const back = fixSoftBreaks(markdownToBlocknote(render([b])));
	return back.length === 1 && shape(back[0]) === shapeShown(b);
}

/** How blocks nest, without those that show nothing (an empty paragraph
 *  writes no Markdown, so it is placed by its neighbours instead). */
const shapeShown = (b: Block): string =>
	`(${kidsOf(b)
		.filter((k) => !blank(k))
		.map(shapeShown)
		.join("")})`;

/** `blocks` without those that show nothing, at any depth. */
function withoutBlank(blocks: Block[]): Block[] {
	return blocks
		.filter((b) => !blank(b))
		.map((b) => {
			const kids = kidsOf(b);
			if (kids.length === 0) return b;
			const next = withoutBlank(kids);
			return next.length === kids.length && next.every((k, i) => k === kids[i])
				? b
				: { ...b, children: next };
		});
}

/** Why an old block whose nested blocks Markdown cannot nest cannot be
 *  rewritten. */
function nestedLoss(b: Block): string {
	return markdownToBlocknote(render([b])).length > 1
		? "its nested blocks would become separate top-level blocks"
		: "its nested blocks would change (their type, colours or other properties)";
}

/** Props Markdown writes (a list's start number, a check, a heading level,
 *  a code block's language): an edit's new Markdown decides them, also
 *  when the parser leaves one out at its default value (start 1). */
const MARKDOWN_PROPS = new Set(["start", "checked", "level", "language"]);

/**
 * `fresh` (parsed from Markdown) standing in for `old`: it keeps old's id
 * (or lack of one) and the props Markdown cannot carry, such as colours;
 * the props Markdown writes, the text and the nested blocks come from
 * `fresh` (the caller aligns the nested blocks).
 */
function inherit(old: Block, fresh: Block): Block {
	const props: Record<string, unknown> = { ...(old.props ?? {}) };
	for (const k of MARKDOWN_PROPS) {
		if (!(k in (fresh.props ?? {})) && NEUTRAL_PROPS[k] !== props[k]) {
			delete props[k];
		}
	}
	for (const [k, v] of Object.entries(fresh.props ?? {})) {
		if (k in props && DEFAULT_PROPS[k] === v) continue;
		props[k] = v;
	}
	const out: Block = { ...fresh, props };
	if ("id" in old) out.id = old.id;
	else delete out.id;
	const table = inheritTable(old.content, fresh.content);
	if (table) out.content = table;
	return out;
}

/** A table's new text (`fresh`) with the formatting Markdown cannot write
 *  kept from `old` (column widths, header settings, each cell's props), when
 *  both have the same rows and columns; otherwise null. */
function inheritTable(old: unknown, fresh: unknown): Block | null {
	const o = old as Block | null;
	const f = fresh as Block | null;
	if (o?.type !== "tableContent" || f?.type !== "tableContent") return null;
	const dims = (t: Block) =>
		(t.rows ?? []).map((r: Block) => (r.cells ?? []).length).join(",");
	if (dims(o) !== dims(f)) return null;
	const out: Block = { ...f };
	for (const k of ["columnWidths", "headerRows", "headerCols"]) {
		if (k in o) out[k] = o[k];
		else delete out[k];
	}
	out.rows = (f.rows ?? []).map((r: Block, y: number) => ({
		...r,
		cells: (r.cells ?? []).map((c: unknown, x: number) => {
			const was = o.rows[y].cells[x];
			if (Array.isArray(was) || !was?.props) return c;
			const content = Array.isArray(c) ? c : (c as Block).content;
			const base = Array.isArray(c) ? { type: "tableCell" } : (c as Block);
			return { ...base, props: was.props, content };
		}),
	}));
	return out;
}

/** What `b` carries that tells it apart from a block that reads the same. */
const dataOf = (b: Block) => stable([b.type, uncarried(b)]);
const carries = (b: Block) => !carriesNothing(uncarried(b));

/** Why the alignment cannot rewrite an old block. */
export interface Problem {
	block: Block;
	why: string;
	/** "differ": the blocks merged with it (`with`) have other colours;
	 *  "ambiguous": the edit could mean it or the blocks in `with`. */
	kind: "loss" | "differ" | "ambiguous";
	with?: Block[];
}

/** A block of an edited range, in document order (parents before
 *  children). */
interface Entry {
	b: Block;
	/** The block it is nested in; null at the top of the range. */
	parent: Block | null;
	/** Its own Markdown ("" when it shows nothing). For an old block whose
	 *  nested blocks Markdown cannot nest (`whole`), the Markdown of its
	 *  whole subtree: it is matched, or rewritten, as one. */
	key: string;
	whole: boolean;
	/** Entries in its subtree, itself included. */
	size: number;
}

function flatten(
	blocks: Block[],
	parent: Block | null,
	old: boolean,
	out: Entry[] = [],
	// The blocks were read back nested as they are, as part of a parent
	// whose whole subtree Markdown nests: no need to parse them again
	// (each check parses the whole subtree, which made deep lists slow).
	nested = false,
): Entry[] {
	for (const b of blocks) {
		const whole = old && !nested && !nestsInMarkdown(b);
		const e: Entry = {
			b,
			parent,
			// A new block's key is looked up when needed (see align).
			key: whole ? render([b]) : old ? ownKey(b) : "",
			whole,
			size: 1,
		};
		out.push(e);
		const at = out.length;
		if (!whole) flatten(kidsOf(b), b, old, out, old);
		e.size = out.length - at + 1;
	}
	return out;
}

/**
 * Replaces `oldBlocks` with `fresh` (parsed from the edited Markdown). Which
 * old block a new block is, is decided by one rule, at every depth at once
 * (both trees flattened in document order, parents before children):
 * 1. Position. The longest common subsequence of the blocks' own Markdown
 *    (without their nested blocks) pairs old and new blocks that read the
 *    same, in order. Such an old block is returned as the very object read
 *    when its nested blocks are too, else with only its nested blocks
 *    replaced: its type, props, text and id stay. So an indent nests the
 *    second of two equal items under the first, never the other way round.
 *    When a deleted old block reads like a kept one next to it in that
 *    order and differs from it (colours, type, chips), the edit could mean
 *    either: it is refused ("ambiguous").
 * 2. Moves. Old and new blocks the order left out that read the same pair
 *    in order (a swap); when the old ones differ and are not as many as
 *    the new ones, the edit is refused ("ambiguous").
 * 3. Text. The rest, between two blocks paired above, are rewritten:
 *    - as many new blocks as old ones, of the same types, and no new block
 *      continues another old block's text better than its own (see
 *      shared): each stands in for the old block at its position (its id,
 *      colours, alignment);
 *    - otherwise each old block goes to the new block that continues its
 *      text most (a tie refuses the edit when the old block carries
 *      something Markdown cannot write; otherwise the first wins; an old
 *      block whose text no new block continues is deleted). The first old
 *      block a new block continues gives it its id and props, when their
 *      types match; the others merged into it must have the same colours
 *      and alignment. A plain old block no new block continues gives its
 *      id to the next new block of its type nothing continues, in order.
 *      Other new blocks are new: plain, with ids of their own.
 * Old blocks that render to nothing (an empty paragraph) stay, placed
 * after the block that now stands for their previous sibling, else before
 * the first new block next to the one that stands for their next sibling,
 * else first in what stands for their parent.
 */
function align(
	oldBlocks: Block[],
	fresh: Block[],
): { blocks: Block[]; problems: Problem[] } {
	const O = flatten(oldBlocks, null, true);
	const F = flatten(fresh, null, false);
	const n = O.length;
	const m = F.length;
	const subOf = (j: number) => subKey(F[j].b);
	// Blocks equal as Markdown sees them render the same: the exporter
	// runs only for those that differ there.
	const eq = (i: number, j: number) =>
		O[i].key !== "" &&
		(O[i].whole
			? subOf(j) === O[i].key
			: canonOwn(O[i].b) === canonOwn(F[j].b) || O[i].key === ownKey(F[j].b));
	const step = (i: number, j: number) => (O[i].whole ? F[j].size : 1);

	type Op =
		| { op: "keep"; i: number; j: number }
		| { op: "del"; i: number }
		| { op: "ins"; j: number };
	// The common prefix and suffix first: most edits touch a few blocks.
	const head: Op[] = [];
	let i0 = 0;
	let j0 = 0;
	while (i0 < n && j0 < m && eq(i0, j0)) {
		head.push({ op: "keep", i: i0, j: j0 });
		j0 += step(i0, j0);
		i0++;
	}
	const tail: Op[] = [];
	let i1 = n;
	let j1 = m;
	while (i1 > i0 && j1 > j0 && !O[i1 - 1].whole && eq(i1 - 1, j1 - 1)) {
		i1--;
		j1--;
		tail.unshift({ op: "keep", i: i1, j: j1 });
	}
	const w = j1 - j0 + 1;
	const dp = new Int32Array((i1 - i0 + 1) * w);
	const at = (i: number, j: number) => (i - i0) * w + (j - j0);
	for (let i = i1 - 1; i >= i0; i--) {
		for (let j = j1 - 1; j >= j0; j--) {
			let v = Math.max(dp[at(i + 1, j)], dp[at(i, j + 1)]);
			if (eq(i, j) && j + step(i, j) <= j1) {
				v = Math.max(v, 1 + dp[at(i + 1, j + step(i, j))]);
			}
			dp[at(i, j)] = v;
		}
	}
	const ops: Op[] = [...head];
	for (let i = i0, j = j0; i < i1 || j < j1; ) {
		if (
			i < i1 &&
			j < j1 &&
			eq(i, j) &&
			j + step(i, j) <= j1 &&
			dp[at(i, j)] === 1 + dp[at(i + 1, j + step(i, j))]
		) {
			ops.push({ op: "keep", i, j });
			j += step(i, j);
			i++;
		} else if (j < j1 && (i === i1 || dp[at(i, j + 1)] >= dp[at(i + 1, j)])) {
			ops.push({ op: "ins", j });
			j++;
		} else {
			ops.push({ op: "del", i });
			i++;
		}
	}
	ops.push(...tail);

	type Role = { how: "keep" | "whole" | "pair"; old: Block } | { how: "new" };
	const role = new Map<Block, Role>();
	/** The new block that stands for an old one. */
	const counterpart = new Map<Block, Block>();
	const problems: Problem[] = [];
	const freshParent = new Map<Block, Block | null>();
	for (const e of F) freshParent.set(e.b, e.parent);
	const loss = (o: Entry, carry: Carry) => {
		const why = o.whole ? nestedLoss(o.b) : markdownLoss(o.b, carry);
		if (why) problems.push({ block: o.b, why, kind: "loss" });
	};

	for (const op of ops) {
		if (op.op !== "keep") continue;
		const o = O[op.i];
		role.set(F[op.j].b, { how: o.whole ? "whole" : "keep", old: o.b });
		counterpart.set(o.b, F[op.j].b);
	}

	// Moves.
	const loose = new Map<string, { olds: Entry[]; news: Entry[] }>();
	const group = (key: string) => {
		let g = loose.get(key);
		if (!g) {
			g = { olds: [], news: [] };
			loose.set(key, g);
		}
		return g;
	};
	for (const op of ops) {
		if (op.op === "del" && O[op.i].key !== "" && !O[op.i].whole) {
			group(O[op.i].key).olds.push(O[op.i]);
		} else if (op.op === "ins") group(ownKey(F[op.j].b)).news.push(F[op.j]);
	}
	for (const { olds, news } of loose.values()) {
		const k = Math.min(olds.length, news.length);
		if (k === 0) continue;
		const differ = new Set(olds.map((o) => dataOf(o.b))).size > 1;
		if (
			(olds.length > news.length && differ) ||
			(news.length > olds.length && olds.some((o) => carries(o.b)))
		) {
			problems.push({
				block: olds[0].b,
				why: "ambiguous",
				kind: "ambiguous",
				with: olds.slice(1).map((o) => o.b),
			});
			continue;
		}
		for (let q = 0; q < k; q++) {
			role.set(news[q].b, { how: "keep", old: olds[q].b });
			counterpart.set(olds[q].b, news[q].b);
		}
	}

	// Rewrites, stretch by stretch.
	const stretchOf = new Map<Block, Entry[]>();
	let prevKeep: Entry | null = null;
	for (let k = 0; k < ops.length; ) {
		const op = ops[k];
		if (op.op === "keep") {
			prevKeep = O[op.i];
			k++;
			continue;
		}
		const st: Op[] = [];
		while (k < ops.length && ops[k].op !== "keep") st.push(ops[k++]);
		const next = ops[k] as { op: "keep"; i: number } | undefined;
		const nextKeep = next ? O[next.i] : null;
		const olds = st.flatMap((s) =>
			s.op === "del" && !counterpart.has(O[s.i].b) ? [O[s.i]] : [],
		);
		const news = st.flatMap((s) =>
			s.op === "ins" && !role.has(F[s.j].b) ? [F[s.j]] : [],
		);
		for (const o of olds) stretchOf.set(o.b, news);
		const reals = olds.filter((o) => o.key !== "");
		const deleted: Entry[] = [];
		const consistent = () =>
			reals.length === news.length &&
			reals.every((r, q) => r.b.type === news[q].b.type) &&
			reals.every((r, q) => {
				const mine = shared(ownText(r.b), ownText(news[q].b));
				return news.every(
					(f, l) =>
						l === q ||
						(shared(ownText(r.b), ownText(f.b)) <= mine &&
							shared(ownText(reals[l].b), ownText(news[q].b)) <= mine),
				);
			});
		if (news.length === 0) deleted.push(...reals);
		else if (reals.length > 0 && consistent()) {
			reals.forEach((r, q) => {
				role.set(news[q].b, { how: "pair", old: r.b });
				counterpart.set(r.b, news[q].b);
				loss(r, "paired");
			});
		} else if (reals.length > 0) {
			const claims = new Map<Entry, Entry[]>();
			for (const r of reals) {
				// Text inside a new block's text continues there whole.
				const mine = ownText(r.b);
				const scores = news.map((f) =>
					mine !== "" && ownText(f.b).includes(mine)
						? mine.length
						: shared(mine, ownText(f.b)),
				);
				const best = Math.max(...scores);
				if (best === 0) {
					deleted.push(r);
					continue;
				}
				const ties = news.filter((_, q) => scores[q] === best);
				if (ties.length > 1 && carries(r.b)) {
					problems.push({ block: r.b, why: "split", kind: "ambiguous" });
					continue;
				}
				const list = claims.get(ties[0]) ?? [];
				list.push(r);
				claims.set(ties[0], list);
			}
			// Plain blocks no new block continues (rewritten past recognition)
			// keep their ids on the new blocks nothing continues, in order and
			// by type. Blocks that carry more stay deleted: the audit decides.
			let from = 0;
			for (const r of [...deleted]) {
				if (carries(r.b) || r.whole) continue;
				const q = news.findIndex(
					(f, k) => k >= from && !claims.has(f) && f.b.type === r.b.type,
				);
				if (q < 0) continue;
				claims.set(news[q], [r]);
				deleted.splice(deleted.indexOf(r), 1);
				from = q + 1;
			}
			for (const f of news) {
				const cl = claims.get(f);
				if (!cl) continue;
				const first = cl[0];
				for (const r of cl) counterpart.set(r.b, f.b);
				if (first.b.type === f.b.type) {
					role.set(f.b, { how: "pair", old: first.b });
					loss(first, "paired");
				} else if (first.whole || carries(first.b)) {
					problems.push({
						block: first.b,
						why: first.whole
							? nestedLoss(first.b)
							: `it would become ${[...new Set(news.map((x) => x.b.type))].join(", ")} blocks, which cannot keep its colour or alignment`,
						kind: "loss",
					});
				}
				for (const r of cl.slice(1)) {
					if (
						stable(uncarried(r.b).props) !== stable(uncarried(first.b).props)
					) {
						problems.push({
							block: r.b,
							why: "differ",
							kind: "differ",
							with: cl.map((c) => c.b),
						});
					} else loss(r, "carried");
				}
			}
		}
		// A deleted block that reads like a kept neighbour in the order, but
		// differs from it: the edit could mean either.
		for (const d of deleted) {
			for (const kept of [prevKeep, nextKeep]) {
				if (
					kept &&
					!kept.whole &&
					kept.key === d.key &&
					dataOf(kept.b) !== dataOf(d.b)
				) {
					problems.push({
						block: kept.b,
						why: "ambiguous",
						kind: "ambiguous",
						with: [d.b],
					});
				}
			}
		}
	}

	// Blocks that render to nothing.
	const before = new Map<Block, Block[]>();
	const after = new Map<Block, Block[]>();
	const first = new Map<Block | null, Block[]>();
	const put = <K>(map: Map<K, Block[]>, k: K, b: Block) => {
		const list = map.get(k) ?? [];
		list.push(b);
		map.set(k, list);
	};
	for (const e of O) {
		if (e.key !== "" || counterpart.has(e.b)) continue;
		const sibs = e.parent ? kidsOf(e.parent) : oldBlocks;
		const k = sibs.indexOf(e.b);
		let done = false;
		for (let p = k - 1; p >= 0 && !done; p--) {
			const c = counterpart.get(sibs[p]);
			if (c) {
				put(after, c, e.b);
				done = true;
			}
		}
		for (let q = k + 1; q < sibs.length && !done; q++) {
			const c = counterpart.get(sibs[q]);
			if (c) {
				const par = freshParent.get(c);
				const lead = (stretchOf.get(e.b) ?? []).find(
					(f) => freshParent.get(f.b) === par,
				);
				put(before, lead?.b ?? c, e.b);
				done = true;
			}
		}
		if (!done) {
			// No sibling stands anywhere: by the old blocks around it in its
			// stretch. Between two, after the first new block; before them
			// all, before the new blocks; after them all, after.
			const pc = e.parent ? counterpart.get(e.parent) : undefined;
			const par = pc ?? null;
			const news = (stretchOf.get(e.b) ?? []).filter(
				(f) => freshParent.get(f.b) === par,
			);
			const real = (x: Block) => x !== e.b && ownKey(x) !== "";
			const prev = sibs.slice(0, k).some(real);
			const nextReal = sibs.slice(k + 1).some(real);
			if (news.length > 0 && prev)
				put(after, news[nextReal ? 0 : news.length - 1].b, e.b);
			else if (news.length > 0 && nextReal) put(before, news[0].b, e.b);
			else put(first, par, e.b);
		}
	}

	const same = (a: Block[], b: Block[]) =>
		a.length === b.length && a.every((x, k) => x === b[k]);
	const arrange = (parent: Block | null, list: Block[]): Block[] => {
		const out = [...(first.get(parent) ?? [])];
		for (const c of list) {
			out.push(...(before.get(c) ?? []), build(c), ...(after.get(c) ?? []));
		}
		return out;
	};
	const build = (f: Block): Block => {
		const r = role.get(f);
		if (r?.how === "whole") return r.old;
		const kids = arrange(f, kidsOf(f));
		if (r?.how === "keep") {
			if (same(kids, kidsOf(r.old))) return r.old;
			// A type Markdown has no form for may write its nested blocks
			// differently from the type it reads back as (a toggle indents a
			// paragraph, a bullet does not).
			if (
				r.old.type !== f.type &&
				render([{ ...r.old, children: kids }]) !==
					render([{ ...f, children: kids }])
			) {
				problems.push({
					block: r.old,
					why:
						markdownLoss(r.old, "paired") ??
						"Markdown writes its nested blocks differently",
					kind: "loss",
				});
			}
			return { ...r.old, children: kids };
		}
		if (r?.how === "pair") return { ...inherit(r.old, f), children: kids };
		return Array.isArray(f.children) ? { ...f, children: kids } : f;
	};
	return { blocks: arrange(null, fresh), problems };
}

// ── Final audit ───────────────────────────────────────────────────────────────
//
// Checked on the result of every edited range, whatever path built it: the
// alignment above should never produce what this refuses, but if it does,
// the edit is refused instead of written.

/** Block types Markdown writes (their text and Markdown props). */
const MARKDOWN_TYPES = new Set([
	"paragraph",
	"heading",
	"bulletListItem",
	"numberedListItem",
	"checkListItem",
	"codeBlock",
	"quote",
	"table",
]);
const MARKDOWN_STYLES = new Set(["bold", "italic", "code", "strike"]);

/** What of `b` Markdown cannot carry: a type it has no form for, props
 *  other than the ones it writes (colours, alignment and the like), inline
 *  nodes other than text and links (mention chips), text styles other than
 *  bold, italic, code and strike (underline, coloured text). Text aside. */
function uncarried(b: Block): {
	type: string | null;
	props: Record<string, unknown>;
	inline: string[];
} {
	const props = canonProps(b.props);
	for (const k of MARKDOWN_PROPS) delete props[k];
	const inline: string[] = [];
	const walk = (nodes: unknown) => {
		if (!Array.isArray(nodes)) return;
		for (const n of nodes as Block[]) {
			if (n?.type === "text") {
				for (const [k, v] of Object.entries(n.styles ?? {})) {
					if (v !== false && v !== undefined && !MARKDOWN_STYLES.has(k)) {
						inline.push(`${k}=${stable(v)}`);
					}
				}
			} else if (n?.type === "link") walk(n.content);
			else inline.push(stable(n));
		}
	};
	if (Array.isArray(b.content)) walk(b.content);
	else if (b.content?.type === "tableContent") {
		const tc = b.content;
		// Markdown writes a table's text, with one header row: not its
		// column widths, other header rows or columns, or cell formatting.
		if ((tc.columnWidths ?? []).some((w: unknown) => w != null)) {
			props["column widths"] = tc.columnWidths;
		}
		if ((tc.headerRows ?? 0) !== 1) props["header rows"] = tc.headerRows ?? 0;
		if (tc.headerCols) props["header columns"] = tc.headerCols;
		const cells: unknown[] = [];
		(tc.rows ?? []).forEach((r: Block, y: number) => {
			(r.cells ?? []).forEach((c: unknown, x: number) => {
				const cp = Array.isArray(c) ? {} : canonProps((c as Block)?.props);
				if (Object.keys(cp).length > 0) cells.push([y, x, cp]);
				walk(Array.isArray(c) ? c : (c as Block)?.content);
			});
		});
		if (cells.length > 0) props["cell formatting"] = cells;
	}
	return { type: MARKDOWN_TYPES.has(b.type) ? null : b.type, props, inline };
}

function carriesNothing(u: ReturnType<typeof uncarried>): boolean {
	return (
		u.type === null &&
		Object.keys(u.props).length === 0 &&
		u.inline.length === 0
	);
}

function describeUncarried(u: ReturnType<typeof uncarried>): string {
	const what = [
		...(u.type ? [`its ${u.type} type`] : []),
		...Object.keys(u.props),
		...new Set(
			u.inline.map((x) =>
				x.startsWith("{") ? JSON.parse(x).type : x.split("=")[0],
			),
		),
	];
	return what.join(", ");
}

/** A block's own Markdown, without its nested blocks. */
function ownKey(b: Block): string {
	let k = keys.own.get(b);
	if (k === undefined) {
		k = render([{ ...b, children: [] }]);
		keys.own.set(b, k);
	}
	return k;
}

/** A block's own text: its text runs (and link text) joined, or its own
 *  Markdown for a block without runs (a table). */
function ownText(b: Block): string {
	let k = keys.text.get(b);
	if (k === undefined) {
		if (Array.isArray(b.content)) {
			const parts: string[] = [];
			const walk = (nodes: Block[]) => {
				for (const n of nodes) {
					if (typeof n?.text === "string") parts.push(n.text);
					else if (Array.isArray(n?.content)) walk(n.content);
				}
			};
			walk(b.content);
			k = parts.join("");
		} else k = ownKey(b);
		keys.text.set(b, k);
	}
	return k;
}

/**
 * How much text `a` and `b` share: their common prefix plus their common
 * suffix (not overlapping it). Cheap, and enough to tell which new block
 * continues an old one: "Warning: x" shares 10 with "Warning: x2" and none
 * with "Note: y".
 */
function shared(a: string, b: string): number {
	const max = Math.min(a.length, b.length);
	let p = 0;
	while (p < max && a.charCodeAt(p) === b.charCodeAt(p)) p++;
	let q = 0;
	while (
		q < max - p &&
		a.charCodeAt(a.length - 1 - q) === b.charCodeAt(b.length - 1 - q)
	)
		q++;
	return p + q;
}

// ── Disguised reorders ────────────────────────────────────────────────────────
//
// A guard, not a guarantee: two blocks swapped and reworded can look like
// two in-place edits. When a block's new text shares more words with
// another edited block's old text than with its own, the edit is refused.
// A rewrite that shares no words with anything is, by definition, new
// content at that position, and applies. These checks only refuse; they
// never choose where data goes.

/** The words of `text`: lower-cased runs of letters and digits, counted. */
function wordsOf(text: string): Map<string, number> {
	let w = keys.words.get(text);
	if (!w) {
		w = new Map();
		for (const m of text.toLowerCase().matchAll(/[\p{L}\p{N}]+/gu)) {
			w.set(m[0], (w.get(m[0]) ?? 0) + 1);
		}
		keys.words.set(text, w);
	}
	return w;
}

/** How alike two texts are by their words (Dice: shared words, counted
 *  with repeats, over all words), from 0 to 1. */
function alike(a: string, b: string): number {
	const x = wordsOf(a);
	const y = wordsOf(b);
	let total = 0;
	for (const n of x.values()) total += n;
	for (const n of y.values()) total += n;
	if (total === 0) return 0;
	const [small, big] = x.size <= y.size ? [x, y] : [y, x];
	let both = 0;
	for (const [w, n] of small) both += Math.min(n, big.get(w) ?? 0);
	return (2 * both) / total;
}

/**
 * Among edited units (old and new text at each position), the first pair
 * where unit `k`'s new text is equal or closer to unit `j`'s old text than
 * to its own: [k, j], or null. Only units in `changed` take part.
 */
function closerToOther(
	was: string[],
	now: string[],
	changed: number[],
): [number, number] | null {
	for (const k of changed) {
		const own = alike(now[k], was[k]);
		for (const j of changed) {
			if (j === k) continue;
			if (now[k] === was[j] || alike(now[k], was[j]) > own) return [k, j];
		}
	}
	return null;
}

const TABLE_DATA = [
	"column widths",
	"header rows",
	"header columns",
	"cell formatting",
];

/**
 * Inside a table with formatting Markdown cannot write (column widths,
 * header settings, cell props), an edit may only change cell text in
 * place: the formatting stays by row and column index. Returns why the
 * edit from `o` to `n` could be a reorder of rows (or, when every row
 * changed, of columns), or null.
 */
function tableMoves(o: Block, n: Block): string | null {
	if (o.content?.type !== "tableContent" || n.content?.type !== "tableContent")
		return null;
	const props = uncarried(o).props;
	if (!TABLE_DATA.some((k) => k in props)) return null;
	const grid = (b: Block): string[][] =>
		(b.content.rows ?? []).map((r: Block) =>
			(r.cells ?? []).map((c: unknown) =>
				ownText({ content: Array.isArray(c) ? c : (c as Block)?.content }),
			),
		);
	const a = grid(o);
	const z = grid(n);
	const dims = (g: string[][]) => g.map((r) => r.length).join(",");
	if (dims(a) !== dims(z)) return null;
	const check = (was: string[], now: string[], what: string) => {
		const changed = now.flatMap((x, k) => (x !== was[k] ? [k] : []));
		if (changed.length < 2) return null;
		const hit = closerToOther(was, now, changed);
		return hit
			? `the edit changes its ${what}s ${hit[0] + 1} and ${hit[1] + 1} so that one reads like the other did, which could move its formatting to another ${what}`
			: null;
	};
	const sep = "\u0000";
	const rows = check(
		a.map((r) => r.join(sep)),
		z.map((r) => r.join(sep)),
		"row",
	);
	if (rows) return rows;
	if (a.some((r, y) => r.join(sep) === z[y].join(sep))) return null;
	const cols = (g: string[][]) =>
		(g[0] ?? []).map((_, x) => g.map((r) => r[x]).join(sep));
	return check(cols(a), cols(z), "column");
}

/** The blocks of `blocks` (any depth) that hold data Markdown cannot write,
 *  or hold a nested block that does. */
function withData(blocks: Block[]): Set<Block> {
	const out = new Set<Block>();
	const walk = (b: Block): boolean => {
		let has = carries(b);
		for (const k of kidsOf(b)) if (walk(k)) has = true;
		if (has) out.add(b);
		return has;
	};
	for (const b of blocks) walk(b);
	return out;
}

/** A block of a range with where it is: its parent, its place in document
 *  order, and its path of sibling indices counting only blocks that show
 *  something ("" for a block that shows nothing). */
interface Place {
	b: Block;
	parent: Block | null;
	order: number;
	path: string;
}

function places(blocks: Block[]): Place[] {
	const out: Place[] = [];
	const walk = (bs: Block[], parent: Block | null, prefix: string) => {
		let k = 0;
		for (const b of bs) {
			const path = blank(b) ? "" : `${prefix}${k++}`;
			out.push({ b, parent, order: out.length, path });
			walk(kidsOf(b), b, path === "" ? `${prefix}~` : `${path}.`);
		}
	};
	walk(blocks, null, "");
	return out;
}

/**
 * R1, checked on the result: inside an edited range, every old block that
 * holds data Markdown cannot write (or holds a nested block that does,
 * `deep`) has one of these outcomes, else the edit is refused:
 * (a) kept: a result block with its id has its own part unchanged (type,
 *     props, text), under the block that stands for its old parent, and
 *     all kept blocks of the range, with data or not, keep their order;
 * (b) edited in place: the range maps one to one by position (the same
 *     blocks at the same paths, by id, blanks aside), and no block's new
 *     text holds the text of another block that left its place (a reorder
 *     rewritten as edits);
 * (c) deleted: its id is gone and its own Markdown is unique in the range.
 * And no block is deleted while a block with data reads the same: the edit
 * could mean either. Text similarity decides nothing here.
 */
function staysInPlace(
	oldBlocks: Block[],
	newBlocks: Block[],
	deep: Set<Block>,
	oldById: Map<string, Block>,
): { block: Block; why: string; smaller: true; pair?: Block } | null {
	const O = places(oldBlocks);
	const N = places(newBlocks);
	const oldPlace = new Map(O.map((p) => [p.b, p]));
	const oldOf = (n: Block | null): Block | null | undefined => {
		if (n === null) return null;
		if (oldPlace.has(n)) return n;
		return typeof n.id === "string" ? oldById.get(n.id) : undefined;
	};
	const newOf = new Map<Block, Place>();
	for (const p of N) {
		const o = oldOf(p.b);
		if (o) newOf.set(o, p);
	}
	const kept = (o: Block, n: Block) =>
		o === n ||
		(o.type === n.type &&
			dataOf(o) === dataOf(n) &&
			(canonOwn(o) === canonOwn(n) || ownKey(o) === ownKey(n)));
	const firstData = O.find((p) => deep.has(p.b))?.b as Block;
	const has = (d: Block) => {
		const u = uncarried(d);
		if (!carriesNothing(u))
			return `it has ${describeUncarried(u)}, which Markdown cannot show,`;
		const kid = subtree(d).find((x) => x !== d && carries(x)) as Block;
		return `it holds ${aBlock(kid.type)} with ${describeUncarried(uncarried(kid))}, which Markdown cannot show,`;
	};
	const refuse = (d: Block, what: string) => ({
		block: d,
		why: `${has(d)} and the edit ${what}`,
		smaller: true as const,
	});

	// (a)'s order: kept blocks, in the result's order, in the old order.
	let last: Place | null = null;
	for (const p of N) {
		const o = oldOf(p.b);
		if (!o || !kept(o, p.b)) continue;
		const op = oldPlace.get(o) as Place;
		if (last && op.order < last.order) {
			const d = deep.has(o) ? o : deep.has(last.b) ? last.b : firstData;
			return refuse(d, "would change the order of the blocks around it");
		}
		last = op;
	}

	// (b): one to one by position.
	const shownO = O.filter((p) => p.path !== "");
	const shownN = N.filter((p) => p.path !== "");
	const byPos =
		shownO.length === shownN.length &&
		shownO.every(
			(p, k) => shownN[k].path === p.path && oldOf(shownN[k].b) === p.b,
		);
	if (byPos) {
		const texts = shownO.map((p) => ownText(p.b));
		const now = shownN.map((p) => ownText(p.b));
		const left = texts.map((x, k) => x !== "" && !now[k].includes(x));
		for (let k = 0; k < now.length && byPos; k++) {
			if (!left[k]) continue;
			const q = texts.findIndex(
				(x, j) => j !== k && left[j] && now[k].includes(x),
			);
			if (q >= 0) {
				const d = deep.has(shownO[k].b)
					? shownO[k].b
					: deep.has(shownO[q].b)
						? shownO[q].b
						: firstData;
				return refuse(d, "moves text from one block to another");
			}
		}
		// Disguised reorders (see closerToOther): when two or more blocks
		// are edited in place and one holds data, no edited block's new text
		// may read more like another edited block's old text than its own,
		// and no block's new text may read more like a data block's old text
		// than the data block's own new text does.
		const changed = now.flatMap((x, k) => (x !== texts[k] ? [k] : []));
		if (changed.length >= 2 && changed.some((k) => deep.has(shownO[k].b))) {
			for (const k of changed) {
				if (!deep.has(shownO[k].b)) continue;
				const own = alike(now[k], texts[k]);
				for (const j of changed) {
					if (j === k) continue;
					if (alike(now[k], texts[j]) > own || alike(now[j], texts[k]) > own) {
						return {
							...refuse(
								shownO[k].b,
								"rewrites it and another block so that one reads more like the other's old text than its own, which could be a reorder",
							),
							pair: shownO[j].b,
						};
					}
				}
			}
		}
	}

	const count = new Map<string, number>();
	for (const p of O) {
		const k = ownKey(p.b);
		count.set(k, (count.get(k) ?? 0) + 1);
	}

	for (const op of O) {
		const o = op.b;
		const np = newOf.get(o);
		if (!np) {
			// Deleted: not while a block with data reads the same.
			const twin = [...deep].find((d) => d !== o && ownKey(d) === ownKey(o));
			if (twin && ownKey(o) !== "") {
				return refuse(
					twin,
					"deletes a block that reads the same, so edit_doc cannot tell which one goes",
				);
			}
			if (
				deep.has(o) &&
				(ownKey(o) === "" || (count.get(ownKey(o)) ?? 0) > 1)
			) {
				return refuse(o, "deletes it or a block that reads the same");
			}
			continue;
		}
		if (!deep.has(o)) continue;
		if (kept(o, np.b)) {
			if (oldOf(np.parent) !== op.parent) {
				return refuse(o, "would move it under another block");
			}
			continue;
		}
		if (byPos && np.path === op.path) continue;
		return refuse(
			o,
			"splits, merges, moves or rewrites the blocks around it, so edit_doc cannot tell which new block it is",
		);
	}
	return null;
}

/**
 * Checks the blocks an edit produced (`newBlocks`) against the ones it
 * replaced (`oldBlocks`), at every depth. Returns the first old block the
 * result mistreats, with why, or null. The rule:
 * 1. no id appears twice in the result;
 * 2. an old block that holds anything Markdown cannot carry (see
 *    uncarried) and is not returned as the very object read:
 *    a. when its id is in the result, that block has its type and the same
 *       uncarried data (its text may differ: edited in place);
 *    b. when its own Markdown is in the result, a block with that Markdown
 *       has its id (its content did not move to another block);
 *    c. when neither, the result has no new block (one with an id not
 *       read): it was deleted, not rewritten;
 *    d. and when its id is gone, its text is not inside the text of a
 *       block the edit wrote (it was merged there);
 * 3. a result block with an old block's id but other Markdown does not
 *    hold the Markdown of a different old block whose id is not on it;
 * 4. a result block with an old block's id but other text continues that
 *    text (see shared) at least as well as any new block does, and at
 *    least as well as it continues the text of any other old block not
 *    returned as read;
 * 5. old blocks with the same own Markdown keep their order: none moves
 *    past another (an indent nests the second under the first, not the
 *    other way round).
 */
export function auditEdit(
	oldBlocks: Block[],
	newBlocks: Block[],
): { block: Block; why: string; smaller?: boolean; pair?: Block } | null {
	const olds = oldBlocks.flatMap(subtree);
	const news = newBlocks.flatMap(subtree);
	const returned = new Set(news);
	const oldSet = new Set(olds);
	const oldById = new Map<string, Block>();
	for (const o of olds) if (typeof o.id === "string") oldById.set(o.id, o);
	const byId = new Map<string, Block>();
	for (const n of news) {
		if (typeof n.id !== "string") continue;
		if (byId.has(n.id)) {
			return {
				block: oldById.get(n.id) ?? n,
				why: "its id would appear twice",
			};
		}
		byId.set(n.id, n);
	}
	const newKeys = news.map(ownKey);
	const hasNew = news.some(
		(n) => typeof n.id !== "string" || !oldById.has(n.id),
	);
	const holds = (id: unknown, key: string) =>
		news.some((x, k) => x.id === id && newKeys[k] === key);

	for (const o of olds) {
		if (returned.has(o)) continue;
		const u = uncarried(o);
		if (carriesNothing(u)) continue;
		const key = ownKey(o);
		const n = typeof o.id === "string" ? byId.get(o.id) : undefined;
		if (n) {
			if (n.type !== o.type) {
				return {
					block: o,
					why: `it would become ${aBlock(n.type)} and lose its ${describeUncarried(u)}`,
				};
			}
			if (stable(uncarried(n)) !== stable(u)) {
				return { block: o, why: `its ${describeUncarried(u)} would be lost` };
			}
		}
		if (newKeys.includes(key) && !holds(o.id, key)) {
			return {
				block: o,
				why: `its content would move to another block without its ${describeUncarried(u)}`,
			};
		}
		if (!n && !newKeys.includes(key) && hasNew) {
			return {
				block: o,
				why: `it would be replaced by new blocks without its ${describeUncarried(u)}`,
			};
		}
		const text = ownText(o);
		if (
			!n &&
			text.trim() !== "" &&
			news.some(
				(x) =>
					!oldSet.has(x) &&
					ownText(x).includes(text) &&
					!(
						u.type === null &&
						u.inline.length === 0 &&
						stable(uncarried(x).props) === stable(u.props)
					),
			)
		) {
			return {
				block: o,
				why: `its text would move into another block without its ${describeUncarried(u)}`,
			};
		}
	}

	// Tables with formatting: cell text changes in place only.
	for (const o of olds) {
		if (o.type !== "table" || returned.has(o) || typeof o.id !== "string")
			continue;
		const n = byId.get(o.id);
		const why = n && tableMoves(o, n);
		if (why) return { block: o, why, smaller: true };
	}

	const deep = withData(oldBlocks);
	if (deep.size > 0) {
		const bad = staysInPlace(oldBlocks, newBlocks, deep, oldById);
		if (bad) return bad;
	}

	for (let k = 0; k < news.length; k++) {
		const n = news[k];
		const o = typeof n.id === "string" ? oldById.get(n.id) : undefined;
		// A block returned as the very object read is as it was; ids of
		// blocks without data may move (R2).
		if (!o || o === n || !deep.has(o) || newKeys[k] === ownKey(o)) continue;
		const from = olds.find(
			(p) => p !== o && ownKey(p) === newKeys[k] && !holds(p.id, newKeys[k]),
		);
		if (from) {
			return {
				block: o,
				why: "its id would end up on text that came from another block",
			};
		}
	}

	// 4. An id stays with the text it was on.
	const fresh = news.filter(
		(n) => typeof n.id !== "string" || !oldById.has(n.id),
	);
	const others = olds.filter((p) => !returned.has(p));
	for (const n of news) {
		const o = typeof n.id === "string" ? oldById.get(n.id) : undefined;
		if (!o || o === n || !deep.has(o)) continue;
		const text = ownText(n);
		const was = ownText(o);
		if (text === was) continue;
		const mine = shared(text, was);
		if (fresh.some((f) => shared(ownText(f), was) > mine)) {
			return {
				block: o,
				why: "its id would stay on text that does not continue it, while a new block does",
			};
		}
		if (others.some((p) => p !== o && shared(text, ownText(p)) > mine)) {
			return {
				block: o,
				why: "its id would end up on text that came from another block",
			};
		}
	}

	// 5. Blocks that read the same keep their order (in a range with data).
	if (deep.size === 0) return null;
	const at = new Map<unknown, number>();
	news.forEach((n, k) => {
		if (typeof n.id === "string") at.set(n.id, k);
	});
	const last = new Map<string, number>();
	for (const o of olds) {
		const key = ownKey(o);
		const k = at.get(o.id);
		if (key === "" || k === undefined) continue;
		if (k < (last.get(key) ?? -1)) {
			return {
				block: o,
				why: "it and another block that reads the same in Markdown would swap places",
			};
		}
		last.set(key, k);
	}
	return null;
}

/**
 * Replaces `oldString` with `newString` in the document's Markdown and
 * returns the new top-level blocks. Only the blocks the matches touch are
 * re-parsed from Markdown (a list as a whole, as Markdown writes it as one);
 * every other block in the result is the same object as in `blocks`.
 *
 * Throws a SpliceError when `oldString` is empty, is not found, or is found
 * more than once without `replaceAll`, and when a block the edit would
 * rewrite has something Markdown cannot carry ("unsafe"): nothing is
 * changed then.
 */
export function spliceEdit(
	blocks: Block[],
	oldString: string,
	newString: string,
	replaceAll: boolean,
): { blocks: Block[]; replaced: number; lines: number[] } {
	keys = newKeys();
	if (oldString === "") {
		throw new SpliceError("old_string must not be empty.", "empty");
	}
	const renders = blocks.map(subKey);
	const full = render(blocks);
	const found = occurrences(full, oldString);
	const lines = found.map((at) => lineAt(full, at));
	if (found.length === 0) {
		throw new SpliceError(
			"old_string was not found in the document. It must match the Markdown read_doc returns exactly, including spaces and line breaks.",
			"not_found",
		);
	}
	if (found.length > 1 && !replaceAll) {
		throw new SpliceError(
			`old_string was found ${found.length} times (starting on lines ${lines.join(", ")}). Add surrounding text to make it unique, or pass replaceAll.`,
			"ambiguous",
			lines,
		);
	}

	const units = unitsOf(blocks, renders, full);
	const starts = blockStarts(renders, units, full);

	// Each match touches a run of units; overlapping runs merge.
	const ranges: Array<{ ua: number; ub: number; matches: number[] }> = [];
	for (const ms of found) {
		const me = ms + oldString.length;
		let ua = -1;
		let ub = -1;
		units.forEach((u, k) => {
			if (!touches(u, ms, me)) return;
			if (ua === -1) ua = k;
			ub = k;
		});
		const last = ranges[ranges.length - 1];
		if (last && ua <= last.ub) {
			last.ub = Math.max(last.ub, ub);
			last.matches.push(ms);
		} else {
			ranges.push({ ua, ub, matches: [ms] });
		}
	}

	const out: Block[] = [];
	let next = 0; // the first block not yet copied
	for (const { ua, ub, matches } of ranges) {
		const from = units[ua].from;
		const to = units[ub].to;
		const start = units[ua].start;
		let text = "";
		let at = start;
		for (const ms of matches) {
			text += full.slice(at, ms) + newString;
			at = ms + oldString.length;
		}
		text += full.slice(at, units[ub].end);

		const fresh = fixSoftBreaks(markdownToBlocknote(text));
		const aligned = align(blocks.slice(from, to), fresh);
		// The line where block `idx` starts in the full Markdown.
		const lineOf = (idx: number) => {
			if (starts[idx] >= 0) return lineAt(full, starts[idx]);
			const unit = units.find((u) => u.from <= idx && idx < u.to) ?? units[ua];
			const first = renders[idx].split("\n")[0];
			const at = first ? full.indexOf(first, unit.start) : -1;
			return lineAt(full, at >= 0 ? at : unit.start + (unit.start > 0 ? 1 : 0));
		};
		// Every block of the range, at any depth, with the top-level block
		// it is in (its line names it).
		const topOf = new Map<Block, number>();
		for (let k = from; k < to; k++) {
			for (const b of subtree(blocks[k])) topOf.set(b, k);
		}
		// A nested block's line: its own first line, looked for in order
		// from where its top-level block starts.
		const nestedLines = new Map<Block, number>();
		const lineOfBlock = (b: Block) => {
			const k = topOf.get(b) ?? from;
			if (blocks[k] === b) return lineOf(k);
			if (!nestedLines.has(b)) {
				const unit = units.find((u) => u.from <= k && k < u.to) ?? units[ua];
				let cursor = starts[k] >= 0 ? starts[k] : unit.start;
				for (const x of subtree(blocks[k])) {
					// Without its list marker: alone, a nested numbered item
					// renders "1. dup" where the document writes "2. dup".
					const first = ownKey(x)
						.split("\n")[0]
						.replace(LIST_MARKER, "")
						.trim();
					const found = first ? lineStartOf(full, first, cursor) : -1;
					if (found < 0) continue;
					nestedLines.set(x, lineAt(full, found));
					cursor = found + first.length;
				}
			}
			return nestedLines.get(b) ?? lineOf(k);
		};
		const named = (bs: Block[]) => {
			const lines = [...new Set(bs.map(lineOfBlock))].sort((x, y) => x - y);
			return {
				lines,
				text:
					lines.length === 1
						? `line ${lines[0]}`
						: `lines ${lines.slice(0, -1).join(", ")} and ${lines[lines.length - 1]}`,
			};
		};
		const where = (b: Block) => {
			const k = topOf.get(b) ?? from;
			return blocks[k] === b
				? `the ${b.type} block on line ${lineOf(k)}`
				: `a ${b.type} block nested in the block on line ${lineOf(k)}`;
		};
		for (const p of aligned.problems) {
			if (p.kind === "differ") {
				const { lines, text } = named(p.with ?? [p.block]);
				throw new SpliceError(
					`The edit merges, splits or retypes the blocks on ${text}, which edit_doc cannot rewrite from Markdown: they have different colours or alignment, so the new blocks cannot keep them. Nothing was changed. Edit those blocks in the web editor, or rewrite the document with write_doc if losing that is acceptable.`,
					"unsafe",
					lines,
				);
			}
			if (p.kind === "ambiguous") {
				const all = [p.block, ...(p.with ?? [])];
				const marked = all.find((b) => carries(b)) ?? p.block;
				const what = describeUncarried(uncarried(marked)) || "type";
				if (p.why === "split") {
					const line = lineOf(topOf.get(p.block) ?? from);
					throw new SpliceError(
						`The edit splits ${where(p.block)} into blocks that continue its text equally, so edit_doc cannot tell which one keeps its ${what}. Nothing was changed. Split it unevenly, edit it in the web editor, or rewrite that part with write_doc.`,
						"unsafe",
						[line],
					);
				}
				const { lines, text } = named(all);
				throw new SpliceError(
					`The edit could mean more than one of the blocks on ${text}: they read the same in Markdown but differ in ${what}, so edit_doc cannot tell which one the edit keeps. Nothing was changed. Make that change in the web editor, or rewrite that part with write_doc.`,
					"unsafe",
					lines,
				);
			}
			const k = topOf.get(p.block) ?? from;
			const line = lineOf(k);
			const why =
				blocks[k] === p.block
					? p.why
					: `in ${aBlock(p.block.type)} nested in it, ${p.why}`;
			throw new SpliceError(
				`The edit touches the ${blocks[k].type} block on line ${line}, which edit_doc cannot rewrite from Markdown: ${why}. Nothing was changed. Edit that block in the web editor, or rewrite the document with write_doc if losing that is acceptable.`,
				"unsafe",
				[line],
			);
		}
		// The result must say what the edit asks for: its Markdown is that
		// of the edited text parsed as a whole (blocks that render to nothing
		// aside), and differs from what was there.
		const shown = (bs: Block[]) => render(withoutBlank(bs));
		const got = shown(aligned.blocks);
		if (got === shown(blocks.slice(from, to))) {
			throw new SpliceError(
				`The edit changes nothing in the document as Markdown shows it (starting on line ${lineAt(full, matches[0])}), so nothing was changed. Markdown does not show some differences, such as extra spaces; edit the text that read_doc returns.`,
				"no_change",
				[lineAt(full, matches[0])],
			);
		}
		if (got !== shown(fresh)) {
			throw new SpliceError(
				`edit_doc could not apply the edit starting on line ${lineAt(full, matches[0])} exactly as written, so nothing was changed. Rewrite that part with write_doc, or edit it in the web editor.`,
				"unsafe",
				[lineAt(full, matches[0])],
			);
		}
		const bad = auditEdit(blocks.slice(from, to), aligned.blocks);
		if (bad?.pair) {
			const { lines, text } = named([bad.block, bad.pair]);
			const what = describeUncarried(uncarried(bad.block)) || "data";
			throw new SpliceError(
				`The edit rewrites the blocks on ${text} so that one reads more like the other's old text than its own, so edit_doc cannot tell it from a reorder, and the block on line ${lineOfBlock(bad.block)} has ${what}, which Markdown cannot show. Nothing was changed. Edit the blocks one at a time, or move them in the web editor.`,
				"unsafe",
				lines,
			);
		}
		if (bad) {
			const line = lineOf(topOf.get(bad.block) ?? from);
			throw new SpliceError(
				bad.smaller
					? `The edit touches ${where(bad.block)}, which edit_doc cannot rewrite from Markdown: ${bad.why}. Nothing was changed. Make the change in smaller edits (change text in place, and add, delete or move plain blocks in edits of their own), or in the web editor.`
					: `The edit touches ${where(bad.block)}, which edit_doc cannot rewrite from Markdown: ${bad.why}. Nothing was changed. Edit that block in the web editor, or rewrite the document with write_doc if losing that is acceptable.`,
				"unsafe",
				[line],
			);
		}
		out.push(...blocks.slice(next, from));
		out.push(...aligned.blocks);
		next = to;
	}
	out.push(...blocks.slice(next));
	// Each range was parsed on its own, so an edit that changes how
	// Markdown reads the blocks after it (it leaves a code fence open, say)
	// would apply as if they were not there: the document as a whole must
	// read as the edited Markdown does.
	let edited = "";
	let at = 0;
	for (const ms of found) {
		edited += full.slice(at, ms) + newString;
		at = ms + oldString.length;
	}
	edited += full.slice(at);
	const reread = (md: string) =>
		fixSoftBreaks(markdownToBlocknote(md))
			.filter((b) => !blank(b))
			.map(subKey)
			.join("\n");
	const now = render(out);
	if (now !== edited && reread(now) !== reread(edited)) {
		throw new SpliceError(
			`The edit starting on line ${lines[0]} changes how Markdown reads the blocks after it (it leaves a code fence open, for example), so nothing was changed. Close what the edit opens, or rewrite that part with write_doc.`,
			"unsafe",
			[lines[0]],
		);
	}
	return { blocks: out, replaced: found.length, lines };
}

// ── edit_doc ──────────────────────────────────────────────────────────────────

/** A document as read for an edit, with the If-Match value of that read
 *  (its ETag or content hash; absent from a server older than both). */
export interface LoadedDoc {
	doc: Document;
	tag?: string;
	/** How the reply names the document (its path, or its title). */
	label: string;
}

export interface EditDeps {
	/** Reads the document (again, on a retry). */
	load(): Promise<LoadedDoc | { error: string }>;
	/** Writes the blocks with If-Match `tag`; a 412 or 409 is thrown as a
	 *  PacaAPIError. */
	write(
		doc: Document,
		blocks: Block[],
		tag: string | undefined,
	): Promise<unknown>;
	/** The display name of a user id, when it can be found. */
	writerName?(userId: string): Promise<string | undefined>;
}

export interface EditRequest {
	oldString: string;
	newString: string;
	replaceAll: boolean;
}

function toolText(text: string, isError = false) {
	return isError
		? { content: [{ type: "text", text }], isError: true }
		: { content: [{ type: "text", text }] };
}

function lockedMessage(doc: Document): string {
	const src = doc.source;
	const where = src
		? `a locked copy of ${src.path} in ${src.repository}`
		: "a locked managed copy of a repository file";
	return `This document is ${where}. Edit the file in the repository and push it; edit_doc cannot change it.`;
}

/** "Last change: by …, at …; note: …; client: …; task: …." from 412 details. */
async function describeWriter(
	details: Record<string, string>,
	deps: EditDeps,
): Promise<string> {
	const parts: string[] = [];
	if (details.updated_by) {
		const name = await deps
			.writerName?.(details.updated_by)
			.catch(() => undefined);
		parts.push(
			`by ${name ? `${name} (${details.updated_by})` : details.updated_by}`,
		);
	}
	if (details.updated_at) parts.push(`at ${details.updated_at}`);
	const what: string[] = [];
	if (details.note) what.push(`note: "${details.note}"`);
	if (details.client) what.push(`client: ${details.client}`);
	if (details.task) what.push(`task: ${details.task}`);
	if (parts.length === 0 && what.length === 0) {
		return "The server did not say who changed it.";
	}
	return `Last change ${[parts.join(" "), ...what].filter(Boolean).join("; ")}.`;
}

/**
 * Runs edit_doc: read, splice, write with If-Match. A 412 re-reads and
 * re-applies the edit once; a second 412, or `old_string` gone after the
 * re-read, is a tool error naming the other writer. A locked managed copy
 * is refused before any write, and a 409 DOC_LOCKED from the write (the
 * copy was locked since the read) is not retried.
 */
export async function editDocument(deps: EditDeps, edit: EditRequest) {
	if (edit.oldString === edit.newString) {
		return toolText(
			"old_string and new_string are the same, so nothing changed and nothing was written.",
		);
	}
	let conflict: Record<string, string> | undefined;
	for (let attempt = 1; attempt <= 2; attempt++) {
		const loaded = await deps.load();
		if ("error" in loaded) return toolText(loaded.error, true);
		const { doc, tag, label } = loaded;
		if (doc.source_locked) return toolText(lockedMessage(doc), true);

		let spliced: ReturnType<typeof spliceEdit>;
		try {
			spliced = spliceEdit(
				(doc.content ?? []) as Block[],
				edit.oldString,
				edit.newString,
				edit.replaceAll,
			);
		} catch (err) {
			if (!(err instanceof SpliceError)) throw err;
			if (conflict && err.kind === "not_found") {
				return toolText(
					`old_string is no longer in the document: someone changed it after you read it, and nothing was written. ${await describeWriter(conflict, deps)} Read the section again with read_doc and decide how to redo the edit.`,
					true,
				);
			}
			if (conflict) {
				return toolText(
					`Someone changed the document after you read it, and in their version: ${err.message} Nothing was written. ${await describeWriter(conflict, deps)} Read the section again with read_doc and redo the edit.`,
					true,
				);
			}
			return toolText(err.message, true);
		}

		try {
			await deps.write(doc, spliced.blocks, tag);
		} catch (err) {
			if (err instanceof PacaAPIError && err.status === 412) {
				if (attempt === 1) {
					conflict = err.details ?? {};
					continue;
				}
				return toolText(
					`The document changed again while the edit was being re-applied, so nothing was written. ${await describeWriter(err.details ?? {}, deps)} Read the section again with read_doc and redo the edit.`,
					true,
				);
			}
			if (
				err instanceof PacaAPIError &&
				err.status === 409 &&
				err.code === "DOC_LOCKED"
			) {
				// Locked since the read: read it again for the repository file.
				const fresh = await deps.load().catch(() => null);
				const current = fresh && !("error" in fresh) ? fresh.doc : doc;
				return toolText(lockedMessage(current), true);
			}
			throw err;
		}

		const n = spliced.replaced;
		const lines = [...new Set(spliced.lines)];
		const out = [
			`Edited ${label}: replaced ${n} occurrence${n === 1 ? "" : "s"} (line${lines.length === 1 ? "" : "s"} ${lines.join(", ")} as read).`,
		];
		if (conflict) {
			out.push(
				`The document changed after it was read, so the edit was applied to the newer version. ${await describeWriter(conflict, deps)}`,
			);
		}
		if (doc.source) {
			out.push(
				`This document is a copy of ${doc.source.path} in ${doc.source.repository}: the next push from the repository overwrites this edit unless it is merged into the file.`,
			);
		}
		return toolText(out.join("\n"));
	}
	// Not reached: the second attempt returns or throws.
	throw new Error("edit_doc: retry loop ended without a result");
}
