import { useQuery } from "@tanstack/react-query";
import { GitBranch } from "lucide-react";
import { useTranslation } from "react-i18next";
import { automationDependencyMapQueryOptions } from "@/lib/automation-api";

interface AutomationDependencyMapProps {
	projectId: string;
}

export function AutomationDependencyMap({
	projectId,
}: AutomationDependencyMapProps) {
	const { t } = useTranslation("projects");
	const { data: entries = [], isLoading } = useQuery(
		automationDependencyMapQueryOptions(projectId),
	);

	return (
		<div className="rounded-xl border border-border/60 bg-card p-4">
			<div className="flex items-center gap-2 mb-1">
				<GitBranch className="size-4 text-muted-foreground" />
				<h2 className="text-sm font-semibold">
					{t("automation.dependencyMap.title")}
				</h2>
			</div>
			<p className="text-xs text-muted-foreground mb-3">
				{t("automation.dependencyMap.subtitle")}
			</p>
			{isLoading ? (
				<div className="text-xs text-muted-foreground">…</div>
			) : entries.length === 0 ? (
				<div className="text-xs text-muted-foreground">
					{t("automation.dependencyMap.empty")}
				</div>
			) : (
				<div className="space-y-2">
					{entries.map((entry) => (
						<div
							key={entry.node_id}
							className="flex items-center justify-between rounded-lg border border-border/40 px-3 py-2 text-xs"
						>
							<span className="font-medium">{entry.automation_name}</span>
							<span className="text-muted-foreground">
								{t("automation.dependencyMap.watchedTasks", {
									count: entry.watched_task_ids.length,
								})}
							</span>
						</div>
					))}
				</div>
			)}
		</div>
	);
}
