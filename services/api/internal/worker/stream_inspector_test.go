package worker

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"

	"github.com/Paca-AI/api/internal/events"
)

func TestStreamInspector_ReportsPendingAndDeadLetters(t *testing.T) {
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	ctx := context.Background()

	// An entry delivered to a consumer that never acknowledged it.
	if err := client.XGroupCreateMkStream(ctx, events.StreamTaskAssignments, notificationConsumerGroup, "0").Err(); err != nil {
		t.Fatal(err)
	}
	if err := client.XAdd(ctx, &redis.XAddArgs{Stream: events.StreamTaskAssignments, Values: map[string]any{"payload": "x"}}).Err(); err != nil {
		t.Fatal(err)
	}
	if err := client.XReadGroup(ctx, &redis.XReadGroupArgs{
		Group: notificationConsumerGroup, Consumer: "c", Streams: []string{events.StreamTaskAssignments, ">"}, Count: 10,
	}).Err(); err != nil {
		t.Fatal(err)
	}
	mr.SetTime(time.Now().Add(7 * time.Minute)) // time passes in the fake server

	// One dead letter now, one from yesterday.
	dead := events.StreamTaskAssignments + ".dead"
	old := time.Now().Add(-24 * time.Hour).UnixMilli()
	if err := client.XAdd(ctx, &redis.XAddArgs{Stream: dead, ID: formatID(old), Values: map[string]any{"k": "old"}}).Err(); err != nil {
		t.Fatal(err)
	}
	if err := client.XAdd(ctx, &redis.XAddArgs{Stream: dead, Values: map[string]any{"k": "new"}}).Err(); err != nil {
		t.Fatal(err)
	}

	got, err := NewStreamInspector(client).StreamHealth(ctx, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(notificationStreams) {
		t.Fatalf("reported %d streams, want %d", len(got), len(notificationStreams))
	}
	var tasks, plugins = got[0], got[1]
	if tasks.Stream != events.StreamTaskAssignments || tasks.Pending != 1 {
		t.Fatalf("tasks = %+v; want the unacknowledged entry counted", tasks)
	}
	if tasks.OldestPendingIdle <= 0 {
		t.Fatalf("idle = %v, want a positive wait for the unacknowledged entry", tasks.OldestPendingIdle)
	}
	if tasks.DeadTotal != 2 || tasks.DeadLastHour != 1 {
		t.Fatalf("dead total/last hour = %d/%d, want 2/1 (yesterday's does not count as recent)", tasks.DeadTotal, tasks.DeadLastHour)
	}
	if plugins.Pending != 0 || plugins.DeadTotal != 0 {
		t.Fatalf("a stream that does not exist yet must read as empty, got %+v", plugins)
	}
}

func formatID(ms int64) string { return strconv.FormatInt(ms, 10) + "-0" }

func TestStreamInspector_ReportsValkeyFailuresInsteadOfReadingAsEmpty(t *testing.T) {
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	mr.Close() // Valkey goes away

	if _, err := NewStreamInspector(client).StreamHealth(context.Background(), time.Now()); err == nil {
		t.Fatal("an unreachable Valkey must be an error, not a stream with nothing pending: the admin page would say ok")
	}
}
