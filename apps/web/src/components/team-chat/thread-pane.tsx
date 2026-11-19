import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Bell, BellOff, X } from "lucide-react";
import { useEffect } from "react";
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";
import {
	type ChatBlock,
	type ChatMessage,
	chatKeys,
	deleteMessage,
	editMessage,
	listMessages,
	markThreadRead,
	moderateDeleteMessage,
	postMessage,
	setMessageSaved,
	setReaction,
	setThreadFollowed,
} from "@/lib/chat-api";
import {
	firstSeq,
	lastSeq,
	type MessageWindow,
	toggleReaction,
	withNewest,
	withOlder,
} from "@/lib/chat-cache";
import { Composer } from "./composer";
import { type MessageActions, MessageItem } from "./message-item";
import { MessageList } from "./message-list";

const REPLY_PAGE = 100;

/** The replies to one message, with their own composer. */
export function ThreadPane({
	projectId,
	channelId,
	root,
	currentUserId,
	canWrite,
	canReact,
	canModerate,
	onClose,
	onRootChanged,
	mentionProjectId,
}: {
	projectId: string;
	channelId: string;
	root: ChatMessage;
	currentUserId: string | undefined;
	canWrite: boolean;
	canReact: boolean;
	canModerate: boolean;
	onClose: () => void;
	/** The reply count of the root changed. */
	onRootChanged: () => void;
	mentionProjectId?: string;
}) {
	const { t } = useTranslation("chat");
	const queryClient = useQueryClient();
	const key = chatKeys.thread(projectId, channelId, root.id);

	// The replies live in the query cache as one window, so a refetch of the
	// newest page and the older pages the user loaded stay together.
	const replies = useQuery({
		queryKey: key,
		queryFn: async (): Promise<MessageWindow> => {
			const page = await listMessages(projectId, channelId, {
				threadRootId: root.id,
				pageSize: REPLY_PAGE,
			});
			// Merge into what is loaded unless a gap opened since; then start over.
			const cur = queryClient.getQueryData<MessageWindow>(key);
			const first = page.items[0]?.seq ?? 0;
			const contiguous = !!cur && (first === 0 || first <= lastSeq(cur) + 1);
			return withNewest(cur, page, !contiguous);
		},
	});
	const allReplies = replies.data?.items ?? [];
	const loadOlder = useMutation({
		mutationFn: () =>
			listMessages(projectId, channelId, {
				threadRootId: root.id,
				beforeSeq: firstSeq(replies.data),
				pageSize: REPLY_PAGE,
			}),
		onSuccess: (page) =>
			queryClient.setQueryData<MessageWindow>(key, (cur) =>
				withOlder(cur, page),
			),
	});

	const refresh = () => {
		void queryClient.invalidateQueries({ queryKey: key });
		onRootChanged();
	};

	const send = useMutation({
		mutationFn: (content: ChatBlock[]) =>
			postMessage(projectId, channelId, content, root.id),
		onSuccess: refresh,
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
		onSettled: () => void queryClient.invalidateQueries({ queryKey: key }),
	});
	const edit = useMutation({
		mutationFn: (v: { m: ChatMessage; content: ChatBlock[] }) =>
			editMessage(projectId, channelId, v.m.id, v.content),
		onSuccess: refresh,
	});
	const remove = useMutation({
		mutationFn: (m: ChatMessage) =>
			m.author_id === currentUserId
				? deleteMessage(projectId, channelId, m.id)
				: moderateDeleteMessage(projectId, channelId, m.id),
		onSuccess: refresh,
	});

	const follow = useMutation({
		mutationFn: (on: boolean) =>
			setThreadFollowed(projectId, channelId, root.id, on),
		onSuccess: onRootChanged,
	});
	const save = useMutation({
		mutationFn: (v: { m: ChatMessage; saved: boolean }) =>
			setMessageSaved(projectId, channelId, v.m.id, v.saved),
		onSuccess: () => {
			void queryClient.invalidateQueries({ queryKey: key });
			onRootChanged();
			void queryClient.invalidateQueries({ queryKey: chatKeys.refs("saved") });
		},
	});

	// A followed thread on screen is a read thread.
	const newest = lastSeq(replies.data);
	useEffect(() => {
		if (!root.following || newest === 0) return;
		const id = setTimeout(() => {
			void markThreadRead(projectId, channelId, root.id, newest)
				.then(() =>
					queryClient.invalidateQueries({ queryKey: chatKeys.inboxAll() }),
				)
				.catch(() => undefined);
		}, 500);
		return () => clearTimeout(id);
	}, [root.following, root.id, newest, projectId, channelId, queryClient]);

	const actions: MessageActions = {
		save: (m, saved) => save.mutate({ m, saved }),
		react: (m, emoji, on) => react.mutate({ m, emoji, on }),
		edit: async (m, content) => {
			await edit.mutateAsync({ m, content });
		},
		remove: (m) => remove.mutate(m),
	};

	return (
		<aside
			className="absolute inset-0 z-10 flex min-h-0 flex-col bg-background md:static md:z-auto md:w-96 md:border-l"
			aria-label={t("thread.title")}
		>
			<div className="flex items-center justify-between border-b px-4 py-2">
				<h2 className="min-w-0 flex-1 text-sm font-semibold">
					{t("thread.title")}
				</h2>
				<Button
					size="sm"
					variant="ghost"
					aria-pressed={root.following}
					disabled={follow.isPending}
					onClick={() => follow.mutate(!root.following)}
				>
					{root.following ? <BellOff /> : <Bell />}
					{root.following ? t("thread.unfollow") : t("thread.follow")}
				</Button>
				<Button
					size="icon-sm"
					variant="ghost"
					aria-label={t("thread.close")}
					onClick={onClose}
				>
					<X />
				</Button>
			</div>
			<div className="border-b py-2">
				<MessageItem
					message={root}
					currentUserId={currentUserId}
					canReact={false}
					canModerate={false}
					canReply={false}
					actions={{ ...actions, openThread: undefined }}
				/>
			</div>
			<MessageList
				items={allReplies}
				hasOlder={replies.data?.hasOlder ?? false}
				loadingOlder={loadOlder.isPending}
				onLoadOlder={() => loadOlder.mutate()}
				currentUserId={currentUserId}
				canReact={canReact}
				canModerate={canModerate}
				canReply={canWrite}
				actions={actions}
				emptyLabel={t("thread.empty")}
			/>
			<Composer
				draftId={`thread:${root.id}`}
				placeholder={t("composer.replyPlaceholder")}
				disabledReason={canWrite ? undefined : t("composer.readOnly")}
				mentionProjectId={mentionProjectId}
				onSend={async (content) => {
					await send.mutateAsync(content);
				}}
				autoFocus
			/>
		</aside>
	);
}
