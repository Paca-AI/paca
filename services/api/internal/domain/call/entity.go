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

// Call is the open call of a channel.
type Call struct {
	ChannelID    uuid.UUID
	StartedAt    time.Time
	Participants []Participant
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
	// HandleWebhook applies a verified LiveKit webhook.
	HandleWebhook(ctx context.Context, authHeader string, body []byte) error
	Enabled() bool
}
