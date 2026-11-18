import { createFileRoute } from "@tanstack/react-router";
import { ChannelBrowser } from "@/components/team-chat/channel-browser";

export const Route = createFileRoute(
	"/_authenticated/projects/$projectId/chat/",
)({
	component: ChatBrowse,
});

function ChatBrowse() {
	const { projectId } = Route.useParams();
	return <ChannelBrowser projectId={projectId} />;
}
