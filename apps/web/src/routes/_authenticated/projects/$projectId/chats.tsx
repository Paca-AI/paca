import { createFileRoute } from "@tanstack/react-router";
import { ProjectChatsLayout } from "@/components/projects/agents/project-chats-layout";
import {
	agentsQueryOptions,
	projectChatSessionsQueryOptions,
} from "@/lib/agent-api";

export const Route = createFileRoute(
	"/_authenticated/projects/$projectId/chats",
)({
	loader: async ({ context: { queryClient }, params: { projectId } }) => {
		await Promise.all([
			queryClient.ensureInfiniteQueryData(
				projectChatSessionsQueryOptions(projectId),
			),
			queryClient.ensureQueryData(agentsQueryOptions(projectId)),
		]);
	},
	component: ProjectChatsPageLayout,
});

function ProjectChatsPageLayout() {
	const { projectId } = Route.useParams();
	return <ProjectChatsLayout projectId={projectId} />;
}
