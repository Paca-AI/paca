import { createFileRoute } from "@tanstack/react-router";
import { ConversationView } from "@/components/projects/agents/conversation-view";
import { RouteErrorComponent } from "@/components/route-error-boundary";
import { conversationQueryOptions } from "@/lib/agent-api";

export const Route = createFileRoute(
	"/_authenticated/projects/$projectId/conversations/$conversationId",
)({
	loader: async ({
		context: { queryClient },
		params: { projectId, conversationId },
	}) => {
		// The conversation only: it carries `event_count`, which is all the
		// view needs to open on the newest events.
		await queryClient.ensureQueryData(
			conversationQueryOptions(projectId, conversationId),
		);
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
