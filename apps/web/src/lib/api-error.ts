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
	ProjectPrefixInvalid: "PROJECT_PREFIX_INVALID",
	ProjectRoleNotFound: "PROJECT_ROLE_NOT_FOUND",
	ProjectRoleNameTaken: "PROJECT_ROLE_NAME_TAKEN",
	ProjectRoleNameInvalid: "PROJECT_ROLE_NAME_INVALID",
	ProjectRoleHasMembers: "PROJECT_ROLE_HAS_MEMBERS",
	ProjectMemberNotFound: "PROJECT_MEMBER_NOT_FOUND",
	ProjectMemberAlreadyAdded: "PROJECT_MEMBER_ALREADY_ADDED",

	// Task type domain errors.
	TaskTypeNotFound: "TASK_TYPE_NOT_FOUND",
	TaskTypeNameInvalid: "TASK_TYPE_NAME_INVALID",
	TaskTypeIsSystem: "TASK_TYPE_IS_SYSTEM",
	TaskTypeNameReserved: "TASK_TYPE_NAME_RESERVED",

	// Task status domain errors.
	TaskStatusNotFound: "TASK_STATUS_NOT_FOUND",
	TaskStatusNameInvalid: "TASK_STATUS_NAME_INVALID",
	TaskStatusCategoryInvalid: "TASK_STATUS_CATEGORY_INVALID",
	TaskStatusReorderInvalid: "TASK_STATUS_REORDER_INVALID",

	// Task domain errors.
	TaskNotFound: "TASK_NOT_FOUND",
	TaskTitleInvalid: "TASK_TITLE_INVALID",

	// Custom field domain errors.
	CustomFieldNotFound: "CUSTOM_FIELD_NOT_FOUND",
	CustomFieldKeyInvalid: "CUSTOM_FIELD_KEY_INVALID",
	CustomFieldKeyTaken: "CUSTOM_FIELD_KEY_TAKEN",
	CustomFieldTypeInvalid: "CUSTOM_FIELD_TYPE_INVALID",
	CustomFieldNameInvalid: "CUSTOM_FIELD_NAME_INVALID",

	// GitHub integration errors.
	GitHubIntegrationNotFound: "GITHUB_INTEGRATION_NOT_FOUND",
	GitHubRepositoryNotFound: "GITHUB_REPOSITORY_NOT_FOUND",
	GitHubPRNotFound: "GITHUB_PR_NOT_FOUND",
	GitHubPRLinkNotFound: "GITHUB_PR_LINK_NOT_FOUND",
	GitHubPRAlreadyLinked: "GITHUB_PR_ALREADY_LINKED",
	GitHubInvalidToken: "GITHUB_INVALID_TOKEN",
	GitHubWebhookURLRequired: "GITHUB_WEBHOOK_URL_REQUIRED",
	GitHubRepoNotAccessible: "GITHUB_REPO_NOT_ACCESSIBLE",
	GitHubRepoAlreadyLinked: "GITHUB_REPO_ALREADY_LINKED",
	GitHubWebhookCreationFailed: "GITHUB_WEBHOOK_CREATION_FAILED",
	GitHubWebhookURLNotPublic: "GITHUB_WEBHOOK_URL_NOT_PUBLIC",
	GitHubBranchAlreadyLinked: "GITHUB_BRANCH_ALREADY_LINKED",
	GitHubTokenInsufficientPermissions: "GITHUB_TOKEN_INSUFFICIENT_PERMISSIONS",

	// Plugin domain errors.
	PluginNotFound: "PLUGIN_NOT_FOUND",
	PluginNameTaken: "PLUGIN_NAME_TAKEN",
	PluginAlreadyUpToDate: "PLUGIN_ALREADY_UP_TO_DATE",
	PluginDowngradeNotAllowed: "PLUGIN_DOWNGRADE_NOT_ALLOWED",
	PluginIncompatibleHostVersion: "PLUGIN_INCOMPATIBLE_HOST_VERSION",

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
	/**
	 * Structured, non-localized values for the handful of error codes that
	 * need them (e.g. `PLUGIN_INCOMPATIBLE_HOST_VERSION` carries
	 * `required_version`/`host_version`) — see `getApiErrorDetails`.
	 */
	error_details?: Record<string, string>;
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

/**
 * Extracts the human-readable `error` message from an API error envelope.
 * This is free-text set by the server and is never localized — prefer
 * `getApiErrorDetails` plus a translated string with interpolation when the
 * code supports it, and fall back to this only for unrecognized codes.
 * Returns `null` when the error is not an API error envelope.
 */
export function getApiErrorMessage(error: unknown): string | null {
	const err = error as {
		response?: { data?: { error?: string } };
	};
	return err?.response?.data?.error ?? null;
}

/**
 * Extracts the structured `error_details` map from an API error envelope
 * (see `ApiErrorEnvelope.error_details`) — values here are plain data (IDs,
 * version numbers), never translated text, so they're safe to interpolate
 * into a locally translated string. Returns `null` when absent.
 */
export function getApiErrorDetails(
	error: unknown,
): Record<string, string> | null {
	const err = error as {
		response?: { data?: { error_details?: Record<string, string> } };
	};
	return err?.response?.data?.error_details ?? null;
}
