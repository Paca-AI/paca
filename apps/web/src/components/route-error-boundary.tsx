import { AlertTriangle, Lock, RefreshCw } from "lucide-react";
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";
import { getHttpStatus, isForbiddenError } from "@/lib/api-error";

/**
 * Generic error fallback for TanStack Router route errors (loader failures,
 * render crashes in lazy-loaded route components, etc.).
 *
 * TanStack Router's `autoCodeSplitting` wraps route components in an internal
 * `Lazy` component. When a loader throws or a lazy chunk fails to resolve, the
 * error surfaces as "Element type is invalid… Check the render method of Lazy".
 * Providing an `errorComponent` / `defaultErrorComponent` short-circuits that
 * crash and renders this fallback instead.
 */
export function RouteErrorComponent({ error }: { error: Error }) {
	const { t } = useTranslation();

	// A loader forwards whatever its queryFn's axios call rejected with, so
	// `error` here carries the same response.status/data.error_code shape
	// used everywhere else — checked by HTTP status rather than message
	// text, unlike the not-found check below, since a permission error's
	// message is free server text, not something to pattern-match on.
	const isForbidden = isForbiddenError(error);
	const isNotFound =
		!isForbidden &&
		(getHttpStatus(error) === 404 ||
			error?.message?.toLowerCase().includes("not found") ||
			error?.message?.toLowerCase().includes("404"));

	return (
		<div className="flex flex-col h-full items-center justify-center gap-4 p-6">
			<div
				className={`flex size-12 items-center justify-center rounded-full ${
					isForbidden
						? "bg-amber-100 dark:bg-amber-900/20"
						: "bg-destructive/10"
				}`}
			>
				{isForbidden ? (
					<Lock className="size-6 text-amber-500 dark:text-amber-400" />
				) : (
					<AlertTriangle className="size-6 text-destructive" />
				)}
			</div>
			<div className="text-center space-y-1 max-w-sm">
				<p
					className={`text-sm font-medium ${
						isForbidden
							? "text-amber-700 dark:text-amber-400"
							: "text-destructive"
					}`}
				>
					{isForbidden
						? t(
								"common.noPermissionToView",
								"You don't have permission to view this",
							)
						: isNotFound
							? t("common.notFound", "Not found")
							: t("common.somethingWentWrong", "Something went wrong")}
				</p>
				{/* A permission error's own message ("insufficient permissions") adds
				    nothing beyond the title above — only shown for other error kinds,
				    where the raw server message can carry a genuinely useful detail. */}
				{!isForbidden && error?.message && (
					<p className="text-xs text-muted-foreground wrap-break-word">
						{error.message}
					</p>
				)}
			</div>
			{/* Retrying a permission error hits the same 403 again — nothing on
			    this page changed, so there's nothing for Retry to do. */}
			{!isForbidden && (
				<Button
					variant="outline"
					size="sm"
					className="gap-1.5"
					onClick={() => window.location.reload()}
				>
					<RefreshCw className="size-3.5" />
					{t("common.retry", "Retry")}
				</Button>
			)}
		</div>
	);
}
