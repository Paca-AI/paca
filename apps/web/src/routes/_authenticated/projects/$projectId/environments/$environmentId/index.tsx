import { createFileRoute } from "@tanstack/react-router";
import { EnvironmentDetailView } from "@/components/projects/environments/environment-detail";
import {
	environmentConfigQueryOptions,
	environmentFoldersQueryOptions,
	environmentQueryOptions,
} from "@/lib/environment-api";

export const Route = createFileRoute(
	"/_authenticated/projects/$projectId/environments/$environmentId/",
)({
	loader: async ({
		context: { queryClient },
		params: { projectId, environmentId },
	}) => {
		const [environment] = await Promise.all([
			queryClient.ensureQueryData(
				environmentQueryOptions(projectId, environmentId),
			),
			queryClient.ensureQueryData(environmentConfigQueryOptions()),
		]);
		// Folders are gated on RequireEnvironmentAccess when the environment
		// is restricted — prefetching this unconditionally would 403 the
		// whole route loader (and with it, this entire page, including the
		// Overview tab that doesn't need it) for a member who isn't granted
		// access. Sequenced after the environment fetch above since
		// access_mode/access_granted live on that response — same fix as
		// the Connect route's own loader.
		if (
			environment.access_mode !== "restricted" ||
			environment.access_granted
		) {
			await queryClient.ensureQueryData(
				environmentFoldersQueryOptions(projectId, environmentId),
			);
		}
	},
	component: ProjectEnvironmentDetailPage,
});

function ProjectEnvironmentDetailPage() {
	const { projectId, environmentId } = Route.useParams();
	return (
		<EnvironmentDetailView
			projectId={projectId}
			environmentId={environmentId}
		/>
	);
}
