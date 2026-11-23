package worker

import (
	"context"
	"errors"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	agentdom "github.com/Paca-AI/api/internal/domain/agent"
	notificationdom "github.com/Paca-AI/api/internal/domain/notification"
	"github.com/Paca-AI/api/internal/events"
)

func TestIsAskQuestionResult(t *testing.T) {
	result := `{"content":[{"type":"content","content":{"text":"Question shown to the user. Stop now and wait.\n\n` + "```paca-ask" + `\n{}\n` + "```" + `","type":"text"}}]}`
	cases := []struct {
		name, eventType, payload string
		want                     bool
	}{
		{"ask result", "tool_call_update", result, true},
		{"wrong event type", "tool_call", result, false},
		{"other tool result", "tool_call_update", `{"content":"ok"}`, false},
		{"prefix without block", "tool_call_update", "Question shown to the user", false},
	}
	for _, tc := range cases {
		if got := isAskQuestionResult(tc.eventType, tc.payload); got != tc.want {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}
}

type fakeQuestionConvs struct{ conv *agentdom.AgentConversation }

func (f fakeQuestionConvs) FindConversationByID(context.Context, uuid.UUID) (*agentdom.AgentConversation, error) {
	return f.conv, nil
}

// failingQuestionSvc embeds the interface so only NotifyQuestion need exist.
type failingQuestionSvc struct {
	notificationdom.Service
	err   error
	calls int
}

func (s *failingQuestionSvc) NotifyQuestion(context.Context, notificationdom.NotifyQuestionInput) error {
	s.calls++
	return s.err
}

func questionFx(t *testing.T, svc *failingQuestionSvc) (*QuestionNotificationConsumer, *redis.Client, redis.XMessage) {
	t.Helper()
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	ctx := context.Background()
	if err := client.XGroupCreateMkStream(ctx, events.StreamAgentEvents, questionConsumerGroup, "0").Err(); err != nil {
		t.Fatal(err)
	}
	convID, owner := uuid.New(), uuid.New()
	if err := client.XAdd(ctx, &redis.XAddArgs{Stream: events.StreamAgentEvents, Values: map[string]any{
		"event_type":      "tool_call_update",
		"payload":         askResultPrefix + " " + askResultFence,
		"conversation_id": convID.String(),
	}}).Err(); err != nil {
		t.Fatal(err)
	}
	res, err := client.XReadGroup(ctx, &redis.XReadGroupArgs{
		Group: questionConsumerGroup, Consumer: "c", Streams: []string{events.StreamAgentEvents, ">"}, Count: 1,
	}).Result()
	if err != nil || len(res) == 0 {
		t.Fatalf("read: %v %v", res, err)
	}
	c := NewQuestionNotificationConsumer(client, fakeQuestionConvs{conv: &agentdom.AgentConversation{ID: convID, ActorAccountID: &owner}}, nil, svc, discardLogger())
	return c, client, res[0].Messages[0]
}

func pendingQuestions(t *testing.T, client *redis.Client) int64 {
	t.Helper()
	p, err := client.XPending(context.Background(), events.StreamAgentEvents, questionConsumerGroup).Result()
	if err != nil {
		t.Fatal(err)
	}
	return p.Count
}

func TestQuestionConsumer_ANotificationThatCouldNotBeCreatedIsNotAcked(t *testing.T) {
	svc := &failingQuestionSvc{err: errors.New("db down")}
	c, client, msg := questionFx(t, svc)
	c.handle(msg)
	if svc.calls != 1 || pendingQuestions(t, client) != 1 {
		t.Fatalf("calls=%d pending=%d; a failed create must stay pending so the reclaimer retries it", svc.calls, pendingQuestions(t, client))
	}
}

func TestQuestionConsumer_ASuccessfulNotificationIsAcked(t *testing.T) {
	svc := &failingQuestionSvc{}
	c, client, msg := questionFx(t, svc)
	c.handle(msg)
	if svc.calls != 1 || pendingQuestions(t, client) != 0 {
		t.Fatalf("calls=%d pending=%d; a created notification must be acked", svc.calls, pendingQuestions(t, client))
	}
}
