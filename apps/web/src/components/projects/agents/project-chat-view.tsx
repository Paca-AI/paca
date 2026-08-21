import type { AppendMessage } from "@assistant-ui/react";
import {
	AssistantRuntimeProvider,
	useExternalStoreRuntime,
} from "@assistant-ui/react";
import {
	useInfiniteQuery,
	useMutation,
	useQuery,
	useQueryClient,
} from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { ArrowLeft, Bot, Loader2, ShieldAlert } from "lucide-react";
import { useCallback, useEffect, useMemo, useRef } from "react";
import { useTranslation } from "react-i18next";
import { Thread } from "@/components/assistant-ui/thread";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { useProjectChatPermissions } from "@/hooks/use-can-use-project-chats";
import {
	agentsQueryOptions,
	appendProjectChatTurn,
	PROJECT_CHAT_RECONCILE_INTERVAL_MS,
	type ProjectChatContextSourceRef,
	type ProjectChatTurnHistoryItem,
	projectChatContextSourcesQueryOptions,
	projectChatSessionQueryOptions,
	projectChatTurnsQueryOptions,
	replaceProjectChatContextSources,
	stopProjectChatTurn,
} from "@/lib/agent-api";
import { projectChatContextSourcesEqual } from "@/lib/project-chat-navigation";
import { cn } from "@/lib/utils";
import { extractTextOnlyContent } from "./conversation-to-thread-messages";
import { ProjectChatCommandMenu } from "./project-chat-command-menu";
import { ProjectChatContextPicker } from "./project-chat-context-picker";
import {
	canPublishProjectChatConclusion,
	isProjectChatTurnActive,
	projectChatTurnsToThreadMessages,
} from "./project-chat-to-thread-messages";
import { ProjectChatWritebackPrompt } from "./project-chat-writeback-prompt";

type PendingCommand = { fingerprint: string; key: string };

export function ProjectChatView({
	projectId,
	sessionId,
}: {
	projectId: string;
	sessionId: string;
}) {
	const { t } = useTranslation("projects");
	const qc = useQueryClient();
	const { canUseTaskContext, canPublishConclusion } =
		useProjectChatPermissions(projectId);
	const sessionQuery = useQuery(
		projectChatSessionQueryOptions(projectId, sessionId),
	);
	const turnsQuery = useInfiniteQuery(
		projectChatTurnsQueryOptions(projectId, sessionId),
	);
	const sourcesQuery = useQuery(
		projectChatContextSourcesQueryOptions(projectId, sessionId),
	);
	const { data: agents = [] } = useQuery(agentsQueryOptions(projectId));
	const pendingRef = useRef<PendingCommand | null>(null);

	const turnItems = useMemo(() => {
		const byId = new Map<string, ProjectChatTurnHistoryItem>();
		for (const item of turnsQuery.data?.pages.flatMap((page) => page.items) ??
			[]) {
			byId.set(item.turn.id, item);
		}
		return [...byId.values()].sort(
			(a, b) => a.turn.turn_index - b.turn.turn_index,
		);
	}, [turnsQuery.data]);
	const latest = turnItems.at(-1);
	const latestPublishable =
		latest && canPublishProjectChatConclusion(latest) ? latest : null;
	const isRunning = isProjectChatTurnActive(latest);
	const session = sessionQuery.data;
	const agent = agents.find((value) => value.id === session?.agent_id);
	const isACP = agent?.agent_type === "acp" || latest?.run.backend === "acp";
	const selectedSources = useMemo(
		() =>
			sourcesQuery.data?.map((source) => ({
				type: source.source_type,
				id: source.source_id,
			})) ?? [],
		[sourcesQuery.data],
	);
	const relatedTaskIds = useMemo(
		() => [
			...new Set(
				selectedSources
					.filter((source) => source.type === "task")
					.map((source) => source.id),
			),
		],
		[selectedSources],
	);

	const messages = useMemo(
		() =>
			projectChatTurnsToThreadMessages(turnItems, {
				terminalFallback: (item) =>
					item.result?.error_message?.trim() ||
					t(`chats.result.${item.result?.terminal_status ?? item.turn.status}`),
			}),
		[turnItems, t],
	);

	useEffect(() => {
		if (!isRunning) return;
		const timer = window.setInterval(() => {
			void turnsQuery.refetch();
		}, PROJECT_CHAT_RECONCILE_INTERVAL_MS);
		return () => window.clearInterval(timer);
	}, [isRunning, turnsQuery.refetch]);

	const replaceSources = useMutation({
		mutationFn: (sources: ProjectChatContextSourceRef[]) =>
			replaceProjectChatContextSources(projectId, sessionId, sources),
		onSuccess: (sources, requestedSources) => {
			const changed = !projectChatContextSourcesEqual(
				selectedSources,
				requestedSources,
			);
			qc.setQueryData(
				projectChatContextSourcesQueryOptions(projectId, sessionId).queryKey,
				sources,
			);
			if (changed) pendingRef.current = null;
		},
	});

	const appendText = useCallback(
		async (text: string) => {
			if (isRunning) throw new Error(t("chats.errors.turnActive"));
			if (isACP) throw new Error(t("chats.acpUnavailable"));
			const fingerprint = JSON.stringify({ sessionId, message: text });
			if (pendingRef.current?.fingerprint !== fingerprint) {
				pendingRef.current = { fingerprint, key: crypto.randomUUID() };
			}
			await appendProjectChatTurn(
				projectId,
				sessionId,
				{ message: text },
				pendingRef.current.key,
			);
			pendingRef.current = null;
			await Promise.all([
				qc.invalidateQueries({
					queryKey: ["projects", projectId, "chat-sessions"],
				}),
				turnsQuery.refetch(),
			]);
		},
		[isACP, isRunning, projectId, qc, sessionId, t, turnsQuery.refetch],
	);

	const onNew = async (message: AppendMessage) => {
		const text = extractTextOnlyContent(message);
		if (text === null) {
			throw new Error(t("agents.conversationView.textOnlyMessage"));
		}
		await appendText(text);
	};

	const stopMutation = useMutation({
		mutationFn: async () => {
			if (!latest) throw new Error("missing turn");
			return stopProjectChatTurn(projectId, sessionId, latest.turn.id);
		},
		onSuccess: () => {
			void turnsQuery.refetch();
			void qc.invalidateQueries({
				queryKey: ["projects", projectId, "chat-sessions"],
			});
		},
	});

	const runtime = useExternalStoreRuntime({
		messages,
		isRunning,
		convertMessage: (message) => message,
		onNew,
		onCancel: async () => {
			await stopMutation.mutateAsync();
		},
		isDisabled: !!isACP,
		isSendDisabled: isRunning || replaceSources.isPending,
	});
	const threadComponents = useMemo(() => {
		const ChatComposerStart = () => (
			<div className="flex items-center gap-1">
				<ProjectChatContextPicker
					projectId={projectId}
					excludeSessionId={sessionId}
					value={selectedSources}
					canSelectTasks={canUseTaskContext}
					iconOnly
					onChange={(sources) => replaceSources.mutate(sources)}
					disabled={isRunning || replaceSources.isPending || isACP}
				/>
				{canPublishConclusion && (
					<ProjectChatCommandMenu
						hasTaskContext={relatedTaskIds.length > 0}
						disabled={isRunning || replaceSources.isPending || isACP}
					/>
				)}
			</div>
		);
		const ChatComposerPrompt = () => (
			<ProjectChatWritebackPrompt
				projectId={projectId}
				sessionId={sessionId}
				sourceItem={latestPublishable}
				relatedTaskIds={relatedTaskIds}
				onContinue={appendText}
			/>
		);
		return {
			ComposerStart: ChatComposerStart,
			...(canPublishConclusion ? { ComposerPrompt: ChatComposerPrompt } : {}),
		};
	}, [
		appendText,
		canPublishConclusion,
		canUseTaskContext,
		isACP,
		isRunning,
		latestPublishable,
		projectId,
		relatedTaskIds,
		replaceSources,
		selectedSources,
		sessionId,
	]);

	if (sessionQuery.isLoading || turnsQuery.isLoading) {
		return (
			<div className="flex h-full items-center justify-center">
				<Loader2 className="size-5 animate-spin text-muted-foreground" />
			</div>
		);
	}

	if (!session) {
		return (
			<div className="flex h-full flex-col items-center justify-center gap-3 text-muted-foreground">
				<Bot className="size-9 opacity-40" />
				<p>{t("chats.notFound")}</p>
			</div>
		);
	}

	return (
		<div className="flex h-full min-h-0 flex-col">
			<header className="shrink-0 border-b border-border/40 bg-background/80 px-5 py-3 backdrop-blur-sm">
				<div className="flex min-w-0 items-center gap-3">
					<Button
						variant="ghost"
						size="icon-sm"
						className="md:hidden"
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
					</Button>
					<div className="flex min-w-0 flex-1 items-center gap-2">
						<Bot className="size-4 shrink-0 text-primary" />
						<span className="truncate text-sm font-medium">
							{t("agents.conversationView.chatSession")}
						</span>
						{latest && (
							<Badge
								variant="outline"
								className={cn(
									"shrink-0 text-xs font-semibold",
									latest.turn.status === "running" ||
										latest.turn.status === "queued"
										? "border-blue-500/30 text-blue-600 dark:text-blue-300"
										: latest.turn.status === "succeeded"
											? "border-emerald-500/30 text-emerald-600 dark:text-emerald-300"
											: latest.turn.status === "failed"
												? "border-destructive/30 text-destructive"
												: "text-muted-foreground",
								)}
							>
								{t(`chats.status.${latest.turn.status}`)}
							</Badge>
						)}
					</div>
				</div>
				{isACP && (
					<div className="mt-2 flex items-start gap-2 rounded-lg bg-amber-500/8 px-3 py-2 text-xs text-amber-700 dark:text-amber-300">
						<ShieldAlert className="mt-0.5 size-3.5 shrink-0" />
						<span>{t("chats.acpUnavailable")}</span>
					</div>
				)}
			</header>

			<div className="min-h-0 flex-1">
				<AssistantRuntimeProvider runtime={runtime}>
					<Thread
						components={threadComponents}
						viewportHeader={
							turnsQuery.hasNextPage ? (
								<div className="mb-4 flex justify-center">
									<Button
										variant="ghost"
										size="sm"
										onClick={() => void turnsQuery.fetchNextPage()}
										disabled={turnsQuery.isFetchingNextPage}
									>
										{t("chats.loadEarlier")}
									</Button>
								</div>
							) : undefined
						}
					/>
				</AssistantRuntimeProvider>
			</div>
		</div>
	);
}
