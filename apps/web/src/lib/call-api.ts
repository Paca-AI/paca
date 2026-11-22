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

/** A recording of a call (a LiveKit egress writing one MP4). */
export interface CallRecording {
	id: string;
	call_id: string;
	status: "starting" | "recording" | "stopping" | "complete" | "failed";
	started_by: string | null;
	started_at: string;
	ended_at: string | null;
	duration_ms: number;
	size_bytes: number;
	error: string;
}

/** The open call of a channel. No participants means nobody is in it. */
export interface Call {
	channel_id: string;
	started_at: string | null;
	participants: CallParticipant[];
	/** Set while the call is being recorded: everyone in it is told. */
	recording?: CallRecording | null;
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

/** What this deployment can do with calls: whether they are on at all, and
 * whether they can be recorded (LiveKit egress and storage are set up). */
export const callFeaturesQueryOptions = queryOptions({
	queryKey: [...callKeys.config(), "features"] as const,
	queryFn: async (): Promise<{ enabled: boolean; recording: boolean }> => {
		const { data } =
			await apiClient.instance.get<
				SuccessEnvelope<{ enabled: boolean; recording?: boolean }>
			>("/chat/calls/config");
		return {
			enabled: data.data.enabled,
			recording: data.data.recording === true,
		};
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

/** Starts recording the call (needs calls:record). Everyone in it sees that. */
export async function startRecording(
	projectId: string,
	channelId: string,
): Promise<CallRecording> {
	const { data } = await apiClient.instance.post<
		SuccessEnvelope<CallRecording>
	>(`${base(projectId, channelId)}/recording`);
	return data.data;
}

/** Stops the recording; the file is finalised and appears in the meeting history. */
export async function stopRecording(
	projectId: string,
	channelId: string,
): Promise<void> {
	await apiClient.instance.delete(`${base(projectId, channelId)}/recording`);
}

export const channelRecordingsQueryOptions = (
	projectId: string,
	channelId: string,
	enabled = true,
) =>
	queryOptions({
		queryKey: ["calls", projectId, "recordings", channelId] as const,
		queryFn: async (): Promise<CallRecording[]> => {
			const { data } = await apiClient.instance.get<
				SuccessEnvelope<CallRecording[]>
			>(`/projects/${projectId}/channels/${channelId}/recordings`);
			return data.data;
		},
		enabled,
		staleTime: 10_000,
	});

/** A short-lived link to play (or, with download, save) a finished recording. */
export async function recordingUrl(
	projectId: string,
	channelId: string,
	recordingId: string,
	download = false,
): Promise<string> {
	const { data } = await apiClient.instance.get<
		SuccessEnvelope<{ url: string }>
	>(
		`/projects/${projectId}/channels/${channelId}/recordings/${recordingId}/url`,
		{
			params: download ? { download: 1 } : undefined,
		},
	);
	return data.data.url;
}
