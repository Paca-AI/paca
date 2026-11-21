import { useInfiniteQuery, useQueries, useQuery } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { AlertCircle, CheckCircle2, Settings2 } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { ReportIssueButton } from "@/components/report-issue-button";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
	DropdownMenu,
	DropdownMenuCheckboxItem,
	DropdownMenuContent,
	DropdownMenuLabel,
	DropdownMenuRadioGroup,
	DropdownMenuRadioItem,
	DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Skeleton } from "@/components/ui/skeleton";
import {
	type Task,
	WAITING_TASKS_PAGE_SIZE,
	waitingTasksQueryOptions,
} from "@/lib/interaction-api";
import {
	projectsLookupInfiniteQueryOptions,
	taskStatusesQueryOptions,
	taskTypesQueryOptions,
} from "@/lib/project-api";

import { ListGroup } from "../projects/interactions/list-group";

// Read-only home page widget: fetches open tasks with the least recent
// activity across every project the viewer can access ("waiting" = nothing
// happened to them for the longest time). List size and visible columns are
// persisted in localStorage; the list itself renders with the same
// ListGroup/TaskRow components the project list view uses. All tasks come
// permission-filtered from the server (GET /users/me/waiting-tasks).
const ALL_FIELDS = ["type", "status", "importance", "waiting"] as const;
const DEFAULT_FIELDS = ["status", "waiting"] as const;
const PAGE_SIZES = [10, 25, 50];
const FIELDS_STORAGE_KEY = "home.waitingTasks.fields";
const SIZE_STORAGE_KEY = "home.waitingTasks.pageSize";

function WaitingTasksListSkeleton() {
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
					<Skeleton className="h-3.5 w-14 shrink-0" />
				</div>
			))}
		</div>
	);
}

function readStoredFields(): string[] {
	try {
		const raw = localStorage.getItem(FIELDS_STORAGE_KEY);
		if (!raw) return [...DEFAULT_FIELDS];
		const parsed: unknown = JSON.parse(raw);
		const valid = Array.isArray(parsed)
			? parsed.filter((f): f is string =>
					(ALL_FIELDS as readonly string[]).includes(f as string),
				)
			: [];
		return valid.length > 0 ? valid : [...DEFAULT_FIELDS];
	} catch {
		return [...DEFAULT_FIELDS];
	}
}

function readStoredPageSize(): number {
	try {
		const n = Number.parseInt(localStorage.getItem(SIZE_STORAGE_KEY) ?? "", 10);
		return PAGE_SIZES.includes(n) ? n : WAITING_TASKS_PAGE_SIZE;
	} catch {
		return WAITING_TASKS_PAGE_SIZE;
	}
}

export function WaitingTasksList() {
	const { t } = useTranslation("shared");
	const { t: tProjects } = useTranslation("projects");
	const navigate = useNavigate();

	// SSR-safe: start with defaults, pick up persisted values after mount.
	const [pageSize, setPageSize] = useState(WAITING_TASKS_PAGE_SIZE);
	const [visibleFields, setVisibleFields] = useState<string[]>([
		...DEFAULT_FIELDS,
	]);
	useEffect(() => {
		setPageSize(readStoredPageSize());
		setVisibleFields(readStoredFields());
	}, []);

	const { data, isLoading, isError, error } = useQuery(
		waitingTasksQueryOptions(pageSize),
	);
	const {
		data: projectsData,
		fetchNextPage: fetchNextProjectsPage,
		hasNextPage: hasMoreProjects,
		isFetchingNextPage: isFetchingMoreProjects,
		isFetchNextPageError: projectsPageFetchErrored,
	} = useInfiniteQuery(projectsLookupInfiniteQueryOptions());
	// Same background drain as the "My Tasks" widget: this widget only needs a
	// complete id -> project lookup for the group headers, and draining the
	// separate lookup cache avoids touching the home grid's shared pages.
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

	const tasks = data?.items ?? [];

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

	const headerLabels: Record<string, string> = {
		type: tProjects("board.taskRow.columnHeaders.type"),
		status: tProjects("board.taskRow.columnHeaders.status"),
		importance: tProjects("board.taskRow.columnHeaders.importance"),
		waiting: tProjects("board.taskRow.columnHeaders.waiting"),
	};

	const persistFields = (fields: string[]) => {
		setVisibleFields(fields);
		localStorage.setItem(FIELDS_STORAGE_KEY, JSON.stringify(fields));
	};

	const persistPageSize = (size: number) => {
		setPageSize(size);
		localStorage.setItem(SIZE_STORAGE_KEY, String(size));
	};

	if (isLoading) {
		return <WaitingTasksListSkeleton />;
	}

	if (isError) {
		return (
			<div className="flex flex-col items-center justify-center gap-2 rounded-xl border border-destructive/20 bg-destructive/5 py-10 text-center">
				<AlertCircle className="size-6 text-destructive/40" />
				<p className="text-sm font-medium text-destructive">
					{t("home.waitingTasks.loadError")}
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
					{t("home.waitingTasks.emptyTitle")}
				</p>
				<p className="text-xs text-muted-foreground max-w-xs">
					{t("home.waitingTasks.emptyDescription")}
				</p>
			</div>
		);
	}

	return (
		<div className="rounded-xl border border-border/60 bg-card overflow-hidden">
			<div className="flex items-center justify-between px-4 py-2 border-b border-border/50">
				<Badge
					variant="outline"
					className="text-xs text-muted-foreground border-border/60"
				>
					{t("home.waitingTasks.subtitle")}
				</Badge>
				<div className="flex items-center gap-1.5">
					<DropdownMenu>
						<DropdownMenuTrigger
							render={
								<Button
									variant="ghost"
									size="sm"
									className="h-7 gap-1.5 px-2 text-xs text-muted-foreground"
								/>
							}
						>
							<Settings2 className="size-3.5" />
							{t("home.waitingTasks.columns")}
						</DropdownMenuTrigger>
						<DropdownMenuContent align="end">
							<DropdownMenuLabel>
								{t("home.waitingTasks.columns")}
							</DropdownMenuLabel>
							{ALL_FIELDS.map((field) => (
								<DropdownMenuCheckboxItem
									key={field}
									checked={visibleFields.includes(field)}
									onCheckedChange={(checked) => {
										const next = checked
											? [...visibleFields, field]
											: visibleFields.filter((f) => f !== field);
										// Never allow an empty list — the rows would be bare.
										if (next.length > 0) persistFields(next);
									}}
								>
									{headerLabels[field]}
								</DropdownMenuCheckboxItem>
							))}
						</DropdownMenuContent>
					</DropdownMenu>
					<DropdownMenu>
						<DropdownMenuTrigger
							render={
								<Button
									variant="ghost"
									size="sm"
									className="h-7 gap-1.5 px-2 text-xs text-muted-foreground"
								/>
							}
						>
							{t("home.waitingTasks.showCount", { count: pageSize })}
						</DropdownMenuTrigger>
						<DropdownMenuContent align="end">
							<DropdownMenuRadioGroup
								value={String(pageSize)}
								onValueChange={(value) => persistPageSize(Number(value))}
							>
								{PAGE_SIZES.map((size) => (
									<DropdownMenuRadioItem key={size} value={String(size)}>
										{t("home.waitingTasks.showCount", { count: size })}
									</DropdownMenuRadioItem>
								))}
							</DropdownMenuRadioGroup>
						</DropdownMenuContent>
					</DropdownMenu>
				</div>
			</div>
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
						members={[]}
						customFields={[]}
						canCreate={false}
						isStatusGrouping={false}
						visibleFields={visibleFields}
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
			{data?.has_more ? (
				<div className="border-t border-border/50 px-4 py-2.5 text-center text-xs text-muted-foreground">
					{t("home.waitingTasks.moreHidden")}
				</div>
			) : null}
		</div>
	);
}
