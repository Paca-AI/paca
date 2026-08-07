import {
	type AppendMessage,
	AssistantRuntimeProvider,
	useExternalStoreRuntime,
} from "@assistant-ui/react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
	AlertTriangle,
	Bot,
	GitBranch,
	GitPullRequest,
	Loader2,
	Square,
} from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { Thread } from "@/components/assistant-ui/thread";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import {
	type AgentConversation,
	agentQueryOptions,
	CONVERSATION_HEARTBEAT_INTERVAL_MS,
	CONVERSATION_STATUS_COLORS,
	CONVERSATION_STATUS_LABELS,
	chattableAgentsQueryOptions,
	conversationQueryOptions,
	globalConversationQueryOptions,
	heartbeatConversation,
	heartbeatGlobalConversation,
	pauseConversation,
	pauseGlobalConversation,
	sendChatMessage,
	sendConversationMessage,
	sendGlobalChatMessage,
	sendGlobalConversationMessage,
	stopConversation,
	stopGlobalConversation,
} from "@/lib/agent-api";
import { cn } from "@/lib/utils";
import {
	eventsToThreadMessages,
	extractTextOnlyContent,
} from "./conversation-to-thread-messages";
import { LoadOlderEvents, TailFollowIndicator } from "./event-window-controls";
import { useConversationEventWindow } from "./use-conversation-event-window";

// ── Controls ──────────────────────────────────────────────────────────────────

function ConversationControls({
	projectId,
	conversation,
	isACP,
}: {
	/** Absent for a global-chat conversation (home/admin pages, no project). */
	projectId?: string;
	conversation: AgentConversation;
	isACP: boolean;
}) {
	const { t } = useTranslation("projects");
	const qc = useQueryClient();

	const invalidate = () => {
		if (projectId) {
			qc.invalidateQueries({
				queryKey: ["projects", projectId, "conversations", conversation.id],
			});
			qc.invalidateQueries({
				queryKey: ["projects", projectId, "conversations"],
			});
		} else {
			qc.invalidateQueries({
				queryKey: ["global-chat", "conversations", conversation.id],
			});
			qc.invalidateQueries({ queryKey: ["global-chat", "conversations"] });
		}
	};

	const stopMut = useMutation({
		mutationFn: async () => {
			if (projectId) {
				await stopConversation(projectId, conversation.id);
			} else {
				await stopGlobalConversation(conversation.id);
			}
		},
		onSuccess: invalidate,
	});

	// assistant-ui's own composer shows a Cancel button while running, but
	// only for chat conversations — its composer is hidden entirely for
	// task/comment-triggered ones (see `isDisabled` below), which would
	// otherwise have no way to stop a running conversation at all. Show this
	// control for every non-terminal status (queued, running, paused) so a
	// stop action is always available, regardless of trigger type.
	//
	// ACP is the exception: its composer is now shown for every trigger type
	// (see canReply below), so the composer's own Cancel/pause button is
	// always reachable there — this header Stop button (a full teardown,
	// distinct from pause) would just be redundant.
	const isTerminal =
		conversation.status === "finished" ||
		conversation.status === "failed" ||
		conversation.status === "stopped";
	if (isTerminal || isACP) return null;

	return (
		<div className="flex items-center gap-2">
			<Button
				size="sm"
				variant="outline"
				className="h-7 text-xs gap-1.5 text-destructive border-destructive/30 hover:bg-destructive/10"
				onClick={() => stopMut.mutate()}
				disabled={stopMut.isPending}
			>
				{stopMut.isPending ? (
					<Loader2 className="size-3 animate-spin" />
				) : (
					<Square className="size-3" />
				)}
				{t("agents.conversationView.stop")}
			</Button>
		</div>
	);
}

// ── Main component ────────────────────────────────────────────────────────────

interface ConversationViewProps {
	/** Absent for a global-chat conversation (home/admin pages, no project). */
	projectId?: string;
	conversationId: string;
}

export function ConversationView({
	projectId,
	conversationId: routeConversationId,
}: ConversationViewProps) {
	const { t } = useTranslation("projects");
	const qc = useQueryClient();

	// Normally mirrors the `conversationId` prop, but a reply can silently
	// start a fresh conversation server-side (see onNew below) — tracking it
	// locally lets this view follow along without the caller (route param or
	// modal state) needing to know. Resyncs if the caller points us at a
	// genuinely different conversation (e.g. navigating to another permalink).
	const [conversationId, setConversationId] = useState(routeConversationId);
	useEffect(() => {
		setConversationId(routeConversationId);
	}, [routeConversationId]);

	const {
		data: conversation,
		isLoading: convLoading,
		isError,
	} = useQuery(
		projectId
			? conversationQueryOptions(projectId, conversationId)
			: globalConversationQueryOptions(conversationId),
	);
	const {
		events,
		isLoading: eventsLoading,
		hasOlder,
		isLoadingOlder,
		loadOlder,
		newBelow,
		following,
		setFollowing,
		jumpToLatest,
	} = useConversationEventWindow({
		projectId,
		conversationId,
		eventCount: conversation?.event_count,
		ready: !convLoading,
	});
	// Project scope: GET /projects/:id/agents/:agentId (project members may
	// always read their own project's agents). Global scope: the caller may
	// not have agents.read (admin-gated), so this uses the unrestricted
	// "browse global agents to chat with" list instead (same as
	// ai-chat-float-global.tsx) and finds the agent by id client-side.
	const { data: projectAgent } = useQuery({
		...agentQueryOptions(projectId ?? "", conversation?.agent_id ?? ""),
		enabled: !!projectId && !!conversation?.agent_id,
	});
	const { data: chattableAgents = [] } = useQuery({
		...chattableAgentsQueryOptions,
		enabled: !projectId && !!conversation?.agent_id,
	});
	const agent = projectId
		? projectAgent
		: chattableAgents.find((a) => a.id === conversation?.agent_id);
	const isACP = agent?.agent_type === "acp";

	const isRunning =
		conversation?.status === "queued" || conversation?.status === "running";
	const isTerminal =
		conversation?.status === "finished" ||
		conversation?.status === "failed" ||
		conversation?.status === "stopped";
	const isChatMessage = conversation?.trigger_type === "chat_message";
	// ACP conversations stay replyable for every trigger type (task_assigned,
	// comment_mention, etc.), not just chat_message ones — the user's local
	// bridge daemon keeps a conversation alive by conversation_id regardless
	// of why it started, and regardless of status (see
	// SendConversationMessage's ACP branch in services/api), so a reply can
	// always continue it. LLM conversations are unchanged: only chat_message
	// ones with a live session, and never once terminal (handled
	// transparently by onNew below via the returned conversation id).
	const canReply = isACP
		? !isChatMessage || !!conversation?.chat_session_id
		: isChatMessage && !!conversation?.chat_session_id && !isTerminal;

	const messages = useMemo(
		() => eventsToThreadMessages(events, isRunning),
		[events, isRunning],
	);

	const invalidate = (id: string = conversationId) => {
		if (projectId) {
			qc.invalidateQueries({
				queryKey: ["projects", projectId, "conversations", id],
			});
			qc.invalidateQueries({
				queryKey: ["projects", projectId, "conversations"],
			});
		} else {
			qc.invalidateQueries({
				queryKey: ["global-chat", "conversations", id],
			});
			qc.invalidateQueries({ queryKey: ["global-chat", "conversations"] });
		}
	};

	const onNew = async (message: AppendMessage) => {
		if (!conversation) {
			throw new Error(t("agents.conversationView.conversationEnded"));
		}
		const text = extractTextOnlyContent(message);
		if (text === null) {
			throw new Error(t("agents.conversationView.textOnlyMessage"));
		}

		if (!conversation.chat_session_id) {
			// ACP conversation of a non-chat trigger type (task_assigned,
			// comment_mention, etc.) — reply in place on the same
			// conversation_id rather than through a chat session.
			if (projectId) {
				await sendConversationMessage(projectId, conversation.id, text);
			} else {
				await sendGlobalConversationMessage(conversation.id, text);
			}
			invalidate();
			return;
		}

		const result = projectId
			? await sendChatMessage(
					projectId,
					conversation.agent_id,
					conversation.chat_session_id,
					{ message: text },
				)
			: await sendGlobalChatMessage(conversation.chat_session_id, {
					message: text,
				});
		// The previous conversation may have already ended (explicitly
		// stopped, or reaped after 3 minutes with no heartbeat) — replying
		// then silently starts a fresh conversation server-side. Follow it,
		// otherwise this view keeps polling the old (now terminal)
		// conversation and the reply appears to vanish.
		if (result.id !== conversationId) {
			qc.setQueryData(
				(projectId
					? conversationQueryOptions(projectId, result.id)
					: globalConversationQueryOptions(result.id)
				).queryKey,
				result,
			);
			setConversationId(result.id);
		}
		invalidate(result.id);
	};

	const onCancel = async () => {
		if (!conversation) return;
		if (projectId) {
			await pauseConversation(projectId, conversation.id);
		} else {
			await pauseGlobalConversation(conversation.id);
		}
		invalidate();
	};

	const runtime = useExternalStoreRuntime({
		messages,
		isRunning,
		convertMessage: (m) => m,
		onNew,
		onCancel,
		isDisabled: !canReply,
	});

	// Pings the ai-agent service every ~30s while this chat conversation is
	// loaded, so its sandbox's idle timer never trips as long as this view
	// stays open — mirrors the heartbeat in ai-chat-float.tsx. Only chat
	// conversations have a sandbox that pauses between turns; task/comment
	// triggered ones would just be a pointless no-op server-side. ACP
	// conversations have no cloud sandbox to keep alive either (the user's
	// local bridge daemon owns their lifecycle instead), so heartbeating one
	// would just be a wasted round trip.
	useEffect(() => {
		if (conversation?.trigger_type !== "chat_message" || isTerminal || isACP)
			return;
		const ping = () => {
			void (
				projectId
					? heartbeatConversation(projectId, conversationId)
					: heartbeatGlobalConversation(conversationId)
			).catch(() => {});
		};
		ping();
		const interval = setInterval(ping, CONVERSATION_HEARTBEAT_INTERVAL_MS);
		return () => clearInterval(interval);
	}, [
		conversation?.trigger_type,
		isTerminal,
		isACP,
		projectId,
		conversationId,
	]);

	if (convLoading || eventsLoading) {
		return (
			<div className="flex flex-col h-full gap-4 p-6">
				<Skeleton className="h-10 w-full rounded-xl" />
				<div className="space-y-4 flex-1">
					{Array.from({ length: 4 }).map((_, i) => (
						// biome-ignore lint/suspicious/noArrayIndexKey: skeleton
						<Skeleton key={i} className="h-16 w-3/4 rounded-2xl" />
					))}
				</div>
			</div>
		);
	}

	if (!conversation) {
		return (
			<div className="flex flex-col h-full items-center justify-center text-muted-foreground/50 gap-3">
				<Bot className="size-10" />
				<p className="text-sm">{t("agents.conversationView.notFound")}</p>
			</div>
		);
	}

	// Show the error fallback only when the conversation failed AND produced
	// no visible messages. When messages exist, render the Thread normally so
	// the user can trace what happened before the failure — the header's
	// status badge and the bottom error footer already convey the failure.
	// Skipped when canReply is true (an ACP conversation, which stays
	// replyable straight through a failure regardless of trigger type) so
	// the user can retry instead of hitting a dead end.
	if (
		isError ||
		(conversation.status === "failed" && messages.length === 0 && !canReply)
	) {
		return (
			<div className="flex flex-col h-full items-center justify-center gap-4 p-6">
				<div className="flex size-12 items-center justify-center rounded-full bg-destructive/10">
					<AlertTriangle className="size-6 text-destructive" />
				</div>
				<div className="text-center space-y-1">
					<p className="text-sm font-medium text-destructive">
						{t("agents.conversationView.failed")}
					</p>
					<p className="text-xs text-muted-foreground wrap-break-word">
						{conversation.error_message ??
							t("agents.conversationView.noOutput")}
					</p>
				</div>
			</div>
		);
	}

	const statusColor = CONVERSATION_STATUS_COLORS[conversation.status];
	const statusLabel = CONVERSATION_STATUS_LABELS[conversation.status];

	return (
		<div className="flex flex-col h-full min-h-0">
			{/* Header */}
			<div className="shrink-0 border-b border-border/40 px-5 py-3 flex items-center gap-3 bg-background/80 backdrop-blur-sm">
				<div className="flex items-center gap-2 min-w-0 flex-1">
					<Bot className="size-4 text-primary shrink-0" />
					<span className="text-sm font-medium truncate">
						{conversation.trigger_type === "chat_message"
							? t("agents.conversationView.chatSession")
							: t("agents.conversationView.taskSession")}
					</span>
					<Badge
						variant="outline"
						className={cn("text-xs font-semibold shrink-0", statusColor)}
					>
						{statusLabel}
					</Badge>
				</div>

				<div className="flex items-center gap-3 shrink-0">
					{conversation.branch_name && (
						<span className="flex items-center gap-1 text-xs text-muted-foreground">
							<GitBranch className="size-3" />
							{conversation.branch_name}
						</span>
					)}
					{conversation.pr_url && (
						<a
							href={conversation.pr_url}
							target="_blank"
							rel="noreferrer"
							className="flex items-center gap-1 text-xs text-muted-foreground hover:text-foreground transition-colors"
						>
							<GitPullRequest className="size-3" />
							{t("agents.conversationView.pr")}
						</a>
					)}
					<ConversationControls
						projectId={projectId}
						conversation={conversation}
						isACP={isACP}
					/>
				</div>
			</div>

			{/* Thread */}
			<div className="flex-1 min-h-0">
				<AssistantRuntimeProvider runtime={runtime}>
					<Thread
						turnAnchor="bottom"
						// A run starting must not pull a reader who is paging back
						// through history.
						scrollToBottomOnRunStart={false}
						viewportHeader={
							<LoadOlderEvents
								hasOlder={hasOlder}
								isLoadingOlder={isLoadingOlder}
								loadOlder={loadOlder}
							/>
						}
						viewportOverlay={
							<TailFollowIndicator
								newBelow={newBelow}
								following={following}
								setFollowing={setFollowing}
								jumpToLatest={jumpToLatest}
							/>
						}
					/>
				</AssistantRuntimeProvider>
			</div>

			{/* Footer */}
			{conversation.error_message && (
				<div className="shrink-0 border-t border-destructive/20 bg-destructive/5 px-5 py-3">
					<p className="text-xs text-destructive">
						{conversation.error_message}
					</p>
				</div>
			)}
		</div>
	);
}
