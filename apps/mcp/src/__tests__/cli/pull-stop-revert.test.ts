import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { describe, expect, it, vi } from "vitest";
import { runPull } from "../../cli/pull.js";
import { runPush } from "../../cli/push.js";
import { blocknoteToMarkdown } from "../../utils/index.js";
import { docId, fakeDocsServer, storedBlocks } from "./fake-docs-server.js";

// A pull that stops at its n-th file write or rename: killed there (no
// error, no cleanup) or failing there (an error, as a full disk gives).
const stop = vi.hoisted(() => ({
	armed: false,
	left: 0,
	mode: "kill" as "kill" | "throw",
	reached: null as null | (() => void),
}));
vi.mock("../../cli/state.js", async (importOriginal) => {
	const orig = await importOriginal<typeof import("../../cli/state.js")>();
	const gate =
		<A extends [string, ...unknown[]]>(fn: (...a: A) => Promise<void>) =>
		(...a: A) => {
			if (stop.armed && stop.left-- <= 0) {
				stop.armed = false;
				if (stop.mode === "throw")
					return Promise.reject(new Error("ENOSPC: no space left on device"));
				stop.reached?.();
				return new Promise<void>(() => {});
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
const DOC = "# Plan\n\nFirst.\n\nSecond.\n\nThird.\n\nLast.";

async function setup() {
	const root = fs.realpathSync(
		fs.mkdtempSync(path.join(os.tmpdir(), "paca-sr-")),
	);
	const mirror = path.join(root, "m");
	const server = fakeDocsServer();
	server.add(ID, "Plan", storedBlocks(DOC));
	const io = { out: () => {}, err: () => {} };
	const pull = () =>
		runPull({ projectId: "p1", dir: mirror, force: false }, server.client, io);
	const push = () =>
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
			io,
			{ cwd: root },
		);
	const file = path.join(mirror, "Plan.md");
	expect(await pull()).toBe(0);
	return {
		server,
		pull,
		push,
		mirror,
		read: () => fs.readFileSync(file, "utf8"),
		write: (t: string) => fs.writeFileSync(file, t),
		render: () =>
			blocknoteToMarkdown(server.docs.get(ID)?.content as any[]),
		cleanup: () => fs.rmSync(root, { recursive: true, force: true }),
	};
}

/** Runs pull, stopping it at its n-th write: "done" when it got through. */
async function stopped(
	h: Awaited<ReturnType<typeof setup>>,
	n: number,
	mode: "kill" | "throw",
): Promise<"done" | "stopped"> {
	const hit = new Promise<"stopped">((r) => {
		stop.reached = () => r("stopped");
	});
	stop.mode = mode;
	stop.left = n;
	stop.armed = true;
	const first = await Promise.race([
		h.pull().then(
			() => "done" as const,
			() => "stopped" as const,
		),
		hit,
	]);
	const done = stop.armed;
	stop.armed = false;
	fs.rmSync(path.join(h.mirror, ".paca-pull.lock"), { force: true });
	return done ? "done" : first;
}

// The invariant, from outside: whatever stops a pull, and whatever the user
// does after, when Paca goes back to the earlier version, Paca never ends up
// holding the change that was taken back, and the user's edits are never
// lost (they are in Paca or in the file).
describe("a stopped pull, then Paca goes back to the earlier version", () => {
	for (const kind of ["plain", "merge"] as const)
		for (const mode of ["kill", "throw"] as const)
			for (const edit of [false, true])
				it(`${kind} pull, ${mode} at every write, ${edit ? "an edit after" : "no edit"}`, async () => {
					for (let n = 0; n < 12; n++) {
						const h = await setup();
						try {
							if (kind === "merge")
								h.write(h.read().replace("Last.", "Last, mine."));
							h.server.edit(ID, (b) => {
								b[1].content[0].text = "First, by Ann.";
							});
							const first = await stopped(h, n, mode);
							if (edit) h.write(h.read().replace("Third.", "Third, mine."));
							h.server.edit(ID, (b) => {
								b[1].content[0].text = "First.";
							});
							await h.pull();
							await h.push();
							// And once more, as a user who saw nothing wrong would.
							await h.pull();
							await h.push();
							const where = `${kind} ${mode} n=${n} edit=${edit}\nPaca:\n${h.render()}\nfile:\n${h.read()}`;
							expect(h.render(), where).not.toContain("First, by Ann.");
							for (const mine of [
								...(kind === "merge" ? ["Last, mine."] : []),
								...(edit ? ["Third, mine."] : []),
							])
								expect(`${h.render()}\n${h.read()}`, where).toContain(mine);
							if (first === "done") break;
						} finally {
							h.cleanup();
						}
					}
				});
});
