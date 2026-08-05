import { createFileRoute } from "@tanstack/react-router";
import { ConversationsLayout } from "@/components/projects/agents/conversations-layout";
import { agentsQueryOptions, conversationsQueryOptions } from "@/lib/agent-api";

export const Route = createFileRoute(
	"/_authenticated/projects/$projectId/conversations",
)({
	loader: async ({ context: { queryClient }, params: { projectId } }) => {
		await Promise.all([
			queryClient.ensureInfiniteQueryData(conversationsQueryOptions(projectId)),
			queryClient.ensureQueryData(agentsQueryOptions(projectId)),
		]);
	},
	component: ProjectConversationsLayout,
});

function ProjectConversationsLayout() {
	const { projectId } = Route.useParams();
	return <ConversationsLayout projectId={projectId} />;
}
