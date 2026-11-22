package dto

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"

	meetingdom "github.com/Paca-AI/api/internal/domain/meeting"
)

// MeetingSettingsResponse is a meeting agent's settings. The transcription key
// is never returned, only whether one is stored.
type MeetingSettingsResponse struct {
	STTProvider             string    `json:"stt_provider"`
	STTModel                string    `json:"stt_model"`
	HasSTTKey               bool      `json:"has_stt_key"`
	STTBaseURL              string    `json:"stt_base_url"`
	STTLanguages            []string  `json:"stt_languages"`
	Mode                    string    `json:"mode"`
	SuggestIntervalSeconds  int       `json:"suggest_interval_seconds"`
	ProposalKinds           []string  `json:"proposal_kinds"`
	MaxProposalsPerCall     int       `json:"max_proposals_per_call"`
	StoreTranscript         bool      `json:"store_transcript"`
	TranscriptRetentionDays int       `json:"transcript_retention_days"`
	Announcement            string    `json:"announcement"`
	SummaryTarget           string    `json:"summary_target"`
	DefaultTarget           string    `json:"default_target"`
	UpdatedAt               time.Time `json:"updated_at"`
	// STTProviders lists the transcription providers the worker supports.
	STTProviders []string `json:"stt_providers"`
}

// MeetingSettingsFromEntity converts settings.
func MeetingSettingsFromEntity(s *meetingdom.Settings, providers []string) MeetingSettingsResponse {
	return MeetingSettingsResponse{
		STTProvider: s.STTProvider, STTModel: s.STTModel, HasSTTKey: s.HasSTTKey(), STTBaseURL: s.STTBaseURL,
		STTLanguages: s.STTLanguages, Mode: s.Mode, SuggestIntervalSeconds: s.SuggestIntervalSeconds,
		ProposalKinds: s.ProposalKinds, MaxProposalsPerCall: s.MaxProposalsPerCall, StoreTranscript: s.StoreTranscript,
		TranscriptRetentionDays: s.TranscriptRetentionDays, Announcement: s.Announcement, SummaryTarget: s.SummaryTarget,
		DefaultTarget: s.DefaultTarget, UpdatedAt: s.UpdatedAt, STTProviders: providers,
	}
}

// UpdateMeetingSettingsRequest changes some settings. Omitted fields stay as
// they are; stt_api_key "" clears the stored key.
type UpdateMeetingSettingsRequest struct {
	STTProvider             *string   `json:"stt_provider"`
	STTModel                *string   `json:"stt_model"`
	STTAPIKey               *string   `json:"stt_api_key"`
	STTBaseURL              *string   `json:"stt_base_url"`
	STTLanguages            *[]string `json:"stt_languages"`
	Mode                    *string   `json:"mode"`
	SuggestIntervalSeconds  *int      `json:"suggest_interval_seconds"`
	ProposalKinds           *[]string `json:"proposal_kinds"`
	MaxProposalsPerCall     *int      `json:"max_proposals_per_call"`
	StoreTranscript         *bool     `json:"store_transcript"`
	TranscriptRetentionDays *int      `json:"transcript_retention_days"`
	Announcement            *string   `json:"announcement"`
	SummaryTarget           *string   `json:"summary_target"`
	DefaultTarget           *string   `json:"default_target"`
}

// Patch converts the request.
func (r UpdateMeetingSettingsRequest) Patch() meetingdom.SettingsPatch {
	return meetingdom.SettingsPatch{
		STTProvider: r.STTProvider, STTModel: r.STTModel, STTAPIKey: r.STTAPIKey, STTBaseURL: r.STTBaseURL,
		STTLanguages: r.STTLanguages, Mode: r.Mode, SuggestIntervalSeconds: r.SuggestIntervalSeconds,
		ProposalKinds: r.ProposalKinds, MaxProposalsPerCall: r.MaxProposalsPerCall, StoreTranscript: r.StoreTranscript,
		TranscriptRetentionDays: r.TranscriptRetentionDays, Announcement: r.Announcement,
		SummaryTarget: r.SummaryTarget, DefaultTarget: r.DefaultTarget,
	}
}

// MeetingAgentResponse is a meeting agent in the invite picker, with the
// privacy summary shown before it is invited.
type MeetingAgentResponse struct {
	ID              uuid.UUID `json:"id"`
	Name            string    `json:"name"`
	Handle          string    `json:"handle"`
	Description     string    `json:"description"`
	Mode            string    `json:"mode"`
	SummaryTarget   string    `json:"summary_target"`
	StoreTranscript bool      `json:"store_transcript"`
	RetentionDays   int       `json:"transcript_retention_days"`
}

// MeetingAgentFromEntity converts a picker row.
func MeetingAgentFromEntity(a *meetingdom.MeetingAgent) MeetingAgentResponse {
	return MeetingAgentResponse{ID: a.ID, Name: a.Name, Handle: a.Handle, Description: a.Description,
		Mode: a.Mode, SummaryTarget: a.SummaryTarget, StoreTranscript: a.StoreTranscript, RetentionDays: a.RetentionDays}
}

// CallAssistantResponse is an assistant in a call, with what it does.
type CallAssistantResponse struct {
	AgentID         uuid.UUID  `json:"agent_id"`
	Name            string     `json:"name"`
	Status          string     `json:"status"`
	InvitedBy       *uuid.UUID `json:"invited_by"`
	JoinedAt        time.Time  `json:"joined_at"`
	Mode            string     `json:"mode"`
	SummaryTarget   string     `json:"summary_target"`
	StoreTranscript bool       `json:"store_transcript"`
	RetentionDays   int        `json:"transcript_retention_days"`
	Announcement    string     `json:"announcement"`
}

// CallAssistantFromEntity converts an agent session.
func CallAssistantFromEntity(s *meetingdom.AgentSession) CallAssistantResponse {
	return CallAssistantResponse{AgentID: s.AgentID, Name: s.AgentName, Status: s.Status, InvitedBy: s.InvitedBy,
		JoinedAt: s.JoinedAt, Mode: s.Mode, SummaryTarget: s.SummaryTarget, StoreTranscript: s.StoreTranscript, RetentionDays: s.RetentionDays,
		Announcement: s.Announcement}
}

// ProposalChipResponse is what anyone who can read the channel sees of a
// proposal: that it exists and where it stands, never its fields.
type ProposalChipResponse struct {
	ID              uuid.UUID  `json:"id"`
	ChannelID       *uuid.UUID `json:"channel_id"`
	AgentID         uuid.UUID  `json:"agent_id"`
	AgentName       string     `json:"agent_name"`
	TargetAccountID uuid.UUID  `json:"target_account_id"`
	Kind            string     `json:"kind"`
	Status          string     `json:"status"`
	ExpiresAt       time.Time  `json:"expires_at"`
	CreatedAt       time.Time  `json:"created_at"`
}

// ProposalChipFromEntity converts a proposal to its chip.
func ProposalChipFromEntity(p *meetingdom.Proposal) ProposalChipResponse {
	return ProposalChipResponse{ID: p.ID, ChannelID: p.ChannelID, AgentID: p.AgentAccountID, AgentName: p.AgentName,
		TargetAccountID: p.TargetAccountID, Kind: p.Kind, Status: p.Status, ExpiresAt: p.ExpiresAt, CreatedAt: p.CreatedAt}
}

// ProposalResponse is a proposal with the fields that will be written. Only
// the person it is for (proposals:resolve with the target condition) gets it.
type ProposalResponse struct {
	ProposalChipResponse
	CallSessionID *uuid.UUID      `json:"call_session_id"`
	Payload       json.RawMessage `json:"payload"`
	Result        json.RawMessage `json:"result"`
	ResolvedBy    *uuid.UUID      `json:"resolved_by"`
}

// ProposalFromEntity converts a proposal.
func ProposalFromEntity(p *meetingdom.Proposal) ProposalResponse {
	return ProposalResponse{ProposalChipResponse: ProposalChipFromEntity(p), CallSessionID: p.CallSessionID,
		Payload: json.RawMessage(p.Payload), Result: json.RawMessage(p.Result), ResolvedBy: p.ResolvedBy}
}

// CreateProposalRequest is what an assistant sends to propose something.
type CreateProposalRequest struct {
	ChannelID uuid.UUID       `json:"channel_id"`
	Kind      string          `json:"kind"`
	Payload   json.RawMessage `json:"payload"`
	// TargetID optionally names who should review (a participant of the call).
	TargetID *uuid.UUID `json:"target_id"`
}

// ResolveProposalRequest records what the person decided.
type ResolveProposalRequest struct {
	Status string          `json:"status"`
	Result json.RawMessage `json:"result"`
}

// MeetingConfigResponse tells a client whether assistants are available.
type MeetingConfigResponse struct {
	Enabled bool `json:"enabled"`
}

// WorkerBootstrapResponse is everything a worker needs to run one job.
type WorkerBootstrapResponse struct {
	SessionID    uuid.UUID  `json:"session_id"`
	ProjectID    uuid.UUID  `json:"project_id"`
	ChannelID    uuid.UUID  `json:"channel_id"`
	CallID       uuid.UUID  `json:"call_id"`
	AgentID      uuid.UUID  `json:"agent_id"`
	AgentName    string     `json:"agent_name"`
	RequesterID  *uuid.UUID `json:"requester_id"`
	HostID       *uuid.UUID `json:"host_id"`
	Instructions string     `json:"instructions"`
	LLM          struct {
		Provider string `json:"provider"`
		Model    string `json:"model"`
		APIKey   string `json:"api_key"`
		BaseURL  string `json:"base_url"`
	} `json:"llm"`
	STT struct {
		Provider  string   `json:"provider"`
		Model     string   `json:"model"`
		APIKey    string   `json:"api_key"`
		BaseURL   string   `json:"base_url"`
		Languages []string `json:"languages"`
	} `json:"stt"`
	Behavior struct {
		Mode                   string   `json:"mode"`
		SuggestIntervalSeconds int      `json:"suggest_interval_seconds"`
		ProposalKinds          []string `json:"proposal_kinds"`
		MaxProposalsPerCall    int      `json:"max_proposals_per_call"`
		StoreTranscript        bool     `json:"store_transcript"`
		Announcement           string   `json:"announcement"`
		SummaryTarget          string   `json:"summary_target"`
	} `json:"behavior"`
	APIKey    string    `json:"api_key"`
	ExpiresAt time.Time `json:"expires_at"`
}

// WorkerBootstrapFromEntity converts a bootstrap.
func WorkerBootstrapFromEntity(b *meetingdom.WorkerBootstrap) WorkerBootstrapResponse {
	r := WorkerBootstrapResponse{SessionID: b.SessionID, ProjectID: b.ProjectID, ChannelID: b.ChannelID, CallID: b.CallID,
		AgentID: b.AgentID, AgentName: b.AgentName, RequesterID: b.RequesterID, HostID: b.HostID,
		Instructions: b.Instructions, APIKey: b.APIKey, ExpiresAt: b.ExpiresAt}
	r.LLM.Provider, r.LLM.Model, r.LLM.APIKey, r.LLM.BaseURL = b.LLMProvider, b.LLMModel, b.LLMAPIKey, b.LLMBaseURL
	s := b.Settings
	r.STT.Provider, r.STT.Model, r.STT.APIKey, r.STT.BaseURL, r.STT.Languages = s.STTProvider, s.STTModel, b.STTAPIKey, s.STTBaseURL, s.STTLanguages
	r.Behavior.Mode, r.Behavior.SuggestIntervalSeconds, r.Behavior.ProposalKinds = s.Mode, s.SuggestIntervalSeconds, s.ProposalKinds
	r.Behavior.MaxProposalsPerCall, r.Behavior.StoreTranscript = s.MaxProposalsPerCall, s.StoreTranscript
	r.Behavior.Announcement, r.Behavior.SummaryTarget = s.Announcement, s.SummaryTarget
	return r
}

// TranscriptSegmentRequest is one finalized turn the worker reports.
type TranscriptSegmentRequest struct {
	SpeakerID uuid.UUID `json:"speaker_id"`
	Text      string    `json:"text"`
	SpokenAt  time.Time `json:"spoken_at"`
}

// AppendTranscriptRequest is a batch of turns.
type AppendTranscriptRequest struct {
	Segments []TranscriptSegmentRequest `json:"segments"`
}

// TranscriptSegmentResponse is one stored turn.
type TranscriptSegmentResponse struct {
	SpeakerID *uuid.UUID `json:"speaker_id"`
	Text      string     `json:"text"`
	SpokenAt  time.Time  `json:"spoken_at"`
}

// MeetingResponse is a call of a channel that was transcribed.
type MeetingResponse struct {
	CallID    uuid.UUID  `json:"call_id"`
	StartedAt time.Time  `json:"started_at"`
	EndedAt   *time.Time `json:"ended_at"`
	Segments  int        `json:"segments"`
	Speakers  []string   `json:"speakers"`
}

// MeetingFromEntity converts a meeting summary.
func MeetingFromEntity(m *meetingdom.MeetingSummary) MeetingResponse {
	sp := m.Speakers
	if sp == nil {
		sp = []string{}
	}
	return MeetingResponse{CallID: m.CallID, StartedAt: m.StartedAt, EndedAt: m.EndedAt, Segments: m.Segments, Speakers: sp}
}

// TranscriptLineResponse is one turn of a call's transcript, with who said it.
type TranscriptLineResponse struct {
	SpeakerID   *uuid.UUID `json:"speaker_id"`
	SpeakerName string     `json:"speaker_name"`
	Text        string     `json:"text"`
	SpokenAt    time.Time  `json:"spoken_at"`
}

// TranscriptLineFromEntity converts a named segment.
func TranscriptLineFromEntity(s *meetingdom.NamedSegment) TranscriptLineResponse {
	return TranscriptLineResponse{SpeakerID: s.SpeakerID, SpeakerName: s.SpeakerName, Text: s.Text, SpokenAt: s.SpokenAt}
}
