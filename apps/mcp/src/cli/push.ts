import fs from "node:fs/promises";
import path from "node:path";
import {
	type PacaAPIDocClient,
	PacaAPIError,
	type PutByPathInput,
	type PutByPathResult,
} from "../api/doc-client.js";
import type { GitExec } from "../git/git-runner.js";
import { blocknoteToMarkdown, markdownToBlocknote } from "../utils/index.js";
import { unifiedDiff } from "./diff.js";
import { gitSource } from "./git-source.js";
import {
	type CliIO,
	call,
	EXIT,
	type PushState,
	readJSON,
	sha256,
	TOO_OLD,
	writeFileAtomic,
	writeJSON,
} from "./state.js";

export interface PushOptions {
	projectId: string;
	/** The repository file to push. */
	file: string;
	/** The document path ("Folder/Title"); defaults to the recorded one, then
	 *  Plans/<name> (Specifications/<name> under a specs folder). */
	path?: string;
	note?: string;
	task?: string;
	lock: boolean;
	force: boolean;
	dryRun: boolean;
	/** Client label sent with the write. */
	client: string;
	/** The Paca server URL; recorded so a push to another server does not
	 *  reuse this one's state. */
	server?: string;
}

export type PushClient = Pick<PacaAPIDocClient, "putByPath" | "getByPath">;

const NOTE_MAX = 500;

/** The document path a file is pushed to when nothing else says. */
export function defaultDocPath(relPath: string): string {
	const parts = relPath.split("/");
	const name = (parts.pop() as string).replace(/\.md$/i, "");
	return parts.includes("specs") ? `Specifications/${name}` : `Plans/${name}`;
}

/**
 * The key two document paths are compared by, the way the server resolves
 * them: split on "/", segments trimmed, empty ones dropped, compared without
 * case. JavaScript's toLowerCase and Postgres's LOWER differ for a few
 * non-ASCII characters; the document id check after a write covers those.
 */
export function docPathKey(docPath: string): string {
	return docPath
		.split("/")
		.map((s) => s.trim())
		.filter(Boolean)
		.join("/")
		.toLowerCase();
}

/**
 * Writes a repository file to its Paca document by path. Returns the exit
 * code: 0 written or unchanged, 1 error, 2 conflict. Throws UnreachableError
 * when the server cannot be reached (nothing is recorded then).
 */
export async function runPush(
	opts: PushOptions,
	client: PushClient,
	io: CliIO,
	deps: { cwd?: string; git?: GitExec } = {},
): Promise<number> {
	const cwd = deps.cwd ?? process.cwd();
	const abs = path.resolve(cwd, opts.file);
	let text: string;
	try {
		text = await fs.readFile(abs, "utf8");
	} catch (err) {
		io.err(`cannot read ${opts.file}: ${(err as Error).message}`);
		return EXIT.error;
	}
	let src: Awaited<ReturnType<typeof gitSource>>;
	try {
		src = await gitSource(abs, cwd, deps.git);
	} catch (err) {
		io.err((err as Error).message);
		return EXIT.error;
	}
	const rel = src.relPath;
	const statePath = path.join(src.root, ".paca", "push.json");
	const server = opts.server ?? "";
	const baseFor = (file: string) =>
		path.join(
			src.root,
			".paca",
			"push-base",
			`${sha256(`${server}\n${opts.projectId}\n${file}`)}.md`,
		);
	const basePath = baseFor(rel);
	const state: PushState = (await readJSON<PushState>(statePath)) ?? {};
	// An entry recorded for another project or server says nothing here.
	const recorded = state[rel];
	const entry =
		recorded &&
		recorded.project === opts.projectId &&
		(recorded.server ?? "") === server
			? recorded
			: undefined;
	const fileSha = sha256(text);

	if (
		entry &&
		entry.file_sha256 === fileSha &&
		!opts.force &&
		!opts.lock &&
		(!opts.path || docPathKey(opts.path) === docPathKey(entry.doc_path))
	) {
		io.out("unchanged");
		return EXIT.ok;
	}

	const docPath = opts.path || entry?.doc_path || defaultDocPath(rel);
	// Two files pushed to one document would overwrite each other. --force
	// overwrites the Paca copy; it does not make two files share a document.
	// The path is recorded as typed (messages show it as the user wrote it)
	// and compared by its key.
	const key = docPathKey(docPath);
	const sameTarget = (e: PushState[string]) =>
		e.project === opts.projectId && (e.server ?? "") === server;
	const owners = Object.entries(state)
		.filter(
			([file, e]) =>
				file !== rel && sameTarget(e) && docPathKey(e.doc_path) === key,
		)
		.map(([file]) => file)
		.sort();
	// Recorded files that are gone were moved (git mv): this file takes over
	// their entries, so If-Match still catches a change made in Paca. So does
	// a recorded name that is this same file (a move that changed only case,
	// on a case-insensitive file system). Any other file still there blocks.
	const gone = new Set<string>();
	for (const file of owners) {
		const other = path.join(src.root, file);
		if (!(await exists(other))) gone.add(file);
		else if (!(await sameFile(other, abs))) {
			io.err(
				`${docPath} is already pushed from ${file}; pass --path "Folder/Title" to push ${rel} to another document`,
			);
			return EXIT.error;
		}
	}
	// The first by name gives If-Match: if two entries claim the document, an
	// older ETag can only end in a conflict, never in an overwrite.
	const takeover: string | undefined = owners[0];
	if (takeover) {
		io.out(
			gone.has(takeover)
				? `${rel} takes over ${docPath} from ${takeover}, which no longer exists`
				: `${rel} takes over ${docPath} from ${takeover}, the same file under its old name`,
		);
		if (owners.length > 1)
			io.out(
				`  also drops the stale entries for ${owners.slice(1).join(", ")}`,
			);
	}
	const prior = takeover ? state[takeover] : entry;
	const ifMatch =
		!opts.force && prior?.etag && docPathKey(prior.doc_path) === key
			? prior.etag
			: undefined;
	const note =
		opts.note ??
		(src.commit && src.lastSubject
			? src.lastSubject.slice(0, NOTE_MAX)
			: undefined);
	const input: PutByPathInput = {
		path: docPath,
		content: [],
		source: {
			repository: src.repository,
			path: rel,
			...(src.commit ? { commit: src.commit } : {}),
		},
	};
	if (opts.lock) input.lock = true;
	if (note) input.note = note;
	if (opts.client) input.client = opts.client.slice(0, 200);
	if (opts.task) input.task = opts.task;

	if (opts.dryRun) {
		io.out(`would push ${rel} to ${docPath} (nothing sent)`);
		io.out(
			`  source: ${src.repository} ${rel}${src.commit ? ` @ ${src.commit.slice(0, 12)}` : " (uncommitted)"}`,
		);
		io.out(`  If-Match: ${ifMatch ?? "none"}`);
		if (opts.lock) io.out("  lock: yes");
		if (note) io.out(`  note: ${note}`);
		if (opts.task) io.out(`  task: ${opts.task}`);
		return EXIT.ok;
	}

	input.content = markdownToBlocknote(text);
	// The base copy is kept as the converter writes it, so the conflict diff
	// against the Paca copy (also converted) shows only real changes.
	const canonical = blocknoteToMarkdown(input.content);

	let res: PutByPathResult | undefined;
	for (let attempt = 1; !res; attempt++) {
		try {
			res = await call(() =>
				client.putByPath(opts.projectId, input, ifMatch ? { ifMatch } : {}),
			);
		} catch (err) {
			if (!(err instanceof PacaAPIError)) throw err;
			if (err.code === "DOC_PATH_CHANGED" && attempt < 2) continue;
			return reportWriteError(err, {
				opts,
				client,
				io,
				docPath,
				rel,
				basePath: takeover ? baseFor(takeover) : basePath,
				text: canonical,
			});
		}
	}
	if (res.status === "unsupported") {
		io.err(TOO_OLD);
		return EXIT.error;
	}

	// The server matched a document another file records under a path that
	// compares differently here: do not record a second entry for it.
	const twin = Object.entries(state).find(
		([file, e]) =>
			file !== rel &&
			!owners.includes(file) &&
			sameTarget(e) &&
			e.doc_id === res.doc.id,
	);
	if (twin) {
		io.err(
			`${docPath} was written, but Paca resolved it to the document ${twin[0]} is pushed to (${twin[1].doc_path}); nothing was recorded. Push ${twin[0]} again to put its content back, and pass --path "Folder/Title" to push ${rel} to another document`,
		);
		return EXIT.error;
	}

	await writeFileAtomic(basePath, canonical);
	for (const file of owners) delete state[file];
	state[rel] = {
		project: opts.projectId,
		server,
		doc_path: docPath,
		doc_id: res.doc.id,
		file_sha256: fileSha,
		etag: res.etag ?? res.doc.content_hash,
	};
	await writeJSON(statePath, state);
	// Only once the entry has moved: until then a rerun takes over again and
	// needs the old base copy for its conflict diff. A copy left behind is
	// never read again.
	for (const file of owners)
		await fs.rm(baseFor(file), { force: true }).catch(() => {});

	if (
		res.created &&
		prior?.doc_id &&
		docPathKey(prior.doc_path) === key &&
		prior.doc_id !== res.doc.id
	)
		io.err(
			`warning: the document pushed before (${prior.doc_id}) is gone from ${prior.doc_path}; created a new one`,
		);
	if (!res.changed)
		io.out(
			opts.lock && res.doc.source_locked
				? `unchanged content; ${docPath} is locked`
				: "unchanged",
		);
	else io.out(`${res.created ? "created" : "updated"} ${docPath}`);
	return EXIT.ok;
}

async function reportWriteError(
	err: PacaAPIError,
	ctx: {
		opts: PushOptions;
		client: PushClient;
		io: CliIO;
		docPath: string;
		rel: string;
		basePath: string;
		text: string;
	},
): Promise<number> {
	const { opts, client, io, docPath } = ctx;
	if (err.status === 412) {
		const d = err.details;
		if (!d || Object.keys(d).length === 0) {
			io.err(
				`conflict: the document at ${docPath} is gone or was moved since the last push`,
			);
			io.err("use --force to create it again");
			return EXIT.conflict;
		}
		io.err("conflict: the Paca copy changed since the last push");
		io.err(
			`  written by ${d.updated_by || "someone"}${d.updated_at ? ` at ${d.updated_at}` : ""}`,
		);
		if (d.note) io.err(`  note: ${d.note}`);
		if (d.client) io.err(`  client: ${d.client}`);
		if (d.task) io.err(`  task: ${d.task}`);
		// Any failure here (even an unreachable server) still ends as a
		// conflict, just without the diff.
		const paca = await call(() =>
			client.getByPath(opts.projectId, docPath),
		).catch(() => null);
		if (paca?.status === "found") {
			const pacaText = blocknoteToMarkdown(paca.doc.content as any[] | null);
			let base: string | null = null;
			try {
				base = await fs.readFile(ctx.basePath, "utf8");
			} catch {
				// No copy of the last push kept here.
			}
			if (base === null)
				io.err(
					`  (the last pushed version is not kept here; showing ${ctx.rel} against the Paca copy)`,
				);
			const diff = unifiedDiff(
				base ?? ctx.text,
				pacaText,
				base === null ? ctx.rel : "last push",
				"Paca copy",
			);
			for (const line of diff.trimEnd().split("\n")) if (line) io.err(line);
		} else {
			io.err("  (could not read the Paca copy to show what changed)");
		}
		io.err(
			`merge that change into ${opts.file} and push again, or use --force to overwrite`,
		);
		return EXIT.conflict;
	}
	if (err.code === "DOC_PATH_CHANGED") {
		io.err(
			`the document path ${docPath} kept changing during the push; try again`,
		);
		return EXIT.error;
	}
	if (err.code === "DOC_LOCKED") {
		const cur = await call(() =>
			client.getByPath(opts.projectId, docPath),
		).catch(() => null);
		const s = cur?.status === "found" ? cur.doc.source : null;
		io.err(
			s
				? `${docPath} is a managed copy locked to ${s.repository} (${s.path}); push it from that repository, or unlock it in Paca`
				: `${docPath} is locked to another repository; unlock it in Paca to push from here`,
		);
		return EXIT.error;
	}
	if (err.code === "DOC_PATH_IS_FOLDER") {
		io.err(`${docPath} is a folder in Paca; pass --path "Folder/Title"`);
		return EXIT.error;
	}
	io.err(err.message);
	return EXIT.error;
}

async function exists(file: string): Promise<boolean> {
	try {
		await fs.lstat(file);
		return true;
	} catch {
		return false;
	}
}

/** Whether two paths name one file (their real paths are equal). */
async function sameFile(a: string, b: string): Promise<boolean> {
	try {
		return (await fs.realpath(a)) === (await fs.realpath(b));
	} catch {
		return false;
	}
}
