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

	// User domain errors.
	UserNotFound: "USER_NOT_FOUND",
	UsernameTaken: "USER_USERNAME_TAKEN",
	Forbidden: "FORBIDDEN",

	// Generic / request errors.
	BadRequest: "BAD_REQUEST",
	InternalError: "INTERNAL_ERROR",
} as const;

export type ApiErrorCode = (typeof ApiErrorCode)[keyof typeof ApiErrorCode];

/** Shape of the error envelope returned by the API on failure. */
export interface ApiErrorEnvelope {
	success: false;
	error_code: ApiErrorCode;
	error: string;
	request_id?: string;
}

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
