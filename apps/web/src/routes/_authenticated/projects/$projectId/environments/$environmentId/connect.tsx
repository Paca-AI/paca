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
		const [environment] = await Promise.all([
			queryClient.ensureQueryData(
				environmentQueryOptions(projectId, environmentId),
			),
			queryClient.ensureQueryData(environmentConfigQueryOptions()),
		]);
		// SSH keys are gated on RequireEnvironmentAccess when the environment
		// is restricted — prefetching this unconditionally would 403 the
		// whole route loader (and with it, this entire page) for a member
		// who isn't granted access, instead of letting the page render its
		// own "restricted" message. Sequenced after the environment fetch
		// above since access_mode/access_granted live on that response.
		if (
			environment.access_mode !== "restricted" ||
			environment.access_granted
		) {
			await queryClient.ensureQueryData(
				environmentSSHKeysQueryOptions(projectId, environmentId),
			);
		}
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
