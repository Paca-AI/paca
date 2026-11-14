import { describe, expect, it } from "vitest";
import {
	blockNoteToLines,
	collapseUnchangedContext,
	type DiffLine,
	diffBlockNoteContent,
} from "./diff-utils";

function paragraph(content: unknown[], children: unknown[] = []) {
	return { type: "paragraph", content, children };
}

function link(text: string, href = "https://example.com") {
	return { type: "link", href, content: [{ type: "text", text, styles: {} }] };
}

function table(rows: unknown[][]) {
	return {
		type: "table",
		content: { type: "tableContent", rows: rows.map((cells) => ({ cells })) },
	};
}

function cell(text: string) {
	return {
		type: "tableCell",
		content: [{ type: "text", text, styles: {} }],
	};
}

describe("BlockNote table diff text", () => {
	it("extracts rows with styled text, links and empty cells", () => {
		expect(
			blockNoteToLines([
				table([
					[cell("Name"), cell("Status")],
					[
						{
							type: "tableCell",
							content: [
								{ type: "text", text: "Read ", styles: { bold: true } },
								link("文档"),
							],
						},
						cell(""),
					],
				]),
			]),
		).toEqual(["Name\tStatus", "Read 文档\t"]);
	});

	it("supports legacy inline-array cells", () => {
		expect(
			blockNoteToLines([
				table([[[{ type: "text", text: "Legacy" }], [link("Guide")], []]]),
			]),
		).toEqual(["Legacy\tGuide\t"]);
	});

	it("reports an edited cell and preserves an unchanged header", () => {
		expect(
			diffBlockNoteContent(
				[table([[cell("Status")], [cell("Pending")]])],
				[table([[cell("Status")], [cell("Done")]])],
			),
		).toEqual([
			{ type: "unchanged", text: "Status" },
			{ type: "removed", text: "Pending" },
			{ type: "added", text: "Done" },
		]);
	});

	it("keeps cell boundaries when the concatenated text is identical", () => {
		expect(
			diffBlockNoteContent(
				[table([[cell("a"), cell("bc")]])],
				[table([[cell("ab"), cell("c")]])],
			),
		).toEqual([
			{ type: "removed", text: "a\tbc" },
			{ type: "added", text: "ab\tc" },
		]);
	});

	it("preserves empty tables and nested block order", () => {
		expect(
			blockNoteToLines([
				table([]),
				paragraph(
					[{ type: "text", text: "Parent" }],
					[table([[cell("Child")]])],
				),
				paragraph([{ type: "text", text: "After" }]),
			]),
		).toEqual(["", "Parent", "Child", "After"]);
	});
});

describe("BlockNote diff text", () => {
	it("includes link labels in their original inline order", () => {
		expect(
			blockNoteToLines([
				paragraph([
					{ type: "text", text: "Read " },
					{
						type: "link",
						href: "https://example.com",
						content: [
							{ type: "text", text: "the ", styles: { bold: true } },
							{ type: "text", text: "guide", styles: {} },
						],
					},
					{ type: "text", text: " first." },
				]),
			]),
		).toEqual(["Read the guide first."]);
	});

	it("includes links in nested blocks and legacy string labels", () => {
		expect(
			blockNoteToLines([
				paragraph(
					[link("Guide")],
					[
						paragraph([
							{
								type: "link",
								href: "https://example.com",
								content: "说明文档",
							},
						]),
					],
				),
			]),
		).toEqual(["Guide", "说明文档"]);
	});

	it("reports an edited link label as removed and added text", () => {
		expect(
			diffBlockNoteContent(
				[paragraph([link("Old guide")])],
				[paragraph([link("New guide")])],
			),
		).toEqual([
			{ type: "removed", text: "Old guide" },
			{ type: "added", text: "New guide" },
		]);
	});

	it("keeps the plain-text diff convention for URL and style-only changes", () => {
		expect(
			diffBlockNoteContent(
				[paragraph([link("Guide", "https://example.com/old")])],
				[paragraph([{ type: "text", text: "Guide", styles: { bold: true } }])],
			),
		).toEqual([{ type: "unchanged", text: "Guide" }]);
	});

	it("keeps empty, non-text and ordinary text blocks supported", () => {
		expect(blockNoteToLines(null)).toEqual([]);
		expect(
			blockNoteToLines([
				paragraph([{ type: "text", text: "Plain" }]),
				paragraph([{ type: "link", href: "https://example.com", content: [] }]),
				paragraph([{ type: "custom", text: "not plain text" }]),
			]),
		).toEqual(["Plain", "", ""]);
	});
});

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
