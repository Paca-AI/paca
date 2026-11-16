import { describe, expect, it, vi } from "vitest";
import { EXIT, UnreachableError } from "../../cli/state.js";
import {
	DOC_MIRROR_TOOL_NAMES,
	getDocMirrorTools,
	handleDocMirrorTool,
} from "../../tools/doc-mirror-tools.js";

const client = {} as never;

describe("getDocMirrorTools", () => {
	it("lists pull_docs and push_docs with their required arguments", () => {
		const tools = getDocMirrorTools();
		expect(tools.map((t) => t.name)).toEqual(["pull_docs", "push_docs"]);
		expect(tools[0]?.inputSchema.required).toEqual(["projectId"]);
		expect(tools[1]?.inputSchema.required).toEqual(["projectId", "file"]);
		expect(DOC_MIRROR_TOOL_NAMES.has("push_docs")).toBe(true);
	});
});

describe("handleDocMirrorTool pull_docs", () => {
	it("pulls with the default folder and returns the command's output", async () => {
		const pull = vi.fn(async (_o, _c, io) => {
			io.out("2 documents written");
			return EXIT.ok;
		});
		const res = await handleDocMirrorTool(
			"pull_docs",
			{ projectId: "p1" },
			client,
			{ pull },
		);
		expect(pull).toHaveBeenCalledWith(
			{ projectId: "p1", dir: ".paca/docs", force: false },
			client,
			expect.anything(),
		);
		expect(res.isError).toBe(false);
		expect(res.content[0].text).toContain("Pulled documents to .paca/docs");
		expect(res.content[0].text).toContain("2 documents written");
	});

	it("passes dir and force through", async () => {
		const pull = vi.fn(async () => EXIT.ok);
		await handleDocMirrorTool(
			"pull_docs",
			{ projectId: "p1", dir: "docs-mirror", force: true },
			client,
			{ pull },
		);
		expect(pull).toHaveBeenCalledWith(
			{ projectId: "p1", dir: "docs-mirror", force: true },
			client,
			expect.anything(),
		);
	});

	it("reports a conflict without marking the call as an error", async () => {
		const res = await handleDocMirrorTool(
			"pull_docs",
			{ projectId: "p1" },
			client,
			{ pull: async () => EXIT.conflict },
		);
		expect(res.isError).toBe(false);
		expect(res.content[0].text).toContain("need attention");
	});

	it("marks a failed pull as an error and keeps its messages", async () => {
		const res = await handleDocMirrorTool(
			"pull_docs",
			{ projectId: "p1" },
			client,
			{
				pull: async (_o, _c, io) => {
					io.err("refusing to pull: x is a symlink");
					return EXIT.error;
				},
			},
		);
		expect(res.isError).toBe(true);
		expect(res.content[0].text).toContain("refusing to pull");
	});

	it("reports an unreachable server as an error", async () => {
		const res = await handleDocMirrorTool(
			"pull_docs",
			{ projectId: "p1" },
			client,
			{
				pull: async () => {
					throw new UnreachableError(new TypeError("fetch failed"));
				},
			},
		);
		expect(res.isError).toBe(true);
		expect(res.content[0].text).toContain("Could not reach the Paca server");
	});

	it("turns an unexpected failure into an error result", async () => {
		const res = await handleDocMirrorTool(
			"pull_docs",
			{ projectId: "p1" },
			client,
			{
				pull: async () => {
					throw new Error("disk full");
				},
			},
		);
		expect(res.isError).toBe(true);
		expect(res.content[0].text).toContain("disk full");
	});
});

describe("handleDocMirrorTool push_docs", () => {
	it("pushes a file with defaults and returns the output", async () => {
		const push = vi.fn(async (_o, _c, io) => {
			io.out("updated Plans/a");
			return EXIT.ok;
		});
		const res = await handleDocMirrorTool(
			"push_docs",
			{
				projectId: "p1",
				file: "a.md",
				path: "Plans/a",
				note: "n",
				task: "PACA-1",
			},
			client,
			{ push },
		);
		expect(push).toHaveBeenCalledWith(
			expect.objectContaining({
				projectId: "p1",
				file: "a.md",
				path: "Plans/a",
				note: "n",
				task: "PACA-1",
				lock: false,
				force: false,
				dryRun: false,
			}),
			client,
			expect.anything(),
		);
		expect(res.isError).toBe(false);
		expect(res.content[0].text).toContain("Pushed a.md");
		expect(res.content[0].text).toContain("updated Plans/a");
	});

	it("says a dry run wrote nothing", async () => {
		const res = await handleDocMirrorTool(
			"push_docs",
			{ projectId: "p1", file: "a.md", dryRun: true },
			client,
			{ push: async () => EXIT.ok },
		);
		expect(res.content[0].text).toContain("nothing was written");
	});

	it("tells the caller to pull on a conflict, without an error flag", async () => {
		const res = await handleDocMirrorTool(
			"push_docs",
			{ projectId: "p1", file: "a.md" },
			client,
			{ push: async () => EXIT.conflict },
		);
		expect(res.isError).toBe(false);
		expect(res.content[0].text).toContain("pull_docs");
	});

	it("marks a failed push as an error", async () => {
		const res = await handleDocMirrorTool(
			"push_docs",
			{ projectId: "p1", file: "a.md" },
			client,
			{
				push: async (_o, _c, io) => {
					io.err("no such file");
					return EXIT.error;
				},
			},
		);
		expect(res.isError).toBe(true);
		expect(res.content[0].text).toContain("no such file");
	});

	it("rejects a call without a file", async () => {
		await expect(
			handleDocMirrorTool("push_docs", { projectId: "p1" }, client),
		).rejects.toThrow();
	});
});
