import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import {
	AlertCircle,
	ArrowLeft,
	Check,
	CheckCircle2,
	Copy,
	ExternalLink,
	MessageSquare,
	Plus,
	RotateCcw,
	SquareCheckBig,
} from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Avatar, AvatarFallback, AvatarImage } from "@/components/ui/avatar";
import { Badge } from "@/components/ui/badge";
import { Button, buttonVariants } from "@/components/ui/button";
import {
	DropdownMenu,
	DropdownMenuContent,
	DropdownMenuItem,
	DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Skeleton } from "@/components/ui/skeleton";
import { Textarea } from "@/components/ui/textarea";
import { useProjectPermissions } from "@/hooks/use-project-permissions";
import {
	addComment,
	annotationQueryOptions,
	annotationScreenshotUrlQueryOptions,
	createTaskFromAnnotation,
	type PageAnnotation,
	reopenAnnotation,
	resolveAnnotation,
} from "@/lib/annotation-api";
import {
	environmentConfigQueryOptions,
	portForwardQueryOptions,
	portForwardUrl,
} from "@/lib/environment-api";

// A full page for one comment — reached from PortForwardCommentsTab's list
// (or a link shared directly). Genuinely additive, not just a relocation:
// element_selector/accessible_name/console_errors/failed_requests are
// captured on every annotation but had no UI surface anywhere in the web
// app before this — only the extension's own popover showed any of it.
export function CommentDetailView({
	projectId,
	environmentId,
	portForwardId,
	annotationId,
}: {
	projectId: string;
	environmentId: string;
	portForwardId: string;
	annotationId: string;
}) {
	const { t } = useTranslation("projects");
	const qc = useQueryClient();
	const { hasProjectPermission } = useProjectPermissions(projectId);
	const canResolve = hasProjectPermission("annotations.resolve");
	const canReply = hasProjectPermission("annotations.write");
	const canCreateTask =
		hasProjectPermission("annotations.write") &&
		hasProjectPermission("tasks.write");

	const annotationKey = annotationQueryOptions(
		projectId,
		environmentId,
		portForwardId,
		annotationId,
	).queryKey;
	const { data: annotation, isLoading } = useQuery(
		annotationQueryOptions(
			projectId,
			environmentId,
			portForwardId,
			annotationId,
		),
	);
	const { data: screenshotUrl } = useQuery({
		...annotationScreenshotUrlQueryOptions(
			projectId,
			environmentId,
			portForwardId,
			annotation ?? ({ screenshot_file_id: null } as PageAnnotation),
		),
		enabled: Boolean(annotation?.screenshot_file_id),
	});
	// Only for the "Open" button below — jumps straight to the exact page
	// the comment was made on, on the port forward's own live dev server
	// (an entirely different destination from the back-nav link, which goes
	// to this comment's page *inside* Paca).
	const { data: config } = useQuery(environmentConfigQueryOptions());
	const { data: portForward } = useQuery(
		portForwardQueryOptions(projectId, environmentId, portForwardId),
	);

	const [reply, setReply] = useState("");
	const [linkCopied, setLinkCopied] = useState(false);

	const invalidate = () => qc.invalidateQueries({ queryKey: annotationKey });

	const handleCopyLink = () => {
		const url = `${window.location.origin}/projects/${projectId}/environments/${environmentId}/port-forwards/${portForwardId}/comments/${annotationId}`;
		navigator.clipboard?.writeText(url)?.catch(() => {});
		setLinkCopied(true);
		setTimeout(() => setLinkCopied(false), 2000);
	};

	const resolveMutation = useMutation({
		mutationFn: () =>
			resolveAnnotation(projectId, environmentId, portForwardId, annotationId),
		onSuccess: invalidate,
	});
	const reopenMutation = useMutation({
		mutationFn: () =>
			reopenAnnotation(projectId, environmentId, portForwardId, annotationId),
		onSuccess: invalidate,
	});
	const replyMutation = useMutation({
		mutationFn: (body: string) =>
			addComment(projectId, environmentId, portForwardId, annotationId, body),
		onSuccess: () => {
			setReply("");
			invalidate();
		},
	});
	// The backend's own description-builder already embeds the comment body
	// (plus page path, element, and captured console/network context) into
	// the new task, and attaches the screenshot if one exists — nothing
	// extra to build here, just kick it off and jump to the result.
	const createTaskMutation = useMutation({
		mutationFn: () =>
			createTaskFromAnnotation(
				projectId,
				environmentId,
				portForwardId,
				annotationId,
			),
		onSuccess: (updated) => {
			invalidate();
			if (updated.task_id) {
				window.open(
					`${window.location.origin}/projects/${projectId}/tasks/${updated.task_id}`,
					"_blank",
					"noopener,noreferrer",
				);
			}
		},
	});

	// Opens a new tab rather than navigating in place -- the comment stays
	// on screen either way, and a new tab is also the only option here:
	// staging the annotation into useContextInjectionStore and navigating
	// in-place would work for THIS tab, but that store is in-memory only, so
	// a new tab's own module instance would never see it. The new-conversation
	// route (projects/$projectId/conversations/index.tsx) reads the
	// `annotationId` param back out and does the actual staging itself.
	const handleCreateConversation = () => {
		if (!annotation) return;
		window.open(
			`${window.location.origin}/projects/${projectId}/conversations?annotationId=${annotation.id}`,
			"_blank",
			"noopener,noreferrer",
		);
	};

	if (isLoading) {
		return (
			<div className="flex h-full flex-col overflow-hidden bg-background">
				<div className="shrink-0 border-b border-border/40 px-5 py-2.5">
					<Skeleton className="h-4 w-32" />
				</div>
				<div className="flex-1 overflow-y-auto px-4 lg:px-8 py-6 space-y-6 max-w-2xl mx-auto w-full">
					<Skeleton className="h-48 w-full rounded-lg" />
					<Skeleton className="h-20 w-full rounded-lg" />
					<Skeleton className="h-20 w-full rounded-lg" />
				</div>
			</div>
		);
	}

	if (!annotation) {
		return (
			<div className="flex h-full flex-col items-center justify-center gap-4 text-muted-foreground/60">
				<AlertCircle className="size-10" />
				<div className="text-center">
					<p className="text-base font-medium text-foreground/70">
						{t("commentDetail.notFound.title")}
					</p>
					<p className="text-sm mt-1">
						{t("commentDetail.notFound.description")}
					</p>
				</div>
				<Link
					to="/projects/$projectId/environments/$environmentId/port-forwards/$portForwardId"
					params={{ projectId, environmentId, portForwardId }}
					hash="comments"
					className="flex items-center gap-1.5 rounded-lg border border-border/60 px-4 py-2 text-sm font-medium text-foreground/70 hover:bg-muted/50 transition-colors mt-2"
				>
					<ArrowLeft className="size-4" />
					{t("commentDetail.notFound.backToPortForward")}
				</Link>
			</div>
		);
	}

	const resolved = annotation.status === "resolved";
	const hostPort = portForward?.host_port ?? null;

	return (
		<div className="flex h-full flex-col overflow-hidden bg-background">
			{/* Back navigation strip */}
			<div className="shrink-0 border-b border-border/40 px-5 py-2.5 flex items-center justify-between gap-3">
				<Link
					to="/projects/$projectId/environments/$environmentId/port-forwards/$portForwardId"
					params={{ projectId, environmentId, portForwardId }}
					hash="comments"
					className="flex items-center gap-1.5 text-sm text-muted-foreground hover:text-foreground transition-colors"
				>
					<ArrowLeft className="size-3.5" />
					{t("commentDetail.notFound.backToPortForward")}
				</Link>
				<div className="flex items-center gap-2">
					{hostPort !== null && config?.port_forward_host && (
						<Button
							size="sm"
							variant="outline"
							onClick={() =>
								window.open(
									portForwardUrl(
										config.port_forward_host,
										hostPort,
										annotation.page_path,
									),
									"_blank",
									"noopener,noreferrer",
								)
							}
						>
							<ExternalLink className="size-3.5 mr-1.5" />
							{t("portForwardDetail.overview.open")}
						</Button>
					)}
					<DropdownMenu>
						<DropdownMenuTrigger
							className={buttonVariants({ size: "sm", variant: "outline" })}
						>
							<Plus className="size-3.5 mr-1.5" />
							{t("commentDetail.actions.create")}
						</DropdownMenuTrigger>
						<DropdownMenuContent align="end" className="w-52">
							{canCreateTask &&
								(annotation.task_id ? (
									<DropdownMenuItem disabled>
										<SquareCheckBig className="size-3.5 mr-2" />
										{t("portForwardDetail.comments.taskCreated")}
									</DropdownMenuItem>
								) : (
									<DropdownMenuItem
										onClick={() => createTaskMutation.mutate()}
										disabled={createTaskMutation.isPending}
									>
										<SquareCheckBig className="size-3.5 mr-2" />
										{t("portForwardDetail.comments.createTask")}
									</DropdownMenuItem>
								))}
							<DropdownMenuItem onClick={handleCreateConversation}>
								<MessageSquare className="size-3.5 mr-2" />
								{t("commentDetail.actions.createConversation")}
							</DropdownMenuItem>
						</DropdownMenuContent>
					</DropdownMenu>
				</div>
			</div>

			<div className="flex-1 overflow-y-auto px-4 lg:px-8 py-6 space-y-6 max-w-2xl mx-auto w-full">
				{annotation.screenshot_file_id && (
					<div className="rounded-lg border border-border/60 bg-muted overflow-hidden">
						{screenshotUrl ? (
							<img src={screenshotUrl} alt="" className="w-full" />
						) : (
							<Skeleton className="h-48 w-full" />
						)}
					</div>
				)}

				<div className="space-y-4">
					<div className="flex items-center gap-2">
						<Badge variant={resolved ? "secondary" : "default"}>
							{resolved
								? t("portForwardDetail.comments.status.resolved")
								: t("portForwardDetail.comments.status.open")}
						</Badge>
						<span className="text-xs text-muted-foreground">
							{t("commentDetail.header.createdAt", {
								date: new Date(annotation.created_at).toLocaleString(),
							})}
						</span>
					</div>

					<CommentEntry
						name={annotation.created_by_name}
						username={annotation.created_by_username}
						avatarUrl={
							annotation.created_by_avatar_thumb_url ??
							annotation.created_by_avatar_url
						}
						createdAt={annotation.created_at}
						body={annotation.body}
					/>
					{annotation.comments.map((c) => (
						<CommentEntry
							key={c.id}
							name={c.created_by_name}
							username={c.created_by_username}
							avatarUrl={
								c.created_by_avatar_thumb_url ?? c.created_by_avatar_url
							}
							createdAt={c.created_at}
							body={c.body}
						/>
					))}
				</div>

				{canReply && (
					<div className="space-y-2">
						<Textarea
							value={reply}
							onChange={(e) => setReply(e.target.value)}
							placeholder={t("commentDetail.thread.replyPlaceholder")}
							rows={3}
						/>
						<div className="flex justify-end">
							<Button
								size="sm"
								disabled={!reply.trim() || replyMutation.isPending}
								onClick={() => replyMutation.mutate(reply.trim())}
							>
								{t("commentDetail.thread.reply")}
							</Button>
						</div>
					</div>
				)}

				<div className="flex items-center gap-2 pt-1 border-t border-border/40 pt-4">
					{canResolve &&
						(resolved ? (
							<Button
								size="sm"
								variant="outline"
								onClick={() => reopenMutation.mutate()}
							>
								<RotateCcw className="size-3.5 mr-1.5" />
								{t("portForwardDetail.comments.reopen")}
							</Button>
						) : (
							<Button
								size="sm"
								variant="outline"
								onClick={() => resolveMutation.mutate()}
							>
								<CheckCircle2 className="size-3.5 mr-1.5" />
								{t("portForwardDetail.comments.resolve")}
							</Button>
						))}
					<Button size="sm" variant="outline" onClick={handleCopyLink}>
						{linkCopied ? (
							<>
								<Check className="size-3.5 mr-1.5 text-emerald-500" />
								{t("commentDetail.actions.linkCopied")}
							</>
						) : (
							<>
								<Copy className="size-3.5 mr-1.5" />
								{t("commentDetail.actions.copyLink")}
							</>
						)}
					</Button>
				</div>

				<CommentContext annotation={annotation} />
			</div>
		</div>
	);
}

function CommentEntry({
	name,
	username,
	avatarUrl,
	createdAt,
	body,
}: {
	name: string;
	username: string;
	avatarUrl?: string;
	createdAt: string;
	body: string;
}) {
	const displayName = name || username;
	return (
		<div className="flex gap-2.5">
			<Avatar className="size-7 shrink-0">
				{avatarUrl ? <AvatarImage src={avatarUrl} /> : null}
				<AvatarFallback className="text-xs font-medium">
					{displayName ? displayName.charAt(0).toUpperCase() : "?"}
				</AvatarFallback>
			</Avatar>
			<div className="min-w-0 flex-1">
				<div className="flex items-baseline gap-2">
					<span className="text-sm font-medium">{displayName}</span>
					<span className="text-xs text-muted-foreground">
						{new Date(createdAt).toLocaleString()}
					</span>
				</div>
				<p className="text-sm whitespace-pre-wrap break-words">{body}</p>
			</div>
		</div>
	);
}

// Surfaces element_selector/accessible_name/console_errors/failed_requests
// — captured by the extension on every comment but, before this page,
// never shown anywhere in the web app.
function CommentContext({ annotation }: { annotation: PageAnnotation }) {
	const { t } = useTranslation("projects");
	return (
		<div className="space-y-3 rounded-lg border border-border/60 bg-card p-4">
			<p className="text-xs font-medium text-muted-foreground uppercase tracking-wide">
				{t("commentDetail.context.title")}
			</p>
			<div className="space-y-1.5 text-sm">
				<ContextRow
					label={t("commentDetail.context.pagePath")}
					value={annotation.page_path}
					mono
				/>
				<ContextRow
					label={t("commentDetail.context.elementSelector")}
					value={annotation.element_selector}
					mono
				/>
				{annotation.element_snapshot.accessible_name && (
					<ContextRow
						label={t("commentDetail.context.accessibleName")}
						value={annotation.element_snapshot.accessible_name}
					/>
				)}
			</div>

			<div className="space-y-1.5">
				<p className="text-xs font-medium text-muted-foreground">
					{t("commentDetail.context.consoleErrors")}
				</p>
				{annotation.console_errors.length === 0 ? (
					<p className="text-xs text-muted-foreground/70">
						{t("commentDetail.context.noConsoleErrors")}
					</p>
				) : (
					<ul className="space-y-1">
						{annotation.console_errors.map((entry, i) => (
							<li
								// biome-ignore lint/suspicious/noArrayIndexKey: captured, immutable list with no stable id
								key={i}
								className="text-xs font-mono text-destructive break-all"
							>
								[{entry.level}] {entry.message}
							</li>
						))}
					</ul>
				)}
			</div>

			<div className="space-y-1.5">
				<p className="text-xs font-medium text-muted-foreground">
					{t("commentDetail.context.failedRequests")}
				</p>
				{annotation.failed_requests.length === 0 ? (
					<p className="text-xs text-muted-foreground/70">
						{t("commentDetail.context.noFailedRequests")}
					</p>
				) : (
					<ul className="space-y-1">
						{annotation.failed_requests.map((req, i) => (
							<li
								// biome-ignore lint/suspicious/noArrayIndexKey: captured, immutable list with no stable id
								key={i}
								className="text-xs font-mono text-destructive break-all"
							>
								{req.method} {req.url} — {req.status_code || req.error}
							</li>
						))}
					</ul>
				)}
			</div>
		</div>
	);
}

function ContextRow({
	label,
	value,
	mono,
}: {
	label: string;
	value: string;
	mono?: boolean;
}) {
	return (
		<div className="flex gap-2">
			<span className="text-muted-foreground shrink-0">{label}:</span>
			<span className={mono ? "font-mono break-all" : "break-words"}>
				{value}
			</span>
		</div>
	);
}
