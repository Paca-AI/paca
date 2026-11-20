package chatsvc

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"

	chatdom "github.com/Paca-AI/api/internal/domain/chat"
)

// agentRepo is a small in-memory stand-in for the agent-DM parts of the repository.
type agentRepo struct {
	chatdom.Repository
	channel  *chatdom.Channel
	link     *chatdom.AgentLink
	global   bool
	messages []*chatdom.Message // what the person wrote (top level, in order)
	posted   []*chatdom.Message // what the agent wrote
	reply    string
}

func (r *agentRepo) FindChannel(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) (*chatdom.Channel, error) {
	return r.channel, nil
}
func (r *agentRepo) CreateMessage(_ context.Context, m *chatdom.Message) error {
	m.ID = uuid.New()
	if m.AuthorID == r.link.AgentID {
		m.Seq = int64(100 + len(r.posted))
		r.posted = append(r.posted, m)
		return nil
	}
	m.Seq = int64(len(r.messages) + 1)
	r.messages = append(r.messages, m)
	return nil
}
func (r *agentRepo) FindMessage(_ context.Context, _, id, _ uuid.UUID) (*chatdom.Message, error) {
	for _, m := range append(append([]*chatdom.Message{}, r.messages...), r.posted...) {
		if m.ID == id {
			return m, nil
		}
	}
	return nil, chatdom.ErrMessageNotFound
}
func (r *agentRepo) ChannelRecipients(context.Context, uuid.UUID) ([]uuid.UUID, error) {
	return nil, nil
}
func (r *agentRepo) RecordMessageEffects(context.Context, *chatdom.Message, []uuid.UUID) error {
	return nil
}
func (r *agentRepo) IsGlobalAgent(context.Context, uuid.UUID) (bool, error) { return r.global, nil }
func (r *agentRepo) EnsureAgentLink(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) (*chatdom.AgentLink, error) {
	return r.link, nil
}
func (r *agentRepo) FindAgentLinkBySession(_ context.Context, s uuid.UUID) (*chatdom.AgentLink, error) {
	if r.link.SessionID != nil && *r.link.SessionID == s {
		return r.link, nil
	}
	return nil, nil
}
func (r *agentRepo) MessagesToForward(_ context.Context, _, _ uuid.UUID, after int64) ([]*chatdom.Message, error) {
	var out []*chatdom.Message
	for _, m := range r.messages {
		if m.Seq > after {
			out = append(out, m)
		}
	}
	return out, nil
}
func (r *agentRepo) ClaimForward(_ context.Context, _ uuid.UUID, from, to int64) (bool, error) {
	if r.link.ForwardedSeq != from {
		return false, nil
	}
	r.link.ForwardedSeq = to
	return true, nil
}
func (r *agentRepo) ReleaseForward(_ context.Context, _ uuid.UUID, from, to int64) error {
	if r.link.ForwardedSeq == to {
		r.link.ForwardedSeq = from
	}
	return nil
}
func (r *agentRepo) SetAgentRun(_ context.Context, _, s, c uuid.UUID) error {
	r.link.SessionID, r.link.ConversationID = &s, &c
	return nil
}
func (r *agentRepo) SetBridgedIndex(_ context.Context, _ uuid.UUID, i int) error {
	r.link.BridgedIndex = i
	return nil
}
func (r *agentRepo) AgentReply(context.Context, uuid.UUID, int, int) (string, error) {
	return r.reply, nil
}

type fakeResponder struct {
	sent    []string
	busy    bool
	session uuid.UUID
	conv    uuid.UUID
}

func (f *fakeResponder) Send(_ context.Context, _, _ uuid.UUID, _ *uuid.UUID, text string) (uuid.UUID, uuid.UUID, error) {
	if f.busy {
		return uuid.Nil, uuid.Nil, chatdom.ErrAgentBusy
	}
	f.sent = append(f.sent, text)
	return f.session, f.conv, nil
}

func setupAgentDM(global bool) (*Service, *agentRepo, *fakeResponder, uuid.UUID) {
	person, agent, ch := uuid.New(), uuid.New(), uuid.New()
	repo := &agentRepo{
		global:  global,
		channel: &chatdom.Channel{ID: ch, Kind: chatdom.KindDM, Peer: &chatdom.DMPeer{AccountID: agent, Name: "Atlas", Kind: chatdom.AccountAgent}},
		link:    &chatdom.AgentLink{ChannelID: ch, AgentID: agent, AccountID: person, BridgedIndex: -1},
	}
	resp := &fakeResponder{session: uuid.New(), conv: uuid.New()}
	return New(repo).WithAgentResponder(resp), repo, resp, person
}

const hello = `[{"type":"paragraph","content":[{"type":"text","text":"hello Atlas"}]}]`

func TestMessageToAGlobalAgentIsForwardedOnce(t *testing.T) {
	svc, repo, resp, person := setupAgentDM(true)
	if _, err := svc.PostMessage(context.Background(), uuid.Nil, repo.channel.ID, person, []byte(hello), nil); err != nil {
		t.Fatal(err)
	}
	if len(resp.sent) != 1 || resp.sent[0] != "hello Atlas" {
		t.Fatalf("sent = %v", resp.sent)
	}
	if repo.link.ForwardedSeq != 1 || repo.link.SessionID == nil || *repo.link.ConversationID != resp.conv {
		t.Fatalf("link not advanced: %+v", repo.link)
	}
}

func TestMessageToOtherAccountsIsNotForwarded(t *testing.T) {
	svc, repo, resp, person := setupAgentDM(false) // a project agent
	if _, err := svc.PostMessage(context.Background(), uuid.Nil, repo.channel.ID, person, []byte(hello), nil); err != nil {
		t.Fatal(err)
	}
	if len(resp.sent) != 0 {
		t.Fatalf("a project agent was sent %v", resp.sent)
	}
	repo.channel.Peer.Kind = chatdom.AccountUser
	repo.global = true
	if _, err := svc.PostMessage(context.Background(), uuid.Nil, repo.channel.ID, person, []byte(hello), nil); err != nil {
		t.Fatal(err)
	}
	if len(resp.sent) != 0 {
		t.Fatalf("a person was sent %v", resp.sent)
	}
}

func TestBusyAgentGetsTheMessagesAfterItsTurn(t *testing.T) {
	svc, repo, resp, person := setupAgentDM(true)
	resp.busy = true
	ctx := context.Background()
	for _, txt := range []string{"one", "two"} {
		body := `[{"type":"paragraph","content":[{"type":"text","text":"` + txt + `"}]}]`
		if _, err := svc.PostMessage(ctx, uuid.Nil, repo.channel.ID, person, []byte(body), nil); err != nil {
			t.Fatal(err)
		}
	}
	if len(resp.sent) != 0 || repo.link.ForwardedSeq != 0 {
		t.Fatalf("busy agent was sent %v (forwarded %d)", resp.sent, repo.link.ForwardedSeq)
	}
	// The agent finishes its turn: it answers, then gets what was written meanwhile.
	resp.busy = false
	session, conv := uuid.New(), uuid.New()
	repo.link.SessionID, repo.link.ConversationID = &session, &conv
	repo.reply = "Done with the first part.\n\nSecond paragraph."
	if err := svc.OnAgentTurnEnd(ctx, session, conv, 4); err != nil {
		t.Fatal(err)
	}
	if len(repo.posted) != 1 || repo.posted[0].ContentText != repo.reply || repo.posted[0].RunID == nil || *repo.posted[0].RunID != conv {
		t.Fatalf("reply not posted as the agent: %+v", repo.posted)
	}
	if !strings.Contains(string(repo.posted[0].Content), "Second paragraph.") || strings.Count(string(repo.posted[0].Content), `"paragraph"`) != 2 {
		t.Fatalf("reply is not two paragraph blocks: %s", repo.posted[0].Content)
	}
	if repo.link.BridgedIndex != 4 {
		t.Fatalf("bridged index = %d", repo.link.BridgedIndex)
	}
	if len(resp.sent) != 1 || resp.sent[0] != "one\n\ntwo" {
		t.Fatalf("pending messages sent = %v", resp.sent)
	}
}

func TestTurnOfAnotherRunIsIgnored(t *testing.T) {
	svc, repo, _, _ := setupAgentDM(true)
	session, conv := uuid.New(), uuid.New()
	repo.link.SessionID, repo.link.ConversationID = &session, &conv
	repo.reply = "stray"
	if err := svc.OnAgentTurnEnd(context.Background(), session, uuid.New(), 1); err != nil {
		t.Fatal(err)
	}
	if len(repo.posted) != 0 {
		t.Fatalf("posted %v", repo.posted)
	}
}

func TestRepeatedTurnEndPostsNothing(t *testing.T) {
	svc, repo, _, _ := setupAgentDM(true)
	session, conv := uuid.New(), uuid.New()
	repo.link.SessionID, repo.link.ConversationID = &session, &conv
	repo.reply = "Hello."
	ctx := context.Background()
	if err := svc.OnAgentTurnEnd(ctx, session, conv, 5); err != nil {
		t.Fatal(err)
	}
	repo.reply = ""
	// The same turn delivered again, and an older one: neither says anything.
	for _, idx := range []int{5, 3} {
		if err := svc.OnAgentTurnEnd(ctx, session, conv, idx); err != nil {
			t.Fatal(err)
		}
	}
	if len(repo.posted) != 1 {
		t.Fatalf("posted %d messages, want 1", len(repo.posted))
	}
	// A later turn with only tool calls still answers, in a line.
	if err := svc.OnAgentTurnEnd(ctx, session, conv, 9); err != nil {
		t.Fatal(err)
	}
	if len(repo.posted) != 2 || !strings.Contains(repo.posted[1].ContentText, "nothing to add") {
		t.Fatalf("posted: %+v", repo.posted)
	}
}
