import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { runPull } from "../../cli/pull.js";
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
		// Only the edited doc is fetched; the moved one is renamed.
		expect(srv.client.batchGet.mock.calls.map((c) => c[1])).toEqual([[ID(1)]]);
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

	it("locally edited file is kept and exits 2", async () => {
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
		expect(read("A.md")).toBe("my local edit\n");
		expect(lines.join("\n")).toContain(
			"kept A.md: edited locally; use --force to overwrite",
		);
		// The next pull still knows the doc is behind, and --force overwrites.
		srv.feed([]);
		expect(await pull(srv)).toBe(2);
		srv.feed([]);
		expect(await pull(srv, true)).toBe(0);
		expect(read("A.md")).toContain("one v2");
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
});
