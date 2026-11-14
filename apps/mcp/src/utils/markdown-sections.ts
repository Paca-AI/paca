import { Worker } from "node:worker_threads";

// Line-based views of a document's Markdown: outline, sections, line ranges
// and grep matching.
//
// Every line number here is a 1-based line of the Markdown that
// blocknoteToMarkdown produces, which is the text read_doc returns, so a line
// found by one tool means the same line in the others.

export interface MarkdownHeading {
	line: number;
	level: number;
	text: string;
}

/** The lines of `markdown`, without the empty string a final newline leaves. */
export function markdownLines(markdown: string): string[] {
	if (markdown === "") return [];
	const lines = markdown.split("\n");
	if (lines[lines.length - 1] === "") lines.pop();
	return lines;
}

const FENCE = /^ {0,3}(`{3,}|~{3,})/;
const HEADING = /^ {0,3}(#{1,6})(?:[ \t]+(.*?))?(?:[ \t]+#+)?[ \t]*$/;

/** The ATX headings of `markdown` (`# …` to `###### …`), skipping fenced code. */
export function markdownHeadings(markdown: string): MarkdownHeading[] {
	const out: MarkdownHeading[] = [];
	let fence: string | null = null;
	markdownLines(markdown).forEach((line, i) => {
		const f = FENCE.exec(line);
		if (fence) {
			// A fence closes on the same character, at least as long, with
			// nothing after it.
			if (
				f &&
				f[1][0] === fence[0] &&
				f[1].length >= fence.length &&
				line.trim() === f[1]
			) {
				fence = null;
			}
			return;
		}
		if (f) {
			fence = f[1];
			return;
		}
		const h = HEADING.exec(line);
		if (h) out.push({ line: i + 1, level: h[1].length, text: h[2] ?? "" });
	});
	return out;
}

/** Each heading as `<line>: <#…> <text>`, then the document's line count. */
export function outlineOf(markdown: string): string {
	const headings = markdownHeadings(markdown);
	const body =
		headings.length === 0
			? "(no headings)"
			: headings
					.map((h) => `${h.line}: ${"#".repeat(h.level)} ${h.text}`)
					.join("\n");
	return `${body}\n\n${markdownLines(markdown).length} lines`;
}

export type SectionResult =
	| { status: "found"; heading: MarkdownHeading; start: number; end: number }
	| { status: "ambiguous"; matches: MarkdownHeading[] }
	| { status: "not_found" };

/**
 * The section under the heading named `name` (case-insensitive, with or
 * without its leading hashes): from that heading to the line before the next
 * heading of the same or a higher level, or to the end of the document.
 */
export function findSection(markdown: string, name: string): SectionResult {
	// Compared without emphasis markers, so "Step one" finds "Step **one**".
	const norm = (s: string) => stripEmphasis(s).trim().toLowerCase();
	const wanted = norm(name.trim().replace(/^#{1,6}\s+/, ""));
	const headings = markdownHeadings(markdown);
	const matches = headings.filter((h) => norm(h.text) === wanted);
	if (matches.length === 0) return { status: "not_found" };
	if (matches.length > 1) return { status: "ambiguous", matches };
	const heading = matches[0];
	const next = headings.find(
		(h) => h.line > heading.line && h.level <= heading.level,
	);
	return {
		status: "found",
		heading,
		start: heading.line,
		end: next ? next.line - 1 : markdownLines(markdown).length,
	};
}

/**
 * Reads `"A-B"` (also `"A"` and `"A-"`) as an inclusive 1-based line range,
 * clamped to a document of `total` lines.
 */
export function parseLineRange(
	range: string,
	total: number,
): { start: number; end: number } | { error: string } {
	const m = /^\s*(\d+)\s*(?:-\s*(\d*)\s*)?$/.exec(range);
	if (!m) {
		return {
			error: `Invalid line range "${range}": use "A-B", such as "40-80".`,
		};
	}
	const from = Number(m[1]);
	const to = m[2] === undefined ? from : m[2] === "" ? total : Number(m[2]);
	if (from > to) {
		return {
			error: `Invalid line range "${range}": the start is after the end.`,
		};
	}
	if (to < 1) {
		return {
			error: `Invalid line range "${range}": lines are numbered from 1.`,
		};
	}
	const start = Math.max(1, from);
	if (start > total) {
		return {
			error: `Line ${start} is past the end: the document has ${total} lines.`,
		};
	}
	return { start, end: Math.min(to, total) };
}

/** `lines` with each prefixed by its line number, the first being `first`. */
export function numberLines(lines: string[], first: number): string {
	return lines.map((l, i) => `${first + i}: ${l}`).join("\n");
}

// ── grep ──────────────────────────────────────────────────────────────────────

/** Tests one line of Markdown (`index` is its 0-based line index). */
export type LineMatcher = (line: string, index: number) => boolean;

/** Removes the `*` and `_` emphasis markers, so `foo **bar**` reads `foo bar`. */
export function stripEmphasis(text: string): string {
	return text.replace(/[*_]+/g, "");
}

/**
 * A literal matcher: the line contains `pattern` as written, or does once
 * the emphasis markers are removed from both (so a phrase that crosses bold
 * or italic text still matches).
 */
export function literalMatcher(
	pattern: string,
	ignoreCase = false,
): LineMatcher {
	const fold = (s: string) => (ignoreCase ? s.toLowerCase() : s);
	const plain = fold(pattern);
	const stripped = fold(stripEmphasis(pattern));
	return (line) => {
		const l = fold(line);
		return l.includes(plain) || stripEmphasis(l).includes(stripped);
	};
}

/** Thrown when a regular expression ran past its time limit. */
export class RegexTimeoutError extends Error {
	readonly ms: number;
	constructor(ms: number) {
		super(`the regular expression took longer than ${ms} ms`);
		this.name = "RegexTimeoutError";
		this.ms = ms;
	}
}

// Runs in a worker thread, so a pattern that backtracks for hours can be
// stopped: the MCP process itself is single-threaded.
// Written to run whether Node evaluates it as CommonJS or as an ES module
// (an eval worker inherits --input-type), so it uses import(), not require.
const REGEX_WORKER = `
import("node:worker_threads").then(({ parentPort, workerData }) => {
const re = new RegExp(workerData.pattern, workerData.flags);
const out = workerData.texts.map((text) => {
	const hits = [];
	const lines = text.split("\\n");
	for (let i = 0; i < lines.length; i++) if (re.test(lines[i])) hits.push(i);
	return hits;
});
parentPort.postMessage(out);
});
`;

/**
 * The 0-based indexes of the lines of each text that match `pattern`,
 * computed in a worker thread that is terminated after `timeoutMs` (then a
 * RegexTimeoutError is thrown). An invalid pattern throws a SyntaxError
 * before any worker starts.
 */
export async function regexLineHits(
	pattern: string,
	ignoreCase: boolean,
	texts: string[],
	timeoutMs = 2000,
): Promise<number[][]> {
	const flags = ignoreCase ? "i" : "";
	new RegExp(pattern, flags); // compiling never backtracks: syntax check only
	if (texts.length === 0) return [];
	const worker = new Worker(REGEX_WORKER, {
		eval: true,
		workerData: { pattern, flags, texts },
	});
	try {
		return await new Promise<number[][]>((resolve, reject) => {
			const timer = setTimeout(
				() => reject(new RegexTimeoutError(timeoutMs)),
				timeoutMs,
			);
			worker.once("message", (hits: number[][]) => {
				clearTimeout(timer);
				resolve(hits);
			});
			worker.once("error", (err) => {
				clearTimeout(timer);
				reject(err);
			});
		});
	} finally {
		void worker.terminate();
	}
}

export interface GrepLine {
	line: number;
	text: string;
	match: boolean;
}

/**
 * The matching lines of `markdown` with `context` lines around each, in
 * groups of adjacent lines (grep's `--` separated hunks). At most `limit`
 * matching lines are returned; `matches` counts the ones returned.
 */
export function grepLines(
	markdown: string,
	matcher: LineMatcher,
	context: number,
	limit: number,
): { groups: GrepLine[][]; matches: number; truncated: boolean } {
	const lines = markdownLines(markdown);
	const hits: number[] = [];
	let truncated = false;
	for (let i = 0; i < lines.length; i++) {
		if (!matcher(lines[i], i)) continue;
		if (hits.length === limit) {
			truncated = true;
			break;
		}
		hits.push(i);
	}
	const groups: GrepLine[][] = [];
	let current: GrepLine[] = [];
	let shownTo = -1; // index of the last line shown
	const hitSet = new Set(hits);
	for (const h of hits) {
		const from = Math.max(0, h - context, shownTo + 1);
		const to = Math.min(lines.length - 1, h + context);
		if (current.length > 0 && from > shownTo + 1) {
			groups.push(current);
			current = [];
		}
		for (let i = from; i <= to; i++) {
			current.push({ line: i + 1, text: lines[i], match: hitSet.has(i) });
		}
		shownTo = Math.max(shownTo, to);
	}
	if (current.length > 0) groups.push(current);
	return { groups, matches: hits.length, truncated };
}
