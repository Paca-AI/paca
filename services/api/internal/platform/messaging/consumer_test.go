package messaging

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
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

// TestConsumer_SelfHealsAfterConsumerGroupDisappears is a regression test
// for a NOGROUP error mid-loop: if the stream's consumer group vanishes out
// from under an already-running consumer (a Valkey restart without
// persistence, a FLUSHALL, or a manual XGROUP DESTROY), Run must recreate
// it rather than spin on the exact same failing read forever — before this
// fix, a consumer that reached this state never processed another message
// again for the rest of the process's life. Mirrors agent-runner's own
// internal/messaging.TestConsumer_SelfHealsAfterConsumerGroupDisappears,
// which covers the same fix in that service's sibling consumer.
func TestConsumer_SelfHealsAfterConsumerGroupDisappears(t *testing.T) {
	client, cleanup := newTestRedisClient(t)
	defer cleanup()

	const stream = "paca.test_stream"
	handled := make(chan struct{}, 1)
	consumer := NewConsumer(client, stream, func(_ context.Context, _ string, _ []byte) error {
		handled <- struct{}{}
		return nil
	}, testLogger())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go consumer.Run(ctx)

	waitForGroup(t, client, stream, consumerGroup)

	if err := client.XGroupDestroy(ctx, stream, consumerGroup).Err(); err != nil {
		t.Fatalf("XGroupDestroy: %v", err)
	}

	// Longer deadline than waitForGroup's: Run's read loop may currently be
	// blocked inside an XReadGroup call that started before the destroy
	// above (and so won't itself return NOGROUP) for up to blockDuration
	// before its *next* call notices the group is gone and self-heals.
	deadline := time.Now().Add(blockDuration + 3*time.Second)
	var recreated bool
	for time.Now().Before(deadline) {
		groups, err := client.XInfoGroups(ctx, stream).Result()
		if err == nil {
			for _, g := range groups {
				if g.Name == consumerGroup {
					recreated = true
				}
			}
		}
		if recreated {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !recreated {
		t.Fatalf("consumer group %q was not recreated after disappearing", consumerGroup)
	}

	if err := client.XAdd(ctx, &redis.XAddArgs{
		Stream: stream,
		Values: map[string]any{"type": "test.event", "payload": "{}"},
	}).Err(); err != nil {
		t.Fatalf("XAdd: %v", err)
	}

	waitOrFail(t, handled, "handler after self-heal")
}
