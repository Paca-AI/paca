import { ApiErrorCode, getApiErrorCode } from "@/lib/api-error";

/** Translation keys (roles namespace, errors.*) for the API errors a role
 *  save, delete or assignment can fail with. */
const KEY_BY_CODE: Partial<Record<string, string>> = {
	[ApiErrorCode.RoleNotFound]: "notFound",
	[ApiErrorCode.RoleNameTaken]: "nameTaken",
	[ApiErrorCode.RoleNameInvalid]: "nameInvalid",
	[ApiErrorCode.RolePolicyInvalid]: "policyInvalid",
	[ApiErrorCode.RoleIsSystem]: "isSystem",
	[ApiErrorCode.RoleIsDefault]: "isDefault",
	[ApiErrorCode.RoleLastFullAccess]: "lastFullAccess",
	[ApiErrorCode.RoleNotAttachable]: "notAttachable",
	[ApiErrorCode.RoleNoDefault]: "noDefault",
	[ApiErrorCode.RoleRequired]: "required",
	[ApiErrorCode.Forbidden]: "forbidden",
	[ApiErrorCode.InternalError]: "internal",
};

/** The `roles` errors.* key for an API error, or "generic". */
export function roleErrorKey(err: unknown): string {
	const code = getApiErrorCode(err);
	return (code && KEY_BY_CODE[code]) || "generic";
}
