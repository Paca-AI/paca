import fs from "node:fs/promises";
import path from "node:path";
import type { PacaAPIDocClient } from "../api/doc-client.js";
import type { Document } from "../types/index.js";
import { blocknoteToMarkdown } from "../utils/index.js";
import {
	folderPathFor,
	isInside,
	type MirrorDoc,
	type MirrorFolder,
	mirrorCandidates,
	mirrorFileFor,
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

interface DocMeta extends MirrorDoc {
	/** Content hash the server reported most recently. */
	remote_hash: string | null;
	managed?: true;
}

/**
 * Mirrors the project's documents to Markdown files under `opts.dir`.
 * Returns the exit code: 0 done, 1 error, 2 a locally edited file was kept.
 * Throws on a failed file write (the state file is then left unchanged) and
 * UnreachableError when the server cannot be reached.
 */
export async function runPull(
	opts: PullOptions,
	client: PullClient,
	io: CliIO,
): Promise<number> {
	const dir = path.resolve(opts.dir);
	const statePath = path.join(dir, PULL_STATE_FILE);
	const stateText = await readText(statePath);
	const prev: PullState | null = stateText ? JSON.parse(stateText) : null;
	if (prev && prev.project !== opts.projectId) {
		io.err(
			`the mirror folder ${opts.dir} belongs to project ${prev.project}; use another --dir for project ${opts.projectId}`,
		);
		return EXIT.error;
	}
	const prevDocs: Record<string, PullDocState> = prev?.docs ?? {};

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

	// What the mirror should hold, starting from the last pull.
	const meta = new Map<string, DocMeta>();
	for (const [id, d] of Object.entries(prevDocs)) {
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
		meta.set(c.id, {
			id: c.id,
			title: c.title,
			folder_id: c.folder_id,
			remote_hash: c.content_hash,
			managed: meta.get(c.id)?.managed,
		});
	}
	for (const id of removed) meta.delete(id);

	// Fetch what is new, changed, behind, or missing on disk.
	const fetchIds: string[] = [];
	for (const [id, m] of meta) {
		const p = prevDocs[id];
		if (
			!p ||
			m.remote_hash !== p.content_hash ||
			!(await exists(resolveIn(dir, p.file)))
		)
			fetchIds.push(id);
	}
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
	}

	const lines: string[] = [];
	let kept = 0;

	// Removals: a file edited since the last pull is kept (unless --force).
	const toRemove: string[] = [];
	const keptFiles: string[] = [];
	for (const id of removed) {
		const p = prevDocs[id];
		if (!p) continue;
		const abs = resolveIn(dir, p.file);
		const sha = await fileSha256(abs);
		if (sha !== null && sha !== p.file_sha256 && !opts.force) {
			kept++;
			keptFiles.push(p.file);
			lines.push(
				`kept ${p.file}: edited locally; the document was removed in Paca`,
			);
			continue;
		}
		toRemove.push(id);
	}

	// Layout: names already in use by files that are not ours, and folders.
	const owned = new Set(
		Object.values(prevDocs).map((d) => d.file.toLowerCase()),
	);
	for (const f of keptFiles) owned.delete(f.toLowerCase());
	const taken = await foreignPaths(dir, owned);
	for (const id of Object.keys(folders)) {
		const p = folderPathFor(id, folders);
		if (p) taken.add(p.toLowerCase());
	}
	const layout = new Map<string, string>();
	const ids = [...meta.keys()].sort();
	for (const id of ids) {
		const p = prevDocs[id];
		const m = meta.get(id) as DocMeta;
		if (
			p &&
			mirrorCandidates(m, folders).includes(p.file) &&
			!taken.has(p.file.toLowerCase())
		) {
			layout.set(id, p.file);
			taken.add(p.file.toLowerCase());
		}
	}
	for (const id of ids) {
		if (!layout.has(id))
			layout.set(id, mirrorFileFor(meta.get(id) as DocMeta, folders, taken));
	}

	const vacated: string[] = [];
	const next: Record<string, PullDocState> = {};
	let removedCount = 0;
	let moved = 0;
	let updated = 0;

	// 1. Remove.
	for (const id of toRemove) {
		const p = prevDocs[id];
		const abs = resolveIn(dir, p.file);
		if (await exists(abs)) {
			await fs.rm(abs, { force: true });
			removedCount++;
			lines.push(`removed ${p.file}`);
		}
		await fs.rm(resolveIn(dir, `${BASE_DIR}/${id}.md`), { force: true });
		vacated.push(abs);
	}
	for (const id of removed) {
		if (!toRemove.includes(id))
			await fs.rm(resolveIn(dir, `${BASE_DIR}/${id}.md`), { force: true });
	}

	// 2. Move, through temporary names so two files can swap.
	const moves: Array<{ id: string; from: string; to: string; tmp: string }> =
		[];
	for (const id of ids) {
		const p = prevDocs[id];
		const to = layout.get(id) as string;
		if (!p || p.file === to) continue;
		const from = resolveIn(dir, p.file);
		if (!(await exists(from))) continue;
		moves.push({ id, from, to, tmp: resolveIn(dir, `.paca-tmp-move-${id}`) });
	}
	for (const m of moves) await fs.rename(m.from, m.tmp);
	for (const m of moves) {
		const target = resolveIn(dir, m.to);
		await fs.mkdir(path.dirname(target), { recursive: true });
		await fs.rename(m.tmp, target);
		vacated.push(m.from);
		moved++;
		lines.push(`moved ${prevDocs[m.id].file} -> ${m.to}`);
	}

	// 3. Write fetched content; keep files edited since the last pull.
	for (const id of ids) {
		const m = meta.get(id) as DocMeta;
		const p = prevDocs[id];
		const file = layout.get(id) as string;
		const d = fetched.get(id);
		if (!d) {
			next[id] = {
				...(p as PullDocState),
				file,
				title: m.title,
				folder_id: m.folder_id,
			};
			if (m.managed) next[id].managed = true;
			else delete next[id].managed;
			continue;
		}
		const markdown = blocknoteToMarkdown(d.content as any[] | null);
		const mdSha = sha256(markdown);
		const abs = resolveIn(dir, file);
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
			next[id] = {
				...p,
				file,
				title: m.title,
				folder_id: m.folder_id,
				remote_hash: m.remote_hash,
			};
			if (m.managed) next[id].managed = true;
			else delete next[id].managed;
			continue;
		}
		if (current !== mdSha) {
			await writeFileAtomic(abs, markdown);
			updated++;
			lines.push(`updated ${file}`);
		}
		await writeFileAtomic(resolveIn(dir, `${BASE_DIR}/${id}.md`), markdown);
		next[id] = {
			file,
			title: m.title,
			folder_id: m.folder_id,
			content_hash: m.remote_hash,
			file_sha256: mdSha,
		};
		if (m.managed) next[id].managed = true;
	}

	// 4. Drop folders the moves and removals emptied.
	for (const f of vacated) await pruneEmpty(dir, path.dirname(f));

	const state: PullState = {
		project: opts.projectId,
		cursor: feed.cursor,
		folders,
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

/** `rel` resolved under `dir`; throws if it would land outside it. */
function resolveIn(dir: string, rel: string): string {
	const abs = path.resolve(dir, rel);
	if (!isInside(dir, abs))
		throw new Error(`refusing a path outside the mirror folder: ${rel}`);
	return abs;
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

/** Lower-cased relative paths of everything in the mirror folder that pull
 *  did not write (files and folders), so pull never writes over them. */
async function foreignPaths(
	dir: string,
	owned: Set<string>,
): Promise<Set<string>> {
	const out = new Set<string>();
	const walk = async (abs: string, rel: string) => {
		let entries: import("node:fs").Dirent[];
		try {
			entries = await fs.readdir(abs, { withFileTypes: true });
		} catch {
			return;
		}
		for (const e of entries) {
			const r = rel ? `${rel}/${e.name}` : e.name;
			if (!rel && (e.name === BASE_DIR || e.name === PULL_STATE_FILE)) continue;
			if (e.name.startsWith(".paca-tmp-")) continue;
			if (e.isDirectory()) {
				out.add(r.toLowerCase());
				await walk(path.join(abs, e.name), r);
			} else if (!owned.has(r.toLowerCase())) {
				out.add(r.toLowerCase());
			}
		}
	};
	await walk(dir, "");
	return out;
}

/** Removes `from` and its parents while they are empty and inside `dir`. */
async function pruneEmpty(dir: string, from: string): Promise<void> {
	let cur = from;
	while (isInside(dir, cur)) {
		try {
			await fs.rmdir(cur);
		} catch {
			return;
		}
		cur = path.dirname(cur);
	}
}
