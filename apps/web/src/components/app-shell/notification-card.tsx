import type { TFunction } from "i18next";
import { AtSign, MessageCircleQuestion, UserPlus, Zap } from "lucide-react";
import { Fragment, type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { EntityAvatar } from "@/components/shared/entity-avatar";
import { AvatarBadge } from "@/components/ui/avatar";
import type { Notification } from "@/lib/notification-api";
import { notificationActorAvatar } from "@/lib/provider-logos";
import { timeAgo } from "@/lib/time-ago";

/** One-line text of a notification, as the bell has always shown it. */
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

/** A mention on a task gets the rich card: where it happened, not just who. */
export function isTaskMention(n: Notification): boolean {
	return n.type === "mentioned" && !!n.task_id && !n.document_id;
}

export interface NotificationGroup {
	/** The newest notification of the group. */
	head: Notification;
	items: Notification[];
}

/** Collapses runs of mentions on the same task into one group, keeping the
 *  list's order. Everything else stays a group of one. */
export function groupNotifications(list: Notification[]): NotificationGroup[] {
	const groups: NotificationGroup[] = [];
	for (const n of list) {
		const last = groups[groups.length - 1];
		if (
			last &&
			isTaskMention(n) &&
			isTaskMention(last.head) &&
			last.head.task_id === n.task_id
		) {
			last.items.push(n);
		} else {
			groups.push({ head: n, items: [n] });
		}
	}
	return groups;
}

const MENTION_RE = /(@[^\s@]+)/g;

/** The excerpt with each @mention picked out. */
export function HighlightedExcerpt({ text }: { text: string }) {
	const parts = text
		.split(MENTION_RE)
		.map((part, position) => ({ part, position }));
	return (
		<>
			{parts.map(({ part, position }) =>
				part.startsWith("@") && part.length > 1 ? (
					<mark
						key={position}
						className="rounded bg-primary/15 px-0.5 font-medium text-foreground"
					>
						{part}
					</mark>
				) : (
					<Fragment key={position}>{part}</Fragment>
				),
			)}
		</>
	);
}

const chip =
	"inline-flex max-w-full items-center gap-1 truncate rounded-md border border-border/60 bg-muted/50 px-1.5 py-0.5 text-xs text-muted-foreground";

function sourceLabel(n: Notification, t: TFunction<"appShell">): string {
	switch (n.mention_source) {
		case "description":
			return t("notifications.source.description");
		case "document":
			return t("notifications.source.document");
		case "doc_comment":
			return t("notifications.source.docComment");
		default:
			return t("notifications.source.comment");
	}
}

interface NotificationCardProps {
	group: NotificationGroup;
	onOpen: (n: Notification) => void;
	/** Larger text and no line clamps, for the /notifications page. */
	roomy?: boolean;
}

/** One entry of the bell and of the /notifications page. */
export function NotificationCard({
	group,
	onOpen,
	roomy,
}: NotificationCardProps) {
	const { t } = useTranslation("appShell");
	const { t: tCommon } = useTranslation("common");
	const n = group.head;
	const unread = group.items.some((x) => !x.read_at);
	const rich = isTaskMention(n);
	const count = group.items.length;

	let body: ReactNode;
	if (rich) {
		body = (
			<>
				<p className={`text-sm leading-snug ${unread ? "font-medium" : ""}`}>
					{count > 1
						? t("notifications.groupedMentions", {
								n: count,
								taskNumber: n.task_number,
								taskTitle: n.task_title,
							})
						: t("notifications.mentionedYou", { actor: n.actor_full_name })}
				</p>
				<div className="mt-1 flex flex-wrap items-center gap-1">
					<span className={chip}>{n.project_name}</span>
					<span className={chip} data-testid="sprint-chip">
						{n.sprint_id ? n.sprint_name : t("notifications.backlog")}
					</span>
					<span className={chip}>
						#{n.task_number} {n.task_title}
					</span>
					{n.task_status_name && (
						<span className={chip}>
							<span
								aria-hidden="true"
								className="size-1.5 rounded-full bg-muted-foreground"
								style={
									n.task_status_color
										? { backgroundColor: n.task_status_color }
										: undefined
								}
							/>
							{n.task_status_name}
						</span>
					)}
					<span className={chip}>{sourceLabel(n, t)}</span>
				</div>
				{n.excerpt && count === 1 && (
					<p
						className={`mt-1 text-xs text-muted-foreground ${roomy ? "" : "line-clamp-2"}`}
					>
						<HighlightedExcerpt text={n.excerpt} />
					</p>
				)}
				<p className="mt-1 text-xs text-muted-foreground">
					{timeAgo(n.created_at, tCommon)}
				</p>
			</>
		);
	} else {
		body = (
			<>
				<p className={`text-sm leading-snug ${unread ? "font-medium" : ""}`}>
					{notificationText(n, t)}
				</p>
				<p className="mt-0.5 truncate text-xs text-muted-foreground">
					{n.project_name || t("notifications.globalChat")} ·{" "}
					{timeAgo(n.created_at, tCommon)}
				</p>
			</>
		);
	}

	return (
		<button
			type="button"
			onClick={() => onOpen(n)}
			className={`flex w-full items-start gap-3 px-4 py-3 text-left transition-colors hover:bg-muted/50 ${unread ? "bg-primary/5" : ""}`}
		>
			{n.type === "automation" ? (
				<span
					aria-hidden="true"
					className="mt-0.5 flex size-8 shrink-0 items-center justify-center rounded-full bg-secondary text-secondary-foreground"
				>
					<Zap className="size-4" />
				</span>
			) : (
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
			)}
			<div className="min-w-0 flex-1">{body}</div>
			{unread && (
				<span
					aria-hidden="true"
					className="mt-2 h-1.5 w-1.5 shrink-0 rounded-full bg-primary"
				/>
			)}
		</button>
	);
}
