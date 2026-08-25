import { useQuery } from "@tanstack/react-query";
import { TerminalSquare, X } from "lucide-react";
import { useTranslation } from "react-i18next";
import { Skeleton } from "@/components/ui/skeleton";
import { environmentQueryOptions } from "@/lib/environment-api";
import { EnvironmentTerminal } from "./environment-terminal";

// The full-page browser terminal — opened in a new tab from the Connect
// page's "web app" tab (environment-connect.tsx) rather than embedded as a
// small panel inside environment-detail.tsx's own tab strip, per
// docs/ai-agent/environment-management.md's Terminal / SSH Access section:
// a real working session deserves the whole tab, not a 480px box. Renders
// as a fixed overlay covering the entire viewport (including this app's
// own sidebar) rather than living outside the `_authenticated` route
// layout — visually identical to a standalone page, without needing a
// second auth/layout boundary for one route.
export function EnvironmentTerminalPage({
	projectId,
	environmentId,
}: {
	projectId: string;
	environmentId: string;
}) {
	const { t } = useTranslation("projects");
	const { data: environment } = useQuery(
		environmentQueryOptions(projectId, environmentId),
	);

	return (
		<div className="fixed inset-0 z-50 flex flex-col bg-background">
			<div className="flex items-center justify-between gap-3 border-b border-border/50 px-4 py-3 shrink-0">
				<div className="flex items-center gap-2 min-w-0">
					<TerminalSquare className="size-4 text-primary shrink-0" />
					<span className="text-sm font-medium truncate">
						{environment
							? t("environments.connect.webApp.pageTitle", {
									name: environment.name,
								})
							: t("environments.connect.webApp.pageTitleLoading")}
					</span>
				</div>
				<button
					type="button"
					onClick={() => window.close()}
					className="flex items-center gap-1.5 text-sm text-muted-foreground hover:text-foreground"
				>
					<X className="size-4" />
					{t("environments.connect.webApp.closeTab")}
				</button>
			</div>
			<div className="flex-1 min-h-0 p-3">
				{!environment ? (
					<Skeleton className="h-full w-full rounded-lg" />
				) : environment.status !== "running" ? (
					<div className="flex h-full flex-col items-center justify-center gap-3 rounded-xl border border-dashed border-border">
						<TerminalSquare className="size-8 text-muted-foreground/40" />
						<p className="text-sm text-muted-foreground">
							{t("environments.detail.terminal.notRunning")}
						</p>
					</div>
				) : (
					<EnvironmentTerminal
						projectId={projectId}
						environmentId={environmentId}
					/>
				)}
			</div>
		</div>
	);
}
