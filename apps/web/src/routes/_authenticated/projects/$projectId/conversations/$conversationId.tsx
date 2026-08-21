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
		// Prefetches the conversation itself (agent, status, etc.) — the events
		// window is fetched separately by useConversationEventWindow and opens
		// on the newest page on its own, with no dependency on this data.
		const conversation = await queryClient.ensureQueryData(
			conversationQueryOptions(projectId, conversationId),
		);
		return { readOnly: conversation.chat_session_id != null };
	},
	// Without an errorComponent, a loader failure (e.g. deleted conversation,
	// API 500) bubbles up and crashes the router's internal Lazy wrapper.
	errorComponent: ({ error }) => <RouteErrorComponent error={error} />,
	component: ConversationPage,
});

function ConversationPage() {
	const { projectId, conversationId } = Route.useParams();
	const { readOnly } = Route.useLoaderData();

	return (
		<ConversationView
			projectId={projectId}
			conversationId={conversationId}
			readOnly={readOnly}
		/>
	);
}
