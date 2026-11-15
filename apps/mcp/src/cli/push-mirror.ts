// `push` of a file that `pull` wrote: the edited file becomes an edit of the
// document it was pulled from. Only the blocks the edit reaches change; the
// rest are sent as they were read, so their stored JSON stays the same.

import fs from "node:fs/promises";
import path from "node:path";
import {
	type PacaAPIDocClient,
	PacaAPIError,
	type PatchDocumentInput,
} from "../api/doc-client.js";
import { type Block, SpliceError, spliceMarkdown } from "../tools/doc-edit.js";
import type { DocSource } from "../types/index.js";
import { blocknoteToMarkdown } from "../utils/index.js";
import { nameKey } from "./mirror-paths.js";
import { releaseLock, takeLock } from "./pull.js";
import type { PushOptions } from "./push.js";
import {
	BASE_DIR,
	type CliIO,
	call,
	EXIT,
	PULL_STATE_FILE,
	type PullDocState,
	type PullState,
	readJSON,
	sha256,
	TOO_OLD,
	writeFileAtomic,
	writeJSON,
} from "./state.js";

export type MirrorPushClient = Pick<
	PacaAPIDocClient,
	"batchGet" | "patchDocument"
>;

/** Ids pull accepts (they name the base copies). */
const SAFE_ID = /^[A-Za-z0-9][A-Za-z0-9-]{0,63}$/;
/** A line git merge-file writes around a conflict. "=======" alone is not
 *  taken for one: it is also a heading underline in Markdown. */
const CONFLICT_MARKER = /^(?:<{7}|>{7}|\|{7})(?:\s|$)/;
const AGAIN = "the document changed in Paca; pull, then push again";

function managedMessage(source: DocSource | null | undefined): string {
	return source
		? `this document is a copy of ${source.path} in ${source.repository}; edit that file and push it`
		: "this document is a managed copy of a repository file; edit that file in its repository and push it";
}

function reportConflict(
	io: CliIO,
	label: string,
	who: {
		updated_by?: string | null;
		updated_at?: string | null;
		note?: string;
		client?: string;
		task?: string;
	},
): number {
	io.err(`conflict: the document changed in Paca since ${label} was pulled`);
	if (who.updated_by || who.updated_at)
		io.err(
			`  written by ${who.updated_by || "someone"}${who.updated_at ? ` at ${who.updated_at}` : ""}`,
		);
	if (who.note) io.err(`  note: ${who.note}`);
	if (who.client) io.err(`  client: ${who.client}`);
	if (who.task) io.err(`  task: ${who.task}`);
	io.err(AGAIN);
	return EXIT.conflict;
}

/**
 * Pushes `file` (absolute), a file in the mirror folder `mirror.root`, to
 * the document pull wrote it from. Holds the mirror's lock throughout, so a
 * pull of the same mirror never runs in between. Refusals write nothing
 * and send nothing. Returns the exit code: 0 written or unchanged, 1
 * refused or error, 2 the document changed in Paca.
 */
export async function pushMirrorFile(
	opts: PushOptions,
	client: MirrorPushClient,
	io: CliIO,
	mirror: { root: string; rel: string },
	file: string,
	cwd: string,
): Promise<number> {
	const label = opts.file;
	const refused: Array<[boolean, string]> = [
		[
			!!opts.path,
			`--path cannot be used with a pulled document (${label}): it is pushed to the document it was pulled from`,
		],
		[
			opts.lock,
			`--lock cannot be used with a pulled document (${label}): only repository files are pushed as locked copies`,
		],
		[
			opts.force,
			`--force cannot be used with a pulled document (${label}): it would overwrite changes made in Paca; pull, then push again`,
		],
	];
	for (const [on, why] of refused) {
		if (on) {
			io.err(why);
			return EXIT.error;
		}
	}
	const token = await takeLock(
		mirror.root,
		path.relative(cwd, mirror.root) || ".",
		io,
	);
	if (!token) return EXIT.error;
	try {
		return await pushLocked(opts, client, io, mirror, file, label);
	} finally {
		await releaseLock(mirror.root, token);
	}
}

async function pushLocked(
	opts: PushOptions,
	client: MirrorPushClient,
	io: CliIO,
	{ root, rel }: { root: string; rel: string },
	file: string,
	label: string,
): Promise<number> {
	const statePath = path.join(root, PULL_STATE_FILE);
	const state = await readJSON<PullState>(statePath);
	if (!state?.docs) {
		io.err(
			`${label}: the mirror folder's ${PULL_STATE_FILE} is gone; pull again`,
		);
		return EXIT.error;
	}
	if (state.project !== opts.projectId) {
		io.err(
			`${label} is in a mirror of project ${state.project}; push it with --project ${state.project}`,
		);
		return EXIT.error;
	}
	const entries = Object.entries(state.docs).filter(
		([, d]) => d && typeof d.file === "string",
	);
	const found =
		entries.find(([, d]) => d.file === rel) ??
		entries.find(([, d]) => nameKey(d.file) === nameKey(rel));
	if (!found) {
		io.err(
			`${label}: not a pulled document; pull writes the files here, and only those are pushed from a mirror folder`,
		);
		return EXIT.error;
	}
	const [id, entry] = found;
	if (!SAFE_ID.test(id)) {
		io.err(`${label}: its state entry has an unusable id; pull again`);
		return EXIT.error;
	}
	if (entry.orphan) {
		io.err(
			`${label}: the document was removed in Paca (or you can no longer read it), so there is nothing to push it to`,
		);
		return EXIT.error;
	}
	if (entry.reserved) {
		io.err(
			`${label}: an interrupted pull has not recorded this document yet; run pull first`,
		);
		return EXIT.error;
	}
	if (entry.managed) {
		// Read only to name the repository file.
		const res = await call(() => client.batchGet(opts.projectId, [id])).catch(
			() => null,
		);
		const doc =
			res?.status === "ok" ? res.items.find((d) => d.id === id) : undefined;
		io.err(managedMessage(doc?.source));
		return EXIT.error;
	}

	const bytes = await fs.readFile(file);
	const fileSha = sha256(bytes);
	const text = bytes.toString("utf8");
	const marker = text.split(/\r?\n/).findIndex((l) => CONFLICT_MARKER.test(l));
	if (marker >= 0) {
		io.err(
			`${label} has conflict markers (line ${marker + 1}); resolve them, then push`,
		);
		return EXIT.error;
	}
	if (fileSha === entry.file_sha256) {
		io.out("unchanged");
		return EXIT.ok;
	}
	// The last pull already saw a newer version and kept the file.
	if (entry.remote_hash && entry.remote_hash !== entry.content_hash)
		return reportConflict(io, label, {});

	const read = await call(() => client.batchGet(opts.projectId, [id]));
	if (read.status === "unsupported") {
		io.err(TOO_OLD);
		return EXIT.error;
	}
	const doc = read.items.find((d) => d.id === id);
	if (!doc) {
		io.err(
			`conflict: ${entry.title} is no longer in Paca (deleted, or you can no longer read it); pull to update the mirror`,
		);
		return EXIT.conflict;
	}
	if (doc.source || doc.source_locked) {
		io.err(managedMessage(doc.source));
		return EXIT.error;
	}

	const blocks = (doc.content ?? []) as Block[];
	const recorded = entry.content_hash;
	const moved = !recorded || doc.content_hash !== recorded;
	let spliced: ReturnType<typeof spliceMarkdown> | null = null;
	let refusal: SpliceError | null = null;
	try {
		spliced = spliceMarkdown(blocks, text);
	} catch (err) {
		if (!(err instanceof SpliceError)) throw err;
		refusal = err;
	}

	const basePath = path.join(root, BASE_DIR, `${id}.md`);
	/** The base copy, then the state: a stop between them leaves a state
	 *  that says "edited locally", which the next push settles as
	 *  unchanged (the server then already holds the file). */
	const record = async (hash: string | null, markdown: string) => {
		await writeFileAtomic(basePath, markdown);
		const next: PullDocState = {
			...entry,
			content_hash: hash,
			file_sha256: fileSha,
		};
		delete next.remote_hash;
		delete next.pending_sha256;
		state.docs[id] = next;
		await writeJSON(statePath, state);
	};

	// Nothing to send only when the result is the stored array itself:
	// counts never decide it (a reorder can keep every block as read).
	if (spliced && spliced.blocks === blocks) {
		// Paca already holds what the file says (a save that changed only
		// line ends, or a push that stopped before recording its write).
		if (!opts.dryRun)
			await record(doc.content_hash ?? null, blocknoteToMarkdown(blocks));
		io.out("unchanged");
		return EXIT.ok;
	}
	if (moved)
		return reportConflict(io, label, {
			updated_by: doc.updated_by,
			updated_at: doc.updated_at,
		});
	if (refusal || !spliced) {
		io.err(
			`cannot push ${label}: ${(refusal as SpliceError).message.replaceAll("edit_doc", "push")}`,
		);
		return EXIT.error;
	}

	const n = spliced.changed;
	const blocksSaid = `${n} block${n === 1 ? "" : "s"}`;
	if (opts.dryRun) {
		io.out(
			`${label}: ${blocksSaid} would change in ${entry.title} (nothing sent)`,
		);
		return EXIT.ok;
	}

	const input: PatchDocumentInput = { content: spliced.blocks };
	if (opts.note) input.note = opts.note;
	if (opts.client) input.client = opts.client.slice(0, 200);
	if (opts.task) input.task = opts.task;
	let written: Awaited<ReturnType<MirrorPushClient["patchDocument"]>>;
	try {
		written = await call(() =>
			client.patchDocument(opts.projectId, id, input, {
				ifMatch: recorded as string,
			}),
		);
	} catch (err) {
		if (!(err instanceof PacaAPIError)) throw err;
		if (err.status === 412) return reportConflict(io, label, err.details ?? {});
		if (err.status === 409 && err.code === "DOC_LOCKED") {
			const again = await call(() =>
				client.batchGet(opts.projectId, [id]),
			).catch(() => null);
			const cur =
				again?.status === "ok"
					? again.items.find((d) => d.id === id)
					: undefined;
			io.err(managedMessage(cur?.source));
			return EXIT.error;
		}
		if (err.status === 404) {
			io.err(
				`conflict: ${entry.title} is no longer in Paca (deleted, or you can no longer read it); pull to update the mirror`,
			);
			return EXIT.conflict;
		}
		io.err(err.message);
		return EXIT.error;
	}
	await record(
		written.doc?.content_hash ?? null,
		blocknoteToMarkdown(spliced.blocks),
	);
	io.out(`updated ${entry.title} from ${label} (${blocksSaid} changed)`);
	return EXIT.ok;
}
