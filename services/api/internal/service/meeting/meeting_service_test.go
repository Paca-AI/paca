package meetingsvc

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	agentdom "github.com/Paca-AI/api/internal/domain/agent"
	meetingdom "github.com/Paca-AI/api/internal/domain/meeting"
)

type fakeRepo struct {
	info      *meetingdom.AgentInfo
	settings  *meetingdom.Settings
	callID    uuid.UUID
	host      *uuid.UUID
	inCall    map[uuid.UUID]bool
	left      map[uuid.UUID]bool // were in the call, have left
	sessions  map[uuid.UUID]*meetingdom.AgentSession
	proposals map[uuid.UUID]*meetingdom.Proposal
	segments  []meetingdom.SegmentInput
	keys      map[string]bool // session -> live key
}

func newFake() *fakeRepo {
	id := uuid.New()
	return &fakeRepo{
		info:      &meetingdom.AgentInfo{ID: id, Name: "Atlas", Type: agentdom.AgentTypeMeeting, LLMProvider: "openai", LLMModel: "gpt", LLMKeySecret: "enc:llm"},
		settings:  &meetingdom.Settings{AgentID: id, STTProvider: "openai", STTModel: "whisper-1", STTAPIKeySecret: "enc:stt", Mode: meetingdom.ModeOnDemand, SuggestIntervalSeconds: 120, ProposalKinds: append([]string(nil), meetingdom.ProposalKinds...), MaxProposalsPerCall: 2, StoreTranscript: true, TranscriptRetentionDays: 7, SummaryTarget: meetingdom.SummaryChannelMessage, DefaultTarget: meetingdom.TargetRequester},
		callID:    uuid.New(),
		inCall:    map[uuid.UUID]bool{},
		left:      map[uuid.UUID]bool{},
		sessions:  map[uuid.UUID]*meetingdom.AgentSession{},
		proposals: map[uuid.UUID]*meetingdom.Proposal{},
		keys:      map[string]bool{},
	}
}

func (f *fakeRepo) AgentInfo(context.Context, uuid.UUID, uuid.UUID) (*meetingdom.AgentInfo, error) {
	return f.info, nil
}
func (f *fakeRepo) GetSettings(context.Context, uuid.UUID) (*meetingdom.Settings, error) {
	c := *f.settings
	return &c, nil
}
func (f *fakeRepo) SaveSettings(_ context.Context, s *meetingdom.Settings) error {
	c := *s
	f.settings = &c
	return nil
}
func (f *fakeRepo) OpenCall(context.Context, uuid.UUID) (uuid.UUID, *uuid.UUID, error) {
	return f.callID, f.host, nil
}
func (f *fakeRepo) ChannelProject(context.Context, uuid.UUID) (uuid.UUID, bool, error) {
	return uuid.Nil, true, nil
}
func (f *fakeRepo) IsCallParticipant(_ context.Context, _, a uuid.UUID) (bool, error) {
	return f.inCall[a], nil
}
func (f *fakeRepo) WasCallParticipant(_ context.Context, _, a uuid.UUID) (bool, error) {
	return f.inCall[a] || f.left[a], nil
}
func (f *fakeRepo) CreateAgentSession(_ context.Context, s *meetingdom.AgentSession) error {
	for _, o := range f.sessions {
		if o.AgentID == s.AgentID && o.LeftAt == nil {
			return meetingdom.ErrAlreadyInCall
		}
	}
	c := *s
	c.JoinedAt = time.Now()
	f.sessions[s.ID] = &c
	return nil
}
func (f *fakeRepo) SetAgentSessionDispatch(_ context.Context, id uuid.UUID, d string) error {
	f.sessions[id].DispatchID = d
	return nil
}
func (f *fakeRepo) SetAgentSessionStatus(_ context.Context, id uuid.UUID, st string) error {
	s := f.sessions[id]
	s.Status = st
	if st == meetingdom.SessionLeft || st == meetingdom.SessionFailed {
		now := time.Now()
		s.LeftAt = &now
	}
	return nil
}
func (f *fakeRepo) FindAgentSession(_ context.Context, id uuid.UUID) (*meetingdom.AgentSession, error) {
	s, ok := f.sessions[id]
	if !ok {
		return nil, meetingdom.ErrNotInCall
	}
	c := *s
	return &c, nil
}
func (f *fakeRepo) OpenAgentSession(_ context.Context, ch, agent uuid.UUID) (*meetingdom.AgentSession, error) {
	for _, s := range f.sessions {
		if s.ChannelID == ch && s.AgentID == agent && s.LeftAt == nil {
			c := *s
			return &c, nil
		}
	}
	return nil, meetingdom.ErrNotInCall
}
func (f *fakeRepo) ListAgentSessions(_ context.Context, ch uuid.UUID) ([]*meetingdom.AgentSession, error) {
	var out []*meetingdom.AgentSession
	for _, s := range f.sessions {
		if s.ChannelID == ch && s.LeftAt == nil {
			c := *s
			out = append(out, &c)
		}
	}
	return out, nil
}
func (f *fakeRepo) EndAgentSessions(_ context.Context, _ uuid.UUID) error {
	for id := range f.sessions {
		_ = f.SetAgentSessionStatus(context.Background(), id, meetingdom.SessionLeft)
	}
	return nil
}
func (f *fakeRepo) ListMeetingAgents(context.Context, uuid.UUID) ([]*meetingdom.MeetingAgent, error) {
	return nil, nil
}
func (f *fakeRepo) CreateWorkerKey(_ context.Context, _, sid uuid.UUID, _, _ string, _ time.Time) error {
	f.keys[sid.String()] = true
	return nil
}
func (f *fakeRepo) RevokeWorkerKeys(_ context.Context, sid uuid.UUID) error {
	f.keys[sid.String()] = false
	return nil
}
func (f *fakeRepo) AppendSegments(_ context.Context, _, _ uuid.UUID, _ int, s []meetingdom.SegmentInput) error {
	f.segments = append(f.segments, s...)
	return nil
}
func (f *fakeRepo) ListSegments(context.Context, uuid.UUID, int) ([]*meetingdom.Segment, error) {
	return nil, nil
}
func (f *fakeRepo) DeleteExpiredSegments(context.Context) (int64, error) { return 0, nil }
func (f *fakeRepo) ListMeetings(context.Context, uuid.UUID, int) ([]*meetingdom.MeetingSummary, error) {
	return nil, nil
}
func (f *fakeRepo) ListNamedSegments(context.Context, uuid.UUID, uuid.UUID, int) ([]*meetingdom.NamedSegment, error) {
	return nil, nil
}
func (f *fakeRepo) CountProposals(_ context.Context, _, agent uuid.UUID) (int, error) {
	n := 0
	for _, p := range f.proposals {
		if p.AgentAccountID == agent {
			n++
		}
	}
	return n, nil
}
func (f *fakeRepo) CreateProposal(_ context.Context, p *meetingdom.Proposal) error {
	c := *p
	f.proposals[p.ID] = &c
	return nil
}
func (f *fakeRepo) GetProposal(_ context.Context, _, id uuid.UUID) (*meetingdom.Proposal, error) {
	p, ok := f.proposals[id]
	if !ok {
		return nil, meetingdom.ErrProposalNotFound
	}
	c := *p
	return &c, nil
}
func (f *fakeRepo) ListProposals(context.Context, uuid.UUID, *uuid.UUID, *uuid.UUID, int) ([]*meetingdom.Proposal, error) {
	return nil, nil
}
func (f *fakeRepo) TransitionProposal(_ context.Context, id uuid.UUID, from []string, to string, by *uuid.UUID, result []byte) (bool, error) {
	p := f.proposals[id]
	for _, st := range from {
		if p.Status == st {
			p.Status = to
			if by != nil {
				p.ResolvedBy = by
			}
			if len(result) > 0 {
				p.Result = result
			}
			return true, nil
		}
	}
	return false, nil
}
func (f *fakeRepo) ExpireProposals(context.Context) (int64, error) { return 0, nil }

type fakeRoom struct {
	enabled    bool
	dispatched []string
	deleted    []string
	removed    []string
	fail       error
}

func (r *fakeRoom) Enabled() bool { return r.enabled }
func (r *fakeRoom) DispatchAgent(_ context.Context, room, name, meta string) (string, error) {
	if r.fail != nil {
		return "", r.fail
	}
	r.dispatched = append(r.dispatched, room+"|"+name+"|"+meta)
	return "AD_1", nil
}
func (r *fakeRoom) DeleteDispatch(_ context.Context, _, id string) error {
	r.deleted = append(r.deleted, id)
	return nil
}
func (r *fakeRoom) RemoveParticipant(_ context.Context, _, id string) error {
	r.removed = append(r.removed, id)
	return nil
}

type fakeCrypt struct{}

func (fakeCrypt) Encrypt(p string) (string, error) { return "enc:" + p, nil }
func (fakeCrypt) Decrypt(e string) (string, error) { return strings.TrimPrefix(e, "enc:"), nil }

type fakeNotifier struct{ assistant, proposal int }

func (n *fakeNotifier) AssistantChanged(context.Context, uuid.UUID) { n.assistant++ }
func (n *fakeNotifier) ProposalChanged(context.Context, *uuid.UUID, uuid.UUID, uuid.UUID) {
	n.proposal++
}

const secret = "worker-secret"

func setup() (*Service, *fakeRepo, *fakeRoom, *fakeNotifier) {
	repo, room, n := newFake(), &fakeRoom{enabled: true}, &fakeNotifier{}
	return New(repo, room, secret).WithCrypter(fakeCrypt{}).WithNotifier(n), repo, room, n
}

func invite(t *testing.T, s *Service, repo *fakeRepo) (*meetingdom.AgentSession, uuid.UUID, uuid.UUID) {
	t.Helper()
	ch, requester := uuid.New(), uuid.New()
	repo.inCall[requester] = true
	sess, err := s.Invite(context.Background(), uuid.New(), ch, repo.info.ID, requester)
	if err != nil {
		t.Fatalf("invite: %v", err)
	}
	return sess, ch, requester
}

func TestInvite(t *testing.T) {
	ctx := context.Background()
	t.Run("dispatches with ids only", func(t *testing.T) {
		s, repo, room, n := setup()
		sess, ch, _ := invite(t, s, repo)
		if len(room.dispatched) != 1 {
			t.Fatalf("dispatched %v", room.dispatched)
		}
		d := room.dispatched[0]
		if !strings.HasPrefix(d, ch.String()+"|"+AgentName+"|") || !strings.Contains(d, sess.ID.String()) {
			t.Errorf("dispatch %s", d)
		}
		if strings.Contains(d, "enc:") || strings.Contains(d, "key") {
			t.Errorf("metadata must carry ids only: %s", d)
		}
		if sess.DispatchID != "AD_1" || n.assistant != 1 {
			t.Errorf("dispatch id %q announced %d", sess.DispatchID, n.assistant)
		}
	})
	t.Run("disabled", func(t *testing.T) {
		s, repo, room, _ := setup()
		room.enabled = false
		_, err := s.Invite(ctx, uuid.New(), uuid.New(), repo.info.ID, uuid.New())
		if !errors.Is(err, meetingdom.ErrAssistantsDisabled) {
			t.Fatalf("got %v", err)
		}
		s2 := New(repo, &fakeRoom{enabled: true}, "")
		if s2.Enabled() {
			t.Error("no worker token must switch assistants off")
		}
	})
	t.Run("not a meeting agent", func(t *testing.T) {
		s, repo, _, _ := setup()
		repo.info.Type = agentdom.AgentTypeLLM
		if _, err := s.Invite(ctx, uuid.New(), uuid.New(), repo.info.ID, uuid.New()); !errors.Is(err, meetingdom.ErrNotMeetingAgent) {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("not configured", func(t *testing.T) {
		s, repo, _, _ := setup()
		repo.settings.STTModel = ""
		if _, err := s.Invite(ctx, uuid.New(), uuid.New(), repo.info.ID, uuid.New()); !errors.Is(err, meetingdom.ErrAssistantNotConfigured) {
			t.Fatalf("got %v", err)
		}
		repo.settings.STTModel, repo.settings.STTAPIKeySecret = "whisper-1", ""
		if _, err := s.Invite(ctx, uuid.New(), uuid.New(), repo.info.ID, uuid.New()); !errors.Is(err, meetingdom.ErrAssistantNotConfigured) {
			t.Fatalf("hosted provider without key: got %v", err)
		}
		repo.settings.STTBaseURL = "http://whisper:8000/v1"
		repo.inCall[uuid.Nil] = true
		if _, err := s.Invite(ctx, uuid.New(), uuid.New(), repo.info.ID, uuid.Nil); err != nil {
			t.Fatalf("self-hosted endpoint needs no key: %v", err)
		}
	})
	t.Run("inviter must be in the call", func(t *testing.T) {
		s, repo, _, _ := setup()
		if _, err := s.Invite(ctx, uuid.New(), uuid.New(), repo.info.ID, uuid.New()); !errors.Is(err, meetingdom.ErrNoOpenCall) {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("no open call", func(t *testing.T) {
		s, repo, _, _ := setup()
		repo.callID = uuid.Nil
		if _, err := s.Invite(ctx, uuid.New(), uuid.New(), repo.info.ID, uuid.New()); !errors.Is(err, meetingdom.ErrNoOpenCall) {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("twice", func(t *testing.T) {
		s, repo, _, _ := setup()
		_, ch, requester := invite(t, s, repo)
		if _, err := s.Invite(ctx, uuid.New(), ch, repo.info.ID, requester); !errors.Is(err, meetingdom.ErrAlreadyInCall) {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("dispatch failure closes the session", func(t *testing.T) {
		s, repo, room, _ := setup()
		room.fail = errors.New("boom")
		requester := uuid.New()
		repo.inCall[requester] = true
		if _, err := s.Invite(ctx, uuid.New(), uuid.New(), repo.info.ID, requester); err == nil {
			t.Fatal("want an error")
		}
		for _, sess := range repo.sessions {
			if sess.Status != meetingdom.SessionFailed || sess.LeftAt == nil {
				t.Errorf("session %+v", sess)
			}
		}
	})
}

func TestRemoveEndsSession(t *testing.T) {
	s, repo, room, _ := setup()
	sess, ch, _ := invite(t, s, repo)
	if err := s.Remove(context.Background(), uuid.New(), ch, repo.info.ID); err != nil {
		t.Fatal(err)
	}
	if len(room.deleted) != 1 || len(room.removed) != 1 || room.removed[0] != repo.info.ID.String() {
		t.Errorf("deleted %v removed %v", room.deleted, room.removed)
	}
	if repo.sessions[sess.ID].LeftAt == nil || repo.keys[sess.ID.String()] {
		t.Error("session must be closed and keys revoked")
	}
	if err := s.Remove(context.Background(), uuid.New(), ch, repo.info.ID); !errors.Is(err, meetingdom.ErrNotInCall) {
		t.Errorf("second remove: %v", err)
	}
}

func TestBootstrap(t *testing.T) {
	ctx := context.Background()
	s, repo, _, _ := setup()
	sess, _, requester := invite(t, s, repo)

	if _, err := s.Bootstrap(ctx, "wrong", sess.ID); !errors.Is(err, meetingdom.ErrWorkerUnauthorized) {
		t.Fatalf("wrong secret: %v", err)
	}
	if _, err := s.Bootstrap(ctx, "", sess.ID); !errors.Is(err, meetingdom.ErrWorkerUnauthorized) {
		t.Fatalf("empty secret: %v", err)
	}
	b, err := s.Bootstrap(ctx, secret, sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if b.LLMAPIKey != "llm" || b.STTAPIKey != "stt" {
		t.Errorf("keys must arrive decrypted, got %q %q", b.LLMAPIKey, b.STTAPIKey)
	}
	if !strings.HasPrefix(b.APIKey, "paca_") || len(b.APIKey) != len("paca_")+64 {
		t.Errorf("api key %q", b.APIKey)
	}
	if b.RequesterID == nil || *b.RequesterID != requester {
		t.Errorf("requester %v", b.RequesterID)
	}
	if repo.sessions[sess.ID].Status != meetingdom.SessionActive || !repo.keys[sess.ID.String()] {
		t.Error("session must be active with a live key")
	}
	b2, err := s.Bootstrap(ctx, secret, sess.ID)
	if err != nil || b2.APIKey == b.APIKey {
		t.Errorf("a second bootstrap must mint a new key (err %v)", err)
	}
	if err := s.Finish(ctx, secret, sess.ID); err != nil {
		t.Fatal(err)
	}
	if repo.keys[sess.ID.String()] {
		t.Error("finish must revoke the key")
	}
	if _, err := s.Bootstrap(ctx, secret, sess.ID); !errors.Is(err, meetingdom.ErrNotInCall) {
		t.Errorf("bootstrap after finish: %v", err)
	}
	if err := s.Finish(ctx, secret, sess.ID); err != nil {
		t.Errorf("finish is idempotent: %v", err)
	}
}

func TestAppendTranscript(t *testing.T) {
	ctx := context.Background()
	s, repo, _, _ := setup()
	sess, _, requester := invite(t, s, repo)
	stranger := uuid.New()
	err := s.AppendTranscript(ctx, secret, sess.ID, []meetingdom.SegmentInput{
		{SpeakerID: requester, Text: "  hello  "},
		{SpeakerID: stranger, Text: "not in the call"},
		{SpeakerID: requester, Text: "   "},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(repo.segments) != 1 || repo.segments[0].Text != "hello" {
		t.Fatalf("stored %+v", repo.segments)
	}
	repo.settings.StoreTranscript = false
	repo.segments = nil
	if err := s.AppendTranscript(ctx, secret, sess.ID, []meetingdom.SegmentInput{{SpeakerID: requester, Text: "private"}}); err != nil {
		t.Fatal(err)
	}
	if len(repo.segments) != 0 {
		t.Error("nothing is stored when the assistant does not keep a transcript")
	}
	// Someone who has left since speaking is still a speaker of this call: the
	// worker's last flush comes after the people are gone.
	gone := uuid.New()
	repo.settings.StoreTranscript = true
	repo.left[gone] = true
	repo.segments = nil
	if err := s.AppendTranscript(ctx, secret, sess.ID, []meetingdom.SegmentInput{{SpeakerID: gone, Text: "bye"}}); err != nil {
		t.Fatal(err)
	}
	if len(repo.segments) != 1 {
		t.Errorf("a participant who left must still be attributed, stored %d", len(repo.segments))
	}
	if err := s.AppendTranscript(ctx, "nope", sess.ID, nil); !errors.Is(err, meetingdom.ErrWorkerUnauthorized) {
		t.Errorf("got %v", err)
	}
}

func TestCreateProposal(t *testing.T) {
	ctx := context.Background()
	project := uuid.New()
	newP := func(ch uuid.UUID, kind, payload string) meetingdom.NewProposal {
		return meetingdom.NewProposal{ProjectID: project, AgentID: uuid.Nil, ChannelID: ch, Kind: kind, Payload: []byte(payload)}
	}
	t.Run("goes to the requester and expires", func(t *testing.T) {
		s, repo, _, n := setup()
		_, ch, requester := invite(t, s, repo)
		in := newP(ch, meetingdom.KindTaskCreate, `{"title":"Ship it"}`)
		in.AgentID = repo.info.ID
		p, err := s.CreateProposal(ctx, in)
		if err != nil {
			t.Fatal(err)
		}
		if p.TargetAccountID != requester || p.Status != meetingdom.StatusPending {
			t.Errorf("%+v", p)
		}
		if !p.ExpiresAt.After(time.Now().Add(ProposalTTL - time.Minute)) {
			t.Errorf("expires %v", p.ExpiresAt)
		}
		if n.proposal != 1 {
			t.Error("must announce")
		}
	})
	t.Run("named target must be in the call", func(t *testing.T) {
		s, repo, _, _ := setup()
		_, ch, requester := invite(t, s, repo)
		other, stranger := uuid.New(), uuid.New()
		repo.inCall[other] = true
		in := newP(ch, meetingdom.KindTaskCreate, `{"title":"x"}`)
		in.AgentID, in.TargetID = repo.info.ID, &other
		if p, err := s.CreateProposal(ctx, in); err != nil || p.TargetAccountID != other {
			t.Fatalf("named participant: %v %+v", err, p)
		}
		in.TargetID = &stranger
		if p, err := s.CreateProposal(ctx, in); err != nil || p.TargetAccountID != requester {
			t.Fatalf("a stranger is ignored, falls back to requester: %v %+v", err, p)
		}
	})
	t.Run("host first when configured", func(t *testing.T) {
		s, repo, _, _ := setup()
		host := uuid.New()
		repo.host = &host
		repo.settings.DefaultTarget = meetingdom.TargetHost
		_, ch, _ := invite(t, s, repo)
		in := newP(ch, meetingdom.KindTaskComment, `{"task_id":"`+uuid.NewString()+`","body":"hi"}`)
		in.AgentID = repo.info.ID
		p, err := s.CreateProposal(ctx, in)
		if err != nil || p.TargetAccountID != host {
			t.Fatalf("%v %+v", err, p)
		}
	})
	t.Run("limit, disabled kinds and notes only", func(t *testing.T) {
		s, repo, _, _ := setup()
		_, ch, _ := invite(t, s, repo)
		in := newP(ch, meetingdom.KindTaskCreate, `{"title":"x"}`)
		in.AgentID = repo.info.ID
		for i := 0; i < 2; i++ {
			if _, err := s.CreateProposal(ctx, in); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := s.CreateProposal(ctx, in); !errors.Is(err, meetingdom.ErrProposalLimit) {
			t.Fatalf("limit: %v", err)
		}
		repo.settings.MaxProposalsPerCall = 50
		repo.settings.ProposalKinds = []string{meetingdom.KindTaskComment}
		if _, err := s.CreateProposal(ctx, in); !errors.Is(err, meetingdom.ErrProposalKindDisabled) {
			t.Fatalf("kind: %v", err)
		}
		repo.settings.ProposalKinds = meetingdom.ProposalKinds
		repo.settings.Mode = meetingdom.ModeNotesOnly
		if _, err := s.CreateProposal(ctx, in); !errors.Is(err, meetingdom.ErrProposalKindDisabled) {
			t.Fatalf("notes only: %v", err)
		}
	})
	t.Run("invalid payload and not in a call", func(t *testing.T) {
		s, repo, _, _ := setup()
		_, ch, _ := invite(t, s, repo)
		bad := newP(ch, meetingdom.KindTaskCreate, `{"title":""}`)
		bad.AgentID = repo.info.ID
		if _, err := s.CreateProposal(ctx, bad); !errors.Is(err, meetingdom.ErrProposalInvalid) {
			t.Fatalf("got %v", err)
		}
		away := newP(uuid.New(), meetingdom.KindTaskCreate, `{"title":"x"}`)
		away.AgentID = repo.info.ID
		if _, err := s.CreateProposal(ctx, away); !errors.Is(err, meetingdom.ErrNotInCall) {
			t.Fatalf("a proposal needs the assistant to be in that call: %v", err)
		}
	})
}

func TestClaimAndResolve(t *testing.T) {
	ctx := context.Background()
	s, repo, _, _ := setup()
	_, ch, requester := invite(t, s, repo)
	project := uuid.New()
	p, err := s.CreateProposal(ctx, meetingdom.NewProposal{ProjectID: project, AgentID: repo.info.ID, ChannelID: ch,
		Kind: meetingdom.KindTaskCreate, Payload: []byte(`{"title":"x"}`)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Resolve(ctx, project, p.ID, requester, meetingdom.StatusResolved, nil); !errors.Is(err, meetingdom.ErrProposalClosed) {
		t.Fatalf("resolving before claiming: %v", err)
	}
	if _, err := s.Claim(ctx, project, p.ID, requester); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Claim(ctx, project, p.ID, requester); err != nil {
		t.Fatalf("the same person may claim again after a reload: %v", err)
	}
	if _, err := s.Claim(ctx, project, p.ID, uuid.New()); !errors.Is(err, meetingdom.ErrProposalClosed) {
		t.Fatalf("a second person cannot claim: %v", err)
	}
	if _, err := s.Resolve(ctx, project, p.ID, uuid.New(), meetingdom.StatusResolved, nil); !errors.Is(err, meetingdom.ErrProposalClosed) {
		t.Fatalf("only the claimer resolves: %v", err)
	}
	done, err := s.Resolve(ctx, project, p.ID, requester, meetingdom.StatusResolved, []byte(`{"task_key":"PACA-1"}`))
	if err != nil || done.Status != meetingdom.StatusResolved || !strings.Contains(string(done.Result), "PACA-1") {
		t.Fatalf("%v %+v", err, done)
	}
	if _, err := s.Resolve(ctx, project, p.ID, requester, meetingdom.StatusRejected, nil); !errors.Is(err, meetingdom.ErrProposalClosed) {
		t.Fatalf("a closed proposal stays closed: %v", err)
	}
	if _, err := s.Resolve(ctx, project, p.ID, requester, "weird", nil); !errors.Is(err, meetingdom.ErrProposalInvalid) {
		t.Fatalf("unknown status: %v", err)
	}
}

func TestProposalExpires(t *testing.T) {
	ctx := context.Background()
	s, repo, _, _ := setup()
	_, ch, requester := invite(t, s, repo)
	project := uuid.New()
	p, _ := s.CreateProposal(ctx, meetingdom.NewProposal{ProjectID: project, AgentID: repo.info.ID, ChannelID: ch,
		Kind: meetingdom.KindTaskCreate, Payload: []byte(`{"title":"x"}`)})
	s.now = func() time.Time { return time.Now().Add(time.Hour) }
	if _, err := s.Claim(ctx, project, p.ID, requester); !errors.Is(err, meetingdom.ErrProposalClosed) {
		t.Fatalf("expired proposal: %v", err)
	}
	if repo.proposals[p.ID].Status != meetingdom.StatusExpired {
		t.Errorf("status %s", repo.proposals[p.ID].Status)
	}
}

func TestUpdateSettings(t *testing.T) {
	ctx := context.Background()
	s, repo, _, _ := setup()
	str := func(v string) *string { return &v }
	num := func(v int) *int { return &v }
	list := func(v ...string) *[]string { return &v }

	got, err := s.UpdateSettings(ctx, uuid.New(), repo.info.ID, meetingdom.SettingsPatch{
		STTProvider: str("deepgram"), STTAPIKey: str("k-123"), Mode: str(meetingdom.ModeAutoSuggest),
		SuggestIntervalSeconds: num(60), STTLanguages: list(" en ", "en", "vi", ""), ProposalKinds: list(meetingdom.KindTaskCreate),
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.STTProvider != "deepgram" || got.Mode != meetingdom.ModeAutoSuggest || got.SuggestIntervalSeconds != 60 {
		t.Errorf("%+v", got)
	}
	if strings.Join(got.STTLanguages, ",") != "en,vi" {
		t.Errorf("languages %v", got.STTLanguages)
	}
	if repo.settings.STTAPIKeySecret != "enc:k-123" {
		t.Errorf("key must be stored encrypted: %q", repo.settings.STTAPIKeySecret)
	}
	if _, err := s.UpdateSettings(ctx, uuid.New(), repo.info.ID, meetingdom.SettingsPatch{Announcement: str("hi")}); err != nil || repo.settings.STTAPIKeySecret != "enc:k-123" {
		t.Errorf("an unrelated patch must leave the key alone (err %v)", err)
	}
	// An edit of the transcription setup cannot leave an agent unable to listen.
	if _, err := s.UpdateSettings(ctx, uuid.New(), repo.info.ID, meetingdom.SettingsPatch{STTAPIKey: str("")}); !errors.Is(err, meetingdom.ErrSettingsInvalid) {
		t.Errorf("clearing the only key must be refused, got %v", err)
	}
	if _, err := s.UpdateSettings(ctx, uuid.New(), repo.info.ID, meetingdom.SettingsPatch{STTAPIKey: str(""), STTBaseURL: str("http://whisper:8000/v1")}); err != nil || repo.settings.STTAPIKeySecret != "" {
		t.Errorf("a key may be cleared when an endpoint replaces it (err %v)", err)
	}
	if _, err := s.UpdateSettings(ctx, uuid.New(), repo.info.ID, meetingdom.SettingsPatch{STTModel: str("")}); !errors.Is(err, meetingdom.ErrSettingsInvalid) {
		t.Errorf("an empty model must be refused, got %v", err)
	}
	repo.settings.STTProvider, repo.settings.STTModel, repo.settings.STTBaseURL = "", "", ""
	if _, err := s.UpdateSettings(ctx, uuid.New(), repo.info.ID, meetingdom.SettingsPatch{Announcement: str("hello")}); err != nil {
		t.Errorf("an unrelated edit leaves an older unconfigured agent alone (err %v)", err)
	}
	repo.settings.STTProvider, repo.settings.STTModel, repo.settings.STTBaseURL = "deepgram", "nova-3", "http://whisper:8000/v1"
	for name, p := range map[string]meetingdom.SettingsPatch{
		"provider":  {STTProvider: str("nope")},
		"mode":      {Mode: str("loud")},
		"interval":  {SuggestIntervalSeconds: num(5)},
		"kinds":     {ProposalKinds: list("task.delete")},
		"retention": {TranscriptRetentionDays: num(0)},
		"base url":  {STTBaseURL: str("ftp://x")},
		"target":    {DefaultTarget: str("everyone")},
		"summary":   {SummaryTarget: str("email")},
		"max":       {MaxProposalsPerCall: num(101)},
	} {
		if _, err := s.UpdateSettings(ctx, uuid.New(), repo.info.ID, p); !errors.Is(err, meetingdom.ErrSettingsInvalid) {
			t.Errorf("%s: got %v", name, err)
		}
	}
	repo.info.Type = agentdom.AgentTypeACP
	if _, err := s.GetSettings(ctx, uuid.New(), repo.info.ID); !errors.Is(err, meetingdom.ErrNotMeetingAgent) {
		t.Errorf("got %v", err)
	}
}

func TestNotesAndSuggestionsNeedTheModel(t *testing.T) {
	ctx := context.Background()
	str := func(v string) *string { return &v }
	s, repo, _, _ := setup()
	repo.info.LLMProvider, repo.info.LLMModel, repo.info.LLMKeySecret = "", "", ""
	repo.settings.Mode, repo.settings.SummaryTarget = meetingdom.ModeNotesOnly, meetingdom.SummaryNone

	// Transcription alone: nothing to refuse, and it can be invited.
	if _, err := s.UpdateSettings(ctx, uuid.New(), repo.info.ID, meetingdom.SettingsPatch{Announcement: str("hi")}); err != nil {
		t.Fatalf("transcription only needs no model: %v", err)
	}
	requester := uuid.New()
	repo.inCall[requester] = true
	if _, err := s.Invite(ctx, uuid.New(), uuid.New(), repo.info.ID, requester); err != nil {
		t.Fatalf("a transcription-only assistant is ready without a model: %v", err)
	}

	// Turning either option on needs the model.
	for name, p := range map[string]meetingdom.SettingsPatch{
		"suggestions": {Mode: str(meetingdom.ModeOnDemand)},
		"notes":       {SummaryTarget: str(meetingdom.SummaryChannelMessage)},
	} {
		if _, err := s.UpdateSettings(ctx, uuid.New(), repo.info.ID, p); !errors.Is(err, meetingdom.ErrModelRequired) {
			t.Errorf("%s without a model: got %v", name, err)
		}
	}
	if repo.settings.Mode != meetingdom.ModeNotesOnly || repo.settings.SummaryTarget != meetingdom.SummaryNone {
		t.Errorf("a refused change must not be stored: %+v", repo.settings)
	}
	repo.info.LLMProvider, repo.info.LLMModel, repo.info.LLMKeySecret = "openai", "gpt", "enc:k"
	if _, err := s.UpdateSettings(ctx, uuid.New(), repo.info.ID, meetingdom.SettingsPatch{Mode: str(meetingdom.ModeOnDemand), SummaryTarget: str(meetingdom.SummaryChannelMessage)}); err != nil {
		t.Errorf("with a model both can be turned on: %v", err)
	}

	// An agent that already has an option on but lost its model cannot be invited.
	repo.info.LLMKeySecret = ""
	repo.sessions = map[uuid.UUID]*meetingdom.AgentSession{}
	if _, err := s.Invite(ctx, uuid.New(), uuid.New(), repo.info.ID, requester); !errors.Is(err, meetingdom.ErrAssistantNotConfigured) {
		t.Errorf("notes on without a model: got %v", err)
	}
}

func TestNeedsModelFollowsTheDefaults(t *testing.T) {
	s, _, _, _ := setup()
	str := func(v string) *string { return &v }
	if s.NeedsModel(meetingdom.SettingsPatch{}) {
		t.Error("a new agent is transcription only")
	}
	if !s.NeedsModel(meetingdom.SettingsPatch{Mode: str(meetingdom.ModeAutoSuggest)}) || !s.NeedsModel(meetingdom.SettingsPatch{SummaryTarget: str(meetingdom.SummaryChannelMessage)}) {
		t.Error("suggestions or notes need the model")
	}
	if s.NeedsModel(meetingdom.SettingsPatch{Mode: str(meetingdom.ModeNotesOnly), SummaryTarget: str(meetingdom.SummaryNone)}) {
		t.Error("both off needs none")
	}
}

func TestValidateListening(t *testing.T) {
	s, _, _, _ := setup()
	str := func(v string) *string { return &v }
	ok := meetingdom.SettingsPatch{STTProvider: str("openai"), STTModel: str("whisper-1"), STTAPIKey: str("k")}
	if err := s.ValidateListening(ok); err != nil {
		t.Fatalf("a complete setup: %v", err)
	}
	if err := s.ValidateListening(meetingdom.SettingsPatch{STTProvider: str("openai"), STTModel: str("w"), STTBaseURL: str("http://w/v1")}); err != nil {
		t.Errorf("an endpoint replaces the key: %v", err)
	}
	for name, p := range map[string]meetingdom.SettingsPatch{
		"empty":         {},
		"no provider":   {STTModel: str("w"), STTAPIKey: str("k")},
		"bad provider":  {STTProvider: str("nope"), STTModel: str("w"), STTAPIKey: str("k")},
		"no model":      {STTProvider: str("openai"), STTAPIKey: str("k")},
		"no key or url": {STTProvider: str("openai"), STTModel: str("w")},
		"blank model":   {STTProvider: str("openai"), STTModel: str("  "), STTAPIKey: str("k")},
	} {
		if err := s.ValidateListening(p); !errors.Is(err, meetingdom.ErrSettingsInvalid) {
			t.Errorf("%s: got %v", name, err)
		}
	}
}

func TestCallEndedClosesAssistants(t *testing.T) {
	s, repo, room, _ := setup()
	sess, ch, _ := invite(t, s, repo)
	if _, err := s.Bootstrap(context.Background(), secret, sess.ID); err != nil {
		t.Fatal(err)
	}
	s.CallEnded(context.Background(), ch)
	if repo.sessions[sess.ID].LeftAt == nil || repo.keys[sess.ID.String()] || len(room.deleted) != 1 {
		t.Errorf("session %+v keys %v deleted %v", repo.sessions[sess.ID], repo.keys, room.deleted)
	}
}
