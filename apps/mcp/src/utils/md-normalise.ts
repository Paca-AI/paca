// The ways a pulled Markdown file may differ from the document's render
// and still say the same thing, so an editor's or a formatter's habits are
// not taken for edits. Line by line, and only these rules:
//
// - a byte order mark, CRLF line ends and the newlines at the end;
// - outside fenced code, spaces and tabs at the end of a line;
// - a run of blank lines outside fenced code reads as one blank line;
// - outside fenced code, a "-" or "+" bullet reads as "*" (not on a
//   thematic break line, such as "- - -");
// - outside fenced code, a list item nested in another is indented as the
//   render indents it (two spaces under "* ", three under "1. "), whether
//   written with spaces or tabs, as long as it nests the same; one at the
//   top level is not indented. Only where the list it is in is known: not
//   after a line that continues an item lazily, until the list ends;
// - outside fenced code, blank lines between two list items read as none;
// - outside fenced code, a numbered item after the first in its list reads
//   with the number its place gives it (the first item's number, the
//   list's start, counted on), as Markdown reads it: "1." for every item
//   reads as Paca's "1.", "2.", "3.";
// - outside fenced code, a table row reads with its cells trimmed, and a
//   table's second row, its delimiter row ("| --- | :-: |"), as just its
//   alignment.
//
// Nothing else: any other difference is a difference. Inside fenced code
// only line ends are read loosely.

/** An opening or closing code fence: its character run. */
const FENCE = /^ {0,3}(`{3,}|~{3,})/;
const BULLET = /^(\s*)[-+](?=[ \t])/;
/** A list item's start: its indent, its marker and the white space after. */
const ITEM = /^([ \t]*)([-*+]|\d{1,9}[.)])([ \t]+|$)/;
/** A thematic break ("***", "- - -", "___"). */
const BREAK = /^ {0,3}([-*_])([ \t]*\1){2,}[ \t]*$/;
const DELIMITER = /^:?-+:?$/;
/** Spaces and tabs at the end of a line (not other white space, such as a
 *  non-breaking space, which is text). */
const TRAILING = /[ \t]+$/;

/** Whether a line has nothing but spaces and tabs. */
export function isBlank(line: string): boolean {
	return /^[ \t]*$/.test(line);
}

/**
 * For each line, whether it is part of fenced code (the fence lines
 * included). An unclosed fence runs to the end.
 */
export function fencedLines(lines: string[]): boolean[] {
	let open: string | null = null;
	return lines.map((line) => {
		const f = FENCE.exec(line)?.[1];
		if (open === null) {
			// A backtick fence's info string has no backticks.
			if (!f) return false;
			const rest = line.slice(line.indexOf(f) + f.length);
			if (f[0] === "`" && rest.includes("`")) return false;
			open = f;
			return true;
		}
		if (f && f[0] === open[0] && f.length >= open.length && line.trim() === f)
			open = null;
		return true;
	});
}

/** The column a run of leading spaces and tabs ends at (tabs stop at
 *  every fourth column). */
function width(ws: string): number {
	let col = 0;
	for (const c of ws) col = c === "\t" ? col + 4 - (col % 4) : col + 1;
	return col;
}

/** A list item whose place in its list is known: the indent the render
 *  writes it with, and, for a numbered item, the number its place gives
 *  it (the first item's own number, which is the list's start, counted on
 *  for the items after it). */
interface ItemPlace {
	indent: string;
	number: number | null;
}

/**
 * For each line outside fenced code that starts a list item whose place in
 * its list is known, that place (see ItemPlace); null for every other line.
 */
export function itemPlaces(lines: string[]): Array<ItemPlace | null> {
	const fenced = fencedLines(lines);
	const out: Array<ItemPlace | null> = lines.map(() => null);
	/** The open items: where their content starts in the file, and in the
	 *  render. */
	let open: Array<{ file: number; render: number }> = [];
	/** Per depth, the numbered list open there: its delimiter and the
	 *  number of its next item. */
	let lists: Array<{ delim: string; next: number } | undefined> = [];
	/** Whether the list is unknown (a lazy line, or a line whose place
	 *  depends on how its items are indented), until the list ends. */
	let unknown = false;
	/** The first line of the outermost list open. */
	let listStart = 0;
	let blankBefore = true;
	/** Whether the line before is a paragraph's text (or other text). */
	let text = false;
	const end = () => {
		open = [];
		lists = [];
		unknown = false;
	};
	/** A line that is not an item, at column `w`: if it falls in other
	 *  items (or is code in one) with the items' content where the file
	 *  has it than where the render has it, the items' indents cannot be
	 *  read the render's way: the list becomes unknown, and what was read
	 *  of it is taken back. */
	const giveUp = (k: number) => {
		unknown = true;
		for (let n = listStart; n < k; n++) out[n] = null;
	};
	const check = (w: number, k: number) => {
		for (const o of open)
			if (
				w >= o.file !== w >= o.render ||
				w >= o.file + 4 !== w >= o.render + 4
			)
				return giveUp(k);
	};
	lines.forEach((line, k) => {
		if (fenced[k]) {
			text = false;
			const w = width(/^[ \t]*/.exec(line)?.[0] ?? "");
			// A fence at the top level ends the lists.
			if (w < (open[0]?.file ?? 1)) end();
			else {
				check(w, k);
				lists.length = Math.min(lists.length, open.length);
			}
			blankBefore = false;
			return;
		}
		if (isBlank(line)) {
			blankBefore = true;
			text = false;
			return;
		}
		let m = BREAK.test(line) ? null : ITEM.exec(line);
		const w = width(/^[ \t]*/.exec(line)?.[0] ?? "");
		const wasBlank = blankBefore;
		const wasText = text;
		blankBefore = false;
		// Only a bullet item, or a numbered one starting at 1, with text,
		// can interrupt a paragraph; any other such line continues it.
		if (
			m &&
			wasText &&
			(m[3] === "" || (/^\d/.test(m[2]) && !/^0*1[.)]$/.test(m[2])))
		)
			m = null;
		text = !m;
		if (!m) {
			if (open.length > 0) check(w, k);
			while (open.length > 0 && w < open[open.length - 1].file) {
				if (!wasBlank) {
					// A lazy continuation: the item goes on, and where the
					// lines after it belong depends on how its items are
					// indented.
					giveUp(k);
					return;
				}
				open.pop();
			}
			// Text in an item (or at the top level) ends the lists below it.
			lists.length = Math.min(lists.length, open.length);
			if (open.length === 0) end();
			return;
		}
		if (unknown) return;
		const fresh = open.length === 0;
		while (open.length > 0 && w < open[open.length - 1].file) open.pop();
		const parent = open[open.length - 1];
		// Indented code, not an item.
		if (parent ? w >= parent.file + 4 : w > 3) {
			if (parent) check(w, k);
			return;
		}
		if (fresh) {
			lists = [];
			listStart = k;
		}
		const depth = open.length;
		lists.length = Math.min(lists.length, depth + 1);
		const indent = parent ? parent.render : 0;
		const marker = m[2];
		let number: number | null = null;
		const num = /^(\d+)([.)])$/.exec(marker);
		if (num) {
			const list = lists[depth];
			number =
				list && list.delim === num[2] ? list.next : Number.parseInt(num[1], 10);
			lists[depth] = { delim: num[2], next: number + 1 };
		} else lists[depth] = undefined;
		const shown = number === null ? marker : `${number}${marker.slice(-1)}`;
		const after =
			m[3] === ""
				? 1
				: Math.min(width(m[1] + marker + m[3]) - w - marker.length, 4);
		open.push({
			file: w + marker.length + after,
			render: indent + shown.length + 1,
		});
		out[k] = { indent: " ".repeat(indent), number };
	});
	return out;
}

/** itemPlaces' indents alone. */
export function itemIndents(lines: string[]): Array<string | null> {
	return itemPlaces(lines).map((p) => p?.indent ?? null);
}

/** The cells of a table row, split at "|" signs that are not escaped. */
function cells(row: string): string[] {
	const out: string[] = [];
	let cur = "";
	for (let k = 0; k < row.length; k++) {
		const c = row[k];
		if (c === "\\" && k + 1 < row.length) {
			cur += c + row[++k];
		} else if (c === "|") {
			out.push(cur);
			cur = "";
		} else cur += c;
	}
	out.push(cur);
	// The row's own leading and trailing "|".
	if (out[0].trim() === "") out.shift();
	if (out.length > 0 && out[out.length - 1].trim() === "") out.pop();
	return out.map((x) => x.trim());
}

function tableRow(line: string, second: boolean): string {
	let cs = cells(line.trim());
	if (second && cs.length > 0 && cs.every((x) => DELIMITER.test(x))) {
		cs = cs.map(
			(x) => `${x.startsWith(":") ? ":" : ""}-${x.endsWith(":") ? ":" : ""}`,
		);
	}
	return `| ${cs.join(" | ")} |`;
}

/**
 * Each line as the rules read it, one for one (blank runs are not
 * collapsed here): two lines with the same key say the same thing.
 */
export function lineKeys(lines: string[]): string[] {
	const fenced = fencedLines(lines);
	const places = itemPlaces(lines);
	/** Rows of the table the line is in, before it. */
	let row = 0;
	return lines.map((raw, k) => {
		if (fenced[k]) {
			row = 0;
			return raw;
		}
		const line = raw.replace(TRAILING, "");
		// A table row is indented three columns at most (four make code).
		if (
			line.trimStart().startsWith("|") &&
			width(/^[ \t]*/.exec(line)?.[0] ?? "") <= 3
		)
			return tableRow(line, row++ === 1);
		row = 0;
		const place = places[k];
		// Only a line known to start a list item (not, say, a line that
		// continues a paragraph) has its indent, number and bullet read.
		if (!place) return line;
		let item = place.indent + line.trimStart();
		if (place.number !== null)
			item = item.replace(/^(\s*)\d+(?=[.)])/, `$1${place.number}`);
		return item.replace(BULLET, "$1*");
	});
}

/**
 * `text` as the rules read it: no byte order mark, LF line ends, each line
 * keyed, blank runs outside fenced code collapsed, no blank lines at the
 * end, and one newline after the last line (nothing for an empty text).
 * `from[k]` is the line of `text` (from 1) that line k (from 0) of the
 * result came from.
 */
export function normaliseMarkdown(text: string): {
	text: string;
	lines: string[];
	from: number[];
} {
	const raw = text.replace(/^﻿/, "").replace(/\r\n/g, "\n").split("\n");
	const keys = lineKeys(raw);
	const fenced = fencedLines(raw);
	const items = itemIndents(raw);
	/** Whether line k is blank and between two list items. */
	const between = raw.map(() => false);
	for (let k = 0; k < raw.length; k++) {
		if (items[k] === null) continue;
		let n = k + 1;
		while (n < raw.length && !fenced[n] && isBlank(raw[n])) n++;
		if (n > k + 1 && n < raw.length && items[n] !== null)
			for (let b = k + 1; b < n; b++) between[b] = true;
	}
	const lines: string[] = [];
	const from: number[] = [];
	keys.forEach((key, k) => {
		if (between[k]) return;
		// One more blank line in a run.
		if (
			key === "" &&
			!fenced[k] &&
			lines.length > 0 &&
			lines[lines.length - 1] === "" &&
			!fenced[k - 1]
		)
			return;
		lines.push(key);
		from.push(k + 1);
	});
	while (lines.length > 0 && lines[lines.length - 1] === "") {
		lines.pop();
		from.pop();
	}
	return {
		text: lines.length === 0 ? "" : `${lines.join("\n")}\n`,
		lines,
		from,
	};
}
