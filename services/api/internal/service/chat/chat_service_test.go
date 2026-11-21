package chatsvc

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	chatdom "github.com/Paca-AI/api/internal/domain/chat"
)

// fakeRepo implements only what the notification paths touch; any other call
// panics on the nil embedded interface, which is the point.
type effectCall struct {
	message  uuid.UUID
	mentions []uuid.UUID
}

type fakeRepo struct {
	chatdom.Repository
	msg        *chatdom.Message
	recipients []uuid.UUID
	effects    []effectCall
}

func (f *fakeRepo) FindChannel(_ context.Context, p, c, _ uuid.UUID) (*chatdom.Channel, error) {
	return &chatdom.Channel{ID: c, ProjectID: &p, Name: "x"}, nil
}
func (f *fakeRepo) CreateMessage(_ context.Context, m *chatdom.Message) error {
	m.ID, m.Seq = uuid.New(), 7
	f.msg = m
	return nil
}
func (f *fakeRepo) FindMessage(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) (*chatdom.Message, error) {
	return f.msg, nil
}
func (f *fakeRepo) ChannelRecipients(context.Context, uuid.UUID) ([]uuid.UUID, error) {
	return f.recipients, nil
}
func (f *fakeRepo) RecordMessageEffects(_ context.Context, m *chatdom.Message, ids []uuid.UUID) error {
	f.effects = append(f.effects, effectCall{message: m.ID, mentions: ids})
	return nil
}
func (f *fakeRepo) RemoveMember(context.Context, uuid.UUID, uuid.UUID) error { return nil }

type captureNotifier struct{ got []chatdom.Notification }

func (c *captureNotifier) Notify(_ context.Context, n chatdom.Notification) { c.got = append(c.got, n) }

func TestPostMessageAnnouncesSeqAndRecipientsNotContent(t *testing.T) {
	rcpt := []uuid.UUID{uuid.New(), uuid.New()}
	n := &captureNotifier{}
	svc := New(&fakeRepo{recipients: rcpt}).WithNotifier(n)
	p, ch, author := uuid.New(), uuid.New(), uuid.New()

	if _, err := svc.PostMessage(context.Background(), p, ch, author, []byte(`[{"type":"paragraph","content":[{"type":"text","text":"secret words"}]}]`), nil); err != nil {
		t.Fatal(err)
	}
	if len(n.got) != 1 {
		t.Fatalf("announcements = %d", len(n.got))
	}
	g := n.got[0]
	if g.Type != chatdom.EventMessageNew || g.ChannelID != ch || g.ProjectID != p || g.Seq != 7 || len(g.Recipients) != 2 {
		t.Errorf("announcement = %+v", g)
	}
}

func TestInvalidMessageAnnouncesNothing(t *testing.T) {
	n := &captureNotifier{}
	svc := New(&fakeRepo{}).WithNotifier(n)
	if _, err := svc.PostMessage(context.Background(), uuid.New(), uuid.New(), uuid.New(), []byte(`[]`), nil); !errors.Is(err, chatdom.ErrMessageEmpty) {
		t.Fatalf("err = %v", err)
	}
	if len(n.got) != 0 {
		t.Errorf("announced %v", n.got)
	}
}

func TestLeavingAnnouncesAccessChange(t *testing.T) {
	n := &captureNotifier{}
	svc := New(&fakeRepo{}).WithNotifier(n)
	if err := svc.LeaveChannel(context.Background(), uuid.New(), uuid.New(), uuid.New()); err != nil {
		t.Fatal(err)
	}
	if len(n.got) != 1 || n.got[0].Type != chatdom.EventAccessChanged || len(n.got[0].Recipients) != 0 {
		t.Errorf("announcements = %+v", n.got)
	}
}

type capturePub struct{ body []byte }

func (c *capturePub) Publish(_ context.Context, _ string, payload any) error {
	c.body, _ = json.Marshal(payload)
	return nil
}

func TestWireFormat(t *testing.T) {
	pub := &capturePub{}
	p, ch, u := uuid.New(), uuid.New(), uuid.New()
	nt := NewPubSubNotifier(pub)

	nt.Notify(context.Background(), chatdom.Notification{Type: chatdom.EventMessageNew, ProjectID: p, ChannelID: ch, Seq: 3, Recipients: []uuid.UUID{u}})
	var m map[string]any
	_ = json.Unmarshal(pub.body, &m)
	if m["type"] != chatdom.EventMessageNew || m["authz"] != nil {
		t.Errorf("message event = %s", pub.body)
	}
	if r, _ := m["recipient_ids"].([]any); len(r) != 1 || r[0] != u.String() {
		t.Errorf("recipients = %v", m["recipient_ids"])
	}
	if pl := m["payload"].(map[string]any); pl["channel_id"] != ch.String() || pl["seq"] != float64(3) || pl["recipient_ids"] != nil {
		t.Errorf("payload = %v", pl)
	}

	nt.Notify(context.Background(), chatdom.Notification{Type: chatdom.EventChannelChanged, ProjectID: p, ChannelID: ch})
	_ = json.Unmarshal(pub.body, &m)
	az, _ := m["authz"].(map[string]any)
	if az["action"] != "channels:read" || az["resource"] != "project/"+p.String()+"/channel/"+ch.String() {
		t.Errorf("channel event authz = %v", m["authz"])
	}
}

func TestMentionsAreRecordedAndShownAsText(t *testing.T) {
	a, b := uuid.New(), uuid.New()
	repo := &fakeRepo{}
	svc := New(repo)
	content := []byte(`[{"type":"paragraph","content":[
		{"type":"text","text":"ping "},
		{"type":"teamMention","props":{"id":"` + a.String() + `","name":"Mina"}},
		{"type":"teamMention","props":{"id":"` + a.String() + `","name":"Mina"}},
		{"type":"teamMention","props":{"id":"not-a-uuid","name":"Nobody"}},
		{"type":"teamMention","props":{"id":"` + b.String() + `","name":"Dev"}}]}]`)

	m, err := svc.PostMessage(context.Background(), uuid.New(), uuid.New(), uuid.New(), content, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(repo.effects) != 1 || len(repo.effects[0].mentions) != 2 || repo.effects[0].mentions[0] != a || repo.effects[0].mentions[1] != b {
		t.Errorf("mentions must be distinct valid account ids in order: %+v", repo.effects)
	}
	if repo.msg.ContentText != "ping  @Mina @Mina @Nobody @Dev" && !strings.Contains(repo.msg.ContentText, "@Mina") {
		t.Errorf("a mention is searchable text: %q", repo.msg.ContentText)
	}
	_ = m
}

func TestAMentionAloneIsAMessage(t *testing.T) {
	text, err := ContentText([]byte(`[{"type":"paragraph","content":[{"type":"teamMention","props":{"id":"x","name":"Mina"}}]}]`))
	if err != nil || text != "@Mina" {
		t.Errorf("text = %q err = %v", text, err)
	}
}

func TestContentText_References(t *testing.T) {
	text, err := ContentText([]byte(`[{"type":"paragraph","content":[{"type":"text","text":"see"},{"type":"taskReference","props":{"id":"x","projectId":"p","number":42}},{"type":"docReference","props":{"id":"d","projectId":"p"}}]}]`))
	if err != nil {
		t.Fatal(err)
	}
	if text != "see #42 #doc" {
		t.Errorf("references keep a label, never a title: %q", text)
	}
}
