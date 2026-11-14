import fs from "node:fs/promises";
import path from "node:path";
import { defaultGitExec, type GitExec } from "../git/git-runner.js";

/** Where a file sits in its repository, for a push's `source`. */
export interface GitSource {
	/** The repository root, or the working folder outside git. */
	root: string;
	/** "owner/name" from the origin remote, else the root folder's name. */
	repository: string;
	/** The file's path from the root, "/" separated. */
	relPath: string;
	/** HEAD, only when the file is tracked and has no uncommitted change. */
	commit?: string;
	/** Subject of the file's last commit, only when `commit` is set. */
	lastSubject?: string;
}

/**
 * The repository name in a remote URL: "owner/name" from
 * `git@host:owner/name.git`, `https://host/owner/name` or
 * `ssh://git@host:22/owner/name.git`; null when there is no path.
 * Credentials in the URL are never part of the result.
 */
export function repoNameFromRemote(url: string): string | null {
	let p = url.trim();
	const scheme = p.match(/^[a-z][a-z0-9+.-]*:\/\/[^/]*(\/.*)?$/i);
	if (scheme) p = scheme[1] ?? "";
	else if (/^[^/]+@?[^/]*:/.test(p)) p = p.slice(p.indexOf(":") + 1);
	const parts = p
		.replace(/\/+$/, "")
		.replace(/\.git$/, "")
		.split("/")
		.filter(Boolean);
	if (parts.length === 0) return null;
	return parts.slice(-2).join("/");
}

/** Finds the repository, source path and (for a clean file) commit of
 *  `file`. Outside a git repository the working folder `cwd` is the root. */
export async function gitSource(
	file: string,
	cwd: string,
	exec: GitExec = defaultGitExec,
): Promise<GitSource> {
	const abs = await fs.realpath(path.resolve(cwd, file));
	const dir = path.dirname(abs);
	let runIn = dir;
	const run = async (...args: string[]): Promise<string | null> => {
		try {
			return (await exec(args, { cwd: runIn })).stdout.trim();
		} catch {
			return null;
		}
	};

	const top = await run("rev-parse", "--show-toplevel");
	const root = top ? await fs.realpath(top) : await fs.realpath(cwd);
	const rel = path.relative(root, abs);
	if (!rel || rel.startsWith("..") || path.isAbsolute(rel))
		throw new Error(
			`${file} is outside ${top ? "the repository" : "the working folder"} (${root})`,
		);
	const relPath = rel.split(path.sep).join("/");
	if (!top) return { root, repository: path.basename(root), relPath };
	runIn = root;

	const remote = await run("remote", "get-url", "origin");
	const repository =
		(remote && repoNameFromRemote(remote)) || path.basename(root);
	const tracked =
		(await run("ls-files", "--error-unmatch", "--", relPath)) !== null;
	const status = await run("status", "--porcelain", "--", relPath);
	if (!tracked || status === null || status !== "")
		return { root, repository, relPath };
	const commit = (await run("rev-parse", "HEAD")) || undefined;
	const lastSubject =
		(await run("log", "-1", "--format=%s", "--", relPath)) || undefined;
	return { root, repository, relPath, commit, lastSubject };
}
