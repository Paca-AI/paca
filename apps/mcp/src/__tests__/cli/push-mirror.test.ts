import { execFileSync } from "node:child_process";
import { createHash } from "node:crypto";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { PacaAPIError } from "../../api/doc-client.js";
import { runPull } from "../../cli/pull.js";
import { type PushOptions, runPush } from "../../cli/push.js";
import { conflictLine } from "../../cli/push-mirror.js";
import { blocknoteToMarkdown } from "../../utils/index.js";
import { normaliseMarkdown } from "../../utils/md-normalise.js";
import { docId, fakeDocsServer, storedBlocks } from "./fake-docs-server.js";

const ID = docId(1);
const sha = (x: string | Buffer) =>
	createHash("sha256").update(x).digest("hex");

const PLAN = [
	"# Plan",
	"First paragraph.",
	"Red paragraph.",
	"Blue paragraph.",
	"- one\n- two",
	"Last paragraph.",
].join("\n\n");

describe("push of a mirror file", () => {
	let root: string;
	let mirror: string;
	let lines: string[];
	let server: ReturnType<typeof fakeDocsServer>;
	const io = {
		out: (l: string) => lines.push(l),
		err: (l: string) => lines.push(l),
	};
	const output = () => lines.join("\n");
	const pull = () =>
		runPull({ projectId: "p1", dir: mirror, force: false }, server.client, io);
	const push = (file: string, extra: Partial<PushOptions> = {}) =>
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
	const state = () =>
		JSON.parse(fs.readFileSync(path.join(mirror, ".paca-sync.json"), "utf8"));
	const setState = (change: (s: any) => void) => {
		const s = state();
		change(s);
		fs.writeFileSync(
			path.join(mirror, ".paca-sync.json"),
			`${JSON.stringify(s, null, 2)}\n`,
		);
	};
	const base = () =>
		fs.readFileSync(path.join(mirror, ".paca-base", `${ID}.md`), "utf8");
	/** Everything push may write, to check that a refusal wrote nothing. */
	const snapshot = () => ({
		state: fs.readFileSync(path.join(mirror, ".paca-sync.json"), "utf8"),
		base: base(),
		lock: fs.existsSync(path.join(mirror, ".paca-pull.lock")),
	});
	const stored = () => server.docs.get(ID)?.content as any[];
	const doc = () =>
		server.docs.get(ID) as NonNullable<ReturnType<typeof server.docs.get>>;

	beforeEach(async () => {
		root = fs.realpathSync(fs.mkdtempSync(path.join(os.tmpdir(), "paca-pm-")));
		mirror = path.join(root, ".paca", "docs");
		lines = [];
		server = fakeDocsServer();
		server.add(
			ID,
			"Plan",
			storedBlocks(PLAN, (b) => {
				b[2].props.textColor = "red";
				b[3].props.backgroundColor = "blue";
			}),
		);
		expect(await pull()).toBe(0);
		lines = [];
	});
	afterEach(() => fs.rmSync(root, { recursive: true, force: true }));

	it("one-line file edit sends a PATCH whose other blocks deep-equal the stored ones", async () => {
		const before = JSON.parse(JSON.stringify(stored()));
		const hash = state().docs[ID].content_hash;
		const edited = read().replace("Last paragraph.", "Last paragraph, edited.");
		fs.writeFileSync(file(), edited);

		expect(
			await push(".paca/docs/Plan.md", {
				note: "tighten the ending",
				task: "PACA-88",
				client: "cc/s1",
			}),
		).toBe(0);
		expect(server.client.patchDocument).toHaveBeenCalledTimes(1);
		const [project, id, input, opts] =
			server.client.patchDocument.mock.calls[0];
		expect(project).toBe("p1");
		expect(id).toBe(ID);
		expect(opts).toEqual({ ifMatch: hash });
		expect(input).toMatchObject({
			note: "tighten the ending",
			task: "PACA-88",
			client: "cc/s1",
		});
		const sent = input.content as any[];
		expect(sent).toHaveLength(before.length);
		const k = before.findIndex((b: any) =>
			JSON.stringify(b.content).includes("Last paragraph."),
		);
		sent.forEach((b, i) => {
			if (i !== k) expect(JSON.stringify(b)).toBe(JSON.stringify(before[i]));
		});
		expect(sent[k].id).toBe(before[k].id);
		expect(output()).toContain("updated Plan");

		// Recorded: base copy, then state, both matching the file.
		expect(base()).toBe(edited);
		const s = state().docs[ID];
		expect(s.content_hash).toBe(server.docs.get(ID)?.hash);
		expect(s.file_sha256).toBe(sha(edited));
		expect(s.remote_hash).toBeUndefined();
		expect(fs.existsSync(path.join(mirror, ".paca-pull.lock"))).toBe(false);

		// The next push and pull have nothing to do.
		lines = [];
		expect(await push(".paca/docs/Plan.md")).toBe(0);
		expect(output()).toBe("unchanged");
		expect(server.client.patchDocument).toHaveBeenCalledTimes(1);
		lines = [];
		expect(await pull()).toBe(0);
		expect(output()).toBe("up to date");
		expect(read()).toBe(edited);
	});

	it("unchanged file sends nothing", async () => {
		server.client.batchGet.mockClear();
		expect(await push(".paca/docs/Plan.md")).toBe(0);
		expect(output()).toBe("unchanged");
		expect(server.client.batchGet).not.toHaveBeenCalled();
		expect(server.client.patchDocument).not.toHaveBeenCalled();
	});

	it("a file saved with CRLF line ends and no change sends nothing, and is normalised", async () => {
		const pulled = read();
		fs.writeFileSync(file(), pulled.replace(/\n/g, "\r\n"));
		const before = snapshot();
		expect(await push(".paca/docs/Plan.md", { dryRun: true })).toBe(0);
		expect(output()).toBe("unchanged");
		expect(snapshot()).toEqual(before);
		expect(read()).toBe(pulled.replace(/\n/g, "\r\n"));
		lines = [];
		expect(await push(".paca/docs/Plan.md")).toBe(0);
		expect(output()).toBe("unchanged\nnormalised .paca/docs/Plan.md");
		expect(server.client.patchDocument).not.toHaveBeenCalled();
		// The file is the render again, and counts as what pull wrote.
		expect(read()).toBe(pulled);
		expect(state().docs[ID].file_sha256).toBe(sha(pulled));
		lines = [];
		expect(await pull()).toBe(0);
		expect(output()).toBe("up to date");
	});

	it("an edit written with other bullets and table padding is sent, then the file is rewritten to the render", async () => {
		server.add(
			docId(5),
			"Table",
			storedBlocks(
				"Intro.\n\n* a\n\n* b\n\n| key | value |\n|---|---|\n| x | y |",
			),
		);
		expect(await pull()).toBe(0);
		const md = read("Table.md");
		const edited = md
			.replace("* a\n\n* b", "- a\n\n\n- b  \n\n- c")
			.replace(/\| x +\| y +\|/, "|x|y, longer|");
		fs.writeFileSync(file("Table.md"), edited);
		lines = [];
		expect(await push(".paca/docs/Table.md", { dryRun: true }), output()).toBe(
			0,
		);
		expect(read("Table.md")).toBe(edited);
		lines = [];
		expect(await push(".paca/docs/Table.md"), output()).toBe(0);
		const now = blocknoteToMarkdown(
			server.docs.get(docId(5))?.content as any[],
		);
		expect(now).toContain("* a\n\n* b\n\n* c");
		expect(now).toContain("| x   | y, longer |");
		expect(output()).toBe(
			"updated Table from .paca/docs/Table.md (2 blocks changed)\nnormalised .paca/docs/Table.md",
		);
		expect(read("Table.md")).toBe(now);
		expect(state().docs[docId(5)].file_sha256).toBe(sha(now));
		expect(
			fs.readFileSync(
				path.join(mirror, ".paca-base", `${docId(5)}.md`),
				"utf8",
			),
		).toBe(now);
		lines = [];
		expect(await pull()).toBe(0);
		expect(output()).toBe("up to date");
		lines = [];
		expect(await push(".paca/docs/Table.md")).toBe(0);
		expect(output()).toBe("unchanged");
	});

	it("stale hash exits 2 naming the other writer (a 412 adds the note, next test)", async () => {
		server.edit(
			ID,
			(b) => {
				b[1].content[0].text = "First paragraph, by Ann.";
			},
			{ updated_by: "ann", note: "fix the intro", client: "web" },
		);
		fs.writeFileSync(file(), read().replace("Last paragraph.", "Last one."));
		const before = snapshot();
		expect(await push(".paca/docs/Plan.md")).toBe(2);
		expect(server.client.patchDocument).not.toHaveBeenCalled();
		expect(output()).toContain("written by ann at 2026-11-15T09:30:00Z");
		expect(output()).toContain(
			"the document changed in Paca; pull, then push again",
		);
		expect(snapshot()).toEqual(before);
	});

	it("a 412 on the write exits 2 with the writer, note and client", async () => {
		fs.writeFileSync(file(), read().replace("Last paragraph.", "Last one."));
		// Someone writes between push's read and its write.
		server.client.batchGet.mockImplementationOnce(async (p, ids) => {
			const res = await server.client.batchGet.getMockImplementation()?.(
				p,
				ids,
			);
			server.edit(
				ID,
				(b) => {
					b[1].content[0].text = "First, by Ann.";
				},
				{ note: "intro", client: "web", task: "PACA-7" },
			);
			return res as any;
		});
		const before = snapshot();
		expect(await push(".paca/docs/Plan.md")).toBe(2);
		expect(output()).toContain("written by ann at 2026-11-15T09:30:00Z");
		expect(output()).toContain("note: intro");
		expect(output()).toContain("client: web");
		expect(output()).toContain("task: PACA-7");
		expect(output()).toContain(
			"the document changed in Paca; pull, then push again",
		);
		expect(snapshot()).toEqual(before);
	});

	it("whether the document changed in Paca is asked of Paca, never taken from what a pull saw", async () => {
		// A flag an earlier pull left: Paca is back at the version pulled.
		setState((s) => {
			s.docs[ID].remote_hash = "newer";
		});
		fs.writeFileSync(file(), read().replace("Last paragraph.", "Last one."));
		expect(await push(".paca/docs/Plan.md"), output()).toBe(0);
		expect(state().docs[ID].remote_hash).toBeUndefined();
		// The version a file was edited from not known: refused, with the
		// ways out, whatever Paca holds.
		fs.writeFileSync(file("Plan.remote.md"), "Paca's version\n");
		setState((s) => {
			s.docs[ID].unmerged = true;
			s.docs[ID].remote_copy = {
				file: "Plan.remote.md",
				sha256: sha("Paca's version\n"),
			};
		});
		fs.writeFileSync(file(), read().replace("Last one.", "Last one!"));
		lines = [];
		const sent = server.client.patchDocument.mock.calls.length;
		expect(await push(".paca/docs/Plan.md")).toBe(1);
		expect(output()).toBe(
			".paca/docs/Plan.md: the version it was edited from is not known (an interrupted pull may have written into it, or the version it was pulled at is gone), so it cannot be pushed; Paca's version is in .paca/docs/Plan.remote.md to compare with. Save your edits elsewhere, then delete .paca/docs/Plan.md and pull, or run pull --force; either replaces it with Paca's version",
		);
		// A copy no longer holding what pull wrote is not named.
		fs.writeFileSync(file("Plan.remote.md"), "my notes\n");
		lines = [];
		expect(await push(".paca/docs/Plan.md")).toBe(1);
		expect(output()).not.toContain("Plan.remote.md");
		expect(server.client.patchDocument.mock.calls.length).toBe(sent);
		setState((s) => {
			delete s.docs[ID].unmerged;
			delete s.docs[ID].remote_copy;
		});
		// Paca changed: refused, naming the writer.
		server.edit(ID, (b) => {
			b[1].content[0].text = "First, by Ann.";
		});
		fs.writeFileSync(file(), read().replace("Last one.", "Last two."));
		lines = [];
		expect(await push(".paca/docs/Plan.md")).toBe(2);
		expect(output()).toContain("written by ann");
		expect(output()).toContain(
			"the document changed in Paca; pull, then push again",
		);
	});

	it("an edited file whose base copy is gone or is not the version recorded is refused as base unknown, and nothing is sent", async () => {
		const basePath = path.join(mirror, ".paca-base", `${ID}.md`);
		fs.writeFileSync(file(), read().replace("Last paragraph.", "Last one."));
		const sent = server.client.patchDocument.mock.calls.length;
		for (const spoil of [
			() => fs.writeFileSync(basePath, "not what pull wrote\n"),
			() => fs.rmSync(basePath),
		]) {
			spoil();
			lines = [];
			expect(await push(".paca/docs/Plan.md")).toBe(2);
			expect(output()).toBe(
				".paca/docs/Plan.md: the version it was edited from is not known (its base copy, .paca-base/" +
					`${ID}.md, is gone or is not the version the last pull or push wrote), so it cannot be pushed. Save your edits elsewhere, then delete .paca/docs/Plan.md and pull, or run pull --force; either replaces it with Paca's version`,
			);
			expect(server.client.patchDocument.mock.calls.length).toBe(sent);
		}
		// A file that already says what Paca holds (here saved with CRLF) is
		// recorded again, which writes the base copy back.
		fs.writeFileSync(
			file(),
			blocknoteToMarkdown(stored()).replace(/\n/g, "\r\n"),
		);
		lines = [];
		expect(await push(".paca/docs/Plan.md")).toBe(0);
		expect(base()).toBe(blocknoteToMarkdown(stored()));
	});

	it("managed, orphan, reserved and unknown files are refused", async () => {
		fs.writeFileSync(file(), read().replace("Last paragraph.", "Last one."));
		const before = snapshot();

		doc().source = {
			repository: "Acme/widgets",
			path: "docs/plan.md",
		};
		setState((s) => {
			s.docs[ID].managed = true;
		});
		expect(await push(".paca/docs/Plan.md")).toBe(1);
		expect(output()).toContain(
			"this document is a copy of docs/plan.md in Acme/widgets; edit that file and push it",
		);

		for (const [flag, want] of [
			["orphan", "was removed in Paca"],
			["reserved", "run pull first"],
		] as const) {
			lines = [];
			setState((s) => {
				delete s.docs[ID].managed;
				delete s.docs[ID].orphan;
				delete s.docs[ID].reserved;
				s.docs[ID][flag] = true;
			});
			expect(await push(".paca/docs/Plan.md")).toBe(1);
			expect(output()).toContain(want);
		}
		setState((s) => {
			delete s.docs[ID].reserved;
		});

		lines = [];
		fs.writeFileSync(file("Notes.md"), "# Mine\n");
		expect(await push(".paca/docs/Notes.md")).toBe(1);
		expect(output()).toContain(
			"not a pulled document; pull writes the files here",
		);

		lines = [];
		expect(await push(`.paca/docs/.paca-base/${ID}.md`)).toBe(1);
		expect(output()).toContain("not a pulled document");

		lines = [];
		expect(
			await runPush(
				{
					projectId: "p2",
					file: ".paca/docs/Plan.md",
					lock: false,
					force: false,
					dryRun: false,
					client: "paca-cli",
				},
				server.client as any,
				io,
				{ cwd: root },
			),
		).toBe(1);
		expect(output()).toContain("project p1");

		expect(server.client.patchDocument).not.toHaveBeenCalled();
		expect({ ...snapshot(), state: "" }).toEqual({ ...before, state: "" });
	});

	it("refuses --path, --lock and --force for a mirror file", async () => {
		fs.writeFileSync(file(), read().replace("Last paragraph.", "Last one."));
		for (const [opt, want] of [
			[{ path: "Plans/Other" }, "--path"],
			[{ lock: true }, "--lock"],
			[{ force: true }, "--force"],
		] as const) {
			lines = [];
			expect(await push(".paca/docs/Plan.md", opt)).toBe(1);
			expect(output()).toContain(want);
		}
		expect(server.client.patchDocument).not.toHaveBeenCalled();
	});

	it("conflict markers are refused", async () => {
		fs.writeFileSync(
			file(),
			read().replace(
				"Last paragraph.",
				"<<<<<<< local\nLast one.\n=======\nLast two.\n>>>>>>> Paca",
			),
		);
		const before = snapshot();
		expect(await push(".paca/docs/Plan.md")).toBe(1);
		expect(output()).toMatch(/conflict markers \(line \d+\)/);
		expect(server.client.patchDocument).not.toHaveBeenCalled();
		expect(snapshot()).toEqual(before);
	});

	it("marker lines the document already holds, or inside fenced code, are not conflicts", async () => {
		const CODE = docId(3);
		server.add(
			CODE,
			"Merge notes",
			storedBlocks(
				"How a conflict looks:\n\n```\n<<<<<<< HEAD\nmine\n=======\ntheirs\n>>>>>>> branch\n```\n\nThe end.",
			),
		);
		expect(await pull()).toBe(0);
		const md = read("Merge notes.md");
		expect(md).toContain("\n<<<<<<< HEAD\n");
		// Saved again unchanged: no conflict, and nothing to send.
		fs.writeFileSync(file("Merge notes.md"), md);
		lines = [];
		expect(await push(".paca/docs/Merge notes.md"), output()).toBe(0);
		expect(output()).toBe("unchanged");
		// Edited elsewhere: the markers the document holds are not conflicts.
		fs.writeFileSync(
			file("Merge notes.md"),
			md.replace("The end.", "The end, edited."),
		);
		lines = [];
		expect(await push(".paca/docs/Merge notes.md"), output()).toBe(0);
		expect(output()).toMatch(/^updated Merge notes/);
		// New marker lines inside a fence are code, not conflicts.
		const fenced = read("Merge notes.md").replace(
			"theirs\n>>>>>>> branch\n```",
			"theirs\n>>>>>>> branch\n```\n\n```text\n|||||||  base\n>>>>>>> other\n```",
		);
		fs.writeFileSync(file("Merge notes.md"), fenced);
		lines = [];
		expect(await push(".paca/docs/Merge notes.md"), output()).toBe(0);
		expect(blocknoteToMarkdown(server.docs.get(CODE)?.content as any[])).toBe(
			fenced,
		);
	});

	it("a file whose name only folds to a pulled one's counts only if it is that file on disk", async () => {
		const upper = file("PLAN.md");
		// On a case-insensitive file system PLAN.md is Plan.md.
		const sensitive = !fs.existsSync(upper);
		const edited = read().replace("Last paragraph.", "Last, edited.");
		if (!sensitive) {
			fs.writeFileSync(upper, edited);
			expect(await push(".paca/docs/PLAN.md"), output()).toBe(0);
			expect(blocknoteToMarkdown(stored())).toBe(edited);
			return;
		}
		// Another file that only differs by case: not a pulled document.
		fs.writeFileSync(upper, edited);
		const before = snapshot();
		expect(await push(".paca/docs/PLAN.md")).toBe(1);
		expect(output()).toBe(
			".paca/docs/PLAN.md: not a pulled document; pull writes the files here, and only those are pushed from a mirror folder",
		);
		expect(server.client.patchDocument).not.toHaveBeenCalled();
		expect(snapshot()).toEqual(before);
		// A second name of the very file (a hard link) is that file.
		fs.rmSync(upper);
		fs.writeFileSync(file(), edited);
		fs.linkSync(file(), upper);
		lines = [];
		expect(await push(".paca/docs/PLAN.md"), output()).toBe(0);
		expect(blocknoteToMarkdown(stored())).toBe(edited);
	});

	it("an empty or whitespace-only file is refused", async () => {
		for (const empty of ["", "\n", "  \n\n\t\r\n", "\ufeff\n"]) {
			fs.writeFileSync(file(), empty);
			const before = snapshot();
			lines = [];
			expect(await push(".paca/docs/Plan.md"), JSON.stringify(empty)).toBe(1);
			expect(output()).toBe(
				".paca/docs/Plan.md is empty; a document is deleted in Paca, not by emptying its file",
			);
			expect(server.client.patchDocument).not.toHaveBeenCalled();
			expect(snapshot()).toEqual(before);
		}
		// A document that is empty in Paca: blank lines are no edit.
		server.add(docId(4), "Blank", []);
		lines = [];
		expect(await pull()).toBe(0);
		fs.writeFileSync(file("Blank.md"), "\n\n");
		lines = [];
		expect(await push(".paca/docs/Blank.md"), output()).toBe(0);
		expect(output()).toBe("unchanged\nnormalised .paca/docs/Blank.md");
		expect(read("Blank.md")).toBe("");
		expect(server.client.patchDocument).not.toHaveBeenCalled();
	});

	it("a conflict file git merge-file wrote is refused", async () => {
		const md = read();
		const tmp = fs.mkdtempSync(path.join(root, "m-"));
		const put = (n: string, x: string) => {
			fs.writeFileSync(path.join(tmp, n), x);
			return path.join(tmp, n);
		};
		const ours = put("ours", md.replace("Last paragraph.", "Last, mine."));
		const baseFile = put("base", md);
		const theirs = put(
			"theirs",
			md.replace("Last paragraph.", "Last, theirs."),
		);
		let merged = "";
		try {
			execFileSync(
				"git",
				[
					"merge-file",
					"-p",
					"--diff3",
					"-L",
					"Plan.md",
					"-L",
					"base",
					"-L",
					"Paca",
					ours,
					baseFile,
					theirs,
				],
				{ encoding: "utf8" },
			);
		} catch (err: any) {
			merged = err.stdout;
		}
		expect(merged).toContain("||||||| base");
		fs.writeFileSync(file(), merged);
		const before = snapshot();
		expect(await push(".paca/docs/Plan.md")).toBe(1);
		const at = merged.split("\n").indexOf("<<<<<<< Plan.md") + 1;
		expect(output()).toContain(`conflict markers (line ${at})`);
		expect(server.client.patchDocument).not.toHaveBeenCalled();
		expect(snapshot()).toEqual(before);
	});

	it("refused splice writes nothing and exits 1", async () => {
		// Swap the red and the blue paragraph.
		const md = read();
		fs.writeFileSync(
			file(),
			md
				.replace("Red paragraph.", "@@")
				.replace("Blue paragraph.", "Red paragraph.")
				.replace("@@", "Blue paragraph."),
		);
		const before = snapshot();
		expect(await push(".paca/docs/Plan.md")).toBe(1);
		expect(output()).toMatch(
			/cannot push \.paca\/docs\/Plan\.md: .*line [57]/s,
		);
		expect(output()).not.toContain("edit_doc");
		expect(server.client.patchDocument).not.toHaveBeenCalled();
		expect(snapshot()).toEqual(before);
	});

	it("--dry-run says how many blocks would change and sends nothing", async () => {
		fs.writeFileSync(
			file(),
			read()
				.replace("First paragraph.", "First one.")
				.replace("Last paragraph.", "Last one."),
		);
		const before = snapshot();
		expect(await push(".paca/docs/Plan.md", { dryRun: true })).toBe(0);
		expect(output()).toContain("2 blocks would change");
		expect(output()).toContain("nothing sent");
		expect(server.client.patchDocument).not.toHaveBeenCalled();
		expect(snapshot()).toEqual(before);
	});

	it("a document locked since the pull gets the managed-copy message", async () => {
		doc().source = {
			repository: "Acme/widgets",
			path: "docs/plan.md",
		};
		doc().source_locked = true;
		fs.writeFileSync(file(), read().replace("Last paragraph.", "Last one."));
		const before = snapshot();
		expect(await push(".paca/docs/Plan.md")).toBe(1);
		expect(output()).toContain(
			"this document is a copy of docs/plan.md in Acme/widgets; edit that file and push it",
		);
		expect(server.client.patchDocument).not.toHaveBeenCalled();
		expect(snapshot()).toEqual(before);

		// Locked between the read and the write: 409 DOC_LOCKED.
		lines = [];
		doc().source = null;
		doc().source_locked = false;
		server.client.patchDocument.mockRejectedValueOnce(
			new PacaAPIError("locked", 409, "DOC_LOCKED"),
		);
		expect(await push(".paca/docs/Plan.md")).toBe(1);
		expect(output()).toContain("managed copy");
		expect(snapshot()).toEqual(before);
	});

	it("a document gone from Paca exits 2 and writes nothing", async () => {
		server.docs.delete(ID);
		fs.writeFileSync(file(), read().replace("Last paragraph.", "Last one."));
		const before = snapshot();
		expect(await push(".paca/docs/Plan.md")).toBe(2);
		expect(output()).toContain("no longer in Paca");
		expect(snapshot()).toEqual(before);
	});

	it("does not run while a pull holds the mirror", async () => {
		fs.writeFileSync(file(), read().replace("Last paragraph.", "Last one."));
		fs.writeFileSync(
			path.join(mirror, ".paca-pull.lock"),
			JSON.stringify({ pid: process.pid, started: "now", token: "x" }),
		);
		expect(await push(".paca/docs/Plan.md")).toBe(1);
		expect(output()).toContain("another pull is running in");
		expect(server.client.batchGet).toHaveBeenCalledTimes(1); // the pull's
		expect(server.client.patchDocument).not.toHaveBeenCalled();
	});

	it("a pull after the push leaves the pushed file alone; a second edit pushes again", async () => {
		fs.writeFileSync(file(), read().replace("First paragraph.", "First one."));
		expect(await push(".paca/docs/Plan.md")).toBe(0);
		const pushed = read();
		lines = [];
		expect(await pull()).toBe(0);
		expect(read()).toBe(pushed);
		fs.writeFileSync(file(), read().replace("Last paragraph.", "Last one."));
		lines = [];
		expect(await push(".paca/docs/Plan.md")).toBe(0);
		expect(blocknoteToMarkdown(stored())).toBe(read());
		expect(stored()[2].props.textColor).toBe("red");
		expect(stored()[3].props.backgroundColor).toBe("blue");
	});

	it("pull's own file of every block type pushes one edited line as one block", async () => {
		// The renderer assumption against pull's real output: a table with
		// widths and a coloured cell, a mention chip, underline, a toggle
		// heading, a nested paragraph, code, a quote, lists and colours.
		const ID2 = docId(2);
		const blocks = storedBlocks(
			[
				"# Title",
				"Plain with **bold**, *italic*, `code`, ~~strike~~ and a [link](https://example.com).",
				"Coloured.",
				"- bullet one\n- bullet two\n  - nested bullet",
				"3. third\n4. fourth",
				"- [ ] open\n- [x] done",
				"```ts\nconst x = 1;\n```",
				"> A quote.",
				"| a | b |\n|---|---|\n| 1 | 2 |",
				"Chip.",
				"Under.",
				"Toggle",
				"Parent",
				"Last line.",
			].join("\n\n"),
			(b) => {
				const find = (text: string) =>
					b.find((x: any) => JSON.stringify(x.content ?? "").includes(text));
				find("Coloured.").props.textColor = "red";
				find("Chip.").content = [
					{ type: "text", text: "Chip ", styles: {} },
					{ type: "teamMention", props: { id: "u1", name: "Ann", avatar: "" } },
				];
				find("Under.").content = [
					{ type: "text", text: "Under", styles: { underline: true } },
				];
				const toggle = find("Toggle");
				toggle.type = "heading";
				toggle.props = { ...toggle.props, level: 2, isToggleable: true };
				const table = b.find((x: any) => x.type === "table");
				table.content.columnWidths = [180, null];
				table.content.rows[1].cells[0].props.backgroundColor = "blue";
				find("Parent").children = [
					{
						id: "kid1",
						type: "paragraph",
						props: {
							textColor: "default",
							backgroundColor: "default",
							textAlignment: "left",
						},
						content: [{ type: "text", text: "Nested", styles: {} }],
						children: [],
					},
				];
			},
		);
		server.add(ID2, "Every", blocks);
		expect(await pull()).toBe(0);
		const before = JSON.stringify(server.docs.get(ID2)?.content);
		const original = JSON.parse(before) as any[];
		fs.writeFileSync(
			file("Every.md"),
			read("Every.md").replace("Last line.", "Last line, edited."),
		);
		lines = [];
		expect(await push(".paca/docs/Every.md")).toBe(0);
		expect(output()).toContain("(1 block changed)");
		const now = server.docs.get(ID2)?.content as any[];
		expect(now).toHaveLength(original.length);
		const k = original.length - 1;
		for (let i = 0; i < k; i++)
			expect(JSON.stringify(now[i])).toBe(JSON.stringify(original[i]));
		expect(now[k].id).toBe(original[k].id);
		expect(blocknoteToMarkdown(now)).toBe(read("Every.md"));
	});

	it("a reorder-only edit is sent, and the next pull leaves the file alone", async () => {
		// Plain blocks only: a swap next to a formatted block is refused
		// (a documented limit).
		const PLAIN = docId(2);
		server.add(
			PLAIN,
			"Plain",
			storedBlocks("Alpha one.\n\nBeta two.\n\n- li one\n- li two\n\nEnd."),
		);
		expect(await pull()).toBe(0);
		const plain = () => server.docs.get(PLAIN)?.content as any[];
		const before = plain().map((b: any) => b.id);
		for (const [from, to] of [
			["Alpha one.\n\nBeta two.", "Beta two.\n\nAlpha one."],
			["* li one\n\n* li two", "* li two\n\n* li one"],
		]) {
			const swapped = read("Plain.md").replace(from, to);
			expect(swapped).not.toBe(read("Plain.md"));
			fs.writeFileSync(file("Plain.md"), swapped);
			lines = [];
			const calls = server.client.patchDocument.mock.calls.length;
			expect(
				await push(".paca/docs/Plain.md", { dryRun: true }),
				output(),
			).toBe(0);
			expect(output()).toMatch(/[1-9]\d* blocks? would change/);
			expect(server.client.patchDocument.mock.calls.length).toBe(calls);
			lines = [];
			expect(await push(".paca/docs/Plain.md"), output()).toBe(0);
			expect(server.client.patchDocument.mock.calls.length).toBe(calls + 1);
			expect(output()).not.toContain("unchanged");
			expect(output()).toMatch(/[1-9]\d* blocks? changed/);
			expect(blocknoteToMarkdown(plain())).toBe(swapped);
			expect([...plain().map((b: any) => b.id)].sort()).toEqual(
				[...before].sort(),
			);
			lines = [];
			expect(await pull()).toBe(0);
			expect(output()).toBe("up to date");
			expect(read("Plain.md")).toBe(swapped);
		}
	});

	it("a file that differs from what Paca holds is sent or refused, never recorded as unchanged", async () => {
		const md = read();
		const variants = [
			md.replace("* one\n\n* two", "* two\n\n* one"),
			md.replace("# Plan\n\nFirst paragraph.", "First paragraph.\n\n# Plan"),
			md.replace("First paragraph.", "First paragraph!"),
		];
		expect(md).toContain("* one\n\n* two");
		for (const v of variants) {
			expect(v).not.toBe(md);
			fs.writeFileSync(file(), v);
			lines = [];
			const calls = server.client.patchDocument.mock.calls.length;
			const code = await push(".paca/docs/Plan.md");
			if (code === 0) {
				expect(server.client.patchDocument.mock.calls.length, v).toBe(
					calls + 1,
				);
				expect(blocknoteToMarkdown(stored()), v).toBe(v);
			} else {
				expect(code, output()).toBe(1);
				expect(output()).toContain("cannot push");
			}
			// Back to what Paca holds for the next variant.
			fs.writeFileSync(file(), blocknoteToMarkdown(stored()));
			lines = [];
			await push(".paca/docs/Plan.md");
		}
	});

	it("edits the normalising rules do not cover are sent: spaces in code, a non-breaking space, a dash table cell", async () => {
		const ID2 = docId(2);
		server.add(
			ID2,
			"Code",
			storedBlocks(
				"Intro.\n\n```text\nline one\nkeep  \n```\n\nPrice: 5\n\n| a | b |\n| --- | --- |\n| - | - |\n| x | y |",
			),
		);
		expect(await pull()).toBe(0);
		const pulled = read("Code.md");
		expect(pulled).toContain("keep  \n```");
		const edits = [
			(x: string) => x.replace("keep  \n", "keep\n"),
			(x: string) => x.replace("Price: 5", "Price: 5\u00a0"),
			(x: string) => x.replace("| - | - |\n| x", "| --- | - |\n| x"),
		];
		for (const edit of edits) {
			const was = blocknoteToMarkdown(server.docs.get(ID2)?.content as any[]);
			const want = edit(was);
			expect(want).not.toBe(was);
			fs.writeFileSync(file("Code.md"), want);
			lines = [];
			expect(await push(".paca/docs/Code.md"), output()).toBe(0);
			expect(output()).toContain("(1 block changed)");
			const now = blocknoteToMarkdown(server.docs.get(ID2)?.content as any[]);
			// The render pads table columns, which the rules allow.
			expect(normaliseMarkdown(now).text).toBe(normaliseMarkdown(want).text);
			expect(read("Code.md")).toBe(now);
		}
	});

	it("one edited paragraph is one block changed, in --dry-run and in the push, when the blocks have no ids", async () => {
		const BARE = docId(2);
		server.add(
			BARE,
			"Bare",
			storedBlocks("# Bare\n\nOne.\n\nTwo.\n\nThree.\n\nFour.").map((b) => {
				const { id: _id, ...rest } = b;
				return rest;
			}),
		);
		expect(await pull()).toBe(0);
		fs.writeFileSync(
			file("Bare.md"),
			read("Bare.md").replace("Three.", "Three, edited."),
		);
		lines = [];
		expect(await push(".paca/docs/Bare.md", { dryRun: true }), output()).toBe(
			0,
		);
		expect(output()).toBe(
			".paca/docs/Bare.md: 1 block would change in Bare (nothing sent)",
		);
		lines = [];
		expect(await push(".paca/docs/Bare.md"), output()).toBe(0);
		expect(output()).toBe(
			"updated Bare from .paca/docs/Bare.md (1 block changed)",
		);
	});

	it("repository files next to a mirror still push as repository files", async () => {
		const git = (...args: string[]) =>
			execFileSync("git", args, {
				cwd: root,
				env: {
					...process.env,
					GIT_AUTHOR_NAME: "t",
					GIT_AUTHOR_EMAIL: "t@example.com",
					GIT_COMMITTER_NAME: "t",
					GIT_COMMITTER_EMAIL: "t@example.com",
					GIT_CONFIG_GLOBAL: "/dev/null",
					GIT_CONFIG_NOSYSTEM: "1",
				},
			});
		git("init", "-q");
		git("remote", "add", "origin", "git@github.com:Acme/widgets.git");
		fs.mkdirSync(path.join(root, "docs"));
		fs.writeFileSync(path.join(root, "docs", "plan.md"), "# Plan\n");
		const putByPath = vi.fn(async () => ({
			status: "written" as const,
			doc: {
				id: "d9",
				title: "plan",
				content: [],
				position: 0,
				created_at: "",
				updated_at: "",
				content_hash: "h",
			},
			created: true,
			changed: true,
			etag: '"e1"',
		}));
		const res = await runPush(
			{
				projectId: "p1",
				file: "docs/plan.md",
				lock: false,
				force: false,
				dryRun: false,
				client: "paca-cli",
			},
			{ ...server.client, putByPath, getByPath: vi.fn() } as any,
			io,
			{ cwd: root },
		);
		expect(res).toBe(0);
		expect(putByPath).toHaveBeenCalledTimes(1);
		expect(server.client.patchDocument).not.toHaveBeenCalled();
	});
});

describe("conflictLine", () => {
	it("finds new marker lines outside fenced code", () => {
		const conflict =
			"a\n\n<<<<<<< mine\nx\n||||||| base\ny\n=======\nz\n>>>>>>> Paca\n";
		expect(conflictLine(conflict, "a\n")).toBe(3);
		expect(conflictLine("a\n\n>>>>>>> Paca\n", "a\n")).toBe(3);
		expect(conflictLine("a\r\n\r\n||||||| base\r\n", "a\n")).toBe(3);
		// "=======" alone is a heading underline.
		expect(conflictLine("Title\n=======\n", "")).toBe(0);
		// Seven characters, then a space or the end of the line.
		expect(conflictLine("<<<<<<<<\n<<<<<<<x\n", "")).toBe(0);
	});
	it("skips fenced code and marker lines the base copy holds", () => {
		expect(conflictLine("```\n<<<<<<< a\n```\n", "")).toBe(0);
		expect(conflictLine("~~~~\n<<<<<<< a\n~~~\n>>>>>>> b\n~~~~\n", "")).toBe(0);
		expect(conflictLine("```\nx\n```\n<<<<<<< a\n", "")).toBe(4);
		expect(conflictLine("x\n<<<<<<< a\n", "<<<<<<< a\n")).toBe(0);
		expect(conflictLine("x\n<<<<<<< b\n", "<<<<<<< a\n")).toBe(2);
	});
	it("finds the marker lines pull writes anywhere, fenced code included", () => {
		expect(conflictLine("```\n<<<<<<< local\nx\n```\n", "")).toBe(2);
		expect(conflictLine("```\nx\n||||||| base\n```\n", "")).toBe(3);
		expect(conflictLine("~~~\n>>>>>>> Paca\n~~~\n", "")).toBe(2);
		expect(conflictLine("```\n>>>>>>> Paca (ann, 2026-11-15)\n```\n", "")).toBe(
			2,
		);
		// Not pull's labels: other text after the marker, inside a fence.
		expect(
			conflictLine("```\n<<<<<<< localhost\n>>>>>>> Pacament\n```\n", ""),
		).toBe(0);
		// The same line in the base copy is the document's own.
		expect(
			conflictLine("```\n<<<<<<< local\n```\n", "```\n<<<<<<< local\n```\n"),
		).toBe(0);
	});
});
