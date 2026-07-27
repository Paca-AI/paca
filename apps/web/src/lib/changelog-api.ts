import { queryOptions } from "@tanstack/react-query";

import { apiClient } from "./api-client";
import type { SuccessEnvelope } from "./api-error";

// ── Shapes ────────────────────────────────────────────────────────────────────

export interface ReleaseEntry {
	tag: string;
	name: string;
	url: string;
	publishedAt: string;
	body: string;
	isCurrent: boolean;
}

export interface ReleasesResponse {
	current: string;
	repo: string;
	releases: ReleaseEntry[];
}

// ── Query ───────────────────────────────────────────────────────────────────

export async function getReleases(): Promise<ReleasesResponse> {
	const { data } =
		await apiClient.instance.get<SuccessEnvelope<ReleasesResponse>>(
			"/releases",
		);
	return data.data;
}

export const releasesQueryOptions = () =>
	queryOptions({
		queryKey: ["releases"],
		queryFn: getReleases,
		// The backend caches the GitHub response; keep the client copy fresh for
		// a while and avoid refetching on every window focus.
		staleTime: 1000 * 60 * 30,
		refetchOnWindowFocus: false,
		retry: false,
	});
