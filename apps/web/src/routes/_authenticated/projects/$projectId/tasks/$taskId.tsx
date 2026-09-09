import { useQuery } from "@tanstack/react-query";
import { createFileRoute, Link } from "@tanstack/react-router";
import { AlertCircle, ArrowLeft } from "lucide-react";
import { useTranslation } from "react-i18next";

import { TaskDetailModal } from "@/components/projects/interactions/task-detail-modal";
import { NoPermissionState } from "@/components/shared/no-permission-state";
import { Skeleton } from "@/components/ui/skeleton";
import { useProjectPermissions } from "@/hooks/use-project-permissions";
import { isForbiddenError } from "@/lib/api-error";
import { taskQueryOptions } from "@/lib/interaction-api";
import {
	projectMembersQueryOptions,
	projectQueryOptions,
	taskStatusesQueryOptions,
	taskTypesQueryOptions,
} from "@/lib/project-api";

export const Route = createFileRoute(
	"/_authenticated/projects/$projectId/tasks/$taskId",
)({
	component: TaskDetailPage,
});

function TaskDetailSkeleton() {
	return (
		<div className="flex h-full flex-col overflow-hidden bg-background">
			{/* Back nav strip */}
			<div className="shrink-0 border-b border-border/40 px-5 py-2.5 flex items-center gap-3">
				<Skeleton className="h-4 w-28" />
			</div>
			{/* Task detail skeleton body */}
			<div className="flex flex-col lg:flex-row flex-1 min-w-0 min-h-0 overflow-y-auto lg:overflow-hidden">
				{/* Main content */}
				<div className="lg:flex-1 lg:overflow-y-auto px-4 lg:px-8 py-5 lg:py-7 space-y-6 lg:space-y-8 max-w-3xl mx-auto w-full">
					{/* Type + status badges */}
					<div className="flex items-center gap-2.5 flex-wrap">
						<Skeleton className="h-6 w-20 rounded-md" />
						<Skeleton className="h-6 w-24 rounded-full" />
					</div>
					{/* Title */}
					<Skeleton className="h-8 w-3/4 rounded-md" />
					{/* Properties section */}
					<div className="space-y-3">
						<div className="flex items-center gap-2 mb-3">
							<Skeleton className="h-3 w-20" />
							<div className="flex-1 h-px bg-border/30" />
						</div>
						{[
							{ lw: "w-20", vw: "w-32" },
							{ lw: "w-16", vw: "w-28" },
							{ lw: "w-24", vw: "w-20" },
							{ lw: "w-20", vw: "w-36" },
							{ lw: "w-24", vw: "w-24" },
						].map(({ lw, vw }, i) => (
							// biome-ignore lint/suspicious/noArrayIndexKey: static skeleton
							<div key={i} className="flex items-center gap-3 py-1.5">
								<Skeleton className={`h-3.5 ${lw} shrink-0`} />
								<Skeleton className={`h-5 ${vw} rounded-md`} />
							</div>
						))}
					</div>
					{/* Description section */}
					<div className="space-y-2 pt-2">
						<div className="flex items-center gap-2 mb-3">
							<Skeleton className="h-3 w-24" />
							<div className="flex-1 h-px bg-border/30" />
						</div>
						<Skeleton className="h-3.5 w-full" />
						<Skeleton className="h-3.5 w-5/6" />
						<Skeleton className="h-3.5 w-4/5" />
						<Skeleton className="h-3.5 w-2/3" />
					</div>
				</div>
				{/* Right sidebar — activity panel placeholder */}
				<div className="hidden lg:block w-72 shrink-0 border-l border-border/30 p-4 space-y-3">
					<Skeleton className="h-4 w-24 mb-4" />
					{[
						{ fw: "w-4/5" },
						{ fw: "w-3/5" },
						{ fw: "w-3/4" },
						{ fw: "w-1/2" },
						{ fw: "w-2/3" },
					].map(({ fw }, i) => (
						// biome-ignore lint/suspicious/noArrayIndexKey: static skeleton
						<div key={i} className="flex gap-2.5">
							<Skeleton className="size-6 rounded-full shrink-0 mt-0.5" />
							<div className="flex-1 space-y-1.5">
								<Skeleton className={`h-3 ${fw}`} />
								<Skeleton className="h-2.5 w-1/3" />
							</div>
						</div>
					))}
				</div>
			</div>
		</div>
	);
}

function TaskDetailPage() {
	const { t } = useTranslation("projects");
	const { projectId, taskId } = Route.useParams();

	const { hasProjectPermission } = useProjectPermissions(projectId);
	const canEdit = hasProjectPermission("tasks.write");

	const { data: project } = useQuery(projectQueryOptions(projectId));
	const { data: taskStatuses = [] } = useQuery(
		taskStatusesQueryOptions(projectId),
	);
	const { data: taskTypes = [] } = useQuery(taskTypesQueryOptions(projectId));
	const { data: members = [] } = useQuery(
		projectMembersQueryOptions(projectId),
	);
	const {
		data: task = null,
		isLoading,
		isError,
		error,
	} = useQuery(taskQueryOptions(projectId, taskId));

	if (isLoading) {
		return <TaskDetailSkeleton />;
	}

	if (!task) {
		if (isError && isForbiddenError(error)) {
			return (
				<div className="flex h-full flex-col items-center justify-center p-6">
					<NoPermissionState
						title={t("taskDetail.noPermission.title")}
						description={t("taskDetail.noPermission.description")}
					/>
				</div>
			);
		}
		return (
			<div className="flex h-full flex-col items-center justify-center gap-4 text-muted-foreground/60">
				<AlertCircle className="size-10" />
				<div className="text-center">
					<p className="text-base font-medium text-foreground/70">
						{t("taskDetail.notFound.title")}
					</p>
					<p className="text-sm mt-1">{t("taskDetail.notFound.description")}</p>
				</div>
				<Link
					to="/projects/$projectId"
					params={{ projectId }}
					className="flex items-center gap-1.5 rounded-lg border border-border/60 px-4 py-2 text-sm font-medium text-foreground/70 hover:bg-muted/50 transition-colors mt-2"
				>
					<ArrowLeft className="size-4" />
					{t("taskDetail.notFound.backToProject")}
				</Link>
			</div>
		);
	}

	return (
		<div className="flex h-full flex-col overflow-hidden bg-background">
			{/* Back navigation strip */}
			<div className="shrink-0 border-b border-border/40 px-5 py-2.5 flex items-center gap-3">
				<Link
					to="/projects/$projectId"
					params={{ projectId }}
					className="flex items-center gap-1.5 text-sm text-muted-foreground hover:text-foreground transition-colors"
				>
					<ArrowLeft className="size-3.5" />
					{project?.name ?? t("taskDetail.notFound.projectFallback")}
				</Link>
			</div>

			{/* Task detail — full height below nav strip */}
			<div className="flex-1 overflow-hidden">
				<TaskDetailModal
					task={task}
					open
					onOpenChange={() => {}}
					statuses={taskStatuses}
					taskTypes={taskTypes}
					members={members}
					projectName={project?.name}
					projectId={projectId}
					mode="page"
					canEdit={canEdit}
				/>
			</div>
		</div>
	);
}
