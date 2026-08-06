import { createFileRoute } from "@tanstack/react-router";
import { ConversationView } from "@/components/projects/agents/conversation-view";
import { RouteErrorComponent } from "@/components/route-error-boundary";
import {
	globalConversationEventsQueryOptions,
	globalConversationQueryOptions,
} from "@/lib/agent-api";

export const Route = createFileRoute(
	"/_authenticated/conversations/$conversationId",
)({
	loader: async ({ context: { queryClient }, params: { conversationId } }) => {
		await Promise.all([
			queryClient.ensureQueryData(
				globalConversationQueryOptions(conversationId),
			),
			queryClient.ensureQueryData(
				globalConversationEventsQueryOptions(conversationId),
			),
		]);
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
