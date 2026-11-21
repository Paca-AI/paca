package chatsvc

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	chatdom "github.com/Paca-AI/api/internal/domain/chat"
)

// channelRepo is an in-memory stand-in for the channel-mention parts of the repository.
type channelRepo struct {
	chatdom.Repository
	channel  *chatdom.Channel
	kinds    map[uuid.UUID]string
	members  map[uuid.UUID]uuid.UUID // account -> project_members id
	messages []*chatdom.Message
	runs     map[uuid.UUID]*chatdom.ChannelAgentRun
	claimed  map[uuid.UUID]bool
	reply    string
}

func (r *channelRepo) FindChannel(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) (*chatdom.Channel, error) {
	return r.channel, nil
}
func (r *channelRepo) CreateMessage(_ context.Context, m *chatdom.Message) error {
	m.ID = uuid.New()
	m.Seq = int64(len(r.messages) + 1)
	m.AuthorName = map[bool]string{true: "Atlas", false: "Mina"}[r.kinds[m.AuthorID] == chatdom.AccountAgent]
	r.messages = append(r.messages, m)
	return nil
}
func (r *channelRepo) FindMessage(_ context.Context, _, id, _ uuid.UUID) (*chatdom.Message, error) {
	for _, m := range r.messages {
		if m.ID == id {
			return m, nil
		}
	}
	return nil, chatdom.ErrMessageNotFound
}
func (r *channelRepo) ListMessages(_ context.Context, _, _ uuid.UUID, f chatdom.MessageFilter) ([]*chatdom.Message, bool, error) {
	var out []*chatdom.Message
	for _, m := range r.messages {
		if f.BeforeSeq != nil && m.Seq < *f.BeforeSeq {
			out = append(out, m)
		}
	}
	return out, false, nil
}
func (r *channelRepo) ChannelRecipients(context.Context, uuid.UUID) ([]uuid.UUID, error) {
	return nil, nil
}
func (r *channelRepo) RecordMessageEffects(context.Context, *chatdom.Message, []uuid.UUID) error {
	return nil
}
func (r *channelRepo) AccountKind(_ context.Context, id uuid.UUID) (string, bool, error) {
	k, ok := r.kinds[id]
	return k, ok, nil
}
func (r *channelRepo) ProjectMemberID(_ context.Context, _, id uuid.UUID) (uuid.UUID, bool, error) {
	m, ok := r.members[id]
	return m, ok, nil
}
func (r *channelRepo) CreateAgentRun(_ context.Context, a *chatdom.ChannelAgentRun) error {
	r.runs[a.ConversationID] = a
	return nil
}
func (r *channelRepo) FindAgentRun(_ context.Context, id uuid.UUID) (*chatdom.ChannelAgentRun, error) {
	return r.runs[id], nil
}
func (r *channelRepo) ClaimAgentRunPost(_ context.Context, id uuid.UUID) (bool, error) {
	if r.runs[id].Posted || r.claimed[id] {
		return false, nil
	}
	if r.claimed == nil {
		r.claimed = map[uuid.UUID]bool{}
	}
	r.claimed[id] = true
	return true, nil
}
func (r *channelRepo) ReleaseAgentRunPost(_ context.Context, id uuid.UUID) error {
	delete(r.claimed, id)
	return nil
}
func (r *channelRepo) MarkAgentRunPosted(_ context.Context, id uuid.UUID) error {
	r.runs[id].Posted = true
	return nil
}
func (r *channelRepo) AgentReply(context.Context, uuid.UUID, int, int) (string, error) {
	return r.reply, nil
}

type fakeTrigger struct {
	calls []string
	by    []uuid.UUID
	err   error
	conv  uuid.UUID
}

func (f *fakeTrigger) TriggerChannelMention(_ context.Context, _, _, by uuid.UUID, message string) (uuid.UUID, error) {
	if f.err != nil {
		return uuid.Nil, f.err
	}
	f.calls = append(f.calls, message)
	f.by = append(f.by, by)
	return f.conv, nil
}

type channelFixture struct {
	svc            *Service
	repo           *channelRepo
	trig           *fakeTrigger
	project, ch    uuid.UUID
	person, agent  uuid.UUID
	personMemberID uuid.UUID
}

func setupChannel(kind string) channelFixture {
	f := channelFixture{project: uuid.New(), ch: uuid.New(), person: uuid.New(), agent: uuid.New(), personMemberID: uuid.New()}
	f.repo = &channelRepo{
		channel: &chatdom.Channel{ID: f.ch, Kind: kind, Name: "backend", ProjectID: &f.project},
		kinds:   map[uuid.UUID]string{f.person: chatdom.AccountUser, f.agent: chatdom.AccountAgent},
		members: map[uuid.UUID]uuid.UUID{f.person: f.personMemberID, f.agent: uuid.New()},
		runs:    map[uuid.UUID]*chatdom.ChannelAgentRun{},
	}
	f.trig = &fakeTrigger{conv: uuid.New()}
	f.svc = New(f.repo).WithAgentTrigger(f.trig)
	return f
}

func mentionOf(id uuid.UUID, name string) []byte {
	return []byte(`[{"type":"paragraph","content":[{"type":"teamMention","props":{"id":"` + id.String() + `","name":"` + name + `"}},{"type":"text","text":" please look at this"}]}]`)
}

func TestMention_StartsARunAsThePersonAndLinksIt(t *testing.T) {
	f := setupChannel(chatdom.KindPublic)
	if _, err := f.svc.PostMessage(context.Background(), f.project, f.ch, f.person, []byte(`[{"type":"paragraph","content":[{"type":"text","text":"earlier"}]}]`), nil); err != nil {
		t.Fatal(err)
	}
	m, err := f.svc.PostMessage(context.Background(), f.project, f.ch, f.person, mentionOf(f.agent, "Atlas"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(f.trig.calls) != 1 || f.trig.by[0] != f.personMemberID {
		t.Fatalf("one run, started as the author's member: %v %v", f.trig.calls, f.trig.by)
	}
	if p := f.trig.calls[0]; !strings.Contains(p, "@Atlas  please look at this") || !strings.Contains(p, "earlier") || !strings.Contains(p, "#backend") {
		t.Errorf("the agent is told who, where, what and what came before: %q", p)
	}
	run := f.repo.runs[f.trig.conv]
	if run == nil || run.SourceMessageID != m.ID || run.ThreadRootID != m.ID || run.AgentID != f.agent || run.RequesterID != f.person {
		t.Errorf("run is linked to the mentioning message: %+v", run)
	}
}

func TestMention_PrivateChannelSharesOnlyTheMessage(t *testing.T) {
	f := setupChannel(chatdom.KindPrivate)
	_, _ = f.svc.PostMessage(context.Background(), f.project, f.ch, f.person, []byte(`[{"type":"paragraph","content":[{"type":"text","text":"secret earlier"}]}]`), nil)
	_, _ = f.svc.PostMessage(context.Background(), f.project, f.ch, f.person, mentionOf(f.agent, "Atlas"), nil)
	if len(f.trig.calls) != 1 || strings.Contains(f.trig.calls[0], "secret earlier") {
		t.Errorf("a private channel's history is not handed to the agent: %v", f.trig.calls)
	}
}

func TestMention_Skips(t *testing.T) {
	cases := map[string]func(f *channelFixture) uuid.UUID{
		"an agent not in the project": func(f *channelFixture) uuid.UUID { delete(f.repo.members, f.agent); return f.person },
		"an author outside the project": func(f *channelFixture) uuid.UUID {
			delete(f.repo.members, f.person)
			return f.person
		},
		"a person mentioned": func(f *channelFixture) uuid.UUID {
			other := uuid.New()
			f.repo.kinds[other] = chatdom.AccountUser
			f.repo.members[other] = uuid.New()
			f.agent = other
			return f.person
		},
		"an agent author (no agent loops)": func(f *channelFixture) uuid.UUID {
			second := uuid.New()
			f.repo.kinds[second] = chatdom.AccountAgent
			f.repo.members[second] = uuid.New()
			return second
		},
	}
	for name, arrange := range cases {
		t.Run(name, func(t *testing.T) {
			f := setupChannel(chatdom.KindPublic)
			author := arrange(&f)
			if _, err := f.svc.PostMessage(context.Background(), f.project, f.ch, author, mentionOf(f.agent, "Atlas"), nil); err != nil {
				t.Fatal(err)
			}
			if len(f.trig.calls) != 0 {
				t.Errorf("no run: %v", f.trig.calls)
			}
		})
	}
}

func TestMention_RefusedRunDoesNotLoseTheMessage(t *testing.T) {
	f := setupChannel(chatdom.KindPublic)
	f.trig.err = errors.New("forbidden")
	m, err := f.svc.PostMessage(context.Background(), f.project, f.ch, f.person, mentionOf(f.agent, "Atlas"), nil)
	if err != nil || m == nil {
		t.Fatalf("the message is stored whatever the trigger does: %v", err)
	}
	if len(f.repo.runs) != 0 {
		t.Error("no link for a run that did not start")
	}
}

func TestMention_NoTriggerNoRun(t *testing.T) {
	f := setupChannel(chatdom.KindPublic)
	svc := New(f.repo) // not wired
	if _, err := svc.PostMessage(context.Background(), f.project, f.ch, f.person, mentionOf(f.agent, "Atlas"), nil); err != nil {
		t.Fatal(err)
	}
	if len(f.repo.runs) != 0 {
		t.Error("nothing without a trigger")
	}
}

func TestRunEnd_PostsTheAnswerInTheThreadOnce(t *testing.T) {
	f := setupChannel(chatdom.KindPublic)
	m, _ := f.svc.PostMessage(context.Background(), f.project, f.ch, f.person, mentionOf(f.agent, "Atlas"), nil)
	f.repo.reply = "Looks fine.\n\nShip it."
	if err := f.svc.OnChannelRunEnd(context.Background(), f.trig.conv, 7); err != nil {
		t.Fatal(err)
	}
	reply := f.repo.messages[len(f.repo.messages)-1]
	if reply.AuthorID != f.agent || reply.ThreadRootID == nil || *reply.ThreadRootID != m.ID || reply.RunID == nil || *reply.RunID != f.trig.conv {
		t.Fatalf("the agent replies in the thread, linked to its run: %+v", reply)
	}
	if got := mentionedAccounts(reply.Content); len(got) != 1 || got[0] != f.person {
		t.Errorf("the asker is mentioned so it reaches their Inbox: %v", got)
	}
	if !strings.Contains(reply.ContentText, "Ship it.") {
		t.Errorf("text: %q", reply.ContentText)
	}
	n := len(f.repo.messages)
	if err := f.svc.OnChannelRunEnd(context.Background(), f.trig.conv, 7); err != nil || len(f.repo.messages) != n {
		t.Error("a repeated turn_end adds nothing")
	}
	if err := f.svc.OnChannelRunEnd(context.Background(), uuid.New(), 1); err != nil {
		t.Error("a run no channel started is ignored")
	}
}

func TestRunEnd_ADeadWorkersClaimDoesNotLoseTheAnswer(t *testing.T) {
	f := setupChannel(chatdom.KindPublic)
	if _, err := f.svc.PostMessage(context.Background(), f.project, f.ch, f.person, mentionOf(f.agent, "Atlas"), nil); err != nil {
		t.Fatal(err)
	}
	f.repo.reply = "Done."
	// Another delivery holds the claim: this one must be retried, not acked.
	f.repo.claimed = map[uuid.UUID]bool{f.trig.conv: true}
	n := len(f.repo.messages)
	if err := f.svc.OnChannelRunEnd(context.Background(), f.trig.conv, 7); !errors.Is(err, errReplyInFlight) || len(f.repo.messages) != n {
		t.Fatalf("a held claim is retried, not dropped: %v", err)
	}
	// Once the claim is gone (lease over) the retry posts the answer.
	delete(f.repo.claimed, f.trig.conv)
	if err := f.svc.OnChannelRunEnd(context.Background(), f.trig.conv, 7); err != nil || len(f.repo.messages) != n+1 {
		t.Fatalf("the retry posts it: %v", err)
	}
}
