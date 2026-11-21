package chatsvc

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
)

type fakeEmitter struct {
	topics   []string
	payloads []map[string]any
}

func (f *fakeEmitter) EmitAutomationEvent(_ context.Context, topic string, p map[string]any) error {
	f.topics = append(f.topics, topic)
	f.payloads = append(f.payloads, p)
	return nil
}

func (r *channelRepo) AgentAccountOfMember(_ context.Context, _, member uuid.UUID) (uuid.UUID, bool, error) {
	for acct, m := range r.members {
		if m == member && r.kinds[acct] == "agent" {
			return acct, true, nil
		}
	}
	return uuid.Nil, false, nil
}

func TestAutomation_TriggersFromPeopleOnly(t *testing.T) {
	f := setupChannel("public")
	em := &fakeEmitter{}
	f.svc.WithAutomationEmitter(em)
	ctx := context.Background()
	if _, err := f.svc.PostMessage(ctx, f.project, f.ch, f.person, []byte(`[{"type":"paragraph","content":[{"type":"text","text":"hello"}]}]`), nil); err != nil {
		t.Fatal(err)
	}
	if len(em.topics) != 1 || em.topics[0] != TriggerMessagePosted {
		t.Fatalf("a plain message raises message_posted only: %v", em.topics)
	}
	em.topics, em.payloads = nil, nil
	if _, err := f.svc.PostMessage(ctx, f.project, f.ch, f.person, mentionOf(f.agent, "Atlas"), nil); err != nil {
		t.Fatal(err)
	}
	if len(em.topics) != 2 || em.topics[1] != TriggerMention || em.payloads[1]["mentioned"] != "Atlas" {
		t.Fatalf("a mention also raises chat.mention with names: %v %v", em.topics, em.payloads)
	}
	em.topics, em.payloads = nil, nil
	if _, err := f.svc.PostMessage(ctx, f.project, f.ch, f.agent, []byte(`[{"type":"paragraph","content":[{"type":"text","text":"bot"}]}]`), nil); err != nil {
		t.Fatal(err)
	}
	if len(em.topics) != 0 {
		t.Errorf("an agent's message never fires a trigger (no loops): %v", em.topics)
	}
}

func TestAutomation_PrivateChannelsStayOut(t *testing.T) {
	f := setupChannel("private")
	em := &fakeEmitter{}
	f.svc.WithAutomationEmitter(em)
	_, _ = f.svc.PostMessage(context.Background(), f.project, f.ch, f.person, []byte(`[{"type":"paragraph","content":[{"type":"text","text":"x"}]}]`), nil)
	if len(em.topics) != 0 {
		t.Errorf("private channel text is not handed to automations: %v", em.topics)
	}
}

func TestAutomation_SendMessageAsAgent(t *testing.T) {
	f := setupChannel("public")
	agentMember := f.repo.members[f.agent]
	run := func(cfg map[string]any) map[string]any {
		b, _ := json.Marshal(cfg)
		raw, _ := json.Marshal(map[string]any{"node_type": ActionSendMessage, "project_id": f.project, "config": json.RawMessage(b)})
		out, err := f.svc.RunAutomationAction(context.Background(), raw)
		if err != nil {
			t.Fatal(err)
		}
		var res map[string]any
		_ = json.Unmarshal(out, &res)
		return res
	}
	res := run(map[string]any{"member_id": agentMember.String(), "channel_id": f.ch.String(), "text": "build is green"})
	if res["applied"] != true {
		t.Fatalf("applied: %v", res)
	}
	last := f.repo.messages[len(f.repo.messages)-1]
	if last.AuthorID != f.agent || last.ContentText != "build is green" {
		t.Errorf("posted as the agent: %+v", last)
	}
	// a person's member id is not an agent: refused, nothing posted
	n := len(f.repo.messages)
	res = run(map[string]any{"member_id": f.personMemberID.String(), "channel_id": f.ch.String(), "text": "x"})
	if res["applied"] != false || res["error"] == nil || len(f.repo.messages) != n {
		t.Errorf("only an agent member can post: %v", res)
	}
	res = run(map[string]any{"member_id": agentMember.String(), "channel_id": "{{event.channel_id}}", "text": "x"})
	if res["applied"] != false {
		t.Errorf("an unrendered channel id is refused: %v", res)
	}
}
