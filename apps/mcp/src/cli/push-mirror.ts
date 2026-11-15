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
import { fencedLines } from "../utils/md-normalise.js";
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
/** The first line of `text` (from 1) that git merge-file writes around a
 *  conflict, outside fenced code and not already in the base copy; 0 if
 *  none. */
export function conflictLine(text: string, base: string): number {
	const known = new Set(
		base.split(/\r?\n/).filter((l) => CONFLICT_MARKER.test(l)),
	);
	const lines = text.split(/\r?\n/);
	const fenced = fencedLines(lines);
	const k = lines.findIndex(
		(l, i) => !fenced[i] && CONFLICT_MARKER.test(l) && !known.has(l),
	);
	return k + 1;
}

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
	let found = entries.find(([, d]) => d.file === rel);
	if (!found) {
		// A name that only folds to a pulled file's (case, Unicode form) is
		// that file only where the file system says so: the same device and
		// inode.
		const me = await fs.stat(file);
		for (const e of entries) {
			if (nameKey(e[1].file) !== nameKey(rel)) continue;
			const st = await fs.stat(path.join(root, e[1].file)).catch(() => null);
			if (st && st.dev === me.dev && st.ino === me.ino) {
				found = e;
				break;
			}
		}
	}
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
	if (fileSha === entry.file_sha256) {
		io.out("unchanged");
		return EXIT.ok;
	}
	const basePath = path.join(root, BASE_DIR, `${id}.md`);
	const baseText = await fs.readFile(basePath, "utf8").catch(() => "");
	const marker = conflictLine(text, baseText);
	if (marker > 0) {
		io.err(
			`${label} has conflict markers (line ${marker}); resolve them, then push`,
		);
		return EXIT.error;
	}
	// An empty base copy is an empty document: saving its file again with
	// only blank lines is no edit.
	if (text.trim() === "" && baseText.trim() !== "") {
		io.err(
			`${label} is empty; a document is deleted in Paca, not by emptying its file`,
		);
		return EXIT.error;
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

	/** The base copy, then the state (with the sha256 of the render), then
	 *  the file, rewritten to the render when it differs only by the
	 *  normalising rules. A stop before the state is written leaves a state
	 *  that says "edited locally", and a stop before the file is rewritten
	 *  leaves a file that differs from the state: either way the next push
	 *  finds the server already holds the file, and records it again. */
	const record = async (hash: string | null, markdown: string) => {
		await writeFileAtomic(basePath, markdown);
		const next: PullDocState = {
			...entry,
			content_hash: hash,
			file_sha256: sha256(markdown),
		};
		delete next.remote_hash;
		delete next.pending_sha256;
		delete next.unmerged;
		state.docs[id] = next;
		await writeJSON(statePath, state);
		if (text !== markdown) {
			await writeFileAtomic(file, markdown);
			return true;
		}
		return false;
	};

	// Nothing to send only when the result is the stored array itself:
	// counts never decide it (a reorder can keep every block as read).
	if (spliced && spliced.blocks === blocks) {
		// Paca already holds what the file says (a save that changed only
		// what the normalising rules allow, or a push that stopped before
		// recording its write).
		io.out("unchanged");
		if (
			!opts.dryRun &&
			(await record(doc.content_hash ?? null, blocknoteToMarkdown(blocks)))
		)
			io.out(`normalised ${label}`);
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
	io.out(`updated ${entry.title} from ${label} (${blocksSaid} changed)`);
	if (
		await record(
			written.doc?.content_hash ?? null,
			blocknoteToMarkdown(spliced.blocks),
		)
	)
		io.out(`normalised ${label}`);
	return EXIT.ok;
}
