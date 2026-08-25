import { createFileRoute } from "@tanstack/react-router";
import { EnvironmentTerminalPage } from "@/components/projects/environments/environment-terminal-page";
import { environmentQueryOptions } from "@/lib/environment-api";

// Opened in a new browser tab from the Connect page's "web app" tab — see
// environment-terminal-page.tsx's own doc comment for why this renders as
// a full-viewport overlay rather than living outside the `_authenticated`
// layout.
export const Route = createFileRoute(
	"/_authenticated/projects/$projectId/environments/$environmentId/terminal",
)({
	loader: async ({
		context: { queryClient },
		params: { projectId, environmentId },
	}) => {
		await queryClient.ensureQueryData(
			environmentQueryOptions(projectId, environmentId),
		);
	},
	component: ProjectEnvironmentTerminalPage,
});

function ProjectEnvironmentTerminalPage() {
	const { projectId, environmentId } = Route.useParams();
	return (
		<EnvironmentTerminalPage
			projectId={projectId}
			environmentId={environmentId}
		/>
	);
}
