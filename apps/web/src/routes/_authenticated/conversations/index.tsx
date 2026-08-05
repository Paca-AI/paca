import { createFileRoute } from "@tanstack/react-router";
import { NewConversationThread } from "@/components/projects/agents/new-conversation-thread";

export const Route = createFileRoute("/_authenticated/conversations/")({
	component: () => <NewConversationThread />,
});
