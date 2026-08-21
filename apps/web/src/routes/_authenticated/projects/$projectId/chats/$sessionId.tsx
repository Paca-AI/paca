import { createFileRoute } from "@tanstack/react-router";
import { ProjectChatView } from "@/components/projects/agents/project-chat-view";
import { RouteErrorComponent } from "@/components/route-error-boundary";
import {
	projectChatSessionQueryOptions,
	projectChatTurnsQueryOptions,
} from "@/lib/agent-api";

export const Route = createFileRoute(
	"/_authenticated/projects/$projectId/chats/$sessionId",
)({
	validateSearch: (search: Record<string, unknown>) => ({
		turnId: typeof search.turnId === "string" ? search.turnId : undefined,
	}),
	loader: async ({
		context: { queryClient },
		params: { projectId, sessionId },
	}) => {
		await Promise.all([
			queryClient.ensureQueryData(
				projectChatSessionQueryOptions(projectId, sessionId),
			),
			queryClient.ensureInfiniteQueryData(
				projectChatTurnsQueryOptions(projectId, sessionId),
			),
		]);
	},
	errorComponent: ({ error }) => <RouteErrorComponent error={error} />,
	component: ProjectChatSessionPage,
});

function ProjectChatSessionPage() {
	const { projectId, sessionId } = Route.useParams();
	return <ProjectChatView projectId={projectId} sessionId={sessionId} />;
}
