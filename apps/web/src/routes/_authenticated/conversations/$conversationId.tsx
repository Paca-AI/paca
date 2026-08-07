import { createFileRoute } from "@tanstack/react-router";
import { ConversationView } from "@/components/projects/agents/conversation-view";
import { RouteErrorComponent } from "@/components/route-error-boundary";
import { globalConversationQueryOptions } from "@/lib/agent-api";

export const Route = createFileRoute(
	"/_authenticated/conversations/$conversationId",
)({
	loader: async ({ context: { queryClient }, params: { conversationId } }) => {
		// Prefetches the conversation itself (agent, status, etc.) — the events
		// window is fetched separately by useConversationEventWindow and opens
		// on the newest page on its own, with no dependency on this data.
		await queryClient.ensureQueryData(
			globalConversationQueryOptions(conversationId),
		);
	},
	// Without an errorComponent, a loader failure (e.g. deleted conversation,
	// API 500) bubbles up and crashes the router's internal Lazy wrapper.
	errorComponent: ({ error }) => <RouteErrorComponent error={error} />,
	component: GlobalConversationPage,
});

function GlobalConversationPage() {
	const { conversationId } = Route.useParams();
	return <ConversationView conversationId={conversationId} />;
}
