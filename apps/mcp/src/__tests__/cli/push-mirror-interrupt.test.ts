import { createHash } from "node:crypto";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { describe, expect, it, vi } from "vitest";
import { runPull } from "../../cli/pull.js";
import { runPush } from "../../cli/push.js";
import { blocknoteToMarkdown } from "../../utils/index.js";
import { docId, fakeDocsServer, storedBlocks } from "./fake-docs-server.js";

// A push that stops for good at its n-th file write (the base copy, the
// state, then the file when it is normalised), as if the process were killed there: no error, no cleanup.
const stop = vi.hoisted(() => ({
	armed: false,
	left: 0,
	reached: null as null | (() => void),
}));
vi.mock("../../cli/state.js", async (importOriginal) => {
	const orig = await importOriginal<typeof import("../../cli/state.js")>();
	const gate =
		<A extends unknown[]>(fn: (...a: A) => Promise<void>) =>
		(...a: A) => {
			if (stop.armed && stop.left-- <= 0) {
				stop.reached?.();
				return new Promise<void>(() => {});
			}
			return fn(...a);
		};
	return {
		...orig,
		writeFileAtomic: gate(orig.writeFileAtomic),
		writeJSON: gate(orig.writeJSON),
	};
});

const ID = docId(1);
const sha = (x: string) => createHash("sha256").update(x).digest("hex");

describe("a mirror push killed after each write", () => {
	const run = async (n: number, change = (md: string) => md) => {
		const root = fs.realpathSync(
			fs.mkdtempSync(path.join(os.tmpdir(), "paca-pk-")),
		);
		const mirror = path.join(root, "m");
		try {
			const server = fakeDocsServer();
			server.add(
				ID,
				"Plan",
				storedBlocks("# Plan\n\nRed one.\n\nPlain one.\n\nLast.", (b) => {
					b[1].props.textColor = "red";
				}),
			);
			const pull = (out: string[] = []) =>
				runPull({ projectId: "p1", dir: mirror, force: false }, server.client, {
					out: (l) => out.push(l),
					err: (l) => out.push(l),
				});
			const push = (out: string[] = []) =>
				runPush(
					{
						projectId: "p1",
						file: "m/Plan.md",
						lock: false,
						force: false,
						dryRun: false,
						client: "paca-cli",
					},
					server.client as any,
					{ out: (l) => out.push(l), err: (l) => out.push(l) },
					{ cwd: root },
				);
			expect(await pull()).toBe(0);
			const file = path.join(mirror, "Plan.md");
			const edited = fs
				.readFileSync(file, "utf8")
				.replace("Plain one.", "Plain one, edited.");
			fs.writeFileSync(file, change(edited));

			const hit = new Promise<"killed">((r) => {
				stop.reached = () => r("killed");
			});
			stop.left = n;
			stop.armed = true;
			const first = await Promise.race([
				push().then(() => "done" as const),
				hit,
			]);
			stop.armed = false;
			if (first === "done") return null;
			expect(server.client.patchDocument).toHaveBeenCalledTimes(1);
			fs.rmSync(path.join(mirror, ".paca-pull.lock"), { force: true });

			// The restart finds the server already holds the file.
			const out: string[] = [];
			const code = await push(out);
			const state = JSON.parse(
				fs.readFileSync(path.join(mirror, ".paca-sync.json"), "utf8"),
			).docs[ID];
			const base = fs.readFileSync(
				path.join(mirror, ".paca-base", `${ID}.md`),
				"utf8",
			);
			const pulled: string[] = [];
			const pullCode = await pull(pulled);
			return {
				code,
				out,
				state,
				base,
				pullCode,
				pulled,
				file: fs.readFileSync(file, "utf8"),
				edited,
				server,
			};
		} finally {
			fs.rmSync(root, { recursive: true, force: true });
		}
	};

	it("records the write on the next push without sending it again", async () => {
		let kills = 0;
		for (let n = 0; n < 5; n++) {
			const r = await run(n);
			if (r === null) break;
			kills++;
			expect({ n, code: r.code, out: r.out }).toEqual({
				n,
				code: 0,
				out: ["unchanged"],
			});
			expect(r.server.client.patchDocument).toHaveBeenCalledTimes(1);
			const doc = r.server.docs.get(ID);
			expect(r.state.content_hash).toBe(doc?.hash);
			expect(r.state.file_sha256).toBe(sha(r.edited));
			expect(r.base).toBe(r.edited);
			expect(blocknoteToMarkdown(doc?.content as any[])).toBe(r.edited);
			expect(doc?.content[1].props.textColor).toBe("red");
			expect({ n, pull: r.pullCode, said: r.pulled }).toEqual({
				n,
				pull: 0,
				said: ["up to date"],
			});
			expect(r.file).toBe(r.edited);
		}
		// Stopped before the base copy, and between it and the state.
		expect(kills).toBe(2);
	});

	it("a normalising push records the write and rewrites the file on the next push", async () => {
		// Whitespace at a line's end and a CRLF save: the rules allow both.
		const messy = (md: string) =>
			md.replace("Last.", "Last.   ").replace(/\n/g, "\r\n");
		let kills = 0;
		for (let n = 0; n < 6; n++) {
			const r = await run(n, messy);
			if (r === null) break;
			kills++;
			expect({ n, code: r.code, out: r.out }).toEqual({
				n,
				code: 0,
				out: ["unchanged", "normalised m/Plan.md"],
			});
			expect(r.server.client.patchDocument).toHaveBeenCalledTimes(1);
			const doc = r.server.docs.get(ID);
			expect(blocknoteToMarkdown(doc?.content as any[])).toBe(r.edited);
			expect(r.state.content_hash).toBe(doc?.hash);
			expect(r.state.file_sha256).toBe(sha(r.edited));
			expect(r.base).toBe(r.edited);
			expect(r.file).toBe(r.edited);
			expect({ n, pull: r.pullCode, said: r.pulled }).toEqual({
				n,
				pull: 0,
				said: ["up to date"],
			});
		}
		// Stopped before the base copy, the state, and the file.
		expect(kills).toBe(3);
	});
});
