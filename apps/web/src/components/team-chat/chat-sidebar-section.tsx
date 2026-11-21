import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useRouterState } from "@tanstack/react-router";
import {
	ChevronDown,
	ChevronRight,
	Compass,
	Hash,
	Inbox,
	Lock,
	Plus,
	Star,
	Volume2,
} from "lucide-react";
import { type ReactElement, type ReactNode, useState } from "react";
import { useTranslation } from "react-i18next";
import {
	SidebarGroup,
	SidebarGroupContent,
	SidebarGroupLabel,
	SidebarMenu,
	SidebarMenuAction,
	SidebarMenuBadge,
	SidebarMenuButton,
	SidebarMenuItem,
	useSidebar,
} from "@/components/ui/sidebar";
import { useChatPermissions } from "@/hooks/use-chat-permissions";
import { useChatSidebarState } from "@/hooks/use-chat-sidebar-state";
import { useProjectPermissions } from "@/hooks/use-project-permissions";
import {
	callConfigQueryOptions,
	projectCallsQueryOptions,
} from "@/lib/call-api";
import {
	chatKeys,
	DM_SCOPE,
	type InboxItem,
	inboxQueryOptions,
	scopeOf,
	setChannelStarred,
	unreadSummaryQueryOptions,
} from "@/lib/chat-api";
import {
	groupByProject,
	isUnread,
	limitRows,
	rowName,
	type SectionKey,
	splitRows,
	totals,
} from "@/lib/chat-sidebar";
import { cn } from "@/lib/utils";
import { CreateChannelDialog } from "./create-channel-dialog";
import { NewMessageDialog } from "./new-message-dialog";
import { PeerAvatar } from "./peer-avatar";

const ACTIVE =
	"bg-primary/10 text-primary font-medium before:absolute before:left-0 before:inset-y-2 before:w-0.75 before:rounded-full before:bg-primary";
const INACTIVE =
	"text-sidebar-foreground/80 dark:text-sidebar-foreground/60 hover:bg-sidebar-accent/60 hover:text-sidebar-foreground";

const count = (n: number): string => (n > 99 ? "99+" : String(n));

/** The red mention badge when there are mentions, else the plain unread count. */
function Badge({ unread, mentions }: { unread: number; mentions: number }) {
	const { t } = useTranslation("chat");
	if (mentions > 0) {
		return (
			<SidebarMenuBadge
				role="img"
				aria-label={t("channels.mentionBadge")}
				className="rounded-full bg-destructive px-1.5 text-white"
			>
				@{count(mentions)}
			</SidebarMenuBadge>
		);
	}
	if (unread > 0) {
		return (
			<SidebarMenuBadge aria-label={t("channels.unreadBadge")}>
				{count(unread)}
			</SidebarMenuBadge>
		);
	}
	return null;
}

/** A pill for a section header: what is unread inside it while it is closed. */
function HeaderBadge({
	unread,
	mentions,
}: {
	unread: number;
	mentions: number;
}) {
	const { t } = useTranslation("chat");
	if (mentions > 0) {
		return (
			<span
				role="img"
				aria-label={t("channels.mentionBadge")}
				className="rounded-full bg-destructive px-1.5 text-[10px] font-medium text-white"
			>
				@{count(mentions)}
			</span>
		);
	}
	if (unread > 0) {
		return (
			<span
				role="img"
				aria-label={t("channels.unreadBadge")}
				className="rounded-full bg-sidebar-foreground px-1.5 text-[10px] font-medium text-sidebar"
			>
				{count(unread)}
			</span>
		);
	}
	return null;
}

/**
 * A collapsible section header. The toggle is the whole label; actions sit
 * beside it (a button inside a button is not valid). While closed it shows what
 * is unread inside, and the caller still lists the unread rows beneath it.
 */
function SectionHeader({
	label,
	open,
	onToggle,
	unread,
	mentions,
	actions,
}: {
	label: string;
	open: boolean;
	onToggle: () => void;
	unread: number;
	mentions: number;
	actions?: ReactNode;
}) {
	const { t } = useTranslation("chat");
	return (
		<div className="group/header mt-1 flex h-7 items-center gap-0.5 px-1 text-xs text-sidebar-foreground/70">
			<button
				type="button"
				aria-expanded={open}
				aria-label={`${open ? t("sidebar.collapse") : t("sidebar.expand")} ${label}`}
				onClick={onToggle}
				className="flex min-w-0 flex-1 items-center gap-1 rounded px-1 py-1 text-left hover:bg-sidebar-accent/60 hover:text-sidebar-foreground"
			>
				<ChevronDown
					className={cn(
						"size-3 shrink-0 transition-transform",
						!open && "-rotate-90",
					)}
				/>
				<span className="truncate">{label}</span>
				{open ? null : <HeaderBadge unread={unread} mentions={mentions} />}
			</button>
			{actions ? (
				<div className="flex items-center gap-0.5 opacity-0 transition-opacity group-focus-within/header:opacity-100 group-hover/header:opacity-100 [@media(hover:none)]:opacity-100">
					{actions}
				</div>
			) : null}
		</div>
	);
}

function IconAction({
	label,
	onClick,
	children,
}: {
	label: string;
	onClick: () => void;
	children: ReactNode;
}) {
	return (
		<button
			type="button"
			title={label}
			aria-label={label}
			onClick={onClick}
			className="flex size-5 items-center justify-center rounded text-sidebar-foreground/60 hover:bg-sidebar-accent hover:text-sidebar-foreground"
		>
			{children}
		</button>
	);
}

/** How many people are in a voice channel's call, when there is one. */
function VoiceCount({
	projectId,
	channelId,
}: {
	projectId: string;
	channelId: string;
}) {
	const { t } = useTranslation("chat");
	const enabled = useQuery(callConfigQueryOptions).data === true;
	// One request per project, shared by its voice rows.
	const calls = useQuery(projectCallsQueryOptions(projectId, enabled)).data;
	const n =
		calls?.find((c) => c.channel_id === channelId)?.participants.length ?? 0;
	if (n === 0) return null;
	return (
		<span
			title={t("call.voiceRow", { count: n })}
			className="ml-auto rounded-full bg-emerald-600/15 px-1.5 text-[10px] font-medium text-emerald-700 dark:text-emerald-400"
		>
			{n}
		</span>
	);
}

function RowIcon({ item }: { item: InboxItem }) {
	if (item.peer) {
		return <PeerAvatar name={item.peer.name} kind={item.peer.kind} size="xs" />;
	}
	if (item.kind === "private") return <Lock className="size-4" />;
	if (item.kind === "voice") return <Volume2 className="size-4" />;
	return <Hash className="size-4" />;
}

/** Where a row opens. A direct message opens under the current project when
 * there is one, so the sidebar keeps that project's context. */
function hrefOf(item: InboxItem, contextProject?: string): string {
	if (item.project_id) {
		return `/projects/${item.project_id}/chat/${item.channel_id}`;
	}
	return contextProject
		? `/projects/${contextProject}/dm/${item.channel_id}`
		: `/chat/dm/${item.channel_id}`;
}

// An element, not a component: the sidebar button clones it with its own props
// (class, children, ref), which a wrapper component would drop.
function rowLinkElement(
	item: InboxItem,
	contextProject?: string,
): ReactElement {
	if (item.project_id) {
		return (
			<Link
				to="/projects/$projectId/chat/$channelId"
				params={{ projectId: item.project_id, channelId: item.channel_id }}
			/>
		);
	}
	return contextProject ? (
		<Link
			to="/projects/$projectId/dm/$channelId"
			params={{ projectId: contextProject, channelId: item.channel_id }}
		/>
	) : (
		<Link to="/chat/dm/$channelId" params={{ channelId: item.channel_id }} />
	);
}

function Row({
	item,
	path,
	contextProject,
	showProject,
	onStar,
}: {
	item: InboxItem;
	path: string;
	contextProject?: string;
	showProject: boolean;
	onStar: (item: InboxItem) => void;
}) {
	const { t } = useTranslation("chat");
	const href = hrefOf(item, contextProject);
	const active = path === href;
	const unread = isUnread(item);
	const name = rowName(item);
	return (
		<SidebarMenuItem>
			<SidebarMenuButton
				isActive={active}
				tooltip={name}
				render={rowLinkElement(item, contextProject)}
				className={cn(
					"relative",
					active ? ACTIVE : INACTIVE,
					unread && !active && "font-semibold text-sidebar-foreground",
				)}
			>
				<RowIcon item={item} />
				<span className="truncate">{name}</span>
				{item.peer && item.peer.kind !== "user" ? (
					<span className="rounded bg-sidebar-accent px-1 text-[10px] font-normal text-sidebar-foreground/70">
						{t(`dm.badge.${item.peer.kind}`)}
					</span>
				) : null}
				{item.kind === "voice" && item.project_id ? (
					<VoiceCount projectId={item.project_id} channelId={item.channel_id} />
				) : null}
				{showProject && item.project_name ? (
					<span className="ml-auto max-w-20 truncate pr-1 text-[10px] font-normal text-sidebar-foreground/60">
						{item.project_name}
					</span>
				) : null}
			</SidebarMenuButton>
			<SidebarMenuAction
				showOnHover
				aria-label={item.starred ? t("sidebar.unstar") : t("sidebar.star")}
				aria-pressed={item.starred}
				title={item.starred ? t("sidebar.unstar") : t("sidebar.star")}
				onClick={(e) => {
					e.preventDefault();
					e.stopPropagation();
					onStar(item);
				}}
				className="right-7"
			>
				<Star className={cn("size-3.5", item.starred && "fill-current")} />
			</SidebarMenuAction>
			<Badge unread={item.unread_count} mentions={item.mention_count} />
		</SidebarMenuItem>
	);
}

/**
 * The Chat section of the main sidebar, built like its siblings (Project,
 * Interactions, Docs): a collapsible group label, then rows. Inside it come the
 * Inbox, the starred channels and direct messages, one group per project with
 * that project's channels (so a new message in any project is visible from any
 * page), and Direct messages. Every group collapses and remembers it; a closed
 * group still lists its unread rows and shows its unread count, so collapsing
 * never hides news.
 */
export function ChatSidebarSection({ projectId }: { projectId?: string }) {
	const { t } = useTranslation("chat");
	const path = useRouterState({ select: (s) => s.location.pathname });
	const { state: sidebar, isMobile } = useSidebar();
	const rail = sidebar === "collapsed" && !isMobile;
	const { state, update } = useChatSidebarState();
	const queryClient = useQueryClient();
	const [creating, setCreating] = useState(false);
	const [messaging, setMessaging] = useState(false);
	const [dmsAll, setDmsAll] = useState(false);

	const inbox = useQuery(inboxQueryOptions({}));
	const summary = useQuery(unreadSummaryQueryOptions);
	const { can: canDM } = useChatPermissions(DM_SCOPE);
	const { hasProjectPermission } = useProjectPermissions(projectId ?? "");

	const star = useMutation({
		mutationFn: (item: InboxItem) =>
			setChannelStarred(
				scopeOf(item.project_id),
				item.channel_id,
				!item.starred,
			),
		onSettled: () => {
			void queryClient.invalidateQueries({ queryKey: chatKeys.inboxAll() });
			void queryClient.invalidateQueries({ queryKey: ["chat"] });
		},
	});

	const all = inbox.data ?? [];
	const rows = splitRows(all);
	// The summary counts every channel; the rows are capped, so it is the total.
	const totalUnread = (summary.data ?? []).reduce((n, r) => n + r.unread, 0);
	const allMentions = totals(all).mentions;

	const toggle = (key: SectionKey) =>
		update((s) => ({ ...s, open: { ...s.open, [key]: !s.open[key] } }));

	const renderRow = (item: InboxItem, showProject: boolean) => (
		<Row
			key={item.channel_id}
			item={item}
			path={path}
			contextProject={projectId}
			showProject={showProject}
			onStar={(i) => star.mutate(i)}
		/>
	);

	// A closed group keeps its unread rows in view.
	const visible = (list: InboxItem[], open: boolean) =>
		open ? list : list.filter(isUnread);

	const groups = groupByProject(rows.channels, projectId);
	const dmsList = state.open.dms
		? limitRows(rows.dms, dmsAll)
		: { shown: rows.dms.filter(isUnread), hidden: 0 };
	const dmTotals = totals(rows.dms);
	const starTotals = totals(rows.starred);
	const canCreate = !!projectId && hasProjectPermission("channels:write");
	const inCurrent = groups.some((g) => g.id === projectId);
	const open = state.open.chat || rail;

	return (
		<SidebarGroup>
			{rail ? null : (
				<SidebarGroupLabel
					render={<button type="button" />}
					aria-expanded={state.open.chat}
					aria-label={`${state.open.chat ? t("sidebar.collapse") : t("sidebar.expand")} ${t("title")}`}
					className="flex w-full cursor-pointer items-center justify-between gap-2 text-left transition-colors hover:text-sidebar-foreground"
					onClick={() => toggle("chat")}
				>
					<span>{t("title")}</span>
					<span className="flex items-center gap-1.5">
						{state.open.chat ? null : (
							<HeaderBadge unread={totalUnread} mentions={allMentions} />
						)}
						<ChevronRight
							className={cn(
								"size-3.5 text-sidebar-foreground/60 transition-transform duration-200",
								state.open.chat && "rotate-90",
							)}
						/>
					</span>
				</SidebarGroupLabel>
			)}
			{open ? (
				<SidebarGroupContent>
					<SidebarMenu>
						<SidebarMenuItem>
							<SidebarMenuButton
								isActive={path === "/chat"}
								tooltip={t("inbox.title")}
								render={<Link to="/chat" search={{}} />}
								className={cn("relative", path === "/chat" ? ACTIVE : INACTIVE)}
							>
								<Inbox className="size-4" />
								<span>{t("inbox.title")}</span>
							</SidebarMenuButton>
							<Badge unread={totalUnread} mentions={allMentions} />
						</SidebarMenuItem>
					</SidebarMenu>

					{rail ? null : (
						<>
							{rows.starred.length > 0 ? (
								<>
									<SectionHeader
										label={t("sidebar.starred")}
										open={state.open.starred}
										onToggle={() => toggle("starred")}
										unread={starTotals.unread}
										mentions={starTotals.mentions}
									/>
									<SidebarMenu>
										{visible(rows.starred, state.open.starred).map((i) =>
											renderRow(i, !i.peer),
										)}
									</SidebarMenu>
								</>
							) : null}

							{groups.map((g) => {
								const isOpen = !state.closedProjects[g.id];
								const current = g.id === projectId;
								return (
									<div key={g.id}>
										<SectionHeader
											label={g.name}
											open={isOpen}
											onToggle={() =>
												update((s) => ({
													...s,
													closedProjects: {
														...s.closedProjects,
														[g.id]: isOpen,
													},
												}))
											}
											unread={g.unread}
											mentions={g.mentions}
											actions={
												<>
													<Link
														to="/projects/$projectId/chat"
														params={{ projectId: g.id }}
														title={t("browse.title")}
														aria-label={t("browse.title")}
														className="flex size-5 items-center justify-center rounded text-sidebar-foreground/60 hover:bg-sidebar-accent hover:text-sidebar-foreground"
													>
														<Compass className="size-3.5" />
													</Link>
													{current && canCreate ? (
														<IconAction
															label={t("channels.new")}
															onClick={() => setCreating(true)}
														>
															<Plus className="size-3.5" />
														</IconAction>
													) : null}
												</>
											}
										/>
										<SidebarMenu>
											{visible(g.rows, isOpen).map((i) => renderRow(i, false))}
										</SidebarMenu>
									</div>
								);
							})}

							{projectId && !inCurrent ? (
								<SidebarMenu>
									<SidebarMenuItem>
										<SidebarMenuButton
											tooltip={t("browse.title")}
											render={
												<Link
													to="/projects/$projectId/chat"
													params={{ projectId }}
												/>
											}
											className={INACTIVE}
										>
											<Compass className="size-4" />
											<span>{t("browse.title")}</span>
										</SidebarMenuButton>
										{canCreate ? (
											<SidebarMenuAction
												aria-label={t("channels.new")}
												title={t("channels.new")}
												onClick={() => setCreating(true)}
											>
												<Plus className="size-3.5" />
											</SidebarMenuAction>
										) : null}
									</SidebarMenuItem>
								</SidebarMenu>
							) : null}

							<SectionHeader
								label={t("dm.heading")}
								open={state.open.dms}
								onToggle={() => toggle("dms")}
								unread={dmTotals.unread}
								mentions={dmTotals.mentions}
								actions={
									canDM("channels:write") ? (
										<IconAction
											label={t("dm.new")}
											onClick={() => setMessaging(true)}
										>
											<Plus className="size-3.5" />
										</IconAction>
									) : null
								}
							/>
							<SidebarMenu>
								{dmsList.shown.map((i) => renderRow(i, false))}
							</SidebarMenu>
							{state.open.dms &&
							(dmsList.hidden > 0 || dmsAll) &&
							rows.dms.length > 0 ? (
								<button
									type="button"
									onClick={() => setDmsAll((v) => !v)}
									className="px-3 py-1 text-left text-[11px] text-sidebar-foreground/60 hover:text-sidebar-foreground"
								>
									{dmsAll
										? t("sidebar.showLess")
										: t("sidebar.showMore", { n: dmsList.hidden })}
								</button>
							) : null}
						</>
					)}
				</SidebarGroupContent>
			) : null}

			<NewMessageDialog
				open={messaging}
				onOpenChange={setMessaging}
				projectId={projectId}
			/>
			{projectId ? (
				<CreateChannelDialog
					projectId={projectId}
					open={creating}
					onOpenChange={setCreating}
				/>
			) : null}
		</SidebarGroup>
	);
}
