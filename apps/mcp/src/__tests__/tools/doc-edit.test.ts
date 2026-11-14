import { describe, expect, it } from "vitest";
import { SpliceError, spliceEdit } from "../../tools/doc-edit.js";
import {
	blocknoteToMarkdown,
	markdownToBlocknote,
} from "../../utils/converters.js";

/** Blocks the way the API returns them: parsed from Markdown, then passed
 *  through JSON, with a few props Markdown cannot express. */
function stored(markdown: string, extra?: (blocks: any[]) => void): any[] {
	const blocks = JSON.parse(JSON.stringify(markdownToBlocknote(markdown)));
	extra?.(blocks);
	return blocks;
}

/** Asserts every block outside `changed` is the very object passed in and
 *  serializes to the JSON it had before the edit. */
function expectUntouched(
	before: string[],
	input: any[],
	output: any[],
	changed: number[],
) {
	expect(output.length).toBe(input.length);
	for (let i = 0; i < input.length; i++) {
		if (changed.includes(i)) continue;
		expect(output[i]).toBe(input[i]);
		expect(output[i]).toEqual(JSON.parse(before[i]));
		expect(JSON.stringify(output[i])).toBe(before[i]);
	}
}

const TEN = [
	"# Release plan",
	"Intro paragraph with **bold** text.",
	"## Steps",
	"1. Build the image\n2. Push the image\n3. Deploy",
	"- [ ] write notes\n- [x] tag release",
	"| env | url |\n|---|---|\n| prod | example.com |",
	"```bash\n# not a heading\nmake deploy\n```",
	"> Careful with prod.",
	"## Rollback",
	"Run the previous tag and watch the dashboard.",
].join("\n\n");

describe("spliceEdit", () => {
	it("one-line edit leaves other blocks deep-equal and identical in JSON", () => {
		const blocks = stored(TEN, (b) => {
			b[1].props.textColor = "red";
			b[0].props.backgroundColor = "blue";
		});
		const before = blocks.map((b) => JSON.stringify(b));
		const target = blocks.findIndex(
			(b) => b.type === "paragraph" && /previous tag/.test(JSON.stringify(b)),
		);

		const res = spliceEdit(blocks, "previous tag", "last good tag", false);

		expect(res.replaced).toBe(1);
		expectUntouched(before, blocks, res.blocks, [target]);
		expect(res.blocks[target]).not.toBe(blocks[target]);
		expect(res.blocks[target].id).toBe(blocks[target].id);
		expect(blocknoteToMarkdown([res.blocks[target]])).toBe(
			"Run the last good tag and watch the dashboard.\n",
		);
		expect(blocknoteToMarkdown(res.blocks)).toBe(
			blocknoteToMarkdown(blocks).replace("previous tag", "last good tag"),
		);
	});

	it("keeps Markdown-less props of the edited block", () => {
		const blocks = stored(TEN, (b) => {
			b[1].props.textColor = "red";
		});
		const res = spliceEdit(
			blocks,
			"Intro paragraph",
			"Opening paragraph",
			false,
		);
		expect(res.blocks[1].props.textColor).toBe("red");
		expect(res.blocks[1].id).toBe(blocks[1].id);
	});

	it("edit spanning two blocks rewrites only those two", () => {
		const blocks = stored(TEN);
		const before = blocks.map((b) => JSON.stringify(b));
		const last = blocks.length - 1;

		const res = spliceEdit(
			blocks,
			"## Rollback\n\nRun the previous",
			"## Roll back\n\nRedeploy the previous",
			false,
		);

		expectUntouched(before, blocks, res.blocks, [last - 1, last]);
		expect(blocknoteToMarkdown(res.blocks.slice(last - 1))).toBe(
			"## Roll back\n\nRedeploy the previous tag and watch the dashboard.\n",
		);
	});

	it("edits one item of a numbered list without renumbering or touching the others", () => {
		const blocks = stored(TEN);
		const before = blocks.map((b) => JSON.stringify(b));
		const items = blocks
			.map((b, i) => (b.type === "numberedListItem" ? i : -1))
			.filter((i) => i >= 0);
		expect(items).toHaveLength(3);

		const res = spliceEdit(blocks, "Push the image", "Push the tag", false);

		expectUntouched(before, blocks, res.blocks, [items[1]]);
		expect(res.blocks[items[1]].props).toEqual(blocks[items[1]].props);
		expect(blocknoteToMarkdown(res.blocks)).toContain(
			"1. Build the image\n\n2. Push the tag\n\n3. Deploy\n",
		);
	});

	it("finds text that includes the list number", () => {
		const blocks = stored(TEN);
		const res = spliceEdit(blocks, "3. Deploy", "3. Deploy to prod", false);
		expect(blocknoteToMarkdown(res.blocks)).toContain("3. Deploy to prod\n");
	});

	it("keeps check items and an empty paragraph it did not touch", () => {
		const blocks = stored("- [ ] one\n- [x] two\n- [ ] three\n\nafter", (b) => {
			b.splice(3, 0, {
				id: "empty",
				type: "paragraph",
				props: {},
				content: [],
				children: [],
			});
		});
		const before = blocks.map((b) => JSON.stringify(b));

		const res = spliceEdit(blocks, "three", "3", false);

		expectUntouched(before, blocks, res.blocks, [2]);
		expect(res.blocks[2].props.checked).toBe(false);
	});

	it("keeps a mention chip in a block it did not touch", () => {
		const blocks = stored("first para\n\nsecond para", (b) => {
			b[1].content.push({
				type: "teamMention",
				props: { id: "u1", name: "Ana", avatar: "" },
			});
		});
		const before = blocks.map((b) => JSON.stringify(b));
		const res = spliceEdit(blocks, "first", "1st", false);
		expectUntouched(before, blocks, res.blocks, [0]);
	});

	it("zero matches is an error that says so", () => {
		const blocks = stored(TEN);
		expect(() => spliceEdit(blocks, "nowhere", "x", false)).toThrow(
			SpliceError,
		);
		try {
			spliceEdit(blocks, "nowhere", "x", false);
		} catch (err) {
			expect((err as SpliceError).kind).toBe("not_found");
			expect((err as Error).message).toMatch(/not found/i);
		}
	});

	it("multiple matches without replaceAll is an error listing the lines", () => {
		const blocks = stored(TEN);
		try {
			spliceEdit(blocks, "the image", "it", false);
			expect.unreachable();
		} catch (err) {
			expect(err).toBeInstanceOf(SpliceError);
			expect((err as SpliceError).kind).toBe("ambiguous");
			// Lines of the full Markdown: "1. Build the image" and "2. Push the image".
			const md = blocknoteToMarkdown(blocks).split("\n");
			const want = md
				.map((l, i) => (l.includes("the image") ? i + 1 : 0))
				.filter(Boolean);
			expect((err as SpliceError).lines).toEqual(want);
			expect((err as Error).message).toContain(`lines ${want.join(", ")}`);
		}
	});

	it("replaceAll replaces every match and leaves the rest alone", () => {
		const blocks = stored(TEN);
		const before = blocks.map((b) => JSON.stringify(b));
		const items = blocks
			.map((b, i) => (b.type === "numberedListItem" ? i : -1))
			.filter((i) => i >= 0);

		const res = spliceEdit(blocks, "the image", "the container", true);

		expect(res.replaced).toBe(2);
		expectUntouched(before, blocks, res.blocks, [items[0], items[1]]);
		expect(blocknoteToMarkdown(res.blocks)).toBe(
			blocknoteToMarkdown(blocks).replaceAll("the image", "the container"),
		);
	});

	it("deleting a whole block removes only that block", () => {
		const blocks = stored(TEN);
		const res = spliceEdit(blocks, "> Careful with prod.\n\n", "", false);
		expect(res.blocks.length).toBe(blocks.length - 1);
		const kept = blocks.filter((b) => b.type !== "quote");
		for (const [i, b] of kept.entries()) expect(res.blocks[i]).toBe(b);
	});

	it("does not edit the input array", () => {
		const blocks = stored(TEN);
		const copy = [...blocks];
		const json = JSON.stringify(blocks);
		spliceEdit(blocks, "Deploy", "Ship", false);
		expect(blocks).toEqual(copy);
		for (const [i, b] of blocks.entries()) expect(b).toBe(copy[i]);
		expect(JSON.stringify(blocks)).toBe(json);
	});
});
