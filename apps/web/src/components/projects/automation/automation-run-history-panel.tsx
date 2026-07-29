import { useQuery } from "@tanstack/react-query";
import {
	CheckCircle2,
	ChevronDown,
	ChevronRight,
	Circle,
	XCircle,
} from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import {
	type AutomationRun,
	automationRunStepsQueryOptions,
} from "@/lib/automation-api";
import { timeAgo } from "@/lib/time-ago";
import { cn } from "@/lib/utils";

interface AutomationRunHistoryPanelProps {
	projectId: string;
	automationId: string;
	runs: AutomationRun[];
	nodeLabel: (nodeId: string) => string;
}

const RUN_STATUS_ICON = {
	running: Circle,
	completed: CheckCircle2,
	failed: XCircle,
};

const STEP_STATUS_ICON = {
	completed: CheckCircle2,
	failed: XCircle,
	skipped: Circle,
};

export function AutomationRunHistoryPanel({
	projectId,
	automationId,
	runs,
	nodeLabel,
}: AutomationRunHistoryPanelProps) {
	const { t } = useTranslation("projects");
	const { t: tCommon } = useTranslation("common");
	const [expandedRunId, setExpandedRunId] = useState<string | null>(null);

	if (runs.length === 0) {
		return (
			<div className="flex flex-1 items-center justify-center p-10 text-sm text-muted-foreground">
				{t("automation.runHistory.empty")}
			</div>
		);
	}

	return (
		<div className="flex-1 overflow-y-auto p-4 space-y-2">
			{runs.map((run) => {
				const Icon = RUN_STATUS_ICON[run.status];
				const expanded = expandedRunId === run.id;
				return (
					<div
						key={run.id}
						className="rounded-lg border border-border/60 bg-card overflow-hidden"
					>
						<button
							type="button"
							onClick={() => setExpandedRunId(expanded ? null : run.id)}
							className="flex w-full items-center gap-2 px-3 py-2.5 text-left hover:bg-muted/40"
						>
							{expanded ? (
								<ChevronDown className="size-3.5 shrink-0 text-muted-foreground" />
							) : (
								<ChevronRight className="size-3.5 shrink-0 text-muted-foreground" />
							)}
							<Icon
								className={cn(
									"size-3.5 shrink-0",
									run.status === "completed" && "text-green-600",
									run.status === "failed" && "text-destructive",
									run.status === "running" &&
										"text-muted-foreground animate-pulse",
								)}
							/>
							<span className="text-xs font-medium flex-1">
								{t(`automation.runHistory.status.${run.status}`)}
							</span>
							<span className="text-xs text-muted-foreground">
								{t("automation.runHistory.startedAt", {
									time: timeAgo(run.started_at, tCommon),
								})}
							</span>
						</button>
						{expanded && (
							<RunSteps
								projectId={projectId}
								automationId={automationId}
								runId={run.id}
								nodeLabel={nodeLabel}
							/>
						)}
					</div>
				);
			})}
		</div>
	);
}

function RunSteps({
	projectId,
	automationId,
	runId,
	nodeLabel,
}: {
	projectId: string;
	automationId: string;
	runId: string;
	nodeLabel: (nodeId: string) => string;
}) {
	const { t } = useTranslation("projects");
	const { data: steps = [], isLoading } = useQuery(
		automationRunStepsQueryOptions(projectId, automationId, runId),
	);

	if (isLoading) {
		return <div className="px-3 py-2 text-xs text-muted-foreground">…</div>;
	}

	return (
		<div className="border-t border-border/60 divide-y divide-border/40">
			{steps.map((step) => {
				const Icon = STEP_STATUS_ICON[step.status];
				return (
					<div key={step.id} className="flex items-center gap-2 px-3 py-2">
						<Icon
							className={cn(
								"size-3 shrink-0",
								step.status === "completed" && "text-green-600",
								step.status === "failed" && "text-destructive",
								step.status === "skipped" && "text-muted-foreground",
							)}
						/>
						<span className="text-xs flex-1 truncate">
							{nodeLabel(step.node_id)}
						</span>
						<span className="text-[10px] font-mono text-muted-foreground">
							{t(`automation.runHistory.stepStatus.${step.status}`)}
						</span>
					</div>
				);
			})}
		</div>
	);
}
