import { Bug } from "lucide-react";
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";
import { isForbiddenError } from "@/lib/api-error";
import { draftFromError, openIssue } from "@/lib/feedback";
import { isFeedbackEnabledCached } from "@/lib/settings-api";

/**
 * Opens GitHub's bug-report form pre-filled with the full error (message,
 * stack, failed request/response, page path, browser). Opens GitHub directly
 * rather than through a dialog so it also works where no app provider is
 * mounted (e.g. the root route's own error screen).
 *
 * Renders nothing for a permission (403) error (that's an access decision,
 * not something to report) or when an admin has turned feedback off.
 */
export function ReportIssueButton({
	error,
	className,
}: {
	error: unknown;
	className?: string;
}) {
	const { t } = useTranslation();
	if (!isFeedbackEnabledCached() || isForbiddenError(error)) return null;
	return (
		<Button
			type="button"
			variant="outline"
			size="sm"
			className={className ?? "gap-1.5"}
			onClick={() => openIssue(draftFromError(error))}
		>
			<Bug className="size-3.5" />
			{t("common.reportIssue", "Report this issue")}
		</Button>
	);
}
