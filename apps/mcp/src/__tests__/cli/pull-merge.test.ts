import { createHash } from "node:crypto";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import type { GitRunner } from "../../cli/merge.js";
import { runPull } from "../../cli/pull.js";
import { type PushOptions, runPush } from "../../cli/push.js";
import { blocknoteToMarkdown } from "../../utils/index.js";
import { docId, fakeDocsServer, storedBlocks } from "./fake-docs-server.js";

const ID = docId(1);
const sha = (x: string) => createHash("sha256").update(x).digest("hex");

const DOC = [
	"# Plan",
	"First paragraph.",
	"Red paragraph.",
	"Third paragraph.",
	"- one\n- two",
	"Last paragraph.",
].join("\n\n");

/** Ann's edit in Paca: the first paragraph's text. */
const annEdits = (text: string) => (b: any[]) => {
	b[1].content[0].text = text;
};

describe("pull merges a file edited here with changes made in Paca", () => {
	let root: string;
	let mirror: string;
	let lines: string[];
	let server: ReturnType<typeof fakeDocsServer>;
	const io = {
		out: (l: string) => lines.push(l),
		err: (l: string) => lines.push(`ERR ${l}`),
	};
	const output = () => lines.join("\n");
	const pull = (extra: { force?: boolean; git?: GitRunner } = {}) =>
		runPull(
			{ projectId: "p1", dir: mirror, force: false, ...extra },
			server.client,
			io,
		);
	const push = (
		file = ".paca/docs/Plan.md",
		extra: Partial<PushOptions> = {},
	) =>
		runPush(
			{
				projectId: "p1",
				file,
				lock: false,
				force: false,
				dryRun: false,
				client: "paca-cli",
				...extra,
			},
			server.client as any,
			io,
			{ cwd: root },
		);
	const file = (rel = "Plan.md") => path.join(mirror, rel);
	const read = (rel = "Plan.md") => fs.readFileSync(file(rel), "utf8");
	const write = (text: string, rel = "Plan.md") =>
		fs.writeFileSync(file(rel), text);
	const basePath = (id = ID) => path.join(mirror, ".paca-base", `${id}.md`);
	const state = () =>
		JSON.parse(fs.readFileSync(path.join(mirror, ".paca-sync.json"), "utf8"));
	const stored = (id = ID) => server.docs.get(id)?.content as any[];
	const render = (id = ID) => blocknoteToMarkdown(stored(id));

	beforeEach(async () => {
		root = fs.realpathSync(fs.mkdtempSync(path.join(os.tmpdir(), "paca-pm-")));
		mirror = path.join(root, ".paca", "docs");
		lines = [];
		server = fakeDocsServer();
		server.add(
			ID,
			"Plan",
			storedBlocks(DOC, (b) => {
				b[2].props.textColor = "red";
			}),
		);
		expect(await pull()).toBe(0);
		lines = [];
	});
	afterEach(() => fs.rmSync(root, { recursive: true, force: true }));

	it("clean merge of edits in different paragraphs", async () => {
		write(read().replace("Third paragraph.", "Third paragraph, mine."));
		server.edit(ID, annEdits("First paragraph, by Ann."), {
			updated_by: "ann",
			updated_at: "2026-11-15T09:30:00Z",
		});
		expect(await pull(), output()).toBe(0);
		expect(output()).toBe(
			"1 merged\nmerged Plan.md: your edits and the changes in Paca",
		);
		const both = render().replace("Third paragraph.", "Third paragraph, mine.");
		expect(both).toContain("First paragraph, by Ann.");
		expect(read()).toBe(both);
		// Base and state are Paca's version; the file is a local edit of it.
		expect(fs.readFileSync(basePath(), "utf8")).toBe(render());
		expect(state().docs[ID]).toMatchObject({
			content_hash: server.docs.get(ID)?.hash,
			file_sha256: sha(render()),
		});
		expect(state().docs[ID].pending_sha256).toBeUndefined();
		// Then push sends only the local edit.
		const before = JSON.parse(JSON.stringify(stored()));
		lines = [];
		expect(await push(), output()).toBe(0);
		expect(render()).toBe(both);
		stored().forEach((b, i) => {
			if (i !== 3) expect(b, `block ${i}`).toEqual(before[i]);
		});
		expect(stored()[2].props.textColor).toBe("red");
	});

	it("a conflict leaves markers, keeps every local line, and exits 2; push refuses the file", async () => {
		// The whole file rewritten here: every line conflicts with Ann's.
		const mine = "# My plan\n\nNothing like the old one.\n";
		write(mine);
		server.edit(ID, annEdits("First paragraph, by Ann."), {
			updated_by: "ann",
			updated_at: "2026-11-15T09:30:00Z",
		});
		const pulled = fs.readFileSync(basePath(), "utf8");
		expect(await pull(), output()).toBe(2);
		expect(output()).toContain("1 conflicted");
		expect(output()).toContain(
			"conflict Plan.md: edited here and in Paca (ann, 2026-11-15T09:30:00Z); resolve the marked lines (1 conflict), then push",
		);
		// Each side, read back from the conflict file: the lines outside the
		// marked blocks plus that side's lines inside them.
		const side = (which: 0 | 1 | 2) => {
			const out: string[] = [];
			let at: number | null = null;
			for (const l of read().split("\n")) {
				if (l === "<<<<<<< local") at = 0;
				else if (l === "||||||| base" && at !== null) at = 1;
				else if (l === "=======" && at !== null) at = 2;
				else if (l.startsWith(">>>>>>> Paca") && at !== null) at = null;
				else if (at === null || at === which) out.push(l);
			}
			return out.join("\n");
		};
		expect(read()).toContain(">>>>>>> Paca (ann, 2026-11-15T09:30:00Z)\n");
		// The local file is all there, as written. (The base and Paca's
		// sides hold their text where it conflicts; lines only one side
		// changed are already merged outside the markers.)
		expect(side(0)).toBe(mine);
		expect(side(1)).toContain("First paragraph.");
		expect(side(2)).toContain("First paragraph, by Ann.");
		expect(pulled).toContain("First paragraph.");
		expect(fs.readFileSync(basePath(), "utf8")).toBe(render());
		expect(state().docs[ID].content_hash).toBe(server.docs.get(ID)?.hash);
		// push refuses it and sends nothing.
		lines = [];
		expect(await push()).toBe(1);
		const first = read().split("\n").indexOf("<<<<<<< local") + 1;
		expect(first).toBeGreaterThan(0);
		expect(output()).toContain(`has conflict markers (line ${first})`);
		expect(server.client.patchDocument).not.toHaveBeenCalled();
		// Resolved by keeping both, it pushes.
		write(render().replace("Last paragraph.", "Last paragraph, mine."));
		lines = [];
		expect(await push(), output()).toBe(0);
		expect(render()).toContain("First paragraph, by Ann.");
		expect(render()).toContain("Last paragraph, mine.");
	});

	it("a conflict inside fenced code is marked, flagged, refused by push and never stored", async () => {
		const CODE = docId(3);
		server.add(
			CODE,
			"Code",
			storedBlocks(
				"# Code\n\nIntro.\n\n```ts\nconst a = 1;\nconst b = 2;\n```\n\nEnd.",
			),
		);
		expect(await pull()).toBe(0);
		const codeText = () => server.docs.get(CODE)?.content as any[];
		write(read("Code.md").replace("const a = 1;", "const a = 10;"), "Code.md");
		server.edit(CODE, (b) => {
			b[2].content[0].text = b[2].content[0].text.replace(
				"const a = 1;",
				"const a = 2;",
			);
		});
		lines = [];
		expect(await pull(), output()).toBe(2);
		expect(output()).toContain("conflict Code.md: edited here and in Paca");
		expect(read("Code.md")).toContain(
			"```ts\n<<<<<<< local\nconst a = 10;\n||||||| base\nconst a = 1;\n=======\nconst a = 2;\n>>>>>>> Paca",
		);
		expect(state().docs[CODE].conflict).toBe(true);
		// Push refuses: the markers are inside the fence.
		const sent = server.client.patchDocument.mock.calls.length;
		lines = [];
		expect(await push(".paca/docs/Code.md"), output()).toBe(1);
		expect(output()).toContain("has conflict markers (line 6)");
		expect(server.client.patchDocument.mock.calls.length).toBe(sent);
		expect(JSON.stringify(codeText())).not.toContain("<<<<<<<");
		// Pull while the markers are there: still a conflict, not merged again.
		lines = [];
		expect(await pull(), output()).toBe(2);
		expect(output()).toBe(
			"1 conflicted\nconflict Code.md: the conflict markers from an earlier pull are still there (line 6); resolve them, then push",
		);
		// Resolved by hand: pull clears the flag, push stores the resolution.
		const resolved = read("Code.md").replace(
			/<<<<<<< local\n[^]*?>>>>>>> Paca[^\n]*\n/,
			"const a = 12;\n",
		);
		write(resolved, "Code.md");
		lines = [];
		expect(await pull(), output()).toBe(0);
		expect(output()).toBe("1 modified locally\nmodified locally Code.md");
		expect(state().docs[CODE].conflict).toBeUndefined();
		lines = [];
		expect(await push(".paca/docs/Code.md"), output()).toBe(0);
		expect(blocknoteToMarkdown(codeText())).toBe(resolved);
	});

	it("a conflict on a fence line leaves markers outside any fence's reach that push still refuses", async () => {
		const CODE = docId(3);
		server.add(
			CODE,
			"Code",
			storedBlocks("Intro.\n\n```ts\nconst a = 1;\nconst b = 2;\n```\n\nEnd."),
		);
		expect(await pull()).toBe(0);
		write(
			read("Code.md")
				.replace("```ts", "```js")
				.replace("const b = 2;", "const b = 3;"),
			"Code.md",
		);
		server.edit(CODE, (b) => {
			b[1].props.language = "python";
		});
		lines = [];
		expect(await pull(), output()).toBe(2);
		expect(state().docs[CODE].conflict).toBe(true);
		lines = [];
		expect(await push(".paca/docs/Code.md"), output()).toBe(1);
		expect(output()).toContain("has conflict markers");
		const md = blocknoteToMarkdown(server.docs.get(CODE)?.content as any[]);
		expect(md).not.toContain("<<<<<<<");
		expect(md).not.toContain(">>>>>>>");
	});

	it("a missing base copy is replaced by the snapshot of the version pulled", async () => {
		write(read().replace("Third paragraph.", "Third paragraph, mine."));
		server.edit(ID, annEdits("First paragraph, by Ann."));
		fs.rmSync(basePath());
		expect(await pull(), output()).toBe(0);
		expect(output()).toContain("merged Plan.md");
		expect(read()).toBe(
			render().replace("Third paragraph.", "Third paragraph, mine."),
		);
		expect(server.client.listSnapshots).toHaveBeenCalledWith(
			"p1",
			ID,
			expect.objectContaining({ contentHash: expect.any(String) }),
		);
	});

	it("a base copy that is not the version recorded counts as missing", async () => {
		write(read().replace("Third paragraph.", "Third paragraph, mine."));
		server.edit(ID, annEdits("First paragraph, by Ann."));
		fs.writeFileSync(basePath(), "something else\n");
		expect(await pull(), output()).toBe(0);
		expect(read()).toBe(
			render().replace("Third paragraph.", "Third paragraph, mine."),
		);
	});

	it("with no base copy and no snapshot, the file is kept and Paca's version is written next to it", async () => {
		const mine = read().replace("Third paragraph.", "Third paragraph, mine.");
		write(mine);
		server.edit(ID, annEdits("First paragraph, by Ann."));
		fs.rmSync(basePath());
		server.snapshots.clear();
		expect(await pull(), output()).toBe(2);
		expect(output()).toContain(
			"kept Plan.md: edited locally; use --force to overwrite (the version it was pulled at is gone, so it cannot be merged; Paca's version is in Plan.remote.md)",
		);
		expect(read()).toBe(mine);
		expect(read("Plan.remote.md")).toBe(render());
		// Never merged later against some other base.
		lines = [];
		expect(await pull(), output()).toBe(2);
		expect(read()).toBe(mine);
		// --force puts Paca's version in place.
		lines = [];
		expect(await pull({ force: true }), output()).toBe(0);
		expect(read()).toBe(render());
		expect(state().docs[ID].unmerged).toBeUndefined();
	});

	it("without git, pull keeps the file as before and says why", async () => {
		const mine = read().replace("Third paragraph.", "Third paragraph, mine.");
		write(mine);
		server.edit(ID, annEdits("First paragraph, by Ann."));
		const noGit: GitRunner = async () => {
			throw Object.assign(new Error("spawn git ENOENT"), { code: "ENOENT" });
		};
		expect(await pull({ git: noGit }), output()).toBe(2);
		expect(output()).toContain(
			"kept Plan.md: edited locally; use --force to overwrite (could not merge: git is not installed (or not on the PATH))",
		);
		expect(read()).toBe(mine);
		// Merged once git is there.
		lines = [];
		expect(await pull(), output()).toBe(0);
		expect(output()).toContain("merged Plan.md");
		const broken: GitRunner = async () => ({
			code: 255,
			stdout: "",
			stderr: "error: cannot read\n",
		});
		write(read().replace("Last paragraph.", "Last, mine."));
		server.edit(ID, annEdits("First, by Ann again."));
		lines = [];
		expect(await pull({ git: broken }), output()).toBe(2);
		expect(output()).toContain(
			"(could not merge: git merge-file failed: error: cannot read)",
		);
	});

	it("a document moved and changed in Paca while its file was edited: the file moves, then merges", async () => {
		write(read().replace("Third paragraph.", "Third paragraph, mine."));
		server.edit(ID, annEdits("First paragraph, by Ann."));
		(server.docs.get(ID) as any).title = "Roadmap";
		expect(await pull(), output()).toBe(0);
		expect(output()).toContain("moved Plan.md -> Roadmap.md");
		expect(output()).toContain("merged Roadmap.md");
		expect(fs.existsSync(file())).toBe(false);
		expect(read("Roadmap.md")).toBe(
			render().replace("Third paragraph.", "Third paragraph, mine."),
		);
	});

	it("a file saved with CRLF line ends merges line by line", async () => {
		write(
			read()
				.replace("Third paragraph.", "Third paragraph, mine.")
				.replace(/\n/g, "\r\n"),
		);
		server.edit(ID, annEdits("First paragraph, by Ann."));
		expect(await pull(), output()).toBe(0);
		expect(read()).toBe(
			render().replace("Third paragraph.", "Third paragraph, mine."),
		);
	});

	it("a file edited here and unchanged in Paca is reported and left for push", async () => {
		const mine = read().replace("Third paragraph.", "Third paragraph, mine.");
		write(mine);
		expect(await pull(), output()).toBe(0);
		expect(output()).toBe("1 modified locally\nmodified locally Plan.md");
		expect(read()).toBe(mine);
	});

	it("pull after a normalising push reports nothing for that file", async () => {
		write(
			read()
				.replace("* one\n\n* two", "- one\n\n- two\n\n- three")
				.replace(/\n/g, "\r\n"),
		);
		expect(await push(), output()).toBe(0);
		expect(output()).toContain("normalised .paca/docs/Plan.md");
		lines = [];
		expect(await pull(), output()).toBe(0);
		expect(output()).toBe("up to date");
	});

	it("a managed copy edited here is kept, not merged", async () => {
		write(read().replace("Third paragraph.", "Third paragraph, mine."));
		server.edit(ID, annEdits("First paragraph, by Ann."));
		(server.docs.get(ID) as any).source = {
			repository: "Acme/widgets",
			path: "docs/plan.md",
		};
		expect(await pull(), output()).toBe(2);
		expect(output()).toContain("kept Plan.md: edited locally");
		expect(output()).not.toContain("merged");
	});

	it("a file an interrupted pull wrote into is not merged, and nothing is written", async () => {
		const mine = read().replace("Third paragraph.", "Third paragraph, mine.");
		write(mine);
		server.edit(ID, annEdits("First paragraph, by Ann."));
		// As an interrupted pull leaves it: a write claimed, never recorded.
		const s = state();
		s.docs[ID].pending_sha256 = [sha("what that pull wrote\n")];
		fs.writeFileSync(
			path.join(mirror, ".paca-sync.json"),
			`${JSON.stringify(s, null, 2)}\n`,
		);
		const base = fs.readFileSync(basePath(), "utf8");
		expect(await pull(), output()).toBe(2);
		expect(output()).toContain(
			"kept Plan.md: edited locally; use --force to overwrite (an interrupted pull wrote into it, so it is not merged)",
		);
		expect(read()).toBe(mine);
		expect(fs.readFileSync(basePath(), "utf8")).toBe(base);
		expect(fs.existsSync(file("Plan.remote.md"))).toBe(false);
		// Still not merged on the next pull.
		lines = [];
		expect(await pull(), output()).toBe(2);
		expect(read()).toBe(mine);
	});

	it("the full loop: push refused, pull merges, push sends both changes, a reorder included", async () => {
		// Plain blocks only: a document with formatted blocks refuses any
		// reorder (a documented limit).
		const NOTES = docId(2);
		server.add(
			NOTES,
			"Notes",
			storedBlocks(
				"# Notes\n\nIntro.\n\nMiddle.\n\nThird.\n\n- one\n- two\n\nEnd.",
			),
		);
		expect(await pull()).toBe(0);
		const notes = () => server.docs.get(NOTES)?.content as any[];
		const before = JSON.parse(JSON.stringify(notes()));
		// Paragraph 3 here, and two list items swapped (a reorder).
		write(
			read("Notes.md")
				.replace("Third.", "Third, mine.")
				.replace("* one\n\n* two", "* two\n\n* one"),
			"Notes.md",
		);
		server.edit(
			NOTES,
			(b) => {
				b[1].content[0].text = "Intro, by Ann.";
			},
			{
				updated_by: "ann",
				updated_at: "2026-11-15T09:30:00Z",
				note: "tighten the intro",
			},
		);
		lines = [];
		expect(await push(".paca/docs/Notes.md"), output()).toBe(2);
		expect(output()).toContain("written by ann at 2026-11-15T09:30:00Z");
		lines = [];
		expect(await pull(), output()).toBe(0);
		expect(output()).toContain("merged Notes.md");
		lines = [];
		expect(await push(".paca/docs/Notes.md"), output()).toBe(0);
		const md = render(NOTES);
		expect(md).toBe(
			"# Notes\n\nIntro, by Ann.\n\nMiddle.\n\nThird, mine.\n\n* two\n\n* one\n\nEnd.\n",
		);
		// Blocks neither side changed are as stored; the swapped items are
		// the stored ones, moved.
		const now = notes();
		for (const k of [0, 2, 6]) expect(now[k]).toEqual(before[k]);
		expect(now[4]).toEqual(before[5]);
		expect(now[5]).toEqual(before[4]);
		lines = [];
		expect(await pull(), output()).toBe(0);
		expect(output()).toBe("up to date");
		expect(read("Notes.md")).toBe(md);
	});
});
