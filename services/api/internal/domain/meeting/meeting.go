// Package meetingdom defines the meeting assistant: the settings of a
// "meeting" agent, the assistant's presence in a call, the transcript it
// keeps, and the proposals it makes for people to approve. See
// docs/architecture/chat-and-meetings.md (Meeting agent type, Forwarded tool
// calls).
package meetingdom

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
)

// Errors.
var (
	// ErrNotMeetingAgent means the agent is not of type "meeting".
	ErrNotMeetingAgent = errors.New("agent is not a meeting assistant")
	// ErrAssistantsDisabled means the instance has meeting assistants off:
	// calls are not configured, or the worker secret is not set.
	ErrAssistantsDisabled = errors.New("meeting assistants are not enabled")
	// ErrNoOpenCall means there is no call to invite the assistant into.
	ErrNoOpenCall = errors.New("the channel has no open call")
	// ErrAlreadyInCall means the assistant is already in the call.
	ErrAlreadyInCall = errors.New("the assistant is already in the call")
	// ErrNotInCall means the assistant is not in the call.
	ErrNotInCall = errors.New("the assistant is not in the call")
	// ErrAssistantNotConfigured means the assistant lacks the model settings it
	// needs to run (reasoning model, and a transcription model unless it only
	// takes notes).
	ErrAssistantNotConfigured = errors.New("the assistant is missing model settings")
	// ErrModelRequired means notes or suggestions are on but the agent has no
	// language model configured.
	ErrModelRequired = errors.New("notes and suggestions need the assistant's model settings")
	// ErrSettingsInvalid means a settings value is out of range or unknown.
	ErrSettingsInvalid = errors.New("meeting settings are invalid")
	// ErrMeetingNotFound means the call is not one of this channel's.
	ErrMeetingNotFound = errors.New("meeting not found")
	// ErrProposalNotFound means the proposal does not exist.
	ErrProposalNotFound = errors.New("proposal not found")
	// ErrProposalInvalid means the kind is unknown or the payload does not fit it.
	ErrProposalInvalid = errors.New("proposal is invalid")
	// ErrProposalKindDisabled means the assistant's settings do not enable the kind.
	ErrProposalKindDisabled = errors.New("this kind of proposal is not enabled for the assistant")
	// ErrProposalLimit means the assistant made as many proposals in this call
	// as its settings allow.
	ErrProposalLimit = errors.New("the assistant reached its proposal limit for this call")
	// ErrProposalClosed means the proposal is no longer open for this change.
	ErrProposalClosed = errors.New("the proposal is already closed")
	// ErrWorkerUnauthorized means the worker secret was missing or wrong.
	ErrWorkerUnauthorized = errors.New("meeting worker is not authorized")
)

// Modes of an assistant. Transcription (and the notes at the end) is always on;
// the mode says whether the assistant also suggests tasks and docs, which its
// owner turns on.
const (
	// ModeNotesOnly transcribes and writes notes and never suggests. The default.
	ModeNotesOnly = "notes_only"
	// ModeOnDemand also suggests when someone asks ("Suggest tasks").
	ModeOnDemand = "on_demand"
	// ModeAutoSuggest also suggests every suggest_interval_seconds.
	ModeAutoSuggest = "auto_suggest"
)

// SummaryTarget values.
const (
	SummaryNone           = "none"
	SummaryChannelMessage = "channel_message"
)

// DefaultTarget values: who gets the proposals an assistant makes unasked.
const (
	TargetRequester = "requester"
	TargetHost      = "host"
)

// Proposal kinds (v1.0.0). A new kind needs a payload schema in ValidatePayload,
// a card in the web client and a mapping to existing endpoints.
const (
	KindTaskCreate         = "task.create"
	KindTaskUpdate         = "task.update"
	KindTaskComment        = "task.comment"
	KindDocCreateFromNotes = "doc.create_from_notes"
)

// ProposalKinds lists every kind, in display order.
var ProposalKinds = []string{KindTaskCreate, KindTaskUpdate, KindTaskComment, KindDocCreateFromNotes}

// Proposal statuses.
const (
	StatusPending  = "pending"
	StatusClaimed  = "claimed"
	StatusResolved = "resolved"
	StatusRejected = "rejected"
	StatusExpired  = "expired"
	StatusFailed   = "failed"
)

// Agent session statuses.
const (
	SessionDispatched = "dispatched"
	SessionActive     = "active"
	SessionLeft       = "left"
	SessionFailed     = "failed"
)

// Settings are the listening and behaviour settings of one meeting agent.
// STTAPIKeySecret is the encrypted key; it is never returned by the API.
type Settings struct {
	AgentID                 uuid.UUID
	STTProvider             string
	STTModel                string
	STTAPIKeySecret         string
	STTBaseURL              string
	STTLanguages            []string
	Mode                    string
	SuggestIntervalSeconds  int
	ProposalKinds           []string
	MaxProposalsPerCall     int
	StoreTranscript         bool
	TranscriptRetentionDays int
	Announcement            string
	SummaryTarget           string
	DefaultTarget           string
	UpdatedAt               time.Time
}

// NeedsModel reports whether the assistant uses a language model: for the notes
// it writes after a call, or for suggesting tasks and docs. Transcription alone
// needs only the transcription provider.
func (s *Settings) NeedsModel() bool {
	return s.Mode != ModeNotesOnly || s.SummaryTarget != SummaryNone
}

// HasSTTKey reports whether a transcription key is stored.
func (s *Settings) HasSTTKey() bool { return s.STTAPIKeySecret != "" }

// DefaultSettings returns what an agent that never saved settings runs with.
func DefaultSettings(agentID uuid.UUID) *Settings {
	return &Settings{
		AgentID:                 agentID,
		STTLanguages:            []string{},
		Mode:                    ModeNotesOnly,
		SuggestIntervalSeconds:  120,
		ProposalKinds:           append([]string(nil), ProposalKinds...),
		MaxProposalsPerCall:     10,
		StoreTranscript:         true,
		TranscriptRetentionDays: 30,
		SummaryTarget:           SummaryNone,
		DefaultTarget:           TargetRequester,
	}
}

// SettingsPatch changes some settings; nil fields stay as they are. STTAPIKey
// is plaintext; the service encrypts it, and an empty string clears it.
type SettingsPatch struct {
	STTProvider             *string
	STTModel                *string
	STTAPIKey               *string
	STTBaseURL              *string
	STTLanguages            *[]string
	Mode                    *string
	SuggestIntervalSeconds  *int
	ProposalKinds           *[]string
	MaxProposalsPerCall     *int
	StoreTranscript         *bool
	TranscriptRetentionDays *int
	Announcement            *string
	SummaryTarget           *string
	DefaultTarget           *string
}

// AgentSession is an assistant's presence in a call.
type AgentSession struct {
	ID            uuid.UUID
	CallSessionID uuid.UUID
	ChannelID     uuid.UUID
	ProjectID     uuid.UUID
	AgentID       uuid.UUID
	AgentName     string
	InvitedBy     *uuid.UUID
	DispatchID    string
	Status        string
	JoinedAt      time.Time
	LeftAt        *time.Time
	// Mode and the privacy summary are copied from the settings so the call
	// view can say what the assistant does without another request.
	Mode            string
	SummaryTarget   string
	StoreTranscript bool
	RetentionDays   int
	Announcement    string
}

// Segment is one finalized turn of speech.
type Segment struct {
	ID            uuid.UUID
	CallSessionID uuid.UUID
	SpeakerID     *uuid.UUID
	Text          string
	SpokenAt      time.Time
}

// NamedSegment is a stored turn of speech with who said it, for reading.
type NamedSegment struct {
	SpeakerID   *uuid.UUID
	SpeakerName string
	Text        string
	SpokenAt    time.Time
}

// MeetingSummary is one call of a channel that left a transcript or a
// recording behind.
type MeetingSummary struct {
	CallID    uuid.UUID
	StartedAt time.Time
	EndedAt   *time.Time
	Segments  int
	// Speakers are the display names of the people who spoke, in order of first
	// words.
	Speakers []string
}

// SegmentInput is a turn the worker reports.
type SegmentInput struct {
	SpeakerID uuid.UUID
	Text      string
	SpokenAt  time.Time
}

// Proposal is something an assistant suggests for a person to approve.
type Proposal struct {
	ID              uuid.UUID
	ProjectID       uuid.UUID
	ChannelID       *uuid.UUID
	CallSessionID   *uuid.UUID
	AgentAccountID  uuid.UUID
	AgentName       string
	TargetAccountID uuid.UUID
	Kind            string
	Payload         []byte // JSON, validated per kind
	Status          string
	Result          []byte // JSON
	ResolvedBy      *uuid.UUID
	ExpiresAt       time.Time
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// NewProposal is what an assistant sends to propose something.
type NewProposal struct {
	ProjectID uuid.UUID
	AgentID   uuid.UUID
	// ChannelID names the channel whose call the proposal belongs to; the
	// service finds the call and its requester from it.
	ChannelID uuid.UUID
	Kind      string
	Payload   []byte
	// TargetID, when set and a participant of the call, overrides the default
	// target (the assistant names who asked).
	TargetID *uuid.UUID
}

// WorkerBootstrap is what the worker needs to run one job: decrypted settings
// and keys (held in the job's memory only) and a short-lived API key of the
// agent's own account.
type WorkerBootstrap struct {
	SessionID uuid.UUID
	ProjectID uuid.UUID
	ChannelID uuid.UUID
	CallID    uuid.UUID
	AgentID   uuid.UUID
	AgentName string
	// RequesterID is who invited the assistant; HostID who started the call.
	RequesterID *uuid.UUID
	HostID      *uuid.UUID

	Instructions string
	LLMProvider  string
	LLMModel     string
	LLMAPIKey    string
	LLMBaseURL   string

	Settings  *Settings
	STTAPIKey string
	// APIKey authenticates the worker as the agent against the Paca API; it
	// expires when the session ends.
	APIKey    string
	ExpiresAt time.Time
}

// Repository stores meeting data.
type Repository interface {
	// AgentForMeeting returns the agent's type and whether it is visible in the
	// project (its own, or a global agent invited into it).
	AgentInfo(ctx context.Context, projectID, agentID uuid.UUID) (*AgentInfo, error)
	GetSettings(ctx context.Context, agentID uuid.UUID) (*Settings, error)
	SaveSettings(ctx context.Context, s *Settings) error

	// OpenCall returns the id of the channel's open call session, or uuid.Nil
	// with the host (starter) when there is none.
	OpenCall(ctx context.Context, channelID uuid.UUID) (id uuid.UUID, host *uuid.UUID, err error)
	// ChannelProject returns the project of a channel (uuid.Nil for a direct
	// message) and whether it exists.
	ChannelProject(ctx context.Context, channelID uuid.UUID) (uuid.UUID, bool, error)
	IsCallParticipant(ctx context.Context, callID, accountID uuid.UUID) (bool, error)
	// WasCallParticipant reports whether the account is, or at any point was, in
	// the call: speech is attributed to someone who has since left, and the last
	// turns are flushed after the people have gone.
	WasCallParticipant(ctx context.Context, callID, accountID uuid.UUID) (bool, error)

	CreateAgentSession(ctx context.Context, s *AgentSession) error
	SetAgentSessionDispatch(ctx context.Context, id uuid.UUID, dispatchID string) error
	SetAgentSessionStatus(ctx context.Context, id uuid.UUID, status string) error
	FindAgentSession(ctx context.Context, id uuid.UUID) (*AgentSession, error)
	// OpenAgentSession returns the assistant's open session in the channel's call.
	OpenAgentSession(ctx context.Context, channelID, agentID uuid.UUID) (*AgentSession, error)
	ListAgentSessions(ctx context.Context, channelID uuid.UUID) ([]*AgentSession, error)
	// EndAgentSessions closes every open session of the call (the call ended).
	EndAgentSessions(ctx context.Context, callID uuid.UUID) error
	// ListMeetingAgents returns the meeting agents of a project with their
	// privacy summary, for the invite picker.
	ListMeetingAgents(ctx context.Context, projectID uuid.UUID) ([]*MeetingAgent, error)

	// AgentAPIKey registers a key of the agent's account (hash only) that
	// expires at the given time, and revokes the previous keys of its session.
	CreateWorkerKey(ctx context.Context, agentID, sessionID uuid.UUID, hash, prefix string, expires time.Time) error
	RevokeWorkerKeys(ctx context.Context, sessionID uuid.UUID) error

	AppendSegments(ctx context.Context, callID, channelID uuid.UUID, retentionDays int, segs []SegmentInput) error
	ListSegments(ctx context.Context, callID uuid.UUID, limit int) ([]*Segment, error)
	DeleteExpiredSegments(ctx context.Context) (int64, error)
	// ListMeetings returns the calls of a channel that have a transcript, newest first.
	ListMeetings(ctx context.Context, channelID uuid.UUID, limit int) ([]*MeetingSummary, error)
	// ListNamedSegments returns a call's transcript, oldest first, only when the
	// call belongs to the channel (ErrMeetingNotFound otherwise).
	ListNamedSegments(ctx context.Context, channelID, callID uuid.UUID, limit int) ([]*NamedSegment, error)

	CountProposals(ctx context.Context, callID, agentID uuid.UUID) (int, error)
	CreateProposal(ctx context.Context, p *Proposal) error
	GetProposal(ctx context.Context, projectID, id uuid.UUID) (*Proposal, error)
	ListProposals(ctx context.Context, projectID uuid.UUID, channelID *uuid.UUID, target *uuid.UUID, limit int) ([]*Proposal, error)
	// TransitionProposal moves a proposal from one of the given statuses to
	// another, in one statement, and reports whether it did.
	TransitionProposal(ctx context.Context, id uuid.UUID, from []string, to string, by *uuid.UUID, result []byte) (bool, error)
	ExpireProposals(ctx context.Context) (int64, error)
}

// AgentInfo is what the service needs to know about an agent.
type AgentInfo struct {
	ID           uuid.UUID
	Name         string
	Type         string
	Instructions string
	LLMProvider  string
	LLMModel     string
	LLMKeySecret string
	LLMBaseURL   string
}

// MeetingAgent is a meeting agent as the invite picker lists it.
type MeetingAgent struct {
	ID              uuid.UUID
	Name            string
	Handle          string
	Description     string
	Mode            string
	SummaryTarget   string
	StoreTranscript bool
	RetentionDays   int
}

// Notifier announces changes to realtime. A lost announcement costs a client
// one refetch.
type Notifier interface {
	AssistantChanged(ctx context.Context, channelID uuid.UUID)
	ProposalChanged(ctx context.Context, channelID *uuid.UUID, targetID uuid.UUID, proposalID uuid.UUID)
}

// Service is the meeting-assistant use cases. None decides authorization: the
// router gates each route (the worker routes prove themselves with the worker
// secret).
type Service interface {
	// Enabled reports whether assistants can run on this instance.
	Enabled() bool
	GetSettings(ctx context.Context, projectID, agentID uuid.UUID) (*Settings, error)
	UpdateSettings(ctx context.Context, projectID, agentID uuid.UUID, p SettingsPatch) (*Settings, error)
	// ValidateListening checks that a patch carries what a meeting agent needs
	// to listen: a transcription provider and model, and a key or an endpoint.
	// Creating a meeting agent requires it.
	ValidateListening(p SettingsPatch) error
	// NeedsModel reports whether the settings a new agent is created with turn on
	// notes or suggestions, which need its language model.
	NeedsModel(p SettingsPatch) bool
	ListMeetingAgents(ctx context.Context, projectID uuid.UUID) ([]*MeetingAgent, error)

	Invite(ctx context.Context, projectID, channelID, agentID, invitedBy uuid.UUID) (*AgentSession, error)
	Remove(ctx context.Context, projectID, channelID, agentID uuid.UUID) error
	ListAssistants(ctx context.Context, projectID, channelID uuid.UUID) ([]*AgentSession, error)
	// CallEnded closes the assistants of a call that ended.
	CallEnded(ctx context.Context, channelID uuid.UUID)

	// Worker routes.
	Bootstrap(ctx context.Context, secret string, sessionID uuid.UUID) (*WorkerBootstrap, error)
	AppendTranscript(ctx context.Context, secret string, sessionID uuid.UUID, segs []SegmentInput) error
	Transcript(ctx context.Context, secret string, sessionID uuid.UUID, limit int) ([]*Segment, error)
	Finish(ctx context.Context, secret string, sessionID uuid.UUID) error

	// ListMeetings and MeetingTranscript are what people read: the calls of a
	// channel that were transcribed, and one call's transcript. The router
	// gates both on reading the channel.
	ListMeetings(ctx context.Context, channelID uuid.UUID) ([]*MeetingSummary, error)
	MeetingTranscript(ctx context.Context, channelID, callID uuid.UUID) ([]*NamedSegment, error)

	CreateProposal(ctx context.Context, in NewProposal) (*Proposal, error)
	GetProposal(ctx context.Context, projectID, id uuid.UUID) (*Proposal, error)
	ListProposals(ctx context.Context, projectID uuid.UUID, channelID *uuid.UUID, target *uuid.UUID) ([]*Proposal, error)
	Claim(ctx context.Context, projectID, id, by uuid.UUID) (*Proposal, error)
	Resolve(ctx context.Context, projectID, id, by uuid.UUID, status string, result []byte) (*Proposal, error)
}
