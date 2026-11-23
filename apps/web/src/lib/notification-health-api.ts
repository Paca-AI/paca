import { queryOptions } from "@tanstack/react-query";

import { apiClient } from "./api-client";
import type { SuccessEnvelope } from "./api-error";

export type HealthStatus = "ok" | "degraded" | "failing";

export interface DeliveryChannelHealth {
	channel: "realtime" | "push" | "email";
	pending: number;
	retrying: number;
	overdue: number;
	oldest_overdue: string | null;
	sent: number;
	failed: number;
	sent_last_hour: number;
	failed_last_hour: number;
	/** Creation-to-delivery seconds over the last hour; null when nothing was sent. */
	latency_p50_seconds: number | null;
	latency_p95_seconds: number | null;
	top_errors: { error: string; count: number }[];
}

export interface StreamHealth {
	stream: string;
	group: string;
	pending: number;
	oldest_pending_idle_seconds: number;
	dead_last_hour: number;
	dead_total: number;
}

export interface NotificationHealth {
	status: HealthStatus;
	reasons: string[];
	checked_at: string;
	channels: DeliveryChannelHealth[];
	streams: StreamHealth[];
}

/** How often the admin page re-reads the health while it is open. */
export const NOTIFICATION_HEALTH_REFRESH_MS = 30_000;

export const notificationHealthQueryOptions = queryOptions({
	queryKey: ["admin", "notification-health"],
	queryFn: async () => {
		const { data } = await apiClient.instance.get<
			SuccessEnvelope<NotificationHealth>
		>("/admin/notifications/health");
		return data.data;
	},
	refetchInterval: NOTIFICATION_HEALTH_REFRESH_MS,
});
