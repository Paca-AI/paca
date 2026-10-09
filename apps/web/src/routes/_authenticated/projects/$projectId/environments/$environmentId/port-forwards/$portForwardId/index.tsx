import { createFileRoute } from "@tanstack/react-router";
import { PortForwardDetailView } from "@/components/projects/environments/port-forward-detail";
import { portForwardAnnotationsQueryOptions } from "@/lib/annotation-api";
import {
	environmentConfigQueryOptions,
	environmentQueryOptions,
	portForwardQueryOptions,
} from "@/lib/environment-api";

export const Route = createFileRoute(
	"/_authenticated/projects/$projectId/environments/$environmentId/port-forwards/$portForwardId/",
)({
	loader: async ({
		context: { queryClient },
		params: { projectId, environmentId, portForwardId },
	}) => {
		await Promise.all([
			queryClient.ensureQueryData(
				environmentQueryOptions(projectId, environmentId),
			),
			queryClient.ensureQueryData(
				portForwardAnnotationsQueryOptions(
					projectId,
					environmentId,
					portForwardId,
				),
			),
			queryClient.ensureQueryData(environmentConfigQueryOptions()),
		]);
		// Best effort: a caller who may see the environment but not this part
		// of it gets that part's own message on the page, not a failed route.
		await queryClient
			.ensureQueryData(
				portForwardQueryOptions(projectId, environmentId, portForwardId),
			)
			.catch(() => undefined);
	},
	component: PortForwardDetailPage,
});

function PortForwardDetailPage() {
	const { projectId, environmentId, portForwardId } = Route.useParams();
	return (
		<PortForwardDetailView
			projectId={projectId}
			environmentId={environmentId}
			portForwardId={portForwardId}
		/>
	);
}
