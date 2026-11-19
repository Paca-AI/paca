import { diffOps, splitLines } from "../utils/line-diff.js";

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
