import {
	useInfiniteQuery,
	useMutation,
	useQuery,
	useQueryClient,
} from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import {
	Bell,
	BellOff,
	CheckCheck,
	Settings,
	Volume2,
	VolumeX,
} from "lucide-react";
import { useCallback, useEffect, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";
import {
	Popover,
	PopoverContent,
	PopoverTrigger,
} from "@/components/ui/popover";
import { Skeleton } from "@/components/ui/skeleton";
import { Spinner } from "@/components/ui/spinner";
import { useDocumentTitle } from "@/hooks/use-document-title";
import { isDoNotDisturbActive } from "@/lib/do-not-disturb";
import {
	MAX_DISPLAYED_UNREAD_COUNT,
	markAllNotificationsAsRead,
	markNotificationAsRead,
	type Notification,
	notificationsQueryOptions,
} from "@/lib/notification-api";
import { type DayGroup, groupByDay } from "@/lib/notification-groups";
import {
	isNotificationSoundMuted,
	setNotificationSoundMuted,
} from "@/lib/notification-sound";
import { pushSettingsQueryOptions } from "@/lib/push-api";
import { createLoadMoreScrollHandler } from "@/lib/scroll-pagination";
import { cn } from "@/lib/utils";
import { NotificationItem } from "./notification-item";
import { NotificationPushPrompt } from "./notification-push-prompt";

type Filter = "all" | "unread";

const GROUP_KEYS = {
	today: "notifications.group.today",
	yesterday: "notifications.group.yesterday",
	earlier: "notifications.group.earlier",
} as const satisfies Record<DayGroup, string>;

function ListSkeleton() {
	return (
		<ul aria-hidden="true" className="divide-y">
			{[0, 1, 2].map((i) => (
				<li key={i} className="flex items-start gap-3 px-4 py-3">
					<Skeleton className="mt-0.5 size-8 shrink-0 rounded-full" />
					<div className="flex-1 space-y-2">
						<Skeleton className="h-3.5 w-11/12" />
						<Skeleton className="h-3 w-1/3" />
					</div>
				</li>
			))}
		</ul>
	);
}

export function NotificationBell() {
	const { t, i18n } = useTranslation("appShell");
	const { t: tCommon } = useTranslation("common");
	const navigate = useNavigate();
	const queryClient = useQueryClient();
	const [open, setOpen] = useState(false);
	const [filter, setFilter] = useState<Filter>("all");
	const {
		data,
		fetchNextPage,
		hasNextPage,
		isFetchingNextPage,
		isPending,
		isError,
		refetch,
	} = useInfiniteQuery(notificationsQueryOptions);
	// Do Not Disturb is read from the cache the layout keeps warm; the bell
	// only shows it, so a failed load just leaves the plain bell.
	const { data: pushSettings } = useQuery({
		...pushSettingsQueryOptions,
		staleTime: 5 * 60_000,
	});
	const dnd = isDoNotDisturbActive(pushSettings?.do_not_disturb);

	const unreadCount = data?.pages[0]?.unread_count ?? 0;
	const notifications = useMemo(
		() => data?.pages.flatMap((page) => page.items) ?? [],
		[data],
	);
	const visible = useMemo(
		() =>
			filter === "unread"
				? notifications.filter((n) => !n.read_at)
				: notifications,
		[notifications, filter],
	);
	const groups = useMemo(() => groupByDay(visible), [visible]);
	useDocumentTitle(unreadCount);

	// The unread filter only sees what is loaded. While some unread
	// notifications are still on pages not fetched yet, fetch on: a short
	// filtered list gives no scroll to ask for more with.
	const loadedUnread = notifications.filter((n) => !n.read_at).length;
	useEffect(() => {
		if (
			open &&
			filter === "unread" &&
			hasNextPage &&
			!isFetchingNextPage &&
			loadedUnread < unreadCount
		) {
			void fetchNextPage();
		}
	}, [
		open,
		filter,
		hasNextPage,
		isFetchingNextPage,
		loadedUnread,
		unreadCount,
		fetchNextPage,
	]);

	const handleListScroll = createLoadMoreScrollHandler({
		hasMore: !!hasNextPage,
		isLoadingMore: isFetchingNextPage,
		onLoadMore: () => void fetchNextPage(),
	});

	const [soundMuted, setSoundMuted] = useState(isNotificationSoundMuted);
	const toggleSoundMuted = useCallback(() => {
		setSoundMuted((prev) => !prev);
	}, []);
	useEffect(() => {
		setNotificationSoundMuted(soundMuted);
	}, [soundMuted]);

	const { mutate: markRead } = useMutation({
		mutationFn: markNotificationAsRead,
		onSuccess: () =>
			queryClient.invalidateQueries({ queryKey: ["notifications"] }),
	});

	const { mutate: markAllRead } = useMutation({
		mutationFn: markAllNotificationsAsRead,
		onSuccess: () =>
			queryClient.invalidateQueries({ queryKey: ["notifications"] }),
	});

	const handleNotificationClick = useCallback(
		(n: Notification) => {
			if (!n.read_at) markRead(n.id);
			if (n.type === "question" && n.conversation_id) {
				if (n.project_id) {
					navigate({
						to: "/projects/$projectId/conversations/$conversationId",
						params: {
							projectId: n.project_id,
							conversationId: n.conversation_id,
						},
					});
				} else {
					navigate({
						to: "/conversations/$conversationId",
						params: { conversationId: n.conversation_id },
					});
				}
			} else if (!n.project_id) {
				return;
			} else if (n.document_id) {
				navigate({
					to: "/projects/$projectId/docs/$docId",
					params: { projectId: n.project_id, docId: n.document_id },
				});
			} else if (n.task_id) {
				navigate({
					to: "/projects/$projectId/tasks/$taskId",
					params: { projectId: n.project_id, taskId: n.task_id },
				});
			} else {
				navigate({
					to: "/projects/$projectId",
					params: { projectId: n.project_id },
				});
			}
		},
		[markRead, navigate],
	);

	// Opening something closes the list, so the page it opens is not left
	// half-covered by it.
	const handleOpen = useCallback(
		(n: Notification) => {
			handleNotificationClick(n);
			setOpen(false);
		},
		[handleNotificationClick],
	);

	const openSettings = useCallback(() => {
		setOpen(false);
		navigate({ to: "/account/preferences" });
	}, [navigate]);

	const TriggerIcon = dnd ? BellOff : Bell;

	return (
		<Popover open={open} onOpenChange={setOpen}>
			<PopoverTrigger
				className="relative inline-flex h-8 w-8 items-center justify-center rounded-lg text-muted-foreground transition-colors hover:bg-muted hover:text-foreground"
				aria-label={t("notifications.title")}
			>
				<TriggerIcon className="h-4 w-4" />
				{unreadCount > 0 && (
					<span
						className={cn(
							"absolute -top-0.5 -right-0.5 flex h-4 min-w-4 items-center justify-center rounded-full px-1 text-xs font-medium leading-none",
							// A silenced bell still counts, but does not shout.
							dnd
								? "bg-muted-foreground text-background"
								: "bg-primary text-primary-foreground",
						)}
					>
						{unreadCount > MAX_DISPLAYED_UNREAD_COUNT
							? `${MAX_DISPLAYED_UNREAD_COUNT}+`
							: unreadCount}
					</span>
				)}
			</PopoverTrigger>
			<PopoverContent
				align="end"
				sideOffset={8}
				className="w-[min(26rem,calc(100vw-1.5rem))] overflow-hidden p-0 shadow-lg"
			>
				<div className="border-b">
					<div className="flex items-center justify-between gap-2 px-4 pt-3 pb-2">
						<span className="text-sm font-semibold">
							{t("notifications.title")}
						</span>
						<div className="flex items-center gap-1">
							{unreadCount > 0 && (
								<Button
									size="xs"
									variant="ghost"
									onClick={() => markAllRead()}
									className="text-muted-foreground"
								>
									<CheckCheck data-icon="inline-start" />
									{t("notifications.markAllAsRead")}
								</Button>
							)}
							<button
								type="button"
								onClick={toggleSoundMuted}
								aria-label={
									soundMuted
										? t("notifications.unmuteSound")
										: t("notifications.muteSound")
								}
								title={
									soundMuted
										? t("notifications.unmuteSound")
										: t("notifications.muteSound")
								}
								className="flex size-6 items-center justify-center rounded-md text-muted-foreground transition-colors outline-none hover:bg-muted hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring"
							>
								{soundMuted ? (
									<VolumeX className="size-3.5" />
								) : (
									<Volume2 className="size-3.5" />
								)}
							</button>
							<button
								type="button"
								onClick={openSettings}
								aria-label={t("notifications.settings")}
								title={t("notifications.settings")}
								className="flex size-6 items-center justify-center rounded-md text-muted-foreground transition-colors outline-none hover:bg-muted hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring"
							>
								<Settings className="size-3.5" />
							</button>
						</div>
					</div>
					<div className="flex items-center justify-between gap-2 px-4 pb-2.5">
						<fieldset className="m-0 inline-flex min-w-0 rounded-lg border-0 bg-muted p-0.5 text-xs">
							<legend className="sr-only">
								{t("notifications.filter.label")}
							</legend>
							{(["all", "unread"] as const).map((f) => (
								<button
									key={f}
									type="button"
									aria-pressed={filter === f}
									onClick={() => setFilter(f)}
									className={cn(
										"rounded-md px-2.5 py-1 font-medium transition-colors outline-none focus-visible:ring-2 focus-visible:ring-ring",
										filter === f
											? "bg-background text-foreground shadow-sm"
											: "text-muted-foreground hover:text-foreground",
									)}
								>
									{t(`notifications.filter.${f}`)}
									{f === "unread" && unreadCount > 0 ? (
										<span className="ml-1 text-muted-foreground tabular-nums">
											{unreadCount > MAX_DISPLAYED_UNREAD_COUNT
												? `${MAX_DISPLAYED_UNREAD_COUNT}+`
												: unreadCount}
										</span>
									) : null}
								</button>
							))}
						</fieldset>
						{dnd && (
							<button
								type="button"
								onClick={openSettings}
								className="inline-flex items-center gap-1 rounded-md px-1.5 py-1 text-xs text-muted-foreground transition-colors outline-none hover:bg-muted hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring"
							>
								<BellOff className="size-3" aria-hidden="true" />
								{t("notifications.dndOn")}
							</button>
						)}
					</div>
				</div>

				{isPending ? (
					<ListSkeleton />
				) : isError && notifications.length === 0 ? (
					<div className="flex flex-col items-center gap-3 px-6 py-10 text-center">
						<p className="text-sm text-muted-foreground">
							{t("notifications.loadError")}
						</p>
						<Button size="sm" variant="outline" onClick={() => void refetch()}>
							{t("notifications.retry")}
						</Button>
					</div>
				) : visible.length === 0 ? (
					<div className="flex flex-col items-center justify-center px-6 py-10 text-center text-muted-foreground">
						<Bell className="mb-2 h-8 w-8 opacity-30" aria-hidden="true" />
						<p className="text-sm">
							{filter === "unread"
								? isFetchingNextPage ||
									(hasNextPage && loadedUnread < unreadCount)
									? t("notifications.loadingMore")
									: t("notifications.caughtUp")
								: t("notifications.empty")}
						</p>
					</div>
				) : (
					<div
						className="max-h-[min(28rem,60vh)] overflow-y-auto"
						onScroll={handleListScroll}
					>
						{groups.map((g) => (
							<section key={g.group}>
								{/* Opaque and above the avatar badges (z-10 inside each row), or a row
								    scrolling underneath shows through and its badge paints over this. */}
								<h3 className="sticky top-0 z-20 border-b bg-popover px-4 py-1.5 text-xs font-medium text-muted-foreground">
									{t(GROUP_KEYS[g.group])}
								</h3>
								<ul className="relative z-0 divide-y">
									{g.items.map((n) => (
										<NotificationItem
											key={n.id}
											n={n}
											locale={i18n.language}
											onOpen={handleOpen}
											onMarkRead={markRead}
											t={t}
											tCommon={tCommon}
										/>
									))}
								</ul>
							</section>
						))}
						{isFetchingNextPage && (
							<div className="flex items-center justify-center gap-1.5 py-3 text-xs text-muted-foreground">
								<Spinner aria-hidden="true" className="h-3 w-3" />
								{t("notifications.loadingMore")}
							</div>
						)}
					</div>
				)}
				<NotificationPushPrompt />
			</PopoverContent>
		</Popover>
	);
}
