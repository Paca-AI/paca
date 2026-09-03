import { execFile as execFileCb } from "node:child_process";
import { promisify } from "node:util";
import type { Tool } from "@modelcontextprotocol/sdk/types.js";
import { z } from "zod";
import type { PacaAPIClient } from "../api/index.js";

const execFileAsync = promisify(execFileCb);

// PACA_WORKDIR, when set, is this conversation's actual working directory —
// set by services/agent-runner's executor.go buildMCPServers only for a
// conversation attached to a static environment (trigger.Workdir), where
// it's the environment folder the agent was attached to, e.g.
// "/home/paca/workspaces/test". Falls back to sandboxWorkdir + "/repo" (that
// Go constant's own value, "/home/goose") for an ephemeral conversation,
// which never sets PACA_WORKDIR — the Goose sandbox's ACP session cwd, and
// the only directory guaranteed writable by the container's unprivileged
// "goose" user. Used only as this tool set's *default* target/repo dir;
// nothing here requires it, so a Goose sandbox image change wouldn't need
// matching code here, just a different default path. Read once at module
// load, matching every other PACA_* config value in this codebase (see
// index.ts) — this process is re-spawned fresh for every conversation
// attach, so there's no per-call staleness to worry about.
const DEFAULT_REPO_DIR = process.env.PACA_WORKDIR || "/home/goose/repo";

/**
 * Runs one git subcommand. A thin injection seam over child_process.execFile
 * so tests can substitute a fake without mocking node:child_process globally
 * — same spirit as every other tool file taking its API client as a
 * parameter rather than importing a singleton.
 */
export type GitExec = (
	args: string[],
	cwd?: string,
) => Promise<{ stdout: string; stderr: string }>;

async function defaultGitExec(
	args: string[],
	cwd?: string,
): Promise<{ stdout: string; stderr: string }> {
	return execFileAsync("git", args, { cwd, maxBuffer: 10 * 1024 * 1024 });
}

/**
 * Removes an auth token from git command output before it can reach a log
 * or the LLM's context. Port of repo_tools.py's _scrub_token — same three
 * passes: the raw token, its percent-encoded form (it's embedded in a URL),
 * and the general x-access-token:...@ credential pattern git itself may
 * echo in a remote error message even when the token substring doesn't
 * match exactly (e.g. after git's own URL-normalization).
 */
export function scrubToken(text: string, token: string): string {
	if (!token) {
		return text;
	}
	let scrubbed = text.split(token).join("***");
	const encoded = encodeURIComponent(token);
	if (encoded !== token) {
		scrubbed = scrubbed.split(encoded).join("***");
	}
	return scrubbed.replace(/x-access-token:[^@]+@/g, "x-access-token:***@");
}

/** Builds an authenticated HTTPS clone/push URL, token percent-encoded. */
function authenticatedCloneURL(cloneURL: string, token: string): string {
	const parsed = new URL(cloneURL);
	const host = parsed.port
		? `${parsed.hostname}:${parsed.port}`
		: parsed.hostname;
	return `https://x-access-token:${encodeURIComponent(token)}@${host}${parsed.pathname}`;
}

function errorMessage(error: unknown): string {
	if (error && typeof error === "object") {
		const e = error as {
			stderr?: unknown;
			stdout?: unknown;
			message?: unknown;
		};
		if (typeof e.stderr === "string" && e.stderr.trim()) return e.stderr.trim();
		if (typeof e.stdout === "string" && e.stdout.trim()) return e.stdout.trim();
		if (typeof e.message === "string") return e.message;
	}
	return String(error);
}

const ListRepositoriesSchema = z.object({
	projectId: z.string(),
});

const CloneRepositorySchema = z.object({
	projectId: z.string(),
	pluginId: z.string(),
	repoId: z.string(),
	targetDir: z.string().optional(),
});

const PushBranchSchema = z.object({
	projectId: z.string(),
	pluginId: z.string(),
	repoId: z.string(),
	branchName: z.string().optional(),
	repoDir: z.string().optional(),
});

/**
 * Returns the repository-related MCP tools. Only meaningful when the
 * project has at least one repository plugin installed — server.ts hides
 * this entire set from the tool listing otherwise, mirroring
 * executor.py's `has_repos` gate on whether these tools get attached to the
 * agent at all.
 */
export function getRepoTools(): Tool[] {
	return [
		{
			name: "list_repositories",
			description:
				"List all available repositories from all repository plugins " +
				"installed on this project. Use this to see available " +
				"repositories, choose which to clone, or get repository IDs for " +
				"use with clone_repository.",
			inputSchema: {
				type: "object",
				properties: {
					projectId: {
						type: "string",
						description: "The technical UUID of the project.",
					},
				},
				required: ["projectId"],
			},
		},
		{
			name: "clone_repository",
			description:
				"Clone a repository into your workspace so you can read and " +
				"modify the code. You MUST call list_repositories first to get " +
				"the pluginId and repoId. Before calling this, check whether the " +
				"source code might already be present in or under the target " +
				"directory — e.g. with the tree or shell tool — since your " +
				"working directory can persist across conversations and may " +
				"already hold a checkout (possibly in a subdirectory, and " +
				"possibly without git initialized yet, so don't rely on a .git " +
				"folder alone). This tool never deletes anything: it fails " +
				"without touching the target directory if it's non-empty, rather " +
				"than guessing whether it's safe to overwrite.",
			inputSchema: {
				type: "object",
				properties: {
					projectId: {
						type: "string",
						description: "The technical UUID of the project.",
					},
					pluginId: {
						type: "string",
						description: "The plugin ID from list_repositories.",
					},
					repoId: {
						type: "string",
						description: "The repository ID from list_repositories.",
					},
					targetDir: {
						type: "string",
						description: `Target directory for cloning (absolute path). Defaults to ${DEFAULT_REPO_DIR}.`,
					},
				},
				required: ["projectId", "pluginId", "repoId"],
			},
		},
		{
			name: "push_branch",
			description:
				"Push the current local branch to the remote repository. Call " +
				"this after you have committed all your changes and are ready " +
				"to publish the branch. You MUST have cloned the repository " +
				"first with clone_repository.\n\n" +
				"Mandatory next step: after a successful push, you MUST create " +
				"a pull/merge request so the changes can be reviewed. Call the " +
				"repository plugin's PR creation tool (e.g. " +
				"github_create_pull_request for GitHub repos) with the branch " +
				"you just pushed as head_branch and the repo's default branch " +
				"as base_branch. A pushed branch without a PR is unfinished work.",
			inputSchema: {
				type: "object",
				properties: {
					projectId: {
						type: "string",
						description: "The technical UUID of the project.",
					},
					pluginId: {
						type: "string",
						description: "The plugin ID (from list_repositories).",
					},
					repoId: {
						type: "string",
						description: "The repository ID (from list_repositories).",
					},
					branchName: {
						type: "string",
						description:
							"Name of the branch to push. Leave empty to use the currently checked-out branch.",
					},
					repoDir: {
						type: "string",
						description: `Local path to the cloned repository. Defaults to ${DEFAULT_REPO_DIR}.`,
					},
				},
				required: ["projectId", "pluginId", "repoId"],
			},
		},
	];
}

/**
 * Handles repository tool calls. repoPluginIds scopes list_repositories to
 * the plugins actually configured for this conversation (forwarded from
 * agent-runner's trigger.RepoPluginIDs — see PacaConfig.repoPluginIds);
 * clone_repository/push_branch take pluginId explicitly per call instead,
 * exactly like repo_tools.py's own executors.
 *
 * Failures that are a normal, expected tool outcome (git exited non-zero,
 * the plugin API returned an error) are reported as ordinary — not
 * isError — text results, matching repo_tools.py's Observation.success
 * pattern: the agent should see "clone failed: <reason>" as something to
 * read and react to, not have the tool call itself blow up.
 */
export async function handleRepoTool(
	toolName: string,
	args: any,
	client: PacaAPIClient,
	repoPluginIds: string[],
	gitExec: GitExec = defaultGitExec,
): Promise<any> {
	switch (toolName) {
		case "list_repositories": {
			ListRepositoriesSchema.parse(args);
			const { projectId } = args;

			if (repoPluginIds.length === 0) {
				return {
					content: [
						{
							type: "text",
							text: "No repositories found. Make sure a repository is linked to this project.",
						},
					],
				};
			}

			const repos: Array<{
				plugin_id: string;
				plugin_name: string;
				repo_id: string;
				full_name: string;
				owner: string;
				repo_name: string;
				clone_url: string;
			}> = [];
			const errors: string[] = [];

			for (const pluginId of repoPluginIds) {
				try {
					const items = await client.listPluginRepositories(
						pluginId,
						projectId,
					);
					for (const repo of items) {
						repos.push({
							plugin_id: pluginId,
							plugin_name: pluginNameFromID(pluginId),
							repo_id: repo.id,
							full_name: repo.full_name,
							owner: repo.owner,
							repo_name: repo.repo_name,
							clone_url: repo.clone_url,
						});
					}
				} catch (error) {
					errors.push(`${pluginId}: ${errorMessage(error)}`);
				}
			}

			if (errors.length > 0 && repos.length === 0) {
				return {
					content: [
						{
							type: "text",
							text: `Failed to list repositories: ${errors.join("; ")}`,
						},
					],
				};
			}
			if (repos.length === 0) {
				return {
					content: [
						{
							type: "text",
							text: "No repositories found. Make sure a repository is linked to this project.",
						},
					],
				};
			}

			const lines = [`Found ${repos.length} available repository(ies):`, ""];
			repos.forEach((repo, i) => {
				lines.push(
					`${i + 1}. ${repo.full_name}`,
					`   Plugin: ${repo.plugin_name}`,
					`   Repository ID: ${repo.repo_id}`,
					`   Owner: ${repo.owner}`,
					`   Repository: ${repo.repo_name}`,
					`   Clone URL: ${repo.clone_url}`,
					"",
				);
			});
			lines.push("To clone a repository, use the clone_repository tool with:");
			lines.push(`  - pluginId: The plugin ID (e.g., '${repos[0].plugin_id}')`);
			lines.push(`  - repoId: The repository ID (e.g., '${repos[0].repo_id}')`);
			lines.push(
				`  - targetDir: Target directory (default: ${DEFAULT_REPO_DIR})`,
			);
			return { content: [{ type: "text", text: lines.join("\n") }] };
		}

		case "clone_repository": {
			CloneRepositorySchema.parse(args);
			const { projectId, pluginId, repoId } = args;
			const targetDir: string = args.targetDir || DEFAULT_REPO_DIR;

			try {
				const info = await client.getRepositoryCloneInfo(
					pluginId,
					projectId,
					repoId,
				);
				if (!info?.id) {
					return {
						content: [
							{
								type: "text",
								text: `Failed to clone repository:\n  Repository ${repoId} not found in plugin ${pluginId}`,
							},
						],
					};
				}

				const token = info.token ?? "";
				const cloneURL = authenticatedCloneURL(info.clone_url, token);

				// No pre-emptive delete: git itself refuses to clone into a
				// non-empty directory ("destination path ... already exists
				// and is not an empty directory"), which is both simpler and
				// more reliable than this tool trying to guess whether
				// existing content is safe to discard — see the
				// clone_repository tool description's own guidance for why
				// (a prior version of this code auto-detected "already a git
				// repo" via a bare .git check and skipped cloning, which
				// missed a repo cloned into a *subdirectory* of targetDir,
				// and treated existing-but-not-yet-git-initialized source as
				// safe to delete).
				try {
					await gitExec(["clone", cloneURL, targetDir]);
				} catch (error) {
					const safeMsg = scrubToken(errorMessage(error), token);
					return {
						content: [
							{
								type: "text",
								text: `Failed to clone repository:\n  git clone failed: ${safeMsg}`,
							},
						],
					};
				}

				const branchResult = await gitExec(
					["branch", "--show-current"],
					targetDir,
				);
				const branch = branchResult.stdout.trim();

				return {
					content: [
						{
							type: "text",
							text: [
								"Repository cloned successfully!",
								`  Location: ${targetDir}`,
								`  Current branch: ${branch}`,
								"",
								"You can now work with the code in this repository.",
							].join("\n"),
						},
					],
				};
			} catch (error) {
				return {
					content: [
						{
							type: "text",
							text: `Failed to clone repository:\n  ${errorMessage(error)}`,
						},
					],
				};
			}
		}

		case "push_branch": {
			PushBranchSchema.parse(args);
			const { projectId, pluginId, repoId } = args;
			const repoDir: string = args.repoDir || DEFAULT_REPO_DIR;

			try {
				const info = await client.getRepositoryCloneInfo(
					pluginId,
					projectId,
					repoId,
				);
				const token = info?.token ?? "";
				const cloneURLStr = info?.clone_url ?? "";
				if (!cloneURLStr) {
					return {
						content: [
							{
								type: "text",
								text: "Failed to push branch: Could not determine clone URL for repository.",
							},
						],
					};
				}

				let branch = (args.branchName ?? "").trim();
				if (!branch) {
					const br = await gitExec(["branch", "--show-current"], repoDir);
					branch = br.stdout.trim();
				}
				if (!branch) {
					return {
						content: [
							{
								type: "text",
								text: "Failed to push branch: Could not determine current branch. Specify branchName explicitly.",
							},
						],
					};
				}

				const pushURL = authenticatedCloneURL(cloneURLStr, token);
				try {
					await gitExec(["push", pushURL, `HEAD:${branch}`], repoDir);
				} catch (error) {
					const safeMsg = scrubToken(errorMessage(error), token);
					return {
						content: [
							{
								type: "text",
								text: `Failed to push branch: git push failed: ${safeMsg}`,
							},
						],
					};
				}

				return {
					content: [
						{
							type: "text",
							text:
								`Branch '${branch}' pushed to remote successfully.\n\n` +
								"**Next step (mandatory):** Create a pull/merge request now " +
								"so the changes can be reviewed. Call the repository plugin's " +
								"PR creation tool (e.g. `github_create_pull_request`) with " +
								"the branch you just pushed as `head_branch` and the repo's " +
								"default branch as `base_branch`. Do not consider the task " +
								"complete until a PR has been created.",
						},
					],
				};
			} catch (error) {
				return {
					content: [
						{
							type: "text",
							text: `Failed to push branch: ${errorMessage(error)}`,
						},
					],
				};
			}
		}

		default:
			throw new Error(`Unknown repository tool: ${toolName}`);
	}
}

/** "com.paca.github" -> "Github", matching repo_tools.py's plugin_id.split(".")[-1].title(). */
function pluginNameFromID(pluginId: string): string {
	const last = pluginId.split(".").at(-1) ?? pluginId;
	return last.charAt(0).toUpperCase() + last.slice(1).toLowerCase();
}
