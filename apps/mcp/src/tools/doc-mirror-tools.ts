import { promises as fs } from "node:fs";
import os from "node:os";
import path from "node:path";
import type { Tool } from "@modelcontextprotocol/sdk/types.js";
import { z } from "zod";
import type { PacaAPIDocClient } from "../api/index.js";
import { type PullOptions, runPull } from "../cli/pull.js";
import { type PushOptions, runPush } from "../cli/push.js";
import {
	type CliIO,
	EXIT,
	PULL_STATE_FILE,
	UnreachableError,
} from "../cli/state.js";
import { formatToolError } from "../utils/index.js";

const PullDocsSchema = z.object({
	projectId: z.string(),
	dir: z.string().min(1).optional().default(".paca/docs"),
	force: z.boolean().optional().default(false),
});

const PushDocsSchema = z.object({
	projectId: z.string(),
	file: z.string().min(1),
	path: z.string().min(1).optional(),
	note: z.string().max(500).optional(),
	task: z.string().max(50).optional(),
	lock: z.boolean().optional().default(false),
	force: z.boolean().optional().default(false),
	dryRun: z.boolean().optional().default(false),
});

/** Result of a tool call. */
interface ToolResult {
	content: { type: "text"; text: string }[];
	isError: boolean;
}

/** Folders the tools may read from and write to: the server's working
 *  directory, the system temp folder, and any in PACA_MCP_FILE_ROOTS
 *  (path-delimiter separated). Keeps a prompt-injected agent from pushing
 *  e.g. ~/.ssh files into a document or mirroring into arbitrary folders. */
function allowedRoots(): string[] {
	const extra = (process.env.PACA_MCP_FILE_ROOTS ?? "")
		.split(path.delimiter)
		.filter(Boolean);
	// "/tmp" is the folder the tool help and skills name; on macOS and Windows
	// it is not os.tmpdir(), so admit it where it exists.
	const posixTmp = process.platform === "win32" ? [] : ["/tmp"];
	return [process.cwd(), os.tmpdir(), ...posixTmp, ...extra].map((r) =>
		path.resolve(r),
	);
}

/** realpath of the nearest existing ancestor, plus the not-yet-created rest. */
async function realish(abs: string): Promise<string> {
	const rest: string[] = [];
	let cur = abs;
	for (;;) {
		try {
			return path.join(await fs.realpath(cur), ...rest);
		} catch {
			const parent = path.dirname(cur);
			if (parent === cur) return abs;
			rest.unshift(path.basename(cur));
			cur = parent;
		}
	}
}

/** Throws unless `target` (resolved against the cwd, symlinks followed) is
 *  inside an allowed root. */
export async function assertAllowedPath(
	target: string,
	what: string,
): Promise<void> {
	const real = await realish(path.resolve(target));
	for (const root of allowedRoots()) {
		const r = await realish(root);
		const rel = path.relative(r, real);
		if (rel === "" || (!rel.startsWith("..") && !path.isAbsolute(rel))) return;
	}
	throw new Error(
		`${what} "${target}" is outside the folders the Paca MCP server may use (its working directory, the system temp folder ${os.tmpdir()}, /tmp, or PACA_MCP_FILE_ROOTS).`,
	);
}

/** True when `dir` does not exist yet or already holds a pull mirror. */
async function isMirrorOrNew(dir: string): Promise<boolean> {
	const abs = path.resolve(dir);
	try {
		await fs.stat(abs);
	} catch {
		return true;
	}
	try {
		await fs.stat(path.join(abs, PULL_STATE_FILE));
		return true;
	} catch {
		return false;
	}
}

/** Keeps a mirror out of Git: a `.gitignore` of `*` inside the mirror folder. */
async function ignoreMirror(dir: string): Promise<void> {
	const file = path.join(path.resolve(dir), ".gitignore");
	try {
		await fs.writeFile(file, "*\n", { flag: "wx" });
	} catch {
		// exists already, or the folder is not writable: nothing to do
	}
}

export const DOC_MIRROR_TOOL_NAMES = new Set(["pull_docs", "push_docs"]);

export function getDocMirrorTools(): Tool[] {
	return [
		{
			name: "pull_docs",
			description:
				"Pull (mirror) all of a project's documents to Markdown files on disk, one file per document at <dir>/<folder path>/<title>.md. " +
				"Use this when asked to 'pull docs' or to work on documents as local files; use read_doc / list_docs only to read a few documents into the conversation. " +
				"Running it again moves only what changed in Paca, and a file edited locally is merged with the changes made in Paca " +
				"(a conflict leaves conflict markers in the file and is reported). " +
				"Files are written relative to the working directory of the Paca MCP server, and only inside that directory, the system temp folder or PACA_MCP_FILE_ROOTS. " +
				"Prefer a temporary folder outside the repository (e.g. dir '/tmp/paca-docs'); the mirror folder gets its own .gitignore so it is never committed. " +
				"To send an edited file back, use push_docs on the file.",
			inputSchema: {
				type: "object",
				properties: {
					projectId: {
						type: "string",
						description:
							"The technical UUID of the project. Use list_projects to get the project ID.",
					},
					dir: {
						type: "string",
						description:
							"Mirror folder: an absolute path (preferably a temporary folder like '/tmp/paca-docs') or relative to the server's working directory (default '.paca/docs'). The folder gets its own .gitignore.",
					},
					force: {
						type: "boolean",
						description:
							"Replace every mirror file with Paca's version, discarding edits made locally. Default false.",
					},
				},
				required: ["projectId"],
			},
		},
		{
			name: "push_docs",
			description:
				"Push a Markdown file on disk to a Paca document. " +
				"A file written by pull_docs is pushed back to the document it came from, as an edit of only the blocks it changes " +
				"(path, lock and force do not apply; on a conflict, run pull_docs, resolve, then push again). " +
				"Any other file is written to the document at `path` (default Plans/<file name>, or Specifications/<name> under a specs folder) as a managed copy. " +
				"The file is read relative to the working directory of the Paca MCP server, and must be inside that directory, the system temp folder or PACA_MCP_FILE_ROOTS. " +
				"Pushing a file that is not a pull mirror file records its sync state in .paca/push.json at the file's Git root. Use dryRun to see what would change.",
			inputSchema: {
				type: "object",
				properties: {
					projectId: {
						type: "string",
						description:
							"The technical UUID of the project. Use list_projects to get the project ID.",
					},
					file: {
						type: "string",
						description:
							"The file to push, relative to the server's working directory.",
					},
					path: {
						type: "string",
						description:
							"Document path 'Folder/Title' for a file outside a pull mirror.",
					},
					note: {
						type: "string",
						description: "Note recorded with the change (max 500 characters).",
					},
					task: {
						type: "string",
						description: "Task key to link the change to (e.g. PACA-88).",
					},
					lock: {
						type: "boolean",
						description: "Lock the document against edits made in Paca.",
					},
					force: {
						type: "boolean",
						description:
							"Overwrite the document even if it changed in Paca since the last push.",
					},
					dryRun: {
						type: "boolean",
						description: "Show what would change without writing.",
					},
				},
				required: ["projectId", "file"],
			},
		},
	];
}

/** Runs a CLI command against a collecting IO and shapes the tool result:
 *  0 done, 2 conflict (reported, not an error), 1 and 3 errors. */
async function runCommand(
	run: (io: CliIO) => Promise<number>,
	summaries: { ok: string; conflict: string; failed: string },
): Promise<ToolResult> {
	const lines: string[] = [];
	const io: CliIO = {
		out: (line) => lines.push(line),
		err: (line) => lines.push(`error: ${line}`),
	};
	let code: number;
	try {
		code = await run(io);
	} catch (err) {
		if (err instanceof UnreachableError) {
			code = EXIT.unreachable;
			lines.push(`error: ${err.message}`);
		} else {
			return {
				content: [
					{ type: "text", text: [...lines, formatToolError(err)].join("\n") },
				],
				isError: true,
			};
		}
	}
	const summary =
		code === EXIT.ok
			? summaries.ok
			: code === EXIT.conflict
				? summaries.conflict
				: code === EXIT.unreachable
					? "Could not reach the Paca server."
					: summaries.failed;
	return {
		content: [{ type: "text", text: [summary, ...lines].join("\n") }],
		isError: code === EXIT.error || code === EXIT.unreachable,
	};
}

export async function handleDocMirrorTool(
	toolName: string,
	args: unknown,
	docClient: PacaAPIDocClient,
	deps: {
		pull?: (
			opts: PullOptions,
			client: PacaAPIDocClient,
			io: CliIO,
		) => Promise<number>;
		push?: (
			opts: PushOptions,
			client: PacaAPIDocClient,
			io: CliIO,
		) => Promise<number>;
	} = {},
): Promise<ToolResult> {
	if (toolName === "pull_docs") {
		const { projectId, dir, force } = PullDocsSchema.parse(args);
		await assertAllowedPath(dir, "dir");
		if ((await realish(path.resolve(dir))) === (await realish(process.cwd())))
			throw new Error(
				`dir "${dir}" is the server's working directory; use a separate mirror folder.`,
			);
		// Only a folder this pull creates, or an existing mirror, is git-ignored;
		// a caller's own folder is left alone.
		const ownsFolder = await isMirrorOrNew(dir);
		const res = await runCommand(
			(io) => (deps.pull ?? runPull)({ projectId, dir, force }, docClient, io),
			{
				ok: `Pulled documents to ${dir}.`,
				conflict: `Pulled documents to ${dir}, but some files need attention: a file edited locally was kept or has conflict markers. Resolve the markers in those files. Only if the local edits are not needed, pull again with force: that DISCARDS them and takes Paca's version.`,
				failed: "Pull failed; nothing was changed.",
			},
		);
		if (!res.isError && ownsFolder) await ignoreMirror(dir);
		return res;
	}
	if (toolName === "push_docs") {
		const a = PushDocsSchema.parse(args);
		await assertAllowedPath(a.file, "file");
		const opts: PushOptions = {
			projectId: a.projectId,
			file: a.file,
			lock: a.lock,
			force: a.force,
			dryRun: a.dryRun,
			client: process.env.PACA_CLIENT || "paca-cli",
			server: process.env.PACA_API_URL || "http://localhost:8080",
		};
		if (a.path) opts.path = a.path;
		if (a.note !== undefined) opts.note = a.note;
		if (a.task) opts.task = a.task;
		return runCommand((io) => (deps.push ?? runPush)(opts, docClient, io), {
			ok: a.dryRun ? "Dry run done; nothing was written." : `Pushed ${a.file}.`,
			conflict: `Did not push ${a.file}: Paca changed since the last sync, or the pulled file needs a pull first. Follow the guidance below (a command-line flag such as --force is the force argument, and pull is pull_docs), then push again.`,
			failed: "Push failed; nothing was written.",
		});
	}
	throw new Error(`Unknown tool: ${toolName}`);
}
