import { queryOptions } from "@tanstack/react-query";
import { apiClient } from "./api-client";
import type { SuccessEnvelope } from "./api-error";

// ── API shapes ────────────────────────────────────────────────────────────────

export interface AttachmentFile {
	id: string;
	file_name: string;
	content_type: string;
	file_size: number;
	created_at: string;
}

export interface TaskAttachment {
	id: string;
	task_id: string;
	file_id: string;
	created_by?: string | null;
	created_at: string;
	file: AttachmentFile;
}

interface AttachmentListResult {
	items: TaskAttachment[];
}

interface PresignedPart {
	part_number: number;
	upload_url: string;
}

interface MultipartUpload {
	upload_id: string;
	parts: PresignedPart[];
}

export interface UploadSession {
	file_id: string;
	is_multipart: boolean;
	upload_url?: string;
	multipart?: MultipartUpload;
}

interface CompletedPart {
	part_number: number;
	etag: string;
}

interface DownloadURLResult {
	url: string;
}

const MULTIPART_CHUNK_SIZE = 5 * 1024 * 1024; // 5 MiB — matches server DefaultPartSize

// ── API calls ─────────────────────────────────────────────────────────────────

export async function listTaskAttachments(
	projectId: string,
	taskId: string,
): Promise<TaskAttachment[]> {
	const { data } = await apiClient.instance.get<
		SuccessEnvelope<AttachmentListResult>
	>(`/projects/${projectId}/tasks/${taskId}/attachments`);
	return data.data.items;
}

async function initiateUpload(
	projectId: string,
	taskId: string,
	payload: { file_name: string; content_type: string; file_size: number },
): Promise<UploadSession> {
	const { data } = await apiClient.instance.post<
		SuccessEnvelope<UploadSession>
	>(
		`/projects/${projectId}/tasks/${taskId}/attachments/initiate-upload`,
		payload,
	);
	return data.data;
}

async function completeUpload(
	projectId: string,
	taskId: string,
	payload: {
		file_id: string;
		upload_id?: string;
		parts?: CompletedPart[];
	},
): Promise<TaskAttachment> {
	const { data } = await apiClient.instance.post<
		SuccessEnvelope<TaskAttachment>
	>(
		`/projects/${projectId}/tasks/${taskId}/attachments/complete-upload`,
		payload,
	);
	return data.data;
}

export async function getAttachmentDownloadURL(
	projectId: string,
	taskId: string,
	attachmentId: string,
	options: { download?: boolean } = {},
): Promise<string> {
	const params = options.download ? "?download=true" : "";
	const { data } = await apiClient.instance.get<
		SuccessEnvelope<DownloadURLResult>
	>(
		`/projects/${projectId}/tasks/${taskId}/attachments/${attachmentId}/download-url${params}`,
	);
	return data.data.url;
}

export async function deleteTaskAttachment(
	projectId: string,
	taskId: string,
	attachmentId: string,
): Promise<void> {
	await apiClient.instance.delete(
		`/projects/${projectId}/tasks/${taskId}/attachments/${attachmentId}`,
	);
}

// ── Upload orchestration ──────────────────────────────────────────────────────

/**
 * Uploads a single File directly to the object store via a presigned URL,
 * then confirms with the API.  Handles both single-part (< 5 MiB) and
 * multipart (≥ 5 MiB) uploads transparently.
 *
 * @param onProgress Optional callback receiving bytes uploaded so far.
 */
export async function uploadAttachment(
	projectId: string,
	taskId: string,
	file: File,
	onProgress?: (loaded: number, total: number) => void,
): Promise<TaskAttachment> {
	// 1. Initiate upload — get presigned URL(s) from the server.
	const session = await initiateUpload(projectId, taskId, {
		file_name: file.name,
		content_type: file.type || "application/octet-stream",
		file_size: file.size,
	});

	if (session.is_multipart && session.multipart) {
		// 2a. Multipart path: upload each part sequentially.
		const { upload_id, parts } = session.multipart;
		const completedParts: CompletedPart[] = [];
		let loaded = 0;

		for (const part of parts) {
			const start = (part.part_number - 1) * MULTIPART_CHUNK_SIZE;
			const end = Math.min(start + MULTIPART_CHUNK_SIZE, file.size);
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

			// The S3-compatible store returns the ETag in the response headers.
			const etag = resp.headers.get("ETag") ?? resp.headers.get("etag");
			if (!etag) {
				throw new Error(
					`Part ${part.part_number} upload succeeded but did not return an ETag header. ` +
						`Multipart uploads require the object store to expose ETag via CORS.`,
				);
			}
			completedParts.push({ part_number: part.part_number, etag });

			loaded += chunk.size;
			onProgress?.(loaded, file.size);
		}

		// 3a. Complete multipart upload.
		return completeUpload(projectId, taskId, {
			file_id: session.file_id,
			upload_id,
			parts: completedParts,
		});
	} else {
		// 2b. Single-part path: upload the entire file.
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

			xhr.upload.addEventListener("progress", (e) => {
				if (e.lengthComputable) onProgress?.(e.loaded, e.total);
			});
			xhr.addEventListener("load", () =>
				xhr.status >= 200 && xhr.status < 300
					? resolve()
					: reject(new Error(`Upload failed: ${xhr.status}`)),
			);
			xhr.addEventListener("error", () => reject(new Error("Upload error")));
			xhr.send(file);
		});

		// 3b. Confirm single-part upload.
		return completeUpload(projectId, taskId, { file_id: session.file_id });
	}
}

// ── React Query options ───────────────────────────────────────────────────────

export const taskAttachmentsQueryOptions = (
	projectId: string,
	taskId: string,
) =>
	queryOptions({
		queryKey: ["projects", projectId, "tasks", taskId, "attachments"] as const,
		queryFn: () => listTaskAttachments(projectId, taskId),
		enabled: !!projectId && !!taskId,
		staleTime: 30_000,
	});
