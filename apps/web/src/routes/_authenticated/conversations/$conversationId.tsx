import { createFileRoute } from "@tanstack/react-router";
import { ConversationView } from "@/components/projects/agents/conversation-view";
import { RouteErrorComponent } from "@/components/route-error-boundary";

export const Route = createFileRoute(
	"/_authenticated/conversations/$conversationId",
)({
	// The conversation itself isn't prefetched here — ConversationView's own
	// useQuery already handles loading/not-found/permission-denied/failed
	// states with the right UI for each (NoPermissionState for a 403,
	// distinct from a genuinely deleted conversation or one that failed to
	// run). Prefetching it here meant a 403 on first load bubbled past that
	// handling entirely and showed this route's generic errorComponent
	// instead — kept below only as a backstop for a genuinely unexpected
	// render crash, not as the normal path for a restricted conversation.
	errorComponent: ({ error }) => <RouteErrorComponent error={error} />,
	component: GlobalConversationPage,
});

function GlobalConversationPage() {
	const { conversationId } = Route.useParams();
	return <ConversationView conversationId={conversationId} />;
}
