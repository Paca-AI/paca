// Package callsvc implements calls: joining a channel's LiveKit room, keeping
// the participant list in step with LiveKit's webhooks, and moderation.
package callsvc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
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
	StartRoomRecording(ctx context.Context, room, filepath string, out livekit.S3Output) (string, error)
	StopRecording(ctx context.Context, room, egressID string) error
}

// Storage is the part of the object store the recordings need: a link to a
// finished file.
type Storage interface {
	PresignGetObject(ctx context.Context, bucket, key string, ttl time.Duration, contentDisposition string) (string, error)
}

// RecordingConfig says where recordings are written. A recording is possible
// only when the egress service runs (Enabled) and storage is configured.
type RecordingConfig struct {
	Enabled bool
	Bucket  string
	Out     livekit.S3Output
}

// RecordingLinkTTL is how long a playback or download link lasts.
const RecordingLinkTTL = time.Hour

// Service implements calldom.Service.
type Service struct {
	repo     calldom.Repository
	room     Room
	notifier calldom.Notifier
	onEnd    func(ctx context.Context, channelID uuid.UUID)
	storage  Storage
	rec      RecordingConfig
}

// WithRecording enables recordings, written to cfg's bucket and linked through st.
func (s *Service) WithRecording(st Storage, cfg RecordingConfig) *Service {
	s.storage, s.rec = st, cfg
	return s
}

// New returns a Service.
func New(repo calldom.Repository, room Room) *Service { return &Service{repo: repo, room: room} }

// WithNotifier announces call changes to realtime.
func (s *Service) WithNotifier(n calldom.Notifier) *Service {
	s.notifier = n
	return s
}

// WithCallEndHook sets a function run when a channel's call ends, before its
// session is closed (the meeting assistant finds its sessions through it).
func (s *Service) WithCallEndHook(f func(ctx context.Context, channelID uuid.UUID)) *Service {
	s.onEnd = f
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
	if removed, err := s.repo.IsRemoved(ctx, channelID, accountID); err != nil {
		return nil, err
	} else if removed {
		return nil, calldom.ErrRemoved
	}
	name, kind, err := s.repo.AccountName(ctx, accountID)
	if err != nil {
		return nil, err
	}
	meta, err := json.Marshal(map[string]string{"kind": kind})
	if err != nil {
		return nil, err
	}
	if _, err := s.repo.OpenSession(ctx, channelID, accountID); err != nil {
		return nil, err
	}
	room := channelID.String()
	tok, exp, err := s.room.MintToken(
		livekit.Identity{ID: accountID.String(), Name: name, Metadata: string(meta)},
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
	// Everyone in the call sees that it is being recorded.
	if rec, err := s.repo.OpenRecording(ctx, channelID); err != nil {
		return nil, err
	} else if rec != nil {
		c.Recording = rec
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

// Remove implements calldom.Service. LiveKit cannot revoke a token that was
// already handed out, so the account is also barred for as long as such a token
// could still connect (TokenTTL): Join refuses it and the participant_joined
// webhook disconnects it again. The participant row is closed by LiveKit's
// participant_left webhook; it is closed here too so the list does not lag.
func (s *Service) Remove(ctx context.Context, projectID, channelID, actorID, targetID uuid.UUID) error {
	err := s.moderate(ctx, projectID, channelID, actorID, targetID, s.room.RemoveParticipant)
	if err != nil {
		return err
	}
	if err := s.repo.BanRemoved(ctx, channelID, targetID, time.Now().Add(TokenTTL)); err != nil {
		slog.Warn("calls: bar removed participant", "channel_id", channelID, "err", err)
	}
	if err := s.repo.ParticipantLeft(ctx, channelID, targetID, ""); err != nil {
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
	// Egress events name their room inside egressInfo, not in room.
	switch ev.Event {
	case livekit.EventEgressStarted, livekit.EventEgressUpdated, livekit.EventEgressEnded:
		return s.handleEgress(ctx, ev)
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
		if removed, err := s.repo.IsRemoved(ctx, channelID, accountID); err != nil {
			return err
		} else if removed {
			// A moderator removed this account and it reconnected with the
			// token it already held: disconnect it again.
			if err := s.room.RemoveParticipant(ctx, channelID.String(), accountID.String()); err != nil && !errors.Is(err, livekit.ErrNotFound) {
				return err
			}
			return nil
		}
		if err := s.repo.ParticipantJoined(ctx, channelID, accountID, ev.Participant.SID); err != nil {
			return err
		}
	case livekit.EventParticipantLeft:
		accountID, err := uuid.Parse(ev.Participant.Identity)
		if err != nil {
			return nil
		}
		if err := s.repo.ParticipantLeft(ctx, channelID, accountID, ev.Participant.SID); err != nil {
			return err
		}
	case livekit.EventRoomFinished:
		if s.onEnd != nil {
			s.onEnd(ctx, channelID)
		}
		if err := s.repo.EndSession(ctx, channelID); err != nil {
			return err
		}
	default:
		return nil
	}
	s.announce(ctx, channelID)
	return nil
}

// ---------------------------------------------------------------- recording

// RecordingEnabled implements calldom.Service.
func (s *Service) RecordingEnabled() bool {
	return s.Enabled() && s.storage != nil && s.rec.Enabled && s.rec.Bucket != ""
}

func recordingPath(projectID, channelID, recordingID uuid.UUID) string {
	scope := "dm"
	if projectID != uuid.Nil {
		scope = projectID.String()
	}
	return "recordings/" + scope + "/" + channelID.String() + "/" + recordingID.String() + ".mp4"
}

// StartRecording implements calldom.Service.
func (s *Service) StartRecording(ctx context.Context, projectID, channelID, by uuid.UUID) (*calldom.Recording, error) {
	if !s.RecordingEnabled() {
		return nil, calldom.ErrRecordingDisabled
	}
	ch, err := s.channel(ctx, projectID, channelID)
	if err != nil {
		return nil, err
	}
	if ch.Archived {
		return nil, chatdom.ErrChannelArchived
	}
	rec, err := s.repo.CreateRecording(ctx, channelID, by)
	if err != nil {
		return nil, err
	}
	egressID, err := s.room.StartRoomRecording(ctx, channelID.String(), recordingPath(projectID, channelID, rec.ID), s.rec.Out)
	if err != nil {
		if ferr := s.repo.FailRecording(ctx, rec.ID, "the recording could not be started"); ferr != nil {
			slog.Warn("calls: close recording that did not start", "recording_id", rec.ID, "err", ferr)
		}
		s.announce(ctx, channelID)
		return nil, fmt.Errorf("calls: start recording: %w", err)
	}
	rec.EgressID = egressID
	if err := s.repo.SetRecordingEgress(ctx, rec.ID, egressID); err != nil {
		slog.Warn("calls: record egress id", "recording_id", rec.ID, "err", err)
	}
	s.announce(ctx, channelID)
	return rec, nil
}

// StopRecording implements calldom.Service.
func (s *Service) StopRecording(ctx context.Context, projectID, channelID uuid.UUID) error {
	if !s.RecordingEnabled() {
		return calldom.ErrRecordingDisabled
	}
	if _, err := s.channel(ctx, projectID, channelID); err != nil {
		return err
	}
	rec, err := s.repo.OpenRecording(ctx, channelID)
	if err != nil {
		return err
	}
	if rec == nil {
		return calldom.ErrNoRecording
	}
	if rec.EgressID == "" {
		// It never got as far as LiveKit.
		_ = s.repo.FailRecording(ctx, rec.ID, "stopped before it started")
		s.announce(ctx, channelID)
		return nil
	}
	if rec.Status == calldom.RecordingStopping {
		// Stop was already sent. If LiveKit still knows the egress it is
		// finalising and its egress_ended webhook will close the row, so a
		// second press changes nothing. Only an egress LiveKit has lost (the
		// webhook can then never arrive) is closed here, so the call can be
		// recorded again.
		err := s.room.StopRecording(ctx, channelID.String(), rec.EgressID)
		if errors.Is(err, livekit.ErrNotFound) {
			_ = s.repo.FailRecording(ctx, rec.ID, "the recording did not finish")
			s.announce(ctx, channelID)
			return nil
		}
		if err != nil {
			slog.Warn("calls: stop a stopping recording", "recording_id", rec.ID, "err", err)
		}
		return nil
	}
	if err := s.room.StopRecording(ctx, channelID.String(), rec.EgressID); err != nil {
		if errors.Is(err, livekit.ErrNotFound) {
			// LiveKit no longer has it (the room ended): close it here.
			_ = s.repo.FailRecording(ctx, rec.ID, "the recording ended without a file")
			s.announce(ctx, channelID)
			return nil
		}
		return err
	}
	if _, err := s.repo.UpdateRecordingByEgress(ctx, rec.EgressID, calldom.RecordingStopping, "", 0, 0, ""); err != nil {
		slog.Warn("calls: mark recording stopping", "recording_id", rec.ID, "err", err)
	}
	s.announce(ctx, channelID)
	return nil
}

// ListRecordings implements calldom.Service.
func (s *Service) ListRecordings(ctx context.Context, projectID, channelID uuid.UUID) ([]*calldom.Recording, error) {
	if _, err := s.repo.FindChannel(ctx, projectID, channelID); err != nil {
		return nil, err
	}
	return s.repo.ListRecordings(ctx, channelID, 100)
}

// RecordingURL implements calldom.Service.
func (s *Service) RecordingURL(ctx context.Context, projectID, channelID, id uuid.UUID, download bool) (string, error) {
	if !s.RecordingEnabled() {
		return "", calldom.ErrRecordingDisabled
	}
	if _, err := s.repo.FindChannel(ctx, projectID, channelID); err != nil {
		return "", err
	}
	rec, err := s.repo.FindRecording(ctx, channelID, id)
	if err != nil {
		return "", err
	}
	if rec.Status != calldom.RecordingComplete || rec.ObjectKey == "" {
		return "", calldom.ErrRecordingNotFound
	}
	disposition := ""
	if download {
		disposition = `attachment; filename="recording-` + rec.ID.String()[:8] + `.mp4"`
	}
	return s.storage.PresignGetObject(ctx, s.rec.Bucket, rec.ObjectKey, RecordingLinkTTL, disposition)
}

// egressStatus maps LiveKit's EgressStatus (a name, or its number in JSON) to ours.
func egressStatus(v string) string {
	switch strings.TrimPrefix(v, "EGRESS_") {
	case "STARTING", "0":
		return calldom.RecordingStarting
	case "ACTIVE", "1":
		return calldom.RecordingActive
	case "ENDING", "2":
		return calldom.RecordingStopping
	case "COMPLETE", "3":
		return calldom.RecordingComplete
	default: // FAILED, ABORTED, LIMIT_REACHED
		return calldom.RecordingFailed
	}
}

// handleEgress applies an egress_* webhook to the recording it belongs to; an
// egress that is not one of ours is ignored.
func (s *Service) handleEgress(ctx context.Context, ev *livekit.WebhookEvent) error {
	e := ev.EgressInfo
	if e == nil || e.EgressID == "" {
		return nil
	}
	status := egressStatus(e.Status)
	var key string
	var size, durationMs int64
	if len(e.FileResults) > 0 {
		f := e.FileResults[0]
		key, size, durationMs = f.Filename, int64(f.Size), int64(f.Duration)/int64(time.Millisecond)
	}
	if status == calldom.RecordingComplete && key == "" {
		// A recording with no file is not one anybody can play.
		status = calldom.RecordingFailed
		e.Error = "the recording has no file"
	}
	channelID, err := s.repo.UpdateRecordingByEgress(ctx, e.EgressID, status, key, size, durationMs, e.Error)
	if err != nil {
		return err
	}
	if channelID != uuid.Nil {
		s.announce(ctx, channelID)
	}
	return nil
}

func (s *Service) announce(ctx context.Context, channelID uuid.UUID) {
	if s.notifier == nil {
		return
	}
	s.notifier.CallChanged(ctx, channelID)
}
