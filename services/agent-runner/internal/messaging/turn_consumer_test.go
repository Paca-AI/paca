package messaging

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

func newTurnConsumerTestRedis(t *testing.T) (*miniredis.Miniredis, *redis.Client) {
	t.Helper()
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(mr.Close)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	return mr, client
}

func waitTurnHandled(t *testing.T, handled <-chan uuid.UUID, want uuid.UUID) {
	t.Helper()
	select {
	case got := <-handled:
		if got != want {
			t.Fatalf("handled turn = %s, want %s", got, want)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for turn handler")
	}
}

func TestTurnConsumerReadsRequestsPublishedBeforeGroupCreation(t *testing.T) {
	_, client := newTurnConsumerTestRedis(t)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	turnID := uuid.New()
	if err := client.XAdd(ctx, &redis.XAddArgs{Stream: StreamAgentTurnRequests, Values: map[string]any{"turn_id": turnID.String()}}).Err(); err != nil {
		t.Fatal(err)
	}
	handled := make(chan uuid.UUID, 1)
	consumer := NewTurnConsumer(client, 1, func(_ context.Context, id uuid.UUID) error {
		handled <- id
		return nil
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	go consumer.Run(ctx)
	waitTurnHandled(t, handled, turnID)
	cancel()
}

func TestTurnConsumerGroupCreationRecoversWhenValkeyIsUnavailableAtStartup(t *testing.T) {
	mr, client := newTurnConsumerTestRedis(t)
	mr.Close()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	ready := make(chan bool, 1)
	go func() {
		ready <- ensureStreamGroup(ctx, client, StreamAgentTurnRequests, turnConsumerGroup,
			slog.New(slog.NewTextHandler(io.Discard, nil)), "turn consumer")
	}()
	time.Sleep(150 * time.Millisecond)
	if err := mr.Restart(); err != nil {
		t.Fatal(err)
	}
	select {
	case ok := <-ready:
		if !ok {
			t.Fatal("group creation stopped instead of recovering")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("group creation did not recover after Valkey restart")
	}
}

func TestTurnConsumerReclaimsAndAcknowledgesAbandonedPendingRequest(t *testing.T) {
	_, client := newTurnConsumerTestRedis(t)
	ctx := context.Background()
	if err := client.XGroupCreateMkStream(ctx, StreamAgentTurnRequests, turnConsumerGroup, "0-0").Err(); err != nil {
		t.Fatal(err)
	}
	turnID := uuid.New()
	if err := client.XAdd(ctx, &redis.XAddArgs{Stream: StreamAgentTurnRequests, Values: map[string]any{"turn_id": turnID.String()}}).Err(); err != nil {
		t.Fatal(err)
	}
	streams, err := client.XReadGroup(ctx, &redis.XReadGroupArgs{
		Group: turnConsumerGroup, Consumer: "dead-runner",
		Streams: []string{StreamAgentTurnRequests, ">"}, Count: 1,
	}).Result()
	if err != nil || len(streams) != 1 || len(streams[0].Messages) != 1 {
		t.Fatalf("seed pending request: streams=%+v err=%v", streams, err)
	}
	handled := make(chan uuid.UUID, 1)
	consumer := NewTurnConsumer(client, 1, func(_ context.Context, id uuid.UUID) error {
		handled <- id
		return nil
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	consumer.reclaimPending(ctx, "0-0", 0)
	waitTurnHandled(t, handled, turnID)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		pending, err := client.XPending(ctx, StreamAgentTurnRequests, turnConsumerGroup).Result()
		if err == nil && pending.Count == 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("reclaimed turn request was not acknowledged")
}
