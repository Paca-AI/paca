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

/** How a replaced block's props reach what replaces it:
 *  "paired": one block of its type stands in for it, inheriting its id and
 *  the props Markdown cannot carry (colours, alignment);
 *  "carried": the blocks of a merge or split take its colours and alignment. */
type Carry = "paired" | "carried";

/** Why `b` cannot be rewritten from Markdown, or null when it can. */
function markdownLoss(b: Block, carry: Carry): string | null {
	const parsed = fixSoftBreaks(markdownToBlocknote(render([b])));
	let back: Block[] = parsed;
	if (carry === "paired" && parsed.length === 1 && parsed[0].type === b.type) {
		try {
			back = [inherit(b, parsed[0])];
		} catch (err) {
			if (err instanceof NestedUnsafe) return `a nested block: ${err.loss}`;
			throw err;
		}
	}
	if (back.length !== 1) {
		return Array.isArray(b.children) && b.children.length > 0
			? "its nested blocks would become separate top-level blocks"
			: `Markdown has no exact form for ${aBlock(b.type)}`;
	}
	const a = canonBlock(b, true) as Block;
	const z = canonBlock(back[0], true) as Block;
	if (carry === "carried") {
		for (const k of Object.keys(DEFAULT_PROPS)) {
			delete a.props[k];
			delete z.props[k];
		}
	}
	if (a.type !== z.type) return `it would become ${aBlock(z.type)}`;
	if (stable(a.children) !== stable(z.children)) {
		return "its nested blocks would change (their type, colours or other properties)";
	}
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

/**
 * How old blocks map onto parsed ones while one edit is aligned, at every
 * depth: `moves` takes a parsed block to the old block it stands for (its
 * render is the same, so the old object is returned as it was), `used`
 * holds the old blocks taken that way, so none is used twice.
 */
interface AlignCtx {
	moves: Map<Block, Block>;
	used: Set<Block>;
}

const newCtx = (): AlignCtx => ({ moves: new Map(), used: new Set() });

/** A nested block the edit would rewrite with a loss (see markdownLoss). */
class NestedUnsafe extends Error {
	constructor(
		readonly loss: string,
		readonly blockType: string,
	) {
		super(loss);
	}
}

function subtree(b: Block): Block[] {
	return [b, ...(Array.isArray(b.children) ? b.children : []).flatMap(subtree)];
}

/** `b` with every parsed block that stands for an old one replaced by it. */
function materialize(b: Block, ctx: AlignCtx): Block {
	const old = ctx.moves.get(b);
	if (old) return old;
	if (!Array.isArray(b.children) || b.children.length === 0) return b;
	return { ...b, children: b.children.map((c: Block) => materialize(c, ctx)) };
}

/** Props Markdown writes (a list's start number, a check, a heading level,
 *  a code block's language): an edit's new Markdown decides them, also
 *  when the parser leaves one out at its default value (start 1). */
const MARKDOWN_PROPS = new Set(["start", "checked", "level", "language"]);

/**
 * `fresh` (parsed from Markdown) standing in for `old`: it keeps old's id
 * (or lack of one) and the props Markdown cannot carry, such as colours;
 * the props Markdown writes come from `fresh`.
 */
function inherit(old: Block, fresh: Block, ctx: AlignCtx = newCtx()): Block {
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
	if (Array.isArray(fresh.children)) {
		const oc: Block[] = Array.isArray(old.children) ? old.children : [];
		out.children = align(
			oc,
			oc.map((c) => render([c])),
			fresh.children,
			ctx,
			true,
		).blocks;
	}
	if ("id" in old) out.id = old.id;
	else delete out.id;
	return out;
}

/** What happened to an old block the edit replaced. */
type Replaced =
	| { i: number; how: "deleted" | Carry }
	/** Old blocks of a merge or split whose colours or alignment differ;
	 *  `group`: all of that stretch's old blocks. */
	| { i: number; how: "differ"; group: number[] }
	/** A block with colours or alignment that becomes blocks of `to` types. */
	| { i: number; how: "retype"; to: string[] }
	/** A nested block of this one (of `type`) would be rewritten with `loss`. */
	| { i: number; how: "nested"; type: string; loss: string };

/** The colours and alignment of `b` that differ from the defaults. */
function ownProps(b: Block): Record<string, unknown> {
	const out: Record<string, unknown> = {};
	for (const k of Object.keys(DEFAULT_PROPS)) {
		const v = b.props?.[k];
		if (v !== undefined && v !== DEFAULT_PROPS[k]) out[k] = v;
	}
	return out;
}

/** `fresh` with `props` set where it has the default value. */
function withProps(fresh: Block, props: Record<string, unknown>): Block {
	const own = { ...(fresh.props ?? {}) };
	let changed = false;
	for (const [k, v] of Object.entries(props)) {
		if (k in own && own[k] === DEFAULT_PROPS[k]) {
			own[k] = v;
			changed = true;
		}
	}
	return changed ? { ...fresh, props: own } : fresh;
}

/** Why the edit cannot rewrite old block `oldBlocks[r.i]`, or null. */
function lossOf(r: Replaced, oldBlocks: Block[]): string | null {
	if (r.how === "deleted") return null;
	if (r.how === "differ") {
		return "it and the blocks merged or split with it have different colours or alignment, so the new blocks cannot keep them";
	}
	if (r.how === "retype") {
		return `it would become ${r.to.join(", ")} blocks, which cannot keep its colour or alignment`;
	}
	if (r.how === "nested") return `in ${aBlock(r.type)} nested in it, ${r.loss}`;
	return markdownLoss(oldBlocks[r.i], r.how);
}

/**
 * Replaces `oldBlocks` with `fresh` (parsed from the edited Markdown):
 * - an old block whose render is unchanged is kept as the very object read
 *   (longest common subsequence of renders, so duplicates pair in order);
 * - an old block whose render reappears elsewhere among the changed blocks,
 *   at any depth (moved, swapped, indented, outdented), is that object
 *   there; duplicates pair in document order;
 * - the rest pair by position: as many new blocks as old ones, of the same
 *   types, each stands in for the old one (same id, colours, alignment);
 *   otherwise (a merge, split or change of type) the new blocks are new
 *   and take the old colours and alignment only where they all agree;
 * - nested blocks are aligned the same way, recursively;
 * - old blocks that render to nothing (an empty paragraph) always stay: at
 *   their place among blocks paired one to one; in a merge or split, before
 *   the new blocks when they were before the old ones, after the first new
 *   block when they were between old ones, and after the new blocks when
 *   they were after.
 * `nested`: a loss in a replaced block throws NestedUnsafe (the caller
 * names the top-level block); at the top level the losses are returned.
 */
function align(
	oldBlocks: Block[],
	oldKeys: string[],
	fresh: Block[],
	ctx: AlignCtx = newCtx(),
	nested = false,
): { blocks: Block[]; replaced: Replaced[] } {
	const newKeys = fresh.map((b) => render([b]));
	const n = oldBlocks.length;
	const m = fresh.length;
	// A pair the move map already fixed must stay that pair.
	const eq = (i: number, j: number) => {
		if (oldKeys[i] === "" || oldKeys[i] !== newKeys[j]) return false;
		const was = ctx.moves.get(fresh[j]);
		if (was) return was === oldBlocks[i];
		return !ctx.used.has(oldBlocks[i]);
	};
	const lcs: number[][] = Array.from({ length: n + 1 }, () =>
		new Array<number>(m + 1).fill(0),
	);
	for (let i = n - 1; i >= 0; i--) {
		for (let j = m - 1; j >= 0; j--) {
			lcs[i][j] = eq(i, j)
				? lcs[i + 1][j + 1] + 1
				: Math.max(lcs[i + 1][j], lcs[i][j + 1]);
		}
	}

	type Op =
		| { op: "keep"; i: number; j: number }
		| { op: "del"; i: number }
		| { op: "ins"; j: number };
	const ops: Op[] = [];
	let i = 0;
	let j = 0;
	while (i < n || j < m) {
		if (i < n && j < m && eq(i, j) && lcs[i][j] === lcs[i + 1][j + 1] + 1) {
			ops.push({ op: "keep", i, j });
			ctx.moves.set(fresh[j], oldBlocks[i]);
			ctx.used.add(oldBlocks[i]);
			i++;
			j++;
		} else if (j < m && (i === n || lcs[i][j + 1] >= lcs[i + 1][j])) {
			ops.push({ op: "ins", j });
			j++;
		} else {
			ops.push({ op: "del", i });
			i++;
		}
	}

	const stretches: Op[][] = [];
	for (let k = 0; k < ops.length; ) {
		if (ops[k].op === "keep") {
			stretches.push([ops[k++]]);
			continue;
		}
		const st: Op[] = [];
		while (k < ops.length && ops[k].op !== "keep") st.push(ops[k++]);
		stretches.push(st);
	}

	// Moves: an old block (or nested block) the diff did not keep whose
	// render reappears among the new blocks (or nested blocks) the diff did
	// not keep, anywhere at this level, is that old block there (moved,
	// swapped, indented or outdented). Duplicates pair in document order.
	const oldPool = ops.flatMap((o) =>
		o.op === "del" ? subtree(oldBlocks[o.i]) : [],
	);
	const newPool = ops.flatMap((o) =>
		o.op === "ins" ? subtree(fresh[o.j]) : [],
	);
	for (const old of oldPool) {
		if (ctx.used.has(old)) continue;
		const key = render([old]);
		if (key === "") continue;
		const to = newPool.find((f) => !ctx.moves.has(f) && render([f]) === key);
		if (!to) continue;
		ctx.moves.set(to, old);
		ctx.used.add(old);
	}

	const out: Block[] = [];
	const replaced: Replaced[] = [];
	let insertedAlone = false;
	const record = (r: Replaced) => {
		if (!nested) {
			replaced.push(r);
			return;
		}
		const loss = lossOf(r, oldBlocks);
		if (loss) throw new NestedUnsafe(loss, oldBlocks[r.i].type);
	};
	const emit = (st: Op[]) => {
		const olds = st.flatMap((o) => (o.op === "del" ? [o.i] : []));
		const empty = (i: number) => oldKeys[i] === "";
		// Old blocks moved elsewhere are not replaced here.
		const reals = olds.filter((i) => !empty(i) && !ctx.used.has(oldBlocks[i]));
		const ins = st.flatMap((o) => (o.op === "ins" ? [fresh[o.j]] : []));
		const freeIns = ins.filter((f) => !ctx.moves.has(f));

		// The stretch's new blocks in order, each with the old block it
		// stands in for (positional pairing), if any.
		const items: Block[] = [];
		const pairPos = new Map<number, number>();
		let mode: "insert" | "delete" | "paired" | "split";
		if (reals.length === 0) mode = "insert";
		else if (freeIns.length === 0) mode = "delete";
		else if (
			reals.length === freeIns.length &&
			reals.every((r, k) => oldBlocks[r].type === freeIns[k].type)
		)
			mode = "paired";
		else mode = "split";

		if (mode === "insert" && freeIns.length > 0) insertedAlone = true;
		if (mode === "delete")
			for (const r of reals) record({ i: r, how: "deleted" });

		let props: Record<string, unknown> = {};
		if (mode === "split") {
			const dels = reals.map((r) => oldBlocks[r]);
			props = ownProps(dels[0]);
			const agree = dels.every((d) => stable(ownProps(d)) === stable(props));
			const sameType = freeIns.every((b) =>
				dels.every((d) => d.type === b.type),
			);
			for (const r of reals) {
				record(
					!agree
						? { i: r, how: "differ", group: reals }
						: Object.keys(props).length > 0 && !sameType
							? {
									i: r,
									how: "retype",
									to: [...new Set(freeIns.map((b) => b.type))],
								}
							: { i: r, how: "carried" },
				);
			}
		}
		let q = 0;
		for (const f of ins) {
			const moved = ctx.moves.get(f);
			if (moved) {
				items.push(moved);
				continue;
			}
			if (mode === "paired") {
				const r = reals[q++];
				pairPos.set(r, items.length);
				try {
					items.push(inherit(oldBlocks[r], f, ctx));
				} catch (err) {
					if (nested || !(err instanceof NestedUnsafe)) throw err;
					record({ i: r, how: "nested", type: err.blockType, loss: err.loss });
					items.push(f);
					continue;
				}
				record({ i: r, how: "paired" });
			} else if (mode === "split") {
				const made = withProps(materialize(f, ctx), props);
				if (q++ === 0) {
					// The first new block continues the first old one (its id),
					// when they have the same type.
					pairPos.set(reals[0], items.length);
					const first = oldBlocks[reals[0]];
					if (first.type === made.type && typeof first.id === "string") {
						items.push({ ...made, id: first.id });
						continue;
					}
				}
				items.push(made);
			} else {
				items.push(materialize(f, ctx));
			}
		}

		// Old blocks that render to nothing: placed by their old neighbours.
		const before: Block[][] = items.map(() => []);
		const after: Block[][] = items.map(() => []);
		const lead: Block[] = [];
		const tail: Block[] = [];
		for (const e of olds.filter(empty)) {
			const old = oldBlocks[e];
			if (items.length === 0 || mode === "insert" || mode === "delete") {
				// No pairing to place it by: the diff's order.
				const at = st.findIndex((o) => o.op === "del" && o.i === e);
				const insBefore = st.slice(0, at).filter((o) => o.op === "ins").length;
				if (insBefore === 0) lead.push(old);
				else after[Math.min(insBefore, items.length) - 1].push(old);
				continue;
			}
			const next = reals.find((r) => r > e);
			const prev = [...reals].reverse().find((r) => r < e);
			if (mode === "paired") {
				if (next !== undefined) before[pairPos.get(next) as number].push(old);
				else if (prev !== undefined)
					after[pairPos.get(prev) as number].push(old);
				else lead.push(old);
			} else if (prev === undefined) {
				before[pairPos.get(reals[0]) ?? 0].push(old);
			} else if (next !== undefined) {
				after[pairPos.get(reals[0]) ?? 0].push(old);
			} else tail.push(old);
		}
		out.push(...lead);
		items.forEach((b, k) => out.push(...before[k], b, ...after[k]));
		out.push(...tail);
	};
	for (const st of stretches) {
		if (st[0].op === "keep") {
			out.push(oldBlocks[(st[0] as { i: number }).i]);
			continue;
		}
		emit(st);
	}
	// Text deleted in one stretch and inserted in another (moved past a
	// block the edit kept) is a rewrite too: check what was deleted.
	if (insertedAlone && !nested) {
		for (const r of replaced) if (r.how === "deleted") r.how = "carried";
	}
	return { blocks: out, replaced };
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
		for (const r of b.content.rows ?? []) {
			for (const c of r.cells ?? []) walk(Array.isArray(c) ? c : c?.content);
		}
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
const ownKey = (b: Block) => render([{ ...b, children: [] }]);

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
 * 3. a result block with an old block's id but other Markdown does not
 *    hold the Markdown of a different old block whose id is not on it.
 */
export function auditEdit(
	oldBlocks: Block[],
	newBlocks: Block[],
): { block: Block; why: string } | null {
	const olds = oldBlocks.flatMap(subtree);
	const news = newBlocks.flatMap(subtree);
	const returned = new Set(news);
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
	}

	for (let k = 0; k < news.length; k++) {
		const n = news[k];
		const o = typeof n.id === "string" ? oldById.get(n.id) : undefined;
		// A block returned as the very object read is as it was.
		if (!o || o === n || newKeys[k] === ownKey(o)) continue;
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
	if (oldString === "") {
		throw new SpliceError("old_string must not be empty.", "empty");
	}
	const renders = blocks.map((b) => render([b]));
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
		const aligned = align(
			blocks.slice(from, to),
			renders.slice(from, to),
			fresh,
		);
		// The line where block `idx` starts in the full Markdown.
		const lineOf = (idx: number) => {
			if (starts[idx] >= 0) return lineAt(full, starts[idx]);
			const unit = units.find((u) => u.from <= idx && idx < u.to) ?? units[ua];
			const first = renders[idx].split("\n")[0];
			const at = first ? full.indexOf(first, unit.start) : -1;
			return lineAt(full, at >= 0 ? at : unit.start + (unit.start > 0 ? 1 : 0));
		};
		for (const r of aligned.replaced) {
			if (r.how === "deleted") continue;
			const k = r.i;
			const b = blocks[from + k];
			if (r.how === "differ") {
				const lines = r.group.map((g) => lineOf(from + g));
				const named =
					lines.length === 1
						? `line ${lines[0]}`
						: `lines ${lines.slice(0, -1).join(", ")} and ${lines[lines.length - 1]}`;
				throw new SpliceError(
					`The edit merges, splits or retypes the blocks on ${named}, which edit_doc cannot rewrite from Markdown: they have different colours or alignment, so the new blocks cannot keep them. Nothing was changed. Edit those blocks in the web editor, or rewrite the document with write_doc if losing that is acceptable.`,
					"unsafe",
					lines,
				);
			}
			const loss = lossOf(r, blocks.slice(from, to));
			if (!loss) continue;
			const line = lineOf(from + k);
			throw new SpliceError(
				`The edit touches the ${b.type} block on line ${line}, which edit_doc cannot rewrite from Markdown: ${loss}. Nothing was changed. Edit that block in the web editor, or rewrite the document with write_doc if losing that is acceptable.`,
				"unsafe",
				[line],
			);
		}
		// The result must say what the edit asks for: its Markdown is that
		// of the edited text parsed as a whole (blocks that render to nothing
		// aside), and differs from what was there.
		const shown = (bs: Block[]) => render(bs.filter((b) => render([b]) !== ""));
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
		if (bad) {
			const top = blocks
				.slice(from, to)
				.findIndex((b) => subtree(b).includes(bad.block));
			const line = lineOf(from + Math.max(0, top));
			const where =
				top >= 0 && blocks[from + top] !== bad.block
					? `a ${bad.block.type} block nested in the block on line ${line}`
					: `the ${bad.block.type} block on line ${line}`;
			throw new SpliceError(
				`The edit touches ${where}, which edit_doc cannot rewrite from Markdown: ${bad.why}. Nothing was changed. Edit that block in the web editor, or rewrite the document with write_doc if losing that is acceptable.`,
				"unsafe",
				[line],
			);
		}
		out.push(...blocks.slice(next, from));
		out.push(...aligned.blocks);
		next = to;
	}
	out.push(...blocks.slice(next));
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
