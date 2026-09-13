import { createFileRoute } from "@tanstack/react-router";
import { ConversationsLayout } from "@/components/projects/agents/conversations-layout";

export const Route = createFileRoute(
	"/_authenticated/projects/$projectId/conversations",
)({
	// Neither the conversation list itself nor agentsQueryOptions is
	// prefetched here — both are fetched by ConversationsLayout's own
	// queries, each gated on the permission it actually needs
	// (conversations.read for the list, enabled unconditionally but
	// defaulting to [] for the agents.read-gated name/avatar enrichment). A
	// role with conversations.read but not agents.read (the whole point of
	// splitting conversations.* off agents.* — see agent-api.ts) can still
	// read this list; prefetching either unconditionally in the loader
	// meant a missing permission crashed the whole page — or, for the list
	// itself, silently re-fired the same failing request on every visit —
	// instead of showing NoPermissionState in place of just the list.
	component: ProjectConversationsLayout,
});

function ProjectConversationsLayout() {
	const { projectId } = Route.useParams();
	return <ConversationsLayout projectId={projectId} />;
}
