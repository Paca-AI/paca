package agentdom

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
)

// CanonicalizeJSON preserves json.Number values (including integers larger
// than 2^53), rejects trailing JSON values and relies on encoding/json's
// stable object-key ordering for deterministic audit hashes.
func CanonicalizeJSON(raw []byte) ([]byte, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return nil, errors.New("multiple JSON values")
		}
		return nil, err
	}
	return json.Marshal(value)
}

// Turn payload and context size limits bound durable storage and model input.
const (
	MaxTurnInputBytes       = 32 * 1024
	MaxContextSources       = 64
	MaxContextItemBytes     = 32 * 1024
	MaxContextSnapshotBytes = 128 * 1024
	MaxStableOutputBytes    = 128 * 1024
	MaxConclusionBytes      = 16 * 1024
)

// TurnStatus describes the lifecycle state of an authoritative agent turn.
type TurnStatus string

// Authoritative turn lifecycle states.
const (
	TurnStatusQueued    TurnStatus = "queued"
	TurnStatusRunning   TurnStatus = "running"
	TurnStatusSucceeded TurnStatus = "succeeded"
	TurnStatusFailed    TurnStatus = "failed"
	TurnStatusStopped   TurnStatus = "stopped"
	TurnStatusCancelled TurnStatus = "cancelled"
	TurnStatusTimedOut  TurnStatus = "timed_out"
	TurnStatusNoOutput  TurnStatus = "no_output"
)

// IsTerminal reports whether no further execution may occur for the turn.
func (s TurnStatus) IsTerminal() bool {
	switch s {
	case TurnStatusSucceeded, TurnStatusFailed, TurnStatusStopped,
		TurnStatusCancelled, TurnStatusTimedOut, TurnStatusNoOutput:
		return true
	default:
		return false
	}
}

// TurnBackend identifies the execution protocol used for a run.
type TurnBackend string

// Supported authoritative execution backends.
const (
	TurnBackendLLM TurnBackend = "llm"
	TurnBackendACP TurnBackend = "acp"
)

// ContextSourceType identifies a resource selected as turn context.
type ContextSourceType string

// Supported context source kinds.
const (
	ContextSourceTask    ContextSourceType = "task"
	ContextSourceSession ContextSourceType = "session"
	ContextSourceRun     ContextSourceType = "run"
)

// ContextAudience records who may observe a captured context item.
type ContextAudience string

// Supported context visibility audiences.
const (
	ContextAudienceOwnerPrivate  ContextAudience = "owner_private"
	ContextAudienceProjectShared ContextAudience = "project_shared"
)

// SessionContextSource is a live selection for a future turn. Its content is
// deliberately absent: canonical content is re-read and re-authorized while
// constructing each immutable snapshot.
type SessionContextSource struct {
	ID                 uuid.UUID
	SessionID          uuid.UUID
	ProjectID          uuid.UUID
	SourceType         ContextSourceType
	SourceID           uuid.UUID
	Ordinal            int
	SelectedByMemberID uuid.UUID
	CreatedAt          time.Time
}

// TurnContextItem is one immutable resource captured for a turn.
type TurnContextItem struct {
	ID             uuid.UUID
	SnapshotID     uuid.UUID
	Ordinal        int
	SourceType     ContextSourceType
	SourceID       uuid.UUID
	SourceVersion  string
	SourceAudience ContextAudience
	CapturedAt     time.Time
	Content        json.RawMessage
	RenderedText   string
	ContentSHA256  string
	ByteCount      int
}

// TurnContextSnapshot is the immutable, auditable context supplied to a turn.
type TurnContextSnapshot struct {
	ID             uuid.UUID
	TurnID         uuid.UUID
	SchemaVersion  int
	Manifest       json.RawMessage
	RenderedText   string
	ManifestSHA256 string
	TotalBytes     int
	CreatedAt      time.Time
	Items          []TurnContextItem
}

type contextManifestItem struct {
	Ordinal           int               `json:"ordinal"`
	SourceType        ContextSourceType `json:"source_type"`
	SourceID          uuid.UUID         `json:"source_id"`
	SourceVersion     string            `json:"source_version"`
	SourceAudience    ContextAudience   `json:"source_audience"`
	CapturedAt        time.Time         `json:"captured_at"`
	ContentSHA256     string            `json:"content_sha256"`
	ContentByteCount  int               `json:"content_byte_count"`
	RenderedSHA256    string            `json:"rendered_text_sha256"`
	RenderedByteCount int               `json:"rendered_text_byte_count"`
	ByteCount         int               `json:"byte_count"`
	Trust             string            `json:"trust"`
}

// CanonicalizeContextSnapshot recomputes every caller-controlled audit field.
// Source content/audience must already have been resolved by the authorized
// source loader; this function makes its immutable encoding deterministic.
func CanonicalizeContextSnapshot(snapshot TurnContextSnapshot) (TurnContextSnapshot, error) {
	if len(snapshot.Items) > MaxContextSources {
		return TurnContextSnapshot{}, ErrContextSnapshotTooLarge
	}
	sort.Slice(snapshot.Items, func(i, j int) bool { return snapshot.Items[i].Ordinal < snapshot.Items[j].Ordinal })
	manifest := make([]contextManifestItem, 0, len(snapshot.Items))
	rendered := make([]string, 0, len(snapshot.Items))
	for index := range snapshot.Items {
		item := &snapshot.Items[index]
		if item.Ordinal != index || item.SourceID == uuid.Nil || strings.TrimSpace(item.SourceVersion) == "" {
			return TurnContextSnapshot{}, errors.New("agent context snapshot: invalid item order or identity")
		}
		item.SnapshotID = snapshot.ID
		canonical, err := CanonicalizeJSON(item.Content)
		if err != nil {
			return TurnContextSnapshot{}, fmt.Errorf("agent context snapshot: invalid item content: %w", err)
		}
		item.Content = append(json.RawMessage(nil), canonical...)
		contentSum := sha256.Sum256(item.Content)
		item.ContentSHA256 = fmt.Sprintf("%x", contentSum[:])
		contentBytes := len(item.Content)
		renderedBytes := len([]byte(item.RenderedText))
		renderedSum := sha256.Sum256([]byte(item.RenderedText))
		item.ByteCount = contentBytes + renderedBytes
		if item.ByteCount > MaxContextItemBytes {
			return TurnContextSnapshot{}, ErrContextSnapshotTooLarge
		}
		manifest = append(manifest, contextManifestItem{
			Ordinal: item.Ordinal, SourceType: item.SourceType, SourceID: item.SourceID,
			SourceVersion: item.SourceVersion, SourceAudience: item.SourceAudience,
			CapturedAt: item.CapturedAt.UTC(), ContentSHA256: item.ContentSHA256,
			ContentByteCount: contentBytes, RenderedSHA256: fmt.Sprintf("%x", renderedSum[:]),
			RenderedByteCount: renderedBytes, ByteCount: item.ByteCount, Trust: "untrusted",
		})
		rendered = append(rendered, item.RenderedText)
	}
	manifestJSON, err := json.Marshal(manifest)
	if err != nil {
		return TurnContextSnapshot{}, err
	}
	snapshot.Manifest = manifestJSON
	snapshot.RenderedText = strings.Join(rendered, "\n\n")
	snapshot.TotalBytes = len([]byte(snapshot.RenderedText))
	for index := range snapshot.Items {
		snapshot.TotalBytes += len(snapshot.Items[index].Content)
	}
	if snapshot.TotalBytes > MaxContextSnapshotBytes {
		return TurnContextSnapshot{}, ErrContextSnapshotTooLarge
	}
	manifestSum := sha256.Sum256(snapshot.Manifest)
	snapshot.ManifestSHA256 = fmt.Sprintf("%x", manifestSum[:])
	return snapshot, nil
}

// ContextSnapshotRequestSHA256 identifies the canonical context selected by a
// client command without including generated row IDs or the observation time.
// The immutable audit manifest still records CapturedAt; excluding it here is
// what lets two concurrent deliveries of the same idempotent request replay
// instead of conflicting solely because they observed the same content a few
// milliseconds apart.
func ContextSnapshotRequestSHA256(snapshot TurnContextSnapshot) (string, error) {
	canonical, err := CanonicalizeContextSnapshot(snapshot)
	if err != nil {
		return "", err
	}
	type requestItem struct {
		Ordinal           int               `json:"ordinal"`
		SourceType        ContextSourceType `json:"source_type"`
		SourceID          uuid.UUID         `json:"source_id"`
		SourceVersion     string            `json:"source_version"`
		SourceAudience    ContextAudience   `json:"source_audience"`
		ContentSHA256     string            `json:"content_sha256"`
		RenderedSHA256    string            `json:"rendered_text_sha256"`
		ContentByteCount  int               `json:"content_byte_count"`
		RenderedByteCount int               `json:"rendered_text_byte_count"`
		Trust             string            `json:"trust"`
	}
	items := make([]requestItem, 0, len(canonical.Items))
	for _, item := range canonical.Items {
		renderedSum := sha256.Sum256([]byte(item.RenderedText))
		items = append(items, requestItem{
			Ordinal: item.Ordinal, SourceType: item.SourceType, SourceID: item.SourceID,
			SourceVersion: item.SourceVersion, SourceAudience: item.SourceAudience,
			ContentSHA256: item.ContentSHA256, RenderedSHA256: fmt.Sprintf("%x", renderedSum[:]),
			ContentByteCount: len(item.Content), RenderedByteCount: len([]byte(item.RenderedText)),
			Trust: "untrusted",
		})
	}
	encoded, err := json.Marshal(items)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(encoded)
	return fmt.Sprintf("%x", sum[:]), nil
}

// TurnToolPolicy is the auditable, deny-by-default capability envelope used
// by both LLM and ACP turns. Context is always untrusted data and is never
// allowed to add capabilities. Private chat currently permits read-only API
// capabilities; task mutations require a future proposal/preview/apply flow.
type TurnToolPolicy struct {
	Version             int      `json:"version"`
	Mode                string   `json:"mode"`
	AllowedCapabilities []string `json:"allowed_capabilities"`
	ContextMayGrant     bool     `json:"context_may_grant"`
}

// PrivateChatToolPolicy returns the deny-by-default policy for project chats.
func PrivateChatToolPolicy() TurnToolPolicy {
	return TurnToolPolicy{
		Version: 1,
		Mode:    "deny_by_default",
		AllowedCapabilities: []string{
			"agents.read",
			"docs.read",
			"projects.read",
			"sprints.read",
			"tasks.read",
			"workflows.read",
		},
		ContextMayGrant: false,
	}
}

// CanonicalJSON validates and deterministically encodes the policy.
func (p TurnToolPolicy) CanonicalJSON() ([]byte, error) {
	if p.Version != 1 || p.Mode != "deny_by_default" || p.ContextMayGrant {
		return nil, errors.New("agent turn tool policy: unsafe envelope")
	}
	capabilities := append([]string(nil), p.AllowedCapabilities...)
	sort.Strings(capabilities)
	for index, capability := range capabilities {
		if !isReadCapability(capability) {
			return nil, fmt.Errorf("agent turn tool policy: mutation capability %q is forbidden", capability)
		}
		if index > 0 && capability == capabilities[index-1] {
			return nil, fmt.Errorf("agent turn tool policy: duplicate capability %q", capability)
		}
	}
	normalized := p
	normalized.AllowedCapabilities = capabilities
	return json.Marshal(normalized)
}

// SHA256 returns the digest of the validated canonical policy.
func (p TurnToolPolicy) SHA256() (string, error) {
	canonical, err := p.CanonicalJSON()
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(canonical)
	return fmt.Sprintf("%x", sum[:]), nil
}

func isReadCapability(capability string) bool {
	if len(capability) < len("a.read") || capability[len(capability)-5:] != ".read" {
		return false
	}
	for i, char := range capability[:len(capability)-5] {
		if (char >= 'a' && char <= 'z') || (i > 0 && char >= '0' && char <= '9') || (i > 0 && char == '_') {
			continue
		}
		return false
	}
	return true
}

// AgentTurn is the durable logical unit of authoritative agent work.
type AgentTurn struct {
	ID                  uuid.UUID
	SessionID           *uuid.UUID
	ConversationID      uuid.UUID
	ProjectID           *uuid.UUID
	AgentID             uuid.UUID
	RequestedByMemberID *uuid.UUID
	RequestedByUserID   *uuid.UUID
	TurnIndex           int
	InputText           string
	Status              TurnStatus
	IdempotencyKey      string
	ToolPolicy          TurnToolPolicy
	ToolPolicySHA256    string
	CommandSHA256       string
	RequestSHA256       string
	StateVersion        int64
	DeadlineAt          *time.Time
	StartedAt           *time.Time
	FinishedAt          *time.Time
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

// TurnRun is one claimed execution attempt for an AgentTurn.
type TurnRun struct {
	ID                 uuid.UUID
	TurnID             uuid.UUID
	ConversationID     uuid.UUID
	Backend            TurnBackend
	Attempt            int
	Status             TurnStatus
	ClaimToken         *uuid.UUID
	ClaimedBy          *string
	LeaseExpiresAt     *time.Time
	FinalEventSequence *int
	StartedAt          *time.Time
	FinishedAt         *time.Time
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

// RuntimeDisposition describes whether an execution runtime may be reused.
type RuntimeDisposition string

// Supported post-run runtime dispositions.
const (
	RuntimeReusable RuntimeDisposition = "reusable"
	RuntimeRetired  RuntimeDisposition = "retired"
)

// TurnResult is the immutable terminal result of an AgentTurn.
type TurnResult struct {
	TurnID              uuid.UUID
	RunID               uuid.UUID
	TerminalStatus      TurnStatus
	StableOutput        *string
	StableOutputSHA256  *string
	StableOutputEventID *uuid.UUID
	GeneratedByAgentID  uuid.UUID
	ErrorCode           *string
	ErrorMessage        *string
	RuntimeDisposition  RuntimeDisposition
	CreatedAt           time.Time
}

// TurnBundle groups the durable records required to render or execute a turn.
type TurnBundle struct {
	Session      *AgentChatSession
	Conversation *AgentConversation
	Turn         *AgentTurn
	Run          *TurnRun
	Runs         []*TurnRun
	Result       *TurnResult
	Snapshot     *TurnContextSnapshot
}

// ChatSessionListFilter is the owner-scoped, session-first history query.
// Cursor values are opaque to HTTP clients; the transport encodes/decodes the
// stable (last activity, session ID) tuple used by the repository.
type ChatSessionListFilter struct {
	ProjectID  uuid.UUID
	MemberID   uuid.UUID
	AgentID    *uuid.UUID
	Search     string
	Limit      int
	CursorTime *time.Time
	CursorID   *uuid.UUID
}

// ChatSessionSummary is the session-first projection used by chat history.
type ChatSessionSummary struct {
	Session             AgentChatSession
	AgentName           string
	AgentHandle         string
	LatestTurn          *AgentTurn
	LatestRun           *TurnRun
	HasLegacyExecutions bool
}

// TurnEventCursor identifies a stable position in a turn event stream.
type TurnEventCursor struct {
	EventIndex int
	ID         uuid.UUID
}

// TurnEventListFilter scopes and paginates owner-visible turn events.
type TurnEventListFilter struct {
	ProjectID uuid.UUID
	TurnID    uuid.UUID
	MemberID  uuid.UUID
	Limit     int
	Cursor    *TurnEventCursor
}

// LegacyExecutionListFilter scopes compatibility chat execution history.
type LegacyExecutionListFilter struct {
	ProjectID  uuid.UUID
	SessionID  uuid.UUID
	MemberID   uuid.UUID
	Limit      int
	CursorTime *time.Time
	CursorID   *uuid.UUID
}

// ConclusionPublicationListFilter scopes and paginates task publications.
type ConclusionPublicationListFilter struct {
	ProjectID      uuid.UUID
	TaskID         uuid.UUID
	ViewerMemberID uuid.UUID
	Limit          int
	CursorTime     *time.Time
	CursorID       *uuid.UUID
}

// LegacyChatExecution is a read-only compatibility record for a conversation
// created before authoritative turns existed. It is never promoted into a
// synthetic turn and therefore can never be used as a publication source.
type LegacyChatExecution struct {
	ConversationID uuid.UUID
	Status         string
	CreatedAt      time.Time
	FinishedAt     *time.Time
}

// ConclusionPublicationView applies viewer-specific source visibility.
type ConclusionPublicationView struct {
	Publication      ConclusionPublication
	SourceAccessible bool
	SourceSessionID  *uuid.UUID
	SourceTurnID     *uuid.UUID
}

// ChatActor carries the authenticated identity for project chat operations.
type ChatActor struct {
	UserID     uuid.UUID
	MemberID   uuid.UUID
	LegacyRole string
}

// ContextSourceRef identifies a live resource selected for future context.
type ContextSourceRef struct {
	Type ContextSourceType
	ID   uuid.UUID
}

// ProjectChatCommand contains only client-semantic request fields. Generated
// IDs, live source content, capture timestamps, runtime reuse and the concrete
// server-materialized default deadline are intentionally excluded.
type ProjectChatCommand struct {
	NewSession          bool
	SessionID           *uuid.UUID
	ProjectID           uuid.UUID
	AgentID             uuid.UUID
	RequestedByMemberID uuid.UUID
	InputText           string
	ContextSources      []ContextSourceRef
	RequestedDeadline   *time.Time
	Title               *string
}

// SHA256 returns the semantic idempotency fingerprint of the command.
func (command ProjectChatCommand) SHA256() (string, error) {
	if command.NewSession {
		command.SessionID = nil
	} else if command.SessionID == nil || *command.SessionID == uuid.Nil {
		return "", errors.New("agent project chat command: session is required")
	}
	if command.RequestedDeadline != nil {
		deadline := command.RequestedDeadline.UTC()
		command.RequestedDeadline = &deadline
	}
	// A missing context_sources field and an explicit empty array carry the
	// same client semantics. Keep their idempotency fingerprint identical.
	contextSources := command.ContextSources
	if len(contextSources) == 0 {
		contextSources = []ContextSourceRef{}
	}
	payload := struct {
		NewSession          bool               `json:"new_session"`
		SessionID           *uuid.UUID         `json:"session_id,omitempty"`
		ProjectID           uuid.UUID          `json:"project_id"`
		AgentID             uuid.UUID          `json:"agent_id"`
		RequestedByMemberID uuid.UUID          `json:"requested_by_member_id"`
		InputText           string             `json:"input_text"`
		ContextSources      []ContextSourceRef `json:"context_sources"`
		RequestedDeadline   *time.Time         `json:"requested_deadline,omitempty"`
		Title               *string            `json:"title,omitempty"`
	}{
		NewSession: command.NewSession, SessionID: command.SessionID,
		ProjectID: command.ProjectID, AgentID: command.AgentID,
		RequestedByMemberID: command.RequestedByMemberID, InputText: command.InputText,
		ContextSources: contextSources, RequestedDeadline: command.RequestedDeadline,
		Title: command.Title,
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(encoded)
	return fmt.Sprintf("%x", sum[:]), nil
}

// CreateProjectChatInput contains the authenticated first-turn command.
type CreateProjectChatInput struct {
	ProjectID      uuid.UUID
	AgentID        uuid.UUID
	Actor          ChatActor
	Message        string
	Title          *string
	ContextSources []ContextSourceRef
	IdempotencyKey string
	DeadlineAt     *time.Time
}

// AppendProjectChatTurnInput contains an authenticated follow-up command.
type AppendProjectChatTurnInput struct {
	ProjectID      uuid.UUID
	SessionID      uuid.UUID
	Actor          ChatActor
	Message        string
	IdempotencyKey string
	DeadlineAt     *time.Time
}

// StopProjectChatTurnInput identifies an owner-requested turn cancellation.
type StopProjectChatTurnInput struct {
	ProjectID uuid.UUID
	SessionID uuid.UUID
	TurnID    uuid.UUID
	Actor     ChatActor
}

// ReplaceChatContextInput replaces the live selection for the next turn.
type ReplaceChatContextInput struct {
	ProjectID uuid.UUID
	SessionID uuid.UUID
	Actor     ChatActor
	Sources   []ContextSourceRef
}

// PrepareProjectConclusionInput requests a frozen task write-back proposal.
type PrepareProjectConclusionInput struct {
	ProjectID            uuid.UUID
	SourceTurnID         uuid.UUID
	TargetTaskID         uuid.UUID
	Actor                ChatActor
	Kind                 ConclusionKind
	RelatedPublicationID *uuid.UUID
	UpdateDescription    bool
	IdempotencyKey       string
	ExpiresAt            time.Time
}

// ConfirmProjectConclusionInput confirms one frozen write-back proposal.
type ConfirmProjectConclusionInput struct {
	ProjectID       uuid.UUID
	PreparationID   uuid.UUID
	Actor           ChatActor
	ExpectedVersion int
	ExpectedSHA256  string
	IdempotencyKey  string
}

// ProjectChatService defines owner-scoped chat and write-back operations.
type ProjectChatService interface {
	ListChatSessions(ctx context.Context, filter ChatSessionListFilter, actor ChatActor) ([]*ChatSessionSummary, bool, error)
	GetChatSession(ctx context.Context, projectID, sessionID uuid.UUID, actor ChatActor) (*AgentChatSession, error)
	GetChatTurn(ctx context.Context, projectID, sessionID, turnID uuid.UUID, actor ChatActor) (*TurnBundle, error)
	ListChatTurns(ctx context.Context, projectID, sessionID uuid.UUID, actor ChatActor, limit int, beforeIndex *int) ([]*TurnBundle, bool, error)
	ListChatTurnEvents(ctx context.Context, filter TurnEventListFilter, actor ChatActor) ([]*AgentConversationEvent, bool, error)
	ListLegacyChatExecutions(ctx context.Context, filter LegacyExecutionListFilter, actor ChatActor) ([]LegacyChatExecution, bool, error)
	ListChatContextSources(ctx context.Context, projectID, sessionID uuid.UUID, actor ChatActor) ([]SessionContextSource, error)
	ReplaceChatContextSources(ctx context.Context, in ReplaceChatContextInput) ([]SessionContextSource, error)
	CreateProjectChat(ctx context.Context, in CreateProjectChatInput) (*TurnBundle, bool, error)
	AppendProjectChatTurn(ctx context.Context, in AppendProjectChatTurnInput) (*TurnBundle, bool, error)
	StopProjectChatTurn(ctx context.Context, in StopProjectChatTurnInput) (*TurnResult, error)
	PrepareProjectConclusion(ctx context.Context, in PrepareProjectConclusionInput) (*ConclusionPreparation, bool, error)
	ConfirmProjectConclusion(ctx context.Context, in ConfirmProjectConclusionInput) (*ConclusionPublicationView, bool, error)
	ListProjectTaskConclusions(ctx context.Context, filter ConclusionPublicationListFilter, actor ChatActor) ([]ConclusionPublicationView, bool, error)
}

// CreateSessionTurnInput is persisted in one transaction. ClientRequestID
// makes a replay return the original session/turn/run instead of duplicating
// any row or dispatch.
type CreateSessionTurnInput struct {
	Session           AgentChatSession
	Conversation      AgentConversation
	Turn              AgentTurn
	Run               TurnRun
	Snapshot          TurnContextSnapshot
	SelectedSources   []SessionContextSource
	ClientRequestID   string
	AuthorizedUserID  uuid.UUID
	LegacyRole        string
	RequestedDeadline *time.Time
	DefaultTimeout    time.Duration
}

// AppendSessionTurnInput atomically appends a later turn after locking the
// owner-private session and returns replayed=true for an idempotent replay.
type AppendSessionTurnInput struct {
	SessionID         uuid.UUID
	ProjectID         uuid.UUID
	MemberID          uuid.UUID
	Conversation      AgentConversation
	ReuseConversation bool
	Turn              AgentTurn
	Run               TurnRun
	Snapshot          TurnContextSnapshot
	AuthorizedUserID  uuid.UUID
	LegacyRole        string
	RequestedDeadline *time.Time
	DefaultTimeout    time.Duration
}

// ClaimTurnRunInput identifies a worker and its requested execution lease.
type ClaimTurnRunInput struct {
	TurnID        uuid.UUID
	WorkerID      string
	LeaseDuration time.Duration
}

// ClaimedTurnRun contains the immutable execution envelope and fencing token.
type ClaimedTurnRun struct {
	Bundle     TurnBundle
	ClaimToken uuid.UUID
}

// RenewTurnRunLeaseInput renews a fenced worker lease.
type RenewTurnRunLeaseInput struct {
	RunID         uuid.UUID
	ClaimToken    uuid.UUID
	LeaseDuration time.Duration
}

// StopOwnerTurnInput identifies an authenticated owner cancellation.
type StopOwnerTurnInput struct {
	ProjectID  uuid.UUID
	SessionID  uuid.UUID
	TurnID     uuid.UUID
	MemberID   uuid.UUID
	UserID     uuid.UUID
	LegacyRole string
}

// StableOutputEventType identifies the single publishable agent output event.
const StableOutputEventType = "agent.turn.output.stable"

// AppendTurnEventInput appends one fenced, sequenced event to a run.
type AppendTurnEventInput struct {
	ID           uuid.UUID
	TurnID       uuid.UUID
	RunID        uuid.UUID
	ClaimToken   uuid.UUID
	TurnSequence int
	EventType    string
	EventSource  string
	Payload      json.RawMessage
	CreatedAt    time.Time
}

// FinalizeTurnInput atomically records the terminal run and turn result.
type FinalizeTurnInput struct {
	RunID              uuid.UUID
	ClaimToken         uuid.UUID
	TerminalStatus     TurnStatus
	StableOutputEvent  *uuid.UUID
	GeneratedByAgentID uuid.UUID
	ErrorCode          *string
	ErrorMessage       *string
	Disposition        RuntimeDisposition
	FinalEventSequence *int
}

// ConclusionKind describes how a publication changes task-visible knowledge.
type ConclusionKind string

// Supported conclusion publication operations.
const (
	ConclusionPublished ConclusionKind = "published"
	ConclusionRevised   ConclusionKind = "revised"
	ConclusionWithdrawn ConclusionKind = "withdrawn"
)

// ConclusionPreparation is the immutable preview awaiting confirmation.
type ConclusionPreparation struct {
	ID                      uuid.UUID
	ProjectID               uuid.UUID
	SourceTurnID            uuid.UUID
	TargetTaskID            uuid.UUID
	PreparedByUserID        uuid.UUID
	PreparedByMemberID      uuid.UUID
	GeneratedByAgentID      uuid.UUID
	Kind                    ConclusionKind
	RelatedPublicationID    *uuid.UUID
	Summary                 string
	SummaryVersion          int
	SummarySHA256           string
	UpdateDescription       bool
	DescriptionBefore       json.RawMessage
	DescriptionBeforeSHA256 string
	DescriptionAfter        json.RawMessage
	DescriptionAfterSHA256  string
	IsFrozen                bool
	State                   string
	IdempotencyKey          string
	RequestSHA256           string
	ExpiresAt               time.Time
	CreatedAt               time.Time
	UpdatedAt               time.Time
}

// PrepareConclusionInput contains repository-level preparation fields.
type PrepareConclusionInput struct {
	ID                   uuid.UUID
	ProjectID            uuid.UUID
	SourceTurnID         uuid.UUID
	TargetTaskID         uuid.UUID
	PreparedByUserID     uuid.UUID
	PreparedByMemberID   uuid.UUID
	LegacyRole           string
	Kind                 ConclusionKind
	RelatedPublicationID *uuid.UUID
	UpdateDescription    bool
	IdempotencyKey       string
	ExpiresAt            time.Time
}

// ConclusionPublication is an immutable task-visible agent conclusion.
type ConclusionPublication struct {
	ID                      uuid.UUID
	ProjectID               uuid.UUID
	TargetTaskID            uuid.UUID
	SourceTurnID            uuid.UUID
	PreparationID           uuid.UUID
	PublishedByUserID       uuid.UUID
	PublishedByMemberID     uuid.UUID
	GeneratedByAgentID      uuid.UUID
	Kind                    ConclusionKind
	RootPublicationID       *uuid.UUID
	RevisesPublicationID    *uuid.UUID
	WithdrawsPublicationID  *uuid.UUID
	Summary                 string
	SummaryVersion          int
	SummarySHA256           string
	DescriptionUpdated      bool
	DescriptionBeforeSHA256 *string
	DescriptionAfterSHA256  *string
	IdempotencyKey          string
	CreatedAt               time.Time
}

// ConfirmConclusionInput contains the optimistic confirmation contract.
type ConfirmConclusionInput struct {
	PreparationID       uuid.UUID
	ProjectID           uuid.UUID
	PublishedByUserID   uuid.UUID
	PublishedByMemberID uuid.UUID
	LegacyRole          string
	ExpectedVersion     int
	ExpectedSHA256      string
	IdempotencyKey      string
}

// OutboxStatus describes durable event delivery state.
type OutboxStatus string

// Supported durable outbox delivery states.
const (
	OutboxPending    OutboxStatus = "pending"
	OutboxPublishing OutboxStatus = "publishing"
	OutboxPublished  OutboxStatus = "published"
	OutboxDead       OutboxStatus = "dead"
)

// OutboxEvent is one durable notification awaiting publication.
type OutboxEvent struct {
	ID             uuid.UUID
	AggregateType  string
	AggregateID    uuid.UUID
	EventType      string
	Payload        json.RawMessage
	IdempotencyKey string
	Status         OutboxStatus
	Attempts       int
	AvailableAt    time.Time
	LockedAt       *time.Time
	LockedBy       *string
	LockToken      *uuid.UUID
	LockExpiresAt  *time.Time
	PublishedAt    *time.Time
	LastError      *string
	CreatedAt      time.Time
}

// OutboxAudience is the minimum routing view used by the outbox publisher.
// It deliberately contains no private input, context snapshot, transcript, or
// stable output. Owner-private turn notifications use ActorUserID; conclusion
// notifications use ProjectID/TaskID and never expose their private source.
type OutboxAudience struct {
	ProjectID   uuid.UUID
	ActorUserID *uuid.UUID
	SessionID   *uuid.UUID
	TurnID      *uuid.UUID
	TaskID      *uuid.UUID
}

// TurnRepository defines durable authoritative turn and publication storage.
type TurnRepository interface {
	ListOwnerChatSessions(ctx context.Context, filter ChatSessionListFilter) ([]*ChatSessionSummary, bool, error)
	GetOwnerChatSession(ctx context.Context, projectID, sessionID, memberID uuid.UUID) (*AgentChatSession, error)
	GetOwnerCreatedChatByRequest(ctx context.Context, projectID, memberID uuid.UUID, clientRequestID string) (*TurnBundle, error)
	ListOwnerSessionTurns(ctx context.Context, projectID, sessionID, memberID uuid.UUID, limit int, beforeIndex *int) ([]*TurnBundle, bool, error)
	GetOwnerTurn(ctx context.Context, projectID, turnID, memberID uuid.UUID) (*TurnBundle, error)
	GetOwnerSessionTurnByIdempotency(ctx context.Context, projectID, sessionID, memberID uuid.UUID, idempotencyKey string) (*TurnBundle, error)
	GetTurnRuntime(ctx context.Context, turnID uuid.UUID) (*TurnBundle, error)
	ListOwnerTurnEvents(ctx context.Context, filter TurnEventListFilter) ([]*AgentConversationEvent, bool, error)
	ListOwnerSessionLegacyExecutions(ctx context.Context, filter LegacyExecutionListFilter) ([]LegacyChatExecution, bool, error)
	ListSessionContextSources(ctx context.Context, projectID, sessionID, memberID uuid.UUID) ([]SessionContextSource, error)
	ReplaceSessionContextSources(ctx context.Context, projectID, sessionID, memberID, userID uuid.UUID, legacyRole string, sources []SessionContextSource) ([]SessionContextSource, error)
	ResolveContextItems(ctx context.Context, projectID, memberID, snapshotID uuid.UUID, sources []SessionContextSource) ([]TurnContextItem, error)
	ListTaskConclusionPublications(ctx context.Context, filter ConclusionPublicationListFilter) ([]ConclusionPublicationView, bool, error)
	GetOwnerConclusionPreparation(ctx context.Context, projectID, preparationID, memberID, userID uuid.UUID) (*ConclusionPreparation, error)
	CreateSessionTurn(ctx context.Context, in CreateSessionTurnInput) (bundle *TurnBundle, replayed bool, err error)
	AppendSessionTurn(ctx context.Context, in AppendSessionTurnInput) (bundle *TurnBundle, replayed bool, err error)
	StopOwnerTurn(ctx context.Context, in StopOwnerTurnInput) (*TurnResult, error)
	ClaimTurnRun(ctx context.Context, in ClaimTurnRunInput) (*ClaimedTurnRun, error)
	ExpireDueTurns(ctx context.Context, limit int) (int, error)
	RenewTurnRunLease(ctx context.Context, in RenewTurnRunLeaseInput) (time.Time, error)
	AppendTurnEvent(ctx context.Context, in AppendTurnEventInput) (*AgentConversationEvent, error)
	FinalizeTurn(ctx context.Context, in FinalizeTurnInput) (*TurnResult, error)
	PrepareConclusion(ctx context.Context, in PrepareConclusionInput) (*ConclusionPreparation, bool, error)
	ConfirmConclusion(ctx context.Context, in ConfirmConclusionInput) (*ConclusionPublication, bool, error)
	ClaimOutbox(ctx context.Context, workerID string, limit int, lease time.Duration) ([]*OutboxEvent, error)
	RenewOutboxLease(ctx context.Context, eventID, lockToken uuid.UUID, lease time.Duration) (time.Time, error)
	MarkOutboxPublished(ctx context.Context, eventID, lockToken uuid.UUID, at time.Time) error
	RetryOutbox(ctx context.Context, eventID, lockToken uuid.UUID, next time.Time, lastError string, dead bool) error
	ResolveOutboxAudience(ctx context.Context, event *OutboxEvent) (*OutboxAudience, error)
}

// Authoritative turn, context, idempotency, and publication errors.
var (
	ErrTurnNotFound             = errors.New("agent turn: not found")
	ErrTurnBusy                 = errors.New("agent turn: session has an active turn")
	ErrTurnClaimLost            = errors.New("agent turn: run claim was lost")
	ErrTurnDeadlineExceeded     = errors.New("agent turn: deadline exceeded")
	ErrTurnEventInvalid         = errors.New("agent turn: event is invalid")
	ErrTurnAlreadyFinalized     = errors.New("agent turn: already finalized")
	ErrTurnAuthorizationRevoked = errors.New("agent turn: execution authorization revoked")
	ErrTurnResultNotPublishable = errors.New("agent turn: no successful stable output")
	ErrContextSourceForbidden   = errors.New("agent context source: not found")
	ErrContextSnapshotTooLarge  = errors.New("agent context snapshot: bounds exceeded")
	ErrConclusionNotFound       = errors.New("agent conclusion: not found")
	ErrConclusionConflict       = errors.New("agent conclusion: stale or conflicting preparation")
	ErrConclusionExpired        = errors.New("agent conclusion: preparation expired")
	ErrConclusionNotFrozen      = errors.New("agent conclusion: summary is not frozen")
	ErrIdempotencyConflict      = errors.New("idempotency key was reused with a different request")
	ErrProjectChatForbidden     = errors.New("agent project chat: forbidden")
	ErrProjectChatInvalid       = errors.New("agent project chat: invalid request")
)
