import type { TFunction } from "i18next";
import {
	AtSign,
	Check,
	MessageCircleQuestion,
	UserPlus,
	Zap,
} from "lucide-react";
import { EntityAvatar } from "@/components/shared/entity-avatar";
import { AvatarBadge } from "@/components/ui/avatar";
import type { Notification } from "@/lib/notification-api";
import { notificationActorAvatar } from "@/lib/provider-logos";
import { timeAgo } from "@/lib/time-ago";
import { cn } from "@/lib/utils";

export function notificationText(
	n: Notification,
	t: TFunction<"appShell">,
): string {
	if (n.type === "question") {
		return t("notifications.text.question", {
			actor: n.actor_full_name,
			title: n.conversation_title || t("notifications.untitledConversation"),
		});
	}
	if (n.type === "automation") {
		return t("notifications.text.automation", {
			message: n.message,
			taskNumber: n.task_number,
			taskTitle: n.task_title,
		});
	}
	if (n.type === "assigned") {
		return t("notifications.text.assigned", {
			actor: n.actor_full_name,
			taskNumber: n.task_number,
			taskTitle: n.task_title,
		});
	}
	if (n.document_id) {
		return t("notifications.text.mentionedInDoc", {
			actor: n.actor_full_name,
			title: n.document_title || t("notifications.untitledDocument"),
		});
	}
	return t("notifications.text.mentioned", {
		actor: n.actor_full_name,
		taskNumber: n.task_number,
		taskTitle: n.task_title,
	});
}

function Leading({ n }: { n: Notification }) {
	if (n.type === "automation") {
		return (
			<span
				aria-hidden="true"
				className="mt-0.5 flex size-8 shrink-0 items-center justify-center rounded-full bg-secondary text-secondary-foreground"
			>
				<Zap className="size-4" />
			</span>
		);
	}
	return (
		<EntityAvatar
			{...notificationActorAvatar(n)}
			className="mt-0.5 shrink-0"
			badge={
				<AvatarBadge
					aria-hidden="true"
					className={
						n.type === "assigned"
							? undefined
							: "bg-secondary text-secondary-foreground"
					}
				>
					{n.type === "assigned" ? (
						<UserPlus />
					) : n.type === "question" ? (
						<MessageCircleQuestion />
					) : (
						<AtSign />
					)}
				</AvatarBadge>
			}
		/>
	);
}

interface NotificationItemProps {
	n: Notification;
	locale: string;
	onOpen: (n: Notification) => void;
	onMarkRead: (id: string) => void;
	t: TFunction<"appShell">;
	tCommon: TFunction<"common">;
}

/** One row of the notification list: what happened, where, and when, with a
 *  quick "mark as read" that appears on hover or keyboard focus. */
export function NotificationItem({
	n,
	locale,
	onOpen,
	onMarkRead,
	t,
	tCommon,
}: NotificationItemProps) {
	const unread = !n.read_at;
	const absolute = new Date(n.created_at).toLocaleString(locale, {
		dateStyle: "medium",
		timeStyle: "short",
	});
	return (
		<li className="group/item relative">
			<button
				type="button"
				onClick={() => onOpen(n)}
				className={cn(
					"flex w-full items-start gap-3 px-4 py-3 pr-11 text-left transition-colors outline-none hover:bg-muted/60 focus-visible:bg-muted",
					unread && "bg-primary/5",
				)}
			>
				<Leading n={n} />
				<div className="min-w-0 flex-1">
					<p
						className={cn(
							"line-clamp-2 text-sm leading-snug",
							unread ? "font-medium text-foreground" : "text-foreground/80",
						)}
					>
						{notificationText(n, t)}
					</p>
					<p className="mt-1 flex min-w-0 items-center gap-1.5 text-xs text-muted-foreground">
						<span className="truncate">
							{n.project_name || t("notifications.globalChat")}
						</span>
						<span aria-hidden="true">·</span>
						<time dateTime={n.created_at} title={absolute} className="shrink-0">
							{timeAgo(n.created_at, tCommon)}
						</time>
					</p>
				</div>
			</button>
			{unread && (
				<>
					<span
						aria-hidden="true"
						className="pointer-events-none absolute top-4 right-4 size-2 rounded-full bg-primary transition-opacity group-focus-within/item:opacity-0 group-hover/item:opacity-0"
					/>
					<button
						type="button"
						onClick={() => onMarkRead(n.id)}
						aria-label={t("notifications.markRead")}
						title={t("notifications.markRead")}
						className="absolute top-2.5 right-2.5 flex size-6 items-center justify-center rounded-md text-muted-foreground opacity-0 transition-opacity outline-none hover:bg-background hover:text-foreground focus-visible:opacity-100 focus-visible:ring-2 focus-visible:ring-ring group-focus-within/item:opacity-100 group-hover/item:opacity-100"
					>
						<Check className="size-3.5" />
					</button>
				</>
			)}
		</li>
	);
}
