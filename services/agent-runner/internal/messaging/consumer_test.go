package messaging

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"github.com/Paca-AI/agent-runner/internal/agent"
)

func newTestRedisClient(t *testing.T) (*redis.Client, func()) {
	t.Helper()
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis.Run: %v", err)
	}
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	return client, func() {
		client.Close()
		mr.Close()
	}
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// TestConsumer_RoutesControlAndTriggerMessagesSeparately confirms the
// actual "type"-field routing a real stream entry goes through —
// test/e2e/stop_control_test.go verifies HandleControl's *effect* end to
// end, but calls it directly; this is the missing piece, that a genuine
// agent.stop-shaped stream message (no agent_id, no trigger_type, exactly
// what Service.StopConversation actually publishes — see
// agent_service.go) gets identified as a control message at all, rather
// than being handed to decodeTrigger and rejected as malformed.
func TestConsumer_RoutesControlAndTriggerMessagesSeparately(t *testing.T) {
	client, cleanup := newTestRedisClient(t)
	defer cleanup()

	var mu sync.Mutex
	var gotTriggers []agent.Trigger
	var gotControls []Control

	triggerHandled := make(chan struct{}, 1)
	controlHandled := make(chan struct{}, 1)

	consumer := NewConsumer(client, 5,
		func(_ context.Context, trg agent.Trigger) error {
			mu.Lock()
			gotTriggers = append(gotTriggers, trg)
			mu.Unlock()
			triggerHandled <- struct{}{}
			return nil
		},
		func(_ context.Context, c Control) error {
			mu.Lock()
			gotControls = append(gotControls, c)
			mu.Unlock()
			controlHandled <- struct{}{}
			return nil
		},
		testLogger(),
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go consumer.Run(ctx)

	// Wait for the consumer group to actually exist before publishing —
	// otherwise XAdd could land before XGroupCreateMkStream runs.
	waitForGroup(t, client, StreamAgentTriggers, consumerGroup)

	agentID := uuid.New()
	convID := uuid.New()
	projectID := uuid.New()

	// Exactly Service.publishTrigger's shape for a task_assigned trigger.
	if err := client.XAdd(ctx, &redis.XAddArgs{
		Stream: StreamAgentTriggers,
		Values: map[string]any{
			"type":            "agent.task_assigned",
			"trigger_type":    "task_assigned",
			"conversation_id": convID.String(),
			"agent_id":        agentID.String(),
			"project_id":      projectID.String(),
			"message":         "",
		},
	}).Err(); err != nil {
		t.Fatalf("XAdd trigger: %v", err)
	}

	// Exactly Service.StopConversation's shape — no agent_id, no
	// trigger_type, which is why this can't be decoded as a Trigger.
	stopConvID := uuid.New()
	if err := client.XAdd(ctx, &redis.XAddArgs{
		Stream: StreamAgentTriggers,
		Values: map[string]any{
			"type":            "agent.stop",
			"conversation_id": stopConvID.String(),
			"project_id":      projectID.String(),
		},
	}).Err(); err != nil {
		t.Fatalf("XAdd control: %v", err)
	}

	waitOrFail(t, triggerHandled, "trigger handler")
	waitOrFail(t, controlHandled, "control handler")

	mu.Lock()
	defer mu.Unlock()
	if len(gotTriggers) != 1 || gotTriggers[0].ConversationID != convID {
		t.Errorf("gotTriggers = %+v, want exactly one for conversation %s", gotTriggers, convID)
	}
	if len(gotControls) != 1 || gotControls[0].ConversationID != stopConvID || gotControls[0].Type != ControlStop {
		t.Errorf("gotControls = %+v, want exactly one ControlStop for conversation %s", gotControls, stopConvID)
	}
}

// TestConsumer_SerializesTriggersForTheSameConversation is a regression test
// for the event_index/in-flight-registry race: two triggers for the same
// conversation_id, published back to back (e.g. a user sending two chat
// messages in quick succession), must never have their handler calls
// overlap in time — see conversationLocks' doc comment for why.
func TestConsumer_SerializesTriggersForTheSameConversation(t *testing.T) {
	client, cleanup := newTestRedisClient(t)
	defer cleanup()

	var mu sync.Mutex
	inHandler := 0
	maxConcurrent := 0
	handled := make(chan struct{}, 2)

	consumer := NewConsumer(client, 5,
		func(_ context.Context, _ agent.Trigger) error {
			mu.Lock()
			inHandler++
			if inHandler > maxConcurrent {
				maxConcurrent = inHandler
			}
			mu.Unlock()

			// Give a concurrent second call a real chance to interleave if
			// the lock weren't actually serializing — without this sleep, a
			// buggy implementation could still pass by sheer luck.
			time.Sleep(50 * time.Millisecond)

			mu.Lock()
			inHandler--
			mu.Unlock()
			handled <- struct{}{}
			return nil
		},
		func(_ context.Context, _ Control) error { return nil },
		testLogger(),
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go consumer.Run(ctx)

	waitForGroup(t, client, StreamAgentTriggers, consumerGroup)

	agentID := uuid.New()
	convID := uuid.New()
	projectID := uuid.New()

	for range 2 {
		if err := client.XAdd(ctx, &redis.XAddArgs{
			Stream: StreamAgentTriggers,
			Values: map[string]any{
				"type":            "agent.chat_message",
				"trigger_type":    "chat_message",
				"conversation_id": convID.String(),
				"agent_id":        agentID.String(),
				"project_id":      projectID.String(),
				"message":         "hi",
			},
		}).Err(); err != nil {
			t.Fatalf("XAdd trigger: %v", err)
		}
	}

	waitOrFail(t, handled, "first handler call")
	waitOrFail(t, handled, "second handler call")

	mu.Lock()
	defer mu.Unlock()
	if maxConcurrent > 1 {
		t.Errorf("handler ran concurrently for the same conversation_id (max concurrent = %d), want serialized", maxConcurrent)
	}
}

func waitForGroup(t *testing.T, client *redis.Client, stream, group string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		groups, err := client.XInfoGroups(context.Background(), stream).Result()
		if err == nil {
			for _, g := range groups {
				if g.Name == group {
					return
				}
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("consumer group %q on stream %q never appeared", group, stream)
}

func waitOrFail(t *testing.T, ch <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(3 * time.Second):
		t.Fatalf("timed out waiting for %s to run", what)
	}
}
