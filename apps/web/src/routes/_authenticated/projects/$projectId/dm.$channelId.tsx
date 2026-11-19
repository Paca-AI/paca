import { createFileRoute } from "@tanstack/react-router";
import { RouteErrorComponent } from "@/components/route-error-boundary";
import { ChannelView } from "@/components/team-chat/channel-view";
import { DM_SCOPE } from "@/lib/chat-api";

export const Route = createFileRoute(
	"/_authenticated/projects/$projectId/dm/$channelId",
)({
	validateSearch: (
		search: Record<string, unknown>,
	): { thread?: string; seq?: number } => ({
		thread: typeof search.thread === "string" ? search.thread : undefined,
		seq: typeof search.seq === "number" ? search.seq : undefined,
	}),
	errorComponent: ({ error }) => <RouteErrorComponent error={error} />,
	component: ProjectDirectMessagePage,
});

// The same conversation as /chat/dm/$channelId, opened under a project so the
// sidebar keeps that project's context. Access does not depend on the project:
// a direct message belongs to its two members.
function ProjectDirectMessagePage() {
	const { channelId } = Route.useParams();
	const { thread, seq } = Route.useSearch();
	return (
		<ChannelView
			key={channelId}
			projectId={DM_SCOPE}
			channelId={channelId}
			thread={thread ? { id: thread, seq } : undefined}
		/>
	);
}
