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
		await Promise.all([
			queryClient.ensureQueryData(
				environmentQueryOptions(projectId, environmentId),
			),
			queryClient.ensureQueryData(
				environmentFoldersQueryOptions(projectId, environmentId),
			),
			queryClient.ensureQueryData(environmentConfigQueryOptions()),
		]);
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
