import path from "node:path";
import { describe, expect, it } from "vitest";
import {
	isInside,
	mirrorFileFor,
	sanitizeSegment,
} from "../../cli/mirror-paths.js";

const ID1 = "11111111-aaaa-4bbb-8ccc-000000000001";
const ID2 = "22222222-aaaa-4bbb-8ccc-000000000002";

describe("sanitizeSegment", () => {
	it.each([
		["a:b", "a_b"],
		["a/b\\c", "a_b_c"],
		['*?"<>|', "______"],
		["tab\there\u0000\u007f", "tab_here__"],
		[".", "_"],
		["..", "_"],
		["", "_"],
		["   ", "_"],
		["Plan v2.1", "Plan v2.1"],
	])("%j becomes %j", (input, want) => {
		expect(sanitizeSegment(input)).toBe(want);
	});

	it("keeps long names under the file name limit", () => {
		expect(
			Buffer.byteLength(sanitizeSegment("é".repeat(300))),
		).toBeLessThanOrEqual(200);
	});
});

describe("mirrorFileFor", () => {
	const folders = {
		f1: { parent_id: null, name: "Plans" },
		f2: { parent_id: "f1", name: "Q:4" },
		loop1: { parent_id: "loop2", name: "A" },
		loop2: { parent_id: "loop1", name: "B" },
	};

	it("builds the folder path and the title", () => {
		const taken = new Set<string>();
		expect(
			mirrorFileFor({ id: ID1, title: "x", folder_id: "f2" }, folders, taken),
		).toBe("Plans/Q_4/x.md");
		expect(taken.has("plans/q_4/x.md")).toBe(true);
	});

	it.each([
		["a:b", "a_b.md"],
		["..", "_.md"],
		["../../etc/passwd", ".._.._etc_passwd.md"],
	])("unsafe title %j stays in the mirror as %j", (title, want) => {
		const file = mirrorFileFor(
			{ id: ID1, title, folder_id: null },
			folders,
			new Set(),
		);
		expect(file).toBe(want);
		expect(isInside("/m", path.join("/m", file))).toBe(true);
	});

	it("gives a second doc with the same title (any case) a suffix", () => {
		const taken = new Set<string>();
		const a = mirrorFileFor(
			{ id: ID1, title: "Notes", folder_id: null },
			{},
			taken,
		);
		const b = mirrorFileFor(
			{ id: ID2, title: "notes", folder_id: null },
			{},
			taken,
		);
		expect(a).toBe("Notes.md");
		expect(b).toBe("notes~22222222.md");
	});

	it("does not collide a doc with a folder of the same name", () => {
		const taken = new Set<string>(["x.md"]); // a folder called "X.md"
		expect(
			mirrorFileFor({ id: ID1, title: "X", folder_id: null }, {}, taken),
		).toBe("X~11111111.md");
		// A doc X beside folder X: X.md and X/ do not collide.
		expect(
			mirrorFileFor(
				{ id: ID2, title: "Plans", folder_id: null },
				folders,
				taken,
			),
		).toBe("Plans.md");
	});

	it("keeps names that start with .paca- for the mirror's own files", () => {
		const folder = { r: { parent_id: null, name: ".PACA-base" } };
		expect(
			mirrorFileFor({ id: ID1, title: "t", folder_id: "r" }, folder, new Set()),
		).toBe("_.PACA-base/t.md");
		expect(
			mirrorFileFor(
				{ id: ID1, title: ".paca-sync.json", folder_id: null },
				{},
				new Set(),
			),
		).toBe("_.paca-sync.json.md");
	});

	it("survives a folder cycle and a missing parent", () => {
		const f = mirrorFileFor(
			{ id: ID1, title: "t", folder_id: "loop1" },
			folders,
			new Set(),
		);
		expect(f.endsWith("/t.md")).toBe(true);
		expect(
			mirrorFileFor(
				{ id: ID2, title: "t", folder_id: "gone" },
				folders,
				new Set(),
			),
		).toBe("t.md");
	});
});

describe("isInside", () => {
	it.each([
		["/m", "/m/a.md", true],
		["/m", "/m/sub/a.md", true],
		["/m", "/m", false],
		["/m", "/m/../x", false],
		["/m", "/mx/a.md", false],
	])("%s contains %s: %s", (dir, p, want) => {
		expect(isInside(dir, p)).toBe(want);
	});
});
