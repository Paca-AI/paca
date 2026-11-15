import { parseArgs } from "node:util";
import { PacaAPIDocClient, PacaAPIError } from "../api/doc-client.js";
import { type PullClient, type PullOptions, runPull } from "./pull.js";
import { type PushClient, type PushOptions, runPush } from "./push.js";
import { type CliIO, EXIT, UnreachableError } from "./state.js";

export const USAGE = `Usage:
  npx @paca-ai/paca-mcp                 start the MCP server (stdio)
  npx @paca-ai/paca-mcp pull [--project <id>] [--dir .paca/docs] [--force]
      Mirror the project's documents to Markdown files in --dir.
      --force overwrites mirror files that were edited locally.
  npx @paca-ai/paca-mcp push <file> [--path "Folder/Title"] [--project <id>]
      [--note "..."] [--task PACA-88] [--lock] [--force] [--dry-run]
      Write a repository file to the document at --path (default
      Plans/<file name>, or Specifications/<name> under a specs folder).
      A file in a pull mirror is pushed back to the document it was
      pulled from, as an edit of only the blocks it changes (--path,
      --lock and --force do not apply; on a conflict, pull, then push).

Environment: PACA_API_KEY (required), PACA_API_URL (default
http://localhost:8080), PACA_PROJECT_ID (or --project), PACA_CLIENT (client
label sent with writes, default paca-cli).

Exit codes: 0 done or unchanged, 1 error, 2 conflict (the Paca copy changed,
or a mirror file was edited locally), 3 server unreachable.

Removing a mirrored file does not revoke copies already made of it.`;

/** The commands `index.ts` hands to `runCli` instead of starting the server. */
export const CLI_COMMANDS = ["pull", "push"] as const;

export interface CliDeps {
	pull?: (opts: PullOptions, client: PullClient, io: CliIO) => Promise<number>;
	push?: (opts: PushOptions, client: PushClient, io: CliIO) => Promise<number>;
}

/** Runs `pull` or `push` (argv starts with the command) and returns the
 *  exit code. Never calls process.exit. */
export async function runCli(
	argv: string[],
	env: NodeJS.ProcessEnv,
	io: CliIO,
	deps: CliDeps = {},
): Promise<number> {
	const [command, ...rest] = argv;
	if (rest.includes("--help") || rest.includes("-h")) {
		io.out(USAGE);
		return EXIT.ok;
	}
	let parsed: ReturnType<typeof parse>;
	try {
		parsed = parse(command, rest);
	} catch (err) {
		io.err((err as Error).message);
		io.err(USAGE);
		return EXIT.error;
	}
	const { values, positionals } = parsed;

	const apiKey = env.PACA_API_KEY;
	if (!apiKey) {
		io.err("PACA_API_KEY is required (your Paca API key).");
		return EXIT.error;
	}
	const projectId = values.project || env.PACA_PROJECT_ID;
	if (!projectId) {
		io.err("Set PACA_PROJECT_ID or pass --project <id>.");
		return EXIT.error;
	}
	const baseURL = env.PACA_API_URL || "http://localhost:8080";
	const client = new PacaAPIDocClient({ apiKey, baseURL });

	try {
		if (command === "pull") {
			if (positionals.length) {
				io.err(`pull takes no file: ${positionals.join(" ")}`);
				return EXIT.error;
			}
			return await (deps.pull ?? runPull)(
				{
					projectId,
					dir: values.dir || ".paca/docs",
					force: values.force === true,
				},
				client,
				io,
			);
		}
		if (command === "push") {
			const [file, ...extra] = positionals;
			if (!file) {
				io.err("push needs a file: push <file> [options]");
				return EXIT.error;
			}
			if (extra.length) {
				io.err(`push takes one file; also got: ${extra.join(" ")}`);
				return EXIT.error;
			}
			const opts: PushOptions = {
				projectId,
				file,
				lock: values.lock === true,
				force: values.force === true,
				dryRun: values["dry-run"] === true,
				client: env.PACA_CLIENT || "paca-cli",
				server: baseURL,
			};
			if (values.path) opts.path = values.path;
			if (values.note !== undefined) opts.note = values.note;
			if (values.task) opts.task = values.task;
			return await (deps.push ?? runPush)(opts, client, io);
		}
		io.err(`unknown command: ${command}`);
		io.err(USAGE);
		return EXIT.error;
	} catch (err) {
		if (err instanceof UnreachableError) {
			io.err(err.message);
			return EXIT.unreachable;
		}
		io.err(
			err instanceof PacaAPIError
				? err.message
				: String((err as Error)?.message ?? err),
		);
		return EXIT.error;
	}
}

const COMMON = {
	project: { type: "string" },
	force: { type: "boolean" },
} as const;
const PUSH_ONLY = {
	path: { type: "string" },
	note: { type: "string" },
	task: { type: "string" },
	lock: { type: "boolean" },
	"dry-run": { type: "boolean" },
} as const;

function parse(command: string | undefined, args: string[]) {
	const { values, positionals } = parseArgs({
		args,
		allowPositionals: true,
		strict: true,
		options:
			command === "push"
				? { ...COMMON, ...PUSH_ONLY }
				: { ...COMMON, dir: { type: "string" } },
	});
	return {
		values: values as {
			project?: string;
			force?: boolean;
			dir?: string;
			path?: string;
			note?: string;
			task?: string;
			lock?: boolean;
			"dry-run"?: boolean;
		},
		positionals,
	};
}
