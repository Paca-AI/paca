package postgres

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	meetingdom "github.com/Paca-AI/api/internal/domain/meeting"
)

type meetingFx struct {
	db      *iamFxDB
	project uuid.UUID
	agent   uuid.UUID
	channel uuid.UUID
	call    uuid.UUID
	alice   uuid.UUID
	bob     uuid.UUID
	repo    *MeetingRepository
}

type iamFxDB = struct{ *iamFx }

func newMeetingFx(t *testing.T) *meetingFx {
	t.Helper()
	db := newIAMPGTestDB(t)
	fx := newIAMFx(t, db)
	m := &meetingFx{db: &iamFxDB{fx}, repo: NewMeetingRepository(db)}
	m.project = fx.project()
	m.agent = fx.agent(&m.project, false)
	pgExec(t, db, `UPDATE agents SET agent_type = 'meeting' WHERE id = $1`, m.agent)
	fx.member(m.project, m.agent, false)
	m.alice, m.bob = fx.user(false), fx.user(false)
	m.channel = uuid.New()
	pgExec(t, db, `INSERT INTO channels (id, scope, project_id, kind, name) VALUES ($1, 'project', $2, 'voice', 'standup')`, m.channel, m.project)
	m.call = uuid.New()
	pgExec(t, db, `INSERT INTO call_sessions (id, channel_id, started_by) VALUES ($1, $2, $3)`, m.call, m.channel, m.alice)
	for _, a := range []uuid.UUID{m.alice, m.bob} {
		pgExec(t, db, `INSERT INTO call_participants (session_id, account_id) VALUES ($1, $2)`, m.call, a)
	}
	return m
}

func TestMeetingRepository_SettingsAndAgentInfo(t *testing.T) {
	m := newMeetingFx(t)
	ctx := t.Context()

	info, err := m.repo.AgentInfo(ctx, m.project, m.agent)
	if err != nil || info.Type != "meeting" || info.LLMProvider != "openai" {
		t.Fatalf("info %+v err %v", info, err)
	}
	if _, err := m.repo.AgentInfo(ctx, uuid.New(), m.agent); err == nil {
		t.Error("an agent of another project must not resolve")
	}

	s, err := m.repo.GetSettings(ctx, m.agent)
	if err != nil || s.Mode != meetingdom.ModeOnDemand || !s.StoreTranscript || len(s.ProposalKinds) != 4 {
		t.Fatalf("defaults %+v err %v", s, err)
	}
	s.STTProvider, s.STTModel, s.STTAPIKeySecret = "openai", "whisper-1", "enc"
	s.STTLanguages = []string{"en", "vi"}
	s.Mode = meetingdom.ModeAutoSuggest
	s.ProposalKinds = []string{meetingdom.KindTaskCreate}
	s.Announcement = "hello"
	if err := m.repo.SaveSettings(ctx, s); err != nil {
		t.Fatal(err)
	}
	got, err := m.repo.GetSettings(ctx, m.agent)
	if err != nil || got.STTModel != "whisper-1" || len(got.STTLanguages) != 2 || got.ProposalKinds[0] != "task.create" || !got.HasSTTKey() {
		t.Fatalf("saved %+v err %v", got, err)
	}
	s.Mode = "bogus"
	if err := m.repo.SaveSettings(ctx, s); err == nil {
		t.Error("the schema must reject an unknown mode")
	}

	list, err := m.repo.ListMeetingAgents(ctx, m.project)
	if err != nil || len(list) != 1 || list[0].Mode != meetingdom.ModeAutoSuggest {
		t.Fatalf("picker %+v err %v", list, err)
	}
}

func TestMeetingRepository_AgentSessionsAndKeys(t *testing.T) {
	m := newMeetingFx(t)
	ctx := t.Context()

	id, host, err := m.repo.OpenCall(ctx, m.channel)
	if err != nil || id != m.call || host == nil || *host != m.alice {
		t.Fatalf("open call %v %v %v", id, host, err)
	}
	if ok, _ := m.repo.IsCallParticipant(ctx, m.call, m.bob); !ok {
		t.Error("bob is in the call")
	}
	if ok, _ := m.repo.IsCallParticipant(ctx, m.call, uuid.New()); ok {
		t.Error("a stranger is not")
	}
	pgExec(t, m.db.db, `UPDATE call_participants SET left_at = NOW() WHERE account_id = $1`, m.bob)
	if ok, _ := m.repo.IsCallParticipant(ctx, m.call, m.bob); ok {
		t.Error("bob has left")
	}
	if ok, _ := m.repo.WasCallParticipant(ctx, m.call, m.bob); !ok {
		t.Error("but he was in the call, so his last words still count")
	}
	if ok, _ := m.repo.WasCallParticipant(ctx, m.call, uuid.New()); ok {
		t.Error("a stranger never was")
	}

	sess := &meetingdom.AgentSession{ID: uuid.New(), CallSessionID: m.call, AgentID: m.agent, InvitedBy: &m.alice}
	if err := m.repo.CreateAgentSession(ctx, sess); err != nil {
		t.Fatal(err)
	}
	dup := &meetingdom.AgentSession{ID: uuid.New(), CallSessionID: m.call, AgentID: m.agent, InvitedBy: &m.bob}
	if err := m.repo.CreateAgentSession(ctx, dup); !errors.Is(err, meetingdom.ErrAlreadyInCall) {
		t.Fatalf("second open session: %v", err)
	}
	if err := m.repo.SetAgentSessionDispatch(ctx, sess.ID, "AD_9"); err != nil {
		t.Fatal(err)
	}
	open, err := m.repo.OpenAgentSession(ctx, m.channel, m.agent)
	if err != nil || open.DispatchID != "AD_9" || open.ChannelID != m.channel || open.ProjectID != m.project || open.AgentName != "A" {
		t.Fatalf("open session %+v err %v", open, err)
	}
	if list, _ := m.repo.ListAgentSessions(ctx, m.channel); len(list) != 1 {
		t.Fatalf("list %d", len(list))
	}

	if err := m.repo.CreateWorkerKey(ctx, m.agent, sess.ID, "hash-1", "abcd1234", time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	live := func() int {
		var n int
		if err := m.db.db.Get(&n, `SELECT count(*) FROM api_keys WHERE account_id = $1 AND name = $2 AND revoked_at IS NULL`,
			m.agent, workerKeyName(sess.ID)); err != nil {
			t.Fatal(err)
		}
		return n
	}
	if live() != 1 {
		t.Fatal("key must be live")
	}
	if err := m.repo.RevokeWorkerKeys(ctx, sess.ID); err != nil || live() != 0 {
		t.Fatalf("revoke: %v live=%d", err, live())
	}

	if err := m.repo.SetAgentSessionStatus(ctx, sess.ID, meetingdom.SessionLeft); err != nil {
		t.Fatal(err)
	}
	if _, err := m.repo.OpenAgentSession(ctx, m.channel, m.agent); !errors.Is(err, meetingdom.ErrNotInCall) {
		t.Fatalf("after leaving: %v", err)
	}
	again := &meetingdom.AgentSession{ID: uuid.New(), CallSessionID: m.call, AgentID: m.agent, InvitedBy: &m.alice}
	if err := m.repo.CreateAgentSession(ctx, again); err != nil {
		t.Fatalf("an assistant can be invited again after it left: %v", err)
	}
	if err := m.repo.EndAgentSessions(ctx, m.call); err != nil {
		t.Fatal(err)
	}
	if list, _ := m.repo.ListAgentSessions(ctx, m.channel); len(list) != 0 {
		t.Errorf("ended call has %d open sessions", len(list))
	}
}

func TestMeetingRepository_Transcript(t *testing.T) {
	m := newMeetingFx(t)
	ctx := t.Context()
	base := time.Now().Add(-time.Minute)
	err := m.repo.AppendSegments(ctx, m.call, m.channel, 7, []meetingdom.SegmentInput{
		{SpeakerID: m.alice, Text: "one", SpokenAt: base},
		{SpeakerID: m.bob, Text: "two", SpokenAt: base.Add(time.Second)},
		{SpeakerID: m.alice, Text: "three", SpokenAt: base.Add(2 * time.Second)},
	})
	if err != nil {
		t.Fatal(err)
	}
	segs, err := m.repo.ListSegments(ctx, m.call, 2)
	if err != nil || len(segs) != 2 || segs[0].Text != "two" || segs[1].Text != "three" {
		t.Fatalf("the latest two, oldest first: %+v err %v", segs, err)
	}
	pgExec(t, m.db.db, `UPDATE call_transcript_segments SET expires_at = NOW() - interval '1 day' WHERE text = 'one'`)
	if n, err := m.repo.DeleteExpiredSegments(ctx); err != nil || n != 1 {
		t.Fatalf("expired %d err %v", n, err)
	}
	if segs, _ := m.repo.ListSegments(ctx, m.call, 10); len(segs) != 2 {
		t.Errorf("left %d", len(segs))
	}
}

func TestMeetingRepository_Proposals(t *testing.T) {
	m := newMeetingFx(t)
	ctx := t.Context()
	now := time.Now()
	mk := func(target uuid.UUID, expires time.Time) *meetingdom.Proposal {
		p := &meetingdom.Proposal{ID: uuid.New(), ProjectID: m.project, ChannelID: &m.channel, CallSessionID: &m.call,
			AgentAccountID: m.agent, TargetAccountID: target, Kind: meetingdom.KindTaskCreate, Payload: []byte(`{"title":"x"}`),
			Status: meetingdom.StatusPending, ExpiresAt: expires, CreatedAt: now}
		if err := m.repo.CreateProposal(ctx, p); err != nil {
			t.Fatal(err)
		}
		return p
	}
	p := mk(m.alice, now.Add(time.Hour))
	old := mk(m.bob, now.Add(-time.Minute))

	got, err := m.repo.GetProposal(ctx, m.project, p.ID)
	if err != nil || got.AgentName != "A" || got.Status != meetingdom.StatusPending || string(got.Payload) != `{"title": "x"}` {
		t.Fatalf("%+v err %v", got, err)
	}
	if _, err := m.repo.GetProposal(ctx, uuid.New(), p.ID); !errors.Is(err, meetingdom.ErrProposalNotFound) {
		t.Errorf("another project must not see it: %v", err)
	}
	if n, _ := m.repo.CountProposals(ctx, m.call, m.agent); n != 2 {
		t.Errorf("count %d", n)
	}
	mine, _ := m.repo.ListProposals(ctx, m.project, nil, &m.alice, 10)
	if len(mine) != 1 || mine[0].ID != p.ID {
		t.Errorf("mine %+v", mine)
	}
	if all, _ := m.repo.ListProposals(ctx, m.project, &m.channel, nil, 10); len(all) != 2 {
		t.Errorf("channel %d", len(all))
	}

	ok, err := m.repo.TransitionProposal(ctx, p.ID, []string{meetingdom.StatusPending}, meetingdom.StatusClaimed, &m.alice, nil)
	if err != nil || !ok {
		t.Fatalf("claim: %v %v", ok, err)
	}
	if ok, _ := m.repo.TransitionProposal(ctx, p.ID, []string{meetingdom.StatusPending}, meetingdom.StatusClaimed, &m.bob, nil); ok {
		t.Error("compare-and-set: a second claim must lose")
	}
	ok, err = m.repo.TransitionProposal(ctx, p.ID, []string{meetingdom.StatusClaimed}, meetingdom.StatusResolved, &m.alice, []byte(`{"task_key":"P-1"}`))
	if err != nil || !ok {
		t.Fatalf("resolve: %v %v", ok, err)
	}
	got, _ = m.repo.GetProposal(ctx, m.project, p.ID)
	if got.Status != meetingdom.StatusResolved || got.ResolvedBy == nil || *got.ResolvedBy != m.alice || string(got.Result) != `{"task_key": "P-1"}` {
		t.Errorf("%+v result %s", got, got.Result)
	}

	if n, err := m.repo.ExpireProposals(ctx); err != nil || n != 1 {
		t.Fatalf("expire %d err %v", n, err)
	}
	if got, _ := m.repo.GetProposal(ctx, m.project, old.ID); got.Status != meetingdom.StatusExpired {
		t.Errorf("status %s", got.Status)
	}
}
