/** Above this many line pairs the diff shows the whole text as replaced
 *  instead of finding the common lines (keeps memory bounded). */
const MAX_CELLS = 4_000_000;

function splitLines(text: string): string[] {
	if (text === "") return [];
	const lines = text.split("\n");
	if (lines[lines.length - 1] === "") lines.pop();
	return lines;
}

type Op = { kind: " " | "-" | "+"; line: string };

function diffOps(a: string[], b: string[]): Op[] {
	if (a.length * b.length > MAX_CELLS) {
		return [
			...a.map((line) => ({ kind: "-" as const, line })),
			...b.map((line) => ({ kind: "+" as const, line })),
		];
	}
	// lcs[i][j]: length of the longest common subsequence of a[i..] and b[j..].
	const w = b.length + 1;
	const lcs = new Uint32Array((a.length + 1) * w);
	for (let i = a.length - 1; i >= 0; i--) {
		for (let j = b.length - 1; j >= 0; j--) {
			lcs[i * w + j] =
				a[i] === b[j]
					? lcs[(i + 1) * w + j + 1] + 1
					: Math.max(lcs[(i + 1) * w + j], lcs[i * w + j + 1]);
		}
	}
	const ops: Op[] = [];
	let i = 0;
	let j = 0;
	while (i < a.length && j < b.length) {
		if (a[i] === b[j]) {
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

/** A unified diff (as `diff -u` prints it) from `a` to `b`, or "" when they
 *  are equal. */
export function unifiedDiff(
	a: string,
	b: string,
	labelA: string,
	labelB: string,
	context = 3,
): string {
	const ops = diffOps(splitLines(a), splitLines(b));
	const changed = ops
		.map((op, idx) => (op.kind === " " ? -1 : idx))
		.filter((idx) => idx >= 0);
	if (changed.length === 0) return "";

	// Group changes whose context windows touch into one hunk.
	const hunks: Array<[number, number]> = [];
	for (const idx of changed) {
		const start = Math.max(0, idx - context);
		const end = Math.min(ops.length, idx + context + 1);
		const last = hunks[hunks.length - 1];
		if (last && start <= last[1]) last[1] = Math.max(last[1], end);
		else hunks.push([start, end]);
	}

	const out = [`--- ${labelA}`, `+++ ${labelB}`];
	// Line numbers in a and b where each op sits.
	const posA: number[] = [];
	const posB: number[] = [];
	let la = 0;
	let lb = 0;
	for (const op of ops) {
		posA.push(la);
		posB.push(lb);
		if (op.kind !== "+") la++;
		if (op.kind !== "-") lb++;
	}
	for (const [start, end] of hunks) {
		const slice = ops.slice(start, end);
		const countA = slice.filter((op) => op.kind !== "+").length;
		const countB = slice.filter((op) => op.kind !== "-").length;
		const startA = countA ? posA[start] + 1 : posA[start];
		const startB = countB ? posB[start] + 1 : posB[start];
		out.push(`@@ -${startA},${countA} +${startB},${countB} @@`);
		for (const op of slice) out.push(`${op.kind}${op.line}`);
	}
	return `${out.join("\n")}\n`;
}
