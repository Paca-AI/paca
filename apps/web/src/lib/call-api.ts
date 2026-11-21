import { queryOptions } from "@tanstack/react-query";
import { apiClient } from "./api-client";
import type { SuccessEnvelope } from "./api-error";
import { DM_SCOPE } from "./chat-api";

// Mirrors services/api/internal/transport/http/dto/call_dto.go.

export interface CallParticipant {
	account_id: string;
	name: string;
	kind: "user" | "agent" | "service_account";
	joined_at: string;
}

/** The open call of a channel. No participants means nobody is in it. */
export interface Call {
	channel_id: string;
	started_at: string | null;
	participants: CallParticipant[];
}

/** What a client connects to a call with. */
export interface CallJoin {
	url: string;
	token: string;
	room: string;
	expires_at: string;
}

export const callKeys = {
	config: () => ["calls", "config"] as const,
	project: (projectId: string) => ["calls", projectId, "list"] as const,
	channel: (projectId: string, channelId: string) =>
		["calls", projectId, "channel", channelId] as const,
};

const base = (scope: string, channelId: string) =>
	scope === DM_SCOPE
		? `/chat/dms/${channelId}/call`
		: `/projects/${scope}/channels/${channelId}/call`;

/** Whether this deployment has calls switched on (LiveKit configured). */
export const callConfigQueryOptions = queryOptions({
	queryKey: callKeys.config(),
	queryFn: async (): Promise<boolean> => {
		const { data } =
			await apiClient.instance.get<SuccessEnvelope<{ enabled: boolean }>>(
				"/chat/calls/config",
			);
		return data.data.enabled;
	},
	staleTime: 5 * 60_000,
});

/** The open calls of a project's channels the caller may read. */
export const projectCallsQueryOptions = (projectId: string, enabled = true) =>
	queryOptions({
		queryKey: callKeys.project(projectId),
		queryFn: async (): Promise<Call[]> => {
			const { data } = await apiClient.instance.get<SuccessEnvelope<Call[]>>(
				`/projects/${projectId}/calls`,
			);
			return data.data;
		},
		enabled,
		staleTime: 10_000,
		// A missed announcement costs a stale list for at most this long.
		refetchInterval: 20_000,
	});

export const channelCallQueryOptions = (
	scope: string,
	channelId: string,
	enabled = true,
) =>
	queryOptions({
		queryKey: callKeys.channel(scope, channelId),
		queryFn: async (): Promise<Call> => {
			const { data } = await apiClient.instance.get<SuccessEnvelope<Call>>(
				base(scope, channelId),
			);
			return data.data;
		},
		enabled,
		staleTime: 5_000,
		refetchInterval: 20_000,
	});

export async function joinCall(
	scope: string,
	channelId: string,
): Promise<CallJoin> {
	const { data } = await apiClient.instance.post<SuccessEnvelope<CallJoin>>(
		`${base(scope, channelId)}/join`,
	);
	return data.data;
}

/** Mutes everything the participant publishes (needs calls:moderate). */
export async function muteParticipant(
	projectId: string,
	channelId: string,
	accountId: string,
): Promise<void> {
	await apiClient.instance.post(
		`${base(projectId, channelId)}/participants/${accountId}/mute`,
	);
}

/** Removes the participant from the call (needs calls:moderate). */
export async function removeParticipant(
	projectId: string,
	channelId: string,
	accountId: string,
): Promise<void> {
	await apiClient.instance.delete(
		`${base(projectId, channelId)}/participants/${accountId}`,
	);
}
