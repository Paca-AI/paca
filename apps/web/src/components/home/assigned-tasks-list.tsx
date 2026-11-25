import { useInfiniteQuery, useQueries } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { AlertCircle, CheckCircle2 } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { ReportIssueButton } from "@/components/report-issue-button";
import { Skeleton } from "@/components/ui/skeleton";
import { Spinner } from "@/components/ui/spinner";
import { assignedTasksQueryOptions, type Task } from "@/lib/interaction-api";
import {
	projectsLookupInfiniteQueryOptions,
	STATUS_CATEGORIES,
	type TaskStatus,
	taskStatusesQueryOptions,
	taskTypesQueryOptions,
} from "@/lib/project-api";
import { cn } from "@/lib/utils";

import { ListGroup } from "../projects/interactions/list-group";

// Read-only home page widget: fetches tasks assigned to the current user
// across every project, groups them by project, and renders each group with
// the same ListGroup/TaskRow components the project list view uses — so it
// looks and behaves like a filtered slice of that view rather than a
// bespoke summary.
const VISIBLE_FIELDS = ["type", "status", "importance", "due_date"];
const ALL_FILTER = "all";

interface StatusTab {
	key: string;
	label: string;
	color?: string | null;
	count: number;
	/** STATUS_CATEGORIES index, then the status's position in its project. */
	order: [number, number];
}

const NO_STATUSES: TaskStatus[] = [];

// Module-level so its identity is stable: useQueries only re-runs combine
// when a query result changes, which keeps statusById referentially stable
// for the memos below.
function combineStatuses(results: { data?: TaskStatus[] }[]) {
	const lists = results.map((r) => r.data ?? NO_STATUSES);
	return {
		lists,
		byId: new Map(lists.flat().map((s) => [s.id, s])),
	};
}

// Statuses are per project, so "Blocked" in one project and "Blocked" in
// another are different rows. The filter tabs merge them by name, so one tab
// covers every project that uses that status name.
function statusKey(status: TaskStatus): string {
	return status.name.trim().toLowerCase();
}

function buildStatusTabs(
	tasks: Task[],
	statusById: Map<string, TaskStatus>,
): StatusTab[] {
	const tabs = new Map<string, StatusTab>();
	for (const task of tasks) {
		const status = task.status_id ? statusById.get(task.status_id) : undefined;
		if (!status) continue;
		const key = statusKey(status);
		const tab = tabs.get(key);
		if (tab) {
			tab.count += 1;
			tab.color ??= status.color;
		} else {
			tabs.set(key, {
				key,
				label: status.name,
				color: status.color,
				count: 1,
				order: [STATUS_CATEGORIES.indexOf(status.category), status.position],
			});
		}
	}
	return [...tabs.values()].sort(
		(a, b) => a.order[0] - b.order[0] || a.order[1] - b.order[1],
	);
}

function AssignedTasksListSkeleton() {
	return (
		<div className="rounded-xl border border-border/60 bg-card overflow-hidden">
			<div className="flex items-center gap-2.5 px-4 py-3 border-b border-border/50">
				<Skeleton className="h-3.5 w-24" />
			</div>
			{[0, 1, 2].map((i) => (
				<div
					key={i}
					className="flex items-center gap-3 px-4 py-2.5 border-b border-border/50 last:border-0"
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
	const [statusFilter, setStatusFilter] = useState(ALL_FILTER);

	const {
		data,
		isLoading,
		isError,
		error,
		fetchNextPage,
		hasNextPage,
		isFetchingNextPage,
	} = useInfiniteQuery(assignedTasksQueryOptions());
	const {
		data: projectsData,
		fetchNextPage: fetchNextProjectsPage,
		hasNextPage: hasMoreProjects,
		isFetchingNextPage: isFetchingMoreProjects,
		isFetchNextPageError: projectsPageFetchErrored,
	} = useInfiniteQuery(projectsLookupInfiniteQueryOptions());
	// This widget only needs a complete id -> project lookup (for the group
	// header's name/task_id_prefix below), not a browsable list — so unlike
	// the home page's own project grid, there's no "load more" affordance to
	// put this behind. Drain every page in the background instead: harmless
	// for the common case (well under one page), and for a workspace with
	// more projects than that, it beats silently falling back to a raw
	// project id in the header.
	//
	// Uses projectsLookupInfiniteQueryOptions rather than
	// projectsInfiniteQueryOptions deliberately — the latter's cache entry
	// (["projects", "all"]) is shared with the home grid's and sidebar
	// switcher's own "Load more"/scroll-driven paging, and draining it here
	// would silently exhaust their pages before the user ever gets to. Also
	// stops retrying once a page fetch fails — hasNextPage stays true after
	// a failed fetchNextPage (it's derived from the last *successful* page),
	// so without this guard a persistent failure would retry forever.
	useEffect(() => {
		if (
			hasMoreProjects &&
			!isFetchingMoreProjects &&
			!projectsPageFetchErrored
		) {
			void fetchNextProjectsPage();
		}
	}, [
		hasMoreProjects,
		isFetchingMoreProjects,
		projectsPageFetchErrored,
		fetchNextProjectsPage,
	]);

	const tasks = useMemo(
		() => data?.pages.flatMap((page) => page.items) ?? [],
		[data],
	);

	const projectIds = useMemo(
		() => [...new Set(tasks.map((task) => task.project_id))],
		[tasks],
	);

	const { lists: statusLists, byId: statusById } = useQueries({
		queries: projectIds.map((id) => taskStatusesQueryOptions(id)),
		combine: combineStatuses,
	});

	const statusTabs = useMemo(
		() => buildStatusTabs(tasks, statusById),
		[tasks, statusById],
	);
	// Fall back to "All" when the selected status no longer has any tasks,
	// e.g. after the last blocked task was unblocked.
	const activeFilter = statusTabs.some((tab) => tab.key === statusFilter)
		? statusFilter
		: ALL_FILTER;

	const filteredTasks = useMemo(() => {
		if (activeFilter === ALL_FILTER) return tasks;
		return tasks.filter((task) => {
			const status = task.status_id
				? statusById.get(task.status_id)
				: undefined;
			return status ? statusKey(status) === activeFilter : false;
		});
	}, [tasks, statusById, activeFilter]);

	const tasksByProject = useMemo(() => {
		const map = new Map<string, Task[]>();
		for (const task of filteredTasks) {
			const list = map.get(task.project_id);
			if (list) list.push(task);
			else map.set(task.project_id, [task]);
		}
		return map;
	}, [filteredTasks]);
	const typeQueries = useQueries({
		queries: projectIds.map((id) => taskTypesQueryOptions(id)),
	});

	const projectsById = useMemo(
		() =>
			new Map(
				(projectsData?.pages.flatMap((p) => p.items) ?? []).map((p) => [
					p.id,
					p,
				]),
			),
		[projectsData],
	);

	if (isLoading) {
		return <AssignedTasksListSkeleton />;
	}

	if (isError) {
		return (
			<div className="flex flex-col items-center justify-center gap-2 rounded-xl border border-destructive/20 bg-destructive/5 py-10 text-center">
				<AlertCircle className="size-6 text-destructive/40" />
				<p className="text-sm font-medium text-destructive">
					{t("home.myTasks.loadError")}
				</p>
				<ReportIssueButton error={error} />
			</div>
		);
	}

	if (tasks.length === 0) {
		return (
			<div className="flex flex-col items-center justify-center gap-2 rounded-xl border border-dashed border-border/60 bg-muted/10 py-10 text-center">
				<CheckCircle2 className="size-6 text-muted-foreground/90" />
				<p className="text-sm font-medium text-foreground/70">
					{t("home.myTasks.emptyTitle")}
				</p>
				<p className="text-xs text-muted-foreground max-w-xs">
					{t("home.myTasks.emptyDescription")}
				</p>
			</div>
		);
	}

	return (
		<div className="rounded-xl border border-border/60 bg-card overflow-hidden">
			{statusTabs.length > 1 ? (
				<div
					role="tablist"
					aria-label={t("home.myTasks.filterLabel")}
					className="flex flex-wrap items-center gap-1 border-b border-border/50 px-3 py-2"
				>
					<FilterTab
						selected={activeFilter === ALL_FILTER}
						onSelect={() => setStatusFilter(ALL_FILTER)}
						label={t("home.myTasks.filterAll")}
						count={tasks.length}
					/>
					{statusTabs.map((tab) => (
						<FilterTab
							key={tab.key}
							selected={activeFilter === tab.key}
							onSelect={() => setStatusFilter(tab.key)}
							label={tab.label}
							count={tab.count}
							color={tab.color}
						/>
					))}
				</div>
			) : null}
			{projectIds.map((projectId, idx) => {
				const project = projectsById.get(projectId);
				const projectTasks = tasksByProject.get(projectId);
				if (!projectTasks) return null;
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
						tasks={projectTasks}
						statuses={statusLists[idx] ?? NO_STATUSES}
						taskTypes={typeQueries[idx]?.data ?? []}
						members={[]}
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
					className="flex w-full items-center justify-center gap-1.5 border-t border-border/50 py-2.5 text-xs font-medium text-muted-foreground/90 hover:text-primary hover:bg-primary/5 transition-all duration-150 disabled:opacity-50"
				>
					{isFetchingNextPage ? (
						<>
							<Spinner aria-hidden="true" className="size-3" />
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

function FilterTab({
	selected,
	onSelect,
	label,
	count,
	color,
}: {
	selected: boolean;
	onSelect: () => void;
	label: string;
	count: number;
	color?: string | null;
}) {
	return (
		<button
			type="button"
			role="tab"
			aria-selected={selected}
			onClick={onSelect}
			className={cn(
				"flex items-center gap-1.5 rounded-md px-2.5 py-1 text-xs font-medium transition-colors",
				selected
					? "bg-muted text-foreground"
					: "text-muted-foreground hover:bg-muted/50 hover:text-foreground",
			)}
		>
			{color !== undefined ? (
				<span
					aria-hidden="true"
					className="size-1.5 rounded-full"
					style={{ backgroundColor: color ?? "var(--muted-foreground)" }}
				/>
			) : null}
			{label}
			<span
				className={cn(
					"font-mono tabular-nums",
					selected ? "text-primary" : "text-muted-foreground/70",
				)}
			>
				{count}
			</span>
		</button>
	);
}
