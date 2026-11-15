import { createHash, randomBytes } from "node:crypto";
import fs from "node:fs/promises";
import path from "node:path";

/** Output for a command: one line at a time. */
export interface CliIO {
	out(line: string): void;
	err(line: string): void;
}

/** Exit codes shared by pull and push. */
export const EXIT = {
	ok: 0,
	error: 1,
	conflict: 2,
	unreachable: 3,
} as const;

export const TOO_OLD = "this Paca server is too old for pull/push";

/** Thrown when the Paca server cannot be reached (exit 3). */
export class UnreachableError extends Error {
	constructor(cause: unknown) {
		super(
			`cannot reach the Paca server: ${cause instanceof Error ? (cause.cause instanceof Error ? cause.cause.message : cause.message) : String(cause)}`,
		);
		this.name = "UnreachableError";
	}
}

/** Runs one API call; a network failure (fetch could not connect) becomes
 *  an UnreachableError. Error answers and other errors pass through. */
export async function call<T>(fn: () => Promise<T>): Promise<T> {
	try {
		return await fn();
	} catch (err) {
		if (err instanceof TypeError && /fetch failed/i.test(err.message))
			throw new UnreachableError(err);
		throw err;
	}
}

export function sha256(text: string | Buffer): string {
	return createHash("sha256").update(text).digest("hex");
}

/** The sha256 of a file, or null when there is none. */
export async function fileSha256(file: string): Promise<string | null> {
	try {
		return sha256(await fs.readFile(file));
	} catch (err) {
		// ENOTDIR: a folder on the way is now a file, so there is no file.
		const code = (err as NodeJS.ErrnoException).code;
		if (code === "ENOENT" || code === "ENOTDIR") return null;
		throw err;
	}
}

/** Temporary file of an atomic write: this prefix and 16 hex digits. */
export const WRITE_TMP_PREFIX = ".paca-tmp-w-";
export const WRITE_TMP_RE = /^\.paca-tmp-w-[0-9a-f]{16}$/;

/** Writes a file through a temporary file in the same folder and a rename,
 *  so a reader never sees half of it. */
export async function writeFileAtomic(
	file: string,
	data: string,
): Promise<void> {
	await fs.mkdir(path.dirname(file), { recursive: true });
	const tmp = path.join(
		path.dirname(file),
		`${WRITE_TMP_PREFIX}${randomBytes(8).toString("hex")}`,
	);
	try {
		await fs.writeFile(tmp, data);
		await fs.rename(tmp, file);
	} catch (err) {
		await fs.rm(tmp, { force: true });
		throw err;
	}
}

/** Reads a JSON file, or returns null when it does not exist. */
export async function readJSON<T>(file: string): Promise<T | null> {
	let text: string;
	try {
		text = await fs.readFile(file, "utf8");
	} catch (err) {
		if ((err as NodeJS.ErrnoException).code === "ENOENT") return null;
		throw err;
	}
	return JSON.parse(text) as T;
}

export async function writeJSON(file: string, value: unknown): Promise<void> {
	await writeFileAtomic(file, `${JSON.stringify(value, null, 2)}\n`);
}

// ==================== pull state ====================

export interface PullDocState {
	/** Mirror file, relative to the mirror folder, "/" separated. */
	file: string;
	title: string;
	folder_id: string | null;
	/** Server content hash of what the file and base copy hold. */
	content_hash: string | null;
	/** Set when the server has newer content that was not written (the file
	 *  was edited locally); the next pull fetches it again. */
	remote_hash?: string | null;
	/** sha256 of the file as pull wrote it. */
	file_sha256: string;
	/** The document is a managed copy of a repository file. */
	managed?: true;
	/** The document is gone (deleted or no longer readable) but its file
	 *  was edited locally, so it was kept; reported on every pull until the
	 *  file is removed or `--force` removes it. */
	orphan?: true;
	/** sha256 of Markdown pulls that stopped part way wrote, or were about
	 *  to write, to `file`: a file with one of these hashes came from pull,
	 *  not a local edit. Only hashes of Markdown the server sent. Says
	 *  nothing about what the file holds; at most two (the one on disk and
	 *  the one being written), see `settlePending`. */
	pending_sha256?: string[];
	/** The name was taken by a pull that stopped before it recorded this
	 *  document's content: a file there came from that pull (the name was
	 *  free when it was taken), so the next pull overwrites it. The content
	 *  fields say nothing then. */
	reserved?: true;
}

export interface PullState {
	project: string;
	cursor: string;
	folders: Record<string, { parent_id: string | null; name: string }>;
	/** The mirror directory of each folder, so a folder keeps its name when
	 *  another one with the same name key appears. */
	dirs?: Record<string, string>;
	docs: Record<string, PullDocState>;
}

export const PULL_STATE_FILE = ".paca-sync.json";
export const BASE_DIR = ".paca-base";

// ==================== push state ====================

export interface PushFileState {
	/** Project and server the entry was recorded for; an entry for another
	 *  one is ignored. */
	project: string;
	server: string;
	doc_path: string;
	doc_id?: string;
	file_sha256: string;
	etag?: string;
}

/** Keyed by the repository-relative file path. */
export type PushState = Record<string, PushFileState>;
