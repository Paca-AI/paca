import {
	infiniteQueryOptions,
	type QueryClient,
	queryOptions,
} from "@tanstack/react-query";
import { apiClient } from "./api-client";
import type { SuccessEnvelope } from "./api-error";

// ── Types ─────────────────────────────────────────────────────────────────────

/** Where files live: one project, or the general (workspace) scope. */
export interface FileScope {
	projectId: string | null;
}

export const GENERAL_SCOPE: FileScope = { projectId: null };

/** A folder or a file. Files and folders share one shape. */
export interface FileNode {
	id: string;
	project_id: string | null;
	parent_id: string | null;
	kind: "folder" | "file";
	name: string;
	/** Where a file came from: upload | task_attachment | doc | annotation. */
	source?: string;
	content_type?: string;
	size: number;
	owner_id: string | null;
	owner_name?: string;
	created_at: string;
	updated_at: string;
	deleted_at?: string | null;
	accessed_at?: string | null;
	starred: boolean;
	/** Set on the default folders: task_attachments | task | docs | annotations. */
	system?: string;
	/** Locked items cannot be renamed or moved. */
	locked: boolean;
}

export interface FileRoot {
	kind: "general" | "project";
	project_id?: string;
	name: string;
}

export type FileSortKey = "name" | "modified" | "size";
export type FileTypeFilter =
	| "document"
	| "spreadsheet"
	| "presentation"
	| "pdf"
	| "image"
	| "video"
	| "audio"
	| "archive";

export const FILE_TYPE_FILTERS: readonly FileTypeFilter[] = [
	"document",
	"spreadsheet",
	"presentation",
	"pdf",
	"image",
	"video",
	"audio",
	"archive",
];

export interface FileListParams {
	parentId?: string | null;
	q?: string;
	type?: FileTypeFilter;
	sort?: FileSortKey;
	order?: "asc" | "desc";
}

interface Page {
	items: FileNode[];
	total_count: number;
	page_size: number;
	offset: number;
	has_more: boolean;
}

export const FILES_PAGE_SIZE = 100;

// ── Paths and keys ────────────────────────────────────────────────────────────

export function filesBase(scope: FileScope): string {
	return scope.projectId ? `/projects/${scope.projectId}/files` : "/files";
}

export const scopeKey = (scope: FileScope) => scope.projectId ?? "general";

/** Prefix of every cached query of one scope, for invalidation. */
export const filesScopeKey = (scope: FileScope) =>
	["files", scopeKey(scope)] as const;

export const filesRootsKey = ["files", "roots"] as const;

// ── Calls ─────────────────────────────────────────────────────────────────────

async function getPage(
	scope: FileScope,
	path: string,
	params: Record<string, unknown>,
): Promise<Page> {
	const { data } = await apiClient.instance.get<SuccessEnvelope<Page>>(
		`${filesBase(scope)}${path}`,
		{ params },
	);
	return data.data;
}

export function listFileRoots(): Promise<FileRoot[]> {
	return apiClient.instance
		.get<SuccessEnvelope<{ items: FileRoot[] }>>("/files/roots")
		.then((r) => r.data.data.items);
}

export function listNodes(
	scope: FileScope,
	p: FileListParams,
	offset: number,
): Promise<Page> {
	return getPage(scope, "/nodes", {
		parent_id: p.parentId || undefined,
		q: p.q || undefined,
		type: p.type,
		sort: p.sort,
		order: p.order,
		page_size: FILES_PAGE_SIZE,
		offset,
	});
}

export const listStarred = (scope: FileScope, offset = 0) =>
	getPage(scope, "/starred", { page_size: FILES_PAGE_SIZE, offset });
export const listRecent = (scope: FileScope) =>
	getPage(scope, "/recent", { page_size: 50 });
export const listTrash = (scope: FileScope, offset = 0) =>
	getPage(scope, "/trash", { page_size: FILES_PAGE_SIZE, offset });

export async function getNodePath(
	scope: FileScope,
	id: string,
): Promise<FileNode[]> {
	const { data } = await apiClient.instance.get<
		SuccessEnvelope<{ items: FileNode[] }>
	>(`${filesBase(scope)}/nodes/${id}/path`);
	return data.data.items;
}

export async function getNode(scope: FileScope, id: string): Promise<FileNode> {
	const { data } = await apiClient.instance.get<SuccessEnvelope<FileNode>>(
		`${filesBase(scope)}/nodes/${id}`,
	);
	return data.data;
}

export async function createFolder(
	scope: FileScope,
	payload: { parent_id: string | null; name: string },
): Promise<FileNode> {
	const { data } = await apiClient.instance.post<SuccessEnvelope<FileNode>>(
		`${filesBase(scope)}/folders`,
		payload,
	);
	return data.data;
}

export async function renameNode(
	scope: FileScope,
	id: string,
	name: string,
): Promise<FileNode> {
	const { data } = await apiClient.instance.patch<SuccessEnvelope<FileNode>>(
		`${filesBase(scope)}/nodes/${id}`,
		{ name },
	);
	return data.data;
}

/** Moves to `parentId`; null moves to the root of the scope. */
export async function moveNode(
	scope: FileScope,
	id: string,
	parentId: string | null,
): Promise<FileNode> {
	const { data } = await apiClient.instance.patch<SuccessEnvelope<FileNode>>(
		`${filesBase(scope)}/nodes/${id}`,
		{ move: true, parent_id: parentId },
	);
	return data.data;
}

export async function trashNode(scope: FileScope, id: string): Promise<void> {
	await apiClient.instance.delete(`${filesBase(scope)}/nodes/${id}`);
}

export async function restoreNode(
	scope: FileScope,
	id: string,
): Promise<FileNode> {
	const { data } = await apiClient.instance.post<SuccessEnvelope<FileNode>>(
		`${filesBase(scope)}/nodes/${id}/restore`,
	);
	return data.data;
}

export async function deleteNodePermanently(
	scope: FileScope,
	id: string,
): Promise<void> {
	await apiClient.instance.delete(`${filesBase(scope)}/nodes/${id}/permanent`);
}

export async function setNodeStarred(
	scope: FileScope,
	id: string,
	starred: boolean,
): Promise<void> {
	await apiClient.instance.put(`${filesBase(scope)}/nodes/${id}/star`, {
		starred,
	});
}

export interface FileDownload {
	url: string;
	name: string;
	content_type: string;
	size: number;
	expires_at: string;
}

export async function getNodeDownload(
	scope: FileScope,
	id: string,
	options: { download?: boolean } = {},
): Promise<FileDownload> {
	const { data } = await apiClient.instance.get<SuccessEnvelope<FileDownload>>(
		`${filesBase(scope)}/nodes/${id}/download-url`,
		{ params: options.download ? { download: true } : undefined },
	);
	return data.data;
}

// ── Upload ────────────────────────────────────────────────────────────────────

interface UploadSession {
	file_id: string;
	is_multipart: boolean;
	upload_url?: string;
	multipart?: {
		upload_id: string;
		parts: { part_number: number; upload_url: string }[];
	};
}

const MULTIPART_CHUNK_SIZE = 5 * 1024 * 1024; // matches the server's DefaultPartSize

export class UploadAbortedError extends Error {
	constructor() {
		super("Upload cancelled");
		this.name = "UploadAbortedError";
	}
}

function putWithProgress(
	url: string,
	body: Blob,
	contentType: string,
	onProgress: (loaded: number) => void,
	signal?: AbortSignal,
): Promise<string | null> {
	return new Promise((resolve, reject) => {
		const xhr = new XMLHttpRequest();
		xhr.open("PUT", url);
		xhr.setRequestHeader("Content-Type", contentType);
		xhr.upload.addEventListener("progress", (e) => {
			if (e.lengthComputable) onProgress(e.loaded);
		});
		xhr.addEventListener("load", () =>
			xhr.status >= 200 && xhr.status < 300
				? resolve(xhr.getResponseHeader("ETag"))
				: reject(new Error(`Upload failed: ${xhr.status}`)),
		);
		xhr.addEventListener("error", () => reject(new Error("Upload error")));
		xhr.addEventListener("abort", () => reject(new UploadAbortedError()));
		signal?.addEventListener("abort", () => xhr.abort(), { once: true });
		if (signal?.aborted) {
			reject(new UploadAbortedError());
			return;
		}
		xhr.send(body);
	});
}

/**
 * Uploads one file into a folder: asks the API for presigned URL(s), PUTs the
 * bytes straight to the object store (in 5 MiB parts when large), then tells
 * the API it is done, which makes the file appear in its folder.
 */
export async function uploadFile(
	scope: FileScope,
	parentId: string | null,
	file: File,
	opts: {
		onProgress?: (loaded: number, total: number) => void;
		signal?: AbortSignal;
	} = {},
): Promise<FileNode> {
	const contentType = file.type || "application/octet-stream";
	const { data: init } = await apiClient.instance.post<
		SuccessEnvelope<UploadSession>
	>(`${filesBase(scope)}/uploads/initiate`, {
		parent_id: parentId,
		file_name: file.name,
		content_type: contentType,
		file_size: file.size,
	});
	const session = init.data;
	const report = (loaded: number) => opts.onProgress?.(loaded, file.size);

	let completion: {
		file_id: string;
		upload_id?: string;
		parts?: { part_number: number; etag: string }[];
	} = { file_id: session.file_id };

	if (session.is_multipart && session.multipart) {
		const parts: { part_number: number; etag: string }[] = [];
		let done = 0;
		for (const part of session.multipart.parts) {
			const start = (part.part_number - 1) * MULTIPART_CHUNK_SIZE;
			const chunk = file.slice(
				start,
				Math.min(start + MULTIPART_CHUNK_SIZE, file.size),
			);
			const etag = await putWithProgress(
				part.upload_url,
				chunk,
				contentType,
				(loaded) => report(done + loaded),
				opts.signal,
			);
			if (!etag) {
				throw new Error(
					"The object store did not expose the ETag header; multipart uploads need it exposed through CORS.",
				);
			}
			parts.push({ part_number: part.part_number, etag });
			done += chunk.size;
		}
		completion = {
			file_id: session.file_id,
			upload_id: session.multipart.upload_id,
			parts,
		};
	} else {
		if (!session.upload_url) throw new Error("Server returned no upload URL");
		await putWithProgress(
			session.upload_url,
			file,
			contentType,
			report,
			opts.signal,
		);
	}

	const { data } = await apiClient.instance.post<SuccessEnvelope<FileNode>>(
		`${filesBase(scope)}/uploads/complete`,
		completion,
	);
	return data.data;
}

// ── Query options ─────────────────────────────────────────────────────────────

export const fileRootsQueryOptions = queryOptions({
	queryKey: filesRootsKey,
	queryFn: listFileRoots,
	staleTime: 60_000,
});

export const fileNodesQueryOptions = (scope: FileScope, p: FileListParams) =>
	infiniteQueryOptions({
		queryKey: [...filesScopeKey(scope), "nodes", p] as const,
		queryFn: ({ pageParam }: { pageParam: number }) =>
			listNodes(scope, p, pageParam),
		initialPageParam: 0,
		getNextPageParam: (last) =>
			last.has_more ? last.offset + last.items.length : undefined,
		staleTime: 15_000,
	});

export type FileSection = "recent" | "starred" | "trash";

/** First page (up to 100 items) of Recent, Starred or Trash for one scope. */
export const fileSectionQueryOptions = (
	scope: FileScope,
	section: FileSection,
) =>
	queryOptions({
		queryKey: [...filesScopeKey(scope), "section", section] as const,
		queryFn: async () => {
			const page =
				section === "recent"
					? await listRecent(scope)
					: section === "starred"
						? await listStarred(scope)
						: await listTrash(scope);
			return page.items;
		},
		staleTime: 15_000,
	});

export const fileNodePathQueryOptions = (scope: FileScope, id: string) =>
	queryOptions({
		queryKey: [...filesScopeKey(scope), "path", id] as const,
		queryFn: () => getNodePath(scope, id),
		enabled: !!id,
		staleTime: 15_000,
	});

/** Refreshes every cached list of a scope (listings, starred, recent, trash, paths). */
export function invalidateFiles(qc: QueryClient, scope: FileScope) {
	return qc.invalidateQueries({ queryKey: filesScopeKey(scope) });
}

// ── Helpers ───────────────────────────────────────────────────────────────────

/** The scope a node belongs to (nodes carry their project id). */
export function scopeOfNode(n: FileNode): FileScope {
	return { projectId: n.project_id };
}

export type FileCategory =
	| "folder"
	| "image"
	| "video"
	| "audio"
	| "pdf"
	| "document"
	| "spreadsheet"
	| "presentation"
	| "archive"
	| "code"
	| "text"
	| "other";

const EXT_CATEGORY: Record<string, FileCategory> = {
	pdf: "pdf",
	doc: "document",
	docx: "document",
	odt: "document",
	rtf: "document",
	xls: "spreadsheet",
	xlsx: "spreadsheet",
	ods: "spreadsheet",
	csv: "spreadsheet",
	ppt: "presentation",
	pptx: "presentation",
	odp: "presentation",
	zip: "archive",
	tar: "archive",
	gz: "archive",
	tgz: "archive",
	"7z": "archive",
	rar: "archive",
	txt: "text",
	md: "text",
	log: "text",
	json: "code",
	js: "code",
	ts: "code",
	tsx: "code",
	jsx: "code",
	go: "code",
	py: "code",
	java: "code",
	rs: "code",
	css: "code",
	html: "code",
	xml: "code",
	yml: "code",
	yaml: "code",
	sql: "code",
	sh: "code",
};

/** Coarse file category from the MIME type, falling back to the extension. */
export function fileCategory(
	n: Pick<FileNode, "kind" | "name" | "content_type">,
) {
	if (n.kind === "folder") return "folder" satisfies FileCategory;
	const ct = n.content_type ?? "";
	if (ct.startsWith("image/")) return "image";
	if (ct.startsWith("video/")) return "video";
	if (ct.startsWith("audio/")) return "audio";
	if (ct === "application/pdf") return "pdf";
	const ext = n.name.includes(".")
		? (n.name.split(".").pop() ?? "").toLowerCase()
		: "";
	return EXT_CATEGORY[ext] ?? (ct.startsWith("text/") ? "text" : "other");
}

export function formatFileSize(bytes: number): string {
	if (bytes < 1024) return `${bytes} B`;
	const units = ["KB", "MB", "GB", "TB"];
	let v = bytes / 1024;
	let i = 0;
	while (v >= 1024 && i < units.length - 1) {
		v /= 1024;
		i++;
	}
	return `${v >= 10 ? Math.round(v) : v.toFixed(1)} ${units[i]}`;
}

/** Skip the in-app viewer above this size and offer a download instead. */
export const MAX_PREVIEW_BYTES = 50 * 1024 * 1024;
