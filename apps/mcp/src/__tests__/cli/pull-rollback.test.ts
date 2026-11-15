import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { runPull } from "../../cli/pull.js";
import { markdownToBlocknote } from "../../utils/index.js";

// Renames that fail, as on Windows when another program holds the file open.
const ren = vi.hoisted(() => ({
	fail: null as null | ((from: string, to: string) => boolean),
}));
vi.mock("node:fs/promises", async (importOriginal) => {
	const orig = await importOriginal<typeof import("node:fs/promises")>();
	const rename = async (from: string, to: string) => {
		if (ren.fail?.(String(from), String(to))) {
			const err = new Error("resource busy") as NodeJS.ErrnoException;
			err.code = "EBUSY";
			throw err;
		}
		return orig.rename(from, to);
	};
	return { ...orig, rename, default: { ...orig.default, rename } };
});

const ID1 = "11111111-aaaa-4bbb-8ccc-000000000001";
const ID2 = "22222222-aaaa-4bbb-8ccc-000000000002";

interface ServerDoc {
	id: string;
	title: string;
	text: string;
}

function fakeServer(docs: Map<string, ServerDoc>) {
	const hash = (d: ServerDoc) => `h:${d.text}`;
	return {
		getChanges: vi.fn(async (_p: string, since?: string) => ({
			status: "ok" as const,
			cursor: `c${Date.now()}`,
			reset: !since,
			docs: [...docs.values()].map((d) => ({
				id: d.id,
				folder_id: null,
				title: d.title,
				content_hash: hash(d),
				updated_at: "t",
				deleted: false,
			})),
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
								folder_id: null,
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

describe("runPull rollback with a rename that fails", () => {
	let dir: string;
	const io = { out: () => {}, err: () => {} };
	const pull = (client: ReturnType<typeof fakeServer>) =>
		runPull({ projectId: "p1", dir, force: false }, client, io);
	const mirror = () =>
		fs
			.readdirSync(dir)
			.filter((f) => !f.startsWith(".paca-"))
			.sort();

	beforeEach(() => {
		dir = path.join(fs.mkdtempSync(path.join(os.tmpdir(), "paca-rb-")), "m");
		ren.fail = null;
	});
	afterEach(() => {
		ren.fail = null;
		fs.rmSync(path.dirname(dir), { recursive: true, force: true });
	});

	it("keeps the state matching a file that could not be moved back", async () => {
		const docs = new Map<string, ServerDoc>([
			[ID1, { id: ID1, title: "a", text: "one" }],
			[ID2, { id: ID2, title: "c", text: "three" }],
		]);
		const client = fakeServer(docs);
		await pull(client);
		fs.writeFileSync(path.join(dir, "a.md"), "LOCAL\n");
		// a is renamed to b (and moved); c changes, and writing its base copy
		// fails, so the pull rolls back; moving b.md back fails too.
		docs.set(ID1, { id: ID1, title: "b", text: "one" });
		docs.set(ID2, { id: ID2, title: "c", text: "three v2" });
		fs.rmSync(path.join(dir, ".paca-base"), { recursive: true });
		fs.writeFileSync(path.join(dir, ".paca-base"), "");
		ren.fail = (from, to) =>
			path.basename(from) === "b.md" &&
			path.basename(to).startsWith(".paca-tmp-move-");
		await expect(pull(client)).rejects.toThrow();
		ren.fail = null;
		expect(mirror()).toEqual(["b.md", "c.md"]);
		const state = JSON.parse(
			fs.readFileSync(path.join(dir, ".paca-sync.json"), "utf8"),
		);
		expect(state.docs[ID1].file).toBe("b.md");

		fs.rmSync(path.join(dir, ".paca-base"));
		// a did not change in Paca, so its edited file is left alone.
		expect(await pull(client)).toBe(0);
		expect(mirror()).toEqual(["b.md", "c.md"]);
		expect(fs.readFileSync(path.join(dir, "b.md"), "utf8")).toBe("LOCAL\n");
		expect(fs.readFileSync(path.join(dir, "c.md"), "utf8")).toContain(
			"three v2",
		);
	});
});
