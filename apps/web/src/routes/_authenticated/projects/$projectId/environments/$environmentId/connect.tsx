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
	const { hasProjectPermission } = useProjectPermissions(projectId);
	const canWrite = hasProjectPermission("environments.write");
	// Gates only the terminal-open link (WebAppConnectTab) — opening a
	// shell is a distinct capability from managing the environment's
	// configuration, see router.go's own environments.connect comment.
	const canConnect = hasProjectPermission("environments.connect");
	return (
		<EnvironmentConnectView
			projectId={projectId}
			environmentId={environmentId}
			canWrite={canWrite}
			canConnect={canConnect}
		/>
	);
}
