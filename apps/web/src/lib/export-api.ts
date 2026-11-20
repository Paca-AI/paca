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

/** One numbered page of a project's exports, newest first. */
export interface ProjectExportPage {
	items: ProjectExport[];
	page: number;
	page_size: number;
	/** The project's exports across all pages. */
	total_count: number;
}

export async function listProjectExports(
	projectId: string,
	page = 1,
	pageSize = 20,
): Promise<ProjectExportPage> {
	const { data } = await apiClient.instance.get<
		SuccessEnvelope<ProjectExportPage>
	>(`/projects/${projectId}/exports`, {
		params: { page, page_size: pageSize },
	});
	return data.data;
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
export const projectExportsQueryOptions = (
	projectId: string,
	page = 1,
	pageSize = 20,
) =>
	queryOptions({
		queryKey: [...projectExportsQueryKey(projectId), page, pageSize] as const,
		queryFn: () => listProjectExports(projectId, page, pageSize),
		refetchInterval: (query) =>
			query.state.data?.items.some(isExportActive)
				? EXPORT_POLL_INTERVAL_MS
				: false,
	});

/**
 * Rewrites storage download URLs that were signed for `http://localhost`
 * (no port) so they hit the same origin the SPA is on — e.g. `:3000` in
 * docker-compose.dev. Without this, Cursor's Simple Browser navigates away
 * from the portal when the export download starts.
 */
export function resolveDownloadUrl(url: string): string {
	try {
		const parsed = new URL(url, window.location.origin);
		const onDevPort =
			window.location.port !== "" &&
			window.location.port !== "80" &&
			window.location.port !== "443";
		if (
			parsed.pathname.startsWith("/storage/") &&
			(parsed.hostname === "localhost" || parsed.hostname === "127.0.0.1") &&
			parsed.port === "" &&
			onDevPort
		) {
			parsed.protocol = window.location.protocol;
			parsed.port = window.location.port;
		}
		return parsed.toString();
	} catch {
		return url;
	}
}

function fileNameFromUrl(url: string): string | undefined {
	try {
		const disposition = new URL(url).searchParams.get(
			"response-content-disposition",
		);
		if (!disposition) return undefined;
		const match = /filename\*?=(?:UTF-8''|")?([^\";]+)"?/i.exec(disposition);
		return match?.[1] ? decodeURIComponent(match[1]) : undefined;
	} catch {
		return undefined;
	}
}

/**
 * Downloads a presigned attachment URL without navigating the current tab.
 * Uses a blob + `a[download]` so embedded browsers (Cursor Simple Browser)
 * keep the SPA mounted; falls back to a new tab if fetch fails.
 */
export async function triggerDownload(url: string, fileName?: string) {
	const href = resolveDownloadUrl(url);
	const name = fileName ?? fileNameFromUrl(href) ?? "download";
	try {
		const res = await fetch(href);
		if (!res.ok) throw new Error(`download failed: ${res.status}`);
		const blob = await res.blob();
		const objectUrl = URL.createObjectURL(blob);
		const a = document.createElement("a");
		a.href = objectUrl;
		a.download = name;
		a.rel = "noopener";
		document.body.appendChild(a);
		a.click();
		a.remove();
		URL.revokeObjectURL(objectUrl);
	} catch {
		const a = document.createElement("a");
		a.href = href;
		a.rel = "noopener";
		a.target = "_blank";
		document.body.appendChild(a);
		a.click();
		a.remove();
	}
}
