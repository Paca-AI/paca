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
			queryClient.ensureQueryData(environmentConfigQueryOptions()),
		]);
		// Best effort: a caller who may see the environment but not this part
		// of it gets that part's own message on the page, not a failed route.
		await queryClient
			.ensureQueryData(environmentSSHKeysQueryOptions(projectId, environmentId))
			.catch(() => undefined);
	},
	component: ProjectEnvironmentConnectPage,
});

function ProjectEnvironmentConnectPage() {
	const { projectId, environmentId } = Route.useParams();
	const { hasProjectPermission } = useProjectPermissions(projectId);
	// Gates the environment lifecycle action (starting a stopped
	// environment) on the web-app tab — managing the environment's
	// configuration is distinct from being able to open a shell inside it.
	const canWrite = hasProjectPermission("environments:write");
	// Gates every shell-access affordance: the terminal-open link
	// (WebAppConnectTab) and adding/removing SSH keys (SSHConnectTab) — a
	// registered key is just another way to reach the same root shell, see
	// router.go's own environments.connect comment.
	const canConnect = hasProjectPermission("environments:connect");
	return (
		<EnvironmentConnectView
			projectId={projectId}
			environmentId={environmentId}
			canWrite={canWrite}
			canConnect={canConnect}
		/>
	);
}
