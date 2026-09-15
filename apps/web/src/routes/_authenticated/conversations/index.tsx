import { createFileRoute } from "@tanstack/react-router";
import { NewConversationThread } from "@/components/projects/agents/new-conversation-thread";

export const Route = createFileRoute("/_authenticated/conversations/")({
	// See the project-scoped sibling route's identical `compose` param.
	validateSearch: (search: Record<string, unknown>) => ({
		compose: search.compose === true || search.compose === "true",
	}),
	component: () => <NewConversationThread />,
});
