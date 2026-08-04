import { useInfiniteQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { Activity, CheckSquare, FileText } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { AgentActivityFilters } from "@/components/projects/agents/agent-activity-filters";
import { activityDescription } from "@/components/projects/interactions/task-detail/activity-item";
import { Badge } from "@/components/ui/badge";
import { Skeleton } from "@/components/ui/skeleton";
import {
	type AgentActivity,
	type AgentActivityFilters as AgentActivityFiltersState,
	agentActivitiesQueryOptions,
} from "@/lib/agent-api";
import type { DocActivityContent, DocActivityType } from "@/lib/doc-api";
import { timeAgo } from "@/lib/time-ago";
import { describeDocActivity } from "../docs/doc-activity-pane";

const EMPTY_NAME_MAPS = { members: {}, sprints: {} };

function describeItem(
	item: AgentActivity,
	t: ReturnType<typeof useTranslation<"projects">>["t"],
): string {
	if (item.activity_type === "comment") {
		return t("agents.detail.activity.commented");
	}
	if (item.source_type === "task") {
		return activityDescription(
			{
				activity_type: item.activity_type,
				content: item.content as Record<string, unknown> | unknown[],
			},
			EMPTY_NAME_MAPS,
			t,
		);
	}
	return describeDocActivity(
		{
			activity_type: item.activity_type as DocActivityType,
			content: item.content as string | DocActivityContent | null,
		},
		t,
	);
}

function ActivityRow({
	projectId,
	item,
}: {
	projectId: string;
	item: AgentActivity;
}) {
	const { t } = useTranslation("projects");
	const { t: tCommon } = useTranslation("common");
	const Icon = item.source_type === "task" ? CheckSquare : FileText;
	const description = describeItem(item, t);

	return (
		<div className="flex items-start gap-3 border-b border-border/10 px-1 py-2.5 last:border-0">
			<div className="mt-0.5 flex size-7 shrink-0 items-center justify-center rounded-full bg-muted/40 text-muted-foreground/80">
				<Icon className="size-3.5" />
			</div>
			<div className="flex min-w-0 flex-1 flex-wrap items-center gap-1.5">
				<Badge variant="outline" className="shrink-0 text-[10px] font-semibold">
					{t(`agents.detail.activity.sourceType.${item.source_type}`)}
				</Badge>
				{description && (
					<span className="text-sm text-foreground/80">{description}</span>
				)}
				{item.source_type === "task" ? (
					<Link
						to="/projects/$projectId/tasks/$taskId"
						params={{ projectId, taskId: item.source_id }}
						className="truncate text-sm font-medium text-primary hover:underline"
					>
						{item.source_title}
					</Link>
				) : (
					<Link
						to="/projects/$projectId/docs/$docId"
						params={{ projectId, docId: item.source_id }}
						className="truncate text-sm font-medium text-primary hover:underline"
					>
						{item.source_title}
					</Link>
				)}
				<span className="ml-auto shrink-0 text-xs text-muted-foreground/45">
					{timeAgo(item.created_at, tCommon)}
				</span>
			</div>
		</div>
	);
}

export interface AgentActivityTabProps {
	projectId: string;
	agentId: string;
}

export function AgentActivityTab({
	projectId,
	agentId,
}: AgentActivityTabProps) {
	const { t } = useTranslation("projects");
	const [filters, setFilters] = useState<AgentActivityFiltersState>({});

	const { data, isLoading, fetchNextPage, hasNextPage, isFetchingNextPage } =
		useInfiniteQuery(agentActivitiesQueryOptions(projectId, agentId, filters));

	const items = data?.pages.flatMap((page) => page.items) ?? [];
	const hasActiveFilters = Object.values(filters).some((v) =>
		Array.isArray(v) ? v.length > 0 : !!v,
	);

	const scrollContainerRef = useRef<HTMLDivElement | null>(null);
	const loadMoreRef = useRef<HTMLDivElement | null>(null);

	useEffect(() => {
		const container = scrollContainerRef.current;
		const target = loadMoreRef.current;
		if (!container || !target) return;

		const observer = new IntersectionObserver(
			([entry]) => {
				if (entry?.isIntersecting && hasNextPage && !isFetchingNextPage) {
					void fetchNextPage();
				}
			},
			{ root: container, rootMargin: "150px" },
		);
		observer.observe(target);
		return () => observer.disconnect();
	}, [hasNextPage, isFetchingNextPage, fetchNextPage]);

	return (
		<div className="flex min-h-0 flex-1 flex-col rounded-xl border border-border/50">
			<AgentActivityFilters filters={filters} onFiltersChange={setFilters} />
			<div
				ref={scrollContainerRef}
				className="min-h-0 flex-1 overflow-y-auto px-3 py-1"
			>
				{isLoading ? (
					<div className="space-y-2 py-2">
						{Array.from({ length: 5 }).map((_, i) => (
							// biome-ignore lint/suspicious/noArrayIndexKey: skeleton
							<Skeleton key={i} className="h-10 rounded-lg" />
						))}
					</div>
				) : items.length === 0 ? (
					<div className="flex flex-col items-center justify-center gap-3 py-14 text-center">
						<Activity className="size-8 text-muted-foreground/40" />
						<p className="text-sm text-muted-foreground">
							{hasActiveFilters
								? t("agents.detail.activity.emptyFiltered.title")
								: t("agents.detail.activity.empty.title")}
						</p>
						<p className="max-w-xs text-xs text-muted-foreground">
							{hasActiveFilters
								? t("agents.detail.activity.emptyFiltered.description")
								: t("agents.detail.activity.empty.description")}
						</p>
					</div>
				) : (
					<>
						{items.map((item) => (
							<ActivityRow key={item.id} projectId={projectId} item={item} />
						))}
						{hasNextPage && (
							<div ref={loadMoreRef} className="py-2">
								{isFetchingNextPage && <Skeleton className="h-10 rounded-lg" />}
							</div>
						)}
					</>
				)}
			</div>
		</div>
	);
}
