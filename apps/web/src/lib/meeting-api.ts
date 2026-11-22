import { queryOptions } from "@tanstack/react-query";
import { apiClient } from "./api-client";
import type { SuccessEnvelope } from "./api-error";

// Mirrors services/api/internal/transport/http/dto/meeting_dto.go.

export type MeetingMode = "on_demand" | "auto_suggest" | "notes_only";
export type SummaryTarget = "none" | "channel_message";
export type DefaultTarget = "requester" | "host";

/** The transcription providers the worker supports (the settings response lists
 * the same; the create dialog needs them before an agent exists). */
export const STT_PROVIDERS = ["openai", "deepgram"] as const;

/** The instructions a new meeting assistant starts with (the API fills the same
 * text in for an agent created without any). People edit them freely. */
export const DEFAULT_MEETING_PROMPT = `You are the meeting assistant for this project team. You listen to calls through their transcript and you never speak.
- Write the notes accurately and briefly: what was decided, what is still open, and who agreed to do what.
- Use the speakers' own words and names. Do not invent details, owners or dates that were not said.
- When suggesting tasks, suggest only concrete action items someone clearly committed to or asked for. Give each a short imperative title and one line of context.
- Do not suggest a task for a topic that was only discussed, or one that already exists.
- Write in the language the team speaks.`;

export const PROPOSAL_KINDS = [
	"task.create",
	"task.update",
	"task.comment",
	"doc.create_from_notes",
] as const;
export type ProposalKind = (typeof PROPOSAL_KINDS)[number];

/** A meeting agent's listening and behaviour settings. The transcription key is
 * never returned, only whether one is stored. */
export interface MeetingSettings {
	stt_provider: string;
	stt_model: string;
	has_stt_key: boolean;
	stt_base_url: string;
	stt_languages: string[];
	mode: MeetingMode;
	suggest_interval_seconds: number;
	proposal_kinds: ProposalKind[];
	max_proposals_per_call: number;
	store_transcript: boolean;
	transcript_retention_days: number;
	announcement: string;
	summary_target: SummaryTarget;
	default_target: DefaultTarget;
	updated_at: string;
	/** The transcription providers the worker supports. */
	stt_providers: string[];
}

export type MeetingSettingsPatch = Partial<
	Omit<MeetingSettings, "has_stt_key" | "updated_at" | "stt_providers"> & {
		/** "" clears the stored key. */
		stt_api_key: string;
	}
>;

/** A meeting agent in the invite picker. */
export interface MeetingAgent {
	id: string;
	name: string;
	handle: string;
	description: string;
	mode: MeetingMode;
	summary_target: SummaryTarget;
	store_transcript: boolean;
	transcript_retention_days: number;
}

/** An assistant in a call. */
export interface CallAssistant {
	agent_id: string;
	name: string;
	status: "dispatched" | "active" | "left" | "failed";
	invited_by: string | null;
	joined_at: string;
	mode: MeetingMode;
	summary_target: SummaryTarget;
	store_transcript: boolean;
	transcript_retention_days: number;
	announcement: string;
}

export type ProposalStatus =
	| "pending"
	| "claimed"
	| "resolved"
	| "rejected"
	| "expired"
	| "failed";

/** What anyone who can read the channel sees of a proposal: never its fields. */
export interface ProposalChip {
	id: string;
	channel_id: string | null;
	agent_id: string;
	agent_name: string;
	target_account_id: string;
	kind: ProposalKind;
	status: ProposalStatus;
	expires_at: string;
	created_at: string;
}

/** A proposal with the fields that will be written (only the person it is for). */
export interface Proposal extends ProposalChip {
	call_session_id: string | null;
	payload: Record<string, unknown>;
	result: Record<string, unknown>;
	resolved_by: string | null;
}

/** A call of a channel that was transcribed. */
export interface Meeting {
	call_id: string;
	started_at: string;
	/** null while the call is still open. */
	ended_at: string | null;
	segments: number;
	speakers: string[];
}

/** One turn of a transcript, with who said it. */
export interface TranscriptLine {
	speaker_id: string | null;
	speaker_name: string;
	text: string;
	spoken_at: string;
}

export const meetingKeys = {
	meetings: (projectId: string, channelId: string) =>
		["meeting", projectId, "meetings", channelId] as const,
	transcript: (projectId: string, channelId: string, callId: string) =>
		["meeting", projectId, "transcript", channelId, callId] as const,
	config: () => ["meeting", "config"] as const,
	settings: (projectId: string, agentId: string) =>
		["meeting", projectId, "settings", agentId] as const,
	agents: (projectId: string) => ["meeting", projectId, "agents"] as const,
	assistants: (projectId: string, channelId: string) =>
		["meeting", projectId, "assistants", channelId] as const,
	chips: (projectId: string, channelId: string) =>
		["meeting", projectId, "chips", channelId] as const,
	proposal: (projectId: string, id: string) =>
		["meeting", projectId, "proposal", id] as const,
};

/** Whether assistants can run on this deployment (calls on, worker secret set). */
export const meetingConfigQueryOptions = queryOptions({
	queryKey: meetingKeys.config(),
	queryFn: async (): Promise<boolean> => {
		const { data } = await apiClient.instance.get<
			SuccessEnvelope<{ enabled: boolean }>
		>("/chat/meeting/config");
		return data.data.enabled;
	},
	staleTime: 5 * 60_000,
});

export const meetingSettingsQueryOptions = (
	projectId: string,
	agentId: string,
) =>
	queryOptions({
		queryKey: meetingKeys.settings(projectId, agentId),
		queryFn: async (): Promise<MeetingSettings> => {
			const { data } = await apiClient.instance.get<
				SuccessEnvelope<MeetingSettings>
			>(`/projects/${projectId}/agents/${agentId}/meeting-settings`);
			return data.data;
		},
	});

export async function updateMeetingSettings(
	projectId: string,
	agentId: string,
	patch: MeetingSettingsPatch,
): Promise<MeetingSettings> {
	const { data } = await apiClient.instance.patch<
		SuccessEnvelope<MeetingSettings>
	>(`/projects/${projectId}/agents/${agentId}/meeting-settings`, patch);
	return data.data;
}

/** The meeting agents the caller may invite into a call. */
export const meetingAgentsQueryOptions = (projectId: string, enabled = true) =>
	queryOptions({
		queryKey: meetingKeys.agents(projectId),
		queryFn: async (): Promise<MeetingAgent[]> => {
			const { data } = await apiClient.instance.get<
				SuccessEnvelope<MeetingAgent[]>
			>(`/projects/${projectId}/meeting-agents`);
			return data.data;
		},
		enabled,
		staleTime: 30_000,
	});

const callBase = (projectId: string, channelId: string) =>
	`/projects/${projectId}/channels/${channelId}/call`;

export const callAssistantsQueryOptions = (
	projectId: string,
	channelId: string,
	enabled = true,
) =>
	queryOptions({
		queryKey: meetingKeys.assistants(projectId, channelId),
		queryFn: async (): Promise<CallAssistant[]> => {
			const { data } = await apiClient.instance.get<
				SuccessEnvelope<CallAssistant[]>
			>(`${callBase(projectId, channelId)}/assistants`);
			return data.data;
		},
		enabled,
		staleTime: 5_000,
		// A missed announcement costs a stale list for at most this long.
		refetchInterval: 20_000,
	});

export async function inviteAssistant(
	projectId: string,
	channelId: string,
	agentId: string,
): Promise<CallAssistant> {
	const { data } = await apiClient.instance.post<
		SuccessEnvelope<CallAssistant>
	>(`${callBase(projectId, channelId)}/assistants/${agentId}`);
	return data.data;
}

export async function removeAssistant(
	projectId: string,
	channelId: string,
	agentId: string,
): Promise<void> {
	await apiClient.instance.delete(
		`${callBase(projectId, channelId)}/assistants/${agentId}`,
	);
}

export const channelProposalsQueryOptions = (
	projectId: string,
	channelId: string,
	enabled = true,
) =>
	queryOptions({
		queryKey: meetingKeys.chips(projectId, channelId),
		queryFn: async (): Promise<ProposalChip[]> => {
			const { data } = await apiClient.instance.get<
				SuccessEnvelope<ProposalChip[]>
			>(`/projects/${projectId}/channels/${channelId}/proposals`);
			return data.data;
		},
		enabled,
		staleTime: 5_000,
		refetchInterval: 30_000,
	});

export const proposalQueryOptions = (
	projectId: string,
	id: string,
	enabled = true,
) =>
	queryOptions({
		queryKey: meetingKeys.proposal(projectId, id),
		queryFn: async (): Promise<Proposal> => {
			const { data } = await apiClient.instance.get<SuccessEnvelope<Proposal>>(
				`/projects/${projectId}/proposals/${id}`,
			);
			return data.data;
		},
		enabled,
		staleTime: 5_000,
	});

export async function claimProposal(
	projectId: string,
	id: string,
): Promise<Proposal> {
	const { data } = await apiClient.instance.post<SuccessEnvelope<Proposal>>(
		`/projects/${projectId}/proposals/${id}/claim`,
	);
	return data.data;
}

export async function resolveProposal(
	projectId: string,
	id: string,
	status: "resolved" | "rejected" | "failed",
	result?: Record<string, unknown>,
): Promise<Proposal> {
	const { data } = await apiClient.instance.post<SuccessEnvelope<Proposal>>(
		`/projects/${projectId}/proposals/${id}/resolve`,
		{ status, result },
	);
	return data.data;
}

const meetingsBase = (projectId: string, channelId: string) =>
	`/projects/${projectId}/channels/${channelId}/meetings`;

/** The calls of a channel that were transcribed, newest first. */
export const channelMeetingsQueryOptions = (
	projectId: string,
	channelId: string,
	enabled = true,
	live = false,
) =>
	queryOptions({
		queryKey: meetingKeys.meetings(projectId, channelId),
		queryFn: async (): Promise<Meeting[]> => {
			const { data } = await apiClient.instance.get<SuccessEnvelope<Meeting[]>>(
				meetingsBase(projectId, channelId),
			);
			return data.data;
		},
		enabled,
		staleTime: 10_000,
		refetchInterval: live ? 5_000 : false,
	});

export const meetingTranscriptQueryOptions = (
	projectId: string,
	channelId: string,
	callId: string,
	live = false,
) =>
	queryOptions({
		queryKey: meetingKeys.transcript(projectId, channelId, callId),
		queryFn: async (): Promise<TranscriptLine[]> => {
			const { data } = await apiClient.instance.get<
				SuccessEnvelope<TranscriptLine[]>
			>(`${meetingsBase(projectId, channelId)}/${callId}/transcript`);
			return data.data;
		},
		// A call that is still going gets new lines every few seconds.
		refetchInterval: live ? 5_000 : false,
	});

/** Fetches a transcript as a plain-text file and hands it to the browser to save. */
export async function downloadTranscript(
	projectId: string,
	channelId: string,
	callId: string,
): Promise<void> {
	const { data } = await apiClient.instance.get<string>(
		`${meetingsBase(projectId, channelId)}/${callId}/transcript`,
		{ params: { format: "txt" }, responseType: "text" },
	);
	const url = URL.createObjectURL(new Blob([data], { type: "text/plain" }));
	const a = document.createElement("a");
	a.href = url;
	a.download = `transcript-${callId.slice(0, 8)}.txt`;
	a.click();
	URL.revokeObjectURL(url);
}
