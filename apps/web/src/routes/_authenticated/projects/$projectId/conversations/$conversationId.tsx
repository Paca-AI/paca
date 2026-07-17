import { createFileRoute } from "@tanstack/react-router";
import { ConversationView } from "@/components/projects/agents/conversation-view";
import { RouteErrorComponent } from "@/components/route-error-boundary";
import {
	conversationEventsQueryOptions,
	conversationQueryOptions,
} from "@/lib/agent-api";

export const Route = createFileRoute(
	"/_authenticated/projects/$projectId/conversations/$conversationId",
)({
	loader: async ({
		context: { queryClient },
		params: { projectId, conversationId },
	}) => {
		await Promise.all([
			queryClient.ensureQueryData(
				conversationQueryOptions(projectId, conversationId),
			),
			queryClient.ensureQueryData(
				conversationEventsQueryOptions(projectId, conversationId),
			),
		]);
	},
	// Without an errorComponent, a loader failure (e.g. deleted conversation,
	// API 500) bubbles up and crashes the router's internal Lazy wrapper.
	errorComponent: ({ error }) => <RouteErrorComponent error={error} />,
	component: ConversationPage,
});

function ConversationPage() {
	const { projectId, conversationId } = Route.useParams();

	return (
		<ConversationView projectId={projectId} conversationId={conversationId} />
	);
}
