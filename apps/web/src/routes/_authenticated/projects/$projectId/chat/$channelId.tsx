import { createFileRoute } from "@tanstack/react-router";
import { RouteErrorComponent } from "@/components/route-error-boundary";
import { ChannelView } from "@/components/team-chat/channel-view";

export const Route = createFileRoute(
	"/_authenticated/projects/$projectId/chat/$channelId",
)({
	validateSearch: (
		search: Record<string, unknown>,
	): { thread?: string; seq?: number } => ({
		thread: typeof search.thread === "string" ? search.thread : undefined,
		seq: typeof search.seq === "number" ? search.seq : undefined,
	}),
	errorComponent: ({ error }) => <RouteErrorComponent error={error} />,
	component: ChannelPage,
});

function ChannelPage() {
	const { projectId, channelId } = Route.useParams();
	const { thread, seq } = Route.useSearch();
	return (
		<ChannelView
			key={channelId}
			projectId={projectId}
			channelId={channelId}
			thread={thread ? { id: thread, seq } : undefined}
		/>
	);
}
