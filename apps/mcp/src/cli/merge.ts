// A three-way merge of a mirror file edited locally with the version Paca
// now holds, by `git merge-file` on temporary copies in the mirror folder.

import { execFile } from "node:child_process";
import { randomBytes } from "node:crypto";
import fs from "node:fs/promises";
import os from "node:os";
import path from "node:path";

export type MergeResult =
	| { status: "clean"; text: string }
	| { status: "conflict"; text: string; conflicts: number }
	/** The merge could not run (no git, or git failed): `why` says so. */
	| { status: "unavailable"; why: string };

/** Pull's names for the three sides, in the marker lines git writes. */
export const LOCAL_LABEL = "local";
export const BASE_LABEL = "base";
export function remoteLabel(who: string): string {
	return `Paca${who ? ` (${who})` : ""}`;
}
/** A marker line pull's merge writes, with pull's labels. */
const PULL_MARKER = /^(?:<{7} local|\|{7} base|>{7} Paca(?: \(.*\))?)$/;

/** Whether `line` is a marker line pull's merge writes. */
export function isPullMarker(line: string): boolean {
	return PULL_MARKER.test(line);
}

/** The first line of `text` (from 1) that is a marker line pull's merge
 *  writes, anywhere (fenced code included), and not a line of `base`; 0 if
 *  none. */
export function pullMarkerLine(text: string, base: string): number {
	const known = new Set(base.split(/\r?\n/).filter(isPullMarker));
	const k = text
		.split(/\r?\n/)
		.findIndex((l) => isPullMarker(l) && !known.has(l));
	return k + 1;
}

export interface MergeLabels {
	local: string;
	base: string;
	remote: string;
}

/** Runs git; replaceable in tests. Resolves with the exit code and output,
 *  or rejects when git cannot be started. */
export type GitRunner = (
	args: string[],
	cwd: string,
) => Promise<{ code: number; stdout: string; stderr: string }>;

export const runGit: GitRunner = (args, cwd) =>
	new Promise((resolve, reject) => {
		execFile(
			"git",
			args,
			{
				cwd,
				encoding: "utf8",
				maxBuffer: 256 * 1024 * 1024,
				// The user's and the system's git config cannot change the merge.
				env: {
					...process.env,
					GIT_CONFIG_NOSYSTEM: "1",
					GIT_CONFIG_GLOBAL: os.devNull,
					LC_ALL: "C",
				},
			},
			(err, stdout, stderr) => {
				if (err && typeof err.code !== "number") return reject(err);
				resolve({
					code: err ? (err.code as unknown as number) : 0,
					stdout,
					stderr,
				});
			},
		);
	});

/** The local file the way the base and Paca's version are written: no byte
 *  order mark and LF line ends, so an editor's saving habits are not taken
 *  for changes on every line. */
export function asLF(text: string): string {
	return text.replace(/^﻿/, "").replace(/\r\n/g, "\n");
}

/**
 * Merges `local` and `remote`, both changed from `base`. The three are
 * written to temporary files in `dir` (named like the mirror's write
 * temporaries, so an interrupted run's leftovers are removed by the next
 * pull and never taken for documents) and removed afterwards.
 */
export async function mergeFile(
	dir: string,
	local: string,
	base: string,
	remote: string,
	labels: MergeLabels,
	git: GitRunner = runGit,
): Promise<MergeResult> {
	const names = [0, 1, 2].map(() =>
		path.join(dir, `.paca-tmp-w-${randomBytes(8).toString("hex")}`),
	);
	try {
		await fs.writeFile(names[0], asLF(local));
		await fs.writeFile(names[1], base);
		await fs.writeFile(names[2], remote);
		let res: Awaited<ReturnType<GitRunner>>;
		try {
			res = await git(
				[
					"merge-file",
					"-p",
					"--diff3",
					"-L",
					labels.local,
					"-L",
					labels.base,
					"-L",
					labels.remote,
					...names,
				],
				dir,
			);
		} catch (err) {
			const code = (err as NodeJS.ErrnoException).code;
			return {
				status: "unavailable",
				why:
					code === "ENOENT"
						? "git is not installed (or not on the PATH)"
						: `git could not run: ${(err as Error).message}`,
			};
		}
		// 0 clean, 1-127 the number of conflicts, anything else an error.
		if (res.code === 0) return { status: "clean", text: res.stdout };
		if (res.code > 0 && res.code < 128)
			return { status: "conflict", text: res.stdout, conflicts: res.code };
		const said = res.stderr.trim().split("\n")[0];
		return {
			status: "unavailable",
			why: `git merge-file failed${said ? `: ${said}` : ` (exit ${res.code})`}`,
		};
	} finally {
		for (const n of names) await fs.rm(n, { force: true });
	}
}
