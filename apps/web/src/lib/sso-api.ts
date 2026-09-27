import { queryOptions } from "@tanstack/react-query";

import { apiClient } from "./api-client";
import type { SuccessEnvelope } from "./api-error";

/** An enabled provider, as the (public) login page lists it. */
export interface SsoPublicProvider {
	slug: string;
	display_name: string;
}

/** A provider as the admin settings page sees it. The client secret is
 * never returned — only whether one is set. */
export interface SsoProvider {
	id: string;
	slug: string;
	display_name: string;
	issuer_url: string;
	client_id: string;
	has_client_secret: boolean;
	scopes: string[];
	enabled: boolean;
	auto_provision: boolean;
	link_by_email: boolean;
	allowed_domains: string[];
	/** The redirect URI to register at the provider. */
	callback_url: string;
	created_at: string;
	updated_at: string;
}

/** Create/update body. On update, a null client_secret keeps the stored one. */
export interface SsoProviderInput {
	slug: string;
	display_name: string;
	issuer_url: string;
	client_id: string;
	client_secret: string | null;
	scopes: string[];
	enabled: boolean;
	auto_provision: boolean;
	link_by_email: boolean;
	allowed_domains: string[];
}

export async function listPublicSsoProviders(): Promise<SsoPublicProvider[]> {
	const { data } = await apiClient.instance.get<
		SuccessEnvelope<SsoPublicProvider[]>
	>("/auth/sso/providers");
	return data.data;
}

export const publicSsoProvidersQueryOptions = queryOptions({
	queryKey: ["sso", "public-providers"],
	queryFn: listPublicSsoProviders,
	staleTime: 60 * 1000,
});

/**
 * URL that starts an SSO sign-in. It is a full-page navigation (the API
 * redirects to the provider and back), never an XHR.
 */
export function ssoLoginUrl(
	slug: string,
	rememberMe: boolean,
	redirect?: string,
): string {
	const params = new URLSearchParams({ remember_me: String(rememberMe) });
	if (redirect) params.set("redirect", redirect);
	return `/api/v1/auth/sso/${encodeURIComponent(slug)}/login?${params}`;
}

export async function listSsoProviders(): Promise<SsoProvider[]> {
	const { data } = await apiClient.instance.get<SuccessEnvelope<SsoProvider[]>>(
		"/admin/sso/providers",
	);
	return data.data;
}

export const ssoProvidersQueryOptions = queryOptions({
	queryKey: ["admin", "sso-providers"],
	queryFn: listSsoProviders,
});

export async function createSsoProvider(
	input: SsoProviderInput,
): Promise<SsoProvider> {
	const { data } = await apiClient.instance.post<SuccessEnvelope<SsoProvider>>(
		"/admin/sso/providers",
		input,
	);
	return data.data;
}

export async function updateSsoProvider(
	id: string,
	input: SsoProviderInput,
): Promise<SsoProvider> {
	const { data } = await apiClient.instance.put<SuccessEnvelope<SsoProvider>>(
		`/admin/sso/providers/${id}`,
		input,
	);
	return data.data;
}

export async function deleteSsoProvider(id: string): Promise<void> {
	await apiClient.instance.delete(`/admin/sso/providers/${id}`);
}
