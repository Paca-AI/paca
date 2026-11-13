import { toast } from "@/components/ui/toast";
import i18n from "@/i18n";
import { getApiErrorMessage, isForbiddenError } from "@/lib/api-error";
import { draftFromError, openIssue } from "@/lib/feedback";
import { isFeedbackEnabledCached } from "@/lib/settings-api";

/**
 * Error toast. When there is an underlying `error` it also offers a
 * "Report this issue" button that opens a pre-filled GitHub issue (same
 * feature as the error screens) — unless an admin turned feedback off, or the
 * error is a 403, which is an access decision rather than a bug.
 *
 * `title` is the translated headline; `description` defaults to the server's
 * message, if any.
 */
export function showErrorToast({
	title,
	description,
	error,
}: {
	title?: string;
	description?: string;
	error?: unknown;
}): void {
	const canReport =
		error !== undefined &&
		isFeedbackEnabledCached() &&
		!isForbiddenError(error);
	// An explicit empty description suppresses the server message (used when
	// the title already is that message).
	const detail =
		description !== undefined
			? description || undefined
			: error !== undefined
				? (getApiErrorMessage(error) ?? undefined)
				: undefined;
	toast.add({
		type: "error",
		title: title ?? i18n.t("common.somethingWentWrong"),
		description: detail,
		timeout: 8000,
		...(canReport
			? {
					actionProps: {
						children: i18n.t("common.reportIssue"),
						onClick: () => openIssue(draftFromError(error)),
					},
				}
			: {}),
	});
}

/**
 * Error toast for components that handle a failure themselves (their own
 * `onError`) but have no form or dialog to show it in.
 */
export function notifyError(error: unknown, title?: string): void {
	showErrorToast({ title, error });
}
