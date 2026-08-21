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
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
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
	projectChatTurnEventsQueryOptions,
	projectChatTurnQueryOptions,
	projectChatTurnsQueryOptions,
	replaceProjectChatContextSources,
	stopProjectChatTurn,
} from "@/lib/agent-api";
import { projectChatContextSourcesEqual } from "@/lib/project-chat-navigation";
import { cn } from "@/lib/utils";
import {
	extractTextOnlyContent,
	isEnvironmentReady,
} from "./conversation-to-thread-messages";
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
	focusTurnId,
}: {
	projectId: string;
	sessionId: string;
	focusTurnId?: string;
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
	const submissionInFlightRef = useRef(false);
	const focusedTurnRef = useRef<string | undefined>(undefined);
	const [isSubmitting, setIsSubmitting] = useState(false);
	const [acceptedTurnId, setAcceptedTurnId] = useState<string>();
	const [sendError, setSendError] = useState<string>();

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
	const serverIsRunning = isProjectChatTurnActive(latest);
	const isRunning = serverIsRunning || !!acceptedTurnId;
	const latestPublishable =
		latest && canPublishProjectChatConclusion(latest) ? latest : null;
	const publishableTurnQuery = useQuery({
		...projectChatTurnQueryOptions(
			projectId,
			sessionId,
			latestPublishable?.turn.id ?? "",
		),
		enabled: latestPublishable !== null,
	});
	const turnEventsQuery = useQuery({
		...projectChatTurnEventsQueryOptions(projectId, latest?.turn.id ?? ""),
		enabled: !!latest && isRunning,
		refetchInterval: isRunning ? PROJECT_CHAT_RECONCILE_INTERVAL_MS : false,
	});
	const session = sessionQuery.data;
	const agent = agents.find((value) => value.id === session?.agent_id);
	const isACP = agent?.agent_type === "acp" || latest?.run.backend === "acp";
	const environmentReady = isEnvironmentReady(
		isACP,
		turnEventsQuery.data ?? [],
	);
	const selectedSources = useMemo(
		() =>
			sourcesQuery.data?.map((source) => ({
				type: source.source_type,
				id: source.source_id,
			})) ?? [],
		[sourcesQuery.data],
	);
	const selectedTaskIds = useMemo(
		() => [
			...new Set(
				selectedSources
					.filter((source) => source.type === "task")
					.map((source) => source.id),
			),
		],
		[selectedSources],
	);
	const publishableTaskIds = useMemo(
		() => [
			...new Set(
				publishableTurnQuery.data?.context_snapshot.items
					.filter((item) => item.source_type === "task")
					.map((item) => item.source_id) ?? [],
			),
		],
		[publishableTurnQuery.data],
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
		if (
			acceptedTurnId &&
			turnItems.some((item) => item.turn.id === acceptedTurnId)
		) {
			setAcceptedTurnId(undefined);
		}
	}, [acceptedTurnId, turnItems]);

	useEffect(() => {
		if (!isRunning) return;
		const timer = window.setInterval(() => {
			void turnsQuery.refetch();
		}, PROJECT_CHAT_RECONCILE_INTERVAL_MS);
		return () => window.clearInterval(timer);
	}, [isRunning, turnsQuery.refetch]);

	useEffect(() => {
		if (
			!focusTurnId ||
			turnItems.some((item) => item.turn.id === focusTurnId) ||
			!turnsQuery.hasNextPage ||
			turnsQuery.isFetchingNextPage
		)
			return;
		void turnsQuery.fetchNextPage();
	}, [
		focusTurnId,
		turnItems,
		turnsQuery.fetchNextPage,
		turnsQuery.hasNextPage,
		turnsQuery.isFetchingNextPage,
	]);

	useEffect(() => {
		if (!focusTurnId) {
			focusedTurnRef.current = undefined;
			return;
		}
		if (!focusTurnId || !turnItems.some((item) => item.turn.id === focusTurnId))
			return;
		if (focusedTurnRef.current === focusTurnId) return;
		const frame = requestAnimationFrame(() => {
			const assistant = document.querySelector<HTMLElement>(
				`[data-message-id="${focusTurnId}:assistant"]`,
			);
			const user = document.querySelector<HTMLElement>(
				`[data-message-id="${focusTurnId}:user"]`,
			);
			const target = assistant ?? user;
			if (!target) return;
			target.scrollIntoView({ block: "center" });
			focusedTurnRef.current = focusTurnId;
		});
		return () => cancelAnimationFrame(frame);
	}, [focusTurnId, turnItems]);

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
			if (submissionInFlightRef.current) {
				throw new Error(t("chats.errors.turnActive"));
			}
			if (isACP) throw new Error(t("chats.acpUnavailable"));
			const fingerprint = JSON.stringify({ sessionId, message: text });
			if (pendingRef.current?.fingerprint !== fingerprint) {
				pendingRef.current = { fingerprint, key: crypto.randomUUID() };
			}
			submissionInFlightRef.current = true;
			setIsSubmitting(true);
			try {
				const accepted = await appendProjectChatTurn(
					projectId,
					sessionId,
					{ message: text },
					pendingRef.current.key,
				);
				if (
					accepted.bundle.turn.status === "queued" ||
					accepted.bundle.turn.status === "running"
				) {
					setAcceptedTurnId(accepted.bundle.turn.id);
				}
				pendingRef.current = null;
				await Promise.all([
					qc.invalidateQueries({
						queryKey: ["projects", projectId, "chat-sessions"],
					}),
					turnsQuery.refetch(),
				]);
			} finally {
				submissionInFlightRef.current = false;
				setIsSubmitting(false);
			}
		},
		[isACP, isRunning, projectId, qc, sessionId, t, turnsQuery.refetch],
	);

	const onNew = async (message: AppendMessage) => {
		const text = extractTextOnlyContent(message);
		if (text === null) {
			throw new Error(t("agents.conversationView.textOnlyMessage"));
		}
		setSendError(undefined);
		try {
			await appendText(text);
		} catch {
			if (runtime.thread.composer.getState().text.length === 0) {
				runtime.thread.composer.setText(text);
			}
			setSendError(t("chats.errors.sendFailed"));
		}
	};

	const stopMutation = useMutation({
		mutationFn: async () => {
			const turnId = acceptedTurnId ?? latest?.turn.id;
			if (!turnId) throw new Error("missing turn");
			return stopProjectChatTurn(projectId, sessionId, turnId);
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
		isSendDisabled: isRunning || isSubmitting || replaceSources.isPending,
	});
	const renderStateRef = useRef({
		appendText,
		canPublishConclusion,
		canUseTaskContext,
		isACP,
		isRunning,
		isSubmitting,
		latestPublishable,
		projectId,
		publishableTaskIds,
		publishableTurn: publishableTurnQuery.data,
		replaceSources,
		selectedSources,
		selectedTaskIds,
		sendError,
		sessionId,
	});
	renderStateRef.current = {
		appendText,
		canPublishConclusion,
		canUseTaskContext,
		isACP,
		isRunning,
		isSubmitting,
		latestPublishable,
		projectId,
		publishableTaskIds,
		publishableTurn: publishableTurnQuery.data,
		replaceSources,
		selectedSources,
		selectedTaskIds,
		sendError,
		sessionId,
	};
	const ChatComposerStart = useCallback(() => {
		const state = renderStateRef.current;
		return (
			<div className="flex items-center gap-1">
				<ProjectChatContextPicker
					projectId={state.projectId}
					excludeSessionId={state.sessionId}
					value={state.selectedSources}
					canSelectTasks={state.canUseTaskContext}
					iconOnly
					onChange={(sources) => state.replaceSources.mutate(sources)}
					disabled={
						state.isRunning ||
						state.isSubmitting ||
						state.replaceSources.isPending ||
						state.isACP
					}
				/>
				{state.canPublishConclusion && (
					<ProjectChatCommandMenu
						hasTaskContext={state.selectedTaskIds.length > 0}
						disabled={
							state.isRunning ||
							state.isSubmitting ||
							state.replaceSources.isPending ||
							state.isACP
						}
					/>
				)}
			</div>
		);
	}, []);
	const ChatComposerPrompt = useCallback(() => {
		const state = renderStateRef.current;
		return (
			<>
				{state.sendError && (
					<div
						role="alert"
						className="mb-2 rounded-lg border border-destructive/30 bg-destructive/5 px-3 py-2 text-sm text-destructive"
					>
						{state.sendError}
					</div>
				)}
				{state.canPublishConclusion && (
					<ProjectChatWritebackPrompt
						projectId={state.projectId}
						sessionId={state.sessionId}
						sourceItem={state.publishableTurn ? state.latestPublishable : null}
						relatedTaskIds={state.publishableTaskIds}
						onContinue={state.appendText}
					/>
				)}
			</>
		);
	}, []);
	const threadComponents = useMemo(
		() => ({
			ComposerStart: ChatComposerStart,
			ComposerPrompt: ChatComposerPrompt,
		}),
		[ChatComposerPrompt, ChatComposerStart],
	);

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
						environmentReady={environmentReady}
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
