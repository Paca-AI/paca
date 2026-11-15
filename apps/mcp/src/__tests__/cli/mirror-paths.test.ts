import path from "node:path";
import { describe, expect, it } from "vitest";
import {
	folderDirs,
	isInside,
	mirrorFileFor,
	nameKey,
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
		["CON", "_CON"],
		["nul.txt", "_nul.txt"],
		["com1", "_com1"],
		["Console", "Console"],
		["Plan.", "Plan_"],
		["a. ", "a__"],
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

describe("folder paths and Unicode", () => {
	it("compares names after Unicode normalization", () => {
		const taken = new Set<string>();
		const a = mirrorFileFor(
			{ id: ID1, title: "caf\u00e9", folder_id: null },
			{},
			taken,
		);
		const b = mirrorFileFor(
			{ id: ID2, title: "cafe\u0301", folder_id: null },
			{},
			taken,
		);
		expect(nameKey(a)).not.toBe(nameKey(b));
		expect(b).toBe("cafe\u0301~22222222.md");
	});

	it("gives a folder a suffix when its path is a file", () => {
		const folders = {
			"fsub-1234": { parent_id: null, name: "Sub" },
			child: { parent_id: "fsub-1234", name: "In" },
		};
		const dirs = folderDirs(folders, new Set([nameKey("Sub")]));
		expect(dirs["fsub-1234"]).toBe("Sub~fsub-123");
		expect(dirs.child).toBe("Sub~fsub-123/In");
		expect(
			mirrorFileFor(
				{ id: ID1, title: "t", folder_id: "child" },
				folders,
				new Set(),
				dirs,
			),
		).toBe("Sub~fsub-123/In/t.md");
	});
});

describe("folderDirs for sibling folders with one name key", () => {
	const FA = "aaaaaaaa-0000-4000-8000-000000000001";
	const FB = "bbbbbbbb-0000-4000-8000-000000000002";

	it.each([
		["case", "Design", "design"],
		["Unicode normalization", "caf\u00e9", "cafe\u0301"],
		["characters mapped to _", "a:b", "a?b"],
	])("gives the higher id a suffix (%s)", (_why, a, b) => {
		const dirs = folderDirs(
			{
				[FB]: { parent_id: null, name: b },
				[FA]: { parent_id: null, name: a },
			},
			new Set(),
		);
		expect(dirs[FA]).toBe(sanitizeSegment(a));
		expect(dirs[FB]).toBe(`${sanitizeSegment(b)}~bbbbbbbb`);
	});

	it("puts child folders and documents under the suffixed folder", () => {
		const folders = {
			[FA]: { parent_id: null, name: "Design" },
			[FB]: { parent_id: null, name: "design" },
			child: { parent_id: FB, name: "In" },
		};
		const dirs = folderDirs(folders, new Set());
		expect(dirs.child).toBe("design~bbbbbbbb/In");
		expect(
			mirrorFileFor(
				{ id: ID1, title: "t", folder_id: "child" },
				folders,
				new Set(),
				dirs,
			),
		).toBe("design~bbbbbbbb/In/t.md");
	});

	it("a folder keeps its previous directory when it is still valid", () => {
		const dirs = folderDirs(
			{
				[FA]: { parent_id: null, name: "Design" },
				[FB]: { parent_id: null, name: "design" },
			},
			new Set(),
			{ [FB]: "design", [FA]: "Design~aaaaaaaa" },
		);
		expect(dirs).toEqual({ [FB]: "design", [FA]: "Design~aaaaaaaa" });
	});

	it("a previous directory that no longer fits is not kept", () => {
		const dirs = folderDirs(
			{
				[FA]: { parent_id: null, name: "Design" },
				[FB]: { parent_id: null, name: "Renamed" },
			},
			new Set(),
			// FB was renamed; FA had the suffix only because of the clash.
			{ [FB]: "design", [FA]: "Design~aaaaaaaa" },
		);
		expect(dirs[FB]).toBe("Renamed");
		expect(dirs[FA]).toBe("Design~aaaaaaaa");
	});

	it("never gives two folders one directory", () => {
		const dirs = folderDirs(
			{
				[FA]: { parent_id: null, name: "x" },
				[FB]: { parent_id: null, name: "X" },
				c: { parent_id: null, name: "x~aaaaaaaa" },
			},
			new Set([nameKey("x~bbbbbbbb")]),
			{ c: "x~aaaaaaaa" },
		);
		const keys = Object.values(dirs).map(nameKey);
		expect(new Set(keys).size).toBe(keys.length);
		expect(dirs.c).toBe("x~aaaaaaaa");
		expect(dirs[FA]).toBe("x");
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
