import { queryOptions } from "@tanstack/react-query";

import { apiClient } from "./api-client";
import type { SuccessEnvelope } from "./api-error";

export interface SSOSettings {
	source: "environment" | "database";
	enabled: boolean;
	issuer_url: string;
	client_id: string;
	client_secret_configured: boolean;
	scopes: string[];
	redirect_url: string;
	display_name: string;
	username_claim: string;
	local_login_enabled: boolean;
	encrypted_secret_storage_available: boolean;
}

export interface UpdateSSOSettings {
	enabled: boolean;
	issuer_url: string;
	client_id: string;
	client_secret?: string;
	scopes: string[];
	redirect_url: string;
	display_name: string;
	username_claim: string;
	local_login_enabled: boolean;
}

export async function getSSOSettings(): Promise<SSOSettings> {
	const { data } = await apiClient.instance.get<SuccessEnvelope<SSOSettings>>(
		"/admin/settings/sso",
	);
	return data.data;
}

export async function updateSSOSettings(
	payload: UpdateSSOSettings,
): Promise<SSOSettings> {
	const { data } = await apiClient.instance.patch<SuccessEnvelope<SSOSettings>>(
		"/admin/settings/sso",
		payload,
	);
	return data.data;
}

export const ssoSettingsQueryOptions = queryOptions({
	queryKey: ["admin", "settings", "sso"],
	queryFn: getSSOSettings,
});
