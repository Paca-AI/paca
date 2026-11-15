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
// - outside fenced code, blank lines between two list items read as none,
//   unless one of them is a checklist item (a blank line next to one
//   changes what it parses to);
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
/** A checklist item: next to one, a blank line changes what it parses to
 *  (the render writes a run of them with none between). */
const CHECK = /^[ \t]*([-*+]|\d{1,9}[.)])[ \t]+\[[ xX]\]/;
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

/**
 * For each line outside fenced code that starts a list item whose place in
 * its list is known, the indent the render writes it with ("" at the top
 * level); null for every other line.
 */
export function itemIndents(lines: string[]): Array<string | null> {
	const fenced = fencedLines(lines);
	/** The open items: where their content starts in the file, and in the
	 *  render. */
	let open: Array<{ file: number; render: number }> = [];
	/** Whether a lazy line made the list unknown, until the list ends. */
	let unknown = false;
	let blankBefore = true;
	return lines.map((line, k) => {
		if (fenced[k]) {
			// A fence at the top level ends the lists.
			if (width(/^[ \t]*/.exec(line)?.[0] ?? "") < (open[0]?.file ?? 1)) {
				open = [];
				unknown = false;
			}
			blankBefore = false;
			return null;
		}
		if (isBlank(line)) {
			blankBefore = true;
			return null;
		}
		const m = BREAK.test(line) ? null : ITEM.exec(line);
		const w = width(/^[ \t]*/.exec(line)?.[0] ?? "");
		const wasBlank = blankBefore;
		blankBefore = false;
		if (!m) {
			while (open.length > 0 && w < open[open.length - 1].file) {
				if (!wasBlank) {
					// A lazy continuation: the item goes on.
					unknown = true;
					return null;
				}
				open.pop();
			}
			if (open.length === 0) unknown = false;
			return null;
		}
		if (unknown) return null;
		while (open.length > 0 && w < open[open.length - 1].file) open.pop();
		const parent = open[open.length - 1];
		// Indented code, not an item.
		if (parent ? w >= parent.file + 4 : w > 3) return null;
		const indent = parent ? parent.render : 0;
		const marker = m[2];
		const after =
			m[3] === ""
				? 1
				: Math.min(width(m[1] + marker + m[3]) - w - marker.length, 4);
		open.push({
			file: w + marker.length + after,
			render: indent + marker.length + 1,
		});
		return " ".repeat(indent);
	});
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
	const indents = itemIndents(lines);
	/** Rows of the table the line is in, before it. */
	let row = 0;
	return lines.map((raw, k) => {
		if (fenced[k]) {
			row = 0;
			return raw;
		}
		const line = raw.replace(TRAILING, "");
		if (line.trimStart().startsWith("|")) return tableRow(line, row++ === 1);
		row = 0;
		const indent = indents[k];
		const item = indent === null ? line : indent + line.trimStart();
		return BREAK.test(item) ? item : item.replace(BULLET, "$1*");
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
		if (items[k] === null || CHECK.test(raw[k])) continue;
		let n = k + 1;
		while (n < raw.length && !fenced[n] && isBlank(raw[n])) n++;
		if (n > k + 1 && n < raw.length && items[n] !== null && !CHECK.test(raw[n]))
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
