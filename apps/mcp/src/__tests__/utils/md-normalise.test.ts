import { describe, expect, it } from "vitest";
import { blocknoteToMarkdown, markdownToBlocknote } from "../../utils/index.js";
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
	});

	it("changes nothing inside fenced code but line ends", () => {
		expect(norm("```\ncode   \n```\n")).toBe("```\ncode   \n```\n");
		expect(norm("```\r\nkeep  \r\n\t\r\n```\r\n")).toBe(
			"```\nkeep  \n\t\n```\n",
		);
		expect(norm("```\nkeep  \n```\n")).not.toBe(norm("```\nkeep\n```\n"));
		// An unclosed fence keeps its lines too.
		expect(norm("```\nx  \n")).toBe("```\nx  \n");
	});

	it("drops only spaces and tabs at the end of a line", () => {
		// A non-breaking space is text.
		expect(norm("Price: 5\u00a0\n")).toBe("Price: 5\u00a0\n");
		expect(norm("Price: 5\u00a0\n")).not.toBe(norm("Price: 5\n"));
		expect(norm("a\n\u00a0\n\nb\n")).toBe("a\n\u00a0\n\nb\n");
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
		expect(norm("| a |\n|---|\n\n|  x |\n|:---:|\n")).toBe(
			"| a |\n| - |\n\n| x |\n| :-: |\n",
		);
		// An escaped "|" stays in its cell.
		expect(norm("|  a \\| b  |  c |\n")).toBe("| a \\| b | c |\n");
		// Empty cells count.
		expect(norm("| a |  | c |\n")).toBe("| a |  | c |\n");
		expect(norm("```\n|  a |\n```\n")).toBe("```\n|  a |\n```\n");
	});

	it("reads only a table's second row as its delimiter row", () => {
		// Data rows of dashes are text.
		expect(norm("| a | b |\n| --- | --- |\n| - | - |\n")).toBe(
			"| a | b |\n| - | - |\n| - | - |\n",
		);
		expect(norm("| a | b |\n| --- | --- |\n| - | - |\n")).not.toBe(
			norm("| a | b |\n| --- | --- |\n| --- | :-: |\n"),
		);
		expect(norm("| --- |\n| a |\n")).not.toBe(norm("| - |\n| a |\n"));
		// A fence ends a table.
		expect(norm("| a |\n```\n| --- |\n```\n| b |\n| --- |\n")).toBe(
			"| a |\n```\n| --- |\n```\n| b |\n| - |\n",
		);
	});

	it("reads blank lines between two list items as not there", () => {
		expect(norm("* a\n* b\n")).toBe(norm("* a\n\n* b\n"));
		expect(norm("* two\n  * nested\n* three\n")).toBe(
			norm("* two\n\n  * nested\n\n* three\n"),
		);
		expect(norm("1. a\n2. b\n")).toBe(norm("1. a\n\n2. b\n"));
		// Next to a checklist item a blank line changes the parse.
		expect(norm("- [ ] a\n- [x] b\n")).not.toBe(norm("* [ ] a\n\n* [x] b\n"));
		expect(norm("* a\n* [ ] b\n")).not.toBe(norm("* a\n\n* [ ] b\n"));
		// Not between a paragraph and a list, nor inside fenced code.
		expect(norm("a\n* b\n")).not.toBe(norm("a\n\n* b\n"));
		expect(norm("```\n* a\n* b\n```\n")).not.toBe(
			norm("```\n* a\n\n* b\n```\n"),
		);
	});

	it("reads a nested item's indent as the renderer writes it when it parses the same", () => {
		const render = "* two\n\n  * nested\n";
		expect(norm("* two\n\t* nested\n")).toBe(norm(render));
		expect(norm("* two\n    * nested\n")).toBe(norm(render));
		expect(norm("* two\n   * nested\n")).toBe(norm(render));
		expect(norm("1. a\n    1. b\n")).toBe(norm("1. a\n\n   1. b\n"));
		// One space is not enough to nest under "* ": a sibling.
		expect(norm("* two\n * nested\n")).not.toBe(norm(render));
		expect(norm("* two\n * nested\n")).toBe(norm("* two\n\n* nested\n"));
		// After text at the top level, an indented item starts no nesting.
		expect(norm("* a\n\npara\n\n  * b\n")).toBe(norm("* a\n\npara\n\n* b\n"));
		// A lazy line leaves the list unknown: indents are read as written.
		expect(norm("* a\nlazy\n  * b\n")).not.toBe(norm("* a\nlazy\n* b\n"));
	});

	it("leaves a thematic break's characters alone", () => {
		expect(norm("- - -\n")).not.toBe(norm("* - -\n"));
		expect(norm("- - -\n")).toBe("- - -\n");
		expect(norm("_ _ _\n")).toBe("_ _ _\n");
		expect(norm("* * *\n")).toBe("* * *\n");
	});

	it("reads list blocks the same however blank lines and nesting indents are written", () => {
		let seed = Number(process.env.FUZZ_SEED) || 5;
		const rnd = (k: number) => {
			seed = (seed * 1103515245 + 12345) % 2147483648;
			return Math.floor((seed / 2147483648) * k);
		};
		const shape = (md: string) =>
			JSON.stringify(markdownToBlocknote(md), (k, v) =>
				k === "id" ? undefined : v,
			);
		for (let n = 0; n < 200; n++) {
			const lines: string[] = [];
			let depth = 0;
			/** Per depth: the last item's kind and, if numbered, its number. */
			const last: Array<{ kind: number; num: number }> = [];
			for (let i = 0; i < 2 + rnd(6); i++) {
				depth = Math.max(0, Math.min(depth + rnd(3) - 1, 2));
				if (i === 0) depth = 0;
				last.length = depth + 1;
				const kind = rnd(4);
				// Numbered items count on from the item before at their depth,
				// as the render numbers them.
				const num =
					last[depth]?.kind === 2 && kind === 2 ? last[depth].num + 1 : 1;
				last[depth] = { kind, num };
				const marker = ["*", "-", `${num}.`, "* [ ]"][kind];
				lines.push(`${"\t".repeat(depth)}${marker} item ${i}`);
			}
			const md = `${lines.join("\n")}\n`;
			const render = blocknoteToMarkdown(
				JSON.parse(JSON.stringify(markdownToBlocknote(md))),
			);
			// The same items with no blank lines, nested with tabs or with
			// four spaces.
			for (const file of [md, md.replace(/\t/g, "    ")]) {
				const what = JSON.stringify({ render, file });
				const same = shape(file) === shape(render);
				// Never read as the render when it parses differently.
				if (norm(file) === norm(render)) expect(same, what).toBe(true);
				// Read as the render when it parses the same, checklist items
				// aside (a blank line next to one changes the parse).
				if (same && !file.includes("[ ]"))
					expect(norm(file), what).toBe(norm(render));
			}
		}
	});

	it("never reads a file one visible character away from the render as the render", () => {
		let seed = Number(process.env.FUZZ_SEED) || 7;
		const rnd = (k: number) => {
			seed = (seed * 1103515245 + 12345) % 2147483648;
			return Math.floor((seed / 2147483648) * k);
		};
		const pieces = [
			() => `para ${rnd(100)} text`,
			() => `# head ${rnd(100)}`,
			() => `* item ${rnd(100)}\n* item two`,
			() => `1. one ${rnd(100)}\n2. two`,
			() => `- [ ] todo ${rnd(100)}`,
			() => `> quote ${rnd(100)}`,
			() => `| a | b |\n| --- | :-: |\n| - | x${rnd(100)} |\n| y | --- |`,
			() => `\`\`\`ts\ncode ${rnd(100)}  \n  - x\n\n\`\`\``,
			() => "---",
			() => `**bold** _em_ \`span\` ${rnd(100)}`,
		];
		const ALNUM =
			"abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789";
		let checked = 0;
		for (let n = 0; n < 400; n++) {
			const md = Array.from({ length: 1 + rnd(6) }, () =>
				pieces[rnd(pieces.length)](),
			).join("\n\n");
			const render = blocknoteToMarkdown(
				JSON.parse(JSON.stringify(markdownToBlocknote(md))),
			);
			const lines = render.split("\n");
			const spots = lines.flatMap((l, i) =>
				[...l].flatMap((c, j) => (/\s/.test(c) ? [] : [[i, j] as const])),
			);
			if (spots.length === 0) continue;
			const [i, j] = spots[rnd(spots.length)];
			let c = ALNUM[rnd(ALNUM.length)];
			if (c === lines[i][j]) c = c === "q" ? "Q" : "q";
			// Replace the character, or put one before it.
			const line =
				rnd(2) === 0
					? lines[i].slice(0, j) + c + lines[i].slice(j + 1)
					: lines[i].slice(0, j) + c + lines[i].slice(j);
			const file = [...lines.slice(0, i), line, ...lines.slice(i + 1)].join(
				"\n",
			);
			expect(norm(file), JSON.stringify({ render, file })).not.toBe(
				norm(render),
			);
			checked++;
		}
		expect(checked).toBeGreaterThan(350);
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
