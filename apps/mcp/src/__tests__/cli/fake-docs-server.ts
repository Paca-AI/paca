// A fake Paca document API for the mirror tests: documents as stored blocks
// with a content hash, a change feed, batch reads, and content writes with
// If-Match the way the server answers them.

import { createHash } from "node:crypto";
import { vi } from "vitest";
import { PacaAPIError } from "../../api/doc-client.js";
import { markdownToBlocknote } from "../../utils/index.js";

export interface FakeDoc {
	id: string;
	title: string;
	folder_id: string | null;
	content: any[];
	hash: string;
	updated_by?: string;
	updated_at?: string;
	note?: string;
	client?: string;
	task?: string;
	source?: { repository: string; path: string } | null;
	source_locked?: boolean;
}

export const docId = (n: number) =>
	`${String(n).repeat(8)}-aaaa-4bbb-8ccc-${String(n).padStart(12, "0")}`;

const hashOf = (content: unknown) =>
	createHash("sha256")
		.update(JSON.stringify(content))
		.digest("hex")
		.slice(0, 16);

/** Blocks as the API returns them: parsed, through JSON, with ids. */
export function storedBlocks(
	markdown: string,
	extra?: (blocks: any[]) => void,
): any[] {
	const blocks = JSON.parse(JSON.stringify(markdownToBlocknote(markdown)));
	extra?.(blocks);
	return blocks;
}

export function fakeDocsServer() {
	const docs = new Map<string, FakeDoc>();
	/** Every version each document had, newest last. */
	const snapshots = new Map<
		string,
		Array<{ id: string; content: any[]; content_hash: string }>
	>();
	let tick = 0;
	const snap = (d: FakeDoc) => {
		const list = snapshots.get(d.id) ?? [];
		list.push({
			id: `s${list.length + 1}-${d.id.slice(0, 4)}`,
			content: copy(d.content),
			content_hash: d.hash,
		});
		snapshots.set(d.id, list);
	};
	const copy = <T>(x: T): T => JSON.parse(JSON.stringify(x));
	const toDoc = (d: FakeDoc) => ({
		id: d.id,
		title: d.title,
		folder_id: d.folder_id,
		content: copy(d.content),
		content_hash: d.hash,
		position: 0,
		created_at: "",
		updated_at: d.updated_at ?? "",
		updated_by: d.updated_by ?? null,
		source: d.source ?? null,
		source_locked: d.source_locked ?? false,
	});
	const client = {
		getChanges: vi.fn(async (_p: string, since?: string) => ({
			status: "ok" as const,
			cursor: `c${++tick}`,
			reset: !since,
			docs: [...docs.values()].map((d) => ({
				id: d.id,
				folder_id: d.folder_id,
				title: d.title,
				content_hash: d.hash,
				updated_at: d.updated_at ?? "t",
				deleted: false,
			})),
			folders: [],
		})),
		batchGet: vi.fn(async (_p: string, ids: string[]) => ({
			status: "ok" as const,
			items: ids.flatMap((id) => {
				const d = docs.get(id);
				return d ? [toDoc(d)] : [];
			}),
		})),
		patchDocument: vi.fn(
			async (
				_p: string,
				id: string,
				input: {
					content: unknown[];
					note?: string;
					client?: string;
					task?: string;
				},
				opts: { ifMatch?: string } = {},
			) => {
				const d = docs.get(id);
				if (!d)
					throw new PacaAPIError("document not found", 404, "DOC_NOT_FOUND");
				if (d.source_locked)
					throw new PacaAPIError("document is locked", 409, "DOC_LOCKED");
				if (opts.ifMatch && opts.ifMatch !== d.hash) {
					throw new PacaAPIError("version conflict", 412, "VERSION_CONFLICT", {
						...(d.updated_by ? { updated_by: d.updated_by } : {}),
						...(d.updated_at ? { updated_at: d.updated_at } : {}),
						...(d.note ? { note: d.note } : {}),
						...(d.client ? { client: d.client } : {}),
						...(d.task ? { task: d.task } : {}),
					});
				}
				d.content = copy(input.content);
				d.hash = hashOf(d.content);
				snap(d);
				d.updated_by = "agent";
				d.updated_at = `2026-11-15T10:00:${String(++tick).padStart(2, "0")}Z`;
				d.note = input.note;
				d.client = input.client;
				d.task = input.task;
				return { doc: toDoc(d), etag: `"${d.hash}-gzip"` };
			},
		),
		listSnapshots: vi.fn(
			async (_p: string, id: string, opts: { contentHash?: string } = {}) => ({
				items: [...(snapshots.get(id) ?? [])]
					.reverse()
					.filter(
						(x) => !opts.contentHash || x.content_hash === opts.contentHash,
					)
					.map((x) => ({
						id: x.id,
						title: docs.get(id)?.title ?? "",
						content: null,
						content_hash: x.content_hash,
						snapshot_number: 0,
						created_at: "",
					})),
				nextCursor: null,
			}),
		),
		getSnapshot: vi.fn(async (_p: string, id: string, snapshotId: string) => {
			const x = (snapshots.get(id) ?? []).find((y) => y.id === snapshotId);
			if (!x) throw new PacaAPIError("snapshot not found", 404, "NOT_FOUND");
			return {
				id: x.id,
				title: docs.get(id)?.title ?? "",
				content: copy(x.content),
				content_hash: x.content_hash,
				snapshot_number: 0,
				created_at: "",
			};
		}),
	};
	const add = (
		id: string,
		title: string,
		content: any[],
		extra: Partial<FakeDoc> = {},
	) => {
		docs.set(id, {
			id,
			title,
			folder_id: null,
			content: copy(content),
			hash: hashOf(content),
			...extra,
		});
		snap(docs.get(id) as FakeDoc);
	};
	/** Another writer changes a document. */
	const edit = (
		id: string,
		change: (blocks: any[]) => void,
		who: Partial<FakeDoc> = {},
	) => {
		const d = docs.get(id) as FakeDoc;
		const next = copy(d.content);
		change(next);
		d.content = next;
		d.hash = hashOf(next);
		snap(d);
		Object.assign(d, {
			updated_by: "ann",
			updated_at: "2026-11-15T09:30:00Z",
			note: undefined,
			client: undefined,
			task: undefined,
			...who,
		});
	};
	return { docs, snapshots, client, add, edit };
}
