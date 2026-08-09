import type { QueryClient } from "@tanstack/react-query";
import { queryOptions } from "@tanstack/react-query";

import { apiClient } from "./api-client";
import type { SuccessEnvelope } from "./api-error";

// Logo/favicon upload themselves go through the existing generic
// uploadAvatar(basePath, file) / removeAvatar(basePath) in avatar-api.ts —
// basePath is "/admin/settings/logo" or "/admin/settings/favicon". This file
// only covers the public branding read and the brand name/primary-color
// write, which don't fit that generic avatar flow.

export interface BrandingResponse {
	logo_url?: string | null;
	logo_thumb_url?: string | null;
	favicon_url?: string | null;
	favicon_thumb_url?: string | null;
	brand_name?: string | null;
	primary_color_light?: string | null;
	primary_color_dark?: string | null;
}

// Branding drives CSS variables/favicon/title applied on every page load —
// without a persisted cache, a hard reload always shows default branding
// for one network round-trip before the GET below resolves. Caching the
// last-fetched response in localStorage lets brandingQueryOptions' initial
// render use it immediately (see initialData below); staleTime: 0 then
// forces a background refetch right after mount so the cache never goes
// stale for long.
const BRANDING_CACHE_KEY = "paca:branding-cache";

function readCachedBranding(): BrandingResponse | undefined {
	try {
		const raw = window.localStorage.getItem(BRANDING_CACHE_KEY);
		return raw ? (JSON.parse(raw) as BrandingResponse) : undefined;
	} catch {
		return undefined;
	}
}

function writeCachedBranding(data: BrandingResponse): void {
	try {
		window.localStorage.setItem(BRANDING_CACHE_KEY, JSON.stringify(data));
	} catch {
		// best-effort — private browsing / storage quota failures are fine to ignore
	}
}

export async function getBranding(): Promise<BrandingResponse> {
	const { data } =
		await apiClient.instance.get<SuccessEnvelope<BrandingResponse>>(
			"/branding",
		);
	writeCachedBranding(data.data);
	return data.data;
}

export async function updateSettings(payload: {
	brand_name: string | null;
	primary_color_light: string | null;
	primary_color_dark: string | null;
}): Promise<BrandingResponse> {
	const { data } = await apiClient.instance.patch<
		SuccessEnvelope<BrandingResponse>
	>("/admin/settings", payload);
	return data.data;
}

export const brandingQueryOptions = queryOptions({
	queryKey: ["branding"],
	queryFn: getBranding,
	// Paint immediately from the last-fetched response instead of defaults…
	initialData: readCachedBranding,
	// …then always refetch in the background right after mount (default
	// refetchOnMount behavior) so that cache never stays stale for long.
	staleTime: 0,
});

// Locally patching the React Query branding cache (e.g. after a settings
// PATCH or an avatar-upload response, both of which return only the fields
// that changed) without also updating the localStorage cache would leave
// that cache holding a stale snapshot: a hard reload right after such a
// change would call readCachedBranding() above and briefly paint the old
// logo/brand name/color for one round-trip before the background refetch
// lands. Route every such local patch through this helper instead of
// queryClient.setQueryData directly so the two caches can't drift apart.
export function setBrandingQueryData(
	queryClient: QueryClient,
	updater: (old: BrandingResponse | undefined) => BrandingResponse | undefined,
): void {
	queryClient.setQueryData<BrandingResponse>(
		brandingQueryOptions.queryKey,
		(old) => {
			const next = updater(old);
			if (next) writeCachedBranding(next);
			return next;
		},
	);
}
