import {
	useInfiniteQuery,
	useMutation,
	useQueryClient,
} from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import type { TFunction } from "i18next";
import {
	AtSign,
	Bell,
	Loader2,
	UserPlus,
	Volume2,
	VolumeX,
} from "lucide-react";
import { useCallback, useEffect, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";

import {
	Avatar,
	AvatarBadge,
	AvatarFallback,
	AvatarImage,
} from "@/components/ui/avatar";
import {
	Popover,
	PopoverContent,
	PopoverTrigger,
} from "@/components/ui/popover";
import { useDocumentTitle } from "@/hooks/use-document-title";
import {
	MAX_DISPLAYED_UNREAD_COUNT,
	markAllNotificationsAsRead,
	markNotificationAsRead,
	type Notification,
	notificationsQueryOptions,
} from "@/lib/notification-api";
import {
	isNotificationSoundMuted,
	setNotificationSoundMuted,
} from "@/lib/notification-sound";
import { resolveNotificationActorAvatarUrl } from "@/lib/provider-logos";
import { createLoadMoreScrollHandler } from "@/lib/scroll-pagination";
import { timeAgo } from "@/lib/time-ago";
import { getInitials } from "@/lib/utils";

function notificationText(n: Notification, t: TFunction<"appShell">): string {
	if (n.type === "assigned") {
		return t("notifications.text.assigned", {
			actor: n.actor_full_name,
			taskNumber: n.task_number,
			taskTitle: n.task_title,
		});
	}
	return t("notifications.text.mentioned", {
		actor: n.actor_full_name,
		taskNumber: n.task_number,
		taskTitle: n.task_title,
	});
}

export function NotificationBell() {
	const { t } = useTranslation("appShell");
	const { t: tCommon } = useTranslation("common");
	const navigate = useNavigate();
	const queryClient = useQueryClient();
	const { data, fetchNextPage, hasNextPage, isFetchingNextPage } =
		useInfiniteQuery(notificationsQueryOptions);

	const unreadCount = data?.pages[0]?.unread_count ?? 0;
	const notifications = useMemo(
		() => data?.pages.flatMap((page) => page.items) ?? [],
		[data],
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
			if (n.task_id) {
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
							{notifications.map((n) => {
								const avatarUrl = resolveNotificationActorAvatarUrl(n);
								return (
									<li key={n.id}>
										<button
											type="button"
											onClick={() => handleNotificationClick(n)}
											className={`w-full text-left px-4 py-3 hover:bg-muted/50 transition-colors flex items-start gap-3 ${!n.read_at ? "bg-primary/5" : ""}`}
										>
											<Avatar className="mt-0.5 shrink-0">
												{avatarUrl ? <AvatarImage src={avatarUrl} /> : null}
												<AvatarFallback className="font-medium">
													{getInitials(n.actor_full_name)}
												</AvatarFallback>
												<AvatarBadge
													aria-hidden="true"
													className={
														n.type === "assigned"
															? undefined
															: "bg-secondary text-secondary-foreground"
													}
												>
													{n.type === "assigned" ? <UserPlus /> : <AtSign />}
												</AvatarBadge>
											</Avatar>
											<div className="min-w-0 flex-1">
												<p
													className={`text-sm leading-snug ${!n.read_at ? "font-medium" : ""}`}
												>
													{notificationText(n, t)}
												</p>
												<p className="mt-0.5 truncate text-xs text-muted-foreground">
													{n.project_name} · {timeAgo(n.created_at, tCommon)}
												</p>
											</div>
											{!n.read_at && (
												<span
													aria-hidden="true"
													className="mt-2 h-1.5 w-1.5 shrink-0 rounded-full bg-primary"
												/>
											)}
										</button>
									</li>
								);
							})}
						</ul>
						{isFetchingNextPage && (
							<div className="flex items-center justify-center gap-1.5 py-3 text-xs text-muted-foreground">
								<Loader2 className="h-3 w-3 animate-spin" />
								{t("notifications.loadingMore")}
							</div>
						)}
					</div>
				)}
			</PopoverContent>
		</Popover>
	);
}
