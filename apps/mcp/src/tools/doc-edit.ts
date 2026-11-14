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
		readonly kind: "not_found" | "ambiguous" | "empty" | "unsafe",
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
		texts[0].text = texts[0].text.replace(/^\s+/, "");
		const last = texts[texts.length - 1];
		last.text = last.text.replace(/\s+$/, "");
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
			if (loose) {
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
						? { props: {}, content: canonInline(cell) }
						: {
								props: canonProps((cell as Block).props),
								content: canonInline((cell as Block).content),
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
	const back =
		carry === "paired" && parsed.length === 1 && parsed[0].type === b.type
			? [inherit(b, parsed[0])]
			: parsed;
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

/** Old nested blocks that equal the new ones (ids aside) are kept as they were. */
function reuseChildren(old: Block, fresh: Block): Block[] | undefined {
	const oc = Array.isArray(old.children) ? old.children : [];
	const nc = Array.isArray(fresh.children) ? fresh.children : undefined;
	if (!nc) return nc;
	return nc.map((c: Block, k: number) => {
		const o = oc[k];
		if (!o) return c;
		if (o.type !== c.type) return c;
		const kept = inherit(o, c);
		// Unchanged once its colours are restored: the stored object as it was.
		return stable(canonBlock(o)) === stable(canonBlock(kept)) ? o : kept;
	});
}

/**
 * `fresh` (parsed from Markdown) standing in for `old`: it keeps old's id
 * (or lack of one) and the props Markdown cannot carry, such as colours.
 */
function inherit(old: Block, fresh: Block): Block {
	const props: Record<string, unknown> = { ...(old.props ?? {}) };
	for (const [k, v] of Object.entries(fresh.props ?? {})) {
		if (k in props && DEFAULT_PROPS[k] === v) continue;
		props[k] = v;
	}
	const out: Block = { ...fresh, props };
	const children = reuseChildren(old, fresh);
	if (children) out.children = children;
	if ("id" in old) out.id = old.id;
	else delete out.id;
	return out;
}

/** What happened to an old block the edit replaced. */
type Replaced =
	| { i: number; how: "deleted" | Carry }
	| { i: number; how: "unclear"; from: number; to: number };

/** The colours and alignment of `b` that differ from the defaults. */
function ownProps(b: Block): Record<string, unknown> {
	const out: Record<string, unknown> = {};
	for (const k of Object.keys(DEFAULT_PROPS)) {
		const v = b.props?.[k];
		if (v !== undefined && v !== DEFAULT_PROPS[k]) out[k] = v;
	}
	return out;
}

/**
 * The colours and alignment that the `count` blocks replacing `dels` take:
 * a merge into one block takes the first block's, a split of one block
 * gives every part its own, and otherwise the old blocks must agree. Null
 * when they do not.
 */
function carriedProps(
	dels: Block[],
	count: number,
): Record<string, unknown> | null {
	const first = ownProps(dels[0]);
	if (count === 1 || dels.length === 1) return first;
	return dels.every((d) => stable(ownProps(d)) === stable(first))
		? first
		: null;
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

/**
 * Replaces `oldBlocks` with `fresh`, keeping every old block whose own
 * Markdown is unchanged (longest common subsequence of the per-block
 * renders) as the original object. Old blocks that render to nothing are
 * kept unless the blocks on both sides of them were replaced.
 */
function align(
	oldBlocks: Block[],
	oldKeys: string[],
	fresh: Block[],
): { blocks: Block[]; replaced: Replaced[] } {
	const newKeys = fresh.map((b) => render([b]));
	const n = oldBlocks.length;
	const m = fresh.length;
	const lcs: number[][] = Array.from({ length: n + 1 }, () =>
		new Array<number>(m + 1).fill(0),
	);
	for (let i = n - 1; i >= 0; i--) {
		for (let j = m - 1; j >= 0; j--) {
			lcs[i][j] =
				oldKeys[i] !== "" && oldKeys[i] === newKeys[j]
					? lcs[i + 1][j + 1] + 1
					: Math.max(lcs[i + 1][j], lcs[i][j + 1]);
		}
	}

	type Op =
		| { op: "keep"; i: number }
		| { op: "del"; i: number }
		| { op: "ins"; j: number };
	const ops: Op[] = [];
	let i = 0;
	let j = 0;
	while (i < n || j < m) {
		if (i < n && j < m && oldKeys[i] !== "" && oldKeys[i] === newKeys[j]) {
			ops.push({ op: "keep", i });
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

	const kept = new Set(
		ops.filter((o) => o.op === "keep").map((o) => (o as { i: number }).i),
	);
	// An empty block is dropped only when the edit replaced the blocks on
	// both sides of it.
	const enclosed = (k: number) => {
		let p = k - 1;
		while (p >= 0 && oldKeys[p] === "") p--;
		let q = k + 1;
		while (q < n && oldKeys[q] === "") q++;
		return p >= 0 && !kept.has(p) && q < n && !kept.has(q);
	};

	const out: Block[] = [];
	const replaced: Replaced[] = [];
	let delIdx: number[] = [];
	let dels: Block[] = [];
	let ins: Block[] = [];
	const flush = () => {
		if (ins.length === 0) {
			// Deleted outright: nothing of them is rewritten.
			for (const i of delIdx) replaced.push({ i, how: "deleted" });
		} else if (
			// As many new blocks as old ones, of the same types: each new
			// block stands in for the old one at its place.
			dels.length === ins.length &&
			dels.every((d, k) => d.type === ins[k].type)
		) {
			out.push(...ins.map((b, k) => inherit(dels[k], b)));
			for (const i of delIdx) replaced.push({ i, how: "paired" });
		} else if (dels.length > 0) {
			// A merge, split or change of type: the new blocks take the old
			// colours and alignment where that is unambiguous.
			const props = carriedProps(dels, ins.length);
			out.push(...(props ? ins.map((b) => withProps(b, props)) : ins));
			for (const i of delIdx) {
				replaced.push(
					props
						? { i, how: "carried" }
						: { i, how: "unclear", from: dels.length, to: ins.length },
				);
			}
		} else {
			out.push(...ins);
		}
		delIdx = [];
		dels = [];
		ins = [];
	};
	for (const o of ops) {
		if (o.op === "keep") {
			flush();
			out.push(oldBlocks[o.i]);
		} else if (o.op === "ins") {
			ins.push(fresh[o.j]);
		} else if (oldKeys[o.i] === "" && !enclosed(o.i)) {
			flush();
			out.push(oldBlocks[o.i]);
		} else if (oldKeys[o.i] !== "") {
			dels.push(oldBlocks[o.i]);
			delIdx.push(o.i);
		}
	}
	flush();
	return { blocks: out, replaced };
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

		const aligned = align(
			blocks.slice(from, to),
			renders.slice(from, to),
			fixSoftBreaks(markdownToBlocknote(text)),
		);
		for (const r of aligned.replaced) {
			if (r.how === "deleted") continue;
			const k = r.i;
			const b = blocks[from + k];
			const loss =
				r.how === "unclear"
					? `the edit turns ${r.from} blocks with different colours or alignment into ${r.to}, so which block keeps which is unclear`
					: markdownLoss(b, r.how);
			if (!loss) continue;
			const unit =
				units.find((u) => u.from <= from + k && from + k < u.to) ?? units[ua];
			const first = renders[from + k].split("\n")[0];
			const at = first ? full.indexOf(first, unit.start) : -1;
			const line = lineAt(
				full,
				at >= 0 ? at : unit.start + (unit.start > 0 ? 1 : 0),
			);
			throw new SpliceError(
				`The edit touches the ${b.type} block on line ${line}, which edit_doc cannot rewrite from Markdown: ${loss}. Nothing was changed. Edit that block in the web editor, or rewrite the document with write_doc if losing that is acceptable.`,
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
