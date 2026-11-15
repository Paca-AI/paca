import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { runPull } from "../../cli/pull.js";
import { markdownToBlocknote } from "../../utils/index.js";

// A pull that stops for good at its first content write, as if the process
// were killed there (Ctrl-C, out of memory): no error, no rollback.
const stop = vi.hoisted(() => ({
	armed: false,
	/** Content writes let through before the stop. */
	skip: 0,
	reached: null as null | (() => void),
}));
vi.mock("../../cli/state.js", async (importOriginal) => {
	const orig = await importOriginal<typeof import("../../cli/state.js")>();
	return {
		...orig,
		writeFileAtomic: (file: string, text: string) => {
			if (stop.armed && stop.skip-- <= 0) {
				stop.reached?.();
				return new Promise<void>(() => {});
			}
			return orig.writeFileAtomic(file, text);
		},
	};
});

const ID1 = "11111111-aaaa-4bbb-8ccc-000000000001";
const ID2 = "22222222-aaaa-4bbb-8ccc-000000000002";

interface ServerDoc {
	id: string;
	title: string;
	folder_id: string | null;
	text: string;
}

function fakeServer(docs: Map<string, ServerDoc>, deleted = new Set<string>()) {
	const entry = (d: ServerDoc) => ({
		id: d.id,
		folder_id: d.folder_id,
		title: d.title,
		content_hash: `h:${d.text}`,
		updated_at: "t",
		deleted: false,
	});
	return {
		/** Answers every cursor with every doc and a tombstone per deleted
		 *  id (a reset for the first pull). */
		getChanges: vi.fn(async (_p: string, since?: string) => ({
			status: "ok" as const,
			cursor: `c${Date.now()}`,
			reset: !since,
			docs: [
				...[...docs.values()].map(entry),
				...[...deleted].map((id) => ({
					id,
					folder_id: null,
					title: "",
					content_hash: null,
					updated_at: "t",
					deleted: true,
				})),
			],
			folders: [],
		})),
		batchGet: vi.fn(async (_p: string, ids: string[]) => ({
			status: "ok" as const,
			items: ids.flatMap((id) => {
				const d = docs.get(id);
				return d
					? [
							{
								id: d.id,
								title: d.title,
								folder_id: d.folder_id,
								content: markdownToBlocknote(d.text),
								content_hash: `h:${d.text}`,
								position: 0,
								created_at: "",
								updated_at: "",
								source: null,
								source_locked: false,
							},
						]
					: [];
			}),
		})),
	};
}

/** Every file under `root` except the pull's own bookkeeping. */
function mirrorFiles(root: string): string[] {
	const out: string[] = [];
	const walk = (d: string, rel: string) => {
		for (const e of fs.readdirSync(d, { withFileTypes: true })) {
			const r = rel ? `${rel}/${e.name}` : e.name;
			if (!rel && (e.name.startsWith(".paca-") || e.name === ".paca-base"))
				continue;
			if (e.isDirectory()) walk(path.join(d, e.name), r);
			else out.push(r);
		}
	};
	walk(root, "");
	return out.sort();
}

describe("runPull killed between moves and writes", () => {
	let dir: string;
	const io = { out: () => {}, err: () => {} };

	beforeEach(() => {
		dir = path.join(fs.mkdtempSync(path.join(os.tmpdir(), "paca-kill-")), "m");
		stop.armed = false;
		stop.skip = 0;
	});
	afterEach(() => {
		stop.armed = false;
		fs.rmSync(path.dirname(dir), { recursive: true, force: true });
	});

	it("the next pull leaves one file per document at its name", async () => {
		const docs = new Map<string, ServerDoc>([
			[ID1, { id: ID1, title: "A", folder_id: null, text: "one" }],
			[ID2, { id: ID2, title: "B", folder_id: null, text: "two" }],
		]);
		const client = fakeServer(docs);
		expect(
			await runPull({ projectId: "p1", dir, force: false }, client, io),
		).toBe(0);

		// Renamed and changed in Paca: the pull moves A.md to C.md, then
		// needs to write C.md.
		docs.set(ID1, { id: ID1, title: "C", folder_id: null, text: "one v2" });
		const reached = new Promise<void>((r) => {
			stop.reached = r;
		});
		stop.armed = true;
		void runPull({ projectId: "p1", dir, force: false }, client, io);
		await reached;
		expect(mirrorFiles(dir)).toEqual(["B.md", "C.md"]);

		// Restarted: the dead pull's lock is removed by hand (it names this
		// test's process, which is still running).
		stop.armed = false;
		fs.rmSync(path.join(dir, ".paca-pull.lock"));
		expect(
			await runPull({ projectId: "p1", dir, force: false }, client, io),
		).toBe(0);
		expect(mirrorFiles(dir)).toEqual(["B.md", "C.md"]);
		expect(fs.readFileSync(path.join(dir, "C.md"), "utf8")).toContain("one v2");
		const state = JSON.parse(
			fs.readFileSync(path.join(dir, ".paca-sync.json"), "utf8"),
		);
		expect(state.docs[ID1].file).toBe("C.md");
		expect(state.docs[ID2].file).toBe("B.md");
	});

	it("a file still under its temporary name goes to its new name", async () => {
		const docs = new Map<string, ServerDoc>([
			[ID1, { id: ID1, title: "A", folder_id: null, text: "one" }],
		]);
		const client = fakeServer(docs);
		await runPull({ projectId: "p1", dir, force: false }, client, io);
		docs.set(ID1, { id: ID1, title: "C", folder_id: null, text: "one v2" });
		const reached = new Promise<void>((r) => {
			stop.reached = r;
		});
		stop.armed = true;
		void runPull({ projectId: "p1", dir, force: false }, client, io);
		await reached;
		stop.armed = false;
		// What a kill one rename earlier leaves: the new names are recorded,
		// the file is still under its temporary name.
		fs.renameSync(
			path.join(dir, "C.md"),
			path.join(dir, `.paca-tmp-move-${ID1}`),
		);
		fs.rmSync(path.join(dir, ".paca-pull.lock"));
		expect(
			await runPull({ projectId: "p1", dir, force: false }, client, io),
		).toBe(0);
		expect(mirrorFiles(dir)).toEqual(["C.md"]);
		expect(fs.readFileSync(path.join(dir, "C.md"), "utf8")).toContain("one v2");
	});
	it("a document removed in the same pull is not left in the state", async () => {
		const docs = new Map<string, ServerDoc>([
			[ID1, { id: ID1, title: "A", folder_id: null, text: "one" }],
			[ID2, { id: ID2, title: "B", folder_id: null, text: "two" }],
		]);
		const deleted = new Set<string>();
		const client = fakeServer(docs, deleted);
		await runPull({ projectId: "p1", dir, force: false }, client, io);
		// A is deleted and B takes its name.
		docs.delete(ID1);
		deleted.add(ID1);
		docs.set(ID2, { id: ID2, title: "A", folder_id: null, text: "two v2" });
		const reached = new Promise<void>((r) => {
			stop.reached = r;
		});
		stop.armed = true;
		void runPull({ projectId: "p1", dir, force: false }, client, io);
		await reached;
		stop.armed = false;
		fs.rmSync(path.join(dir, ".paca-pull.lock"));
		expect(
			await runPull({ projectId: "p1", dir, force: false }, client, io),
		).toBe(0);
		expect(mirrorFiles(dir)).toEqual(["A.md"]);
		expect(fs.readFileSync(path.join(dir, "A.md"), "utf8")).toContain("two v2");
	});
	const state = () =>
		JSON.parse(fs.readFileSync(path.join(dir, ".paca-sync.json"), "utf8"));
	const files = () =>
		Object.fromEntries(
			Object.entries(state().docs).map(([id, d]) => [
				id,
				(d as { file: string }).file,
			]),
		);
	const killAtWrite = async (
		client: ReturnType<typeof fakeServer>,
		skip?: number,
	) => {
		if (skip !== undefined) stop.skip = skip;
		const reached = new Promise<void>((r) => {
			stop.reached = r;
		});
		stop.armed = true;
		const run = runPull({ projectId: "p1", dir, force: false }, client, io);
		// A pull that writes nothing finishes instead: the test says which.
		const how = await Promise.race([
			reached.then(() => "killed" as const),
			run.then((code) => code),
		]);
		stop.armed = false;
		fs.rmSync(path.join(dir, ".paca-pull.lock"), { force: true });
		return how;
	};
	const pull = (client: ReturnType<typeof fakeServer>) =>
		runPull({ projectId: "p1", dir, force: false }, client, io);

	it("a title swap with one file deleted locally never gives one file to two documents", async () => {
		const docs = new Map<string, ServerDoc>([
			[ID1, { id: ID1, title: "A", folder_id: null, text: "one" }],
			[ID2, { id: ID2, title: "B", folder_id: null, text: "two" }],
		]);
		const client = fakeServer(docs);
		await pull(client);
		fs.rmSync(path.join(dir, "B.md"));
		docs.set(ID1, { id: ID1, title: "B", folder_id: null, text: "one" });
		docs.set(ID2, { id: ID2, title: "A", folder_id: null, text: "two" });
		await killAtWrite(client);
		const recorded = Object.values(files());
		expect(new Set(recorded).size).toBe(recorded.length);
		expect(await pull(client)).toBe(0);
		expect(await pull(client)).toBe(0);
		expect(mirrorFiles(dir)).toEqual(["A.md", "B.md"]);
		expect(fs.readFileSync(path.join(dir, "A.md"), "utf8")).toContain("two");
		expect(fs.readFileSync(path.join(dir, "B.md"), "utf8")).toContain("one");
		expect(files()).toEqual({ [ID1]: "B.md", [ID2]: "A.md" });
	});
	it("a new document written before the kill is not duplicated", async () => {
		const docs = new Map<string, ServerDoc>([
			[ID1, { id: ID1, title: "A", folder_id: null, text: "one" }],
		]);
		const client = fakeServer(docs);
		await pull(client);
		docs.set(ID2, { id: ID2, title: "N", folder_id: null, text: "new" });
		stop.skip = 1; // N.md is written; its base copy write never returns
		await killAtWrite(client);
		expect(mirrorFiles(dir)).toEqual(["A.md", "N.md"]);
		expect(await pull(client)).toBe(0);
		expect(mirrorFiles(dir)).toEqual(["A.md", "N.md"]);
		expect(files()).toEqual({ [ID1]: "A.md", [ID2]: "N.md" });
		expect(state().docs[ID2].content_hash).toBe("h:new");
	});

	it("a first pull killed part way is not duplicated", async () => {
		const docs = new Map<string, ServerDoc>([
			[ID1, { id: ID1, title: "A", folder_id: null, text: "one" }],
			[ID2, { id: ID2, title: "B", folder_id: null, text: "two" }],
		]);
		const client = fakeServer(docs);
		stop.skip = 2; // A.md and its base copy are written
		await killAtWrite(client);
		expect(mirrorFiles(dir)).toEqual(["A.md"]);
		expect(await pull(client)).toBe(0);
		expect(mirrorFiles(dir)).toEqual(["A.md", "B.md"]);
		expect(files()).toEqual({ [ID1]: "A.md", [ID2]: "B.md" });
	});

	it("a new document deleted after the kill has its file removed", async () => {
		const docs = new Map<string, ServerDoc>([
			[ID1, { id: ID1, title: "A", folder_id: null, text: "one" }],
		]);
		const deleted = new Set<string>();
		const client = fakeServer(docs, deleted);
		await pull(client);
		docs.set(ID2, { id: ID2, title: "N", folder_id: null, text: "new" });
		stop.skip = 1;
		await killAtWrite(client);
		docs.delete(ID2);
		deleted.add(ID2);
		expect(await pull(client)).toBe(0);
		expect(mirrorFiles(dir)).toEqual(["A.md"]);
		expect(files()).toEqual({ [ID1]: "A.md" });
	});
	it("a kept file whose document comes back is safe when a pull is killed and the new document deleted", async () => {
		const docs = new Map<string, ServerDoc>([
			[ID1, { id: ID1, title: "X", folder_id: null, text: "o" }],
		]);
		const deleted = new Set<string>();
		const client = fakeServer(docs, deleted);
		await pull(client);
		fs.writeFileSync(path.join(dir, "X.md"), "MY EDIT\n");
		docs.delete(ID1);
		deleted.add(ID1);
		expect(await pull(client)).toBe(2);
		deleted.delete(ID1);
		docs.set(ID1, { id: ID1, title: "Y", folder_id: null, text: "o" });
		docs.set(ID2, { id: ID2, title: "X", folder_id: null, text: "new doc" });
		await killAtWrite(client);
		docs.delete(ID2);
		deleted.add(ID2);
		expect(await pull(client)).toBe(2);
		expect(mirrorFiles(dir)).toEqual(["Y.md"]);
		expect(fs.readFileSync(path.join(dir, "Y.md"), "utf8")).toBe("MY EDIT\n");
	});
	it.each([
		["in place", false],
		["into a file deleted locally", true],
	])("a file written %s before the kill is not taken for a local edit later", async (_how, deleteFirst) => {
		const docs = new Map<string, ServerDoc>([
			[ID1, { id: ID1, title: "A", folder_id: null, text: "v1" }],
		]);
		const client = fakeServer(docs);
		await pull(client);
		if (deleteFirst) fs.rmSync(path.join(dir, "A.md"));
		docs.set(ID1, { id: ID1, title: "A", folder_id: null, text: "v2" });
		await killAtWrite(client, 1); // A.md is written; its base copy is not
		expect(fs.readFileSync(path.join(dir, "A.md"), "utf8")).toContain("v2");
		docs.set(ID1, { id: ID1, title: "A", folder_id: null, text: "v3" });
		expect(await pull(client)).toBe(0);
		expect(fs.readFileSync(path.join(dir, "A.md"), "utf8")).toContain("v3");
	});
	describe("pull's own write after several interrupted pulls", () => {
		const setup = async () => {
			const docs = new Map<string, ServerDoc>([
				[ID1, { id: ID1, title: "X", folder_id: null, text: "v1" }],
			]);
			const deleted = new Set<string>();
			const client = fakeServer(docs, deleted);
			await pull(client);
			const set = (text: string) =>
				docs.set(ID1, { id: ID1, title: "X", folder_id: null, text });
			const read = () => fs.readFileSync(path.join(dir, "X.md"), "utf8");
			return { docs, deleted, client, set, read };
		};

		it("is recognised after a kill once written and once before writing", async () => {
			const t = await setup();
			t.set("v2");
			await killAtWrite(t.client, 1); // X.md holds v2
			t.set("v3");
			await killAtWrite(t.client, 0); // stopped right after the interim state
			expect(t.read()).toContain("v2");
			expect(await pull(t.client)).toBe(0);
			expect(t.read()).toContain("v3");
		});

		it("is recognised after three interruptions in a row", async () => {
			const t = await setup();
			t.set("v2");
			expect(await killAtWrite(t.client, 1)).toBe("killed");
			t.set("v3");
			expect(await killAtWrite(t.client, 1)).toBe("killed");
			t.set("v4");
			expect(await killAtWrite(t.client, 0)).toBe("killed");
			t.set("v5");
			expect(await killAtWrite(t.client, 0)).toBe("killed");
			expect(await pull(t.client)).toBe(0);
			expect(t.read()).toContain("v5");
		});

		it("a user edit after an interrupted pull is still kept", async () => {
			const t = await setup();
			t.set("v2");
			await killAtWrite(t.client, 1);
			fs.writeFileSync(path.join(dir, "X.md"), "MY EDIT\n");
			t.set("v3");
			expect(await killAtWrite(t.client, 0)).toBe(2); // kept, nothing written
			expect(await pull(t.client)).toBe(2);
			expect(t.read()).toBe("MY EDIT\n");
		});

		it("is removed, not kept, when the document is then deleted", async () => {
			const t = await setup();
			t.set("v2");
			await killAtWrite(t.client, 1);
			t.set("v3");
			await killAtWrite(t.client, 0);
			t.docs.delete(ID1);
			t.deleted.add(ID1);
			expect(await pull(t.client)).toBe(0);
			expect(mirrorFiles(dir)).toEqual([]);
		});
	});
});
