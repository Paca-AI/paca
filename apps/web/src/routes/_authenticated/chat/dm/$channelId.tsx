import { createFileRoute } from "@tanstack/react-router";
import { RouteErrorComponent } from "@/components/route-error-boundary";
import { ChannelView } from "@/components/team-chat/channel-view";
import { DM_SCOPE } from "@/lib/chat-api";

export const Route = createFileRoute("/_authenticated/chat/dm/$channelId")({
	validateSearch: (
		search: Record<string, unknown>,
	): { thread?: string; seq?: number } => ({
		thread: typeof search.thread === "string" ? search.thread : undefined,
		seq: typeof search.seq === "number" ? search.seq : undefined,
	}),
	errorComponent: ({ error }) => <RouteErrorComponent error={error} />,
	component: DirectMessagePage,
});

// A direct message has no project: DM_SCOPE stands in for it, and the chat
// calls, caches and permission checks switch on it.
function DirectMessagePage() {
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
