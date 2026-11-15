import { randomBytes } from "node:crypto";
import type { Dirent } from "node:fs";
import fs from "node:fs/promises";
import path from "node:path";
import type { PacaAPIDocClient } from "../api/doc-client.js";
import type { Document } from "../types/index.js";
import { blocknoteToMarkdown } from "../utils/index.js";
import {
	folderDirs,
	isInside,
	type MirrorDoc,
	type MirrorFolder,
	mirrorCandidates,
	mirrorFileFor,
	nameKey,
} from "./mirror-paths.js";
import {
	BASE_DIR,
	type CliIO,
	call,
	EXIT,
	fileSha256,
	PULL_STATE_FILE,
	type PullDocState,
	type PullState,
	sha256,
	TOO_OLD,
	WRITE_TMP_RE,
	writeFileAtomic,
	writeJSON,
} from "./state.js";

export interface PullOptions {
	projectId: string;
	/** The mirror folder. */
	dir: string;
	/** Overwrite mirror files that were edited locally. */
	force: boolean;
}

export type PullClient = Pick<PacaAPIDocClient, "getChanges" | "batchGet">;

const BATCH = 100;
/** Ids are used as file names (base copies), so only plain ids are taken. */
const SAFE_ID = /^[A-Za-z0-9][A-Za-z0-9-]{0,63}$/;
/** A file mid-move: `<mirror>/.paca-tmp-move-<doc id>`. */
const MOVE_TMP = ".paca-tmp-move-";
/** Held for the length of a pull, so two pulls never share a mirror. */
const LOCK_FILE = ".paca-pull.lock";
/** An empty or unreadable lock file this old is left by a crash. */
const UNREADABLE_LOCK_STALE_MS = 10_000;

/** Whether `name` (in the mirror root) is a move temp this command made. */
function isMoveTmp(name: string): boolean {
	return name.startsWith(MOVE_TMP) && SAFE_ID.test(name.slice(MOVE_TMP.length));
}

interface DocMeta extends MirrorDoc {
	/** Content hash the server reported most recently. */
	remote_hash: string | null;
	managed?: true;
}

/** One file moved this run, so a failure can put it back. */
interface Moved {
	/** Where the file was before this run. */
	orig: string;
	/** Where it is now. */
	cur: string;
	/** Its temporary name in the mirror root. */
	tmp: string;
}

/**
 * Mirrors the project's documents to Markdown files under `opts.dir`.
 * Returns the exit code: 0 done, 1 error (nothing changed), 2 a locally
 * edited file was kept. Throws on a failed file operation, after putting
 * moved files back (the state file is then left unchanged), and
 * UnreachableError when the server cannot be reached.
 */
export async function runPull(
	opts: PullOptions,
	client: PullClient,
	io: CliIO,
): Promise<number> {
	await fs.mkdir(path.resolve(opts.dir), { recursive: true });
	// Every path below is under the real path of the mirror folder.
	const root = await fs.realpath(path.resolve(opts.dir));

	const link = await findSymlink(root);
	if (link) {
		io.err(
			`refusing to pull: ${link} in the mirror folder is a symlink; pull never writes through symlinks, so remove it`,
		);
		return EXIT.error;
	}

	const token = await takeLock(root, opts.dir, io);
	if (!token) return EXIT.error;
	try {
		return await pullLocked(root, opts, client, io);
	} finally {
		await releaseLock(root, token);
	}
}

async function pullLocked(
	root: string,
	opts: PullOptions,
	client: PullClient,
	io: CliIO,
): Promise<number> {
	const statePath = path.join(root, PULL_STATE_FILE);
	const stateText = await readText(statePath);
	const prev: PullState | null = stateText ? JSON.parse(stateText) : null;
	if (prev && prev.project !== opts.projectId) {
		io.err(
			`the mirror folder ${opts.dir} belongs to project ${prev.project}; use another --dir for project ${opts.projectId}`,
		);
		return EXIT.error;
	}
	const prevDocs: Record<string, PullDocState> = {};
	for (const [id, d] of Object.entries(prev?.docs ?? {})) {
		if (
			SAFE_ID.test(id) &&
			d &&
			typeof d.file === "string" &&
			isInside(root, path.resolve(root, d.file))
		)
			prevDocs[id] = d;
		else
			io.err(
				`ignored a state entry with an unusable id or file: ${JSON.stringify(id)}`,
			);
	}

	await recoverMoves(root, prevDocs, io);
	await removeTempFiles(root);

	const feed = await call(() =>
		client.getChanges(opts.projectId, prev?.cursor || undefined),
	);
	if (feed.status === "unsupported") {
		io.err(TOO_OLD);
		return EXIT.error;
	}

	const folders: Record<string, MirrorFolder> = feed.folders
		? Object.fromEntries(
				feed.folders.map((f) => [
					f.id,
					{ parent_id: f.parent_id, name: f.name },
				]),
			)
		: (prev?.folders ?? {});

	// What the mirror should hold, starting from the last pull. Orphans (kept
	// files of gone documents) are not part of it unless the feed lists them.
	const meta = new Map<string, DocMeta>();
	for (const [id, d] of Object.entries(prevDocs)) {
		if (d.orphan) continue;
		meta.set(id, {
			id,
			title: d.title,
			folder_id: d.folder_id,
			remote_hash: d.remote_hash ?? d.content_hash,
			managed: d.managed,
		});
	}
	const removed = new Set<string>();
	if (feed.reset) {
		const listed = new Set(
			feed.docs.filter((d) => !d.deleted).map((d) => d.id),
		);
		for (const id of meta.keys()) if (!listed.has(id)) removed.add(id);
	}
	const listed: string[] = [];
	for (const c of feed.docs) {
		if (!SAFE_ID.test(c.id)) {
			io.err(`skipped a document with an unusable id: ${JSON.stringify(c.id)}`);
			continue;
		}
		if (c.deleted) {
			removed.add(c.id);
			continue;
		}
		removed.delete(c.id);
		listed.push(c.id);
		meta.set(c.id, {
			id: c.id,
			title: c.title,
			folder_id: c.folder_id,
			remote_hash: c.content_hash,
			managed: meta.get(c.id)?.managed,
		});
	}
	for (const id of removed) meta.delete(id);

	// Content is needed for docs that are new, changed, behind, or missing on
	// disk. Docs an incremental feed lists are fetched too, for their source
	// (a source or lock change bumps the feed but not the content hash). A
	// reset lists every doc, so it fetches only what changed.
	const needContent = new Set<string>();
	for (const [id, m] of meta) {
		const p = prevDocs[id];
		if (
			!p ||
			p.orphan ||
			m.remote_hash !== p.content_hash ||
			!(await exists(path.resolve(root, p.file)))
		)
			needContent.add(id);
	}
	const fetchIds = [...meta.keys()].filter(
		(id) => needContent.has(id) || (!feed.reset && listed.includes(id)),
	);
	const fetched = new Map<string, Document>();
	for (let i = 0; i < fetchIds.length; i += BATCH) {
		const chunk = fetchIds.slice(i, i + BATCH);
		const res = await call(() => client.batchGet(opts.projectId, chunk));
		if (res.status === "unsupported") {
			io.err(TOO_OLD);
			return EXIT.error;
		}
		for (const d of res.items) fetched.set(d.id, d);
	}
	for (const id of fetchIds) {
		const d = fetched.get(id);
		if (!d) {
			// Not readable any more (deleted or access lost since the feed).
			meta.delete(id);
			needContent.delete(id);
			removed.add(id);
			continue;
		}
		meta.set(id, {
			id,
			title: d.title,
			folder_id: d.folder_id ?? null,
			remote_hash: d.content_hash ?? meta.get(id)?.remote_hash ?? null,
			managed: d.source ? true : undefined,
		});
		if (d.content_hash && d.content_hash !== prevDocs[id]?.content_hash)
			needContent.add(id);
	}

	const lines: string[] = [];
	const next: Record<string, PullDocState> = {};
	let kept = 0;

	// Gone documents (and earlier orphans): an edited file is kept and
	// reported on every pull until it is removed or --force removes it.
	const toRemove: string[] = [];
	const goneIds = new Set(removed);
	for (const [id, p] of Object.entries(prevDocs))
		if (p.orphan && !meta.has(id)) goneIds.add(id);
	for (const id of goneIds) {
		const p = prevDocs[id];
		if (!p) continue;
		const sha = await fileSha256(path.resolve(root, p.file));
		if (sha === null) continue;
		if (sha !== p.file_sha256 && !opts.force) {
			kept++;
			next[id] = { ...p, orphan: true };
			lines.push(
				`kept ${p.file}: edited locally; the document was removed in Paca (delete the file, or pull --force to remove it)`,
			);
			continue;
		}
		toRemove.push(id);
	}

	// Layout. Names in use by files pull did not write are taken, and a
	// folder whose path is such a file gets another name.
	const owned = new Set<string>();
	for (const [id, p] of Object.entries(prevDocs))
		if (!next[id]?.orphan) owned.add(nameKey(p.file));
	const { files: foreignFiles, dirs: foreignDirs } = await scanMirror(
		root,
		owned,
	);
	const prevDirs: Record<string, string> = {};
	for (const [id, d] of Object.entries(prev?.dirs ?? {}))
		if (typeof d === "string") prevDirs[id] = d;
	const dirs = folderDirs(folders, foreignFiles, prevDirs);
	const taken = new Set([...foreignFiles, ...foreignDirs]);
	for (const d of Object.values(dirs)) if (d) taken.add(nameKey(d));
	const layout = new Map<string, string>();
	const ids = [...meta.keys()].sort();
	for (const id of ids) {
		const p = prevDocs[id];
		const m = meta.get(id) as DocMeta;
		if (
			p &&
			mirrorCandidates(m, folders, dirs).includes(p.file) &&
			!taken.has(nameKey(p.file))
		) {
			layout.set(id, p.file);
			taken.add(nameKey(p.file));
		}
	}
	for (const id of ids) {
		if (!layout.has(id))
			layout.set(
				id,
				mirrorFileFor(meta.get(id) as DocMeta, folders, taken, dirs),
			);
	}

	const vacated: string[] = [];
	const journal: Moved[] = [];
	/** Files this run created where nothing was before. */
	const created: string[] = [];
	let removedCount = 0;
	let moved = 0;
	let updated = 0;
	/** Whether the state file was rewritten mid-run (to restore on failure). */
	let stateChanged = false;
	try {
		// 1. Remove.
		for (const id of toRemove) {
			const p = prevDocs[id];
			const abs = await safePath(root, p.file);
			if (await exists(abs)) {
				await fs.rm(abs, { force: true });
				removedCount++;
				lines.push(`removed ${p.file}`);
			}
			vacated.push(abs);
		}
		for (const id of goneIds) {
			if (next[id]?.orphan) continue;
			await fs.rm(await safePath(root, `${BASE_DIR}/${id}.md`), {
				force: true,
			});
		}

		// 2. Move, through temporary names so two files can swap. A failure
		// puts every moved file back (catch below).
		const moves: Array<{ id: string; to: string; entry: Moved }> = [];
		for (const id of ids) {
			const p = prevDocs[id];
			const to = layout.get(id) as string;
			if (!p || p.orphan || p.file === to) continue;
			const from = await safePath(root, p.file);
			if (!(await exists(from))) continue;
			const tmp = await safePath(root, `${MOVE_TMP}${id}`);
			const entry = { orig: from, cur: from, tmp };
			journal.push(entry);
			await fs.rename(from, tmp);
			entry.cur = tmp;
			moves.push({ id, to, entry });
		}
		// Every moving file is now under its temporary name. Record the new
		// names before putting them there, with the old cursor and folders,
		// so a pull killed from here on leaves a state that matches the disk:
		// recoverMoves puts a file still under its temporary name at the new
		// name, and the next pull redoes the rest. Removed documents are
		// dropped, since a moved file may now sit at one's old name.
		if (moves.length > 0 && prev) {
			const docs: Record<string, PullDocState> = { ...prev.docs };
			for (const id of goneIds) if (!next[id]?.orphan) delete docs[id];
			for (const m of moves) docs[m.id] = { ...docs[m.id], file: m.to };
			await writeJSON(statePath, { ...prev, dirs, docs });
			stateChanged = true;
		}
		for (const m of moves) {
			const target = await safePath(root, m.to, { mkdir: true });
			if (await exists(target))
				throw new Error(
					`refusing to move over ${m.to}: a file is already there`,
				);
			await fs.rename(m.entry.cur, target);
			m.entry.cur = target;
			vacated.push(m.entry.orig);
			moved++;
			lines.push(`moved ${prevDocs[m.id].file} -> ${m.to}`);
		}

		// 3. Write new content; keep files edited since the last pull.
		for (const id of ids) {
			const m = meta.get(id) as DocMeta;
			const p = prevDocs[id];
			const file = layout.get(id) as string;
			const d = fetched.get(id);
			const base: Partial<PullDocState> = {
				file,
				title: m.title,
				folder_id: m.folder_id,
			};
			if (!d || !needContent.has(id)) {
				next[id] = { ...(p as PullDocState), ...base };
				delete next[id].orphan;
				if (m.managed) next[id].managed = true;
				else delete next[id].managed;
				continue;
			}
			const markdown = blocknoteToMarkdown(d.content as any[] | null);
			const mdSha = sha256(markdown);
			const abs = await safePath(root, file, { mkdir: true });
			const current = await fileSha256(abs);
			if (
				p &&
				current !== null &&
				current !== p.file_sha256 &&
				current !== mdSha &&
				!opts.force
			) {
				kept++;
				lines.push(`kept ${file}: edited locally; use --force to overwrite`);
				next[id] = { ...p, ...base, remote_hash: m.remote_hash };
				delete next[id].orphan;
				if (m.managed) next[id].managed = true;
				else delete next[id].managed;
				continue;
			}
			if (current !== mdSha) {
				if (current === null) created.push(abs);
				await writeFileAtomic(abs, markdown);
				updated++;
				lines.push(`updated ${file}`);
			}
			await writeFileAtomic(
				await safePath(root, `${BASE_DIR}/${id}.md`, { mkdir: true }),
				markdown,
			);
			next[id] = {
				file,
				title: m.title,
				folder_id: m.folder_id,
				content_hash: m.remote_hash,
				file_sha256: mdSha,
			};
			if (m.managed) next[id].managed = true;
		}
	} catch (err) {
		await rollBack(root, journal, created, async () => {
			if (!stateChanged) return;
			if (stateText === null) await fs.rm(statePath, { force: true });
			else await writeFileAtomic(statePath, stateText);
		});
		throw err;
	}

	// 4. Drop folders the moves and removals emptied.
	for (const f of vacated) await pruneEmpty(root, path.dirname(f));

	const state: PullState = {
		project: opts.projectId,
		cursor: feed.cursor,
		folders,
		dirs,
		docs: next,
	};
	const newText = `${JSON.stringify(state, null, 2)}\n`;
	if (newText !== stateText) await writeJSON(statePath, state);

	const parts: string[] = [];
	if (updated) parts.push(`${updated} updated`);
	if (moved) parts.push(`${moved} moved`);
	if (removedCount) parts.push(`${removedCount} removed`);
	if (kept) parts.push(`${kept} kept`);
	io.out(parts.length ? parts.join(", ") : "up to date");
	for (const l of lines) io.out(l);
	return kept ? EXIT.conflict : EXIT.ok;
}

/**
 * `rel` resolved under `root`, checked before any write, rename or delete:
 * it must stay inside `root` by its text, the nearest existing folder above
 * it must really be inside `root` (no symlinked folder on the way), and it
 * must not itself be a symlink. `mkdir` creates the parent folders first.
 */
async function safePath(
	root: string,
	rel: string,
	opts: { mkdir?: boolean } = {},
): Promise<string> {
	const abs = path.resolve(root, rel);
	if (!isInside(root, abs))
		throw new Error(`refusing a path outside the mirror folder: ${rel}`);
	if (opts.mkdir) await fs.mkdir(path.dirname(abs), { recursive: true });
	let anc = path.dirname(abs);
	while (!(await exists(anc))) anc = path.dirname(anc);
	const real = await fs.realpath(anc);
	if (real !== root && !isInside(root, real))
		throw new Error(
			`refusing a path that leaves the mirror folder through a symlink: ${rel}`,
		);
	try {
		if ((await fs.lstat(abs)).isSymbolicLink())
			throw new Error(`refusing to write through a symlink: ${rel}`);
	} catch (err) {
		const code = (err as NodeJS.ErrnoException).code;
		if (code !== "ENOENT" && code !== "ENOTDIR") throw err;
	}
	return abs;
}

/**
 * Undoes this run's moves after a failure: files it created are removed
 * (server content, fetched again next time), then every moved file goes to
 * its temporary name, then from there to where it was. Two phases, so swaps
 * and rename cycles come back too. A file that cannot be put back stays
 * under its temporary name, and the next pull recovers it. `restoreState`
 * runs between the phases: with every moved file under its temporary name,
 * the state file can go back to the old names without ever naming a path
 * the file is not at (recoverMoves follows the state either way).
 */
async function rollBack(
	root: string,
	journal: Moved[],
	created: string[],
	restoreState: () => Promise<void>,
): Promise<void> {
	for (const f of created) await fs.rm(f, { force: true }).catch(() => {});
	const back = journal.filter((m) => m.cur !== m.orig);
	for (const m of back) {
		if (m.cur === m.tmp) continue;
		try {
			const from = m.cur;
			await fs.rename(from, m.tmp);
			m.cur = m.tmp;
			await pruneEmpty(root, path.dirname(from));
		} catch {
			// Stays where it is.
		}
	}
	try {
		await restoreState();
	} catch {
		// The state still names the new paths: leave the files under their
		// temporary names, and the next pull puts them there.
		return;
	}
	for (const m of back) {
		if (m.cur !== m.tmp) continue;
		try {
			if (await exists(m.orig)) continue;
			await fs.mkdir(path.dirname(m.orig), { recursive: true });
			await fs.rename(m.tmp, m.orig);
			m.cur = m.orig;
		} catch {
			// Left for recoverMoves on the next run.
		}
	}
}

/**
 * Puts back files a crashed pull left mid-move (`.paca-tmp-move-<id>`) at
 * the path the state file still records, so the local-edit check applies
 * to them as before. One that cannot go back (unknown id, path taken) is
 * renamed to `recovered-<id>.md` and reported, never deleted.
 */
async function recoverMoves(
	root: string,
	docs: Record<string, PullDocState>,
	io: CliIO,
): Promise<void> {
	let entries: Dirent[];
	try {
		entries = await fs.readdir(root, { withFileTypes: true });
	} catch {
		return;
	}
	for (const e of entries) {
		if (!isMoveTmp(e.name) || !e.isFile()) continue;
		const id = e.name.slice(MOVE_TMP.length);
		const tmp = path.join(root, e.name);
		const p = SAFE_ID.test(id) ? docs[id] : undefined;
		if (p) {
			try {
				const target = await safePath(root, p.file, { mkdir: true });
				if (!(await exists(target))) {
					await fs.rename(tmp, target);
					io.err(`recovered ${p.file} from an interrupted pull`);
					continue;
				}
			} catch {
				// Its folder cannot be made (a file is in the way, say): fall
				// back to a name in the mirror root.
			}
		}
		const safeName = SAFE_ID.test(id) ? id : "file";
		let n = 0;
		let name = `recovered-${safeName}.md`;
		while (await exists(path.join(root, name)))
			name = `recovered-${safeName}-${++n}.md`;
		await fs.rename(tmp, path.join(root, name));
		io.err(`an interrupted pull left a file; it is now ${name}`);
	}
}

/** Removes temporary files of interrupted writes anywhere in the mirror
 *  (they hold server content that was never put in place). */
async function removeTempFiles(root: string): Promise<void> {
	const walk = async (abs: string) => {
		let entries: Dirent[];
		try {
			entries = await fs.readdir(abs, { withFileTypes: true });
		} catch {
			return;
		}
		for (const e of entries) {
			const p = path.join(abs, e.name);
			if (e.isDirectory()) await walk(p);
			else if (WRITE_TMP_RE.test(e.name)) await fs.rm(p, { force: true });
		}
	};
	await walk(root);
}

/** The first symlink inside the mirror folder (relative path), or null. */
async function findSymlink(root: string): Promise<string | null> {
	const walk = async (abs: string, rel: string): Promise<string | null> => {
		let entries: Dirent[];
		try {
			entries = await fs.readdir(abs, { withFileTypes: true });
		} catch {
			return null;
		}
		for (const e of entries) {
			const r = rel ? `${rel}/${e.name}` : e.name;
			if (e.isSymbolicLink()) return r;
			if (e.isDirectory()) {
				const found = await walk(path.join(abs, e.name), r);
				if (found) return found;
			}
		}
		return null;
	};
	return walk(root, "");
}

async function readText(file: string): Promise<string | null> {
	try {
		return await fs.readFile(file, "utf8");
	} catch (err) {
		if ((err as NodeJS.ErrnoException).code === "ENOENT") return null;
		throw err;
	}
}

async function exists(file: string): Promise<boolean> {
	try {
		await fs.lstat(file);
		return true;
	} catch {
		return false;
	}
}

/** Name keys of everything in the mirror folder that pull did not write:
 *  files (except `owned` ones) and folders. */
async function scanMirror(
	root: string,
	owned: Set<string>,
): Promise<{ files: Set<string>; dirs: Set<string> }> {
	const files = new Set<string>();
	const dirs = new Set<string>();
	const walk = async (abs: string, rel: string) => {
		let entries: Dirent[];
		try {
			entries = await fs.readdir(abs, { withFileTypes: true });
		} catch {
			return;
		}
		for (const e of entries) {
			const r = rel ? `${rel}/${e.name}` : e.name;
			if (
				!rel &&
				(e.name === BASE_DIR ||
					e.name === PULL_STATE_FILE ||
					e.name === LOCK_FILE ||
					isMoveTmp(e.name))
			)
				continue;
			if (WRITE_TMP_RE.test(e.name)) continue;
			if (e.isDirectory()) {
				dirs.add(nameKey(r));
				await walk(path.join(abs, e.name), r);
			} else if (!owned.has(nameKey(r))) {
				files.add(nameKey(r));
			}
		}
	};
	await walk(root, "");
	return { files, dirs };
}

/** Removes `from` and its parents while they are empty and inside `root`. */
async function pruneEmpty(root: string, from: string): Promise<void> {
	let cur = from;
	while (isInside(root, cur)) {
		try {
			await fs.rmdir(cur);
		} catch {
			return;
		}
		cur = path.dirname(cur);
	}
}

/** Whether a process with this id is running. */
function isRunning(pid: number): boolean {
	try {
		process.kill(pid, 0);
		return true;
	} catch (err) {
		return (err as NodeJS.ErrnoException).code === "EPERM";
	}
}

/**
 * Takes the mirror's lock file (created exclusively), holding this
 * process's id, start time and a token. A lock whose process is gone is
 * taken over and reported. Returns the token, or null when another pull
 * holds the lock (reported).
 */
async function takeLock(
	root: string,
	label: string,
	io: CliIO,
): Promise<string | null> {
	const file = path.join(root, LOCK_FILE);
	const token = randomBytes(8).toString("hex");
	for (let attempt = 0; attempt < 2; attempt++) {
		try {
			await fs.writeFile(
				file,
				JSON.stringify({
					pid: process.pid,
					started: new Date().toISOString(),
					token,
				}),
				{ flag: "wx" },
			);
			return token;
		} catch (err) {
			if ((err as NodeJS.ErrnoException).code !== "EEXIST") throw err;
		}
		let held: { pid?: unknown; started?: unknown } = {};
		try {
			held = JSON.parse(await fs.readFile(file, "utf8"));
		} catch {
			// Being written right now, or unreadable: treat it as held.
		}
		const pid = typeof held.pid === "number" ? held.pid : null;
		if (pid === null) {
			// Empty or unreadable: a pull may be writing it right now, or one
			// crashed between creating and writing it. Stale once old enough.
			let age = 0;
			try {
				age = Date.now() - (await fs.stat(file)).mtimeMs;
			} catch {
				continue; // Gone meanwhile: try to take it again.
			}
			if (age < UNREADABLE_LOCK_STALE_MS) {
				io.err(
					`another pull is running in ${label}; if it is not, remove ${path.join(label, LOCK_FILE)}`,
				);
				return null;
			}
			io.err(
				`took over a stale lock in ${label}: the lock file is empty or unreadable and ${Math.round(age / 1000)}s old`,
			);
		} else if (isRunning(pid)) {
			io.err(
				`another pull is running in ${label} (process ${pid}, started ${String(held.started)}); if it is not, remove ${path.join(label, LOCK_FILE)}`,
			);
			return null;
		} else {
			io.err(
				`took over a stale lock in ${label} from process ${pid} (started ${String(held.started)}), which is no longer running`,
			);
		}
		await fs.rm(file, { force: true });
	}
	io.err(`another pull is running in ${label}`);
	return null;
}

/** Removes the lock file if it is still the one this pull took. */
async function releaseLock(root: string, token: string): Promise<void> {
	const file = path.join(root, LOCK_FILE);
	try {
		const held = JSON.parse(await fs.readFile(file, "utf8"));
		if (held.token === token) await fs.rm(file, { force: true });
	} catch {
		// Already gone.
	}
}
