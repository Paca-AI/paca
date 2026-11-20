import { infiniteQueryOptions, queryOptions } from "@tanstack/react-query";

import { apiClient } from "./api-client";
import type { SuccessEnvelope } from "./api-error";

// ── Shapes ────────────────────────────────────────────────────────────────────

export type NotificationType =
	| "assigned"
	| "mentioned"
	| "question"
	| "automation";

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
	actor_agent_cli_provider?: string | null;
	task_id: string | null;
	task_title: string;
	task_number: number;
	// Null for a question asked in a global-chat conversation.
	project_id: string | null;
	project_name: string;
	// Set only for a mention in a document comment.
	document_id: string | null;
	document_title: string;
	// Set only for "question" notifications.
	conversation_id: string | null;
	conversation_title: string;
	// Text of an "automation" notification (sent by a notify action).
	message: string;
	// A mention: the comment it points at (null for a description or a
	// document body), where it was made, and the stored start of the text.
	comment_id: string | null;
	mention_source: "" | "comment" | "description" | "document" | "doc_comment";
	excerpt: string;
	// Where the task is now (joined when listed). sprint_id is null in the backlog.
	task_status_name: string;
	task_status_color: string;
	sprint_id: string | null;
	sprint_name: string;
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

// ── Mentions in context ───────────────────────────────────────────────────────

export interface NotificationFilter {
	/** Notification types to keep (empty: all). */
	types?: NotificationType[];
	projectId?: string;
	/** Only notifications caused by an agent. */
	agentActor?: boolean;
}

export type NotificationTab = "all" | "mentions" | "assigned" | "agents";

/** The server-side filter each tab of the /notifications page stands for. */
export function filterForTab(
	tab: NotificationTab,
	projectId?: string,
): NotificationFilter {
	const f: NotificationFilter = { projectId };
	if (tab === "mentions") f.types = ["mentioned"];
	if (tab === "assigned") f.types = ["assigned"];
	if (tab === "agents") f.agentActor = true;
	return f;
}

/** The filtered, paginated list behind the /notifications page. */
export const filteredNotificationsQueryOptions = (filter: NotificationFilter) =>
	infiniteQueryOptions({
		queryKey: ["notifications", "list", filter],
		queryFn: async ({ pageParam }: { pageParam: string | undefined }) => {
			const { data } = await apiClient.instance.get<
				SuccessEnvelope<NotificationListResponse>
			>("/users/me/notifications", {
				params: {
					cursor: pageParam,
					page_size: NOTIFICATIONS_PAGE_SIZE,
					type: filter.types?.join(",") || undefined,
					project_id: filter.projectId,
					actor: filter.agentActor ? "agent" : undefined,
				},
			});
			return data.data;
		},
		initialPageParam: undefined as string | undefined,
		getNextPageParam: (lastPage) => lastPage.next_cursor ?? undefined,
		staleTime: 30_000,
	});

export interface UnreadMention {
	project_id: string;
	task_id: string;
	sprint_id: string | null;
	count: number;
}

/** The caller's unread mentions per task: badges on task cards and sprint
 *  headers. Invalidated with ["notifications"] whenever one arrives or is read. */
export const unreadMentionsQueryOptions = (projectId?: string) =>
	queryOptions({
		queryKey: ["notifications", "unread-mentions", projectId ?? null],
		queryFn: async () => {
			const { data } = await apiClient.instance.get<
				SuccessEnvelope<UnreadMention[]>
			>("/users/me/notifications/unread-mentions", {
				params: { project_id: projectId },
			});
			return data.data;
		},
		staleTime: 30_000,
	});

/** The caller opened a task: the server remembers when and reads their
 *  mentions on it. Resolves to the time of the previous visit (null the first
 *  time), which the activity pane draws its "unread since" divider from. */
export async function openTask(
	projectId: string,
	taskId: string,
): Promise<string | null> {
	const { data } = await apiClient.instance.put<
		SuccessEnvelope<{ previous_seen_at: string | null }>
	>(`/projects/${projectId}/tasks/${taskId}/read`);
	return data.data.previous_seen_at;
}

/** Where a notification's card sends you. */
export function notificationTarget(
	n: Notification,
):
	| { kind: "task"; projectId: string; taskId: string; commentId?: string }
	| { kind: "doc"; projectId: string; docId: string }
	| { kind: "conversation"; projectId: string | null; conversationId: string }
	| { kind: "project"; projectId: string }
	| null {
	if (n.type === "question" && n.conversation_id) {
		return {
			kind: "conversation",
			projectId: n.project_id,
			conversationId: n.conversation_id,
		};
	}
	if (!n.project_id) return null;
	if (n.document_id) {
		return { kind: "doc", projectId: n.project_id, docId: n.document_id };
	}
	if (n.task_id) {
		return {
			kind: "task",
			projectId: n.project_id,
			taskId: n.task_id,
			commentId: n.comment_id ?? undefined,
		};
	}
	return { kind: "project", projectId: n.project_id };
}
