import {
	type PublicKeyCredentialCreationOptionsJSON,
	type PublicKeyCredentialRequestOptionsJSON,
	startAuthentication,
	startRegistration,
} from "@simplewebauthn/browser";
import { queryOptions } from "@tanstack/react-query";

import { apiClient } from "./api-client";
import type { SuccessEnvelope } from "./api-error";

/** The signed-in user's MFA state. */
export interface MfaStatus {
	enabled: boolean;
	recovery_codes_left: number;
	/** False when the server has no relying-party identity (PUBLIC_URL). */
	passkeys_available: boolean;
}

/** A registered WebAuthn passkey. */
export interface Passkey {
	id: string;
	name: string;
	created_at: string;
	last_used_at: string | null;
}

/** A pending enrollment: the shared secret and the otpauth:// URI to render
 * as a QR code. */
export interface MfaEnrollment {
	secret: string;
	uri: string;
}

/** The instance login policy. The MFA requirement and the IP allow-list apply
 * to accounts holding any of `privileged_roles`; `sso_only` turns off password
 * sign-in for every account but the default super admin. */
export interface AuthSecurityPolicy {
	privileged_roles: string[];
	require_mfa: boolean;
	ip_allow_list: string[];
	sso_only: boolean;
}

/** What the (public) login page needs to know about the login policy. */
export interface LoginOptions {
	/** Only SSO sign-in is offered; the password form is kept for the
	 * default super admin behind a separate entry. */
	sso_only: boolean;
}

export const loginOptionsQueryOptions = queryOptions({
	queryKey: ["login-options"],
	queryFn: async () => {
		const { data } = await apiClient.instance.get<
			SuccessEnvelope<LoginOptions>
		>("/auth/login-options");
		return data.data;
	},
	staleTime: 60 * 1000,
});

export const mfaStatusQueryOptions = queryOptions({
	queryKey: ["auth", "mfa-status"],
	queryFn: async () => {
		const { data } =
			await apiClient.instance.get<SuccessEnvelope<MfaStatus>>("/users/me/mfa");
		return data.data;
	},
});

export async function beginMfaEnrollment(): Promise<MfaEnrollment> {
	const { data } = await apiClient.instance.post<
		SuccessEnvelope<MfaEnrollment>
	>("/users/me/mfa/enroll");
	return data.data;
}

/** Activates the pending enrollment; resolves to the one-time recovery codes. */
export async function confirmMfaEnrollment(code: string): Promise<string[]> {
	const { data } = await apiClient.instance.post<
		SuccessEnvelope<{ recovery_codes: string[] }>
	>("/users/me/mfa/confirm", { code });
	return data.data.recovery_codes;
}

/** Enrollment for an account that policy requires to use MFA but that cannot
 * sign in until it has: authenticated by password instead of a session. */
export async function beginLoginMfaSetup(
	username: string,
	password: string,
): Promise<MfaEnrollment> {
	const { data } = await apiClient.instance.post<
		SuccessEnvelope<MfaEnrollment>
	>("/auth/mfa/setup", { username, password });
	return data.data;
}

export async function confirmLoginMfaSetup(
	username: string,
	password: string,
	code: string,
): Promise<string[]> {
	const { data } = await apiClient.instance.post<
		SuccessEnvelope<{ recovery_codes: string[] }>
	>("/auth/mfa/setup/confirm", { username, password, code });
	return data.data.recovery_codes;
}

/** Passkey registration for an account that policy requires to use MFA but
 * that cannot sign in until it has: the whole ceremony, authenticated by
 * password instead of a session. */
export async function registerLoginPasskey(
	username: string,
	password: string,
	name: string,
): Promise<void> {
	const { data: begin } = await apiClient.instance.post<
		SuccessEnvelope<{
			options: { publicKey: PublicKeyCredentialCreationOptionsJSON };
		}>
	>("/auth/mfa/setup/passkey/options", { username, password });
	const credential = await startRegistration({
		optionsJSON: begin.data.options.publicKey,
	});
	await apiClient.instance.post("/auth/mfa/setup/passkey/finish", {
		username,
		password,
		name,
		credential,
	});
}

export async function disableMfa(code: string): Promise<void> {
	await apiClient.instance.post("/users/me/mfa/disable", { code });
}

export async function regenerateRecoveryCodes(code: string): Promise<string[]> {
	const { data } = await apiClient.instance.post<
		SuccessEnvelope<{ recovery_codes: string[] }>
	>("/users/me/mfa/recovery-codes", { code });
	return data.data.recovery_codes;
}

export const authSecurityPolicyQueryOptions = queryOptions({
	queryKey: ["admin", "auth-security"],
	queryFn: async () => {
		const { data } = await apiClient.instance.get<
			SuccessEnvelope<AuthSecurityPolicy>
		>("/admin/auth-security");
		return data.data;
	},
});

export async function saveAuthSecurityPolicy(
	policy: AuthSecurityPolicy,
): Promise<AuthSecurityPolicy> {
	const { data } = await apiClient.instance.put<
		SuccessEnvelope<AuthSecurityPolicy>
	>("/admin/auth-security", policy);
	return data.data;
}

/** Admin: removes a user's MFA enrollment (lost device and recovery codes). */
export async function resetUserMfa(userId: string): Promise<void> {
	await apiClient.instance.delete(`/admin/users/${userId}/mfa`);
}

export async function getUserAllowedCidrs(userId: string): Promise<string[]> {
	const { data } = await apiClient.instance.get<
		SuccessEnvelope<{ allowed_cidrs: string[] }>
	>(`/admin/users/${userId}/allowed-cidrs`);
	return data.data.allowed_cidrs;
}

export async function setUserAllowedCidrs(
	userId: string,
	cidrs: string[],
): Promise<string[]> {
	const { data } = await apiClient.instance.put<
		SuccessEnvelope<{ allowed_cidrs: string[] }>
	>(`/admin/users/${userId}/allowed-cidrs`, { allowed_cidrs: cidrs });
	return data.data.allowed_cidrs;
}

export const passkeysQueryOptions = queryOptions({
	queryKey: ["auth", "passkeys"],
	queryFn: async () => {
		const { data } =
			await apiClient.instance.get<SuccessEnvelope<Passkey[]>>(
				"/users/me/passkeys",
			);
		return data.data;
	},
});

/** Runs the whole registration ceremony: fetches options, asks the browser
 * to create the credential (this is where the OS prompt appears), and stores
 * the result under `name`. Rejects with the browser's own error (for example
 * `NotAllowedError` when the user dismisses the prompt) or an API error. */
export async function registerPasskey(name: string): Promise<Passkey> {
	const { data: begin } = await apiClient.instance.post<
		SuccessEnvelope<{
			options: { publicKey: PublicKeyCredentialCreationOptionsJSON };
		}>
	>("/users/me/passkeys/register/options");
	const credential = await startRegistration({
		optionsJSON: begin.data.options.publicKey,
	});
	const { data } = await apiClient.instance.post<SuccessEnvelope<Passkey>>(
		"/users/me/passkeys/register/finish",
		{ name, credential },
	);
	return data.data;
}

/** Removing a passkey needs proof of holding one, like turning TOTP off
 * needs a code: fetches a challenge, has the browser sign it (the OS prompt),
 * and sends the result with the delete. Rejects with the browser's own error
 * if the prompt is dismissed. */
export async function deletePasskey(id: string): Promise<void> {
	const { data } = await apiClient.instance.post<
		SuccessEnvelope<{
			options: { publicKey: PublicKeyCredentialRequestOptionsJSON };
		}>
	>("/users/me/passkeys/verify/options");
	const credential = await startAuthentication({
		optionsJSON: data.data.options.publicKey,
	});
	await apiClient.instance.delete(`/users/me/passkeys/${id}`, {
		data: { credential },
	});
}
