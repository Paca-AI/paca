import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { PacaAPIError } from "../../api/doc-client.js";
import { runCli } from "../../cli/index.js";
import { UnreachableError } from "../../cli/state.js";

function io() {
	const out: string[] = [];
	const err: string[] = [];
	return {
		out,
		err,
		io: { out: (l: string) => out.push(l), err: (l: string) => err.push(l) },
	};
}

const ENV = {
	PACA_API_KEY: "k",
	PACA_API_URL: "http://paca.test",
	PACA_PROJECT_ID: "p-env",
};

describe("runCli", () => {
	it("--help lists both commands", async () => {
		const o = io();
		expect(await runCli(["pull", "--help"], {}, o.io)).toBe(0);
		const text = o.out.join("\n");
		expect(text).toContain(
			"pull [--project <id>] [--dir .paca/docs] [--force]",
		);
		expect(text).toContain("push <file>");
		expect(text).toContain("Exit codes");
	});

	it("needs an API key and a project", async () => {
		let o = io();
		expect(await runCli(["pull"], { PACA_PROJECT_ID: "p" }, o.io)).toBe(1);
		expect(o.err.join("\n")).toContain("PACA_API_KEY");
		o = io();
		expect(await runCli(["pull"], { PACA_API_KEY: "k" }, o.io)).toBe(1);
		expect(o.err.join("\n")).toContain("--project");
	});

	it("passes pull options and the configured client", async () => {
		const pull = vi.fn().mockResolvedValue(0);
		const o = io();
		const code = await runCli(
			["pull", "--project", "p1", "--dir", "mirror", "--force"],
			ENV,
			o.io,
			{ pull },
		);
		expect(code).toBe(0);
		const [opts, client] = pull.mock.calls[0];
		expect(opts).toEqual({ projectId: "p1", dir: "mirror", force: true });
		expect(typeof client.getChanges).toBe("function");
	});

	it("pull defaults to PACA_PROJECT_ID and .paca/docs", async () => {
		const pull = vi.fn().mockResolvedValue(2);
		expect(await runCli(["pull"], ENV, io().io, { pull })).toBe(2);
		expect(pull.mock.calls[0][0]).toEqual({
			projectId: "p-env",
			dir: ".paca/docs",
			force: false,
		});
	});

	it("passes push options and the client label", async () => {
		const push = vi.fn().mockResolvedValue(0);
		await runCli(
			[
				"push",
				"docs/plan.md",
				"--path",
				"Plans/X",
				"--note",
				"why",
				"--task",
				"PACA-88",
				"--lock",
				"--force",
				"--dry-run",
			],
			{ ...ENV, PACA_CLIENT: "claude-code/s1" },
			io().io,
			{ push },
		);
		expect(push.mock.calls[0][0]).toEqual({
			projectId: "p-env",
			file: "docs/plan.md",
			path: "Plans/X",
			note: "why",
			task: "PACA-88",
			lock: true,
			force: true,
			dryRun: true,
			client: "claude-code/s1",
			server: "http://paca.test",
		});
	});

	it("push labels the client paca-cli by default and needs a file", async () => {
		const push = vi.fn().mockResolvedValue(0);
		await runCli(["push", "a.md"], ENV, io().io, { push });
		expect(push.mock.calls[0][0].client).toBe("paca-cli");
		const o = io();
		expect(await runCli(["push"], ENV, o.io, { push })).toBe(1);
		expect(o.err.join("\n")).toContain("push needs a file");
	});

	it("rejects an unknown option", async () => {
		const o = io();
		expect(await runCli(["pull", "--nope"], ENV, o.io)).toBe(1);
		expect(o.err.join("\n")).toContain("--nope");
	});

	it("maps an unreachable server to exit 3 and an API error to 1", async () => {
		let o = io();
		const unreachable = vi
			.fn()
			.mockRejectedValue(new UnreachableError(new Error("ECONNREFUSED")));
		expect(await runCli(["pull"], ENV, o.io, { pull: unreachable })).toBe(3);
		expect(o.err.join("\n")).toContain("cannot reach the Paca server");
		o = io();
		const apiErr = vi
			.fn()
			.mockRejectedValue(new PacaAPIError("Permission denied", 403));
		expect(await runCli(["pull"], ENV, o.io, { pull: apiErr })).toBe(1);
		expect(o.err.join("\n")).toContain("Permission denied");
	});
});

describe("index.ts dispatch", () => {
	const argv = process.argv;
	beforeEach(() => {
		vi.resetModules();
	});
	afterEach(() => {
		process.argv = argv;
		process.exitCode = undefined;
		vi.doUnmock("../../cli/index.js");
		vi.doUnmock("../../server.js");
		vi.restoreAllMocks();
	});

	it("runs the command and sets its exit code instead of starting the server", async () => {
		const runCliMock = vi.fn().mockResolvedValue(2);
		const createServer = vi.fn();
		vi.doMock("../../cli/index.js", () => ({ runCli: runCliMock }));
		vi.doMock("../../server.js", () => ({ createServer }));
		// process.exit would drop output still waiting on a pipe.
		const exit = vi.spyOn(process, "exit").mockImplementation((() => {
			throw new Error("process.exit called");
		}) as never);
		process.argv = ["node", "paca", "push", "a.md"];
		await import("../../index.js");
		await vi.waitFor(() => expect(process.exitCode).toBe(2));
		expect(exit).not.toHaveBeenCalled();
		expect(runCliMock.mock.calls[0][0]).toEqual(["push", "a.md"]);
		expect(createServer).not.toHaveBeenCalled();
	});
});
