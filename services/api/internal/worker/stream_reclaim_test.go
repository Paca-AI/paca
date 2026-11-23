package worker

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

const (
	testStream = "test.stream"
	testGroup  = "test.group"
)

// reclaimFixture returns a reclaimer (min idle 0, so everything pending is due)
// over a stream with one entry delivered to a consumer that never acked it —
// as a dead pod's consumer name would leave it.
func reclaimFixture(t *testing.T) (*streamReclaimer, *redis.Client, string) {
	t.Helper()
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	ctx := context.Background()

	if err := client.XGroupCreateMkStream(ctx, testStream, testGroup, "0").Err(); err != nil {
		t.Fatalf("create group: %v", err)
	}
	id, err := client.XAdd(ctx, &redis.XAddArgs{Stream: testStream, Values: map[string]any{"payload": "x"}}).Result()
	if err != nil {
		t.Fatalf("xadd: %v", err)
	}
	// Delivered to a consumer that then "died": pending, never acked.
	if err := client.XReadGroup(ctx, &redis.XReadGroupArgs{
		Group: testGroup, Consumer: "dead-pod", Streams: []string{testStream, ">"}, Count: 10,
	}).Err(); err != nil {
		t.Fatalf("deliver to dead consumer: %v", err)
	}

	r := newStreamReclaimer(client, discardLogger(), testStream, testGroup, "live-pod")
	r.minIdle = 0
	return r, client, id
}

func pendingCount(t *testing.T, client *redis.Client) int64 {
	t.Helper()
	p, err := client.XPending(context.Background(), testStream, testGroup).Result()
	if err != nil {
		t.Fatalf("xpending: %v", err)
	}
	return p.Count
}

func TestStreamReclaimer_RedeliversEntryOwnedByDeadConsumer(t *testing.T) {
	r, client, id := reclaimFixture(t)

	var handled []string
	r.run(context.Background(), func(m redis.XMessage) {
		handled = append(handled, m.ID)
		// A real handler acks on success.
		_ = client.XAck(context.Background(), testStream, testGroup, m.ID).Err()
	})

	if len(handled) != 1 || handled[0] != id {
		t.Fatalf("expected the dead consumer's entry %s to be redelivered once, got %v", id, handled)
	}
	if n := pendingCount(t, client); n != 0 {
		t.Fatalf("expected nothing pending after the handler acked, got %d", n)
	}
}

func TestStreamReclaimer_DeadLettersAfterMaxDeliveries(t *testing.T) {
	r, client, id := reclaimFixture(t)
	r.maxDeliveries = 3
	ctx := context.Background()

	handled := 0
	failing := func(redis.XMessage) { handled++ } // never acks

	// The fixture's own read was delivery 1; each run claims it again.
	for i := 0; i < 10; i++ {
		r.run(ctx, failing)
	}

	if handled != 2 { // deliveries 2 and 3; delivery 4 exceeds the cap
		t.Fatalf("expected 2 retries before dead-lettering, got %d", handled)
	}
	if n := pendingCount(t, client); n != 0 {
		t.Fatalf("expected the poison entry to be acked off the pending list, got %d pending", n)
	}
	dead, err := client.XRange(ctx, r.deadStream, "-", "+").Result()
	if err != nil || len(dead) != 1 {
		t.Fatalf("expected one dead-letter entry, got %v (err %v)", dead, err)
	}
	if got := dead[0].Values["dead_source_id"]; got != id {
		t.Fatalf("dead-letter entry should record the source id %s, got %v", id, got)
	}
	if got := dead[0].Values["payload"]; got != "x" {
		t.Fatalf("dead-letter entry should keep the original fields, got %v", dead[0].Values)
	}
}

func TestStreamReclaimer_SkipsEntriesNotIdleLongEnough(t *testing.T) {
	r, _, _ := reclaimFixture(t)
	r.minIdle = time.Hour

	called := false
	r.run(context.Background(), func(redis.XMessage) { called = true })
	if called {
		t.Fatal("an entry delivered moments ago must not be reclaimed yet")
	}
}

func TestStreamReclaimer_DueThrottles(t *testing.T) {
	r, _, _ := reclaimFixture(t)
	now := time.Now()
	if !r.due(now) {
		t.Fatal("first call should be due")
	}
	if r.due(now.Add(reclaimInterval / 2)) {
		t.Fatal("a call within the interval should not be due")
	}
	if !r.due(now.Add(reclaimInterval)) {
		t.Fatal("a call after the interval should be due")
	}
}
