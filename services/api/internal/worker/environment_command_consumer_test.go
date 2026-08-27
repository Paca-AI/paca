package worker

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"

	"github.com/Paca-AI/api/internal/events"
)

// fakeLifecycleExecutor is a function-field-based mock of
// environmentLifecycleExecutor, counting how many times ExecuteStart runs.
type fakeLifecycleExecutor struct {
	startCalls int
}

func (f *fakeLifecycleExecutor) ExecuteCreate(context.Context, uuid.UUID) error { return nil }
func (f *fakeLifecycleExecutor) ExecuteStart(context.Context, uuid.UUID) error {
	f.startCalls++
	return nil
}
func (f *fakeLifecycleExecutor) ExecuteStop(context.Context, uuid.UUID) error { return nil }

// TestProcessPending_DrainsMoreThanOneBatch verifies processPending loops
// until the PEL is empty rather than reading a single
// environmentCommandReadCount-sized batch — a backlog larger than one batch
// must not leave its excess stranded until another process restart (see
// processPending's own doc comment).
func TestProcessPending_DrainsMoreThanOneBatch(t *testing.T) {
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer func() { _ = client.Close() }()

	svc := &fakeLifecycleExecutor{}
	c := NewEnvironmentCommandConsumer(client, svc, discardLogger())
	ctx := context.Background()

	require.NoError(t, c.ensureGroup(ctx, "0"))

	// More than one batch (environmentCommandReadCount == 10).
	const totalCommands = 25
	for i := 0; i < totalCommands; i++ {
		payload, err := json.Marshal(environmentCommandPayload{EnvironmentID: uuid.New().String()})
		require.NoError(t, err)
		require.NoError(t, client.XAdd(ctx, &redis.XAddArgs{
			Stream: events.StreamEnvironmentCommands,
			Values: map[string]any{"type": events.TopicEnvironmentStart, "payload": string(payload)},
		}).Err())
	}

	// Deliver every message into this consumer's own PEL without acking —
	// simulates a prior run that read them but crashed (or failed to ack)
	// before finishing.
	_, err := client.XReadGroup(ctx, &redis.XReadGroupArgs{
		Group:    environmentCommandConsumerGroup,
		Consumer: c.consumerName,
		Streams:  []string{events.StreamEnvironmentCommands, ">"},
		Count:    totalCommands,
	}).Result()
	require.NoError(t, err)

	streamLen, err := client.XLen(ctx, events.StreamEnvironmentCommands).Result()
	require.NoError(t, err)
	require.EqualValues(t, totalCommands, streamLen)

	c.processPending(ctx)

	require.Equal(t, totalCommands, svc.startCalls, "expected every pending command to be processed, not just the first batch")

	pending, err := client.XPending(ctx, events.StreamEnvironmentCommands, environmentCommandConsumerGroup).Result()
	require.NoError(t, err)
	require.EqualValues(t, 0, pending.Count, "expected every processed command to be acked")
}

// TestProcessPending_EmptyPELIsANoOp verifies processPending returns
// promptly (a single empty read) rather than looping when there's nothing
// pending.
func TestProcessPending_EmptyPELIsANoOp(t *testing.T) {
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer func() { _ = client.Close() }()

	svc := &fakeLifecycleExecutor{}
	c := NewEnvironmentCommandConsumer(client, svc, discardLogger())
	ctx := context.Background()

	require.NoError(t, c.ensureGroup(ctx, "0"))

	c.processPending(ctx)

	require.Equal(t, 0, svc.startCalls)
}
