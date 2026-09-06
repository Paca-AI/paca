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
import { useProjectPermissions } from "@/hooks/use-project-permissions";
import {
	conversationQueryOptions,
	globalConversationQueryOptions,
	startChatSession,
	startGlobalChatSession,
} from "@/lib/agent-api";
import { useContextInjectionStore } from "@/lib/context-injection-store";
import { useAgentBusyPrompt } from "./agent-busy-dialog";
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

	// Global chat (no projectId) is deliberately open to any authenticated
	// user (see router.go's global chat-session routes); only gate starting a
	// project-scoped conversation, which the backend now requires
	// conversations.write for — a PROJECT_VIEWER (conversations.read only)
	// can land on this route directly (e.g. via the sidebar nav item) even
	// with the "New conversation" button itself hidden elsewhere, so the
	// composer needs its own guard too.
	const { hasProjectPermission } = useProjectPermissions(projectId ?? "");
	const canStartConversation =
		!projectId || hasProjectPermission("conversations.write");
	const { dialog: agentBusyDialog, send: sendWithBusyPrompt } =
		useAgentBusyPrompt();

	const onNew = async (message: AppendMessage) => {
		if (!agentId) throw new Error(t("aiChat.selectAgentFirst"));
		const text = extractTextOnlyContent(message);
		if (text === null) {
			throw new Error(t("agents.conversationView.textOnlyMessage"));
		}
		// Snapshot now (not read again after any await) so a badge staged
		// mid-send can't sneak into this message or get cleared under it.
		const contextItems = useContextInjectionStore.getState().items;

		// Guards against a fast double-Enter firing two chat sessions before
		// the first request resolves and this component navigates away.
		setIsSubmitting(true);
		try {
			if (projectId) {
				const result = await sendWithBusyPrompt((onBusy) =>
					startChatSession(projectId, agentId, {
						message: text,
						...(environmentId ? { environment_id: environmentId } : {}),
						...(folderId ? { folder_id: folderId } : {}),
						contextItems,
						on_busy: onBusy,
					}),
				);
				useContextInjectionStore.getState().clear();
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
				const result = await sendWithBusyPrompt((onBusy) =>
					startGlobalChatSession(agentId, {
						message: text,
						contextItems,
						on_busy: onBusy,
					}),
				);
				useContextInjectionStore.getState().clear();
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
		isDisabled: !canStartConversation,
		isSendDisabled: !canStartConversation || !agentId || isSubmitting,
	});

	return (
		<AgentPickerContext.Provider value={pickerState}>
			<EnvironmentPickerContext.Provider value={environmentPickerState}>
				<AssistantRuntimeProvider runtime={runtime}>
					<Thread components={{ ComposerStart: ComposerStartRow }} />
				</AssistantRuntimeProvider>
			</EnvironmentPickerContext.Provider>
			{agentBusyDialog}
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
