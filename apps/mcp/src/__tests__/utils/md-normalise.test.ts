import { describe, expect, it } from "vitest";
import {
	fencedLines,
	lineKeys,
	normaliseMarkdown,
} from "../../utils/md-normalise.js";

const norm = (x: string) => normaliseMarkdown(x).text;

describe("normaliseMarkdown", () => {
	it("drops a byte order mark, CRLF and the newlines at the end", () => {
		expect(norm("﻿# A\r\n\r\nb\r\n\r\n\r\n")).toBe("# A\n\nb\n");
		expect(norm("a")).toBe("a\n");
		expect(norm("")).toBe("");
		expect(norm("\n\n")).toBe("");
	});

	it("drops whitespace at the end of lines", () => {
		expect(norm("a  \n\t\nb\t\n")).toBe("a\n\nb\n");
		expect(norm("```\ncode   \n```\n")).toBe("```\ncode\n```\n");
	});

	it("collapses runs of blank lines outside fenced code only", () => {
		expect(norm("a\n\n\n\nb\n")).toBe("a\n\nb\n");
		expect(norm("a\n  \n\t\n\nb\n")).toBe("a\n\nb\n");
		expect(norm("```\nx\n\n\n\ny\n```\n")).toBe("```\nx\n\n\n\ny\n```\n");
		expect(norm("```\nx\n```\n\n\n\ny\n")).toBe("```\nx\n```\n\ny\n");
		// One blank line is not a run: a leading one stays.
		expect(norm("\na\n")).toBe("\na\n");
		expect(norm("a\nb\n")).toBe("a\nb\n");
	});

	it('reads "-" and "+" bullets as "*" outside fenced code', () => {
		expect(norm("- a\n+ b\n* c\n  - nested\n- [ ] todo\n")).toBe(
			"* a\n* b\n* c\n  * nested\n* [ ] todo\n",
		);
		expect(norm("```\n- a\n+ b\n```\n")).toBe("```\n- a\n+ b\n```\n");
		// Not bullets: a thematic break, a setext underline, signs in text.
		expect(norm("---\n-\n-1 point\n+5\na - b\n")).toBe(
			"---\n-\n-1 point\n+5\na - b\n",
		);
	});

	it("reads table rows with cells trimmed and the delimiter row as its alignment", () => {
		expect(
			norm(
				"| name       | state |\n| ---------- | ----- |\n| alpha long | done  |\n",
			),
		).toBe("| name | state |\n| - | - |\n| alpha long | done |\n");
		expect(norm("|name|state|\n|:---|---:|\n|a|b|\n")).toBe(
			"| name | state |\n| :- | -: |\n| a | b |\n",
		);
		expect(norm("| :-: |\n")).toBe("| :-: |\n");
		// An escaped "|" stays in its cell.
		expect(norm("|  a \\| b  |  c |\n")).toBe("| a \\| b | c |\n");
		// Empty cells count.
		expect(norm("| a |  | c |\n")).toBe("| a |  | c |\n");
		expect(norm("```\n|  a |\n```\n")).toBe("```\n|  a |\n```\n");
	});

	it("changes nothing else", () => {
		for (const x of [
			"1) one\n",
			"1. one\n",
			"__bold__\n",
			"_em_\n",
			"Title\n=====\n",
			"a\\\nb\n",
			"  indented\n",
			"a | b\n",
			"> quote\n",
		])
			expect(norm(x)).toBe(x);
		// Different text stays different.
		expect(norm("| a | b |\n")).not.toBe(norm("| a | c |\n"));
		expect(norm("| a | b |\n")).not.toBe(norm("| a b |\n"));
		expect(norm("| --- |\n")).not.toBe(norm("| :-- |\n"));
		expect(norm("1. a\n")).not.toBe(norm("* a\n"));
		expect(norm("a\nb\n")).not.toBe(norm("a\n\nb\n"));
	});

	it("maps each line back to the line it came from", () => {
		const r = normaliseMarkdown("﻿a\r\n\r\n\r\n\r\nb\r\n```\n\n\n```\n");
		expect(r.lines).toEqual(["a", "", "b", "```", "", "", "```"]);
		expect(r.from).toEqual([1, 2, 5, 6, 7, 8, 9]);
	});
});

describe("lineKeys and fencedLines", () => {
	it("keys one for one, without collapsing", () => {
		expect(lineKeys(["- a", "", "", "|x|"])).toEqual(["* a", "", "", "| x |"]);
	});
	it("finds fenced code", () => {
		expect(
			fencedLines([
				"a",
				"```js",
				"- x",
				"```",
				"b",
				"~~~~",
				"~~~",
				"~~~~",
				"c",
			]),
		).toEqual([false, true, true, true, false, true, true, true, false]);
		// An inline code span is not a fence; an unclosed fence runs on.
		expect(fencedLines(["```a`b```", "x", "````", "```", "y"])).toEqual([
			false,
			false,
			true,
			true,
			true,
		]);
	});
});
