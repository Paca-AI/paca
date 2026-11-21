package callsvc

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	calldom "github.com/Paca-AI/api/internal/domain/call"
	chatdom "github.com/Paca-AI/api/internal/domain/chat"
	"github.com/Paca-AI/api/internal/platform/livekit"
)

type fakeRepo struct {
	archived bool
	missing  bool
	call     *calldom.Call
	joined   []uuid.UUID
	left     []uuid.UUID
	leftSIDs []string
	joinSIDs []string
	banned   map[uuid.UUID]bool
	ended    int
	sid      string
}

func (f *fakeRepo) FindChannel(_ context.Context, p, c uuid.UUID) (*calldom.Channel, error) {
	if f.missing {
		return nil, chatdom.ErrChannelNotFound
	}
	return &calldom.Channel{ID: c, ProjectID: p, Archived: f.archived}, nil
}
func (f *fakeRepo) AccountName(context.Context, uuid.UUID) (string, string, error) {
	return "Ada", "user", nil
}
func (f *fakeRepo) OpenSession(context.Context, uuid.UUID, uuid.UUID) (uuid.UUID, error) {
	return uuid.New(), nil
}
func (f *fakeRepo) SetRoomSID(_ context.Context, _ uuid.UUID, sid string) error {
	f.sid = sid
	return nil
}
func (f *fakeRepo) ParticipantJoined(_ context.Context, _, a uuid.UUID, sid string) error {
	f.joined = append(f.joined, a)
	f.joinSIDs = append(f.joinSIDs, sid)
	return nil
}
func (f *fakeRepo) ParticipantLeft(_ context.Context, _, a uuid.UUID, sid string) error {
	f.left = append(f.left, a)
	f.leftSIDs = append(f.leftSIDs, sid)
	return nil
}
func (f *fakeRepo) BanRemoved(_ context.Context, _, a uuid.UUID, _ time.Time) error {
	if f.banned == nil {
		f.banned = map[uuid.UUID]bool{}
	}
	f.banned[a] = true
	return nil
}
func (f *fakeRepo) IsRemoved(_ context.Context, _, a uuid.UUID) (bool, error) {
	return f.banned[a], nil
}
func (f *fakeRepo) EndSession(context.Context, uuid.UUID) error { f.ended++; return nil }
func (f *fakeRepo) ListCall(context.Context, uuid.UUID) (*calldom.Call, error) {
	return f.call, nil
}
func (f *fakeRepo) ListCalls(context.Context, uuid.UUID) ([]*calldom.Call, error) { return nil, nil }

type fakeRoom struct {
	enabled bool
	event   *livekit.WebhookEvent
	verr    error
	muted   []string
	removed []string
	rerr    error
}

func (f *fakeRoom) Enabled() bool     { return f.enabled }
func (f *fakeRoom) PublicURL() string { return "wss://x/livekit" }
func (f *fakeRoom) MintToken(id livekit.Identity, g livekit.Grant, ttl time.Duration) (string, time.Time, error) {
	return "tok:" + id.ID + ":" + g.Room, time.Now().Add(ttl), nil
}
func (f *fakeRoom) VerifyWebhook(string, []byte) (*livekit.WebhookEvent, error) {
	return f.event, f.verr
}
func (f *fakeRoom) RemoveParticipant(_ context.Context, _, id string) error {
	f.removed = append(f.removed, id)
	return f.rerr
}
func (f *fakeRoom) MuteParticipant(_ context.Context, _, id string) error {
	f.muted = append(f.muted, id)
	return f.rerr
}

type fakeNotifier struct{ n int }

func (f *fakeNotifier) CallChanged(context.Context, uuid.UUID) { f.n++ }

func TestJoinMintsTokenForTheChannelRoom(t *testing.T) {
	ch, me := uuid.New(), uuid.New()
	s := New(&fakeRepo{}, &fakeRoom{enabled: true})
	info, err := s.Join(t.Context(), uuid.New(), ch, me)
	if err != nil {
		t.Fatal(err)
	}
	if info.Room != ch.String() || info.Token != "tok:"+me.String()+":"+ch.String() || info.URL != "wss://x/livekit" {
		t.Errorf("info = %+v", info)
	}
}

func TestJoinRefusals(t *testing.T) {
	if _, err := New(&fakeRepo{}, &fakeRoom{}).Join(t.Context(), uuid.New(), uuid.New(), uuid.New()); !errors.Is(err, calldom.ErrDisabled) {
		t.Errorf("disabled: err = %v", err)
	}
	if _, err := New(&fakeRepo{archived: true}, &fakeRoom{enabled: true}).Join(t.Context(), uuid.New(), uuid.New(), uuid.New()); !errors.Is(err, chatdom.ErrChannelArchived) {
		t.Errorf("archived: err = %v", err)
	}
	if _, err := New(&fakeRepo{missing: true}, &fakeRoom{enabled: true}).Join(t.Context(), uuid.New(), uuid.New(), uuid.New()); !errors.Is(err, chatdom.ErrChannelNotFound) {
		t.Errorf("missing: err = %v", err)
	}
}

func TestWebhookEvents(t *testing.T) {
	ch, acc := uuid.New(), uuid.New()
	ev := func(kind string) *livekit.WebhookEvent {
		e := &livekit.WebhookEvent{Event: kind}
		e.Room.Name, e.Room.SID = ch.String(), "RM_1"
		e.Participant.Identity, e.Participant.SID = acc.String(), "PA_1"
		return e
	}
	repo, n := &fakeRepo{}, &fakeNotifier{}
	room := &fakeRoom{enabled: true}
	s := New(repo, room).WithNotifier(n)
	for _, kind := range []string{livekit.EventRoomStarted, livekit.EventParticipantJoined, livekit.EventParticipantLeft, livekit.EventRoomFinished} {
		room.event = ev(kind)
		if err := s.HandleWebhook(t.Context(), "auth", nil); err != nil {
			t.Fatalf("%s: %v", kind, err)
		}
	}
	if repo.joinSIDs[0] != "PA_1" || repo.leftSIDs[0] != "PA_1" {
		t.Errorf("sids = %v / %v, want the webhook's participant sid", repo.joinSIDs, repo.leftSIDs)
	}
	if repo.sid != "RM_1" || len(repo.joined) != 1 || len(repo.left) != 1 || repo.ended != 1 || n.n != 4 {
		t.Errorf("repo = %+v, announcements = %d", repo, n.n)
	}
}

func TestWebhookIgnoresForeignRoomsAndBadSignatures(t *testing.T) {
	repo, n := &fakeRepo{}, &fakeNotifier{}
	room := &fakeRoom{enabled: true}
	s := New(repo, room).WithNotifier(n)

	e := &livekit.WebhookEvent{Event: livekit.EventParticipantJoined}
	e.Room.Name = "not-a-channel"
	room.event = e
	if err := s.HandleWebhook(t.Context(), "", nil); err != nil || len(repo.joined) != 0 || n.n != 0 {
		t.Errorf("foreign room: err = %v, repo = %+v", err, repo)
	}

	room.event, room.verr = nil, livekit.ErrBadWebhook
	if err := s.HandleWebhook(t.Context(), "", nil); !errors.Is(err, livekit.ErrBadWebhook) {
		t.Errorf("bad signature: err = %v", err)
	}
}

func TestModeration(t *testing.T) {
	ch, actor, target := uuid.New(), uuid.New(), uuid.New()
	call := &calldom.Call{ChannelID: ch, Participants: []calldom.Participant{{AccountID: target}}}
	repo, room, n := &fakeRepo{call: call}, &fakeRoom{enabled: true}, &fakeNotifier{}
	s := New(repo, room).WithNotifier(n)

	if err := s.Mute(t.Context(), uuid.New(), ch, actor, target); err != nil || len(room.muted) != 1 {
		t.Fatalf("mute: err = %v, muted = %v", err, room.muted)
	}
	if err := s.Remove(t.Context(), uuid.New(), ch, actor, target); err != nil || len(room.removed) != 1 || len(repo.left) != 1 || n.n != 1 {
		t.Fatalf("remove: err = %v, removed = %v", err, room.removed)
	}
	if err := s.Mute(t.Context(), uuid.New(), ch, actor, actor); !errors.Is(err, calldom.ErrSelfModerate) {
		t.Errorf("self: err = %v", err)
	}
	if err := s.Mute(t.Context(), uuid.New(), ch, actor, uuid.New()); !errors.Is(err, calldom.ErrNotInCall) {
		t.Errorf("not in call: err = %v", err)
	}
	room.rerr = livekit.ErrNotFound
	if err := s.Remove(t.Context(), uuid.New(), ch, actor, target); !errors.Is(err, calldom.ErrNotInCall) {
		t.Errorf("already gone: err = %v", err)
	}
}

func TestRemoveBarsTheAccountFromRejoining(t *testing.T) {
	ch, actor, target := uuid.New(), uuid.New(), uuid.New()
	call := &calldom.Call{ChannelID: ch, Participants: []calldom.Participant{{AccountID: target}}}
	repo, room := &fakeRepo{call: call}, &fakeRoom{enabled: true}
	s := New(repo, room)
	if err := s.Remove(t.Context(), uuid.New(), ch, actor, target); err != nil {
		t.Fatal(err)
	}
	if repo.leftSIDs[0] != "" {
		t.Errorf("remove must close the row whatever its connection, got sid %q", repo.leftSIDs[0])
	}
	if _, err := s.Join(t.Context(), uuid.New(), ch, target); !errors.Is(err, calldom.ErrRemoved) {
		t.Errorf("join after removal: err = %v", err)
	}

	// A connection that still holds its old token is disconnected again and not recorded.
	e := &livekit.WebhookEvent{Event: livekit.EventParticipantJoined}
	e.Room.Name, e.Participant.Identity, e.Participant.SID = ch.String(), target.String(), "PA_2"
	room.event = e
	room.removed = nil
	repo.joined = nil
	if err := s.HandleWebhook(t.Context(), "auth", nil); err != nil {
		t.Fatal(err)
	}
	if len(room.removed) != 1 || len(repo.joined) != 0 {
		t.Errorf("removed = %v, joined = %v", room.removed, repo.joined)
	}
}
