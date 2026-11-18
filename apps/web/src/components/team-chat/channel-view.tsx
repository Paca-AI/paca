import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { Archive, Hash, Lock, LogOut, Star } from "lucide-react";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { ConversationEvent } from "@/components/conversation/conversation-event";
import { Button } from "@/components/ui/button";
import {
	Dialog,
	DialogContent,
	DialogDescription,
	DialogFooter,
	DialogHeader,
	DialogTitle,
} from "@/components/ui/dialog";
import { Spinner } from "@/components/ui/spinner";
import { useChatPermissions } from "@/hooks/use-chat-permissions";
import { useChatRealtime } from "@/hooks/use-chat-realtime";
import { currentUserQueryOptions } from "@/lib/auth-api";
import {
	type AgentRuns,
	agentRunsQueryOptions,
	archiveChannel,
	type Channel,
	type ChatBlock,
	type ChatMessage,
	channelsQueryOptions,
	chatKeys,
	DM_SCOPE,
	deleteMessage,
	editMessage,
	joinChannel,
	leaveChannel,
	listMessages,
	markChannelRead,
	moderateDeleteMessage,
	postMessage,
	setChannelStarred,
	setMessageSaved,
	setReaction,
} from "@/lib/chat-api";
import {
	firstSeq,
	lastSeq,
	type MessageWindow,
	toggleReaction,
	withNewest,
	withOlder,
} from "@/lib/chat-cache";
import { chatErrorKey } from "@/lib/chat-errors";
import { AgentRunsPanel } from "./agent-runs-panel";
import { Composer } from "./composer";
import type { MessageActions } from "./message-item";
import { MessageList } from "./message-list";
import { PeerAvatar } from "./peer-avatar";
import { ThreadPane } from "./thread-pane";

const MARK_READ_DELAY_MS = 500;

export function ChannelView({
	projectId,
	channelId,
	thread,
}: {
	projectId: string;
	channelId: string;
	/** A thread to open on arrival: its root message id and seq (an Inbox row). */
	thread?: { id: string; seq?: number };
}) {
	const { t } = useTranslation("chat");
	const navigate = useNavigate();
	const queryClient = useQueryClient();
	const { can } = useChatPermissions(projectId);
	const isDM = projectId === DM_SCOPE;
	const me = useQuery(currentUserQueryOptions).data;
	const meId = me?.id;
	const channels = useQuery(channelsQueryOptions(projectId));
	const channel = channels.data?.find((c) => c.id === channelId);
	const isAgentDM = channel?.peer?.kind === "agent";
	const agent = useQuery(
		agentRunsQueryOptions(channelId, isDM && isAgentDM),
	).data;

	// Where the reader had read to when they arrived, to mark the new messages.
	const arrivedAt = useRef<{ id: string; seq: number } | null>(null);
	if (channel && arrivedAt.current?.id !== channelId) {
		arrivedAt.current = {
			id: channelId,
			seq: channel.is_member ? channel.last_read_seq : 0,
		};
	}
	const [threadRootId, setThreadRootId] = useState<string | null>(
		thread?.id ?? null,
	);
	const [confirmArchive, setConfirmArchive] = useState(false);
	const [actionError, setActionError] = useState<string | null>(null);

	// The newest page; live announcements and catch-ups merge into it.
	const key = chatKeys.messages(projectId, channelId);
	const win = useQuery({
		queryKey: key,
		queryFn: async (): Promise<MessageWindow> =>
			withNewest(
				undefined,
				await listMessages(projectId, channelId, { pageSize: 50 }),
				true,
			),
		staleTime: Number.POSITIVE_INFINITY,
		refetchOnWindowFocus: false,
	});
	const items = win.data?.items ?? [];

	useChatRealtime(projectId, channelId, meId);

	// Another channel starts without a thread open.
	// biome-ignore lint/correctness/useExhaustiveDependencies: reset on channel change only
	useEffect(() => setThreadRootId(thread?.id ?? null), [channelId, thread?.id]);

	// Reading is the act of looking at the newest message.
	const newest = lastSeq(win.data);
	useEffect(() => {
		if (!channel || newest === 0) return;
		if (channel.is_member && channel.last_read_seq >= newest) return;
		if (document.visibilityState !== "visible") return;
		const id = setTimeout(() => {
			void markChannelRead(projectId, channelId, newest)
				.then(() => {
					void queryClient.invalidateQueries({ queryKey: chatKeys.unread() });
					void queryClient.invalidateQueries({ queryKey: chatKeys.inboxAll() });
					void queryClient.invalidateQueries({
						queryKey: chatKeys.channels(projectId),
					});
				})
				.catch(() => undefined);
		}, MARK_READ_DELAY_MS);
		return () => clearTimeout(id);
	}, [channel, newest, projectId, channelId, queryClient]);

	const loadOlder = useMutation({
		mutationFn: async () =>
			listMessages(projectId, channelId, {
				beforeSeq: firstSeq(win.data),
				pageSize: 50,
			}),
		onSuccess: (page) =>
			queryClient.setQueryData<MessageWindow>(key, (cur) =>
				withOlder(cur, page),
			),
	});

	const mergeOne = useCallback(
		(m: ChatMessage) =>
			queryClient.setQueryData<MessageWindow>(key, (cur) =>
				withNewest(cur, { items: [m], has_more: false }, false),
			),
		[queryClient, key],
	);

	const send = useMutation({
		mutationFn: (content: ChatBlock[]) =>
			postMessage(projectId, channelId, content),
		onSuccess: mergeOne,
	});
	const react = useMutation({
		mutationFn: (v: { m: ChatMessage; emoji: string; on: boolean }) =>
			setReaction(projectId, channelId, v.m.id, v.emoji, v.on),
		onMutate: (v) => {
			queryClient.setQueryData<MessageWindow>(key, (cur) =>
				cur
					? {
							...cur,
							items: cur.items.map((m) =>
								m.id === v.m.id ? toggleReaction(m, v.emoji, v.on) : m,
							),
						}
					: cur,
			);
		},
		// The server's counts win, whether or not the toggle worked.
		onSettled: () => void queryClient.invalidateQueries({ queryKey: key }),
	});
	const edit = useMutation({
		mutationFn: (v: { m: ChatMessage; content: ChatBlock[] }) =>
			editMessage(projectId, channelId, v.m.id, v.content),
		onSuccess: mergeOne,
	});
	const remove = useMutation({
		mutationFn: (m: ChatMessage) =>
			m.author_id === meId
				? deleteMessage(projectId, channelId, m.id)
				: moderateDeleteMessage(projectId, channelId, m.id),
		onSuccess: () => void queryClient.invalidateQueries({ queryKey: key }),
		onError: (e) => setActionError(t(`errors.${chatErrorKey(e)}`)),
	});

	const save = useMutation({
		mutationFn: (v: { m: ChatMessage; saved: boolean }) =>
			setMessageSaved(projectId, channelId, v.m.id, v.saved),
		onMutate: (v) => {
			queryClient.setQueryData<MessageWindow>(key, (cur) =>
				cur
					? {
							...cur,
							items: cur.items.map((m) =>
								m.id === v.m.id ? { ...m, saved: v.saved } : m,
							),
						}
					: cur,
			);
		},
		onSettled: () => {
			void queryClient.invalidateQueries({ queryKey: key });
			void queryClient.invalidateQueries({ queryKey: chatKeys.refs("saved") });
		},
		onError: (e) => setActionError(t(`errors.${chatErrorKey(e)}`)),
	});

	const starMutation = useMutation({
		mutationFn: (on: boolean) => setChannelStarred(projectId, channelId, on),
		onSettled: () => {
			void queryClient.invalidateQueries({
				queryKey: chatKeys.channels(projectId),
			});
			void queryClient.invalidateQueries({ queryKey: chatKeys.inboxAll() });
		},
	});

	const membership = useMutation({
		mutationFn: (join: boolean) =>
			join
				? joinChannel(projectId, channelId)
				: leaveChannel(projectId, channelId),
		onSuccess: () =>
			void queryClient.invalidateQueries({
				queryKey: chatKeys.channels(projectId),
			}),
		onError: (e) => setActionError(t(`errors.${chatErrorKey(e)}`)),
	});
	const archive = useMutation({
		mutationFn: () => archiveChannel(projectId, channelId),
		onSuccess: () => {
			setConfirmArchive(false);
			void queryClient.invalidateQueries({
				queryKey: chatKeys.channels(projectId),
			});
			void navigate({ to: "/projects/$projectId/chat", params: { projectId } });
		},
		onError: (e) => setActionError(t(`errors.${chatErrorKey(e)}`)),
	});

	const canWrite = can("messages:write");
	// Nobody moderates or manages a direct message: it belongs to its two members.
	const canModerate = !isDM && can("messages:moderate");
	const canManage = !isDM && can("channels:write");
	const archived = channel?.archived_at != null;

	const actions: MessageActions = useMemo(
		() => ({
			react: (m, emoji, on) => react.mutate({ m, emoji, on }),
			edit: async (m, content) => {
				await edit.mutateAsync({ m, content });
			},
			remove: (m) => remove.mutate(m),
			openThread: (m) => setThreadRootId(m.id),
			save: (m, saved) => save.mutate({ m, saved }),
		}),
		[react, edit, remove],
	);

	// The root of an opened thread may be older than the page loaded: fetch it.
	const inWindow = items.find((m) => m.id === threadRootId);
	const rootFetch = useQuery({
		queryKey: ["chat", "thread-root", projectId, channelId, threadRootId],
		queryFn: async () => {
			const seq = thread?.id === threadRootId ? thread?.seq : undefined;
			if (seq === undefined) return null;
			const page = await listMessages(projectId, channelId, {
				afterSeq: seq - 1,
				pageSize: 1,
			});
			return page.items.find((m) => m.id === threadRootId) ?? null;
		},
		enabled: !!threadRootId && !inWindow && win.isSuccess,
		staleTime: 15_000,
	});
	const threadRoot = inWindow ?? rootFetch.data ?? undefined;

	if (channels.isSuccess && !channel) {
		return (
			<p className="p-8 text-center text-sm text-muted-foreground">
				{t("errors.channelNotFound")}
			</p>
		);
	}

	return (
		<div className="relative flex min-h-0 flex-1">
			<section className="flex min-h-0 min-w-0 flex-1 flex-col">
				<ChannelHeader
					channel={channel}
					onToggleStar={(on) => starMutation.mutate(on)}
					agent={agent}
					canManage={canManage}
					busy={membership.isPending}
					onToggleMembership={(join) => membership.mutate(join)}
					onArchive={() => setConfirmArchive(true)}
				/>
				{actionError ? (
					<p
						role="alert"
						className="border-b bg-destructive/10 px-4 py-2 text-sm text-destructive"
					>
						{actionError}
					</p>
				) : null}
				{win.isError ? (
					<div className="flex flex-1 flex-col items-center justify-center gap-2 text-sm">
						<p>{t("list.loadFailed")}</p>
						<Button
							size="sm"
							variant="outline"
							onClick={() => void win.refetch()}
						>
							{t("list.retry")}
						</Button>
					</div>
				) : win.isPending ? (
					<div className="flex flex-1 items-center justify-center">
						<Spinner />
					</div>
				) : (
					<MessageList
						items={items}
						hasOlder={win.data?.hasOlder ?? false}
						loadingOlder={loadOlder.isPending}
						onLoadOlder={() => loadOlder.mutate()}
						currentUserId={meId}
						canReact={canWrite && !archived}
						canModerate={canModerate}
						canReply={canWrite && !archived}
						actions={actions}
						peer={channel?.peer}
						readSeq={arrivedAt.current?.seq}
						footer={
							agent?.working && channel?.peer ? (
								<ConversationEvent
									className="px-4 py-1"
									avatar={
										<PeerAvatar
											name={channel.peer.name}
											kind={channel.peer.kind}
											size="default"
										/>
									}
									actor={channel.peer.name}
									text={t("agent.isWorking")}
								/>
							) : null
						}
					/>
				)}
				{agent && !agent.replies && channel?.peer ? (
					<p className="border-t bg-muted/40 px-4 py-2 text-xs text-muted-foreground">
						{t("agent.noReplies", { name: channel.peer.name })}
					</p>
				) : null}
				{agent?.replies &&
				!agent.working &&
				agent.runs[0]?.status === "failed" &&
				channel?.peer ? (
					<p className="border-t bg-destructive/10 px-4 py-2 text-xs text-destructive">
						{t("agent.failedHint", { name: channel.peer.name })}
					</p>
				) : null}
				<Composer
					mentionProjectId={isDM ? undefined : projectId}
					draftId={`channel:${channelId}`}
					placeholder={
						channel?.peer
							? t("dm.composerPlaceholder", { name: channel.peer.name })
							: t("composer.placeholder", {
									channel: channel?.name ?? "",
								})
					}
					disabledReason={
						archived
							? t("composer.archived")
							: canWrite
								? undefined
								: t("composer.readOnly")
					}
					onSend={async (content) => {
						await send.mutateAsync(content);
					}}
				/>
			</section>

			{threadRoot ? (
				<ThreadPane
					projectId={projectId}
					channelId={channelId}
					root={threadRoot}
					currentUserId={meId}
					canWrite={canWrite && !archived}
					canReact={canWrite && !archived}
					canModerate={canModerate}
					mentionProjectId={isDM ? undefined : projectId}
					onClose={() => setThreadRootId(null)}
					onRootChanged={() =>
						void queryClient.invalidateQueries({ queryKey: key })
					}
				/>
			) : null}

			<Dialog open={confirmArchive} onOpenChange={setConfirmArchive}>
				<DialogContent className="sm:max-w-sm">
					<DialogHeader>
						<DialogTitle>{t("header.archiveConfirmTitle")}</DialogTitle>
						<DialogDescription>
							{t("header.archiveConfirmBody")}
						</DialogDescription>
					</DialogHeader>
					<DialogFooter>
						<Button variant="ghost" onClick={() => setConfirmArchive(false)}>
							{t("create.cancel")}
						</Button>
						<Button
							variant="destructive"
							disabled={archive.isPending}
							onClick={() => archive.mutate()}
						>
							{t("header.archive")}
						</Button>
					</DialogFooter>
				</DialogContent>
			</Dialog>
		</div>
	);
}

function ChannelHeader({
	channel,
	onToggleStar,
	agent,
	canManage,
	busy,
	onToggleMembership,
	onArchive,
}: {
	channel: Channel | undefined;
	agent?: AgentRuns;
	onToggleStar: (starred: boolean) => void;
	canManage: boolean;
	busy: boolean;
	onToggleMembership: (join: boolean) => void;
	onArchive: () => void;
}) {
	const { t } = useTranslation("chat");
	// A direct message: who it is with, and what kind of account they are.
	if (channel?.peer) {
		return (
			<header className="flex items-center gap-3 border-b px-4 py-2">
				<PeerAvatar
					name={channel.peer.name}
					kind={channel.peer.kind}
					size="default"
				/>
				<div className="min-w-0 flex-1">
					<h1 className="truncate text-sm font-semibold">
						{channel.peer.name}
					</h1>
					<p className="truncate text-xs text-muted-foreground">
						{agent?.working
							? t("agent.working", { name: channel.peer.name })
							: t(`dm.kindLabel.${channel.peer.kind}`)}
					</p>
				</div>
				{agent?.replies ? (
					<AgentRunsPanel runs={agent.runs} agentName={channel.peer.name} />
				) : null}
				<Button
					size="icon-sm"
					variant="ghost"
					aria-label={channel.starred ? t("sidebar.unstar") : t("sidebar.star")}
					aria-pressed={channel.starred}
					onClick={() => onToggleStar(!channel.starred)}
				>
					<Star className={channel.starred ? "fill-current" : undefined} />
				</Button>
			</header>
		);
	}
	return (
		<header className="flex items-center gap-2 border-b px-4 py-2">
			{channel?.kind === "private" ? (
				<Lock
					className="size-4 text-muted-foreground"
					aria-label={t("channels.private")}
				/>
			) : (
				<Hash
					className="size-4 text-muted-foreground"
					aria-label={t("channels.public")}
				/>
			)}
			<div className="min-w-0 flex-1">
				<h1 className="truncate text-sm font-semibold">
					{channel?.name ?? ""}
				</h1>
				<p className="truncate text-xs text-muted-foreground">
					{channel?.topic || t("header.noTopic")}
				</p>
			</div>
			{channel ? (
				<Button
					size="icon-sm"
					variant="ghost"
					aria-label={channel.starred ? t("sidebar.unstar") : t("sidebar.star")}
					aria-pressed={channel.starred}
					onClick={() => onToggleStar(!channel.starred)}
				>
					<Star className={channel.starred ? "fill-current" : undefined} />
				</Button>
			) : null}
			{channel?.archived_at ? (
				<span className="rounded bg-muted px-2 py-0.5 text-xs">
					{t("header.archived")}
				</span>
			) : null}
			{channel && channel.kind !== "private" && !channel.archived_at ? (
				channel.is_member ? (
					<Button
						size="sm"
						variant="outline"
						disabled={busy}
						onClick={() => onToggleMembership(false)}
					>
						<LogOut />
						{t("header.leave")}
					</Button>
				) : (
					<Button
						size="sm"
						disabled={busy}
						onClick={() => onToggleMembership(true)}
					>
						{t("header.join")}
					</Button>
				)
			) : null}
			{channel &&
			canManage &&
			!channel.archived_at &&
			channel.name !== "general" ? (
				<Button
					size="icon-sm"
					variant="ghost"
					aria-label={t("header.archive")}
					onClick={onArchive}
				>
					<Archive />
				</Button>
			) : null}
		</header>
	);
}
