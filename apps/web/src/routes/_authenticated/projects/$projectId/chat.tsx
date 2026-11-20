import { createFileRoute } from "@tanstack/react-router";
import { ChatLayout } from "@/components/team-chat/chat-layout";

export const Route = createFileRoute(
	"/_authenticated/projects/$projectId/chat",
)({
	component: ProjectChat,
});

function ProjectChat() {
	const { projectId } = Route.useParams();
	return <ChatLayout projectId={projectId} />;
}
