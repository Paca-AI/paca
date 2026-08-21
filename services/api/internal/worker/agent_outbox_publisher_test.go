package worker

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"

	agentdom "github.com/Paca-AI/api/internal/domain/agent"
	"github.com/Paca-AI/api/internal/events"
)

type fakeAgentOutboxRepo struct {
	claimed  []*agentdom.OutboxEvent
	marked   []uuid.UUID
	retried  []uuid.UUID
	dead     []bool
	audience *agentdom.OutboxAudience
}

func (f *fakeAgentOutboxRepo) ClaimOutbox(context.Context, string, int, time.Duration) ([]*agentdom.OutboxEvent, error) {
	claimed := f.claimed
	f.claimed = nil
	return claimed, nil
}
func (f *fakeAgentOutboxRepo) MarkOutboxPublished(_ context.Context, id, _ uuid.UUID, _ time.Time) error {
	f.marked = append(f.marked, id)
	return nil
}
func (f *fakeAgentOutboxRepo) RetryOutbox(_ context.Context, id, _ uuid.UUID, _ time.Time, _ string, dead bool) error {
	f.retried = append(f.retried, id)
	f.dead = append(f.dead, dead)
	return nil
}

func TestAgentOutboxPublisherDeadLettersInvalidCanonicalPayload(t *testing.T) {
	event := testOutbox("agent.turn.control.requested", "agent_turn", uuid.New(), map[string]any{
		"turn_id": uuid.New(),
	})
	repo, bus := &fakeAgentOutboxRepo{claimed: []*agentdom.OutboxEvent{event}}, &fakeAgentOutboxBus{}
	worker := NewAgentOutboxPublisher(repo, bus, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err := worker.publishBatch(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(repo.dead) != 1 || !repo.dead[0] {
		t.Fatalf("invalid outbox was not dead-lettered: %+v", repo.dead)
	}
	if len(repo.marked) != 0 {
		t.Fatal("invalid outbox was marked published")
	}
}
func (f *fakeAgentOutboxRepo) ResolveOutboxAudience(context.Context, *agentdom.OutboxEvent) (*agentdom.OutboxAudience, error) {
	return f.audience, nil
}

type fakeAgentOutboxBus struct {
	stream string
	fields map[string]any
	topic  string
	body   map[string]any
	err    error
}

func (f *fakeAgentOutboxBus) AppendFlat(_ context.Context, stream string, fields map[string]any) error {
	f.stream, f.fields = stream, fields
	return f.err
}
func (f *fakeAgentOutboxBus) Publish(_ context.Context, topic string, payload any) error {
	f.topic = topic
	f.body, _ = payload.(map[string]any)
	return f.err
}

func testOutbox(eventType, aggregateType string, aggregateID uuid.UUID, payload map[string]any) *agentdom.OutboxEvent {
	encoded, _ := json.Marshal(payload)
	token := uuid.New()
	return &agentdom.OutboxEvent{
		ID: uuid.New(), AggregateType: aggregateType, AggregateID: aggregateID,
		EventType: eventType, Payload: encoded, IdempotencyKey: "key",
		Attempts: 1, LockToken: &token,
	}
}

func TestAgentOutboxPublisherPublishesMinimalTurnRequest(t *testing.T) {
	turnID := uuid.New()
	event := testOutbox("agent.turn.requested", "agent_turn", turnID, map[string]any{"turn_id": turnID})
	repo, bus := &fakeAgentOutboxRepo{claimed: []*agentdom.OutboxEvent{event}}, &fakeAgentOutboxBus{}
	worker := NewAgentOutboxPublisher(repo, bus, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err := worker.publishBatch(context.Background()); err != nil {
		t.Fatal(err)
	}
	if bus.stream != events.StreamAgentTurnRequests || bus.fields["turn_id"] != turnID.String() {
		t.Fatalf("unexpected stream request: %s %#v", bus.stream, bus.fields)
	}
	if _, leaked := bus.fields["snapshot"]; leaked {
		t.Fatal("private snapshot leaked into turn stream")
	}
	if len(repo.marked) != 1 || repo.marked[0] != event.ID {
		t.Fatalf("published event was not marked: %#v", repo.marked)
	}
}

func TestAgentOutboxPublisherNeverDeadLettersTransientTransportFailure(t *testing.T) {
	turnID := uuid.New()
	event := testOutbox("agent.turn.requested", "agent_turn", turnID, map[string]any{"turn_id": turnID})
	event.Attempts = 100
	repo := &fakeAgentOutboxRepo{claimed: []*agentdom.OutboxEvent{event}}
	worker := NewAgentOutboxPublisher(repo, &fakeAgentOutboxBus{err: errors.New("valkey unavailable")},
		slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err := worker.publishBatch(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(repo.dead) != 1 || repo.dead[0] {
		t.Fatalf("transient event was dead-lettered after many attempts: %+v", repo.dead)
	}
	if len(repo.retried) != 1 || repo.retried[0] != event.ID {
		t.Fatalf("transient event was not rescheduled: %+v", repo.retried)
	}
}

func TestAgentOutboxPublisherPublishesFencedTurnControl(t *testing.T) {
	turnID, runID, conversationID := uuid.New(), uuid.New(), uuid.New()
	agentID, claimToken := uuid.New(), uuid.New()
	event := testOutbox("agent.turn.control.requested", "agent_turn", turnID, map[string]any{
		"turn_id": turnID, "run_id": runID, "conversation_id": conversationID,
		"agent_id": agentID, "backend": "acp", "claim_token": claimToken,
		"attempt": 2, "reason": "stopped_by_user",
	})
	repo, bus := &fakeAgentOutboxRepo{claimed: []*agentdom.OutboxEvent{event}}, &fakeAgentOutboxBus{}
	worker := NewAgentOutboxPublisher(repo, bus, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err := worker.publishBatch(context.Background()); err != nil {
		t.Fatal(err)
	}
	if bus.stream != events.StreamAgentTurnControls || bus.fields["run_id"] != runID.String() ||
		bus.fields["claim_token"] != claimToken.String() || bus.fields["attempt"] != 2 {
		t.Fatalf("unexpected turn control: %s %#v", bus.stream, bus.fields)
	}
	if _, leaked := bus.fields["input_text"]; leaked {
		t.Fatal("private input leaked into turn control stream")
	}
}

func TestAgentOutboxPublisherRoutesTurnFinishedOnlyToOwner(t *testing.T) {
	turnID, projectID, userID, sessionID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	event := testOutbox("agent.turn.finished", "agent_turn", turnID, map[string]any{"turn_id": turnID, "status": "succeeded"})
	repo := &fakeAgentOutboxRepo{claimed: []*agentdom.OutboxEvent{event}, audience: &agentdom.OutboxAudience{
		ProjectID: projectID, ActorUserID: &userID, SessionID: &sessionID, TurnID: &turnID,
	}}
	bus := &fakeAgentOutboxBus{}
	worker := NewAgentOutboxPublisher(repo, bus, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err := worker.publishBatch(context.Background()); err != nil {
		t.Fatal(err)
	}
	if bus.topic != events.ChannelRealtime {
		t.Fatalf("unexpected realtime topic %q", bus.topic)
	}
	payload, _ := bus.body["payload"].(map[string]any)
	if payload["actor_user_id"] != userID.String() || payload["project_id"] != projectID.String() {
		t.Fatalf("owner routing missing: %#v", payload)
	}
	if _, leaked := payload["stable_output"]; leaked {
		t.Fatal("stable output leaked into realtime routing event")
	}
}

type fakeDueTurnRepo struct {
	counts []int
	calls  int
}

func (f *fakeDueTurnRepo) ExpireDueTurns(context.Context, int) (int, error) {
	value := f.counts[f.calls]
	f.calls++
	return value, nil
}

func TestAgentTurnExpirySchedulerDrainsFullBatches(t *testing.T) {
	repo := &fakeDueTurnRepo{counts: []int{agentTurnExpiryBatch, agentTurnExpiryBatch, 2}}
	scheduler := NewAgentTurnExpiryScheduler(repo, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err := scheduler.expireAll(context.Background()); err != nil {
		t.Fatal(err)
	}
	if repo.calls != 3 {
		t.Fatalf("expire calls = %d, want 3", repo.calls)
	}
}
