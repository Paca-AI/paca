import { createFileRoute, redirect } from "@tanstack/react-router";
import { MessageSquare } from "lucide-react";
import { useTranslation } from "react-i18next";
import {
	type AgentConversation,
	conversationsQueryOptions,
} from "@/lib/agent-api";

export const Route = createFileRoute(
	"/_authenticated/projects/$projectId/conversations/",
)({
	// The parent `conversations` layout route's loader already populated the
	// conversations list in the query cache, so this can read it synchronously
	// instead of refetching — landing directly on the most recently created
	// conversation instead of showing a bare "pick one" screen.
	loader: ({ context: { queryClient }, params: { projectId } }) => {
		const conversations =
			queryClient.getQueryData<AgentConversation[]>(
				conversationsQueryOptions(projectId).queryKey,
			) ?? [];
		if (conversations.length === 0) return;

		const latest = conversations.reduce((a, b) =>
			new Date(a.created_at) > new Date(b.created_at) ? a : b,
		);
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
