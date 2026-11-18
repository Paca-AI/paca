import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useNavigate } from "@tanstack/react-router";
import { CheckCheck, Hash, Inbox as InboxIcon, Lock } from "lucide-react";
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";
import { Spinner } from "@/components/ui/spinner";
import {
	chatKeys,
	type InboxItem,
	inboxQueryOptions,
	markAllChatRead,
	markChannelRead,
	messageRefsQueryOptions,
	scopeOf,
} from "@/lib/chat-api";
import { relativeTime } from "@/lib/relative-time";
import { cn } from "@/lib/utils";
import { MessageRefList } from "./message-ref-list";
import { PeerAvatar } from "./peer-avatar";

export type InboxTab = "all" | "mentions" | "threads" | "unread" | "saved";

export const INBOX_TABS: InboxTab[] = [
	"all",
	"mentions",
	"threads",
	"unread",
	"saved",
];

const ROW_LINK = "flex min-w-0 flex-1 flex-col gap-1";

/**
 * The cross-project tracker: every channel the person follows, in every
 * project they belong to, unread first. Opening a row goes to the channel,
 * which switches the sidebar to that channel's project.
 */
export function InboxPage({
	projectId,
	tab,
}: {
	projectId?: string;
	tab: InboxTab;
}) {
	const { t, i18n } = useTranslation("chat");
	const queryClient = useQueryClient();
	const navigate = useNavigate();
	const query = { unreadOnly: tab === "unread", projectId };
	const isRefTab = tab === "mentions" || tab === "threads" || tab === "saved";
	const inbox = useQuery({ ...inboxQueryOptions(query), enabled: !isRefTab });
	// Mentions and threads are fetched for the tab badges too.
	const mentions = useQuery(messageRefsQueryOptions("mentions"));
	const threads = useQuery(messageRefsQueryOptions("threads"));
	const saved = useQuery({
		...messageRefsQueryOptions("saved"),
		enabled: tab === "saved",
	});
	const refQuery = { mentions, threads, saved }[isRefTab ? tab : "mentions"];
	const refItems = (refQuery.data ?? []).filter(
		(r) => !projectId || r.project_id === projectId,
	);
	const badge = (tabKey: InboxTab): number =>
		tabKey === "mentions"
			? (mentions.data ?? []).filter((r) => r.unread).length
			: tabKey === "threads"
				? (threads.data ?? []).filter((r) => r.unread).length
				: 0;
	// Projects to filter by come from the unfiltered list, so choosing one does
	// not make the others disappear from the filter.
	const everything = useQuery(inboxQueryOptions({}));
	// Direct messages belong to no project, so they are not a filter choice.
	const projects = [
		...new Map(
			(everything.data ?? [])
				.filter((i) => i.project_id !== null)
				.map((i) => [i.project_id as string, i.project_name]),
		),
	];

	const refresh = () => {
		void queryClient.invalidateQueries({ queryKey: chatKeys.inboxAll() });
		void queryClient.invalidateQueries({ queryKey: chatKeys.unread() });
		void queryClient.invalidateQueries({ queryKey: ["chat"] });
	};
	const readOne = useMutation({
		mutationFn: (i: InboxItem) =>
			markChannelRead(scopeOf(i.project_id), i.channel_id, i.last_seq),
		onSuccess: refresh,
	});
	const readAll = useMutation({
		mutationFn: () => markAllChatRead(projectId),
		onSuccess: refresh,
	});

	const setSearch = (next: { project?: string; tab?: InboxTab }) =>
		void navigate({
			to: "/chat",
			search: { project: next.project ?? projectId, tab: next.tab ?? tab },
			replace: true,
		});

	const items = inbox.data ?? [];
	const anyUnread = (everything.data ?? []).some((i) => i.unread_count > 0);

	return (
		<div className="mx-auto flex h-full min-h-0 w-full max-w-3xl flex-1 flex-col">
			<header className="flex flex-wrap items-center gap-2 border-b px-4 py-3">
				<InboxIcon className="size-5 text-muted-foreground" />
				<div className="min-w-0 basis-40 flex-1">
					<h1 className="text-base font-semibold">{t("inbox.title")}</h1>
					<p className="text-xs text-muted-foreground">{t("inbox.subtitle")}</p>
				</div>
				<select
					aria-label={t("inbox.projectFilter")}
					className="h-8 rounded-lg border border-input bg-transparent px-2 text-sm"
					value={projectId ?? ""}
					onChange={(e) => setSearch({ project: e.target.value || undefined })}
				>
					<option value="">{t("inbox.allProjects")}</option>
					{projects.map(([id, name]) => (
						<option key={id} value={id}>
							{name}
						</option>
					))}
				</select>
				<Button
					size="sm"
					variant="outline"
					disabled={!anyUnread || readAll.isPending}
					onClick={() => readAll.mutate()}
				>
					<CheckCheck />
					{t("inbox.markAllRead")}
				</Button>
			</header>

			<div
				role="tablist"
				aria-label={t("inbox.title")}
				className="flex gap-1 border-b px-3"
			>
				{INBOX_TABS.map((k) => (
					<button
						key={k}
						type="button"
						role="tab"
						aria-selected={tab === k}
						onClick={() => setSearch({ tab: k })}
						className={cn(
							"-mb-px border-b-2 px-3 py-2 text-sm",
							tab === k
								? "border-foreground font-medium text-foreground"
								: "border-transparent text-muted-foreground hover:text-foreground",
						)}
					>
						{t(`inbox.tabs.${k}`)}
						{badge(k) > 0 ? (
							<span
								className="ml-1.5 rounded-full bg-primary px-1.5 text-xs font-medium text-primary-foreground"
								aria-label={t("inbox.tabUnread", { n: badge(k) })}
							>
								{badge(k) > 99 ? "99+" : badge(k)}
							</span>
						) : null}
					</button>
				))}
			</div>

			<div className="min-h-0 flex-1 overflow-y-auto">
				{isRefTab ? (
					refQuery.isPending ? (
						<div className="flex justify-center p-10">
							<Spinner />
						</div>
					) : refQuery.isError ? (
						<div className="flex flex-col items-center gap-2 p-10 text-sm">
							<p>{t("inbox.loadFailed")}</p>
							<Button
								size="sm"
								variant="outline"
								onClick={() => void refQuery.refetch()}
							>
								{t("list.retry")}
							</Button>
						</div>
					) : refItems.length === 0 ? (
						<div className="p-10 text-center">
							<p className="text-sm font-medium">
								{t(`inbox.empty.${tab}Title`)}
							</p>
							<p className="mt-1 text-sm text-muted-foreground">
								{t(`inbox.empty.${tab}Body`)}
							</p>
						</div>
					) : (
						<MessageRefList list={tab} items={refItems} />
					)
				) : inbox.isPending ? (
					<div className="flex justify-center p-10">
						<Spinner />
					</div>
				) : inbox.isError ? (
					<div className="flex flex-col items-center gap-2 p-10 text-sm">
						<p>{t("inbox.loadFailed")}</p>
						<Button
							size="sm"
							variant="outline"
							onClick={() => void inbox.refetch()}
						>
							{t("list.retry")}
						</Button>
					</div>
				) : items.length === 0 ? (
					<div className="p-10 text-center">
						<p className="text-sm font-medium">
							{tab === "unread"
								? t("inbox.caughtUpTitle")
								: t("inbox.emptyTitle")}
						</p>
						<p className="mt-1 text-sm text-muted-foreground">
							{tab === "unread"
								? t("inbox.caughtUpBody")
								: t("inbox.emptyBody")}
						</p>
					</div>
				) : (
					<ul>
						{items.map((i) => {
							const unread = i.unread_count > 0;
							return (
								<li key={i.channel_id} className="group border-b">
									<div className="flex items-start gap-3 px-4 py-3 hover:bg-muted/40">
										{i.project_id ? (
											<Link
												to="/projects/$projectId/chat/$channelId"
												params={{
													projectId: i.project_id,
													channelId: i.channel_id,
												}}
												className={ROW_LINK}
											>
												<span className="flex flex-wrap items-center gap-2">
													{i.peer ? (
														<PeerAvatar
															name={i.peer.name}
															kind={i.peer.kind}
															size="xs"
														/>
													) : (
														<span className="rounded-full bg-primary/10 px-2 py-0.5 text-xs text-primary">
															{i.project_name}
														</span>
													)}
													<span
														className={cn(
															"inline-flex items-center gap-1 text-sm",
															unread
																? "font-semibold"
																: "text-muted-foreground",
														)}
													>
														{i.peer ? null : i.kind === "private" ? (
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
														{i.peer ? i.peer.name : i.channel_name}
														{i.peer ? (
															<span className="rounded bg-muted px-1 text-[10px] font-normal text-muted-foreground">
																{t("dm.inboxChip")}
															</span>
														) : null}
													</span>
													{i.last_message_at ? (
														<span className="ml-auto text-xs text-muted-foreground">
															{relativeTime(i.last_message_at, i18n.language)}
														</span>
													) : null}
												</span>
												<span
													className={cn(
														"line-clamp-2 break-words text-sm",
														unread
															? "text-foreground"
															: "text-muted-foreground",
													)}
												>
													{i.preview ? (
														<>
															<span className="font-medium">
																{i.last_author}
															</span>
															{": "}
															{i.preview}
														</>
													) : (
														t("inbox.noMessages")
													)}
												</span>
											</Link>
										) : (
											<Link
												to="/chat/dm/$channelId"
												params={{ channelId: i.channel_id }}
												className={ROW_LINK}
											>
												<span className="flex flex-wrap items-center gap-2">
													{i.peer ? (
														<PeerAvatar
															name={i.peer.name}
															kind={i.peer.kind}
															size="xs"
														/>
													) : (
														<span className="rounded-full bg-primary/10 px-2 py-0.5 text-xs text-primary">
															{i.project_name}
														</span>
													)}
													<span
														className={cn(
															"inline-flex items-center gap-1 text-sm",
															unread
																? "font-semibold"
																: "text-muted-foreground",
														)}
													>
														{i.peer ? null : i.kind === "private" ? (
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
														{i.peer ? i.peer.name : i.channel_name}
														{i.peer ? (
															<span className="rounded bg-muted px-1 text-[10px] font-normal text-muted-foreground">
																{t("dm.inboxChip")}
															</span>
														) : null}
													</span>
													{i.last_message_at ? (
														<span className="ml-auto text-xs text-muted-foreground">
															{relativeTime(i.last_message_at, i18n.language)}
														</span>
													) : null}
												</span>
												<span
													className={cn(
														"line-clamp-2 break-words text-sm",
														unread
															? "text-foreground"
															: "text-muted-foreground",
													)}
												>
													{i.preview ? (
														<>
															<span className="font-medium">
																{i.last_author}
															</span>
															{": "}
															{i.preview}
														</>
													) : (
														t("inbox.noMessages")
													)}
												</span>
											</Link>
										)}
										<div className="flex shrink-0 flex-col items-end gap-1">
											{unread ? (
												<span
													role="status"
													aria-label={t("channels.unreadBadge")}
													className="min-w-5 rounded-full bg-primary px-1.5 text-center text-xs font-medium text-primary-foreground"
												>
													{i.unread_count > 99 ? "99+" : i.unread_count}
												</span>
											) : null}
											{unread ? (
												<Button
													size="xs"
													variant="ghost"
													className="opacity-0 focus-visible:opacity-100 group-hover:opacity-100"
													disabled={readOne.isPending}
													onClick={() => readOne.mutate(i)}
												>
													{t("inbox.markRead")}
												</Button>
											) : null}
										</div>
									</div>
								</li>
							);
						})}
					</ul>
				)}
			</div>
		</div>
	);
}
