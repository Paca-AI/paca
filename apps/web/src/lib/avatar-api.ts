import { apiClient } from "./api-client";
import type { SuccessEnvelope } from "./api-error";

// ── Shared constants ─────────────────────────────────────────────────────────
// Mirrors attachmentdom.MaxAvatarUploadSize / AvatarContentTypes on the server
// — checking client-side first gives instant feedback, but the server always
// re-validates.

export const MAX_AVATAR_UPLOAD_SIZE = 5 * 1024 * 1024; // 5 MiB
export const ACCEPTED_AVATAR_CONTENT_TYPES = [
	"image/png",
	"image/jpeg",
	"image/webp",
	"image/gif",
];

/** Returns an error message if file isn't an acceptable avatar upload, else null. */
export function validateAvatarFile(file: File): string | null {
	if (!ACCEPTED_AVATAR_CONTENT_TYPES.includes(file.type)) {
		return "invalidType";
	}
	if (file.size > MAX_AVATAR_UPLOAD_SIZE) {
		return "tooLarge";
	}
	return null;
}

// ── API shapes ────────────────────────────────────────────────────────────────

interface AvatarUploadSession {
	file_id: string;
	upload_url?: string;
}

export interface AvatarResult {
	avatar_url?: string | null;
	avatar_thumb_url?: string | null;
}

// ── API calls ─────────────────────────────────────────────────────────────────

async function initiateAvatarUpload(
	basePath: string,
	payload: { file_name: string; content_type: string; file_size: number },
): Promise<AvatarUploadSession> {
	const { data } = await apiClient.instance.post<
		SuccessEnvelope<AvatarUploadSession>
	>(`${basePath}/avatar/initiate-upload`, payload);
	return data.data;
}

// The server omits avatar_url/avatar_thumb_url entirely (rather than sending
// them as null) when an owner has no avatar, since the DTO fields are
// `*string` with `json:",omitempty"`. Callers merge this result onto cached
// query data with `{ ...old, ...result }` — spreading an object that's
// missing a key leaves the stale value from `old` in place, so a removal
// would silently fail to clear the avatar until the next full refetch.
// Normalizing both keys to be always-present (real URL or explicit null)
// here means every caller's spread-merge just works.
function normalizeAvatarResult(result: AvatarResult): Required<AvatarResult> {
	return {
		avatar_url: result.avatar_url ?? null,
		avatar_thumb_url: result.avatar_thumb_url ?? null,
	};
}

async function completeAvatarUpload(
	basePath: string,
	fileId: string,
): Promise<AvatarResult> {
	const { data } = await apiClient.instance.post<SuccessEnvelope<AvatarResult>>(
		`${basePath}/avatar/complete-upload`,
		{ file_id: fileId },
	);
	return normalizeAvatarResult(data.data);
}

/**
 * Uploads a single image file directly to the object store via a presigned
 * URL, then confirms with the API so the server can derive the "full" and
 * "thumb" variants. `basePath` selects the owner — `/users/me`,
 * `/projects/{projectId}/agents/{agentId}`, or `/admin/agents/{agentId}`.
 */
export async function uploadAvatar(
	basePath: string,
	file: File,
	onProgress?: (loaded: number, total: number) => void,
): Promise<AvatarResult> {
	const session = await initiateAvatarUpload(basePath, {
		file_name: file.name,
		content_type: file.type || "application/octet-stream",
		file_size: file.size,
	});
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

	return completeAvatarUpload(basePath, session.file_id);
}

/** Removes the avatar at basePath (see uploadAvatar for basePath shapes). */
export async function removeAvatar(basePath: string): Promise<AvatarResult> {
	const { data } = await apiClient.instance.delete<
		SuccessEnvelope<AvatarResult>
	>(`${basePath}/avatar`);
	return normalizeAvatarResult(data.data);
}
