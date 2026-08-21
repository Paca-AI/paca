package messaging

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

func TestTurnControlConsumerReadsDurableControlPublishedBeforeStartup(t *testing.T) {
	_, client := newTurnConsumerTestRedis(t)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	control := TurnControl{TurnID: uuid.New(), RunID: uuid.New(), ConversationID: uuid.New(),
		AgentID: uuid.New(), ClaimToken: uuid.New(), Attempt: 2, Backend: "acp", Reason: "stopped_by_user"}
	if err := client.XAdd(ctx, &redis.XAddArgs{Stream: StreamAgentTurnControls, Values: map[string]any{
		"turn_id": control.TurnID.String(), "run_id": control.RunID.String(), "conversation_id": control.ConversationID.String(),
		"agent_id": control.AgentID.String(), "claim_token": control.ClaimToken.String(), "attempt": control.Attempt,
		"backend": control.Backend, "reason": control.Reason,
	}}).Err(); err != nil {
		t.Fatal(err)
	}
	handled := make(chan TurnControl, 1)
	consumer := NewTurnControlConsumer(client, func(_ context.Context, got TurnControl) error {
		handled <- got
		return nil
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	go consumer.Run(ctx)
	select {
	case got := <-handled:
		if got != control {
			t.Fatalf("control = %+v, want %+v", got, control)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for durable turn control")
	}
	cancel()
}

func TestTurnControlConsumerReclaimCursorDoesNotStarveLaterPendingControls(t *testing.T) {
	_, client := newTurnConsumerTestRedis(t)
	ctx := context.Background()
	if err := client.XGroupCreateMkStream(ctx, StreamAgentTurnControls, turnControlGroup, "0-0").Err(); err != nil {
		t.Fatal(err)
	}
	for attempt := 1; attempt <= 25; attempt++ {
		if err := client.XAdd(ctx, &redis.XAddArgs{Stream: StreamAgentTurnControls, Values: map[string]any{
			"turn_id": uuid.NewString(), "run_id": uuid.NewString(), "conversation_id": uuid.NewString(),
			"agent_id": uuid.NewString(), "claim_token": uuid.NewString(), "attempt": attempt,
			"backend": "llm", "reason": "stopped_by_user",
		}}).Err(); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := client.XReadGroup(ctx, &redis.XReadGroupArgs{
		Group: turnControlGroup, Consumer: "dead-control-runner",
		Streams: []string{StreamAgentTurnControls, ">"}, Count: 25,
	}).Result(); err != nil {
		t.Fatal(err)
	}
	handledLater := 0
	consumer := NewTurnControlConsumer(client, func(_ context.Context, control TurnControl) error {
		if control.Attempt <= 20 {
			return errors.New("keep oldest controls pending")
		}
		handledLater++
		return nil
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	next := consumer.reclaimPending(ctx, "0-0", 0)
	if next == "0" || next == "0-0" {
		t.Fatal("first reclaim batch discarded the server cursor")
	}
	consumer.reclaimPending(ctx, next, 0)
	if handledLater != 5 {
		t.Fatalf("later pending controls handled = %d, want 5", handledLater)
	}
}
