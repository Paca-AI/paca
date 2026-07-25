import { describe, expect, it } from "vitest";
import { collapseUnchangedContext, type DiffLine } from "./diff-utils";

function unchanged(texts: string[]): DiffLine[] {
	return texts.map((text) => ({ type: "unchanged", text }));
}

describe("collapseUnchangedContext", () => {
	it("leaves short unchanged runs untouched", () => {
		const lines: DiffLine[] = [
			...unchanged(["a", "b"]),
			{ type: "removed", text: "old" },
			{ type: "added", text: "new" },
			...unchanged(["c", "d"]),
		];

		expect(collapseUnchangedContext(lines, 3)).toEqual(lines);
	});

	it("collapses a long unchanged run between two changes into one marker with context on both sides", () => {
		const lines: DiffLine[] = [
			{ type: "removed", text: "first-old" },
			{ type: "added", text: "first-new" },
			...unchanged([
				"u1",
				"u2",
				"u3",
				"u4",
				"u5",
				"u6",
				"u7",
				"u8",
				"u9",
				"u10",
			]),
			{ type: "removed", text: "second-old" },
			{ type: "added", text: "second-new" },
		];

		const rows = collapseUnchangedContext(lines, 2);

		expect(rows).toEqual([
			{ type: "removed", text: "first-old" },
			{ type: "added", text: "first-new" },
			{ type: "unchanged", text: "u1" },
			{ type: "unchanged", text: "u2" },
			{ type: "collapsed", count: 6 },
			{ type: "unchanged", text: "u9" },
			{ type: "unchanged", text: "u10" },
			{ type: "removed", text: "second-old" },
			{ type: "added", text: "second-new" },
		]);
	});

	it("drops leading context entirely since there is nothing before the file start to show", () => {
		const lines: DiffLine[] = [
			...unchanged(["u1", "u2", "u3", "u4", "u5", "u6", "u7", "u8"]),
			{ type: "added", text: "new" },
		];

		const rows = collapseUnchangedContext(lines, 2);

		expect(rows[0]).toEqual({ type: "collapsed", count: 6 });
		expect(rows[1]).toEqual({ type: "unchanged", text: "u7" });
		expect(rows[2]).toEqual({ type: "unchanged", text: "u8" });
		expect(rows[3]).toEqual({ type: "added", text: "new" });
	});

	it("drops trailing context entirely since there is nothing after the file end to show", () => {
		const lines: DiffLine[] = [
			{ type: "removed", text: "old" },
			...unchanged(["u1", "u2", "u3", "u4", "u5", "u6", "u7", "u8"]),
		];

		const rows = collapseUnchangedContext(lines, 2);

		expect(rows[0]).toEqual({ type: "removed", text: "old" });
		expect(rows[1]).toEqual({ type: "unchanged", text: "u1" });
		expect(rows[2]).toEqual({ type: "unchanged", text: "u2" });
		expect(rows[3]).toEqual({ type: "collapsed", count: 6 });
	});

	it("merges two changes separated by a short unchanged gap without collapsing", () => {
		const lines: DiffLine[] = [
			{ type: "removed", text: "first-old" },
			...unchanged(["u1", "u2", "u3"]),
			{ type: "added", text: "second-new" },
		];

		expect(collapseUnchangedContext(lines, 2)).toEqual(lines);
	});

	it("returns an empty array for an empty diff", () => {
		expect(collapseUnchangedContext([], 3)).toEqual([]);
	});
});
