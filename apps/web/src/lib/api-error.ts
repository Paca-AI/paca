/**
 * Machine-readable error codes returned by the API in error envelopes.
 * Switch on these values instead of HTTP status codes or message strings,
 * as messages are subject to change.
 */
export const ApiErrorCode = {
	// Authentication / token errors.
	InvalidCredentials: "AUTH_INVALID_CREDENTIALS",
	MissingToken: "AUTH_MISSING_TOKEN",
	TokenInvalid: "AUTH_TOKEN_INVALID",
	Unauthenticated: "AUTH_UNAUTHENTICATED",

	// Password / session gate errors.
	PasswordChangeRequired: "AUTH_PASSWORD_CHANGE_REQUIRED",

	// User domain errors.
	UserNotFound: "USER_NOT_FOUND",
	UsernameTaken: "USER_USERNAME_TAKEN",
	InvalidCurrentPassword: "USER_INVALID_CURRENT_PASSWORD",
	Forbidden: "FORBIDDEN",

	// Global role domain errors.
	GlobalRoleNotFound: "GLOBAL_ROLE_NOT_FOUND",
	GlobalRoleNameTaken: "GLOBAL_ROLE_NAME_TAKEN",
	GlobalRoleNameInvalid: "GLOBAL_ROLE_NAME_INVALID",
	GlobalRoleHasUsers: "GLOBAL_ROLE_HAS_ASSIGNED_USERS",

	// Project domain errors.
	ProjectNotFound: "PROJECT_NOT_FOUND",
	ProjectNameTaken: "PROJECT_NAME_TAKEN",
	ProjectNameInvalid: "PROJECT_NAME_INVALID",
	ProjectRoleNotFound: "PROJECT_ROLE_NOT_FOUND",
	ProjectRoleNameTaken: "PROJECT_ROLE_NAME_TAKEN",
	ProjectRoleNameInvalid: "PROJECT_ROLE_NAME_INVALID",
	ProjectRoleHasMembers: "PROJECT_ROLE_HAS_MEMBERS",
	ProjectMemberNotFound: "PROJECT_MEMBER_NOT_FOUND",
	ProjectMemberAlreadyAdded: "PROJECT_MEMBER_ALREADY_ADDED",

	// Generic / request errors.
	BadRequest: "BAD_REQUEST",
	InternalError: "INTERNAL_ERROR",
} as const;

export type ApiErrorCode = (typeof ApiErrorCode)[keyof typeof ApiErrorCode];

/** Returns true when an Axios error is a 403 AUTH_PASSWORD_CHANGE_REQUIRED. */
export function isPasswordChangeRequired(err: unknown): boolean {
	const e = err as {
		response?: { status?: number; data?: { error_code?: string } };
	};
	return (
		e?.response?.status === 403 &&
		e?.response?.data?.error_code === ApiErrorCode.PasswordChangeRequired
	);
}

/** Shape of the success envelope returned by the API on success. */
export interface SuccessEnvelope<T> {
	success: true;
	data: T;
	request_id?: string;
}

/** Shape of the error envelope returned by the API on failure. */
export interface ApiErrorEnvelope {
	success: false;
	error_code: ApiErrorCode;
	error: string;
	request_id?: string;
}

/** Discriminated union of all possible API response envelopes. */
export type ApiEnvelope<T> = SuccessEnvelope<T> | ApiErrorEnvelope;

/**
 * Extracts the `error_code` from an Axios error response.
 * Returns `null` when the error is not an API error envelope.
 */
export function getApiErrorCode(error: unknown): ApiErrorCode | null {
	const err = error as {
		response?: { data?: { error_code?: string } };
	};
	const code = err?.response?.data?.error_code;
	if (!code) return null;
	const known = Object.values(ApiErrorCode) as string[];
	return known.includes(code) ? (code as ApiErrorCode) : null;
}
