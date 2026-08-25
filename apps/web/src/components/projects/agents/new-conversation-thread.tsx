import type { ThreadMessageLike } from "@assistant-ui/react";
import {
	type AppendMessage,
	AssistantRuntimeProvider,
	useExternalStoreRuntime,
} from "@assistant-ui/react";
import { useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Thread } from "@/components/assistant-ui/thread";
import {
	conversationQueryOptions,
	globalConversationQueryOptions,
	startChatSession,
	startGlobalChatSession,
} from "@/lib/agent-api";
import {
	AgentPickerContext,
	AgentPickerInline,
	EnvironmentPickerContext,
	EnvironmentPickerInline,
	FolderPickerInline,
	useAgentPicker,
	useEnvironmentPicker,
	useGlobalAgentPicker,
} from "./agent-picker";
import { extractTextOnlyContent } from "./conversation-to-thread-messages";

// Shared between the project-scoped Conversations page's blank-composer
// index route and the global one — see conversations-layout.tsx for the
// list+outlet shell this renders inside.
//
// No conversation is selected — landing on the bare "/conversations" route
// directly (via the sidebar nav item or the "New conversation" button)
// always shows this blank composer, never an existing conversation. Renders
// a live assistant-ui Thread with the agent picker docked in the composer
// itself (`ComposerStart`) — same box as the message input, no separate step
// before you can type. Same component used by the floating chat widget.
export function NewConversationThread({
	projectId,
}: {
	/** Absent for the global Conversations page (home/admin pages, no project). */
	projectId?: string;
}) {
	const { t } = useTranslation("projects");
	const qc = useQueryClient();
	const navigate = useNavigate();

	// Both hooks are always called (never conditionally, per the rules of
	// hooks) — each internally no-ops its query when disabled, so only the
	// one matching the current scope actually fetches.
	const projectPicker = useAgentPicker(projectId ?? "", {
		enabled: !!projectId,
	});
	const globalPicker = useGlobalAgentPicker({ enabled: !projectId });
	const { agentId, pickerState } = projectId ? projectPicker : globalPicker;

	// Environments are project-scoped only — this hook internally no-ops
	// (via `enabled`) when there's no project, and EnvironmentPickerInline
	// stays invisible whenever its context is unset or empty, so this is
	// fully additive for the global Conversations page.
	const {
		environmentId,
		folderId,
		pickerState: environmentPickerState,
	} = useEnvironmentPicker(projectId ?? "", agentId, {
		enabled: !!projectId,
	});

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
			if (projectId) {
				const result = await startChatSession(projectId, agentId, {
					message: text,
					...(environmentId ? { environment_id: environmentId } : {}),
					...(folderId ? { folder_id: folderId } : {}),
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
			} else {
				const result = await startGlobalChatSession(agentId, {
					message: text,
				});
				qc.setQueryData(
					globalConversationQueryOptions(result.conversation.id).queryKey,
					result.conversation,
				);
				void qc.invalidateQueries({
					queryKey: ["global-chat", "conversations"],
				});
				navigate({
					to: "/conversations/$conversationId",
					params: { conversationId: result.conversation.id },
				});
			}
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
			<EnvironmentPickerContext.Provider value={environmentPickerState}>
				<AssistantRuntimeProvider runtime={runtime}>
					<Thread components={{ ComposerStart: ComposerStartRow }} />
				</AssistantRuntimeProvider>
			</EnvironmentPickerContext.Provider>
		</AgentPickerContext.Provider>
	);
}

// ComposerStart takes no props (see AgentPickerInline's doc comment), so the
// agent, environment, and folder pickers are docked side by side in one
// small wrapper rather than passing separate component slots through
// assistant-ui.
function ComposerStartRow() {
	return (
		<div className="flex items-center gap-1.5">
			<AgentPickerInline />
			<EnvironmentPickerInline />
			<FolderPickerInline />
		</div>
	);
}
