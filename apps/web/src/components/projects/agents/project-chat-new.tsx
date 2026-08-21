import type { AppendMessage, ThreadMessageLike } from "@assistant-ui/react";
import {
	AssistantRuntimeProvider,
	useExternalStoreRuntime,
} from "@assistant-ui/react";
import { useQueryClient } from "@tanstack/react-query";
import { Link, useNavigate } from "@tanstack/react-router";
import { ArrowLeft } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { Thread } from "@/components/assistant-ui/thread";
import { Button } from "@/components/ui/button";
import { useProjectChatPermissions } from "@/hooks/use-can-use-project-chats";
import {
	createProjectChatSession,
	type ProjectChatContextSourceRef,
	projectChatSessionQueryOptions,
} from "@/lib/agent-api";
import {
	mergeRequiredProjectChatSources,
	taskChatInitialContextSources,
} from "@/lib/project-chat-navigation";
import {
	AgentPickerContext,
	AgentPickerInline,
	usePrivateChatAgentPicker,
} from "./agent-picker";
import { extractTextOnlyContent } from "./conversation-to-thread-messages";
import { ProjectChatCommandMenu } from "./project-chat-command-menu";
import { ProjectChatContextPicker } from "./project-chat-context-picker";

interface PendingCommand {
	fingerprint: string;
	key: string;
}

export function ProjectChatNew({
	projectId,
	initialTaskId,
	initialAgentId,
}: {
	projectId: string;
	initialTaskId?: string;
	initialAgentId?: string;
}) {
	const { t } = useTranslation("projects");
	const qc = useQueryClient();
	const navigate = useNavigate();
	const { canUseTaskContext, canPublishConclusion } =
		useProjectChatPermissions(projectId);
	const picker = usePrivateChatAgentPicker(projectId, { initialAgentId });
	const requiredSources = taskChatInitialContextSources(initialTaskId);
	const [sources, setSources] = useState<ProjectChatContextSourceRef[]>(
		() => requiredSources,
	);
	const [isSubmitting, setIsSubmitting] = useState(false);
	const [sendError, setSendError] = useState<string>();
	const pendingRef = useRef<PendingCommand | null>(null);

	useEffect(() => {
		setSources(taskChatInitialContextSources(initialTaskId));
		pendingRef.current = null;
	}, [initialTaskId]);

	const onNew = async (message: AppendMessage) => {
		if (!picker.agentId) throw new Error(t("aiChat.selectAgentFirst"));
		const text = extractTextOnlyContent(message);
		if (text === null) {
			throw new Error(t("agents.conversationView.textOnlyMessage"));
		}
		const submissionSources = mergeRequiredProjectChatSources(
			sources,
			requiredSources,
		);
		const fingerprint = JSON.stringify({
			agentId: picker.agentId,
			message: text,
			sources: submissionSources,
		});
		if (pendingRef.current?.fingerprint !== fingerprint) {
			pendingRef.current = { fingerprint, key: crypto.randomUUID() };
		}

		setSendError(undefined);
		setIsSubmitting(true);
		try {
			const result = await createProjectChatSession(
				projectId,
				{
					agent_id: picker.agentId,
					message: text,
					context_sources: submissionSources,
				},
				pendingRef.current.key,
			);
			const session = result.bundle.session;
			if (!session) throw new Error(t("chats.errors.missingSession"));
			qc.setQueryData(
				projectChatSessionQueryOptions(projectId, session.id).queryKey,
				session,
			);
			void qc.invalidateQueries({
				queryKey: ["projects", projectId, "chat-sessions"],
			});
			pendingRef.current = null;
			navigate({
				to: "/projects/$projectId/chats/$sessionId",
				params: { projectId, sessionId: session.id },
				search: { turnId: undefined },
			});
		} catch {
			if (runtime.thread.composer.getState().text.length === 0) {
				runtime.thread.composer.setText(text);
			}
			setSendError(t("chats.errors.sendFailed"));
		} finally {
			setIsSubmitting(false);
		}
	};

	const runtime = useExternalStoreRuntime<ThreadMessageLike>({
		messages: [],
		isRunning: false,
		convertMessage: (message) => message,
		onNew,
		isSendDisabled:
			!picker.agentId ||
			isSubmitting ||
			(!!initialTaskId && !canUseTaskContext),
	});
	const hasTaskContext = sources.some((source) => source.type === "task");
	const ChatComposerStart = () => (
		<div className="flex items-center gap-1">
			<AgentPickerInline />
			<ProjectChatContextPicker
				projectId={projectId}
				value={sources}
				requiredSources={requiredSources}
				canSelectTasks={canUseTaskContext}
				iconOnly
				onChange={setSources}
				disabled={isSubmitting}
			/>
			{canPublishConclusion && (
				<ProjectChatCommandMenu
					hasTaskContext={hasTaskContext}
					disabled={isSubmitting}
				/>
			)}
		</div>
	);
	const ChatComposerPrompt = () =>
		sendError ? (
			<div
				role="alert"
				className="mb-2 rounded-lg border border-destructive/30 bg-destructive/5 px-3 py-2 text-sm text-destructive"
			>
				{sendError}
			</div>
		) : null;

	return (
		<div className="flex h-full min-h-0 flex-col">
			<div className="min-h-0 flex-1">
				<AgentPickerContext.Provider value={picker.pickerState}>
					<AssistantRuntimeProvider runtime={runtime}>
						<Thread
							components={{
								ComposerStart: ChatComposerStart,
								ComposerPrompt: ChatComposerPrompt,
							}}
							viewportHeader={
								<div className="mb-2 md:hidden">
									<Button
										variant="ghost"
										size="icon-sm"
										nativeButton={false}
										render={
											<Link
												to="/projects/$projectId/chats"
												params={{ projectId }}
												search={{
													contextTaskId: undefined,
													draft: undefined,
													agentId: undefined,
												}}
											/>
										}
									>
										<ArrowLeft className="size-4" />
										<span className="sr-only">{t("chats.backToList")}</span>
									</Button>
								</div>
							}
						/>
					</AssistantRuntimeProvider>
				</AgentPickerContext.Provider>
			</div>
		</div>
	);
}
