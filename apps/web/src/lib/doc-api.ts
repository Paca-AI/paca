import { queryOptions } from "@tanstack/react-query";

import { apiClient } from "./api-client";
import type { SuccessEnvelope } from "./api-error";

// ── Shapes ────────────────────────────────────────────────────────────────────

export interface DocFolder {
	id: string;
	project_id?: string | null;
	parent_id?: string | null;
	name: string;
	position: number;
	created_by?: string | null;
	created_at: string;
	updated_at: string;
}

export interface DocFolderListResult {
	items: DocFolder[];
}

export interface Document {
	id: string;
	project_id?: string | null;
	folder_id?: string | null;
	title: string;
	content: unknown[] | null;
	position: number;
	created_by?: string | null;
	updated_by?: string | null;
	created_at: string;
	updated_at: string;
}

export interface DocumentListResult {
	items: Document[];
}

export interface DocSnapshot {
	id: string;
	document_id?: string | null;
	title: string;
	content: unknown[] | null;
	snapshot_number: number;
	created_by?: string | null;
	created_at: string;
}

export interface DocSnapshotListResult {
	items: DocSnapshot[];
}

export type DocActivityType =
	| "doc.created"
	| "doc.updated"
	| "doc.deleted"
	| "doc.moved"
	| "doc.folder.created"
	| "doc.folder.updated"
	| "doc.folder.deleted"
	| "comment";

export interface FieldChange {
	field: string;
	old: unknown;
	new: unknown;
}

export interface DocActivityContent {
	text?: unknown;
	changes?: FieldChange[] | null;
	[key: string]: unknown;
}

export interface DocActivity {
	id: string;
	document_id: string;
	actor_id: string | null;
	actor_name: string;
	actor_username: string;
	actor_avatar_url?: string | null;
	actor_avatar_thumb_url?: string | null;
	activity_type: DocActivityType;
	content: string | DocActivityContent | null;
	created_at: string;
	updated_at: string;
}

export function getCommentText(content: DocActivity["content"]): string {
	if (typeof content === "string") return content;
	if (content && typeof content === "object" && "text" in content)
		return String(content.text);
	return "";
}

export function getActivityChanges(
	content: DocActivity["content"],
): FieldChange[] | null {
	if (!content || typeof content !== "object" || !("changes" in content)) {
		return null;
	}

	const { changes } = content;
	if (!Array.isArray(changes)) {
		return null;
	}

	return changes.filter(
		(change): change is FieldChange =>
			!!change &&
			typeof change === "object" &&
			"field" in change &&
			typeof change.field === "string",
	);
}

export interface DocActivityListResult {
	items: DocActivity[];
}

// ── Query keys ────────────────────────────────────────────────────────────────

export const docQueryKeys = {
	all: (projectId: string) => ["projects", projectId, "docs"] as const,
	folders: (projectId: string) =>
		["projects", projectId, "docs", "folders"] as const,
	list: (projectId: string, folderId?: string) =>
		folderId
			? (["projects", projectId, "docs", "list", folderId] as const)
			: (["projects", projectId, "docs", "list"] as const),
	detail: (projectId: string, docId: string) =>
		["projects", projectId, "docs", docId] as const,
	snapshots: (projectId: string, docId: string) =>
		["projects", projectId, "docs", docId, "snapshots"] as const,
	snapshot: (projectId: string, docId: string, snapshotId: string) =>
		["projects", projectId, "docs", docId, "snapshots", snapshotId] as const,
	activities: (projectId: string, docId: string) =>
		["projects", projectId, "docs", docId, "activities"] as const,
};

// ── Query options ─────────────────────────────────────────────────────────────

export const docFoldersQueryOptions = (projectId: string) =>
	queryOptions({
		queryKey: docQueryKeys.folders(projectId),
		queryFn: () => listFolders(projectId),
	});

export const docListQueryOptions = (projectId: string, folderId?: string) =>
	queryOptions({
		queryKey: docQueryKeys.list(projectId, folderId),
		queryFn: () => listDocuments(projectId, folderId),
	});

export const docQueryOptions = (projectId: string, docId: string) =>
	queryOptions({
		queryKey: docQueryKeys.detail(projectId, docId),
		queryFn: () => getDocument(projectId, docId),
		enabled: !!docId,
	});

export const docSnapshotsQueryOptions = (projectId: string, docId: string) =>
	queryOptions({
		queryKey: docQueryKeys.snapshots(projectId, docId),
		queryFn: () => listSnapshots(projectId, docId),
		enabled: !!docId,
	});

export const docSnapshotQueryOptions = (
	projectId: string,
	docId: string,
	snapshotId: string,
) =>
	queryOptions({
		queryKey: docQueryKeys.snapshot(projectId, docId, snapshotId),
		queryFn: () => getSnapshot(projectId, docId, snapshotId),
		enabled: !!snapshotId,
	});

export const docActivitiesQueryOptions = (projectId: string, docId: string) =>
	queryOptions({
		queryKey: docQueryKeys.activities(projectId, docId),
		queryFn: () => listActivities(projectId, docId),
		enabled: !!docId,
	});

// ── Folder API ────────────────────────────────────────────────────────────────

export async function listFolders(projectId: string): Promise<DocFolder[]> {
	const { data } = await apiClient.instance.get<
		SuccessEnvelope<DocFolderListResult>
	>(`/projects/${projectId}/docs/folders`);
	return data.data.items;
}

export async function createFolder(
	projectId: string,
	payload: { name: string; parent_id?: string; position?: number },
): Promise<DocFolder> {
	const { data } = await apiClient.instance.post<SuccessEnvelope<DocFolder>>(
		`/projects/${projectId}/docs/folders`,
		payload,
	);
	return data.data;
}

export async function updateFolder(
	projectId: string,
	folderId: string,
	payload: { name?: string; parent_id?: string | null; position?: number },
): Promise<DocFolder> {
	const { data } = await apiClient.instance.patch<SuccessEnvelope<DocFolder>>(
		`/projects/${projectId}/docs/folders/${folderId}`,
		payload,
	);
	return data.data;
}

export async function deleteFolder(
	projectId: string,
	folderId: string,
): Promise<void> {
	await apiClient.instance.delete(
		`/projects/${projectId}/docs/folders/${folderId}`,
	);
}

// ── Document API ──────────────────────────────────────────────────────────────

export async function listDocuments(
	projectId: string,
	folderId?: string,
): Promise<Document[]> {
	const { data } = await apiClient.instance.get<
		SuccessEnvelope<DocumentListResult>
	>(`/projects/${projectId}/docs`, {
		params: folderId ? { folder_id: folderId } : undefined,
	});
	return data.data.items;
}

export async function getDocument(
	projectId: string,
	docId: string,
): Promise<Document> {
	const { data } = await apiClient.instance.get<SuccessEnvelope<Document>>(
		`/projects/${projectId}/docs/${docId}`,
	);
	return data.data;
}

export async function createDocument(
	projectId: string,
	payload: {
		title?: string;
		folder_id?: string | null;
		content?: unknown[] | null;
		position?: number;
	},
): Promise<Document> {
	const { data } = await apiClient.instance.post<SuccessEnvelope<Document>>(
		`/projects/${projectId}/docs`,
		payload,
	);
	return data.data;
}

export async function updateDocument(
	projectId: string,
	docId: string,
	payload: {
		title?: string;
		content?: unknown[] | null;
		folder_id?: string | null;
		position?: number;
	},
): Promise<Document> {
	const { data } = await apiClient.instance.patch<SuccessEnvelope<Document>>(
		`/projects/${projectId}/docs/${docId}`,
		payload,
	);
	return data.data;
}

export async function deleteDocument(
	projectId: string,
	docId: string,
): Promise<void> {
	await apiClient.instance.delete(`/projects/${projectId}/docs/${docId}`);
}

// ── Snapshot API ──────────────────────────────────────────────────────────────

export async function listSnapshots(
	projectId: string,
	docId: string,
): Promise<DocSnapshot[]> {
	const { data } = await apiClient.instance.get<
		SuccessEnvelope<DocSnapshotListResult>
	>(`/projects/${projectId}/docs/${docId}/snapshots`);
	return data.data.items;
}

export async function getSnapshot(
	projectId: string,
	docId: string,
	snapshotId: string,
): Promise<DocSnapshot> {
	const { data } = await apiClient.instance.get<SuccessEnvelope<DocSnapshot>>(
		`/projects/${projectId}/docs/${docId}/snapshots/${snapshotId}`,
	);
	return data.data;
}

// ── Activity / Comment API ────────────────────────────────────────────────────

export async function listActivities(
	projectId: string,
	docId: string,
): Promise<DocActivity[]> {
	const { data } = await apiClient.instance.get<
		SuccessEnvelope<DocActivityListResult>
	>(`/projects/${projectId}/docs/${docId}/activities`);
	return data.data.items;
}

export async function addDocComment(
	projectId: string,
	docId: string,
	content: unknown[],
): Promise<DocActivity> {
	const { data } = await apiClient.instance.post<SuccessEnvelope<DocActivity>>(
		`/projects/${projectId}/docs/${docId}/comments`,
		{ content },
	);
	return data.data;
}

export async function updateDocComment(
	projectId: string,
	docId: string,
	commentId: string,
	content: unknown[],
): Promise<DocActivity> {
	const { data } = await apiClient.instance.patch<SuccessEnvelope<DocActivity>>(
		`/projects/${projectId}/docs/${docId}/comments/${commentId}`,
		{ content },
	);
	return data.data;
}

export async function deleteDocComment(
	projectId: string,
	docId: string,
	commentId: string,
): Promise<void> {
	await apiClient.instance.delete(
		`/projects/${projectId}/docs/${docId}/comments/${commentId}`,
	);
}

// ── Doc file upload API ───────────────────────────────────────────────────────

interface PresignedPart {
	part_number: number;
	upload_url: string;
}

interface MultipartUpload {
	upload_id: string;
	parts: PresignedPart[];
}

interface DocUploadSession {
	file_id: string;
	is_multipart: boolean;
	upload_url?: string;
	multipart?: MultipartUpload;
}

interface CompletedPart {
	part_number: number;
	etag: string;
}

export interface DocFile {
	id: string;
	file_name: string;
	content_type: string;
	file_size: number;
	created_at: string;
}

interface DownloadURLResult {
	url: string;
}

const DOC_MULTIPART_CHUNK_SIZE = 5 * 1024 * 1024; // 5 MiB — matches server DefaultPartSize

async function initiateDocUpload(
	projectId: string,
	docId: string,
	payload: { file_name: string; content_type: string; file_size: number },
): Promise<DocUploadSession> {
	const { data } = await apiClient.instance.post<
		SuccessEnvelope<DocUploadSession>
	>(`/projects/${projectId}/docs/${docId}/files/initiate-upload`, payload);
	return data.data;
}

async function completeDocUpload(
	projectId: string,
	docId: string,
	payload: {
		file_id: string;
		upload_id?: string;
		parts?: CompletedPart[];
	},
): Promise<DocFile> {
	const { data } = await apiClient.instance.post<SuccessEnvelope<DocFile>>(
		`/projects/${projectId}/docs/${docId}/files/complete-upload`,
		payload,
	);
	return data.data;
}

export async function getDocFileDownloadURL(
	projectId: string,
	docId: string,
	fileId: string,
): Promise<string> {
	const { data } = await apiClient.instance.get<
		SuccessEnvelope<DownloadURLResult>
	>(`/projects/${projectId}/docs/${docId}/files/${fileId}/download-url`);
	return data.data.url;
}

export async function deleteDocFile(
	projectId: string,
	docId: string,
	fileId: string,
): Promise<void> {
	await apiClient.instance.delete(
		`/projects/${projectId}/docs/${docId}/files/${fileId}`,
	);
}

/**
 * Uploads a single File directly to the object store via a presigned URL,
 * then confirms with the API.  Handles both single-part (< 5 MiB) and
 * multipart (≥ 5 MiB) uploads transparently.
 */
export async function uploadDocFile(
	projectId: string,
	docId: string,
	file: File,
): Promise<DocFile> {
	const session = await initiateDocUpload(projectId, docId, {
		file_name: file.name,
		content_type: file.type || "application/octet-stream",
		file_size: file.size,
	});

	if (session.is_multipart && session.multipart) {
		const { upload_id, parts } = session.multipart;
		const completedParts: CompletedPart[] = [];

		for (const part of parts) {
			const start = (part.part_number - 1) * DOC_MULTIPART_CHUNK_SIZE;
			const end = Math.min(start + DOC_MULTIPART_CHUNK_SIZE, file.size);
			const chunk = file.slice(start, end);

			const resp = await fetch(part.upload_url, {
				method: "PUT",
				body: chunk,
				headers: { "Content-Type": file.type || "application/octet-stream" },
			});

			if (!resp.ok) {
				throw new Error(
					`Part ${part.part_number} upload failed: ${resp.status}`,
				);
			}

			const etag = resp.headers.get("ETag") ?? resp.headers.get("etag");
			if (!etag) {
				throw new Error(
					`Part ${part.part_number} upload succeeded but did not return an ETag header.`,
				);
			}
			completedParts.push({ part_number: part.part_number, etag });
		}

		return completeDocUpload(projectId, docId, {
			file_id: session.file_id,
			upload_id,
			parts: completedParts,
		});
	}

	if (!session.upload_url) {
		throw new Error("Server returned no upload URL");
	}
	const uploadUrl = session.upload_url;

	await new Promise<void>((resolve, reject) => {
		const xhr = new XMLHttpRequest();
		xhr.open("PUT", uploadUrl);
		xhr.setRequestHeader(
			"Content-Type",
			file.type || "application/octet-stream",
		);
		xhr.addEventListener("load", () =>
			xhr.status >= 200 && xhr.status < 300
				? resolve()
				: reject(new Error(`Upload failed: ${xhr.status}`)),
		);
		xhr.addEventListener("error", () => reject(new Error("Upload error")));
		xhr.send(file);
	});

	return completeDocUpload(projectId, docId, { file_id: session.file_id });
}
