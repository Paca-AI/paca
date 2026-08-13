import { queryOptions } from "@tanstack/react-query";

import { apiClient } from "./api-client";
import type { SuccessEnvelope } from "./api-error";

// SMTP e-mail settings. The password is never returned by the API —
// password_set only reports whether one is stored.
export interface EmailSettings {
	from_email: string;
	from_name: string;
	host: string;
	port: number;
	username: string;
	use_ssl: boolean;
	use_tls: boolean;
	skip_verify: boolean;
	send_user_created_email: boolean;
	password_set: boolean;
	configured: boolean;
}

export async function getEmailSettings(): Promise<EmailSettings> {
	const { data } = await apiClient.instance.get<SuccessEnvelope<EmailSettings>>(
		"/admin/settings/email",
	);
	return data.data;
}

export interface UpdateEmailSettingsPayload {
	from_email: string;
	from_name: string;
	host: string;
	port: number;
	username: string;
	// password is tri-state: omit/undefined keeps the stored password, ""
	// clears it, any other value replaces it.
	password?: string;
	use_ssl: boolean;
	use_tls: boolean;
	skip_verify: boolean;
	send_user_created_email: boolean;
}

export async function updateEmailSettings(
	payload: UpdateEmailSettingsPayload,
): Promise<EmailSettings> {
	const { data } = await apiClient.instance.patch<
		SuccessEnvelope<EmailSettings>
	>("/admin/settings/email", payload);
	return data.data;
}

export async function sendTestEmail(to: string): Promise<void> {
	await apiClient.instance.post("/admin/settings/email/test", { to });
}

export const emailSettingsQueryOptions = queryOptions({
	queryKey: ["admin", "email-settings"],
	queryFn: getEmailSettings,
	staleTime: 0,
});
