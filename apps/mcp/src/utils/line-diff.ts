// A line diff (longest common subsequence of lines), shared by the CLI's
// conflict output and the mirror push, which turns an edited file into
// edits of the document's blocks.

/** Above this many line pairs the diff shows the whole text as replaced
 *  instead of finding the common lines (keeps memory bounded). */
const MAX_CELLS = 4_000_000;

export function splitLines(text: string): string[] {
	if (text === "") return [];
	const lines = text.split("\n");
	if (lines[lines.length - 1] === "") lines.pop();
	return lines;
}

export type LineOp = { kind: " " | "-" | "+"; line: string };

/**
 * The edit script from `a` to `b` that keeps the heaviest common
 * subsequence of lines. `weight` (at most 64 per line) says how much
 * keeping a line is worth; every line weighs 1 by default (the longest
 * common subsequence).
 */
export function diffOps(
	a: string[],
	b: string[],
	weight: (line: string) => number = () => 1,
): LineOp[] {
	if (a.length * b.length > MAX_CELLS) {
		return [
			...a.map((line) => ({ kind: "-" as const, line })),
			...b.map((line) => ({ kind: "+" as const, line })),
		];
	}
	// lcs[i][j]: weight of the heaviest common subsequence of a[i..] and b[j..].
	const w = b.length + 1;
	const lcs = new Uint32Array((a.length + 1) * w);
	for (let i = a.length - 1; i >= 0; i--) {
		for (let j = b.length - 1; j >= 0; j--) {
			const skip = Math.max(lcs[(i + 1) * w + j], lcs[i * w + j + 1]);
			lcs[i * w + j] =
				a[i] === b[j]
					? Math.max(skip, lcs[(i + 1) * w + j + 1] + weight(a[i]))
					: skip;
		}
	}
	const ops: LineOp[] = [];
	let i = 0;
	let j = 0;
	while (i < a.length && j < b.length) {
		if (
			a[i] === b[j] &&
			lcs[i * w + j] === lcs[(i + 1) * w + j + 1] + weight(a[i])
		) {
			ops.push({ kind: " ", line: a[i] });
			i++;
			j++;
		} else if (lcs[(i + 1) * w + j] >= lcs[i * w + j + 1]) {
			ops.push({ kind: "-", line: a[i++] });
		} else {
			ops.push({ kind: "+", line: b[j++] });
		}
	}
	while (i < a.length) ops.push({ kind: "-", line: a[i++] });
	while (j < b.length) ops.push({ kind: "+", line: b[j++] });
	return ops;
}
