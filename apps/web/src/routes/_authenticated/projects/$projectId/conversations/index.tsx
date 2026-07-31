import type { ThreadMessageLike } from "@assistant-ui/react";
import {
	type AppendMessage,
	AssistantRuntimeProvider,
	useExternalStoreRuntime,
} from "@assistant-ui/react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { createFileRoute, useNavigate } from "@tanstack/react-router";
import { useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { Thread } from "@/components/assistant-ui/thread";
import {
	AgentPickerContext,
	AgentPickerInline,
	type AgentPickerState,
} from "@/components/projects/agents/agent-picker";
import {
	agentsQueryOptions,
	conversationQueryOptions,
	startChatSession,
} from "@/lib/agent-api";

export const Route = createFileRoute(
	"/_authenticated/projects/$projectId/conversations/",
)({
	component: NewConversationThread,
});

// No conversation is selected — landing on `/conversations` directly (via the
// sidebar nav item or the "New conversation" button) always shows this blank
// composer, never an existing conversation. Render a live assistant-ui
// Thread with the agent picker docked in the composer itself
// (`ComposerStart`) — same box as the message input, no separate step
// before you can type. Same component used by the floating chat widget.
function NewConversationThread() {
	const { t } = useTranslation("projects");
	const { projectId } = Route.useParams();
	const qc = useQueryClient();
	const navigate = useNavigate();

	const [agentId, setAgentId] = useState("");
	const { data: agents = [], isLoading: agentsLoading } = useQuery(
		agentsQueryOptions(projectId),
	);

	const onNew = async (message: AppendMessage) => {
		if (!agentId) throw new Error(t("aiChat.selectAgentFirst"));
		if (message.content.length !== 1 || message.content[0]?.type !== "text") {
			throw new Error(t("agents.conversationView.textOnlyMessage"));
		}
		const text = message.content[0].text;

		const result = await startChatSession(projectId, agentId, {
			message: text,
		});
		qc.setQueryData(
			conversationQueryOptions(projectId, result.conversation.id).queryKey,
			result.conversation,
		);
		void qc.invalidateQueries({
			queryKey: ["projects", projectId, "conversations"],
		});
		navigate({
			to: "/projects/$projectId/conversations/$conversationId",
			params: { projectId, conversationId: result.conversation.id },
		});
	};

	const messages: ThreadMessageLike[] = [];

	const runtime = useExternalStoreRuntime<ThreadMessageLike>({
		messages,
		isRunning: false,
		convertMessage: (m) => m,
		onNew,
		isSendDisabled: !agentId,
	});

	const pickerState = useMemo<AgentPickerState>(
		() => ({
			agents,
			agentsLoading,
			agentId,
			onAgentChange: setAgentId,
			projectId,
		}),
		[agents, agentsLoading, agentId, projectId],
	);

	return (
		<AgentPickerContext.Provider value={pickerState}>
			<AssistantRuntimeProvider runtime={runtime}>
				<Thread components={{ ComposerStart: AgentPickerInline }} />
			</AssistantRuntimeProvider>
		</AgentPickerContext.Provider>
	);
}
