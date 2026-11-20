import { Link } from "@tanstack/react-router";
import { Bookmark, Hash, Lock, MessagesSquare } from "lucide-react";
import { useTranslation } from "react-i18next";
import type { MessageRef, MessageRefList as RefListKind } from "@/lib/chat-api";
import { relativeTime } from "@/lib/relative-time";
import { cn } from "@/lib/utils";
import { PeerAvatar } from "./peer-avatar";

const ROW_LINK = "flex min-w-0 flex-1 flex-col gap-1";

/** The thread an entry opens: a thread row is its own root; a reply opens the
 * thread it is in; anything else opens just the channel. */
export function threadTarget(
	ref: MessageRef,
): { thread: string; seq: number | undefined } | undefined {
	if (ref.thread_root_id) {
		return { thread: ref.thread_root_id, seq: ref.root_seq ?? undefined };
	}
	return undefined;
}

/**
 * The rows of the Mentions, Threads and Saved tabs: each one message with where
 * it lives, and a click opens it in context (a thread opens its pane).
 */
export function MessageRefList({
	list,
	items,
}: {
	list: RefListKind;
	items: MessageRef[];
}) {
	const { t, i18n } = useTranslation("chat");
	return (
		<ul>
			{items.map((r) => {
				const target =
					list === "threads"
						? { thread: r.message_id, seq: r.root_seq ?? undefined }
						: threadTarget(r);
				const search = target ? { thread: target.thread, seq: target.seq } : {};
				const unread = r.unread;
				const body = (
					<>
						<span className="flex flex-wrap items-center gap-2">
							{r.peer ? (
								<PeerAvatar name={r.peer.name} kind={r.peer.kind} size="xs" />
							) : (
								<span className="rounded-full bg-primary/10 px-2 py-0.5 text-xs text-primary">
									{r.project_name}
								</span>
							)}
							<span
								className={cn(
									"inline-flex items-center gap-1 text-sm",
									unread ? "font-semibold" : "text-muted-foreground",
								)}
							>
								{r.peer ? null : r.kind === "private" ? (
									<Lock
										className="size-3.5"
										aria-label={t("channels.private")}
									/>
								) : (
									<Hash
										className="size-3.5"
										aria-label={t("channels.public")}
									/>
								)}
								{r.peer ? r.peer.name : r.channel_name}
							</span>
							{list === "threads" ? (
								<span className="inline-flex items-center gap-1 text-xs text-muted-foreground">
									<MessagesSquare className="size-3.5" />
									{t("inbox.replies", { n: r.reply_count })}
								</span>
							) : null}
							{list === "saved" ? (
								<Bookmark
									className="size-3.5 fill-current text-muted-foreground"
									aria-label={t("inbox.tabs.saved")}
								/>
							) : null}
							<span className="ml-auto text-xs text-muted-foreground">
								{relativeTime(r.activity_at, i18n.language)}
							</span>
						</span>
						{list === "threads" && r.root_preview ? (
							<span className="line-clamp-1 break-words text-xs text-muted-foreground">
								{r.root_preview}
							</span>
						) : null}
						<span
							className={cn(
								"line-clamp-2 break-words text-sm",
								unread ? "text-foreground" : "text-muted-foreground",
							)}
						>
							<span className="font-medium">{r.author_name}</span>
							{": "}
							{r.preview}
						</span>
					</>
				);
				return (
					<li key={`${r.message_id}`} className="border-b">
						<div className="flex items-start gap-3 px-4 py-3 hover:bg-muted/40">
							{r.project_id ? (
								<Link
									to="/projects/$projectId/chat/$channelId"
									params={{ projectId: r.project_id, channelId: r.channel_id }}
									search={search}
									className={ROW_LINK}
								>
									{body}
								</Link>
							) : (
								<Link
									to="/chat/dm/$channelId"
									params={{ channelId: r.channel_id }}
									search={search}
									className={ROW_LINK}
								>
									{body}
								</Link>
							)}
							{list === "threads" && r.unread_count > 0 ? (
								<span
									role="status"
									aria-label={t("inbox.unreadReplies", {
										count: r.unread_count,
									})}
									className="min-w-5 shrink-0 rounded-full bg-primary px-1.5 text-center text-xs font-medium text-primary-foreground"
								>
									{r.unread_count > 99 ? "99+" : r.unread_count}
								</span>
							) : list === "mentions" && unread ? (
								<span
									role="status"
									aria-label={t("inbox.unreadMention")}
									className="mt-1.5 size-2.5 shrink-0 rounded-full bg-destructive"
								/>
							) : null}
						</div>
					</li>
				);
			})}
		</ul>
	);
}
