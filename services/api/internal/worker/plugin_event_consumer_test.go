package worker

import (
	"context"
	"errors"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"

	plugindom "github.com/Paca-AI/api/internal/domain/plugin"
	"github.com/Paca-AI/api/internal/events"
)

// scriptedEmitter answers each DeliverEvent from a queue and records which
// plugins it was asked to run.
type scriptedEmitter struct {
	replies [][]plugindom.EventDelivery
	only    [][]string
}

func (e *scriptedEmitter) DeliverEvent(_ context.Context, _ string, _ any, only []string) []plugindom.EventDelivery {
	e.only = append(e.only, only)
	r := e.replies[0]
	e.replies = e.replies[1:]
	return r
}

type pluginEventFx struct {
	c      *PluginEventConsumer
	client *redis.Client
	em     *scriptedEmitter
}

func newPluginEventFx(t *testing.T, replies ...[]plugindom.EventDelivery) *pluginEventFx {
	t.Helper()
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	if err := client.XGroupCreateMkStream(context.Background(), events.StreamPluginEvents, pluginEventConsumerGroup, "0").Err(); err != nil {
		t.Fatal(err)
	}
	em := &scriptedEmitter{replies: replies}
	return &pluginEventFx{c: NewPluginEventConsumer(client, em, discardLogger()), client: client, em: em}
}

// deliverNext appends an entry (or re-reads the pending one) and hands it to handle.
func (f *pluginEventFx) read(t *testing.T, start string) redis.XMessage {
	t.Helper()
	res, err := f.client.XReadGroup(context.Background(), &redis.XReadGroupArgs{
		Group: pluginEventConsumerGroup, Consumer: f.c.consumerName,
		Streams: []string{events.StreamPluginEvents, start}, Count: 1,
	}).Result()
	if err != nil || len(res) == 0 || len(res[0].Messages) == 0 {
		t.Fatalf("read %s: %v %v", start, res, err)
	}
	return res[0].Messages[0]
}

func (f *pluginEventFx) add(t *testing.T) {
	t.Helper()
	if err := f.client.XAdd(context.Background(), &redis.XAddArgs{
		Stream: events.StreamPluginEvents, Values: map[string]any{"type": "notification.assigned", "payload": `{"x":1}`},
	}).Err(); err != nil {
		t.Fatal(err)
	}
}

func (f *pluginEventFx) pending(t *testing.T) int64 {
	t.Helper()
	p, err := f.client.XPending(context.Background(), events.StreamPluginEvents, pluginEventConsumerGroup).Result()
	if err != nil {
		t.Fatal(err)
	}
	return p.Count
}

func (f *pluginEventFx) dead(t *testing.T) []redis.XMessage {
	t.Helper()
	m, err := f.client.XRange(context.Background(), events.StreamPluginEvents+".dead", "-", "+").Result()
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestPluginEventConsumer_AcksWhenEveryPluginHandledTheEvent(t *testing.T) {
	f := newPluginEventFx(t, []plugindom.EventDelivery{{Plugin: "com.paca.smtp"}, {Plugin: "com.paca.webhook"}})
	f.add(t)
	f.c.handle(f.read(t, ">"))

	if n := f.pending(t); n != 0 {
		t.Fatalf("expected the entry acked, %d pending", n)
	}
	if len(f.dead(t)) != 0 {
		t.Fatal("a handled event must not be dead-lettered")
	}
}

func TestPluginEventConsumer_RetriesOnlyThePluginThatFailed(t *testing.T) {
	smtpDown := errors.New("dial tcp: connection refused")
	f := newPluginEventFx(t,
		// First delivery: SMTP cannot send, the webhook plugin is fine.
		[]plugindom.EventDelivery{{Plugin: "com.paca.smtp", Err: smtpDown, Retryable: true}, {Plugin: "com.paca.webhook"}},
		// Redelivery: only SMTP runs, and now succeeds.
		[]plugindom.EventDelivery{{Plugin: "com.paca.smtp"}},
	)
	f.add(t)
	msg := f.read(t, ">")

	f.c.handle(msg)
	if n := f.pending(t); n != 1 {
		t.Fatalf("a retryable failure must leave the entry pending, got %d pending", n)
	}

	f.c.handle(msg) // the reclaimer's redelivery
	if got := f.em.only[0]; len(got) != 0 {
		t.Fatalf("the first delivery must run every plugin, got only=%v", got)
	}
	if got := f.em.only[1]; len(got) != 1 || got[0] != "com.paca.smtp" {
		t.Fatalf("the redelivery must run only the plugin that failed, got only=%v", got)
	}
	if n := f.pending(t); n != 0 {
		t.Fatalf("expected the entry acked once the retry succeeded, %d pending", n)
	}
	if keys, _ := f.client.Keys(context.Background(), pluginRetryKeyPrefix+"*").Result(); len(keys) != 0 {
		t.Fatalf("retry state should be cleared after success, found %v", keys)
	}
}

func TestPluginEventConsumer_PermanentFailureIsRecordedAndNotRetried(t *testing.T) {
	f := newPluginEventFx(t, []plugindom.EventDelivery{{Plugin: "com.paca.smtp", Err: errors.New("535 bad credentials"), Retryable: false}})
	f.add(t)
	f.c.handle(f.read(t, ">"))

	if n := f.pending(t); n != 0 {
		t.Fatalf("a permanent failure must not be retried, %d pending", n)
	}
	dead := f.dead(t)
	if len(dead) != 1 {
		t.Fatalf("expected one dead-letter record, got %d", len(dead))
	}
	if reason, _ := dead[0].Values["dead_reason"].(string); reason == "" {
		t.Fatalf("the dead-letter record should say why, got %v", dead[0].Values)
	}
}

func TestPluginEventConsumer_RetryStateIsClearedWhenOnlyAPermanentFailureRemains(t *testing.T) {
	f := newPluginEventFx(t,
		[]plugindom.EventDelivery{{Plugin: "com.paca.smtp", Err: errors.New("timeout"), Retryable: true}},
		[]plugindom.EventDelivery{{Plugin: "com.paca.smtp", Err: errors.New("550 relay denied"), Retryable: false}},
	)
	f.add(t)
	msg := f.read(t, ">")
	f.c.handle(msg)
	f.c.handle(msg)

	if n := f.pending(t); n != 0 {
		t.Fatalf("expected the entry given up on, %d pending", n)
	}
	if keys, _ := f.client.Keys(context.Background(), pluginRetryKeyPrefix+"*").Result(); len(keys) != 0 {
		t.Fatalf("retry state should be cleared, found %v", keys)
	}
}

func TestPluginEventConsumer_DeadLetterWriteFailureLeavesTheEntryPending(t *testing.T) {
	f := newPluginEventFx(t,
		[]plugindom.EventDelivery{
			{Plugin: "com.paca.smtp", Err: errors.New("535 bad credentials"), Retryable: false},
			{Plugin: "com.paca.webhook"},
		},
		// The redelivery runs only the plugin whose failure could not be recorded.
		[]plugindom.EventDelivery{{Plugin: "com.paca.smtp", Err: errors.New("535 bad credentials"), Retryable: false}},
	)
	// A dead-letter stream that cannot be written to (the wrong type of key).
	if err := f.client.Set(context.Background(), events.StreamPluginEvents+".dead", "not a stream", 0).Err(); err != nil {
		t.Fatal(err)
	}
	f.add(t)
	msg := f.read(t, ">")

	f.c.handle(msg)
	if n := f.pending(t); n != 1 {
		t.Fatalf("a failure that could not be recorded must not be acked, %d pending", n)
	}
	f.c.handle(msg)
	if got := f.em.only[1]; len(got) != 1 || got[0] != "com.paca.smtp" {
		t.Fatalf("the redelivery must run only the plugin that was not recorded, got only=%v (the webhook plugin already handled it)", got)
	}
	if n := f.pending(t); n != 1 {
		t.Fatalf("still unrecorded, so still pending: %d", n)
	}
}

func TestPluginEventConsumer_FailedAckKeepsTheRetrySet(t *testing.T) {
	f := newPluginEventFx(t,
		[]plugindom.EventDelivery{{Plugin: "com.paca.smtp", Err: errors.New("timeout"), Retryable: true}, {Plugin: "com.paca.webhook"}},
		[]plugindom.EventDelivery{{Plugin: "com.paca.smtp"}},
	)
	f.add(t)
	msg := f.read(t, ">")
	f.c.handle(msg) // saves {smtp} and leaves the entry pending

	// The ack can now no longer work: the stream key is not a stream any more.
	ctx := context.Background()
	if err := f.client.Del(ctx, events.StreamPluginEvents).Err(); err != nil {
		t.Fatal(err)
	}
	if err := f.client.Set(ctx, events.StreamPluginEvents, "x", 0).Err(); err != nil {
		t.Fatal(err)
	}
	f.c.handle(msg) // the retry succeeds, but acknowledging it fails

	keys, _ := f.client.Keys(ctx, pluginRetryKeyPrefix+"*").Result()
	if len(keys) != 1 {
		t.Fatalf("retry keys = %v; the set must survive a failed ack, or the redelivery would run every plugin again", keys)
	}
}
