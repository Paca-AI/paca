import { createReactBlockSpec } from "@blocknote/react";
import { useQuery } from "@tanstack/react-query";
import { AlertCircle, ExternalLink, MessageSquare } from "lucide-react";
import {
	annotationQueryOptions,
	annotationScreenshotUrlQueryOptions,
	type PageAnnotation,
} from "@/lib/annotation-api";
import { excerptOf } from "@/lib/mention-api";

interface AnnotationCardProps {
	id: string;
	projectId: string;
	environmentId: string;
	portForwardId: string;
}

/** A rich preview card for one page comment, replacing what used to be a
 * small inline "#" mention chip — a comment carries a screenshot, an
 * element, a status, and a reply thread, which a one-line inline pill can't
 * show any of. Only the four IDs needed to look it up are stored on the
 * block itself (baked-in title/status would go stale the moment the
 * comment is replied to or resolved elsewhere, the same problem
 * TeamMention's own avatar prop sidesteps by never storing one) — the card
 * always fetches live and renders its own loading/not-found states. */
export const AnnotationCardBlock = createReactBlockSpec(
	{
		type: "annotationCard",
		propSchema: {
			id: { default: "" },
			projectId: { default: "" },
			environmentId: { default: "" },
			portForwardId: { default: "" },
		},
		content: "none",
	},
	{
		render: ({ block }) => {
			const props = block.props as AnnotationCardProps;
			return <AnnotationCardView {...props} />;
		},
		// Degrade to a plain link for markdown/HTML export and any client that
		// doesn't know the annotationCard block type — mirrors
		// blocknote-mermaid-block.tsx's own toExternalHTML fallback.
		toExternalHTML: ({ block }) => {
			const { id, projectId, environmentId, portForwardId } =
				block.props as AnnotationCardProps;
			const url = `${window.location.origin}/projects/${projectId}/environments/${environmentId}/port-forwards/${portForwardId}/comments/${id}`;
			return <a href={url}>Comment</a>;
		},
	},
);

function AnnotationCardView({
	id,
	projectId,
	environmentId,
	portForwardId,
}: AnnotationCardProps) {
	const {
		data: annotation,
		isLoading,
		isError,
	} = useQuery(
		annotationQueryOptions(projectId, environmentId, portForwardId, id),
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

	if (isLoading) {
		return (
			<div className="my-1 flex items-center gap-2 rounded-lg border border-border/40 bg-muted/20 p-3 text-xs text-muted-foreground">
				<MessageSquare className="size-3.5 shrink-0 animate-pulse" />
				Loading comment…
			</div>
		);
	}

	if (isError || !annotation) {
		return (
			<div className="my-1 flex items-center gap-2 rounded-lg border border-border/40 bg-muted/20 p-3 text-xs text-muted-foreground">
				<AlertCircle className="size-3.5 shrink-0" />
				Comment not found — it may have been deleted.
			</div>
		);
	}

	const resolved = annotation.status === "resolved";
	const displayName =
		annotation.created_by_name || annotation.created_by_username;
	const url = `${window.location.origin}/projects/${projectId}/environments/${environmentId}/port-forwards/${portForwardId}/comments/${id}`;

	return (
		<a
			href={url}
			target="_blank"
			rel="noopener noreferrer"
			className="my-1 flex gap-3 rounded-lg border border-border/40 bg-card p-3 no-underline transition-colors hover:border-border/70 hover:bg-muted/30"
			contentEditable={false}
		>
			{annotation.screenshot_file_id && (
				<div className="size-16 shrink-0 overflow-hidden rounded-md border border-border/40 bg-muted">
					{screenshotUrl && (
						<img
							src={screenshotUrl}
							alt=""
							className="size-full object-cover"
						/>
					)}
				</div>
			)}
			<div className="min-w-0 flex-1 space-y-1">
				<div className="flex items-center gap-2">
					<span
						className={`rounded-full px-1.5 py-0.5 text-[10px] font-medium ${
							resolved
								? "bg-muted text-muted-foreground"
								: "bg-primary/10 text-primary"
						}`}
					>
						{resolved ? "Resolved" : "Open"}
					</span>
					<span className="truncate text-xs font-medium text-foreground">
						{displayName}
					</span>
					{annotation.comments.length > 0 && (
						<span className="shrink-0 text-[11px] text-muted-foreground">
							{annotation.comments.length}{" "}
							{annotation.comments.length === 1 ? "reply" : "replies"}
						</span>
					)}
				</div>
				<p className="truncate text-xs text-muted-foreground">
					{annotation.element_snapshot.tag_name}
					{annotation.element_snapshot.accessible_name
						? ` · ${annotation.element_snapshot.accessible_name}`
						: ""}
				</p>
				<p className="line-clamp-2 text-sm text-foreground/90">
					{excerptOf(annotation.body, 140)}
				</p>
			</div>
			<ExternalLink className="size-3.5 shrink-0 self-start text-muted-foreground/50" />
		</a>
	);
}
