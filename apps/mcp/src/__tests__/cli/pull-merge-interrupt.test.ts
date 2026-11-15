import { createHash } from "node:crypto";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { describe, expect, it, vi } from "vitest";
import { runPull, settled } from "../../cli/pull.js";
import { runPush } from "../../cli/push.js";
import { blocknoteToMarkdown } from "../../utils/index.js";
import { docId, fakeDocsServer, storedBlocks } from "./fake-docs-server.js";

// A merging pull that stops at its n-th file write or rename: for good
// ("kill": no error, no cleanup, as if the process were killed there), or
// with an error ("throw").
const stop = vi.hoisted(() => ({
	armed: false,
	left: 0,
	mode: "kill" as "kill" | "throw",
	/** In "throw" mode, only a write to a file with this name stops. */
	only: null as string | null,
	reached: null as null | (() => void),
	writes: [] as string[],
}));
vi.mock("../../cli/state.js", async (importOriginal) => {
	const orig = await importOriginal<typeof import("../../cli/state.js")>();
	const gate =
		<A extends [string, ...unknown[]]>(fn: (...a: A) => Promise<void>) =>
		(...a: A) => {
			if (stop.armed) {
				const name = path.basename(a[0]);
				if (stop.mode === "throw") {
					if (name === stop.only) {
						stop.armed = false;
						return Promise.reject(new Error("disk full"));
					}
				} else if (stop.left-- <= 0) {
					stop.reached?.();
					return new Promise<void>(() => {});
				}
				stop.writes.push(name);
			}
			return fn(...a);
		};
	return {
		...orig,
		writeFileAtomic: gate(orig.writeFileAtomic),
		writeJSON: gate(orig.writeJSON),
		moveFile: gate(orig.moveFile),
	};
});

const ID = docId(1);
const sha = (x: string) => createHash("sha256").update(x).digest("hex");
const ID2 = docId(2);
const DOC = "# Plan\n\nFirst.\n\nRed one.\n\nThird.\n\nLast.";

async function setup() {
	const root = fs.realpathSync(
		fs.mkdtempSync(path.join(os.tmpdir(), "paca-mk-")),
	);
	const mirror = path.join(root, "m");
	const server = fakeDocsServer();
	server.add(
		ID,
		"Plan",
		storedBlocks(DOC, (b) => {
			b[2].props.textColor = "red";
		}),
	);
	const out: string[] = [];
	const io = {
		out: (l: string) => out.push(l),
		err: (l: string) => out.push(l),
	};
	const said = () => out.splice(0).join("\n");
	const pull = () =>
		runPull({ projectId: "p1", dir: mirror, force: false }, server.client, io);
	const push = (rel = "m/Plan.md") =>
		runPush(
			{
				projectId: "p1",
				file: rel,
				lock: false,
				force: false,
				dryRun: false,
				client: "paca-cli",
			},
			server.client as any,
			io,
			{ cwd: root },
		);
	const file = (rel = "Plan.md") => path.join(mirror, rel);
	const read = (rel = "Plan.md") => fs.readFileSync(file(rel), "utf8");
	const write = (text: string, rel = "Plan.md") =>
		fs.writeFileSync(file(rel), text);
	const render = (id = ID) =>
		blocknoteToMarkdown(server.docs.get(id)?.content as any[]);
	const state = () =>
		JSON.parse(fs.readFileSync(path.join(mirror, ".paca-sync.json"), "utf8"));
	const leftovers = () =>
		fs
			.readdirSync(path.join(mirror, ".paca-base"))
			.filter((n) => n.endsWith(".next.md"));
	expect(await pull()).toBe(0);
	said();
	return {
		root,
		mirror,
		server,
		pull,
		push,
		said,
		read,
		write,
		render,
		state,
		leftovers,
		cleanup: () => fs.rmSync(root, { recursive: true, force: true }),
	};
}

/** Runs `pull` and stops it at its n-th write: "done" when it finished. */
async function killed(
	pull: () => Promise<number>,
	mirror: string,
	n: number,
): Promise<{ first: "done" | "killed"; writes: string[] }> {
	const hit = new Promise<"killed">((r) => {
		stop.reached = () => r("killed");
	});
	stop.mode = "kill";
	stop.left = n;
	stop.writes = [];
	stop.armed = true;
	const first = await Promise.race([pull().then(() => "done" as const), hit]);
	stop.armed = false;
	fs.rmSync(path.join(mirror, ".paca-pull.lock"), { force: true });
	return { first, writes: [...stop.writes] };
}

const opens = (text: string) =>
	text.split("\n").filter((l) => l.startsWith("<<<<<<<")).length;

describe("a merging pull stopped at each step of its journal", () => {
	for (const restart of ["pull", "push"] as const) {
		it(`clean merge, restarted with ${restart}: the same merge, and a push keeps both changes`, async () => {
			const want =
				"# Plan\n\nFirst, by Ann.\n\nRed one.\n\nThird, mine.\n\nLast.\n";
			const stops: string[][] = [];
			for (let n = 0; n < 10; n++) {
				const h = await setup();
				try {
					h.write(h.read().replace("Third.", "Third, mine."));
					h.server.edit(ID, (b) => {
						b[1].content[0].text = "First, by Ann.";
					});
					const k = await killed(h.pull, h.mirror, n);
					if (k.first === "done") break;
					stops.push(k.writes);
					const at = `stopped after ${k.writes.join(", ") || "nothing"}`;
					if (restart === "push") {
						// Refused either way: mid-merge, or Paca changed.
						expect([1, 2], at).toContain(await h.push());
						expect(h.server.client.patchDocument).not.toHaveBeenCalled();
						h.said();
					}
					expect(await h.pull(), `${at}\n${h.said()}`).toBe(0);
					expect(h.read(), at).toBe(want);
					expect(h.leftovers(), at).toEqual([]);
					expect(h.state().docs[ID].merge_pending, at).toBeUndefined();
					expect(await h.push(), at).toBe(0);
					expect(h.said(), at).toBe(
						"updated Plan from m/Plan.md (1 block changed)",
					);
					expect(h.render(), at).toBe(want);
					expect(h.server.docs.get(ID)?.content[2].props.textColor).toBe("red");
				} finally {
					h.cleanup();
				}
			}
			// Stopped before Paca's version, before the journal, before the
			// merged file, before the base copy, and before the final state.
			expect(stops).toEqual([
				[],
				[`${ID}.next.md`],
				[`${ID}.next.md`, ".paca-sync.json"],
				[`${ID}.next.md`, ".paca-sync.json", "Plan.md"],
				[`${ID}.next.md`, ".paca-sync.json", "Plan.md", `${ID}.next.md`],
			]);
		});

		it(`conflicting merge, restarted with ${restart}: one set of markers, no edit lost, and a resolution pushes`, async () => {
			const stops: string[][] = [];
			for (let n = 0; n < 10; n++) {
				const h = await setup();
				try {
					h.write(h.read().replace("First.", "First, mine."));
					h.server.edit(ID, (b) => {
						b[1].content[0].text = "First, by Ann.";
					});
					const k = await killed(h.pull, h.mirror, n);
					if (k.first === "done") break;
					stops.push(k.writes);
					const at = `stopped after ${k.writes.join(", ") || "nothing"}`;
					if (restart === "push") {
						expect([1, 2], at).toContain(await h.push());
						expect(h.server.client.patchDocument).not.toHaveBeenCalled();
						h.said();
					}
					expect(await h.pull(), at).toBe(2);
					expect(h.said(), at).toContain("conflict Plan.md");
					const text = h.read();
					expect(opens(text), at).toBe(1);
					expect(text, at).toContain(
						"<<<<<<< local\nFirst, mine.\n||||||| base\nFirst.\n=======\nFirst, by Ann.\n>>>>>>> Paca",
					);
					expect(h.state().docs[ID]).toMatchObject({ conflict: true });
					expect(h.leftovers(), at).toEqual([]);
					// Pulled again: still one set of markers.
					expect(await h.pull(), at).toBe(2);
					expect(opens(h.read()), at).toBe(1);
					h.said();
					// Resolved, keeping both sides: pushed.
					const resolved = h
						.read()
						.replace(
							/<<<<<<< local\n[\s\S]*?>>>>>>> Paca[^\n]*\n/,
							"First, mine and Ann's.\n",
						);
					h.write(resolved);
					expect(await h.push(), `${at}\n${h.said()}`).toBe(0);
					expect(h.render()).toBe(resolved);
				} finally {
					h.cleanup();
				}
			}
			expect(stops.map((w) => w.length)).toEqual([0, 1, 2, 3, 4]);
		});
	}

	it("a document renamed and edited in Paca while its file is edited here: a stop at any write, then pull, moves and merges it", async () => {
		const want =
			"# Plan\n\nFirst, by Ann.\n\nRed one.\n\nThird, mine.\n\nLast.\n";
		const stops: string[][] = [];
		for (let n = 0; n < 12; n++) {
			const h = await setup();
			try {
				h.write(h.read().replace("Third.", "Third, mine."));
				h.server.edit(ID, (b) => {
					b[1].content[0].text = "First, by Ann.";
				});
				(h.server.docs.get(ID) as any).title = "Renamed";
				const k = await killed(h.pull, h.mirror, n);
				if (k.first === "done") break;
				stops.push(k.writes);
				const at = `stopped after ${k.writes.join(", ") || "nothing"}`;
				expect(await h.pull(), `${at}\n${h.said()}`).toBe(0);
				expect(h.read("Renamed.md"), at).toBe(want);
				expect(h.state().docs[ID].unmerged, at).toBeUndefined();
				expect(await h.push("m/Renamed.md"), at).toBe(0);
				expect(h.render(), at).toBe(want);
			} finally {
				h.cleanup();
			}
		}
		expect(stops.length).toBeGreaterThan(3);
		expect(stops[1]).toEqual([".paca-sync.json"]);
	});

	it("a file an interrupted pull wrote into stays unmerged when Paca goes back: pull keeps it, push refuses, deleting it and pulling settles it", async () => {
		const h = await setup();
		try {
			const before = JSON.parse(JSON.stringify(h.server.docs.get(ID)?.content));
			h.server.edit(ID, (b) => {
				b[1].content[0].text = "First, SECRET by Ann.";
			});
			// A plain pull stopped after writing Ann's version into the file.
			const k = await killed(h.pull, h.mirror, 2);
			expect(k.writes).toEqual([".paca-sync.json", "Plan.md"]);
			expect(h.read()).toContain("SECRET");
			h.write(h.read().replace("Last.", "Last, mine."));
			expect(await h.pull()).toBe(2);
			expect(h.said()).toContain("an interrupted pull may have written into it");
			expect(h.read("Plan.remote.md")).toBe(h.render());
			// Ann reverts: Paca is back at the version recorded.
			h.server.edit(ID, (b) => {
				b.splice(0, b.length, ...JSON.parse(JSON.stringify(before)));
			});
			expect(await h.pull()).toBe(2);
			expect(h.said()).toContain("kept Plan.md");
			expect(h.state().docs[ID].unmerged).toBe(true);
			expect(h.read("Plan.remote.md")).toBe(h.render());
			const sent = h.server.client.patchDocument.mock.calls.length;
			expect(await h.push()).toBe(1);
			const said = h.said();
			expect(said).toContain("delete m/Plan.md and pull");
			expect(said).toContain("pull --force");
			expect(said).toContain("Plan.remote.md");
			expect(h.server.client.patchDocument.mock.calls.length).toBe(sent);
			expect(h.render()).not.toContain("SECRET");
			// The way out: delete the file and pull.
			fs.rmSync(path.join(h.mirror, "Plan.md"));
			expect(await h.pull()).toBe(0);
			expect(h.read()).toBe(h.render());
			expect(h.state().docs[ID].unmerged).toBeUndefined();
			expect(fs.existsSync(path.join(h.mirror, "Plan.remote.md"))).toBe(false);
		} finally {
			h.cleanup();
		}
	});

	it("a merge stopped after its base copy, then an edit here and Ann's revert: the next pull merges the revert, and push never re-applies it", async () => {
		const h = await setup();
		try {
			const before = JSON.parse(JSON.stringify(h.server.docs.get(ID)?.content));
			h.write(h.read().replace("Third.", "Third, mine."));
			h.server.edit(ID, (b) => {
				b[1].content[0].text = "First, SECRET by Ann.";
			});
			const k = await killed(h.pull, h.mirror, 4);
			expect(k.writes).toEqual([
				`${ID}.next.md`,
				".paca-sync.json",
				"Plan.md",
				`${ID}.next.md`,
			]);
			h.write(h.read().replace("Last.", "Last, mine."));
			h.server.edit(ID, (b) => {
				b.splice(0, b.length, ...JSON.parse(JSON.stringify(before)));
			});
			expect(await h.pull(), h.said()).toBe(0);
			expect(h.read()).toBe(
				"# Plan\n\nFirst.\n\nRed one.\n\nThird, mine.\n\nLast, mine.\n",
			);
			expect(await h.push()).toBe(0);
			expect(h.render()).not.toContain("SECRET");
			expect(h.render()).toContain("Last, mine.");
		} finally {
			h.cleanup();
		}
	});

	for (const [what, k, spoil] of [
		[
			"Paca's version (.next.md) deleted",
			3,
			(m: string) => fs.rmSync(path.join(m, ".paca-base", `${ID}.next.md`)),
		],
		[
			"the base copy edited",
			4,
			(m: string) =>
				fs.writeFileSync(path.join(m, ".paca-base", `${ID}.md`), "mine\n"),
		],
		[
			"the base copy deleted",
			4,
			(m: string) => fs.rmSync(path.join(m, ".paca-base", `${ID}.md`)),
		],
	] as Array<[string, number, (m: string) => void]>)
		it(`a merge stopped after its file write, then ${what} and Ann's revert: the file is kept with its base unknown, and push never re-applies the change`, async () => {
			const h = await setup();
			try {
				const before = JSON.parse(
					JSON.stringify(h.server.docs.get(ID)?.content),
				);
				h.write(h.read().replace("Third.", "Third, mine."));
				h.server.edit(ID, (b) => {
					b[1].content[0].text = "First, SECRET by Ann.";
				});
				await killed(h.pull, h.mirror, k);
				expect(h.read()).toContain("SECRET");
				spoil(h.mirror);
				h.server.edit(ID, (b) => {
					b.splice(0, b.length, ...JSON.parse(JSON.stringify(before)));
				});
				expect(await h.pull()).toBe(2);
				expect(h.said()).toContain(
					"the version it was edited from is not known",
				);
				expect(h.state().docs[ID].unmerged).toBe(true);
				expect(h.read("Plan.remote.md")).toBe(h.render());
				expect(await h.push()).toBe(1);
				expect(h.render()).not.toContain("SECRET");
				expect(h.read()).toContain("Third, mine.");
			} finally {
				h.cleanup();
			}
		});

	it("a journal whose Paca version is gone and whose base copy was edited by hand is not finished: the base is unknown", async () => {
		const h = await setup();
		try {
			h.write(h.read().replace("Third.", "Third, mine."));
			const st = h.state();
			const was = st.docs[ID].content_hash;
			st.docs[ID].merge_pending = {
				remote_hash: "R1",
				merged_sha: "a".repeat(64),
				local_sha: "b".repeat(64),
				remote_sha: "c".repeat(64),
			};
			fs.writeFileSync(
				path.join(h.mirror, ".paca-sync.json"),
				JSON.stringify(st),
			);
			fs.writeFileSync(
				path.join(h.mirror, ".paca-base", `${ID}.md`),
				"garbage the user typed\n",
			);
			expect(await h.pull()).toBe(2);
			const e = h.state().docs[ID];
			expect(e.content_hash).toBe(was);
			expect(e.unmerged).toBe(true);
			expect(e.merge_pending).toBeUndefined();
		} finally {
			h.cleanup();
		}
	});

	it("a journal whose file still has the sha it had before the merge is started over, even when the file has lines like pull's markers", async () => {
		const h = await setup();
		try {
			const before = h.state().docs[ID];
			// The user documents a conflict, literally.
			const mine = h
				.read()
				.replace(
					"Last.",
					"Last.\n\n```text\n<<<<<<< local\nx\n=======\ny\n>>>>>>> Paca\n```",
				);
			h.write(mine);
			// As a merge stopped after step 1 leaves it.
			const next =
				"# Plan\n\nFirst, by someone.\n\nRed one.\n\nThird.\n\nLast.\n";
			fs.writeFileSync(
				path.join(h.mirror, ".paca-base", `${ID}.next.md`),
				next,
			);
			const s = h.state();
			s.docs[ID].merge_pending = {
				remote_hash: "elsewhere",
				merged_sha: sha("what the merge would write\n"),
				local_sha: sha(mine),
				remote_sha: sha(next),
			};
			fs.writeFileSync(
				path.join(h.mirror, ".paca-sync.json"),
				`${JSON.stringify(s, null, 2)}\n`,
			);
			expect(await h.pull(), h.said()).toBe(0);
			expect(h.read()).toBe(mine);
			expect(h.leftovers()).toEqual([]);
			const now = h.state().docs[ID];
			expect(now.content_hash).toBe(before.content_hash);
			expect(now.file_sha256).toBe(before.file_sha256);
			expect(now.conflict).toBeUndefined();
			expect(now.merge_pending).toBeUndefined();
		} finally {
			h.cleanup();
		}
	});

	it("a conflicting merge stopped after its file write, then an edit here without resolving: the restart keeps one set of markers, and a resolution pushes", async () => {
		const h = await setup();
		try {
			h.write(h.read().replace("First.", "First, mine."));
			h.server.edit(ID, (b) => {
				b[1].content[0].text = "First, by Ann.";
			});
			const k = await killed(h.pull, h.mirror, 3);
			expect(k.writes).toEqual([`${ID}.next.md`, ".paca-sync.json", "Plan.md"]);
			h.write(h.read().replace("Last.", "Last, mine."));
			expect(await h.pull()).toBe(2);
			h.said();
			expect(opens(h.read())).toBe(1);
			const resolved = h
				.read()
				.replace(
					/<<<<<<< local\n[\s\S]*?>>>>>>> Paca[^\n]*\n/,
					"First, mine and Ann's.\n",
				);
			h.write(resolved);
			// Pushed straight away: the state already records Ann's version.
			expect(await h.push(), h.said()).toBe(0);
			expect(h.render()).toBe(resolved);
		} finally {
			h.cleanup();
		}
	});

	it("a conflicting merge stopped after its base copy, then the conflict resolved to Paca's side: the restart finishes it", async () => {
		const h = await setup();
		try {
			h.write(h.read().replace("First.", "First, mine."));
			h.server.edit(ID, (b) => {
				b[1].content[0].text = "First, by Ann.";
			});
			await killed(h.pull, h.mirror, 4);
			// No snapshot to fall back on, as on a server that pruned it.
			(h.server.client as any).listSnapshots = undefined;
			(h.server.client as any).getSnapshot = undefined;
			const theirs = h
				.read()
				.replace(
					/<<<<<<< local\n[\s\S]*?=======\n([\s\S]*?)>>>>>>> Paca[^\n]*\n/,
					"$1",
				);
			h.write(theirs.replace("Last.", "Last, mine."));
			expect(await h.pull(), h.said()).toBe(0);
			expect(h.state().docs[ID].merge_pending).toBeUndefined();
			expect(await h.push()).toBe(0);
			expect(h.render()).toContain("First, by Ann.");
			expect(h.render()).toContain("Last, mine.");
		} finally {
			h.cleanup();
		}
	});

	it("a later error after a merge keeps the merge: the next pull finishes it without merging again", async () => {
		const h = await setup();
		try {
			h.server.add(ID2, "Zeta", storedBlocks("Zeta one.\n\nZeta two."));
			expect(await h.pull()).toBe(0);
			h.said();
			// Plan conflicts; Zeta merges cleanly but its file write fails.
			h.write(h.read().replace("First.", "First, mine."));
			h.server.edit(ID, (b) => {
				b[1].content[0].text = "First, by Ann.";
			});
			h.write(
				h.read("Zeta.md").replace("Zeta two.", "Zeta two, mine."),
				"Zeta.md",
			);
			h.server.edit(ID2, (b) => {
				b[0].content[0].text = "Zeta one, by Ann.";
			});
			stop.mode = "throw";
			stop.only = "Zeta.md";
			stop.armed = true;
			await expect(h.pull()).rejects.toThrow("disk full");
			stop.armed = false;
			const conflicted = h.read();
			expect(opens(conflicted)).toBe(1);
			expect(h.state().docs[ID].merge_pending).toBeDefined();
			expect(await h.pull()).toBe(2);
			expect(h.read()).toBe(conflicted);
			expect(h.read("Zeta.md")).toBe("Zeta one, by Ann.\n\nZeta two, mine.\n");
			expect(h.leftovers()).toEqual([]);
		} finally {
			h.cleanup();
		}
	});
});

describe("settled", () => {
	it("drops what waits to be merged, but never the unknown base, nor Paca's version written next to such a file", () => {
		const unknown: any = {
			file: "Plan.md",
			content_hash: "R0",
			file_sha256: "s",
			remote_hash: "R1",
			unmerged: true,
			remote_copy: { file: "Plan.remote.md", sha256: "c" },
		};
		settled(unknown);
		expect(unknown.remote_hash).toBeUndefined();
		expect(unknown.unmerged).toBe(true);
		expect(unknown.remote_copy).toEqual({
			file: "Plan.remote.md",
			sha256: "c",
		});
		const known: any = {
			file: "Plan.md",
			content_hash: "R0",
			file_sha256: "s",
			remote_hash: "R1",
			remote_copy: { file: "Plan.remote.md", sha256: "c" },
		};
		settled(known);
		expect(known.remote_hash).toBeUndefined();
		expect(known.remote_copy).toBeUndefined();
	});
});
