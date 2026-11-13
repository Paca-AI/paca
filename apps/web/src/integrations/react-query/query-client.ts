import {
	type Mutation,
	MutationCache,
	QueryClient,
} from "@tanstack/react-query";
import { toast } from "@/components/ui/toast";
import i18n from "@/i18n";
import { notifyError } from "@/lib/notify";

function getHttpStatus(error: unknown): number | undefined {
	return (error as { response?: { status?: number } } | undefined)?.response
		?.status;
}

/**
 * Skips retrying 4xx responses (401/403/404/etc.) — those mean the request
 * was rejected for a reason another attempt won't fix (missing permission,
 * missing resource, bad input), unlike a 5xx or network failure, which might
 * genuinely succeed later. Without this, every permission-denied ("403
 * FORBIDDEN") query used React Query's default retry (3 attempts with
 * exponential backoff), so components kept hitting the API and sitting in
 * a loading state for several seconds before finally reaching their error
 * state — same request outcome, several seconds later.
 *
 * Exported for testing.
 */
export function shouldRetryQuery(
	failureCount: number,
	error: unknown,
): boolean {
	const status = getHttpStatus(error);
	if (status !== undefined && status >= 400 && status < 500) {
		return false;
	}
	return failureCount < 3;
}

/**
 * Global safety net for failed mutations. A mutation that handles its own
 * failure (it declares `onError`) or opts out with `meta: { silent: true }`
 * is left alone; every other one would otherwise fail without the user
 * seeing anything, so it gets an error toast.
 *
 * Exported for testing.
 */
export function handleMutationError(
	error: unknown,
	mutation: Mutation<unknown, unknown, unknown, unknown>,
): void {
	if (mutation.options.onError || mutation.meta?.silent) return;
	// 401 is handled by the auth flow (redirect to login), not a toast.
	if (getHttpStatus(error) === 401) return;
	notifyError(error);
}

export const SUCCESS_KINDS = [
	"saved",
	"updated",
	"created",
	"added",
	"deleted",
	"renamed",
	"removed",
	"started",
	"stopped",
	"requested",
	"resolved",
	"reopened",
	"activated",
	"deactivated",
	"completed",
] as const;
export type SuccessKind = (typeof SUCCESS_KINDS)[number];

export const SUCCESS_SUBJECTS = [
	"role",
	"defaultRole",
	"serviceAccount",
	"serviceAccountKey",
	"serviceAccountRoles",
	"branding",
	"loginSecurity",
	"ssoProvider",
	"user",
	"userSecurity",
	"userMfa",
	"document",
	"folder",
	"pluginPreferences",
	"agent",
	"agentSettings",
	"mcpServer",
	"skill",
	"envVar",
	"conversation",
	"comment",
	"task",
	"sshKey",
	"environment",
	"environmentSettings",
	"portForward",
	"view",
	"sprint",
	"attachment",
	"branch",
	"changeRequestLink",
	"branchLink",
	"taskLink",
	"customField",
	"project",
	"projectExport",
	"projectInfo",
	"gitRepository",
	"gitSettings",
	"jevCredentials",
	"jevSettings",
	"defaultTaskStatus",
	"defaultTaskType",
	"taskStatus",
	"taskType",
	"apiKey",
	"profile",
	"automation",
	"teamMember",
	"memberRoles",
	"memberDescription",
	"generic",
] as const;
export type SuccessSubject = (typeof SUCCESS_SUBJECTS)[number];

/**
 * Opt-in success confirmation: a mutation declaring
 * `meta: { success: "updated", subject: "projectInfo" }` gets a toast reading
 * "Project information updated successfully". Opt-in because most mutations
 * (drag reorders, autosaves, background syncs) would be noise as toasts.
 *
 * Exported for testing.
 */
export function handleMutationSuccess(
	mutation: Mutation<unknown, unknown, unknown, unknown>,
): void {
	const kind = mutation.meta?.success;
	if (
		typeof kind !== "string" ||
		!SUCCESS_KINDS.includes(kind as SuccessKind)
	) {
		return;
	}
	const rawSubject = mutation.meta?.subject;
	const subject: SuccessSubject =
		typeof rawSubject === "string" &&
		SUCCESS_SUBJECTS.includes(rawSubject as SuccessSubject)
			? (rawSubject as SuccessSubject)
			: "generic";
	toast.add({
		type: "success",
		title: i18n.t(`common.toast.${kind as SuccessKind}`, {
			subject: i18n.t(`common.toast.subjects.${subject}`),
		}),
	});
}

export const queryClient = new QueryClient({
	mutationCache: new MutationCache({
		onSuccess: (_data, _variables, _result, mutation) =>
			handleMutationSuccess(mutation),
		onError: (error, _variables, _result, mutation) =>
			handleMutationError(error, mutation),
	}),
	defaultOptions: {
		queries: {
			retry: shouldRetryQuery,
		},
	},
});
