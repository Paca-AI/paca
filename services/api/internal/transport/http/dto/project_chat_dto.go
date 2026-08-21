package dto

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"

	agentdom "github.com/Paca-AI/api/internal/domain/agent"
)

type ContextSourceRefRequest struct {
	Type agentdom.ContextSourceType `json:"type"`
	ID   uuid.UUID                  `json:"id"`
}

type CreateProjectChatRequest struct {
	AgentID        uuid.UUID                 `json:"agent_id"`
	Message        string                    `json:"message"`
	Title          *string                   `json:"title,omitempty"`
	ContextSources []ContextSourceRefRequest `json:"context_sources,omitempty"`
	DeadlineAt     *time.Time                `json:"deadline_at,omitempty"`
}

type AppendProjectChatTurnRequest struct {
	Message    string     `json:"message"`
	DeadlineAt *time.Time `json:"deadline_at,omitempty"`
}

type ReplaceProjectChatContextRequest struct {
	Sources []ContextSourceRefRequest `json:"sources"`
}

type PrepareProjectConclusionRequest struct {
	TargetTaskID        uuid.UUID       `json:"target_task_id"`
	SummaryOverride     *string         `json:"summary_override,omitempty"`
	UpdateDescription   bool            `json:"update_description"`
	DescriptionBase     json.RawMessage `json:"description_base,omitempty"`
	ProposedDescription json.RawMessage `json:"proposed_description,omitempty"`
	ExpiresAt           time.Time       `json:"expires_at"`
}

type ConfirmProjectConclusionRequest struct {
	PreparationID   uuid.UUID `json:"preparation_id"`
	ExpectedVersion int       `json:"expected_version"`
	ExpectedSHA256  string    `json:"expected_sha256"`
}

type ProjectChatSessionResponse struct {
	ID            uuid.UUID  `json:"id"`
	AgentID       uuid.UUID  `json:"agent_id"`
	ProjectID     uuid.UUID  `json:"project_id"`
	Title         *string    `json:"title,omitempty"`
	LastMessageAt *time.Time `json:"last_message_at,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

type ProjectChatSessionSummaryResponse struct {
	Session             ProjectChatSessionResponse `json:"session"`
	AgentName           string                     `json:"agent_name"`
	AgentHandle         string                     `json:"agent_handle"`
	LatestTurn          *ProjectChatTurnResponse   `json:"latest_turn,omitempty"`
	LatestRun           *ProjectChatRunResponse    `json:"latest_run,omitempty"`
	HasLegacyExecutions bool                       `json:"has_legacy_executions"`
}

type LegacyChatExecutionResponse struct {
	ConversationID uuid.UUID  `json:"conversation_id"`
	Status         string     `json:"status"`
	CreatedAt      time.Time  `json:"created_at"`
	FinishedAt     *time.Time `json:"finished_at,omitempty"`
}

type ProjectChatTurnResponse struct {
	ID               uuid.UUID           `json:"id"`
	SessionID        *uuid.UUID          `json:"session_id,omitempty"`
	ConversationID   uuid.UUID           `json:"conversation_id"`
	TurnIndex        int                 `json:"turn_index"`
	InputText        string              `json:"input_text"`
	Status           agentdom.TurnStatus `json:"status"`
	ToolPolicySHA256 string              `json:"tool_policy_sha256"`
	CommandSHA256    string              `json:"command_sha256"`
	RequestSHA256    string              `json:"request_sha256"`
	StateVersion     int64               `json:"state_version"`
	DeadlineAt       *time.Time          `json:"deadline_at,omitempty"`
	StartedAt        *time.Time          `json:"started_at,omitempty"`
	FinishedAt       *time.Time          `json:"finished_at,omitempty"`
	CreatedAt        time.Time           `json:"created_at"`
	UpdatedAt        time.Time           `json:"updated_at"`
}

type ProjectChatRunResponse struct {
	ID                 uuid.UUID            `json:"id"`
	TurnID             uuid.UUID            `json:"turn_id"`
	ConversationID     uuid.UUID            `json:"conversation_id"`
	Backend            agentdom.TurnBackend `json:"backend"`
	Attempt            int                  `json:"attempt"`
	Status             agentdom.TurnStatus  `json:"status"`
	FinalEventSequence *int                 `json:"final_event_sequence,omitempty"`
	StartedAt          *time.Time           `json:"started_at,omitempty"`
	FinishedAt         *time.Time           `json:"finished_at,omitempty"`
	CreatedAt          time.Time            `json:"created_at"`
	UpdatedAt          time.Time            `json:"updated_at"`
}

type ProjectChatTurnResultResponse struct {
	TurnID              uuid.UUID                   `json:"turn_id"`
	RunID               uuid.UUID                   `json:"run_id"`
	TerminalStatus      agentdom.TurnStatus         `json:"terminal_status"`
	StableOutput        *string                     `json:"stable_output,omitempty"`
	StableOutputSHA256  *string                     `json:"stable_output_sha256,omitempty"`
	StableOutputEventID *uuid.UUID                  `json:"stable_output_event_id,omitempty"`
	GeneratedByAgentID  uuid.UUID                   `json:"generated_by_agent_id"`
	ErrorCode           *string                     `json:"error_code,omitempty"`
	ErrorMessage        *string                     `json:"error_message,omitempty"`
	RuntimeDisposition  agentdom.RuntimeDisposition `json:"runtime_disposition"`
	CreatedAt           time.Time                   `json:"created_at"`
}

type ProjectChatContextItemResponse struct {
	ID             uuid.UUID                  `json:"id"`
	Ordinal        int                        `json:"ordinal"`
	SourceType     agentdom.ContextSourceType `json:"source_type"`
	SourceID       uuid.UUID                  `json:"source_id"`
	SourceVersion  string                     `json:"source_version"`
	SourceAudience agentdom.ContextAudience   `json:"source_audience"`
	CapturedAt     time.Time                  `json:"captured_at"`
	Content        json.RawMessage            `json:"content"`
	RenderedText   string                     `json:"rendered_text"`
	ContentSHA256  string                     `json:"content_sha256"`
	ByteCount      int                        `json:"byte_count"`
}

type ProjectChatContextSnapshotResponse struct {
	ID             uuid.UUID                        `json:"id"`
	TurnID         uuid.UUID                        `json:"turn_id"`
	SchemaVersion  int                              `json:"schema_version"`
	Manifest       json.RawMessage                  `json:"manifest"`
	RenderedText   string                           `json:"rendered_text"`
	ManifestSHA256 string                           `json:"manifest_sha256"`
	TotalBytes     int                              `json:"total_bytes"`
	CreatedAt      time.Time                        `json:"created_at"`
	Items          []ProjectChatContextItemResponse `json:"items"`
}

type ProjectChatTurnBundleResponse struct {
	Session  *ProjectChatSessionResponse        `json:"session,omitempty"`
	Turn     ProjectChatTurnResponse            `json:"turn"`
	Run      ProjectChatRunResponse             `json:"run"`
	Runs     []ProjectChatRunResponse           `json:"runs"`
	Result   *ProjectChatTurnResultResponse     `json:"result"`
	Snapshot ProjectChatContextSnapshotResponse `json:"context_snapshot"`
}

type ProjectChatContextSnapshotSummaryResponse struct {
	ID             uuid.UUID `json:"id"`
	SchemaVersion  int       `json:"schema_version"`
	ManifestSHA256 string    `json:"manifest_sha256"`
	TotalBytes     int       `json:"total_bytes"`
	SourceCount    int       `json:"source_count"`
	CreatedAt      time.Time `json:"created_at"`
}

type ProjectChatTurnHistoryResponse struct {
	Turn            ProjectChatTurnResponse                   `json:"turn"`
	Run             ProjectChatRunResponse                    `json:"run"`
	Runs            []ProjectChatRunResponse                  `json:"runs"`
	Result          *ProjectChatTurnResultResponse            `json:"result"`
	ContextSnapshot ProjectChatContextSnapshotSummaryResponse `json:"context_snapshot"`
}

type ProjectChatEventResponse struct {
	ID             uuid.UUID      `json:"id"`
	ConversationID uuid.UUID      `json:"conversation_id"`
	EventIndex     int            `json:"event_index"`
	TurnID         *uuid.UUID     `json:"turn_id,omitempty"`
	TurnRunID      *uuid.UUID     `json:"turn_run_id,omitempty"`
	TurnRunAttempt *int           `json:"turn_run_attempt,omitempty"`
	TurnSequence   *int           `json:"turn_sequence,omitempty"`
	EventType      string         `json:"event_type"`
	EventSource    string         `json:"event_source"`
	Payload        map[string]any `json:"payload"`
	CreatedAt      time.Time      `json:"created_at"`
}

type ProjectChatContextSourceResponse struct {
	ID         uuid.UUID                  `json:"id"`
	SourceType agentdom.ContextSourceType `json:"source_type"`
	SourceID   uuid.UUID                  `json:"source_id"`
	Ordinal    int                        `json:"ordinal"`
	CreatedAt  time.Time                  `json:"created_at"`
}

type ConclusionPreparationResponse struct {
	ID                      uuid.UUID               `json:"id"`
	SourceTurnID            uuid.UUID               `json:"source_turn_id"`
	TargetTaskID            uuid.UUID               `json:"target_task_id"`
	GeneratedByAgentID      uuid.UUID               `json:"generated_by_agent_id"`
	Kind                    agentdom.ConclusionKind `json:"kind"`
	RelatedPublicationID    *uuid.UUID              `json:"related_publication_id,omitempty"`
	Summary                 string                  `json:"summary"`
	SummaryVersion          int                     `json:"summary_version"`
	SummarySHA256           string                  `json:"summary_sha256"`
	UpdateDescription       bool                    `json:"update_description"`
	DescriptionBefore       json.RawMessage         `json:"description_before,omitempty"`
	DescriptionBeforeSHA256 string                  `json:"description_before_sha256,omitempty"`
	DescriptionAfter        json.RawMessage         `json:"description_after,omitempty"`
	DescriptionAfterSHA256  string                  `json:"description_after_sha256,omitempty"`
	IsFrozen                bool                    `json:"is_frozen"`
	State                   string                  `json:"state"`
	ExpiresAt               time.Time               `json:"expires_at"`
	CreatedAt               time.Time               `json:"created_at"`
}

type ConclusionPublicationResponse struct {
	ID                      uuid.UUID               `json:"id"`
	TargetTaskID            uuid.UUID               `json:"target_task_id"`
	SourceAccessible        bool                    `json:"source_accessible"`
	SourceSessionID         *uuid.UUID              `json:"source_session_id,omitempty"`
	SourceTurnID            *uuid.UUID              `json:"source_turn_id,omitempty"`
	PublishedByUserID       uuid.UUID               `json:"published_by_user_id"`
	PublishedByMemberID     uuid.UUID               `json:"published_by_member_id"`
	GeneratedByAgentID      uuid.UUID               `json:"generated_by_agent_id"`
	Kind                    agentdom.ConclusionKind `json:"kind"`
	RootPublicationID       *uuid.UUID              `json:"root_publication_id,omitempty"`
	RevisesPublicationID    *uuid.UUID              `json:"revises_publication_id,omitempty"`
	WithdrawsPublicationID  *uuid.UUID              `json:"withdraws_publication_id,omitempty"`
	Summary                 *string                 `json:"summary,omitempty"`
	SummaryVersion          *int                    `json:"summary_version,omitempty"`
	SummarySHA256           *string                 `json:"summary_sha256,omitempty"`
	DescriptionUpdated      bool                    `json:"description_updated"`
	DescriptionBeforeSHA256 *string                 `json:"description_before_sha256,omitempty"`
	DescriptionAfterSHA256  *string                 `json:"description_after_sha256,omitempty"`
	CreatedAt               time.Time               `json:"created_at"`
}

func ContextSourceRefsFromRequest(values []ContextSourceRefRequest) []agentdom.ContextSourceRef {
	refs := make([]agentdom.ContextSourceRef, 0, len(values))
	for _, value := range values {
		refs = append(refs, agentdom.ContextSourceRef{Type: value.Type, ID: value.ID})
	}
	return refs
}

func ProjectChatSessionFromEntity(value *agentdom.AgentChatSession) ProjectChatSessionResponse {
	return ProjectChatSessionResponse{
		ID: value.ID, AgentID: value.AgentID, ProjectID: value.ProjectID,
		Title: value.Title, LastMessageAt: value.LastMessageAt,
		CreatedAt: value.CreatedAt, UpdatedAt: value.UpdatedAt,
	}
}

func ProjectChatTurnFromEntity(value *agentdom.AgentTurn) ProjectChatTurnResponse {
	return ProjectChatTurnResponse{
		ID: value.ID, SessionID: value.SessionID, ConversationID: value.ConversationID,
		TurnIndex: value.TurnIndex, InputText: value.InputText, Status: value.Status,
		ToolPolicySHA256: value.ToolPolicySHA256, CommandSHA256: value.CommandSHA256,
		RequestSHA256: value.RequestSHA256,
		StateVersion:  value.StateVersion, DeadlineAt: value.DeadlineAt,
		StartedAt: value.StartedAt, FinishedAt: value.FinishedAt,
		CreatedAt: value.CreatedAt, UpdatedAt: value.UpdatedAt,
	}
}

func ProjectChatRunFromEntity(value *agentdom.TurnRun) ProjectChatRunResponse {
	return ProjectChatRunResponse{
		ID: value.ID, TurnID: value.TurnID, ConversationID: value.ConversationID,
		Backend: value.Backend, Attempt: value.Attempt, Status: value.Status,
		FinalEventSequence: value.FinalEventSequence, StartedAt: value.StartedAt,
		FinishedAt: value.FinishedAt, CreatedAt: value.CreatedAt, UpdatedAt: value.UpdatedAt,
	}
}

func ProjectChatTurnResultFromEntity(value *agentdom.TurnResult) *ProjectChatTurnResultResponse {
	if value == nil {
		return nil
	}
	return &ProjectChatTurnResultResponse{
		TurnID: value.TurnID, RunID: value.RunID, TerminalStatus: value.TerminalStatus,
		StableOutput: value.StableOutput, StableOutputSHA256: value.StableOutputSHA256,
		StableOutputEventID: value.StableOutputEventID,
		GeneratedByAgentID:  value.GeneratedByAgentID, ErrorCode: value.ErrorCode,
		ErrorMessage: value.ErrorMessage, RuntimeDisposition: value.RuntimeDisposition,
		CreatedAt: value.CreatedAt,
	}
}

func ProjectChatSnapshotFromEntity(value *agentdom.TurnContextSnapshot) ProjectChatContextSnapshotResponse {
	items := make([]ProjectChatContextItemResponse, 0, len(value.Items))
	for _, item := range value.Items {
		items = append(items, ProjectChatContextItemResponse{
			ID: item.ID, Ordinal: item.Ordinal, SourceType: item.SourceType,
			SourceID: item.SourceID, SourceVersion: item.SourceVersion,
			SourceAudience: item.SourceAudience, CapturedAt: item.CapturedAt,
			Content: item.Content, RenderedText: item.RenderedText,
			ContentSHA256: item.ContentSHA256, ByteCount: item.ByteCount,
		})
	}
	return ProjectChatContextSnapshotResponse{
		ID: value.ID, TurnID: value.TurnID, SchemaVersion: value.SchemaVersion,
		Manifest: value.Manifest, RenderedText: value.RenderedText,
		ManifestSHA256: value.ManifestSHA256, TotalBytes: value.TotalBytes,
		CreatedAt: value.CreatedAt, Items: items,
	}
}

func ProjectChatBundleFromEntity(value *agentdom.TurnBundle) ProjectChatTurnBundleResponse {
	runs := make([]ProjectChatRunResponse, 0, len(value.Runs))
	for _, run := range value.Runs {
		runs = append(runs, ProjectChatRunFromEntity(run))
	}
	response := ProjectChatTurnBundleResponse{
		Turn: ProjectChatTurnFromEntity(value.Turn), Run: ProjectChatRunFromEntity(value.Run),
		Runs:     runs,
		Result:   ProjectChatTurnResultFromEntity(value.Result),
		Snapshot: ProjectChatSnapshotFromEntity(value.Snapshot),
	}
	if value.Session != nil {
		session := ProjectChatSessionFromEntity(value.Session)
		response.Session = &session
	}
	return response
}

func ProjectChatTurnHistoryFromEntity(value *agentdom.TurnBundle) ProjectChatTurnHistoryResponse {
	runs := make([]ProjectChatRunResponse, 0, len(value.Runs))
	for _, run := range value.Runs {
		runs = append(runs, ProjectChatRunFromEntity(run))
	}
	return ProjectChatTurnHistoryResponse{
		Turn: ProjectChatTurnFromEntity(value.Turn), Run: ProjectChatRunFromEntity(value.Run),
		Runs:   runs,
		Result: ProjectChatTurnResultFromEntity(value.Result),
		ContextSnapshot: ProjectChatContextSnapshotSummaryResponse{
			ID: value.Snapshot.ID, SchemaVersion: value.Snapshot.SchemaVersion,
			ManifestSHA256: value.Snapshot.ManifestSHA256, TotalBytes: value.Snapshot.TotalBytes,
			SourceCount: len(value.Snapshot.Items), CreatedAt: value.Snapshot.CreatedAt,
		},
	}
}

func ConclusionPreparationFromEntity(value *agentdom.ConclusionPreparation) ConclusionPreparationResponse {
	return ConclusionPreparationResponse{
		ID: value.ID, SourceTurnID: value.SourceTurnID, TargetTaskID: value.TargetTaskID,
		GeneratedByAgentID: value.GeneratedByAgentID, Kind: value.Kind,
		RelatedPublicationID: value.RelatedPublicationID, Summary: value.Summary,
		SummaryVersion: value.SummaryVersion, SummarySHA256: value.SummarySHA256,
		UpdateDescription:       value.UpdateDescription,
		DescriptionBefore:       value.DescriptionBefore,
		DescriptionBeforeSHA256: value.DescriptionBeforeSHA256,
		DescriptionAfter:        value.DescriptionAfter,
		DescriptionAfterSHA256:  value.DescriptionAfterSHA256,
		IsFrozen:                value.IsFrozen, State: value.State, ExpiresAt: value.ExpiresAt,
		CreatedAt: value.CreatedAt,
	}
}

func ConclusionPublicationFromEntity(value *agentdom.ConclusionPublication, sourceAccessible bool, sourceSessionID, sourceTurnID *uuid.UUID) ConclusionPublicationResponse {
	if !sourceAccessible {
		sourceSessionID = nil
		sourceTurnID = nil
	}
	var summary *string
	var summaryVersion *int
	var summarySHA256 *string
	if !value.DescriptionUpdated {
		summary = &value.Summary
		summaryVersion = &value.SummaryVersion
		summarySHA256 = &value.SummarySHA256
	}
	return ConclusionPublicationResponse{
		ID: value.ID, TargetTaskID: value.TargetTaskID, SourceAccessible: sourceAccessible,
		SourceSessionID: sourceSessionID, SourceTurnID: sourceTurnID,
		PublishedByUserID: value.PublishedByUserID, PublishedByMemberID: value.PublishedByMemberID,
		GeneratedByAgentID: value.GeneratedByAgentID, Kind: value.Kind,
		RootPublicationID: value.RootPublicationID, RevisesPublicationID: value.RevisesPublicationID,
		WithdrawsPublicationID: value.WithdrawsPublicationID, Summary: summary,
		SummaryVersion: summaryVersion, SummarySHA256: summarySHA256,
		DescriptionUpdated:      value.DescriptionUpdated,
		DescriptionBeforeSHA256: value.DescriptionBeforeSHA256,
		DescriptionAfterSHA256:  value.DescriptionAfterSHA256,
		CreatedAt:               value.CreatedAt,
	}
}
