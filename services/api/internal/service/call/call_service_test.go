package callsvc

import (
	"context"
	"errors"
	"strings"
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

	recs      map[uuid.UUID]*calldom.Recording
	noOpen    bool
	failedWhy map[uuid.UUID]string
}

func (f *fakeRepo) CreateRecording(_ context.Context, ch, by uuid.UUID) (*calldom.Recording, error) {
	if f.noOpen {
		return nil, calldom.ErrNoOpenCall
	}
	for _, r := range f.recs {
		if r.Open() {
			return nil, calldom.ErrRecordingActive
		}
	}
	if f.recs == nil {
		f.recs = map[uuid.UUID]*calldom.Recording{}
	}
	r := &calldom.Recording{ID: uuid.New(), CallID: uuid.New(), ChannelID: ch, Status: calldom.RecordingStarting, StartedBy: &by, StartedAt: time.Now()}
	f.recs[r.ID] = r
	c := *r
	return &c, nil
}
func (f *fakeRepo) SetRecordingEgress(_ context.Context, id uuid.UUID, egress string) error {
	f.recs[id].EgressID = egress
	return nil
}
func (f *fakeRepo) FailRecording(_ context.Context, id uuid.UUID, why string) error {
	if f.failedWhy == nil {
		f.failedWhy = map[uuid.UUID]string{}
	}
	f.failedWhy[id] = why
	f.recs[id].Status = calldom.RecordingFailed
	return nil
}
func (f *fakeRepo) UpdateRecordingByEgress(_ context.Context, egress, status, key string, size, dur int64, msg string) (uuid.UUID, error) {
	for _, r := range f.recs {
		if r.EgressID == egress && r.Open() {
			r.Status, r.SizeBytes, r.DurationMs, r.Error = status, size, dur, msg
			if key != "" {
				r.ObjectKey = key
			}
			return r.ChannelID, nil
		}
	}
	return uuid.Nil, nil
}
func (f *fakeRepo) OpenRecording(_ context.Context, _ uuid.UUID) (*calldom.Recording, error) {
	for _, r := range f.recs {
		if r.Open() {
			c := *r
			return &c, nil
		}
	}
	return nil, nil
}
func (f *fakeRepo) ListRecordings(context.Context, uuid.UUID, int) ([]*calldom.Recording, error) {
	var out []*calldom.Recording
	for _, r := range f.recs {
		out = append(out, r)
	}
	return out, nil
}
func (f *fakeRepo) FindRecording(_ context.Context, _, id uuid.UUID) (*calldom.Recording, error) {
	if r, ok := f.recs[id]; ok {
		c := *r
		return &c, nil
	}
	return nil, calldom.ErrRecordingNotFound
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

	recErr  error
	started []string
	stopped []string
	stopErr error
	lastOut livekit.S3Output
}

func (f *fakeRoom) StartRoomRecording(_ context.Context, room, path string, out livekit.S3Output) (string, error) {
	if f.recErr != nil {
		return "", f.recErr
	}
	f.started = append(f.started, room+"|"+path)
	f.lastOut = out
	return "EG_1", nil
}
func (f *fakeRoom) StopRecording(_ context.Context, _, id string) error {
	f.stopped = append(f.stopped, id)
	return f.stopErr
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

type fakeStorage struct{ got []string }

func (f *fakeStorage) PresignGetObject(_ context.Context, bucket, key string, _ time.Duration, disposition string) (string, error) {
	f.got = append(f.got, bucket+"/"+key+"|"+disposition)
	return "https://files/" + key, nil
}

func recordingService(repo *fakeRepo, room *fakeRoom, enabled bool) (*Service, *fakeStorage, *fakeNotifier) {
	st, n := &fakeStorage{}, &fakeNotifier{}
	out := livekit.S3Output{Bucket: "paca", Endpoint: "http://rustfs:9000", ForcePathStyle: true}
	return New(repo, room).WithNotifier(n).WithRecording(st, RecordingConfig{Enabled: enabled, Bucket: "paca", Out: out}), st, n
}

func TestRecordingNeedsEgressAndStorage(t *testing.T) {
	ctx := t.Context()
	s, _, _ := recordingService(&fakeRepo{}, &fakeRoom{enabled: true}, false)
	if s.RecordingEnabled() {
		t.Fatal("egress off must switch recording off")
	}
	if _, err := s.StartRecording(ctx, uuid.New(), uuid.New(), uuid.New()); !errors.Is(err, calldom.ErrRecordingDisabled) {
		t.Errorf("start: %v", err)
	}
	plain := New(&fakeRepo{}, &fakeRoom{enabled: true})
	if plain.RecordingEnabled() {
		t.Error("no storage must switch recording off")
	}
}

func TestStartAndStopRecording(t *testing.T) {
	ctx := t.Context()
	repo, room := &fakeRepo{}, &fakeRoom{enabled: true}
	s, _, n := recordingService(repo, room, true)
	project, ch, me := uuid.New(), uuid.New(), uuid.New()

	rec, err := s.StartRecording(ctx, project, ch, me)
	if err != nil {
		t.Fatal(err)
	}
	if rec.EgressID != "EG_1" || len(room.started) != 1 {
		t.Fatalf("rec %+v started %v", rec, room.started)
	}
	want := ch.String() + "|recordings/" + project.String() + "/" + ch.String() + "/" + rec.ID.String() + ".mp4"
	if room.started[0] != want {
		t.Errorf("path %q want %q", room.started[0], want)
	}
	if room.lastOut.Bucket != "paca" || !room.lastOut.ForcePathStyle {
		t.Errorf("output %+v", room.lastOut)
	}
	if n.n == 0 {
		t.Error("starting must be announced")
	}
	// Everyone in the call sees it.
	repo.call = &calldom.Call{ChannelID: ch, Participants: []calldom.Participant{}}
	if c, _ := s.GetCall(ctx, project, ch); c.Recording == nil || c.Recording.ID != rec.ID {
		t.Errorf("the call must show its recording: %+v", c)
	}
	if _, err := s.StartRecording(ctx, project, ch, me); !errors.Is(err, calldom.ErrRecordingActive) {
		t.Errorf("a second recording: %v", err)
	}

	if err := s.StopRecording(ctx, project, ch); err != nil {
		t.Fatal(err)
	}
	if len(room.stopped) != 1 || room.stopped[0] != "EG_1" || repo.recs[rec.ID].Status != calldom.RecordingStopping {
		t.Errorf("stopped %v status %s", room.stopped, repo.recs[rec.ID].Status)
	}
}

func TestStartRecordingFailureLeavesNothingRunning(t *testing.T) {
	ctx := t.Context()
	repo, room := &fakeRepo{}, &fakeRoom{enabled: true, recErr: errors.New("egress down")}
	s, _, _ := recordingService(repo, room, true)
	if _, err := s.StartRecording(ctx, uuid.New(), uuid.New(), uuid.New()); err == nil {
		t.Fatal("want an error")
	}
	if rec, _ := repo.OpenRecording(ctx, uuid.Nil); rec != nil {
		t.Error("a recording that could not start must not stay open")
	}
	room.recErr = nil
	if _, err := s.StartRecording(ctx, uuid.New(), uuid.New(), uuid.New()); err != nil {
		t.Errorf("the next attempt must be possible: %v", err)
	}
}

func TestRecordingNeedsAnOpenCallAndAnArchivedChannelIsRefused(t *testing.T) {
	ctx := t.Context()
	s, _, _ := recordingService(&fakeRepo{noOpen: true}, &fakeRoom{enabled: true}, true)
	if _, err := s.StartRecording(ctx, uuid.New(), uuid.New(), uuid.New()); !errors.Is(err, calldom.ErrNoOpenCall) {
		t.Errorf("no call: %v", err)
	}
	s, _, _ = recordingService(&fakeRepo{archived: true}, &fakeRoom{enabled: true}, true)
	if _, err := s.StartRecording(ctx, uuid.New(), uuid.New(), uuid.New()); !errors.Is(err, chatdom.ErrChannelArchived) {
		t.Errorf("archived: %v", err)
	}
	s, _, _ = recordingService(&fakeRepo{}, &fakeRoom{enabled: true}, true)
	if err := s.StopRecording(ctx, uuid.New(), uuid.New()); !errors.Is(err, calldom.ErrNoRecording) {
		t.Errorf("stop with nothing running: %v", err)
	}
}

func TestStopRecordingWhenLiveKitLostIt(t *testing.T) {
	ctx := t.Context()
	repo, room := &fakeRepo{}, &fakeRoom{enabled: true}
	s, _, _ := recordingService(repo, room, true)
	rec, _ := s.StartRecording(ctx, uuid.New(), uuid.New(), uuid.New())
	room.stopErr = livekit.ErrNotFound
	if err := s.StopRecording(ctx, uuid.New(), rec.ChannelID); err != nil {
		t.Fatal(err)
	}
	if repo.recs[rec.ID].Status != calldom.RecordingFailed {
		t.Errorf("status %s", repo.recs[rec.ID].Status)
	}
}

func egressEvent(status, filename string, size int64) *livekit.WebhookEvent {
	ev := &livekit.WebhookEvent{Event: livekit.EventEgressUpdated, EgressInfo: &livekit.EgressInfo{EgressID: "EG_1", Status: status}}
	if filename != "" {
		ev.EgressInfo.FileResults = []struct {
			Filename string          `json:"filename"`
			Location string          `json:"location"`
			Size     livekit.FlexInt `json:"size"`
			Duration livekit.FlexInt `json:"duration"`
		}{{Filename: filename, Size: livekit.FlexInt(size), Duration: livekit.FlexInt(90 * int64(time.Second))}}
	}
	return ev
}

func TestEgressWebhooksFollowTheRecording(t *testing.T) {
	ctx := t.Context()
	repo, room := &fakeRepo{}, &fakeRoom{enabled: true}
	s, _, n := recordingService(repo, room, true)
	rec, _ := s.StartRecording(ctx, uuid.New(), uuid.New(), uuid.New())
	before := n.n

	room.event = egressEvent("EGRESS_ACTIVE", "", 0)
	if err := s.HandleWebhook(ctx, "", nil); err != nil {
		t.Fatal(err)
	}
	if repo.recs[rec.ID].Status != calldom.RecordingActive || n.n <= before {
		t.Errorf("status %s announced %d", repo.recs[rec.ID].Status, n.n)
	}

	room.event = egressEvent("EGRESS_COMPLETE", "recordings/x.mp4", 1234)
	if err := s.HandleWebhook(ctx, "", nil); err != nil {
		t.Fatal(err)
	}
	r := repo.recs[rec.ID]
	if r.Status != calldom.RecordingComplete || r.ObjectKey != "recordings/x.mp4" || r.SizeBytes != 1234 || r.DurationMs != 90000 {
		t.Errorf("%+v", r)
	}

	// An egress that is not ours changes nothing.
	room.event = egressEvent("EGRESS_COMPLETE", "other.mp4", 1)
	room.event.EgressInfo.EgressID = "EG_other"
	if err := s.HandleWebhook(ctx, "", nil); err != nil {
		t.Fatal(err)
	}
}

func TestCompleteWithoutAFileIsAFailure(t *testing.T) {
	ctx := t.Context()
	repo, room := &fakeRepo{}, &fakeRoom{enabled: true}
	s, _, _ := recordingService(repo, room, true)
	rec, _ := s.StartRecording(ctx, uuid.New(), uuid.New(), uuid.New())
	room.event = egressEvent("EGRESS_COMPLETE", "", 0)
	if err := s.HandleWebhook(ctx, "", nil); err != nil {
		t.Fatal(err)
	}
	if repo.recs[rec.ID].Status != calldom.RecordingFailed {
		t.Errorf("status %s", repo.recs[rec.ID].Status)
	}
	for in, want := range map[string]string{"EGRESS_FAILED": "failed", "EGRESS_ABORTED": "failed", "EGRESS_LIMIT_REACHED": "failed", "3": "complete", "EGRESS_ENDING": "stopping", "EGRESS_STARTING": "starting"} {
		if got := egressStatus(in); got != want {
			t.Errorf("%s -> %s, want %s", in, got, want)
		}
	}
}

func TestRecordingURLOnlyForFinishedFiles(t *testing.T) {
	ctx := t.Context()
	repo, room := &fakeRepo{}, &fakeRoom{enabled: true}
	s, st, _ := recordingService(repo, room, true)
	rec, _ := s.StartRecording(ctx, uuid.New(), uuid.New(), uuid.New())
	if _, err := s.RecordingURL(ctx, uuid.New(), rec.ChannelID, rec.ID, false); !errors.Is(err, calldom.ErrRecordingNotFound) {
		t.Errorf("a running recording has no file: %v", err)
	}
	repo.recs[rec.ID].Status, repo.recs[rec.ID].ObjectKey = calldom.RecordingComplete, "recordings/x.mp4"
	url, err := s.RecordingURL(ctx, uuid.New(), rec.ChannelID, rec.ID, false)
	if err != nil || url != "https://files/recordings/x.mp4" || st.got[0] != "paca/recordings/x.mp4|" {
		t.Fatalf("url %q err %v got %v", url, err, st.got)
	}
	if _, err := s.RecordingURL(ctx, uuid.New(), rec.ChannelID, rec.ID, true); err != nil || !strings.Contains(st.got[1], "attachment") {
		t.Errorf("download must force a save: %v %v", err, st.got)
	}
	if _, err := s.RecordingURL(ctx, uuid.New(), rec.ChannelID, uuid.New(), false); !errors.Is(err, calldom.ErrRecordingNotFound) {
		t.Errorf("unknown recording: %v", err)
	}
}
