import { createHash } from "node:crypto";
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
		if ((err as NodeJS.ErrnoException).code === "ENOENT") return null;
		throw err;
	}
}

/** Writes a file through a temporary file in the same folder and a rename,
 *  so a reader never sees half of it. */
export async function writeFileAtomic(
	file: string,
	data: string,
): Promise<void> {
	await fs.mkdir(path.dirname(file), { recursive: true });
	const tmp = path.join(
		path.dirname(file),
		`.paca-tmp-${process.pid}-${Math.random().toString(36).slice(2)}`,
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
}

export interface PullState {
	project: string;
	cursor: string;
	folders: Record<string, { parent_id: string | null; name: string }>;
	docs: Record<string, PullDocState>;
}

export const PULL_STATE_FILE = ".paca-sync.json";
export const BASE_DIR = ".paca-base";

// ==================== push state ====================

export interface PushFileState {
	doc_path: string;
	doc_id?: string;
	file_sha256: string;
	etag?: string;
}

/** Keyed by the repository-relative file path. */
export type PushState = Record<string, PushFileState>;
