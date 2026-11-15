import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { describe, expect, it, vi } from "vitest";
import { runPull } from "../../cli/pull.js";
import { runPush } from "../../cli/push.js";
import { blocknoteToMarkdown } from "../../utils/index.js";
import { docId, fakeDocsServer, storedBlocks } from "./fake-docs-server.js";

// A merging pull that stops for good at its n-th file write (the state
// before moves when it changes, the merged file, the base copy, the
// state), as if the process were killed there: no error, no cleanup.
const stop = vi.hoisted(() => ({
	armed: false,
	left: 0,
	reached: null as null | (() => void),
	writes: [] as string[],
}));
vi.mock("../../cli/state.js", async (importOriginal) => {
	const orig = await importOriginal<typeof import("../../cli/state.js")>();
	const gate =
		<A extends [string, ...unknown[]]>(fn: (...a: A) => Promise<void>) =>
		(...a: A) => {
			if (stop.armed) {
				if (stop.left-- <= 0) {
					stop.reached?.();
					return new Promise<void>(() => {});
				}
				stop.writes.push(path.basename(a[0]));
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

describe("a merging pull killed after each write", () => {
	const run = async (n: number) => {
		const root = fs.realpathSync(
			fs.mkdtempSync(path.join(os.tmpdir(), "paca-mk-")),
		);
		const mirror = path.join(root, "m");
		try {
			const server = fakeDocsServer();
			server.add(
				ID,
				"Plan",
				storedBlocks("# Plan\n\nFirst.\n\nRed one.\n\nThird.\n\nLast.", (b) => {
					b[2].props.textColor = "red";
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
			fs.writeFileSync(
				file,
				fs.readFileSync(file, "utf8").replace("Third.", "Third, mine."),
			);
			server.edit(ID, (b) => {
				b[1].content[0].text = "First, by Ann.";
			});

			const hit = new Promise<"killed">((r) => {
				stop.reached = () => r("killed");
			});
			stop.left = n;
			stop.writes = [];
			stop.armed = true;
			const first = await Promise.race([
				pull().then(() => "done" as const),
				hit,
			]);
			stop.armed = false;
			if (first === "done") return null;
			const before = [...stop.writes];
			fs.rmSync(path.join(mirror, ".paca-pull.lock"), { force: true });

			const out: string[] = [];
			const code = await pull(out);
			const merged = fs.readFileSync(file, "utf8");
			const pushed: string[] = [];
			const pushCode = await push(pushed);
			return {
				before,
				code,
				out,
				merged,
				pushCode,
				pushed,
				stored: blocknoteToMarkdown(server.docs.get(ID)?.content as any[]),
				red: server.docs.get(ID)?.content[2].props.textColor,
			};
		} finally {
			fs.rmSync(root, { recursive: true, force: true });
		}
	};

	it("restarts to the same merge, and a push then keeps both changes", async () => {
		const want =
			"# Plan\n\nFirst, by Ann.\n\nRed one.\n\nThird, mine.\n\nLast.\n";
		const stops: string[][] = [];
		for (let n = 0; n < 8; n++) {
			const r = await run(n);
			if (r === null) break;
			stops.push(r.before);
			expect({ n, code: r.code, out: r.out }).toMatchObject({ n, code: 0 });
			expect(r.merged, `stopped after ${r.before.join(", ")}`).toBe(want);
			expect({ n, push: r.pushCode, said: r.pushed }).toEqual({
				n,
				push: 0,
				said: ["updated Plan from m/Plan.md (1 block changed)"],
			});
			expect(r.stored).toBe(want);
			expect(r.red).toBe("red");
		}
		// Stopped before the merged file, before the base copy, and before
		// the state.
		expect(stops.map((w) => w.length)).toEqual([0, 1, 2]);
		expect(stops[2][0]).toBe("Plan.md");
		expect(stops[2][1]).toBe(`${ID}.md`);
	});
});
