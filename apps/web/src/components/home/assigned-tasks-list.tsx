import { useInfiniteQuery, useQueries, useQuery } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { CheckCircle2, Loader2 } from "lucide-react";
import { useMemo } from "react";
import { useTranslation } from "react-i18next";

import { Skeleton } from "@/components/ui/skeleton";
import { assignedTasksQueryOptions, type Task } from "@/lib/interaction-api";
import {
	projectMembersQueryOptions,
	projectsQueryOptions,
	taskStatusesQueryOptions,
	taskTypesQueryOptions,
} from "@/lib/project-api";

import { ListGroup } from "../projects/interactions/list-group";

// Read-only home page widget: fetches tasks assigned to the current user
// across every project, groups them by project, and renders each group with
// the same ListGroup/TaskRow components the project list view uses — so it
// looks and behaves like a filtered slice of that view rather than a
// bespoke summary.
const VISIBLE_FIELDS = ["type", "status", "importance", "due_date"];

function AssignedTasksListSkeleton() {
	return (
		<div className="rounded-xl border border-border/60 bg-card overflow-hidden">
			<div className="flex items-center gap-2.5 px-4 py-3 border-b border-border/25">
				<Skeleton className="h-3.5 w-24" />
			</div>
			{[0, 1, 2].map((i) => (
				<div
					key={i}
					className="flex items-center gap-3 px-4 py-2.5 border-b border-border/20 last:border-0"
				>
					<Skeleton className="h-3.5 w-14 shrink-0" />
					<Skeleton className="h-3.5 flex-1" />
					<Skeleton className="h-3.5 w-16 shrink-0" />
					<Skeleton className="h-3.5 w-20 shrink-0" />
				</div>
			))}
		</div>
	);
}

export function AssignedTasksList() {
	const { t } = useTranslation("shared");
	const navigate = useNavigate();

	const { data, isLoading, fetchNextPage, hasNextPage, isFetchingNextPage } =
		useInfiniteQuery(assignedTasksQueryOptions());
	const { data: projectsResult } = useQuery(projectsQueryOptions());

	const tasks = useMemo(
		() => data?.pages.flatMap((page) => page.items) ?? [],
		[data],
	);

	const projectIds = useMemo(
		() => [...new Set(tasks.map((task) => task.project_id))],
		[tasks],
	);

	const tasksByProject = useMemo(() => {
		const map = new Map<string, Task[]>();
		for (const task of tasks) {
			const list = map.get(task.project_id);
			if (list) list.push(task);
			else map.set(task.project_id, [task]);
		}
		return map;
	}, [tasks]);

	const statusQueries = useQueries({
		queries: projectIds.map((id) => taskStatusesQueryOptions(id)),
	});
	const typeQueries = useQueries({
		queries: projectIds.map((id) => taskTypesQueryOptions(id)),
	});
	const memberQueries = useQueries({
		queries: projectIds.map((id) => projectMembersQueryOptions(id)),
	});

	const projectsById = useMemo(
		() => new Map((projectsResult?.items ?? []).map((p) => [p.id, p])),
		[projectsResult],
	);

	if (isLoading) {
		return <AssignedTasksListSkeleton />;
	}

	if (tasks.length === 0) {
		return (
			<div className="flex flex-col items-center justify-center gap-2 rounded-xl border border-dashed border-border/60 bg-muted/10 py-10 text-center">
				<CheckCircle2 className="size-6 text-muted-foreground/30" />
				<p className="text-sm font-medium text-foreground/70">
					{t("home.myTasks.emptyTitle")}
				</p>
				<p className="text-xs text-muted-foreground/70 max-w-xs">
					{t("home.myTasks.emptyDescription")}
				</p>
			</div>
		);
	}

	return (
		<div className="rounded-xl border border-border/60 bg-card overflow-hidden">
			{projectIds.map((projectId, idx) => {
				const project = projectsById.get(projectId);
				return (
					<ListGroup
						key={projectId}
						projectId={projectId}
						groupDef={{
							key: projectId,
							label: project?.name ?? projectId,
							fieldValue: null,
						}}
						swimlaneDefs={[]}
						swimlaneBy={undefined}
						tasks={tasksByProject.get(projectId) ?? []}
						statuses={statusQueries[idx]?.data ?? []}
						taskTypes={typeQueries[idx]?.data ?? []}
						members={memberQueries[idx]?.data ?? []}
						customFields={[]}
						canCreate={false}
						isStatusGrouping={false}
						visibleFields={VISIBLE_FIELDS}
						taskIdPrefix={project?.task_id_prefix}
						onCreateTask={async () => {}}
						onTaskClick={(task) => {
							void navigate({
								to: "/projects/$projectId/tasks/$taskId",
								params: { projectId: task.project_id, taskId: task.id },
							});
						}}
					/>
				);
			})}
			{hasNextPage ? (
				<button
					type="button"
					onClick={() => void fetchNextPage()}
					disabled={isFetchingNextPage}
					className="flex w-full items-center justify-center gap-1.5 border-t border-border/10 py-2.5 text-xs font-medium text-muted-foreground/60 hover:text-primary hover:bg-primary/5 transition-all duration-150 disabled:opacity-50"
				>
					{isFetchingNextPage ? (
						<>
							<Loader2 className="size-3 animate-spin" />
							{t("home.myTasks.loading")}
						</>
					) : (
						t("home.myTasks.loadMore")
					)}
				</button>
			) : null}
		</div>
	);
}
