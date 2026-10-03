import { queryOptions } from "@tanstack/react-query";
import { apiClient } from "./api-client";
import type { SuccessEnvelope } from "./api-error";

// Project exports are built asynchronously by the API: POST queues one and
// returns immediately, the client polls the list until it settles, then
// downloads through a short-lived presigned URL. Base path
// `/projects/{projectId}/exports`, under the `/api/v1` prefix api-client.ts
// already applies. Every endpoint needs the project.export permission.

export type ProjectExportStatus =
	| "pending"
	| "processing"
	| "completed"
	| "failed";

export interface ProjectExport {
	id: string;
	project_id: string;
	requested_by?: string;
	kind: "project_archive";
	status: ProjectExportStatus;
	file_name?: string;
	file_size?: number;
	row_count?: number;
	error_message?: string;
	created_at: string;
	completed_at?: string;
	expires_at?: string;
	/** True once the file is past its retention window and can't be downloaded. */
	expired: boolean;
}

interface ProjectExportDownload {
	url: string;
	expires_in_seconds: number;
}

/** How often the list is re-fetched while an export is still queued/running. */
export const EXPORT_POLL_INTERVAL_MS = 2000;

export function isExportActive(e: Pick<ProjectExport, "status">): boolean {
	return e.status === "pending" || e.status === "processing";
}

export async function requestProjectExport(
	projectId: string,
): Promise<ProjectExport> {
	const { data } = await apiClient.instance.post<
		SuccessEnvelope<ProjectExport>
	>(`/projects/${projectId}/exports`);
	return data.data;
}

export async function listProjectExports(
	projectId: string,
): Promise<ProjectExport[]> {
	const { data } = await apiClient.instance.get<
		SuccessEnvelope<{ items: ProjectExport[] }>
	>(`/projects/${projectId}/exports`);
	return data.data.items;
}

export async function getExportDownloadUrl(
	projectId: string,
	exportId: string,
): Promise<string> {
	const { data } = await apiClient.instance.get<
		SuccessEnvelope<ProjectExportDownload>
	>(`/projects/${projectId}/exports/${exportId}/download`);
	return data.data.url;
}

export const projectExportsQueryKey = (projectId: string) =>
	["projects", projectId, "exports"] as const;

// Polls only while something is queued or running, so an idle settings tab
// makes no background requests.
export const projectExportsQueryOptions = (projectId: string) =>
	queryOptions({
		queryKey: projectExportsQueryKey(projectId),
		queryFn: () => listProjectExports(projectId),
		refetchInterval: (query) =>
			query.state.data?.some(isExportActive) ? EXPORT_POLL_INTERVAL_MS : false,
	});

/** Starts a browser download of a presigned URL (the response is an attachment). */
export function triggerDownload(url: string) {
	const a = document.createElement("a");
	a.href = url;
	a.rel = "noopener";
	document.body.appendChild(a);
	a.click();
	a.remove();
}
