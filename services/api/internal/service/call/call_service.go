// Package callsvc implements calls: joining a channel's LiveKit room, keeping
// the participant list in step with LiveKit's webhooks, and moderation.
package callsvc

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	calldom "github.com/Paca-AI/api/internal/domain/call"
	chatdom "github.com/Paca-AI/api/internal/domain/chat"
	"github.com/Paca-AI/api/internal/platform/livekit"
)

// TokenTTL is how long a join token is good for connecting; LiveKit keeps a
// connected participant in the room after it expires.
const TokenTTL = 10 * time.Minute

// Room is the LiveKit part of the service.
type Room interface {
	Enabled() bool
	PublicURL() string
	MintToken(id livekit.Identity, g livekit.Grant, ttl time.Duration) (string, time.Time, error)
	VerifyWebhook(authHeader string, body []byte) (*livekit.WebhookEvent, error)
	RemoveParticipant(ctx context.Context, room, identity string) error
	MuteParticipant(ctx context.Context, room, identity string) error
}

// Service implements calldom.Service.
type Service struct {
	repo     calldom.Repository
	room     Room
	notifier calldom.Notifier
}

// New returns a Service.
func New(repo calldom.Repository, room Room) *Service { return &Service{repo: repo, room: room} }

// WithNotifier announces call changes to realtime.
func (s *Service) WithNotifier(n calldom.Notifier) *Service {
	s.notifier = n
	return s
}

var _ calldom.Service = (*Service)(nil)

// Enabled implements calldom.Service.
func (s *Service) Enabled() bool { return s.room != nil && s.room.Enabled() }

func (s *Service) channel(ctx context.Context, projectID, channelID uuid.UUID) (*calldom.Channel, error) {
	if !s.Enabled() {
		return nil, calldom.ErrDisabled
	}
	return s.repo.FindChannel(ctx, projectID, channelID)
}

// Join implements calldom.Service. The router has already checked calls:join;
// the grant here is publish and subscribe for everyone who may join.
func (s *Service) Join(ctx context.Context, projectID, channelID, accountID uuid.UUID) (*calldom.JoinInfo, error) {
	ch, err := s.channel(ctx, projectID, channelID)
	if err != nil {
		return nil, err
	}
	if ch.Archived {
		return nil, chatdom.ErrChannelArchived
	}
	name, kind, err := s.repo.AccountName(ctx, accountID)
	if err != nil {
		return nil, err
	}
	if _, err := s.repo.OpenSession(ctx, channelID, accountID); err != nil {
		return nil, err
	}
	room := channelID.String()
	tok, exp, err := s.room.MintToken(
		livekit.Identity{ID: accountID.String(), Name: name, Metadata: fmt.Sprintf(`{"kind":%q}`, kind)},
		livekit.Grant{Room: room, CanPublish: true, CanSubscribe: true}, TokenTTL)
	if err != nil {
		return nil, err
	}
	return &calldom.JoinInfo{URL: s.room.PublicURL(), Token: tok, Room: room, ExpiresAt: exp}, nil
}

// GetCall implements calldom.Service.
func (s *Service) GetCall(ctx context.Context, projectID, channelID uuid.UUID) (*calldom.Call, error) {
	if _, err := s.repo.FindChannel(ctx, projectID, channelID); err != nil {
		return nil, err
	}
	c, err := s.repo.ListCall(ctx, channelID)
	if err != nil {
		return nil, err
	}
	if c == nil {
		c = &calldom.Call{ChannelID: channelID, Participants: []calldom.Participant{}}
	}
	return c, nil
}

// ListCalls implements calldom.Service.
func (s *Service) ListCalls(ctx context.Context, projectID uuid.UUID) ([]*calldom.Call, error) {
	return s.repo.ListCalls(ctx, projectID)
}

func (s *Service) moderate(ctx context.Context, projectID, channelID, actorID, targetID uuid.UUID,
	act func(ctx context.Context, room, identity string) error) error {
	if _, err := s.channel(ctx, projectID, channelID); err != nil {
		return err
	}
	if actorID == targetID {
		return calldom.ErrSelfModerate
	}
	call, err := s.repo.ListCall(ctx, channelID)
	if err != nil {
		return err
	}
	in := false
	if call != nil {
		for _, p := range call.Participants {
			if p.AccountID == targetID {
				in = true
				break
			}
		}
	}
	if !in {
		return calldom.ErrNotInCall
	}
	if err := act(ctx, channelID.String(), targetID.String()); err != nil {
		if errors.Is(err, livekit.ErrNotFound) {
			return calldom.ErrNotInCall
		}
		return err
	}
	return nil
}

// Mute implements calldom.Service.
func (s *Service) Mute(ctx context.Context, projectID, channelID, actorID, targetID uuid.UUID) error {
	return s.moderate(ctx, projectID, channelID, actorID, targetID, s.room.MuteParticipant)
}

// Remove implements calldom.Service. The participant row is closed by LiveKit's
// participant_left webhook; it is closed here too so the list does not lag.
func (s *Service) Remove(ctx context.Context, projectID, channelID, actorID, targetID uuid.UUID) error {
	err := s.moderate(ctx, projectID, channelID, actorID, targetID, s.room.RemoveParticipant)
	if err != nil {
		return err
	}
	if err := s.repo.ParticipantLeft(ctx, channelID, targetID); err != nil {
		slog.Warn("calls: close removed participant", "channel_id", channelID, "err", err)
	}
	s.announce(ctx, channelID)
	return nil
}

// HandleWebhook implements calldom.Service. Events for rooms that are not a
// channel id are ignored (the LiveKit server may serve other rooms).
func (s *Service) HandleWebhook(ctx context.Context, authHeader string, body []byte) error {
	ev, err := s.room.VerifyWebhook(authHeader, body)
	if err != nil {
		return err
	}
	channelID, err := uuid.Parse(ev.Room.Name)
	if err != nil {
		return nil
	}
	switch ev.Event {
	case livekit.EventRoomStarted:
		if err := s.repo.SetRoomSID(ctx, channelID, ev.Room.SID); err != nil {
			return err
		}
	case livekit.EventParticipantJoined:
		accountID, err := uuid.Parse(ev.Participant.Identity)
		if err != nil {
			return nil
		}
		if err := s.repo.ParticipantJoined(ctx, channelID, accountID); err != nil {
			return err
		}
	case livekit.EventParticipantLeft:
		accountID, err := uuid.Parse(ev.Participant.Identity)
		if err != nil {
			return nil
		}
		if err := s.repo.ParticipantLeft(ctx, channelID, accountID); err != nil {
			return err
		}
	case livekit.EventRoomFinished:
		if err := s.repo.EndSession(ctx, channelID); err != nil {
			return err
		}
	default:
		return nil
	}
	s.announce(ctx, channelID)
	return nil
}

func (s *Service) announce(ctx context.Context, channelID uuid.UUID) {
	if s.notifier == nil {
		return
	}
	s.notifier.CallChanged(ctx, uuid.Nil, channelID)
}
