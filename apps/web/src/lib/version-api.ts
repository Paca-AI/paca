import { queryOptions } from "@tanstack/react-query";

import { apiClient } from "./api-client";
import type { SuccessEnvelope } from "./api-error";

// ── Shapes ────────────────────────────────────────────────────────────────────

export interface ReleaseInfo {
	repo: string;
	latest: string;
	url: string;
	hasUpdate: boolean;
}

export interface VersionInfo {
	current: string;
	upstream?: ReleaseInfo;
	fork?: ReleaseInfo;
}

// ── Query ───────────────────────────────────────────────────────────────────

export async function getVersion(): Promise<VersionInfo> {
	const { data } =
		await apiClient.instance.get<SuccessEnvelope<VersionInfo>>("/version");
	return data.data;
}

export const versionQueryOptions = () =>
	queryOptions({
		queryKey: ["version"],
		queryFn: getVersion,
		// The backend caches the GitHub response; keep the client copy fresh for
		// a while and don't refetch on every window focus.
		staleTime: 1000 * 60 * 30,
		refetchOnWindowFocus: false,
		retry: false,
	});
