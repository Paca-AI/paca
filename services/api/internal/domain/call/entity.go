// Package calldom defines voice and video calls: a LiveKit room per channel
// and who is in it. See docs/architecture/chat-and-meetings.md.
package calldom

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
)

// Errors.
var (
	// ErrDisabled means calls are not configured on this deployment.
	ErrDisabled = errors.New("calls are not enabled")
	// ErrNotInCall means the person to moderate is not in the call.
	ErrNotInCall = errors.New("participant is not in the call")
	// ErrSelfModerate means a moderator tried to remove themselves.
	ErrSelfModerate = errors.New("use leave to remove yourself from the call")
	// ErrRecordingDisabled means recording is not set up here: LiveKit's egress
	// service is not running or storage is not configured.
	ErrRecordingDisabled = errors.New("recording is not enabled")
	// ErrRecordingActive means the call is already being recorded.
	ErrRecordingActive = errors.New("the call is already being recorded")
	// ErrNoRecording means no recording is running in the call.
	ErrNoRecording = errors.New("the call is not being recorded")
	// ErrNoOpenCall means there is no call to record.
	ErrNoOpenCall = errors.New("the channel has no open call")
	// ErrRecordingNotFound means the recording is not one of this channel's, or
	// has no file (yet).
	ErrRecordingNotFound = errors.New("recording not found")
	// ErrRemoved means a moderator removed the account from the call recently
	// and it may not rejoin yet.
	ErrRemoved = errors.New("you were removed from this call")
)

// Participant is an account connected to a call.
type Participant struct {
	AccountID uuid.UUID
	Name      string
	// Kind is the account kind: user, agent or service_account.
	Kind     string
	JoinedAt time.Time
}

// Recording statuses.
const (
	RecordingStarting = "starting"
	RecordingActive   = "recording"
	RecordingStopping = "stopping"
	RecordingComplete = "complete"
	RecordingFailed   = "failed"
)

// Recording is a LiveKit egress that composes a call into one MP4 file.
type Recording struct {
	ID         uuid.UUID
	CallID     uuid.UUID
	ChannelID  uuid.UUID
	EgressID   string
	Status     string
	StartedBy  *uuid.UUID
	StartedAt  time.Time
	EndedAt    *time.Time
	ObjectKey  string
	SizeBytes  int64
	DurationMs int64
	Error      string
}

// Open reports whether the recording is still going (everyone in the call
// should see it).
func (r *Recording) Open() bool {
	return r.Status == RecordingStarting || r.Status == RecordingActive || r.Status == RecordingStopping
}

// Call is the open call of a channel.
type Call struct {
	ChannelID    uuid.UUID
	StartedAt    time.Time
	Participants []Participant
	// Recording is the recording running in the call, or nil.
	Recording *Recording
}

// JoinInfo is what a client needs to connect.
type JoinInfo struct {
	URL       string
	Token     string
	Room      string
	ExpiresAt time.Time
}

// Channel is the part of a channel a call needs.
type Channel struct {
	ID        uuid.UUID
	ProjectID uuid.UUID // uuid.Nil for a direct message
	Archived  bool
}

// Repository stores calls.
type Repository interface {
	// FindChannel returns the channel of a route (projectID uuid.Nil for a
	// direct message), or ErrChannelNotFound-equivalent chatdom error.
	FindChannel(ctx context.Context, projectID, channelID uuid.UUID) (*Channel, error)
	// AccountName returns the display name of an account.
	AccountName(ctx context.Context, accountID uuid.UUID) (name, kind string, err error)
	// OpenSession returns the id of the channel's open session, creating one
	// when none is open.
	OpenSession(ctx context.Context, channelID, startedBy uuid.UUID) (uuid.UUID, error)
	// SetRoomSID records LiveKit's id for the room of a channel's open session.
	SetRoomSID(ctx context.Context, channelID uuid.UUID, sid string) error
	// ParticipantJoined records an account in the channel's open call
	// (idempotent), opening the session if needed. sid is LiveKit's id of the
	// connection; a newer connection of the same account replaces the old one.
	// A channel that no longer exists is ignored.
	ParticipantJoined(ctx context.Context, channelID, accountID uuid.UUID, sid string) error
	// ParticipantLeft records a connection leaving the channel's open call. A
	// non-empty sid closes the row only while it still belongs to that
	// connection, so a late "left" of a replaced connection is a no-op; an
	// empty sid closes the account's row whatever its connection.
	ParticipantLeft(ctx context.Context, channelID, accountID uuid.UUID, sid string) error
	// BanRemoved bars an account from the channel's call until the given time.
	BanRemoved(ctx context.Context, channelID, accountID uuid.UUID, until time.Time) error
	// IsRemoved reports whether the account is currently barred.
	IsRemoved(ctx context.Context, channelID, accountID uuid.UUID) (bool, error)
	// EndSession closes the channel's open session and every participant row.
	EndSession(ctx context.Context, channelID uuid.UUID) error
	// ListCall returns the open call of a channel, or nil.
	ListCall(ctx context.Context, channelID uuid.UUID) (*Call, error)
	// CreateRecording stores a new recording of the channel's open call; a call
	// that is already being recorded is ErrRecordingActive, a channel with no
	// open call ErrNoOpenCall.
	CreateRecording(ctx context.Context, channelID, startedBy uuid.UUID) (*Recording, error)
	SetRecordingEgress(ctx context.Context, id uuid.UUID, egressID string) error
	// FailRecording closes a recording that could not start.
	FailRecording(ctx context.Context, id uuid.UUID, reason string) error
	// UpdateRecordingByEgress applies an egress event; it reports the channel the
	// recording belongs to, or uuid.Nil when the egress is not one of ours.
	UpdateRecordingByEgress(ctx context.Context, egressID, status, objectKey string, sizeBytes, durationMs int64, errMsg string) (uuid.UUID, error)
	// OpenRecording returns the call's running recording, or nil.
	OpenRecording(ctx context.Context, channelID uuid.UUID) (*Recording, error)
	// ListRecordings returns a channel's recordings, newest first.
	ListRecordings(ctx context.Context, channelID uuid.UUID, limit int) ([]*Recording, error)
	FindRecording(ctx context.Context, channelID, id uuid.UUID) (*Recording, error)
	// ListCalls returns the open calls of a project's channels, narrowed by the
	// caller's scope in ctx (the same scoping as the channel list).
	ListCalls(ctx context.Context, projectID uuid.UUID) ([]*Call, error)
}

// Notifier announces that a channel's call changed. A lost announcement costs
// a client one refetch.
type Notifier interface {
	CallChanged(ctx context.Context, channelID uuid.UUID)
}

// Service is the call use cases. None of them decides authorization: the
// router gates each route.
type Service interface {
	Join(ctx context.Context, projectID, channelID, accountID uuid.UUID) (*JoinInfo, error)
	GetCall(ctx context.Context, projectID, channelID uuid.UUID) (*Call, error)
	ListCalls(ctx context.Context, projectID uuid.UUID) ([]*Call, error)
	Mute(ctx context.Context, projectID, channelID, actorID, targetID uuid.UUID) error
	Remove(ctx context.Context, projectID, channelID, actorID, targetID uuid.UUID) error
	// Recording. The router gates start and stop on calls:record and the reads on
	// the channel.
	RecordingEnabled() bool
	StartRecording(ctx context.Context, projectID, channelID, by uuid.UUID) (*Recording, error)
	StopRecording(ctx context.Context, projectID, channelID uuid.UUID) error
	ListRecordings(ctx context.Context, projectID, channelID uuid.UUID) ([]*Recording, error)
	// RecordingURL returns a short-lived link to play or save a finished recording.
	RecordingURL(ctx context.Context, projectID, channelID, id uuid.UUID, download bool) (string, error)
	// HandleWebhook applies a verified LiveKit webhook.
	HandleWebhook(ctx context.Context, authHeader string, body []byte) error
	Enabled() bool
}
