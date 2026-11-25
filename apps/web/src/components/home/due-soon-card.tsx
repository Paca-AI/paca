import { useInfiniteQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { CalendarCheck } from "lucide-react";
import { useMemo } from "react";
import { useTranslation } from "react-i18next";
import { Skeleton } from "@/components/ui/skeleton";
import { formatDate } from "@/lib/format-date";
import { assignedTasksQueryOptions } from "@/lib/interaction-api";
import { projectsLookupInfiniteQueryOptions } from "@/lib/project-api";
import { cn } from "@/lib/utils";
import { DUE_SOON_WINDOW_DAYS, upcomingDueTasks } from "./due-dates";

const MAX_ITEMS = 6;

// Right-rail card on the home page: the viewer's assigned tasks that are
// overdue or due within the next two weeks, soonest first. Reads the same
// cache entry as the "My Tasks" widget, so it adds no request of its own.
export function DueSoonCard() {
	const { t } = useTranslation("shared");
	const { data, isLoading } = useInfiniteQuery(assignedTasksQueryOptions());
	// Shared with AssignedTasksList, which drains the remaining pages.
	const { data: projectsData } = useInfiniteQuery(
		projectsLookupInfiniteQueryOptions(),
	);

	const due = useMemo(
		() =>
			upcomingDueTasks(data?.pages.flatMap((page) => page.items) ?? []).slice(
				0,
				MAX_ITEMS,
			),
		[data],
	);

	const prefixByProject = useMemo(
		() =>
			new Map(
				(projectsData?.pages.flatMap((p) => p.items) ?? []).map((p) => [
					p.id,
					p.task_id_prefix,
				]),
			),
		[projectsData],
	);

	const relative = (days: number) => {
		if (days < 0) return t("home.dueSoon.overdue", { count: -days });
		if (days === 0) return t("home.dueSoon.today");
		if (days === 1) return t("home.dueSoon.tomorrow");
		return t("home.dueSoon.inDays", { count: days });
	};

	return (
		<section className="rounded-xl border border-border/60 bg-card">
			<header className="px-4 pt-4 pb-2">
				<h2 className="font-[Syne] text-sm font-bold tracking-tight">
					{t("home.dueSoon.title")}
				</h2>
				<p className="text-xs text-muted-foreground mt-0.5">
					{t("home.dueSoon.subtitle", { count: DUE_SOON_WINDOW_DAYS })}
				</p>
			</header>
			{isLoading ? (
				<div className="space-y-2 px-4 pb-4">
					{[0, 1, 2].map((i) => (
						<Skeleton key={i} className="h-10 w-full" />
					))}
				</div>
			) : due.length === 0 ? (
				<div className="flex items-center gap-2.5 px-4 pb-4 pt-1 text-xs text-muted-foreground">
					<CalendarCheck className="size-4 shrink-0 text-muted-foreground/80" />
					{t("home.dueSoon.empty")}
				</div>
			) : (
				<ul className="px-2 pb-2">
					{due.map(({ task, dueDate, days }) => {
						const prefix = prefixByProject.get(task.project_id);
						const overdue = days < 0;
						const soon = days >= 0 && days <= 2;
						return (
							<li key={task.id}>
								<Link
									to="/projects/$projectId/tasks/$taskId"
									params={{ projectId: task.project_id, taskId: task.id }}
									className="flex items-center gap-3 rounded-lg px-2 py-2 transition-colors hover:bg-muted/50 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/40"
								>
									<div
										className={cn(
											"flex w-10 shrink-0 flex-col items-center rounded-md border py-0.5 leading-tight",
											overdue
												? "border-destructive/40 text-destructive"
												: soon
													? "border-amber-500/40 text-amber-600 dark:text-amber-400"
													: "border-border/60",
										)}
									>
										<span className="font-mono text-sm font-semibold tabular-nums">
											{formatDate(
												dueDate,
												{ day: "2-digit" },
												{ dateOnly: true },
											)}
										</span>
										<span className="text-[9px] uppercase text-muted-foreground">
											{formatDate(
												dueDate,
												{ month: "short" },
												{ dateOnly: true },
											)}
										</span>
									</div>
									<div className="min-w-0 flex-1">
										<p className="truncate text-sm text-foreground">
											{task.title}
										</p>
										<p className="truncate text-xs text-muted-foreground">
											{prefix ? (
												<span className="font-mono">
													{prefix}-{task.task_number}
												</span>
											) : null}
											{prefix ? " · " : null}
											<span
												className={cn(
													overdue && "text-destructive",
													soon && "text-amber-600 dark:text-amber-400",
												)}
											>
												{relative(days)}
											</span>
										</p>
									</div>
								</Link>
							</li>
						);
					})}
				</ul>
			)}
		</section>
	);
}
