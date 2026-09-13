import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import {
	CheckCircle2,
	ImageOff,
	MessageSquare,
	RotateCcw,
	SquareCheckBig,
} from "lucide-react";
import { useTranslation } from "react-i18next";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { useProjectPermissions } from "@/hooks/use-project-permissions";
import {
	annotationScreenshotUrlQueryOptions,
	createTaskFromAnnotation,
	type PageAnnotation,
	portForwardAnnotationsQueryOptions,
	reopenAnnotation,
	resolveAnnotation,
} from "@/lib/annotation-api";

// The web app's own view of page annotations — created via the Paca
// browser extension (apps/extension) on a port forward's forwarded
// preview. Lets anyone browse/resolve/turn-into-tasks without the
// extension installed; the extension itself renders these as on-page pins
// instead of this flat list. Each card links to the comment's own detail
// page (comment-detail-view.tsx) for its full context (screenshot,
// element/console/network capture, full thread) — this list keeps only
// the inline triage actions for quick resolve/reopen/create-task.
export function PortForwardCommentsTab({
	projectId,
	environmentId,
	portForwardId,
}: {
	projectId: string;
	environmentId: string;
	portForwardId: string;
}) {
	const { t } = useTranslation("projects");
	const qc = useQueryClient();
	const { hasProjectPermission } = useProjectPermissions(projectId);
	const canResolve = hasProjectPermission("annotations.resolve");
	const canCreateTask =
		hasProjectPermission("annotations.write") &&
		hasProjectPermission("tasks.write");

	const annotationsKey = portForwardAnnotationsQueryOptions(
		projectId,
		environmentId,
		portForwardId,
	).queryKey;
	const { data: annotations, isLoading } = useQuery(
		portForwardAnnotationsQueryOptions(projectId, environmentId, portForwardId),
	);

	const resolveMutation = useMutation({
		mutationFn: (annotationId: string) =>
			resolveAnnotation(projectId, environmentId, portForwardId, annotationId),
		onSuccess: () => qc.invalidateQueries({ queryKey: annotationsKey }),
	});
	const reopenMutation = useMutation({
		mutationFn: (annotationId: string) =>
			reopenAnnotation(projectId, environmentId, portForwardId, annotationId),
		onSuccess: () => qc.invalidateQueries({ queryKey: annotationsKey }),
	});
	const createTaskMutation = useMutation({
		mutationFn: (annotationId: string) =>
			createTaskFromAnnotation(
				projectId,
				environmentId,
				portForwardId,
				annotationId,
			),
		onSuccess: () => qc.invalidateQueries({ queryKey: annotationsKey }),
	});

	if (isLoading) {
		return (
			<div className="space-y-3">
				<Skeleton className="h-20 w-full" />
				<Skeleton className="h-20 w-full" />
			</div>
		);
	}

	if (!annotations || annotations.length === 0) {
		return (
			<div className="flex flex-col items-center justify-center gap-3 py-10 rounded-xl border border-dashed border-border">
				<MessageSquare className="size-7 text-muted-foreground/40" />
				<p className="text-sm text-muted-foreground">
					{t("portForwardDetail.comments.empty")}
				</p>
			</div>
		);
	}

	const grouped = groupByPagePath(annotations);

	return (
		<div className="space-y-6">
			{Array.from(grouped.entries()).map(([pagePath, items]) => (
				<div key={pagePath} className="space-y-2">
					<p className="text-xs font-mono text-muted-foreground truncate">
						{pagePath}
					</p>
					<div className="space-y-2">
						{items.map((annotation) => (
							<AnnotationCard
								key={annotation.id}
								projectId={projectId}
								environmentId={environmentId}
								portForwardId={portForwardId}
								annotation={annotation}
								canResolve={canResolve}
								canCreateTask={canCreateTask}
								onResolve={() => resolveMutation.mutate(annotation.id)}
								onReopen={() => reopenMutation.mutate(annotation.id)}
								onCreateTask={() => createTaskMutation.mutate(annotation.id)}
							/>
						))}
					</div>
				</div>
			))}
		</div>
	);
}

function groupByPagePath(
	annotations: PageAnnotation[],
): Map<string, PageAnnotation[]> {
	const groups = new Map<string, PageAnnotation[]>();
	for (const a of annotations) {
		const list = groups.get(a.page_path) ?? [];
		list.push(a);
		groups.set(a.page_path, list);
	}
	return groups;
}

function AnnotationCard({
	projectId,
	environmentId,
	portForwardId,
	annotation,
	canResolve,
	canCreateTask,
	onResolve,
	onReopen,
	onCreateTask,
}: {
	projectId: string;
	environmentId: string;
	portForwardId: string;
	annotation: PageAnnotation;
	canResolve: boolean;
	canCreateTask: boolean;
	onResolve: () => void;
	onReopen: () => void;
	onCreateTask: () => void;
}) {
	const { t } = useTranslation("projects");
	const { data: screenshotUrl } = useQuery(
		annotationScreenshotUrlQueryOptions(
			projectId,
			environmentId,
			portForwardId,
			annotation,
		),
	);

	return (
		<div className="flex gap-3 rounded-lg border border-border/60 bg-card p-4">
			<Link
				to="/projects/$projectId/environments/$environmentId/port-forwards/$portForwardId/comments/$annotationId"
				params={{
					projectId,
					environmentId,
					portForwardId,
					annotationId: annotation.id,
				}}
				className="size-16 shrink-0 rounded-md border border-border/60 bg-muted flex items-center justify-center overflow-hidden"
			>
				{annotation.screenshot_file_id ? (
					screenshotUrl ? (
						<img
							src={screenshotUrl}
							alt=""
							className="size-full object-cover"
						/>
					) : (
						<Skeleton className="size-full" />
					)
				) : (
					<ImageOff className="size-5 text-muted-foreground/40" />
				)}
			</Link>
			<div className="min-w-0 flex-1 space-y-1.5">
				<div className="flex items-center gap-2">
					<Badge
						variant={annotation.status === "resolved" ? "secondary" : "default"}
					>
						{annotation.status === "resolved"
							? t("portForwardDetail.comments.status.resolved")
							: t("portForwardDetail.comments.status.open")}
					</Badge>
					<span className="text-xs text-muted-foreground">
						{new Date(annotation.created_at).toLocaleString()}
					</span>
					{annotation.comments.length > 0 && (
						<span className="text-xs text-muted-foreground">
							·{" "}
							{t("portForwardDetail.comments.replyCount", {
								count: annotation.comments.length,
							})}
						</span>
					)}
				</div>
				<Link
					to="/projects/$projectId/environments/$environmentId/port-forwards/$portForwardId/comments/$annotationId"
					params={{
						projectId,
						environmentId,
						portForwardId,
						annotationId: annotation.id,
					}}
					className="block text-sm hover:underline"
				>
					{annotation.body}
				</Link>
				<p className="text-xs text-muted-foreground truncate">
					{annotation.element_snapshot.tag_name} ·{" "}
					{annotation.element_snapshot.text_excerpt}
				</p>
				<div className="flex items-center gap-2 pt-1">
					{canResolve &&
						(annotation.status === "open" ? (
							<Button size="sm" variant="outline" onClick={onResolve}>
								<CheckCircle2 className="size-3.5 mr-1.5" />
								{t("portForwardDetail.comments.resolve")}
							</Button>
						) : (
							<Button size="sm" variant="outline" onClick={onReopen}>
								<RotateCcw className="size-3.5 mr-1.5" />
								{t("portForwardDetail.comments.reopen")}
							</Button>
						))}
					{canCreateTask && !annotation.task_id && (
						<Button size="sm" variant="outline" onClick={onCreateTask}>
							<SquareCheckBig className="size-3.5 mr-1.5" />
							{t("portForwardDetail.comments.createTask")}
						</Button>
					)}
					{annotation.task_id && (
						<span className="text-xs text-muted-foreground">
							{t("portForwardDetail.comments.taskCreated")}
						</span>
					)}
				</div>
			</div>
		</div>
	);
}
