import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { describe, expect, it, vi } from "vitest";
import { runPull } from "../../cli/pull.js";
import { markdownToBlocknote } from "../../utils/index.js";

// The n-th rename never returns, as if the process were killed there. Every
// file a pull writes is renamed into place (atomic writes), so this stops a
// pull at each move, temporary move and file write in turn.
const kill = vi.hoisted(() => ({
	left: -1,
	hit: null as null | (() => void),
}));
vi.mock("node:fs/promises", async (importOriginal) => {
	const orig = await importOriginal<typeof import("node:fs/promises")>();
	const rename = async (from: string, to: string) => {
		if (kill.left >= 0 && kill.left-- === 0) {
			kill.hit?.();
			return new Promise<void>(() => {});
		}
		return orig.rename(from, to);
	};
	return { ...orig, rename, default: { ...orig.default, rename } };
});

const ID = (n: number) =>
	`${String(n).repeat(8)}-aaaa-4bbb-8ccc-00000000000${n}`;
const F1 = "ffffffff-aaaa-4bbb-8ccc-00000000000f";

interface ServerDoc {
	id: string;
	title: string;
	folder_id: string | null;
	text: string;
}

function fakeServer(
	docs: Map<string, ServerDoc>,
	deleted: Set<string>,
	folders: Array<{ id: string; parent_id: string | null; name: string }>,
) {
	const hash = (d: ServerDoc) => `h:${d.text}`;
	return {
		getChanges: vi.fn(async (_p: string, since?: string) => ({
			status: "ok" as const,
			cursor: `c${Date.now()}`,
			reset: !since,
			docs: [
				...[...docs.values()].map((d) => ({
					id: d.id,
					folder_id: d.folder_id,
					title: d.title,
					content_hash: hash(d),
					updated_at: "t",
					deleted: false,
				})),
				...[...deleted].map((id) => ({
					id,
					folder_id: null,
					title: "",
					content_hash: null,
					updated_at: "t",
					deleted: true,
				})),
			],
			folders: [...folders],
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
								content_hash: hash(d),
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

function mirrorFiles(root: string): string[] {
	const out: string[] = [];
	const walk = (d: string, rel: string) => {
		for (const e of fs.readdirSync(d, { withFileTypes: true })) {
			const r = rel ? `${rel}/${e.name}` : e.name;
			if (!rel && e.name.startsWith(".paca-")) continue;
			if (e.isDirectory()) walk(path.join(d, e.name), r);
			else out.push(r);
		}
	};
	walk(root, "");
	return out.sort();
}

describe("a pull killed at any rename", () => {
	const io = { out: () => {}, err: () => {} };

	/** Runs the scenario with a kill at rename `n` and a restart. Returns
	 *  null when the pull finished before reaching that rename. */
	const run = async (n: number) => {
		const dir = path.join(
			fs.mkdtempSync(path.join(os.tmpdir(), "paca-k-")),
			"m",
		);
		try {
			const docs = new Map<string, ServerDoc>([
				[ID(1), { id: ID(1), title: "A", folder_id: null, text: "one" }],
				[ID(2), { id: ID(2), title: "B", folder_id: null, text: "two" }],
				[ID(3), { id: ID(3), title: "C", folder_id: F1, text: "three" }],
				[ID(4), { id: ID(4), title: "D", folder_id: null, text: "four" }],
				[ID(5), { id: ID(5), title: "E", folder_id: null, text: "five" }],
			]);
			const deleted = new Set<string>();
			const folders = [{ id: F1, parent_id: null, name: "F" }];
			const client = fakeServer(docs, deleted, folders);
			const pull = () =>
				runPull({ projectId: "p1", dir, force: false }, client, io);
			kill.left = -1;
			expect(await pull()).toBe(0);

			// A and B swap names (A edited locally, B changed in Paca), D takes
			// the name of deleted E, C's folder is renamed, and N is new.
			fs.appendFileSync(path.join(dir, "A.md"), "local\n");
			docs.set(ID(1), { ...(docs.get(ID(1)) as ServerDoc), title: "B" });
			docs.set(ID(2), {
				...(docs.get(ID(2)) as ServerDoc),
				title: "A",
				text: "two v2",
			});
			docs.set(ID(4), { ...(docs.get(ID(4)) as ServerDoc), title: "E" });
			docs.delete(ID(5));
			deleted.add(ID(5));
			folders[0] = { id: F1, parent_id: null, name: "G" };
			docs.set(ID(6), { id: ID(6), title: "N", folder_id: null, text: "new" });

			const hit = new Promise<"killed">((r) => {
				kill.hit = () => r("killed");
			});
			kill.left = n;
			const first = await Promise.race([
				pull().then(() => "done" as const),
				hit,
			]);
			kill.left = -1;
			if (first === "done") return null;
			fs.rmSync(path.join(dir, ".paca-pull.lock"), { force: true });
			const code = await pull();
			const state = JSON.parse(
				fs.readFileSync(path.join(dir, ".paca-sync.json"), "utf8"),
			);
			const files = mirrorFiles(dir);
			const read = (f: string) =>
				fs.readFileSync(path.join(dir, f), "utf8").trim();
			return {
				code,
				files,
				folders: fs
					.readdirSync(dir, { withFileTypes: true })
					.filter((e) => e.isDirectory() && !e.name.startsWith("."))
					.map((e) => e.name)
					.sort(),
				recorded: Object.values(state.docs as Record<string, { file: string }>)
					.map((d) => d.file)
					.sort(),
				content: Object.fromEntries(files.map((f) => [f, read(f)])),
			};
		} finally {
			fs.rmSync(path.dirname(dir), { recursive: true, force: true });
		}
	};

	it("ends after a restart with the right files, the local edit kept and no duplicates", async () => {
		let kills = 0;
		for (let n = 0; n < 60; n++) {
			const r = await run(n);
			if (r === null) break;
			kills++;
			const want = ["A.md", "B.md", "E.md", "G/C.md", "N.md"];
			expect({ n, files: r.files }).toEqual({ n, files: want });
			expect({ n, recorded: r.recorded }).toEqual({ n, recorded: want });
			// The renamed folder's old name is gone, not left empty.
			expect({ n, folders: r.folders }).toEqual({ n, folders: ["G"] });
			expect(r.content["B.md"]).toContain("local");
			expect(r.content["A.md"]).toBe("two v2");
			expect(r.content["E.md"]).toBe("four");
			expect(r.content["N.md"]).toBe("new");
			// A did not change in Paca, so its edited file is left alone.
			expect({ n, code: r.code }).toEqual({ n, code: 0 });
		}
		// Every kill point was reached: the scenario has 14 renames.
		expect(kills).toBeGreaterThan(10);
	});
});
