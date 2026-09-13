import { createFileRoute } from "@tanstack/react-router";
import { CommentDetailView } from "@/components/projects/environments/comment-detail-view";
import { annotationQueryOptions } from "@/lib/annotation-api";

export const Route = createFileRoute(
	"/_authenticated/projects/$projectId/environments/$environmentId/port-forwards/$portForwardId/comments/$annotationId",
)({
	loader: async ({
		context: { queryClient },
		params: { projectId, environmentId, portForwardId, annotationId },
	}) => {
		await queryClient.ensureQueryData(
			annotationQueryOptions(
				projectId,
				environmentId,
				portForwardId,
				annotationId,
			),
		);
	},
	component: CommentDetailPage,
});

function CommentDetailPage() {
	const { projectId, environmentId, portForwardId, annotationId } =
		Route.useParams();
	return (
		<CommentDetailView
			projectId={projectId}
			environmentId={environmentId}
			portForwardId={portForwardId}
			annotationId={annotationId}
		/>
	);
}
