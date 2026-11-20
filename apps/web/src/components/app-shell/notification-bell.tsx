import {
	useInfiniteQuery,
	useMutation,
	useQueryClient,
} from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { Bell, Volume2, VolumeX } from "lucide-react";
import { useCallback, useEffect, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import {
	Popover,
	PopoverContent,
	PopoverTrigger,
} from "@/components/ui/popover";
import { Spinner } from "@/components/ui/spinner";
import { useDocumentTitle } from "@/hooks/use-document-title";
import {
	MAX_DISPLAYED_UNREAD_COUNT,
	markAllNotificationsAsRead,
	markNotificationAsRead,
	type Notification,
	notificationsQueryOptions,
	notificationTarget,
} from "@/lib/notification-api";
import {
	isNotificationSoundMuted,
	setNotificationSoundMuted,
} from "@/lib/notification-sound";
import { createLoadMoreScrollHandler } from "@/lib/scroll-pagination";
import { groupNotifications, NotificationCard } from "./notification-card";

export function NotificationBell() {
	const { t } = useTranslation("appShell");
	const navigate = useNavigate();
	const queryClient = useQueryClient();
	const { data, fetchNextPage, hasNextPage, isFetchingNextPage } =
		useInfiniteQuery(notificationsQueryOptions);

	const unreadCount = data?.pages[0]?.unread_count ?? 0;
	const notifications = useMemo(
		() => data?.pages.flatMap((page) => page.items) ?? [],
		[data],
	);
	const groups = useMemo(
		() => groupNotifications(notifications),
		[notifications],
	);
	useDocumentTitle(unreadCount);

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
			openNotificationTarget(navigate, n);
		},
		[markRead, navigate],
	);

	return (
		<Popover>
			<PopoverTrigger
				className="relative inline-flex h-8 w-8 items-center justify-center rounded-lg text-muted-foreground transition-colors hover:bg-muted hover:text-foreground"
				aria-label={t("notifications.title")}
			>
				<Bell className="h-4 w-4" />
				{unreadCount > 0 && (
					<span className="absolute -top-0.5 -right-0.5 flex h-4 w-4 items-center justify-center rounded-full bg-primary text-xs font-medium text-primary-foreground leading-none">
						{unreadCount > MAX_DISPLAYED_UNREAD_COUNT
							? `${MAX_DISPLAYED_UNREAD_COUNT}+`
							: unreadCount}
					</span>
				)}
			</PopoverTrigger>
			<PopoverContent
				align="end"
				sideOffset={8}
				className="w-96 overflow-hidden p-0 shadow-lg"
			>
				<div className="flex items-center justify-between px-4 py-3 border-b">
					<span className="text-sm font-semibold">
						{t("notifications.title")}
					</span>
					<div className="flex items-center gap-3">
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
							className="text-muted-foreground hover:text-foreground transition-colors"
						>
							{soundMuted ? (
								<VolumeX className="h-3.5 w-3.5" />
							) : (
								<Volume2 className="h-3.5 w-3.5" />
							)}
						</button>
						{unreadCount > 0 && (
							<button
								type="button"
								onClick={() => markAllRead()}
								className="text-xs text-muted-foreground hover:text-foreground transition-colors"
							>
								{t("notifications.markAllAsRead")}
							</button>
						)}
					</div>
				</div>
				{notifications.length === 0 ? (
					<div className="flex flex-col items-center justify-center py-10 text-muted-foreground">
						<Bell className="h-8 w-8 mb-2 opacity-30" />
						<p className="text-sm">{t("notifications.empty")}</p>
					</div>
				) : (
					<div className="max-h-96 overflow-y-auto" onScroll={handleListScroll}>
						<ul className="divide-y">
							{groups.map((g) => (
								<li key={g.head.id}>
									<NotificationCard
										group={g}
										onOpen={(n) => {
											for (const x of g.items) {
												if (!x.read_at && x.id !== n.id) markRead(x.id);
											}
											handleNotificationClick(n);
										}}
									/>
								</li>
							))}
						</ul>
						{isFetchingNextPage && (
							<div className="flex items-center justify-center gap-1.5 py-3 text-xs text-muted-foreground">
								<Spinner aria-hidden="true" className="h-3 w-3" />
								{t("notifications.loadingMore")}
							</div>
						)}
					</div>
				)}
				<button
					type="button"
					onClick={() => navigate({ to: "/notifications" })}
					className="w-full border-t px-4 py-2.5 text-center text-xs font-medium text-primary transition-colors hover:bg-muted/50"
				>
					{t("notifications.viewAll")}
				</button>
			</PopoverContent>
		</Popover>
	);
}

type Navigate = ReturnType<typeof useNavigate>;

/** Opens whatever a notification is about; a mention of a comment lands on
 *  that comment. Shared by the bell and the /notifications page. */
export function openNotificationTarget(navigate: Navigate, n: Notification) {
	const target = notificationTarget(n);
	if (!target) return;
	switch (target.kind) {
		case "conversation":
			if (target.projectId) {
				navigate({
					to: "/projects/$projectId/conversations/$conversationId",
					params: {
						projectId: target.projectId,
						conversationId: target.conversationId,
					},
				});
			} else {
				navigate({
					to: "/conversations/$conversationId",
					params: { conversationId: target.conversationId },
				});
			}
			return;
		case "doc":
			navigate({
				to: "/projects/$projectId/docs/$docId",
				params: { projectId: target.projectId, docId: target.docId },
			});
			return;
		case "task":
			navigate({
				to: "/projects/$projectId/tasks/$taskId",
				params: { projectId: target.projectId, taskId: target.taskId },
				...(target.commentId ? { search: { comment: target.commentId } } : {}),
			});
			return;
		case "project":
			navigate({
				to: "/projects/$projectId",
				params: { projectId: target.projectId },
			});
	}
}
