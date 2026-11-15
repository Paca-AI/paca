import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { runPull } from "../../cli/pull.js";
import { runPush } from "../../cli/push.js";
import { markdownToBlocknote } from "../../utils/index.js";

const ID = (n: number) =>
	`${String(n).repeat(8)}-aaaa-4bbb-8ccc-${String(n).padStart(12, "0")}`;

interface ServerDoc {
	id: string;
	title: string;
	folder_id: string | null;
	text: string;
	source?: { repository: string; path: string };
}

/** A fake Paca: docs and folders you change between pulls, and a feed you
 *  script per call. batchGet answers from the docs. */
function fakeServer() {
	const docs = new Map<string, ServerDoc>();
	const toDoc = (d: ServerDoc) => ({
		id: d.id,
		title: d.title,
		folder_id: d.folder_id,
		content: markdownToBlocknote(d.text),
		content_hash: `h:${d.text}`,
		position: 0,
		created_at: "",
		updated_at: "",
		source: d.source ?? null,
		source_locked: false,
	});
	const entry = (d: ServerDoc) => ({
		id: d.id,
		folder_id: d.folder_id,
		title: d.title,
		content_hash: `h:${d.text}`,
		updated_at: "t",
		deleted: false,
	});
	const tomb = (id: string) => ({
		id,
		folder_id: null,
		title: "",
		content_hash: null,
		updated_at: "t",
		deleted: true,
	});
	const client = {
		getChanges: vi.fn(),
		batchGet: vi.fn(async (_p: string, ids: string[]) => ({
			status: "ok" as const,
			items: ids.flatMap((id) => {
				const d = docs.get(id);
				return d ? [toDoc(d)] : [];
			}),
		})),
	};
	/** Queues the next feed answer. */
	const feed = (
		changed: Array<ReturnType<typeof entry> | ReturnType<typeof tomb>>,
		opts: { reset?: boolean; folders?: unknown[] | null; cursor?: string } = {},
	) =>
		client.getChanges.mockResolvedValueOnce({
			status: "ok",
			cursor: opts.cursor ?? `c${client.getChanges.mock.calls.length + 1}`,
			reset: opts.reset ?? false,
			docs: changed,
			folders: opts.folders ?? null,
		});
	/** Queues a full reset listing every doc. */
	const resetFeed = (folders: unknown[] = []) =>
		feed([...docs.values()].map(entry), { reset: true, folders });
	return { docs, client, entry, tomb, feed, resetFeed };
}

/** Temporary files pull leaves anywhere under `root`. */
function tmpFiles(root: string): string[] {
	const out: string[] = [];
	const walk = (d: string) => {
		for (const e of fs.readdirSync(d, { withFileTypes: true })) {
			if (e.name.startsWith(".paca-tmp-")) out.push(path.join(d, e.name));
			if (e.isDirectory()) walk(path.join(d, e.name));
		}
	};
	walk(root);
	return out;
}

describe("runPull", () => {
	let dir: string;
	let lines: string[];
	const io = {
		out: (l: string) => lines.push(l),
		err: (l: string) => lines.push(`ERR ${l}`),
	};
	const read = (f: string) => fs.readFileSync(path.join(dir, f), "utf8");
	const exists = (f: string) => fs.existsSync(path.join(dir, f));
	const state = () =>
		JSON.parse(fs.readFileSync(path.join(dir, ".paca-sync.json"), "utf8"));
	const pull = (srv: ReturnType<typeof fakeServer>, force = false) =>
		runPull({ projectId: "p1", dir, force }, srv.client, io);

	beforeEach(() => {
		dir = path.join(fs.mkdtempSync(path.join(os.tmpdir(), "paca-pull-")), "m");
		lines = [];
	});
	afterEach(() =>
		fs.rmSync(path.dirname(dir), { recursive: true, force: true }),
	);

	it("first pull writes every doc, a base copy and the state", async () => {
		const srv = fakeServer();
		srv.docs.set(ID(1), {
			id: ID(1),
			title: "A",
			folder_id: "f1",
			text: "alpha",
		});
		srv.resetFeed([{ id: "f1", parent_id: null, name: "Plans" }]);
		expect(await pull(srv)).toBe(0);
		expect(read("Plans/A.md")).toContain("alpha");
		expect(read(`.paca-base/${ID(1)}.md`)).toBe(read("Plans/A.md"));
		const s = state();
		expect(s.project).toBe("p1");
		expect(s.cursor).toBe("c1");
		expect(s.folders).toEqual({ f1: { parent_id: null, name: "Plans" } });
		expect(s.docs[ID(1)]).toMatchObject({
			file: "Plans/A.md",
			content_hash: "h:alpha",
		});
		expect(s.docs[ID(1)].file_sha256).toMatch(/^[0-9a-f]{64}$/);
		expect(lines[0]).toBe("1 updated");
		expect(lines).toContain("updated Plans/A.md");
	});

	it("up to date makes one request and writes nothing", async () => {
		const srv = fakeServer();
		srv.docs.set(ID(1), {
			id: ID(1),
			title: "A",
			folder_id: null,
			text: "alpha",
		});
		srv.resetFeed();
		await pull(srv);
		const before = fs.statSync(path.join(dir, "A.md")).mtimeMs;
		const stateBefore = fs.readFileSync(
			path.join(dir, ".paca-sync.json"),
			"utf8",
		);
		srv.client.batchGet.mockClear();
		srv.feed([], { cursor: "c1" });
		lines = [];
		expect(await pull(srv)).toBe(0);
		expect(srv.client.getChanges).toHaveBeenCalledTimes(2);
		expect(srv.client.getChanges.mock.calls[1]).toEqual(["p1", "c1"]);
		expect(srv.client.batchGet).not.toHaveBeenCalled();
		expect(fs.statSync(path.join(dir, "A.md")).mtimeMs).toBe(before);
		expect(fs.readFileSync(path.join(dir, ".paca-sync.json"), "utf8")).toBe(
			stateBefore,
		);
		expect(lines).toEqual(["up to date"]);
	});

	it("edit, move and delete update the mirror", async () => {
		const srv = fakeServer();
		srv.docs.set(ID(1), {
			id: ID(1),
			title: "A",
			folder_id: null,
			text: "one",
		});
		srv.docs.set(ID(2), {
			id: ID(2),
			title: "B",
			folder_id: null,
			text: "two",
		});
		srv.docs.set(ID(3), {
			id: ID(3),
			title: "C",
			folder_id: null,
			text: "three",
		});
		srv.resetFeed();
		await pull(srv);

		srv.docs.set(ID(1), {
			id: ID(1),
			title: "A",
			folder_id: null,
			text: "one v2",
		});
		srv.docs.set(ID(2), {
			id: ID(2),
			title: "B2",
			folder_id: null,
			text: "two",
		});
		srv.docs.delete(ID(3));
		srv.client.batchGet.mockClear();
		srv.feed([
			srv.entry(srv.docs.get(ID(1)) as ServerDoc),
			srv.entry(srv.docs.get(ID(2)) as ServerDoc),
			srv.tomb(ID(3)),
		]);
		lines = [];
		expect(await pull(srv)).toBe(0);
		expect(read("A.md")).toContain("one v2");
		expect(exists("B.md")).toBe(false);
		expect(read("B2.md")).toContain("two");
		expect(exists("C.md")).toBe(false);
		expect(exists(`.paca-base/${ID(3)}.md`)).toBe(false);
		// Docs the feed lists are fetched (to refresh title and source), but
		// the renamed one is moved, not written again.
		expect(srv.client.batchGet.mock.calls.map((c) => c[1])).toEqual([
			[ID(1), ID(2)],
		]);
		expect(lines).not.toContain("updated B2.md");
		expect(lines[0]).toBe("1 updated, 1 moved, 1 removed");
		expect(lines).toContain("moved B.md -> B2.md");
		expect(lines).toContain("removed C.md");
		expect(Object.keys(state().docs).sort()).toEqual([ID(1), ID(2)]);
	});

	it("reset removes files for docs no longer listed", async () => {
		const srv = fakeServer();
		srv.docs.set(ID(1), {
			id: ID(1),
			title: "A",
			folder_id: null,
			text: "one",
		});
		srv.docs.set(ID(2), {
			id: ID(2),
			title: "B",
			folder_id: null,
			text: "two",
		});
		srv.resetFeed();
		await pull(srv);
		srv.docs.delete(ID(2)); // access lost: no tombstone, a reset instead
		srv.client.batchGet.mockClear();
		srv.resetFeed();
		expect(await pull(srv)).toBe(0);
		expect(exists("A.md")).toBe(true);
		expect(exists("B.md")).toBe(false);
		expect(srv.client.batchGet).not.toHaveBeenCalled();
		expect(Object.keys(state().docs)).toEqual([ID(1)]);
	});

	it("an id batch-get leaves out is treated as removed", async () => {
		const srv = fakeServer();
		srv.docs.set(ID(1), {
			id: ID(1),
			title: "A",
			folder_id: null,
			text: "one",
		});
		srv.resetFeed();
		await pull(srv);
		const gone = { ...(srv.docs.get(ID(1)) as ServerDoc), text: "secret" };
		srv.docs.delete(ID(1));
		srv.feed([srv.entry(gone)]);
		expect(await pull(srv)).toBe(0);
		expect(exists("A.md")).toBe(false);
		expect(state().docs).toEqual({});
	});

	it("folder rename moves files without fetching content", async () => {
		const srv = fakeServer();
		srv.docs.set(ID(1), {
			id: ID(1),
			title: "A",
			folder_id: "f2",
			text: "one",
		});
		srv.docs.set(ID(2), {
			id: ID(2),
			title: "B",
			folder_id: "f1",
			text: "two",
		});
		srv.resetFeed([
			{ id: "f1", parent_id: null, name: "Old" },
			{ id: "f2", parent_id: "f1", name: "Sub" },
		]);
		await pull(srv);
		srv.client.batchGet.mockClear();
		srv.feed([], {
			folders: [
				{ id: "f1", parent_id: null, name: "New" },
				{ id: "f2", parent_id: "f1", name: "Sub" },
			],
		});
		expect(await pull(srv)).toBe(0);
		expect(read("New/Sub/A.md")).toContain("one");
		expect(read("New/B.md")).toContain("two");
		expect(exists("Old")).toBe(false); // emptied folders are pruned
		expect(srv.client.batchGet).not.toHaveBeenCalled();
		expect(state().folders.f1.name).toBe("New");
	});

	it("keeps the folder map when the feed sends folders: null", async () => {
		const srv = fakeServer();
		srv.docs.set(ID(1), {
			id: ID(1),
			title: "A",
			folder_id: "f1",
			text: "one",
		});
		srv.resetFeed([{ id: "f1", parent_id: null, name: "F" }]);
		await pull(srv);
		srv.docs.set(ID(2), {
			id: ID(2),
			title: "B",
			folder_id: "f1",
			text: "two",
		});
		srv.feed([srv.entry(srv.docs.get(ID(2)) as ServerDoc)]);
		expect(await pull(srv)).toBe(0);
		expect(read("F/B.md")).toContain("two");
	});

	it("unsafe and colliding titles stay inside the mirror and apart", async () => {
		const srv = fakeServer();
		const titles = ["a:b", "..", "../../escape", "Notes", "Notes", "X"];
		titles.forEach((title, i) => {
			srv.docs.set(ID(i + 1), {
				id: ID(i + 1),
				title,
				folder_id: null,
				text: `body ${i + 1}`,
			});
		});
		srv.docs.set(ID(7), {
			id: ID(7),
			title: "inside",
			folder_id: "fx",
			text: "body 7",
		});
		srv.resetFeed([{ id: "fx", parent_id: null, name: "X" }]);
		expect(await pull(srv)).toBe(0);
		const files = Object.values(state().docs).map((d: any) => d.file as string);
		expect(new Set(files.map((f) => f.toLowerCase())).size).toBe(7);
		for (const [i, f] of files.entries()) {
			const abs = path.resolve(dir, f);
			expect(abs.startsWith(path.resolve(dir) + path.sep)).toBe(true);
			expect(fs.readFileSync(abs, "utf8")).toContain("body");
			expect(f).not.toMatch(/(^|\/)\.\.(\/|$)/);
			void i;
		}
		expect(files).toContain("a_b.md");
		expect(files).toContain("_.md");
		expect(files).toContain("Notes.md");
		expect(files).toContain(`Notes~${ID(5).slice(0, 8)}.md`);
		expect(files).toContain("X.md");
		expect(files).toContain("X/inside.md");
		expect(fs.readdirSync(path.dirname(dir))).toEqual(["m"]);
	});

	it("names stay stable when a same-titled doc appears later", async () => {
		const srv = fakeServer();
		srv.docs.set(ID(5), {
			id: ID(5),
			title: "Notes",
			folder_id: null,
			text: "five",
		});
		srv.resetFeed();
		await pull(srv);
		srv.docs.set(ID(1), {
			id: ID(1),
			title: "Notes",
			folder_id: null,
			text: "one",
		});
		srv.feed([srv.entry(srv.docs.get(ID(1)) as ServerDoc)]);
		await pull(srv);
		expect(read("Notes.md")).toContain("five");
		expect(read(`Notes~${ID(1).slice(0, 8)}.md`)).toContain("one");
	});

	it("never overwrites a file in the mirror that did not come from Paca", async () => {
		const srv = fakeServer();
		fs.mkdirSync(dir, { recursive: true });
		fs.writeFileSync(path.join(dir, "A.md"), "mine");
		srv.docs.set(ID(1), {
			id: ID(1),
			title: "A",
			folder_id: null,
			text: "one",
		});
		srv.resetFeed();
		expect(await pull(srv)).toBe(0);
		expect(read("A.md")).toBe("mine");
		expect(read(`A~${ID(1).slice(0, 8)}.md`)).toContain("one");
	});

	it("a file edited here and in Paca on the same line gets conflict markers, keeps the local text, and exits 2", async () => {
		const srv = fakeServer();
		srv.docs.set(ID(1), {
			id: ID(1),
			title: "A",
			folder_id: null,
			text: "one",
		});
		srv.resetFeed();
		await pull(srv);
		fs.writeFileSync(path.join(dir, "A.md"), "my local edit\n");
		srv.docs.set(ID(1), {
			id: ID(1),
			title: "A",
			folder_id: null,
			text: "one v2",
		});
		srv.feed([srv.entry(srv.docs.get(ID(1)) as ServerDoc)]);
		lines = [];
		expect(await pull(srv)).toBe(2);
		expect(read("A.md")).toBe(
			"<<<<<<< local\nmy local edit\n||||||| base\none\n=======\none v2\n>>>>>>> Paca\n",
		);
		expect(lines.join("\n")).toContain(
			"conflict A.md: edited here and in Paca",
		);
		// Base and state are Paca's version: the file is a local edit of it.
		expect(read(`.paca-base/${ID(1)}.md`)).toBe("one v2\n");
		expect(state().docs[ID(1)]).toMatchObject({ content_hash: "h:one v2" });
		// push refuses the file while it has the markers.
		lines = [];
		expect(
			await runPush(
				{
					projectId: "p1",
					file: "m/A.md",
					lock: false,
					force: false,
					dryRun: false,
					client: "paca-cli",
				},
				srv.client as any,
				io,
				{ cwd: path.dirname(dir) },
			),
		).toBe(1);
		expect(lines.join("\n")).toContain("conflict markers (line 1)");
		// The next pull reports the edit, and --force overwrites.
		srv.feed([]);
		lines = [];
		expect(await pull(srv)).toBe(0);
		expect(lines).toContain("modified locally A.md");
		srv.feed([]);
		expect(await pull(srv, true)).toBe(0);
		expect(read("A.md")).toBe("one v2\n");
	});

	it("a locally edited file is left alone when the doc did not change", async () => {
		const srv = fakeServer();
		srv.docs.set(ID(1), {
			id: ID(1),
			title: "A",
			folder_id: null,
			text: "one",
		});
		srv.docs.set(ID(2), {
			id: ID(2),
			title: "B",
			folder_id: null,
			text: "two",
		});
		srv.resetFeed();
		await pull(srv);
		fs.writeFileSync(path.join(dir, "A.md"), "edit");
		srv.docs.set(ID(2), {
			id: ID(2),
			title: "B",
			folder_id: null,
			text: "two v2",
		});
		srv.feed([srv.entry(srv.docs.get(ID(2)) as ServerDoc)]);
		expect(await pull(srv)).toBe(0);
		expect(read("A.md")).toBe("edit");
	});

	it("a deleted doc whose file was edited locally is kept and exits 2", async () => {
		const srv = fakeServer();
		srv.docs.set(ID(1), {
			id: ID(1),
			title: "A",
			folder_id: null,
			text: "one",
		});
		srv.resetFeed();
		await pull(srv);
		fs.writeFileSync(path.join(dir, "A.md"), "edit");
		srv.docs.delete(ID(1));
		srv.feed([srv.tomb(ID(1))]);
		lines = [];
		expect(await pull(srv)).toBe(2);
		expect(read("A.md")).toBe("edit");
		expect(lines.join("\n")).toContain("kept A.md");
		// Reported on every pull until the file is removed or --force is used.
		srv.feed([]);
		lines = [];
		expect(await pull(srv)).toBe(2);
		expect(lines.join("\n")).toContain(
			"kept A.md: edited locally; the document was removed in Paca (delete the file, or pull --force to remove it)",
		);
		fs.rmSync(path.join(dir, "A.md"));
		srv.feed([]);
		expect(await pull(srv)).toBe(0);
		expect(state().docs).toEqual({});
	});

	describe("a kept file whose document comes back under another name", () => {
		// O ("X") is pulled, X.md is edited, O is deleted (X.md kept), then O
		// comes back as "Y".
		const setup = async () => {
			const srv = fakeServer();
			srv.docs.set(ID(1), {
				id: ID(1),
				title: "X",
				folder_id: null,
				text: "o",
			});
			srv.resetFeed();
			await pull(srv);
			fs.writeFileSync(path.join(dir, "X.md"), "MY EDIT\n");
			srv.docs.delete(ID(1));
			srv.feed([srv.tomb(ID(1))]);
			expect(await pull(srv)).toBe(2);
			srv.docs.set(ID(1), {
				id: ID(1),
				title: "Y",
				folder_id: null,
				text: "o",
			});
			return srv;
		};

		it("moves with its document and is kept when a new document takes its old name", async () => {
			const srv = await setup();
			srv.docs.set(ID(2), {
				id: ID(2),
				title: "X",
				folder_id: null,
				text: "new doc",
			});
			srv.feed([
				srv.entry(srv.docs.get(ID(1)) as ServerDoc),
				srv.entry(srv.docs.get(ID(2)) as ServerDoc),
			]);
			lines = [];
			expect(await pull(srv)).toBe(2);
			expect(read("Y.md")).toBe("MY EDIT\n");
			expect(read("X.md")).toContain("new doc");
			expect(lines.join("\n")).toContain("kept Y.md: edited locally");
			expect(state().docs[ID(1)]).toMatchObject({ file: "Y.md" });
			expect(state().docs[ID(1)].orphan).toBeUndefined();
			// Same server content as before the delete: like any edited file of
			// an unchanged document, it is left alone from now on.
			srv.feed([]);
			expect(await pull(srv)).toBe(0);
			expect(read("Y.md")).toBe("MY EDIT\n");
		});

		it("is not moved over when a live document is renamed onto its old name", async () => {
			const srv = await setup();
			srv.docs.set(ID(3), {
				id: ID(3),
				title: "L",
				folder_id: null,
				text: "live",
			});
			srv.feed([srv.entry(srv.docs.get(ID(3)) as ServerDoc)]);
			await pull(srv);
			srv.docs.set(ID(3), {
				id: ID(3),
				title: "X",
				folder_id: null,
				text: "live",
			});
			srv.feed([
				srv.entry(srv.docs.get(ID(1)) as ServerDoc),
				srv.entry(srv.docs.get(ID(3)) as ServerDoc),
			]);
			expect(await pull(srv)).toBe(2);
			expect(read("Y.md")).toBe("MY EDIT\n");
			expect(read("X.md")).toContain("live");
			expect(exists("L.md")).toBe(false);
			srv.feed([]);
			expect(await pull(srv)).toBe(0);
		});
	});

	it("pull --force removes an edited file whose document was deleted", async () => {
		const srv = fakeServer();
		srv.docs.set(ID(1), {
			id: ID(1),
			title: "A",
			folder_id: null,
			text: "one",
		});
		srv.resetFeed();
		await pull(srv);
		fs.writeFileSync(path.join(dir, "A.md"), "edit");
		srv.docs.delete(ID(1));
		srv.feed([srv.tomb(ID(1))]);
		expect(await pull(srv)).toBe(2);
		srv.feed([]);
		expect(await pull(srv, true)).toBe(0);
		expect(exists("A.md")).toBe(false);
		expect(state().docs).toEqual({});
	});

	it("state is unchanged when a write fails", async () => {
		const srv = fakeServer();
		srv.docs.set(ID(1), {
			id: ID(1),
			title: "A",
			folder_id: null,
			text: "one",
		});
		srv.resetFeed();
		await pull(srv);
		const stateBefore = fs.readFileSync(
			path.join(dir, ".paca-sync.json"),
			"utf8",
		);
		// The base copy folder is now a file: writing the base copy fails.
		fs.rmSync(path.join(dir, ".paca-base"), { recursive: true });
		fs.writeFileSync(path.join(dir, ".paca-base"), "");
		srv.docs.set(ID(1), {
			id: ID(1),
			title: "A",
			folder_id: null,
			text: "one v2",
		});
		srv.feed([srv.entry(srv.docs.get(ID(1)) as ServerDoc)]);
		await expect(pull(srv)).rejects.toThrow();
		expect(fs.readFileSync(path.join(dir, ".paca-sync.json"), "utf8")).toBe(
			stateBefore,
		);
		// Once fixed, the half-done write is not mistaken for a local edit.
		fs.rmSync(path.join(dir, ".paca-base"));
		srv.feed([srv.entry(srv.docs.get(ID(1)) as ServerDoc)]);
		expect(await pull(srv)).toBe(0);
		expect(read("A.md")).toContain("one v2");
	});

	it("records managed: true for a doc that has a source", async () => {
		const srv = fakeServer();
		srv.docs.set(ID(1), {
			id: ID(1),
			title: "A",
			folder_id: null,
			text: "one",
			source: { repository: "o/r", path: "docs/a.md" },
		});
		srv.docs.set(ID(2), {
			id: ID(2),
			title: "B",
			folder_id: null,
			text: "two",
		});
		srv.resetFeed();
		await pull(srv);
		expect(state().docs[ID(1)].managed).toBe(true);
		expect(state().docs[ID(2)].managed).toBeUndefined();
	});

	it("fetches in batches of at most 100", async () => {
		const srv = fakeServer();
		for (let i = 0; i < 150; i++) {
			const id = `00000000-0000-4000-8000-${String(i).padStart(12, "0")}`;
			srv.docs.set(id, { id, title: `d${i}`, folder_id: null, text: `t${i}` });
		}
		srv.resetFeed();
		expect(await pull(srv)).toBe(0);
		expect(srv.client.batchGet.mock.calls.map((c) => c[1].length)).toEqual([
			100, 50,
		]);
	});

	it("skips a doc whose id is not a plain id", async () => {
		const srv = fakeServer();
		srv.docs.set("../../x", {
			id: "../../x",
			title: "A",
			folder_id: null,
			text: "one",
		});
		srv.resetFeed();
		expect(await pull(srv)).toBe(0);
		expect(fs.readdirSync(path.dirname(dir))).toEqual(["m"]);
		expect(state().docs).toEqual({});
	});

	it("a server without the feed exits 1", async () => {
		const srv = fakeServer();
		srv.client.getChanges.mockResolvedValueOnce({ status: "unsupported" });
		expect(await pull(srv)).toBe(1);
		expect(lines.join("\n")).toContain(
			"this Paca server is too old for pull/push",
		);
	});

	it("refuses a mirror folder that belongs to another project", async () => {
		const srv = fakeServer();
		srv.resetFeed();
		await pull(srv);
		const res = await runPull(
			{ projectId: "p2", dir, force: false },
			srv.client,
			io,
		);
		expect(res).toBe(1);
		expect(lines.join("\n")).toContain("belongs to project p1");
	});

	it("a failed move is rolled back and the local edit survives", async () => {
		const srv = fakeServer();
		srv.docs.set(ID(1), {
			id: ID(1),
			title: "A",
			folder_id: null,
			text: "one",
		});
		srv.docs.set(ID(2), {
			id: ID(2),
			title: "B",
			folder_id: "fs",
			text: "two",
		});
		srv.resetFeed([{ id: "fs", parent_id: null, name: "Sub" }]);
		await pull(srv);
		const stateBefore = fs.readFileSync(
			path.join(dir, ".paca-sync.json"),
			"utf8",
		);
		fs.writeFileSync(path.join(dir, "A.md"), "LOCAL EDIT\n");
		fs.chmodSync(path.join(dir, "Sub"), 0o555); // the move into Sub fails
		srv.docs.set(ID(1), {
			id: ID(1),
			title: "A",
			folder_id: "fs",
			text: "one",
		});
		srv.feed([srv.entry(srv.docs.get(ID(1)) as ServerDoc)]);
		try {
			await expect(pull(srv)).rejects.toThrow();
		} finally {
			fs.chmodSync(path.join(dir, "Sub"), 0o755);
		}
		expect(read("A.md")).toBe("LOCAL EDIT\n");
		expect(tmpFiles(dir)).toEqual([]);
		expect(fs.readFileSync(path.join(dir, ".paca-sync.json"), "utf8")).toBe(
			stateBefore,
		);
		srv.feed([srv.entry(srv.docs.get(ID(1)) as ServerDoc)]);
		expect(await pull(srv)).toBe(0);
		expect(read("Sub/A.md")).toBe("LOCAL EDIT\n");
	});

	it("a move stranded by a crash is put back on the next pull", async () => {
		const srv = fakeServer();
		srv.docs.set(ID(1), {
			id: ID(1),
			title: "A",
			folder_id: null,
			text: "one",
		});
		srv.resetFeed();
		await pull(srv);
		fs.writeFileSync(path.join(dir, `.paca-tmp-move-${ID(1)}`), "LOCAL EDIT\n");
		fs.rmSync(path.join(dir, "A.md"));
		srv.docs.set(ID(1), {
			id: ID(1),
			title: "A",
			folder_id: null,
			text: "one v2",
		});
		srv.feed([srv.entry(srv.docs.get(ID(1)) as ServerDoc)]);
		lines = [];
		expect(await pull(srv)).toBe(2);
		expect(read("A.md")).toBe(
			"<<<<<<< local\nLOCAL EDIT\n||||||| base\none\n=======\none v2\n>>>>>>> Paca\n",
		);
		expect(lines.join("\n")).toContain(
			"conflict A.md: edited here and in Paca",
		);
		expect(tmpFiles(dir)).toEqual([]);
	});

	it("leftover temporary files are removed, other .paca-tmp files are not", async () => {
		const srv = fakeServer();
		srv.docs.set(ID(1), { id: ID(1), title: "A", folder_id: "f", text: "one" });
		srv.resetFeed([{ id: "f", parent_id: null, name: "F" }]);
		await pull(srv);
		fs.writeFileSync(
			path.join(dir, "F", ".paca-tmp-w-0123456789abcdef"),
			"half",
		);
		fs.writeFileSync(path.join(dir, ".paca-tmp-notes"), "mine");
		fs.writeFileSync(path.join(dir, "F", ".paca-tmp-123-abc"), "mine too");
		srv.feed([]);
		expect(await pull(srv)).toBe(0);
		expect(exists("F/.paca-tmp-w-0123456789abcdef")).toBe(false);
		expect(read(".paca-tmp-notes")).toBe("mine");
		expect(read("F/.paca-tmp-123-abc")).toBe("mine too");
	});

	it("rolls back a swap of two files when a later step fails", async () => {
		const srv = fakeServer();
		srv.docs.set(ID(1), { id: ID(1), title: "X", folder_id: null, text: "A1" });
		srv.docs.set(ID(2), { id: ID(2), title: "Y", folder_id: null, text: "B1" });
		srv.resetFeed();
		await pull(srv);
		const stateBefore = fs.readFileSync(
			path.join(dir, ".paca-sync.json"),
			"utf8",
		);
		srv.docs.set(ID(1), { id: ID(1), title: "Y", folder_id: null, text: "A2" });
		srv.docs.set(ID(2), { id: ID(2), title: "X", folder_id: null, text: "B1" });
		fs.rmSync(path.join(dir, ".paca-base"), { recursive: true });
		fs.writeFileSync(path.join(dir, ".paca-base"), "");
		const changes = () =>
			srv.feed([
				srv.entry(srv.docs.get(ID(1)) as ServerDoc),
				srv.entry(srv.docs.get(ID(2)) as ServerDoc),
			]);
		changes();
		await expect(pull(srv)).rejects.toThrow();
		// Each file is back at the path the state records. A's file may already
		// hold A's new server content (it was unedited); the next pull adopts it.
		expect(["A1", "A2"]).toContain(read("X.md").trim());
		expect(read("Y.md").trim()).toBe("B1");
		expect(tmpFiles(dir)).toEqual([]);
		expect(fs.readFileSync(path.join(dir, ".paca-sync.json"), "utf8")).toBe(
			stateBefore,
		);
		fs.rmSync(path.join(dir, ".paca-base"));
		changes();
		lines = [];
		expect(await pull(srv)).toBe(0);
		expect(read("Y.md").trim()).toBe("A2");
		expect(read("X.md").trim()).toBe("B1");
		expect(lines.join("\n")).not.toContain("kept");
	});

	it("rolls back a rename cycle and a file written this run", async () => {
		const srv = fakeServer();
		srv.docs.set(ID(1), { id: ID(1), title: "X", folder_id: null, text: "A1" });
		srv.docs.set(ID(2), { id: ID(2), title: "Y", folder_id: null, text: "B1" });
		srv.docs.set(ID(3), { id: ID(3), title: "Z", folder_id: null, text: "C1" });
		srv.resetFeed();
		await pull(srv);
		srv.docs.set(ID(1), { id: ID(1), title: "Y", folder_id: null, text: "A2" });
		srv.docs.set(ID(2), { id: ID(2), title: "Z", folder_id: null, text: "B1" });
		srv.docs.set(ID(3), { id: ID(3), title: "X", folder_id: null, text: "C1" });
		srv.docs.set(ID(4), {
			id: ID(4),
			title: "New",
			folder_id: null,
			text: "D1",
		});
		fs.rmSync(path.join(dir, ".paca-base"), { recursive: true });
		fs.writeFileSync(path.join(dir, ".paca-base"), "");
		const changes = () =>
			srv.feed(
				[1, 2, 3, 4].map((n) => srv.entry(srv.docs.get(ID(n)) as ServerDoc)),
			);
		changes();
		await expect(pull(srv)).rejects.toThrow();
		expect(["A1", "A2"]).toContain(read("X.md").trim());
		expect(read("Y.md").trim()).toBe("B1");
		expect(read("Z.md").trim()).toBe("C1");
		expect(exists("New.md")).toBe(false);
		expect(tmpFiles(dir)).toEqual([]);
		fs.rmSync(path.join(dir, ".paca-base"));
		changes();
		expect(await pull(srv)).toBe(0);
		expect(read("Y.md").trim()).toBe("A2");
		expect(read("Z.md").trim()).toBe("B1");
		expect(read("X.md").trim()).toBe("C1");
		expect(read("New.md").trim()).toBe("D1");
	});

	it("recovers a stranded move to the mirror root when its folder is now a file", async () => {
		const srv = fakeServer();
		srv.docs.set(ID(1), { id: ID(1), title: "A", folder_id: "f", text: "one" });
		srv.resetFeed([{ id: "f", parent_id: null, name: "F" }]);
		await pull(srv);
		fs.rmSync(path.join(dir, "F"), { recursive: true });
		fs.writeFileSync(path.join(dir, "F"), "a file now");
		fs.writeFileSync(path.join(dir, `.paca-tmp-move-${ID(1)}`), "LOCAL EDIT\n");
		srv.feed([]);
		lines = [];
		const code = await pull(srv);
		expect([0, 2]).toContain(code);
		expect(read(`recovered-${ID(1)}.md`)).toBe("LOCAL EDIT\n");
		expect(read("F")).toBe("a file now");
		expect(lines.join("\n")).toContain(`recovered-${ID(1)}.md`);
	});

	it("a second pull on the same mirror exits 1 while one is running", async () => {
		const srv = fakeServer();
		srv.docs.set(ID(1), {
			id: ID(1),
			title: "A",
			folder_id: null,
			text: "one",
		});
		srv.resetFeed();
		srv.resetFeed();
		lines = [];
		const codes = await Promise.all([pull(srv), pull(srv)]);
		expect(codes.sort()).toEqual([0, 1]);
		expect(lines.join("\n")).toContain("another pull is running in");
		expect(exists(".paca-pull.lock")).toBe(false);
	});

	it("a lock held by a running process stops the pull", async () => {
		const srv = fakeServer();
		fs.mkdirSync(dir, { recursive: true });
		const lock = JSON.stringify({
			pid: process.pid,
			started: "2026-11-14T00:00:00Z",
		});
		fs.writeFileSync(path.join(dir, ".paca-pull.lock"), lock);
		lines = [];
		expect(await pull(srv)).toBe(1);
		expect(lines.join("\n")).toContain("another pull is running in");
		expect(srv.client.getChanges).not.toHaveBeenCalled();
		expect(read(".paca-pull.lock")).toBe(lock);
	});

	it("a stale lock is taken over and reported", async () => {
		const srv = fakeServer();
		srv.docs.set(ID(1), {
			id: ID(1),
			title: "A",
			folder_id: null,
			text: "one",
		});
		fs.mkdirSync(dir, { recursive: true });
		fs.writeFileSync(
			path.join(dir, ".paca-pull.lock"),
			JSON.stringify({ pid: 2 ** 22 + 12345, started: "2026-11-14T00:00:00Z" }),
		);
		srv.resetFeed();
		lines = [];
		expect(await pull(srv)).toBe(0);
		expect(lines.join("\n")).toContain("stale lock");
		expect(exists(".paca-pull.lock")).toBe(false);
	});

	it("an empty lock file older than 10 seconds is taken over", async () => {
		const srv = fakeServer();
		srv.docs.set(ID(1), {
			id: ID(1),
			title: "A",
			folder_id: null,
			text: "one",
		});
		fs.mkdirSync(dir, { recursive: true });
		const lock = path.join(dir, ".paca-pull.lock");
		fs.writeFileSync(lock, "");
		const old = new Date(Date.now() - 60_000);
		fs.utimesSync(lock, old, old);
		srv.resetFeed();
		lines = [];
		expect(await pull(srv)).toBe(0);
		expect(lines.join("\n")).toContain("stale lock");
		expect(exists(".paca-pull.lock")).toBe(false);
	});

	it("a fresh empty lock file still counts as held", async () => {
		const srv = fakeServer();
		fs.mkdirSync(dir, { recursive: true });
		fs.writeFileSync(path.join(dir, ".paca-pull.lock"), "");
		lines = [];
		expect(await pull(srv)).toBe(1);
		expect(lines.join("\n")).toContain("another pull is running in");
	});

	it("the lock is released when a pull fails", async () => {
		const srv = fakeServer();
		srv.client.getChanges.mockRejectedValueOnce(new Error("boom"));
		await expect(pull(srv)).rejects.toThrow("boom");
		expect(exists(".paca-pull.lock")).toBe(false);
	});

	it("a file at a folder's path gives the folder another name", async () => {
		const srv = fakeServer();
		srv.docs.set(ID(1), {
			id: ID(1),
			title: "a",
			folder_id: null,
			text: "one",
		});
		srv.resetFeed();
		await pull(srv);
		fs.writeFileSync(path.join(dir, "a.md"), "LOCAL EDIT\n");
		fs.writeFileSync(path.join(dir, "Sub"), "user file");
		srv.docs.set(ID(1), {
			id: ID(1),
			title: "a",
			folder_id: "fsub-1234",
			text: "one",
		});
		srv.feed([srv.entry(srv.docs.get(ID(1)) as ServerDoc)], {
			folders: [{ id: "fsub-1234", parent_id: null, name: "Sub" }],
		});
		expect(await pull(srv)).toBe(0);
		expect(read("Sub")).toBe("user file");
		expect(read("Sub~fsub-123/a.md")).toBe("LOCAL EDIT\n");
		srv.feed([]);
		expect(await pull(srv)).toBe(0);
	});

	describe("sibling folders with one name key", () => {
		const FA = "aaaaaaaa-folder";
		const FB = "bbbbbbbb-folder";

		it("get two directories and keep both documents", async () => {
			const srv = fakeServer();
			srv.docs.set(ID(1), {
				id: ID(1),
				title: "x",
				folder_id: FA,
				text: "one",
			});
			srv.docs.set(ID(2), {
				id: ID(2),
				title: "x",
				folder_id: FB,
				text: "two",
			});
			srv.resetFeed([
				{ id: FA, parent_id: null, name: "Design" },
				{ id: FB, parent_id: null, name: "design" },
			]);
			expect(await pull(srv)).toBe(0);
			expect(read("Design/x.md")).toContain("one");
			expect(read("design~bbbbbbbb/x.md")).toContain("two");
		});

		it("an existing directory keeps its name when a lower id clashes later", async () => {
			const srv = fakeServer();
			srv.docs.set(ID(2), {
				id: ID(2),
				title: "x",
				folder_id: FB,
				text: "two",
			});
			srv.resetFeed([{ id: FB, parent_id: null, name: "design" }]);
			await pull(srv);
			srv.docs.set(ID(1), {
				id: ID(1),
				title: "x",
				folder_id: FA,
				text: "one",
			});
			srv.client.batchGet.mockClear();
			srv.feed([srv.entry(srv.docs.get(ID(1)) as ServerDoc)], {
				folders: [
					{ id: FA, parent_id: null, name: "Design" },
					{ id: FB, parent_id: null, name: "design" },
				],
			});
			lines = [];
			expect(await pull(srv)).toBe(0);
			expect(lines.join("\n")).not.toContain("moved");
			expect(read("design/x.md")).toContain("two");
			expect(read("Design~aaaaaaaa/x.md")).toContain("one");
			expect(srv.client.batchGet.mock.calls.flat(2)).not.toContain(ID(2));
		});

		it("a suffixed directory keeps its suffix after the clash ends, and a renamed folder moves without downloads", async () => {
			const srv = fakeServer();
			srv.docs.set(ID(1), {
				id: ID(1),
				title: "x",
				folder_id: FA,
				text: "one",
			});
			srv.docs.set(ID(2), {
				id: ID(2),
				title: "x",
				folder_id: FB,
				text: "two",
			});
			srv.resetFeed([
				{ id: FA, parent_id: null, name: "Design" },
				{ id: FB, parent_id: null, name: "design" },
			]);
			await pull(srv);
			srv.client.batchGet.mockClear();
			srv.feed([], {
				folders: [
					{ id: FA, parent_id: null, name: "Renamed" },
					{ id: FB, parent_id: null, name: "design" },
				],
			});
			expect(await pull(srv)).toBe(0);
			expect(read("Renamed/x.md")).toContain("one");
			expect(read("design~bbbbbbbb/x.md")).toContain("two");
			expect(exists("Design")).toBe(false);
			expect(srv.client.batchGet).not.toHaveBeenCalled();
			// Stable on the next pull too.
			srv.feed([]);
			lines = [];
			expect(await pull(srv)).toBe(0);
			expect(lines).toEqual(["up to date"]);
		});
	});

	it("a failed write after a move leaves no duplicate on the next pull", async () => {
		const srv = fakeServer();
		srv.docs.set(ID(1), {
			id: ID(1),
			title: "A",
			folder_id: null,
			text: "one",
		});
		srv.docs.set(ID(2), {
			id: ID(2),
			title: "B",
			folder_id: null,
			text: "two",
		});
		srv.resetFeed();
		await pull(srv);
		srv.docs.set(ID(1), {
			id: ID(1),
			title: "A",
			folder_id: null,
			text: "one v2",
		});
		srv.docs.set(ID(2), {
			id: ID(2),
			title: "B2",
			folder_id: null,
			text: "two",
		});
		fs.rmSync(path.join(dir, ".paca-base"), { recursive: true });
		fs.writeFileSync(path.join(dir, ".paca-base"), "");
		const changes = () =>
			srv.feed([
				srv.entry(srv.docs.get(ID(1)) as ServerDoc),
				srv.entry(srv.docs.get(ID(2)) as ServerDoc),
			]);
		changes();
		await expect(pull(srv)).rejects.toThrow();
		fs.rmSync(path.join(dir, ".paca-base"));
		changes();
		expect(await pull(srv)).toBe(0);
		const mds = fs
			.readdirSync(dir)
			.filter((f) => f.endsWith(".md"))
			.sort();
		expect(mds).toEqual(["A.md", "B2.md"]);
	});

	it("refuses to work through a symlink in the mirror", async () => {
		const srv = fakeServer();
		const outside = path.join(path.dirname(dir), "outside");
		fs.mkdirSync(dir, { recursive: true });
		fs.mkdirSync(outside);
		fs.symlinkSync(outside, path.join(dir, "Plans"));
		srv.docs.set(ID(1), {
			id: ID(1),
			title: "x",
			folder_id: "f1",
			text: "secret",
		});
		srv.resetFeed([{ id: "f1", parent_id: null, name: "Plans" }]);
		lines = [];
		expect(await pull(srv)).toBe(1);
		expect(fs.readdirSync(outside)).toEqual([]);
		expect(lines.join("\n")).toContain("symlink");
		expect(lines.join("\n")).toContain("Plans");
	});

	it("refuses a symlinked .paca-base", async () => {
		const srv = fakeServer();
		const outside = path.join(path.dirname(dir), "outside");
		fs.mkdirSync(dir, { recursive: true });
		fs.mkdirSync(outside);
		fs.symlinkSync(outside, path.join(dir, ".paca-base"));
		srv.docs.set(ID(1), {
			id: ID(1),
			title: "x",
			folder_id: null,
			text: "secret",
		});
		srv.resetFeed();
		expect(await pull(srv)).toBe(1);
		expect(fs.readdirSync(outside)).toEqual([]);
	});

	it("titles that differ only in Unicode normalization get two files", async () => {
		const srv = fakeServer();
		srv.docs.set(ID(1), {
			id: ID(1),
			title: "café",
			folder_id: null,
			text: "one",
		});
		srv.docs.set(ID(2), {
			id: ID(2),
			title: "café",
			folder_id: null,
			text: "two",
		});
		srv.resetFeed();
		expect(await pull(srv)).toBe(0);
		const bodies = fs
			.readdirSync(dir)
			.filter((f) => f.endsWith(".md"))
			.map((f) => fs.readFileSync(path.join(dir, f), "utf8").trim())
			.sort();
		expect(bodies).toEqual(["one", "two"]);
	});

	it("refreshes managed when a doc gains a source without a content change", async () => {
		const srv = fakeServer();
		srv.docs.set(ID(1), {
			id: ID(1),
			title: "A",
			folder_id: null,
			text: "one",
		});
		srv.resetFeed();
		await pull(srv);
		const mtime = fs.statSync(path.join(dir, "A.md")).mtimeMs;
		srv.docs.set(ID(1), {
			id: ID(1),
			title: "A",
			folder_id: null,
			text: "one",
			source: { repository: "o/r", path: "a.md" },
		});
		srv.feed([srv.entry(srv.docs.get(ID(1)) as ServerDoc)]);
		expect(await pull(srv)).toBe(0);
		expect(state().docs[ID(1)].managed).toBe(true);
		expect(fs.statSync(path.join(dir, "A.md")).mtimeMs).toBe(mtime);
		srv.docs.set(ID(1), {
			id: ID(1),
			title: "A",
			folder_id: null,
			text: "one",
		});
		srv.feed([srv.entry(srv.docs.get(ID(1)) as ServerDoc)]);
		await pull(srv);
		expect(state().docs[ID(1)].managed).toBeUndefined();
	});

	it("ignores a state entry whose id is not a plain id", async () => {
		const srv = fakeServer();
		srv.resetFeed();
		await pull(srv);
		fs.writeFileSync(path.join(dir, "notes.md"), "mine");
		const s = state();
		s.docs["../notes"] = {
			file: "notes.md",
			title: "notes",
			folder_id: null,
			content_hash: "x",
			file_sha256: "y",
		};
		fs.writeFileSync(path.join(dir, ".paca-sync.json"), JSON.stringify(s));
		srv.resetFeed();
		lines = [];
		expect(await pull(srv)).toBe(0);
		expect(read("notes.md")).toBe("mine");
		expect(lines.join("\n")).toContain("ignored a state entry");
		expect(state().docs).toEqual({});
	});
});
