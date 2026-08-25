import { createFileRoute } from "@tanstack/react-router";
import { EnvironmentConnectView } from "@/components/projects/environments/environment-connect";
import { useProjectPermissions } from "@/hooks/use-project-permissions";
import {
	environmentConfigQueryOptions,
	environmentQueryOptions,
	environmentSSHKeysQueryOptions,
} from "@/lib/environment-api";

export const Route = createFileRoute(
	"/_authenticated/projects/$projectId/environments/$environmentId/connect",
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
				environmentSSHKeysQueryOptions(projectId, environmentId),
			),
			queryClient.ensureQueryData(environmentConfigQueryOptions()),
		]);
	},
	component: ProjectEnvironmentConnectPage,
});

function ProjectEnvironmentConnectPage() {
	const { projectId, environmentId } = Route.useParams();
	// Same permission gate as the environment detail page (see
	// environment-detail.tsx's own comment on why agents.write, not a
	// dedicated environments.* permission).
	const { hasProjectPermission } = useProjectPermissions(projectId);
	const canWrite = hasProjectPermission("agents.write");
	return (
		<EnvironmentConnectView
			projectId={projectId}
			environmentId={environmentId}
			canWrite={canWrite}
		/>
	);
}
