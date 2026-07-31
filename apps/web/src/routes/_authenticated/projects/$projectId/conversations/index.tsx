import type { ThreadMessageLike } from "@assistant-ui/react";
import {
	type AppendMessage,
	AssistantRuntimeProvider,
	useExternalStoreRuntime,
} from "@assistant-ui/react";
import { useQueryClient } from "@tanstack/react-query";
import { createFileRoute, useNavigate } from "@tanstack/react-router";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Thread } from "@/components/assistant-ui/thread";
import {
	AgentPickerContext,
	AgentPickerInline,
	useAgentPicker,
} from "@/components/projects/agents/agent-picker";
import { extractTextOnlyContent } from "@/components/projects/agents/conversation-to-thread-messages";
import { conversationQueryOptions, startChatSession } from "@/lib/agent-api";

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

	const { agentId, pickerState } = useAgentPicker(projectId);
	const [isSubmitting, setIsSubmitting] = useState(false);

	const onNew = async (message: AppendMessage) => {
		if (!agentId) throw new Error(t("aiChat.selectAgentFirst"));
		const text = extractTextOnlyContent(message);
		if (text === null) {
			throw new Error(t("agents.conversationView.textOnlyMessage"));
		}

		// Guards against a fast double-Enter firing two chat sessions before
		// the first request resolves and this component navigates away.
		setIsSubmitting(true);
		try {
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
		} finally {
			setIsSubmitting(false);
		}
	};

	const messages: ThreadMessageLike[] = [];

	const runtime = useExternalStoreRuntime<ThreadMessageLike>({
		messages,
		isRunning: false,
		convertMessage: (m) => m,
		onNew,
		isSendDisabled: !agentId || isSubmitting,
	});

	return (
		<AgentPickerContext.Provider value={pickerState}>
			<AssistantRuntimeProvider runtime={runtime}>
				<Thread components={{ ComposerStart: AgentPickerInline }} />
			</AssistantRuntimeProvider>
		</AgentPickerContext.Provider>
	);
}
