import { queryOptions } from "@tanstack/react-query";

import { apiClient } from "./api-client";
import type { SuccessEnvelope } from "./api-error";

// ── Shapes ────────────────────────────────────────────────────────────────────

export type NotificationType = "assigned" | "mentioned";

export interface Notification {
	id: string;
	type: NotificationType;
	actor_full_name: string;
	actor_username: string;
	task_id: string | null;
	task_title: string;
	task_number: number;
	project_id: string;
	project_name: string;
	read_at: string | null;
	created_at: string;
}

export interface NotificationListResponse {
	items: Notification[];
	unread_count: number;
}

// Shared between the bell badge (notification-bell.tsx) and the browser tab
// title (hooks/use-document-title.ts) so both surfaces agree on where an
// exact unread count gives way to a "+" suffix.
export const MAX_DISPLAYED_UNREAD_COUNT = 9;

// ── API calls ─────────────────────────────────────────────────────────────────

export async function getNotifications(): Promise<NotificationListResponse> {
	const { data } = await apiClient.instance.get<
		SuccessEnvelope<NotificationListResponse>
	>("/users/me/notifications");
	return data.data;
}

export async function markNotificationAsRead(
	notificationId: string,
): Promise<void> {
	await apiClient.instance.patch(
		`/users/me/notifications/${notificationId}/read`,
	);
}

export async function markAllNotificationsAsRead(): Promise<void> {
	await apiClient.instance.post("/users/me/notifications/read-all");
}

// ── Query options ─────────────────────────────────────────────────────────────

export const notificationsQueryOptions = queryOptions({
	queryKey: ["notifications"],
	queryFn: getNotifications,
	staleTime: 30_000,
});
