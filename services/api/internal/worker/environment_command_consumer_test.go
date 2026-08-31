package worker

import (
	"context"
	"encoding/json"
	"testing"
	"time"

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

// fakeConcurrentExecutor calls onExecuteStart (if set) synchronously from
// ExecuteStart, letting a test control timing to observe run()'s new
// goroutine-per-message dispatch — see handle's own doc comment for what
// this is verifying.
type fakeConcurrentExecutor struct {
	onExecuteStart func(id uuid.UUID)
}

func (f *fakeConcurrentExecutor) ExecuteCreate(context.Context, uuid.UUID) error { return nil }
func (f *fakeConcurrentExecutor) ExecuteStart(_ context.Context, id uuid.UUID) error {
	if f.onExecuteStart != nil {
		f.onExecuteStart(id)
	}
	return nil
}
func (f *fakeConcurrentExecutor) ExecuteStop(context.Context, uuid.UUID) error { return nil }

func startMessage(t *testing.T, id uuid.UUID) redis.XMessage {
	t.Helper()
	payload, err := json.Marshal(environmentCommandPayload{EnvironmentID: id.String()})
	require.NoError(t, err)
	return redis.XMessage{
		ID:     uuid.New().String(),
		Values: map[string]any{"type": events.TopicEnvironmentStart, "payload": string(payload)},
	}
}

// TestHandle_DifferentEnvironmentsRunConcurrently is the regression test
// for the actual bug fixed: run() used to call handle inline, so one slow
// command (now up to several minutes, waiting on agent-runner's reply —
// see environmentCommandConcurrency's own doc comment) stalled every
// later command on the stream, including a fast one for a completely
// different environment. handle is now dispatched into its own goroutine
// by run(); this test drives handle directly (bypassing the stream read)
// to verify environment B's command isn't blocked behind environment A's
// still-in-flight one.
func TestHandle_DifferentEnvironmentsRunConcurrently(t *testing.T) {
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer func() { _ = client.Close() }()

	envA := uuid.New()
	envB := uuid.New()
	blockA := make(chan struct{})
	startedB := make(chan struct{}, 1)

	svc := &fakeConcurrentExecutor{
		onExecuteStart: func(id uuid.UUID) {
			switch id {
			case envA:
				<-blockA
			case envB:
				startedB <- struct{}{}
			}
		},
	}
	c := NewEnvironmentCommandConsumer(client, svc, discardLogger())

	go c.handle(startMessage(t, envA)) // blocks indefinitely on blockA, simulating a slow create/start

	go c.handle(startMessage(t, envB))

	select {
	case <-startedB:
		// good: B's ExecuteStart ran without waiting for A to finish.
	case <-time.After(2 * time.Second):
		t.Fatal("environment B's command never started — appears blocked behind environment A's in-flight command")
	}

	close(blockA)
}

// TestHandle_SameEnvironmentSerializes confirms the fix above didn't
// introduce a different race: two commands for the *same* environment
// must still run one at a time, since ExecuteCreate/ExecuteStart/
// ExecuteStop each only re-check the row's current status once, before
// acting — two such checks for the same row interleaving on different
// goroutines would defeat that guard.
func TestHandle_SameEnvironmentSerializes(t *testing.T) {
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer func() { _ = client.Close() }()

	env := uuid.New()
	firstStarted := make(chan struct{})
	release := make(chan struct{})
	secondStarted := make(chan struct{}, 1)
	callCount := 0

	svc := &fakeConcurrentExecutor{
		onExecuteStart: func(uuid.UUID) {
			callCount++
			if callCount == 1 {
				close(firstStarted)
				<-release
			} else {
				secondStarted <- struct{}{}
			}
		},
	}
	c := NewEnvironmentCommandConsumer(client, svc, discardLogger())

	go c.handle(startMessage(t, env))
	<-firstStarted // first call is now blocked inside ExecuteStart, holding the env lock

	go c.handle(startMessage(t, env))

	select {
	case <-secondStarted:
		t.Fatal("second command for the same environment started while the first was still in flight")
	case <-time.After(100 * time.Millisecond):
		// expected: second is blocked waiting for the per-environment lock.
	}

	close(release) // let the first finish, releasing the lock

	select {
	case <-secondStarted:
		// good: second proceeded only after the first released the lock.
	case <-time.After(2 * time.Second):
		t.Fatal("second command never started even after the first released the lock")
	}
}

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
