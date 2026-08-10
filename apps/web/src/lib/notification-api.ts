import { infiniteQueryOptions } from "@tanstack/react-query";

import { apiClient } from "./api-client";
import type { SuccessEnvelope } from "./api-error";

// ── Shapes ────────────────────────────────────────────────────────────────────

export type NotificationType = "assigned" | "mentioned";

export interface Notification {
	id: string;
	type: NotificationType;
	actor_full_name: string;
	actor_username: string;
	actor_avatar_url?: string | null;
	actor_avatar_thumb_url?: string | null;
	// Only meaningful when actor_member_type is "agent" — used to pick a
	// default provider-logo avatar when the actor has no avatar uploaded.
	// See lib/provider-logos.ts.
	actor_member_type?: string; // "human" | "agent"
	actor_agent_type?: string; // "llm" | "acp"
	actor_agent_llm_provider?: string;
	actor_agent_acp_provider?: string | null;
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
	page_size: number;
	next_cursor: string | null;
	unread_count: number;
}

// Shared between the bell badge (notification-bell.tsx) and the browser tab
// title (hooks/use-document-title.ts) so both surfaces agree on where an
// exact unread count gives way to a "+" suffix.
export const MAX_DISPLAYED_UNREAD_COUNT = 9;

export const NOTIFICATIONS_PAGE_SIZE = 20;

// ── API calls ─────────────────────────────────────────────────────────────────

export async function getNotifications(
	opts: { cursor?: string; pageSize?: number } = {},
): Promise<NotificationListResponse> {
	const { data } = await apiClient.instance.get<
		SuccessEnvelope<NotificationListResponse>
	>("/users/me/notifications", {
		params: {
			cursor: opts.cursor,
			page_size: opts.pageSize ?? NOTIFICATIONS_PAGE_SIZE,
		},
	});
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

// Cursor-paginated: the notification bell loads NOTIFICATIONS_PAGE_SIZE at a
// time and fetches another page as the popover list is scrolled near its
// bottom (see notification-bell.tsx / lib/scroll-pagination.ts). Invalidating
// ["notifications"] (see routes/_authenticated.tsx's realtime listener and
// the mark-as-read/mark-all-as-read mutations below) refetches every
// currently-loaded page, same as any other infinite query in the app.
export const notificationsQueryOptions = infiniteQueryOptions({
	queryKey: ["notifications"],
	queryFn: ({ pageParam }: { pageParam: string | undefined }) =>
		getNotifications({ cursor: pageParam }),
	initialPageParam: undefined as string | undefined,
	getNextPageParam: (lastPage) => lastPage.next_cursor ?? undefined,
	staleTime: 30_000,
});
