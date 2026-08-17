import { beforeEach, describe, expect, it, vi } from "vitest";

// clone_repository calls node:fs/promises' rm(targetDir, {recursive:true,
// force:true}) directly (no injection seam like gitExec has). Mocked here
// so a test — especially the protected-directory regression tests below,
// which deliberately exercise targetDir values like "/etc" and "/home"
// that really exist on whatever machine runs this suite — can never touch
// the real filesystem, regardless of whether the guard being tested is
// actually correct.
const rmMock = vi.fn().mockResolvedValue(undefined);
vi.mock("node:fs/promises", () => ({
	rm: (...args: unknown[]) => rmMock(...args),
}));

beforeEach(() => {
	rmMock.mockClear();
});

import {
	getRepoTools,
	handleRepoTool,
	scrubToken,
} from "../../tools/repo-tools.js";

function makeClient(overrides: Record<string, any> = {}) {
	return {
		listPluginRepositories: vi.fn().mockResolvedValue([]),
		getRepositoryCloneInfo: vi.fn().mockResolvedValue(undefined),
		...overrides,
	} as any;
}

// ---------------------------------------------------------------------------
// scrubToken
// ---------------------------------------------------------------------------

describe("scrubToken", () => {
	it("redacts the raw token", () => {
		expect(scrubToken("error near ghs_abc123", "ghs_abc123")).toBe(
			"error near ***",
		);
	});

	it("redacts a percent-encoded occurrence of the token", () => {
		const token = "ghs_abc/123";
		const text = `url had ${encodeURIComponent(token)} embedded`;
		expect(scrubToken(text, token)).toBe("url had *** embedded");
	});

	it("redacts the x-access-token credential pattern even if it doesn't match the token exactly", () => {
		const text =
			"remote: fatal: https://x-access-token:somethingelse@github.com/x/y not found";
		expect(scrubToken(text, "ghs_abc123")).toBe(
			"remote: fatal: https://x-access-token:***@github.com/x/y not found",
		);
	});

	it("returns the text unchanged when token is empty", () => {
		expect(scrubToken("hello world", "")).toBe("hello world");
	});
});

// ---------------------------------------------------------------------------
// getRepoTools
// ---------------------------------------------------------------------------

describe("getRepoTools", () => {
	it("returns exactly 3 tools", () => {
		expect(getRepoTools()).toHaveLength(3);
	});

	it("includes list_repositories, clone_repository, push_branch", () => {
		const names = getRepoTools().map((t) => t.name);
		expect(names).toEqual([
			"list_repositories",
			"clone_repository",
			"push_branch",
		]);
	});

	it("marks projectId/pluginId/repoId as required on clone_repository", () => {
		const tool = getRepoTools().find((t) => t.name === "clone_repository");
		expect(tool).toBeDefined();
		expect(tool?.inputSchema.required).toEqual([
			"projectId",
			"pluginId",
			"repoId",
		]);
	});
});

// ---------------------------------------------------------------------------
// handleRepoTool – list_repositories
// ---------------------------------------------------------------------------

describe("handleRepoTool – list_repositories", () => {
	it("returns a 'no repositories' message when repoPluginIds is empty", async () => {
		const client = makeClient();
		const result = await handleRepoTool(
			"list_repositories",
			{ projectId: "p1" },
			client,
			[],
		);
		expect(client.listPluginRepositories).not.toHaveBeenCalled();
		expect(result.content[0].text).toContain("No repositories found");
	});

	it("aggregates repositories across every configured plugin", async () => {
		const client = makeClient({
			listPluginRepositories: vi.fn(async (pluginId: string) => {
				if (pluginId === "com.paca.github") {
					return [
						{
							id: "r1",
							full_name: "acme/widgets",
							owner: "acme",
							repo_name: "widgets",
							clone_url: "https://github.com/acme/widgets.git",
						},
					];
				}
				return [];
			}),
		});
		const result = await handleRepoTool(
			"list_repositories",
			{ projectId: "p1" },
			client,
			["com.paca.github", "com.paca.gitlab"],
		);
		expect(client.listPluginRepositories).toHaveBeenCalledWith(
			"com.paca.github",
			"p1",
		);
		expect(client.listPluginRepositories).toHaveBeenCalledWith(
			"com.paca.gitlab",
			"p1",
		);
		expect(result.content[0].text).toContain("acme/widgets");
		expect(result.content[0].text).toContain("Plugin: Github");
		expect(result.content[0].text).toContain("pluginId: The plugin ID");
	});

	it("reports a per-plugin error without failing the whole call when some plugins fail", async () => {
		const client = makeClient({
			listPluginRepositories: vi.fn(async (pluginId: string) => {
				if (pluginId === "broken") throw new Error("plugin unreachable");
				return [
					{
						id: "r1",
						full_name: "acme/widgets",
						owner: "acme",
						repo_name: "widgets",
						clone_url: "https://github.com/acme/widgets.git",
					},
				];
			}),
		});
		const result = await handleRepoTool(
			"list_repositories",
			{ projectId: "p1" },
			client,
			["broken", "ok-plugin"],
		);
		expect(result.content[0].text).toContain("acme/widgets");
	});

	it("returns an error message when every plugin fails", async () => {
		const client = makeClient({
			listPluginRepositories: vi.fn().mockRejectedValue(new Error("boom")),
		});
		const result = await handleRepoTool(
			"list_repositories",
			{ projectId: "p1" },
			client,
			["broken"],
		);
		expect(result.content[0].text).toContain("Failed to list repositories");
		expect(result.content[0].text).toContain("boom");
	});

	it("throws a ZodError when projectId is missing", async () => {
		await expect(
			handleRepoTool("list_repositories", {}, makeClient(), []),
		).rejects.toThrow();
	});
});

// ---------------------------------------------------------------------------
// handleRepoTool – clone_repository
// ---------------------------------------------------------------------------

describe("handleRepoTool – clone_repository", () => {
	const cloneInfo = {
		id: "r1",
		full_name: "acme/widgets",
		clone_url: "https://github.com/acme/widgets.git",
		token: "ghs_secret",
	};

	it("clones using an authenticated URL and reports the checked-out branch", async () => {
		const client = makeClient({
			getRepositoryCloneInfo: vi.fn().mockResolvedValue(cloneInfo),
		});
		const gitExec = vi
			.fn()
			.mockResolvedValueOnce({ stdout: "", stderr: "" }) // clone
			.mockResolvedValueOnce({ stdout: "main\n", stderr: "" }); // branch --show-current

		const result = await handleRepoTool(
			"clone_repository",
			{ projectId: "p1", pluginId: "com.paca.github", repoId: "r1" },
			client,
			[],
			gitExec,
		);

		expect(client.getRepositoryCloneInfo).toHaveBeenCalledWith(
			"com.paca.github",
			"p1",
			"r1",
		);
		const cloneCall = gitExec.mock.calls[0];
		expect(cloneCall[0][0]).toBe("clone");
		expect(cloneCall[0][1]).toContain("x-access-token:ghs_secret@github.com");
		expect(cloneCall[0][2]).toBe("/home/goose/repo");
		expect(result.content[0].text).toContain("cloned successfully");
		expect(result.content[0].text).toContain("main");
	});

	it("uses the provided targetDir instead of the default", async () => {
		const client = makeClient({
			getRepositoryCloneInfo: vi.fn().mockResolvedValue(cloneInfo),
		});
		const gitExec = vi
			.fn()
			.mockResolvedValueOnce({ stdout: "", stderr: "" })
			.mockResolvedValueOnce({ stdout: "main\n", stderr: "" });

		await handleRepoTool(
			"clone_repository",
			{
				projectId: "p1",
				pluginId: "com.paca.github",
				repoId: "r1",
				targetDir: "/home/goose/custom",
			},
			client,
			[],
			gitExec,
		);
		expect(gitExec.mock.calls[0][0][2]).toBe("/home/goose/custom");
	});

	it("reports repository-not-found without touching git", async () => {
		const client = makeClient({
			getRepositoryCloneInfo: vi.fn().mockResolvedValue(undefined),
		});
		const gitExec = vi.fn();
		const result = await handleRepoTool(
			"clone_repository",
			{ projectId: "p1", pluginId: "com.paca.github", repoId: "missing" },
			client,
			[],
			gitExec,
		);
		expect(gitExec).not.toHaveBeenCalled();
		expect(result.content[0].text).toContain("not found");
	});

	it("scrubs the token out of a git clone failure message", async () => {
		const client = makeClient({
			getRepositoryCloneInfo: vi.fn().mockResolvedValue(cloneInfo),
		});
		const gitExec = vi.fn().mockRejectedValueOnce({
			stderr:
				"fatal: could not read from x-access-token:ghs_secret@github.com/acme/widgets.git",
		});
		const result = await handleRepoTool(
			"clone_repository",
			{ projectId: "p1", pluginId: "com.paca.github", repoId: "r1" },
			client,
			[],
			gitExec,
		);
		expect(result.content[0].text).not.toContain("ghs_secret");
		expect(result.content[0].text).toContain("git clone failed");
	});

	// Regression tests: clone_repository force-deletes targetDir before
	// cloning into it (`rm(targetDir, { recursive: true, force: true })`),
	// and targetDir was previously taken as-is from agent-supplied input
	// with no validation — a task/prompt that got the agent to pass a
	// catastrophic targetDir (e.g. "/", "/home") could wipe it out with no
	// confirmation. None of these should ever reach gitExec or the delete.
	it.each([
		["/", "/"],
		["/home", "/home"],
		["/home/goose", "/home/goose"],
		["/home/goose/", "/home/goose"],
		["/etc", "/etc"],
		["/etc/", "/etc"],
		["/tmp", "/tmp"],
		["/../../etc", "/etc"],
		["/home/goose/../../etc", "/etc"],
		["/home/goose/../", "/home"],
	])("refuses to clone into the protected system directory %s", async (targetDir, _resolved) => {
		const client = makeClient({
			getRepositoryCloneInfo: vi.fn().mockResolvedValue(cloneInfo),
		});
		const gitExec = vi.fn();
		const result = await handleRepoTool(
			"clone_repository",
			{ projectId: "p1", pluginId: "com.paca.github", repoId: "r1", targetDir },
			client,
			[],
			gitExec,
		);
		expect(rmMock).not.toHaveBeenCalled();
		expect(gitExec).not.toHaveBeenCalled();
		expect(result.content[0].text).toContain("Failed to clone repository");
		expect(result.content[0].text).toMatch(/protected system directory/i);
	});

	it("still allows cloning into a subdirectory of a protected top-level directory", async () => {
		const client = makeClient({
			getRepositoryCloneInfo: vi.fn().mockResolvedValue(cloneInfo),
		});
		const gitExec = vi
			.fn()
			.mockResolvedValueOnce({ stdout: "", stderr: "" })
			.mockResolvedValueOnce({ stdout: "main\n", stderr: "" });

		const result = await handleRepoTool(
			"clone_repository",
			{
				projectId: "p1",
				pluginId: "com.paca.github",
				repoId: "r1",
				targetDir: "/home/goose/repo",
			},
			client,
			[],
			gitExec,
		);
		expect(gitExec).toHaveBeenCalled();
		expect(result.content[0].text).toContain("cloned successfully");
	});

	it("throws a ZodError when repoId is missing", async () => {
		await expect(
			handleRepoTool(
				"clone_repository",
				{ projectId: "p1", pluginId: "com.paca.github" },
				makeClient(),
				[],
			),
		).rejects.toThrow();
	});
});

// ---------------------------------------------------------------------------
// handleRepoTool – push_branch
// ---------------------------------------------------------------------------

describe("handleRepoTool – push_branch", () => {
	const cloneInfo = {
		id: "r1",
		full_name: "acme/widgets",
		clone_url: "https://github.com/acme/widgets.git",
		token: "ghs_secret",
	};

	it("pushes HEAD to the explicit branch name when given", async () => {
		const client = makeClient({
			getRepositoryCloneInfo: vi.fn().mockResolvedValue(cloneInfo),
		});
		const gitExec = vi.fn().mockResolvedValue({ stdout: "", stderr: "" });

		const result = await handleRepoTool(
			"push_branch",
			{
				projectId: "p1",
				pluginId: "com.paca.github",
				repoId: "r1",
				branchName: "feature/x",
			},
			client,
			[],
			gitExec,
		);

		expect(gitExec).toHaveBeenCalledTimes(1);
		const [args, cwd] = gitExec.mock.calls[0];
		expect(args[0]).toBe("push");
		expect(args[1]).toContain("x-access-token:ghs_secret@github.com");
		expect(args[2]).toBe("HEAD:feature/x");
		expect(cwd).toBe("/home/goose/repo");
		expect(result.content[0].text).toContain("feature/x");
		expect(result.content[0].text).toContain("pushed to remote successfully");
		expect(result.content[0].text).toContain("github_create_pull_request");
	});

	it("resolves the current branch when branchName is omitted", async () => {
		const client = makeClient({
			getRepositoryCloneInfo: vi.fn().mockResolvedValue(cloneInfo),
		});
		const gitExec = vi
			.fn()
			.mockResolvedValueOnce({ stdout: "auto-branch\n", stderr: "" }) // branch --show-current
			.mockResolvedValueOnce({ stdout: "", stderr: "" }); // push

		await handleRepoTool(
			"push_branch",
			{ projectId: "p1", pluginId: "com.paca.github", repoId: "r1" },
			client,
			[],
			gitExec,
		);

		expect(gitExec.mock.calls[0][0]).toEqual(["branch", "--show-current"]);
		expect(gitExec.mock.calls[1][0][2]).toBe("HEAD:auto-branch");
	});

	it("fails without pushing when the branch cannot be resolved", async () => {
		const client = makeClient({
			getRepositoryCloneInfo: vi.fn().mockResolvedValue(cloneInfo),
		});
		const gitExec = vi.fn().mockResolvedValueOnce({ stdout: "", stderr: "" });

		const result = await handleRepoTool(
			"push_branch",
			{ projectId: "p1", pluginId: "com.paca.github", repoId: "r1" },
			client,
			[],
			gitExec,
		);
		expect(gitExec).toHaveBeenCalledTimes(1);
		expect(result.content[0].text).toContain(
			"Could not determine current branch",
		);
	});

	it("scrubs the token out of a git push failure message", async () => {
		const client = makeClient({
			getRepositoryCloneInfo: vi.fn().mockResolvedValue(cloneInfo),
		});
		const gitExec = vi.fn().mockRejectedValueOnce({
			stderr:
				"remote: permission denied for x-access-token:ghs_secret@github.com",
		});
		const result = await handleRepoTool(
			"push_branch",
			{
				projectId: "p1",
				pluginId: "com.paca.github",
				repoId: "r1",
				branchName: "feature/x",
			},
			client,
			[],
			gitExec,
		);
		expect(result.content[0].text).not.toContain("ghs_secret");
		expect(result.content[0].text).toContain("git push failed");
	});
});

// ---------------------------------------------------------------------------
// handleRepoTool – unknown tool
// ---------------------------------------------------------------------------

describe("handleRepoTool – unknown tool", () => {
	it("throws for an unknown tool name", async () => {
		await expect(
			handleRepoTool("unknown_tool", {}, makeClient(), []),
		).rejects.toThrow("Unknown repository tool");
	});
});
