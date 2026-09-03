import { createFileRoute } from "@tanstack/react-router";
import { useEffect } from "react";
import { NewConversationThread } from "@/components/projects/agents/new-conversation-thread";
import { getAnnotationInProject } from "@/lib/annotation-api";
import { useContextInjectionStore } from "@/lib/context-injection-store";
import { excerptOf } from "@/lib/mention-api";

export const Route = createFileRoute(
	"/_authenticated/projects/$projectId/conversations/",
)({
	// Populated by comment-detail-view.tsx's "Create conversation" action,
	// which opens this page in a new tab rather than navigating in-place
	// (unlike agents/index.tsx's `?create=`, this crosses a tab boundary, so
	// there's no in-memory store state to hand off — the annotation id is
	// all the URL can carry, and the effect below re-fetches from it).
	validateSearch: (search: Record<string, unknown>) => ({
		annotationId:
			typeof search.annotationId === "string" ? search.annotationId : undefined,
	}),
	component: ProjectNewConversationThread,
});

function ProjectNewConversationThread() {
	const { projectId } = Route.useParams();
	const { annotationId } = Route.useSearch();
	const navigate = Route.useNavigate();

	useEffect(() => {
		if (!annotationId) return;
		// Strip the param immediately (not after the fetch) so a refresh or
		// back-navigation never re-attaches or re-fetches the same comment —
		// mirrors agents/index.tsx's own "consume `?create=` exactly once"
		// convention.
		navigate({
			search: (prev) => ({ ...prev, annotationId: undefined }),
			replace: true,
		});
		getAnnotationInProject(projectId, annotationId)
			.then((annotation) => {
				useContextInjectionStore.getState().add({
					type: "annotation",
					id: annotation.id,
					title: excerptOf(annotation.body),
					projectId,
				});
			})
			.catch(() => {
				// Comment may have been deleted, or the link is stale -- the
				// composer just opens with nothing pre-attached.
			});
	}, [annotationId, projectId, navigate]);

	return <NewConversationThread projectId={projectId} />;
}
