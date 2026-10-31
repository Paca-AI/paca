import { queryOptions } from "@tanstack/react-query";

import { apiClient } from "./api-client";
import type { SuccessEnvelope } from "./api-error";

/** The signed-in user's MFA state. */
export interface MfaStatus {
	enabled: boolean;
	recovery_codes_left: number;
}

/** A pending enrollment: the shared secret and the otpauth:// URI to render
 * as a QR code. */
export interface MfaEnrollment {
	secret: string;
	uri: string;
}

/** The instance login policy. It applies to accounts holding any of
 * `privileged_roles`. */
export interface AuthSecurityPolicy {
	privileged_roles: string[];
	require_mfa: boolean;
	ip_allow_list: string[];
}

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
