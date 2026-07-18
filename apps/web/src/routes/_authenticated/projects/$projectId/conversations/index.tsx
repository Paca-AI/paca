import type { InfiniteData } from "@tanstack/react-query";
import { createFileRoute, redirect } from "@tanstack/react-router";
import { MessageSquare } from "lucide-react";
import { useTranslation } from "react-i18next";
import {
	type ConversationListResult,
	conversationsQueryOptions,
} from "@/lib/agent-api";

export const Route = createFileRoute(
	"/_authenticated/projects/$projectId/conversations/",
)({
	// The parent `conversations` layout route's loader already populated the
	// conversations list in the query cache, so this can read it synchronously
	// instead of refetching — landing directly on the most recently created
	// conversation instead of showing a bare "pick one" screen. The list's
	// first page is ordered newest-first, so its first item is the latest
	// conversation without needing to scan every loaded page.
	//
	// `preload` is true when this loader runs speculatively (e.g. the sidebar
	// nav Link's hover-intent preload, since the router defaults to
	// `defaultPreload: "intent"`) rather than for a real navigation — must not
	// redirect in that case, or merely hovering the nav item silently
	// navigates the page away from wherever the user actually is.
	loader: ({ context: { queryClient }, params: { projectId }, preload }) => {
		if (preload) return;

		const data = queryClient.getQueryData<InfiniteData<ConversationListResult>>(
			conversationsQueryOptions(projectId).queryKey,
		);
		const latest = data?.pages[0]?.items[0];
		if (!latest) return;

		throw redirect({
			to: "/projects/$projectId/conversations/$conversationId",
			params: { projectId, conversationId: latest.id },
			replace: true,
		});
	},
	component: EmptyConversationState,
});

function EmptyConversationState() {
	const { t } = useTranslation("projects");
	return (
		<div className="flex flex-col h-full items-center justify-center gap-3 text-center px-6">
			<MessageSquare className="size-10 text-muted-foreground/40" />
			<div>
				<p className="text-sm font-medium">
					{t("conversationsPage.detail.empty.title")}
				</p>
				<p className="text-xs text-muted-foreground mt-1 max-w-xs">
					{t("conversationsPage.detail.empty.description")}
				</p>
			</div>
		</div>
	);
}
