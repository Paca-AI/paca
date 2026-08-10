import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import type { TFunction } from "i18next";
import { Bell } from "lucide-react";
import { useCallback } from "react";
import { useTranslation } from "react-i18next";

import {
	Popover,
	PopoverContent,
	PopoverTrigger,
} from "@/components/ui/popover";
import { ScrollArea } from "@/components/ui/scroll-area";
import { useDocumentTitle } from "@/hooks/use-document-title";
import {
	markAllNotificationsAsRead,
	markNotificationAsRead,
	type Notification,
	notificationsQueryOptions,
} from "@/lib/notification-api";
import { timeAgo } from "@/lib/time-ago";

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
	const { data } = useQuery(notificationsQueryOptions);

	const unreadCount = data?.unread_count ?? 0;
	const notifications = data?.items ?? [];
	useDocumentTitle(unreadCount);

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
						{unreadCount > 9 ? "9+" : unreadCount}
					</span>
				)}
			</PopoverTrigger>
			<PopoverContent align="end" sideOffset={8} className="w-80 p-0 shadow-lg">
				<div className="flex items-center justify-between px-4 py-3 border-b">
					<span className="text-sm font-semibold">
						{t("notifications.title")}
					</span>
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
				{notifications.length === 0 ? (
					<div className="flex flex-col items-center justify-center py-10 text-muted-foreground">
						<Bell className="h-8 w-8 mb-2 opacity-30" />
						<p className="text-sm">{t("notifications.empty")}</p>
					</div>
				) : (
					<ScrollArea className="max-h-96">
						<ul className="divide-y">
							{notifications.map((n) => (
								<li key={n.id}>
									<button
										type="button"
										onClick={() => handleNotificationClick(n)}
										className={`w-full text-left px-4 py-3 hover:bg-muted/50 transition-colors flex gap-3 ${!n.read_at ? "bg-primary/5" : ""}`}
									>
										{!n.read_at && (
											<span className="mt-1.5 h-2 w-2 shrink-0 rounded-full bg-primary" />
										)}
										<div className={!n.read_at ? "" : "pl-5"}>
											<p className="text-sm leading-snug">
												{notificationText(n, t)}
											</p>
											<p className="mt-0.5 text-xs text-muted-foreground">
												{n.project_name} · {timeAgo(n.created_at, tCommon)}
											</p>
										</div>
									</button>
								</li>
							))}
						</ul>
					</ScrollArea>
				)}
			</PopoverContent>
		</Popover>
	);
}
