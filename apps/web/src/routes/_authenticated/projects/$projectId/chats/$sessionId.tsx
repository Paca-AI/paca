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
		turnId:
			typeof search.turnId === "string" &&
			/^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i.test(
				search.turnId,
			)
				? search.turnId
				: undefined,
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
	const { turnId } = Route.useSearch();
	return (
		<ProjectChatView
			projectId={projectId}
			sessionId={sessionId}
			focusTurnId={turnId}
		/>
	);
}
