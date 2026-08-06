import { createFileRoute } from "@tanstack/react-router";
import { NewConversationThread } from "@/components/projects/agents/new-conversation-thread";

export const Route = createFileRoute(
	"/_authenticated/projects/$projectId/conversations/",
)({
	component: ProjectNewConversationThread,
});

function ProjectNewConversationThread() {
	const { projectId } = Route.useParams();
	return <NewConversationThread projectId={projectId} />;
}
