import {
	type AppendMessage,
	AssistantRuntimeProvider,
	useExternalStoreRuntime,
} from "@assistant-ui/react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { AlertTriangle, Bot, Loader2, Plus, Square, X } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { Thread } from "@/components/assistant-ui/thread";
import {
	AgentPickerContext,
	AgentPickerInline,
	useAgentPicker,
} from "@/components/projects/agents/agent-picker";
import { Button } from "@/components/ui/button";
import {
	type AgentConversation,
	agentQueryOptions,
	CONVERSATION_HEARTBEAT_INTERVAL_MS,
	conversationEventsQueryOptions,
	conversationQueryOptions,
	heartbeatConversation,
	pauseConversation,
	sendChatMessage,
	sendConversationMessage,
	startChatSession,
	stopConversation,
} from "@/lib/agent-api";
import { cn } from "@/lib/utils";
import { ConversationErrorBox } from "./agents/conversation-error-box";
import {
	canReplyToConversation,
	eventsToThreadMessages,
	extractTextOnlyContent,
	isEnvironmentReady,
} from "./agents/conversation-to-thread-messages";

interface AIChatFloatProps {
	projectId: string;
}

// Floating chatbox is a compact overlay — no room for (and no need for) the
// full-page welcome heading, so suppress it here; the conversation page's
// blank composer still shows it via Thread's default.
const THREAD_COMPONENTS = {
	ComposerStart: AgentPickerInline,
	Welcome: () => null,
};

// ── Failed conversation banner ────────────────────────────────────────────────

/**
 * Shown when the current conversation failed without producing any visible
 * messages. The Thread shows nothing for such a conversation; without this
 * banner the user would see a blank chat panel with no feedback.
 */
function FloatingChatFailedBanner({
	message,
}: {
	message: string | null | undefined;
}) {
	const { t } = useTranslation("projects");
	return (
		<div className="flex flex-col items-center gap-3 px-6 py-8 text-center">
			<div className="flex size-10 items-center justify-center rounded-full bg-destructive/10">
				<AlertTriangle className="size-5 text-destructive" />
			</div>
			<div className="space-y-1">
				<p className="text-sm font-medium text-destructive">
					{t("agents.conversationView.failed")}
				</p>
				<p className="text-xs text-muted-foreground wrap-break-word">
					{message ?? t("agents.conversationView.noOutput")}
				</p>
			</div>
		</div>
	);
}

// ── Component ─────────────────────────────────────────────────────────────────

export function AIChatFloat({ projectId }: AIChatFloatProps) {
	const { t } = useTranslation("projects");
	const [open, setOpen] = useState(false);
	const [conversationId, setConversationId] = useState<string | null>(null);
	const [isSubmitting, setIsSubmitting] = useState(false);
	const [isStopping, setIsStopping] = useState(false);
	const qc = useQueryClient();

	// Locked once a conversation exists — the agent is fixed for its
	// lifetime; switching it here would silently do nothing useful.
	const { agentId, pickerState } = useAgentPicker(projectId, {
		disabled: !!conversationId,
	});

	const { data: conversation } = useQuery({
		...conversationQueryOptions(projectId, conversationId ?? ""),
		enabled: !!conversationId,
	});
	const { data: events = [] } = useQuery({
		...conversationEventsQueryOptions(projectId, conversationId ?? ""),
		enabled: !!conversationId,
	});
	const { data: agent } = useQuery({
		...agentQueryOptions(projectId, conversation?.agent_id ?? ""),
		enabled: !!conversation?.agent_id,
	});
	const isACP = agent?.agent_type === "acp";
	const environmentReady = isEnvironmentReady(isACP, events);

	const isRunning =
		conversation?.status === "queued" || conversation?.status === "running";
	const isTerminal =
		conversation?.status === "finished" ||
		conversation?.status === "failed" ||
		conversation?.status === "stopped";

	const messages = useMemo(
		() => (conversationId ? eventsToThreadMessages(events, isRunning) : []),
		[events, isRunning, conversationId],
	);

	const invalidate = (id: string | null = conversationId) => {
		if (id) {
			qc.invalidateQueries({
				queryKey: ["projects", projectId, "conversations", id],
			});
		}
		qc.invalidateQueries({
			queryKey: ["projects", projectId, "conversations"],
		});
	};

	const onNew = async (message: AppendMessage) => {
		const text = extractTextOnlyContent(message);
		if (text === null) {
			throw new Error(t("agents.conversationView.textOnlyMessage"));
		}

		// Guards against a fast double-Enter firing two requests (e.g. two
		// chat sessions) before the first one resolves and flips isRunning.
		setIsSubmitting(true);
		try {
			if (!conversationId) {
				if (!agentId) throw new Error(t("aiChat.selectAgentFirst"));
				const result = await startChatSession(projectId, agentId, {
					message: text,
				});
				// Seed the cache before flipping conversationId so the Thread
				// doesn't flash a "can't reply" state while the query resolves.
				qc.setQueryData(
					conversationQueryOptions(projectId, result.conversation.id).queryKey,
					result.conversation,
				);
				setConversationId(result.conversation.id);
				void qc.invalidateQueries({
					queryKey: ["projects", projectId, "conversations"],
				});
				return;
			}

			if (!conversation) {
				throw new Error(t("agents.conversationView.conversationEnded"));
			}
			if (!conversation.chat_session_id) {
				// ACP, or an LLM conversation attached to a static environment
				// (see canReplyToConversation's own doc comment) — reply in
				// place on the same conversation_id rather than through a chat
				// session.
				await sendConversationMessage(projectId, conversation.id, text);
				invalidate();
				return;
			}
			const result = await sendChatMessage(
				projectId,
				conversation.agent_id,
				conversation.chat_session_id,
				{ message: text },
			);
			// The previous conversation may have already ended (explicitly
			// stopped, or reaped after 3 minutes with no heartbeat) — replying
			// then silently starts a fresh conversation server-side. Follow it,
			// otherwise the UI keeps polling the old (now terminal) conversation
			// and the reply appears to vanish.
			if (result.id !== conversationId) {
				qc.setQueryData(
					conversationQueryOptions(projectId, result.id).queryKey,
					result,
				);
				setConversationId(result.id);
			}
			invalidate(result.id);
		} finally {
			setIsSubmitting(false);
		}
	};

	const onCancel = async () => {
		if (!conversationId) return;
		await pauseConversation(projectId, conversationId);
		invalidate();
	};

	const stopCurrentConversation = async () => {
		if (!conversationId) return;
		setIsStopping(true);
		try {
			await stopConversation(projectId, conversationId);
			invalidate();
		} finally {
			setIsStopping(false);
		}
	};

	// !conversationId: no conversation yet, so there's nothing to be blocked
	// on — the composer is for starting a brand new one. See
	// canReplyToConversation's own doc comment for every other case.
	const canReply =
		!conversationId || canReplyToConversation(conversation, isACP);

	const showFailedBanner =
		!!conversationId &&
		isTerminal &&
		conversation?.status === "failed" &&
		messages.length === 0 &&
		!canReply;

	const runtime = useExternalStoreRuntime({
		messages,
		isRunning,
		convertMessage: (m) => m,
		onNew,
		onCancel,
		isDisabled: !canReply,
		isSendDisabled: (!conversationId && !agentId) || isSubmitting,
	});

	// Chat sandboxes are kept alive between replies (see conversation-view's
	// composer) — starting a new conversation in this window should
	// explicitly end the one being left behind rather than leaving it running
	// indefinitely. Best-effort: a failure here (e.g. it already ended) is
	// not worth surfacing to the user.
	//
	// Exception: while the agent is actively working (queued/running), don't
	// stop it — let it finish server-side; the user can check back later.
	// Only "paused" (idle, waiting for a reply) gets stopped here.
	//
	// Collapsing the floating panel does NOT end the conversation anymore —
	// only starting a new one here, or the tab going quiet for 3 minutes
	// (see the heartbeat effect below), does.
	function endConversation(id: string) {
		const cached = qc.getQueryData<AgentConversation>(
			conversationQueryOptions(projectId, id).queryKey,
		);
		if (cached?.status === "queued" || cached?.status === "running") return;
		void stopConversation(projectId, id).catch(() => {});
	}

	function handleNewConversation() {
		if (conversationId) endConversation(conversationId);
		setConversationId(null);
	}

	function handleToggleOpen() {
		setOpen((o) => !o);
	}

	// Pings the agent-runner service every ~30s while this tab has a conversation
	// loaded (regardless of whether the panel is expanded or collapsed), so
	// its sandbox's idle timer never trips as long as the tab stays open. If
	// the tab closes (or the network drops), heartbeats simply stop and
	// agent-runner's idle reaper reclaims the sandbox once ~3 minutes pass with
	// no heartbeat — this replaces the old pagehide-triggered immediate stop.
	// ACP conversations skip this entirely: there's no cloud sandbox to keep
	// alive (the user's local bridge daemon owns that lifecycle instead), so
	// heartbeating one would just be a wasted round trip. Same for a
	// conversation attached to a static environment (environment_id set) —
	// see conversation-view.tsx's matching heartbeat effect for why that's a
	// guaranteed no-op server-side too.
	useEffect(() => {
		if (!conversationId || isTerminal || isACP || conversation?.environment_id)
			return;
		const id = conversationId;
		void heartbeatConversation(projectId, id).catch(() => {});
		const interval = setInterval(() => {
			void heartbeatConversation(projectId, id).catch(() => {});
		}, CONVERSATION_HEARTBEAT_INTERVAL_MS);
		return () => clearInterval(interval);
	}, [
		conversationId,
		conversation?.environment_id,
		isTerminal,
		isACP,
		projectId,
	]);

	return (
		<>
			{/* Floating trigger button */}
			<button
				type="button"
				aria-label={t("aiChat.chatWithAgent")}
				onClick={handleToggleOpen}
				className={cn(
					"fixed bottom-6 right-6 z-40 flex size-12 items-center justify-center rounded-full shadow-lg transition-all hover:scale-105",
					open
						? "bg-muted text-foreground border border-border"
						: "bg-primary text-primary-foreground hover:bg-primary/90",
				)}
			>
				{open ? <X className="size-5" /> : <Bot className="size-5" />}
			</button>

			{/* Chat panel */}
			{open && (
				<div
					className={cn(
						"fixed bottom-20 right-6 z-40 flex w-95 flex-col overflow-hidden rounded-2xl border border-border/60 bg-background shadow-2xl",
						conversationId ? "h-150" : "max-h-150",
					)}
				>
					{/* Panel header */}
					<div className="flex shrink-0 items-center justify-between border-b border-border/40 bg-muted/30 px-4 py-3">
						<div className="flex items-center gap-2">
							<Bot className="size-4 text-primary" />
							<span className="text-sm font-semibold">
								{t("aiChat.chatWithAgent")}
							</span>
						</div>
						{conversationId && (
							<div className="flex items-center gap-2">
								{conversation &&
									!isTerminal &&
									!conversation.environment_id && (
										<Button
											size="sm"
											variant="outline"
											className="h-7 gap-1.5 text-xs text-destructive border-destructive/30 hover:bg-destructive/10"
											onClick={() => void stopCurrentConversation()}
											disabled={isStopping}
										>
											{isStopping ? (
												<Loader2 className="size-3 animate-spin" />
											) : (
												<Square className="size-3" />
											)}
											{t("agents.conversationView.stopConversation", {
												defaultValue: "Stop conversation",
											})}
										</Button>
									)}
								<Button
									size="sm"
									variant="outline"
									className="h-7 gap-1.5 text-xs"
									onClick={handleNewConversation}
								>
									<Plus className="size-3" />
									{t("aiChat.newConversation")}
								</Button>
							</div>
						)}
					</div>

					<div className="min-h-0 flex-1 overflow-y-auto">
						{showFailedBanner ? (
							<FloatingChatFailedBanner message={conversation?.error_message} />
						) : (
							<AgentPickerContext.Provider value={pickerState}>
								<AssistantRuntimeProvider runtime={runtime}>
									<Thread
										components={{
											...THREAD_COMPONENTS,
											ComposerCancelLabel: isACP
												? t("agents.thread.stopCurrentResponseAriaLabel", {
														defaultValue: "Stop current response",
													})
												: undefined,
										}}
										environmentReady={environmentReady}
										// A chat_message trigger always persists the user's own
										// message before the agent runs (see handler.Handle), so
										// a failed/recoverable turn almost never has
										// messages.length === 0 — showFailedBanner above
										// essentially never fires for the common case. Rendered
										// inside the message flow (not a page footer below the
										// composer, which read as small print easy to miss) so a
										// failure with a visible message still explains itself.
										viewportOverlay={
											conversation?.error_message ? (
												<ConversationErrorBox
													message={conversation.error_message}
												/>
											) : undefined
										}
									/>
								</AssistantRuntimeProvider>
							</AgentPickerContext.Provider>
						)}
					</div>
				</div>
			)}
		</>
	);
}
