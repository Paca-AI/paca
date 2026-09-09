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
		const [environment] = await Promise.all([
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
		// The port forward itself is gated on RequireEnvironmentAccess when
		// the environment is restricted (annotations above aren't — see
		// annotation_service.go's own doc comment on that being a separate,
		// deliberately out-of-scope gap) — prefetching it unconditionally
		// would 403 the whole route loader for a member who isn't granted
		// access. Sequenced after the environment fetch above, same fix as
		// the environment detail and Connect routes' own loaders.
		if (
			environment.access_mode !== "restricted" ||
			environment.access_granted
		) {
			await queryClient.ensureQueryData(
				portForwardQueryOptions(projectId, environmentId, portForwardId),
			);
		}
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
