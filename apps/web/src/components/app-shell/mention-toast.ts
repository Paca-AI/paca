import type { useNavigate } from "@tanstack/react-router";
import { toast } from "@/components/ui/toast";
import i18n from "@/i18n";
import { getNotifications } from "@/lib/notification-api";
import { openNotificationTarget } from "./notification-bell";

/** A mention arrived while you are online: a toast with the same context the
 *  notification card shows (project, sprint, task, excerpt). The realtime
 *  event carries only ids, so the notification is read back from the API. */
export async function showMentionToast(
	notificationId: string,
	navigate: ReturnType<typeof useNavigate>,
): Promise<void> {
	let page: Awaited<ReturnType<typeof getNotifications>>;
	try {
		page = await getNotifications({ pageSize: 10 });
	} catch {
		return;
	}
	const n = page.items.find((x) => x.id === notificationId);
	if (!n?.task_id || n.document_id) return;
	const t = i18n.getFixedT(null, "appShell");
	const where = [
		n.project_name,
		n.sprint_id ? n.sprint_name : t("notifications.backlog"),
	]
		.filter(Boolean)
		.join(" · ");
	toast.add({
		type: "info",
		title: t("notifications.toast.mentioned", {
			actor: n.actor_full_name,
			taskNumber: n.task_number,
			taskTitle: n.task_title,
		}),
		description: [where, n.excerpt].filter(Boolean).join("\n"),
		timeout: 8000,
		actionProps: {
			children: t("notifications.toast.open"),
			onClick: () => {
				openNotificationTarget(navigate, n);
			},
		},
	});
}
