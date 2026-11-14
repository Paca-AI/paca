import { describe, expect, it } from "vitest";
import {
	findSection,
	markdownHeadings,
	markdownLines,
	numberLines,
	outlineOf,
	parseLineRange,
} from "../../utils/markdown-sections.js";

const DOC = [
	"# Plan", // 1
	"", // 2
	"Intro text.", // 3
	"", // 4
	"## Setup", // 5
	"", // 6
	"Install it.", // 7
	"", // 8
	"### Linux", // 9
	"", // 10
	"apt install", // 11
	"", // 12
	"## Usage", // 13
	"", // 14
	"Run it.", // 15
	"", // 16
	"# Appendix", // 17
	"", // 18
	"More.", // 19
	"",
].join("\n");

describe("markdownLines", () => {
	it("drops the empty string after the final newline", () => {
		expect(markdownLines("a\nb\n")).toEqual(["a", "b"]);
		expect(markdownLines("a\nb")).toEqual(["a", "b"]);
		expect(markdownLines("")).toEqual([]);
	});
});

describe("outline", () => {
	it("lists nested headings with their line numbers and the line count", () => {
		expect(markdownHeadings(DOC)).toEqual([
			{ line: 1, level: 1, text: "Plan" },
			{ line: 5, level: 2, text: "Setup" },
			{ line: 9, level: 3, text: "Linux" },
			{ line: 13, level: 2, text: "Usage" },
			{ line: 17, level: 1, text: "Appendix" },
		]);
		expect(outlineOf(DOC)).toBe(
			[
				"1: # Plan",
				"5: ## Setup",
				"9: ### Linux",
				"13: ## Usage",
				"17: # Appendix",
				"",
				"19 lines",
			].join("\n"),
		);
	});

	it("ignores headings inside fenced code", () => {
		const md = [
			"# Real",
			"",
			"```bash",
			"# a shell comment",
			"```",
			"",
			"~~~~",
			"## also code",
			"```",
			"# still code: a shorter or different fence does not close it",
			"~~~~",
			"",
			"## After",
			"",
		].join("\n");
		expect(markdownHeadings(md).map((h) => h.text)).toEqual(["Real", "After"]);
	});

	it("reads closing hashes and up to three spaces of indent", () => {
		expect(
			markdownHeadings("  ## Title ##\n#NoSpace\n####### seven\n"),
		).toEqual([{ line: 1, level: 2, text: "Title" }]);
	});

	it("says when there are no headings", () => {
		expect(outlineOf("just text\n")).toBe("(no headings)\n\n1 lines");
	});
});

describe("findSection", () => {
	it("runs from the heading to the next heading of the same or higher level", () => {
		expect(findSection(DOC, "setup")).toEqual({
			status: "found",
			heading: { line: 5, level: 2, text: "Setup" },
			start: 5,
			end: 12,
		});
		expect(findSection(DOC, "Linux")).toMatchObject({ start: 9, end: 12 });
		expect(findSection(DOC, "PLAN")).toMatchObject({ start: 1, end: 16 });
		// The last section runs to the end of the document.
		expect(findSection(DOC, "Appendix")).toMatchObject({ start: 17, end: 19 });
	});

	it("matches the heading text with or without its hashes", () => {
		expect(findSection(DOC, "## Usage")).toMatchObject({ start: 13, end: 16 });
	});

	it("lists every heading when the name is ambiguous", () => {
		const md = "# A\n\n## Notes\n\nx\n\n# B\n\n## notes\n\ny\n";
		expect(findSection(md, "Notes")).toEqual({
			status: "ambiguous",
			matches: [
				{ line: 3, level: 2, text: "Notes" },
				{ line: 9, level: 2, text: "notes" },
			],
		});
	});

	it("reports a missing section", () => {
		expect(findSection(DOC, "Nope")).toEqual({ status: "not_found" });
	});
});

describe("parseLineRange", () => {
	it("clamps a range to the document", () => {
		expect(parseLineRange("3-5", 19)).toEqual({ start: 3, end: 5 });
		expect(parseLineRange("15-80", 19)).toEqual({ start: 15, end: 19 });
		expect(parseLineRange("0-2", 19)).toEqual({ start: 1, end: 2 });
		expect(parseLineRange("7", 19)).toEqual({ start: 7, end: 7 });
		expect(parseLineRange("12-", 19)).toEqual({ start: 12, end: 19 });
		expect(parseLineRange(" 4 - 6 ", 19)).toEqual({ start: 4, end: 6 });
	});

	it("reports a range past the end or backwards", () => {
		expect(parseLineRange("40-50", 19)).toEqual({
			error: "Line 40 is past the end: the document has 19 lines.",
		});
		expect(parseLineRange("9-3", 19)).toEqual({
			error: 'Invalid line range "9-3": the start is after the end.',
		});
	});

	it("rejects text that is not a range", () => {
		expect(parseLineRange("a-b", 19)).toHaveProperty("error");
		expect(parseLineRange("", 19)).toHaveProperty("error");
	});
});

describe("numberLines", () => {
	it("prefixes each line with its number", () => {
		expect(numberLines(["a", "b"], 9)).toBe("9: a\n10: b");
	});
});

describe("review fixes", () => {
	it("line 0 alone is not a range", () => {
		expect(parseLineRange("0", 10)).toHaveProperty("error");
		expect(parseLineRange("0-0", 10)).toHaveProperty("error");
		expect(parseLineRange("0-3", 10)).toEqual({ start: 1, end: 3 });
	});

	it("finds a section whose heading has emphasis", () => {
		const md = "## Step **one**\n\nbody\n\n## Step _two_\n\nmore\n";
		expect(findSection(md, "Step one")).toMatchObject({
			status: "found",
			start: 1,
			end: 4,
		});
		expect(findSection(md, "step **two**")).toMatchObject({ start: 5 });
	});
});
