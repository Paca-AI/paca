// The ways a pulled Markdown file may differ from the document's render
// and still say the same thing, so an editor's or a formatter's habits are
// not taken for edits. Line by line, and only these rules:
//
// - a byte order mark, CRLF line ends and the newlines at the end;
// - whitespace at the end of a line;
// - a run of blank lines outside fenced code reads as one blank line;
// - outside fenced code, a "-" or "+" bullet reads as "*";
// - outside fenced code, a table row reads with its cells trimmed, and the
//   delimiter row ("| --- | :-: |") as just its alignment.
//
// Nothing else: any other difference is a difference.

/** An opening or closing code fence: its character run. */
const FENCE = /^ {0,3}(`{3,}|~{3,})/;
const BULLET = /^(\s*)[-+](?=[ \t])/;
const DELIMITER = /^:?-+:?$/;

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

function tableRow(line: string): string {
	let cs = cells(line.trim());
	if (cs.length > 0 && cs.every((x) => DELIMITER.test(x))) {
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
	return lines.map((raw, k) => {
		const line = raw.replace(/\s+$/, "");
		if (fenced[k]) return line;
		if (line.trimStart().startsWith("|")) return tableRow(line);
		return line.replace(BULLET, "$1*");
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
	const lines: string[] = [];
	const from: number[] = [];
	keys.forEach((key, k) => {
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
	while (lines.length > 0 && lines[lines.length - 1].trim() === "") {
		lines.pop();
		from.pop();
	}
	return {
		text: lines.length === 0 ? "" : `${lines.join("\n")}\n`,
		lines,
		from,
	};
}
