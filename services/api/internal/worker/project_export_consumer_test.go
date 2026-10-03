package worker

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"github.com/Paca-AI/api/internal/events"
	exportsvc "github.com/Paca-AI/api/internal/service/export"
)

type claimFailingExecutor struct{ calls atomic.Int32 }

func (e *claimFailingExecutor) Execute(context.Context, uuid.UUID) error {
	e.calls.Add(1)
	return errors.Join(exportsvc.ErrClaim, errors.New("db down"))
}
func (e *claimFailingExecutor) CleanupExpired(context.Context) (int, error) { return 0, nil }

// A backlog of unacked claim failures larger than one read batch must be
// replayed once and then the replay must end, not spin on the same messages.
func TestProcessPending_UnackedBacklogDoesNotSpin(t *testing.T) {
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	ctx := context.Background()
	exec := &claimFailingExecutor{}
	c := NewProjectExportConsumer(client, exec, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err := c.ensureGroup(ctx, "0"); err != nil {
		t.Fatal(err)
	}

	const n = projectExportReadCount*2 + 1
	for i := 0; i < n; i++ {
		if err := client.XAdd(ctx, &redis.XAddArgs{Stream: events.StreamProjectExports, Values: map[string]any{
			"type": events.TopicProjectExportRequested, "payload": `{"export_id":"` + uuid.NewString() + `"}`,
		}}).Err(); err != nil {
			t.Fatal(err)
		}
	}
	// Deliver everything to this consumer so it sits in the pending list.
	if err := client.XReadGroup(ctx, &redis.XReadGroupArgs{
		Group: projectExportConsumerGroup, Consumer: c.consumerName,
		Streams: []string{events.StreamProjectExports, ">"}, Count: n,
	}).Err(); err != nil {
		t.Fatal(err)
	}

	done := make(chan struct{})
	go func() { c.processPending(ctx); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("processPending did not return: it is re-reading unacked messages")
	}
	if got := exec.calls.Load(); got != n {
		t.Errorf("Execute called %d times, want %d (each message once)", got, n)
	}
	pending, err := client.XPending(ctx, events.StreamProjectExports, projectExportConsumerGroup).Result()
	if err != nil || pending.Count != n {
		t.Errorf("pending = %v (err %v), want %d left for the next start", pending, err, n)
	}
}
