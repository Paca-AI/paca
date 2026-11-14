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
		readonly kind: "not_found" | "ambiguous" | "empty",
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
	if ("id" in old) out.id = old.id;
	else delete out.id;
	return out;
}

/**
 * Replaces `oldBlocks` with `fresh`, keeping every old block whose own
 * Markdown is unchanged (longest common subsequence of the per-block
 * renders) as the original object. Old blocks that render to nothing are
 * kept unless the blocks on both sides of them were replaced.
 */
function align(oldBlocks: Block[], oldKeys: string[], fresh: Block[]): Block[] {
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
	let dels: Block[] = [];
	let ins: Block[] = [];
	const flush = () => {
		// A changed stretch with as many new blocks as old ones of the same
		// types: each new block stands in for the old one at its place.
		const paired =
			dels.length === ins.length &&
			dels.every((d, k) => d.type === ins[k].type);
		out.push(...(paired ? ins.map((b, k) => inherit(dels[k], b)) : ins));
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
		}
	}
	flush();
	return out;
}

/**
 * Replaces `oldString` with `newString` in the document's Markdown and
 * returns the new top-level blocks. Only the blocks the matches touch are
 * re-parsed from Markdown (a list as a whole, as Markdown writes it as one);
 * every other block in the result is the same object as in `blocks`.
 *
 * Throws a SpliceError when `oldString` is empty, is not found, or is found
 * more than once without `replaceAll`.
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

		out.push(...blocks.slice(next, from));
		out.push(
			...align(
				blocks.slice(from, to),
				renders.slice(from, to),
				markdownToBlocknote(text),
			),
		);
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
				return toolText(lockedMessage(doc), true);
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
