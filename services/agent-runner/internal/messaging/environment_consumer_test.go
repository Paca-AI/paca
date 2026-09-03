package messaging

import (
	"context"
	"encoding/json"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

// TestEnvironmentCommandConsumer_DispatchesAndReplies confirms the basic
// round trip: a command entry on StreamAgentEnvironmentCommands reaches
// the handler with its decoded type/payload, and a successful result comes
// back as an {"ok":true,"payload":...} envelope on reply_key.
func TestEnvironmentCommandConsumer_DispatchesAndReplies(t *testing.T) {
	client, cleanup := newTestRedisClient(t)
	defer cleanup()

	var gotType string
	var gotPayload string
	handled := make(chan struct{}, 1)
	consumer := NewEnvironmentCommandConsumer(client, 5,
		func(_ context.Context, cmdType string, payload json.RawMessage) (json.RawMessage, error) {
			gotType = cmdType
			gotPayload = string(payload)
			handled <- struct{}{}
			return json.RawMessage(`{"backend_ref":"container-123"}`), nil
		},
		testLogger(),
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go consumer.Run(ctx)
	waitForGroup(t, client, StreamAgentEnvironmentCommands, environmentCommandConsumerGroup)

	replyKey := EnvironmentReplyKey("req-1")
	if err := client.XAdd(ctx, &redis.XAddArgs{
		Stream: StreamAgentEnvironmentCommands,
		Values: map[string]any{
			"type":       EnvironmentCommandCreate,
			"request_id": "req-1",
			"reply_key":  replyKey,
			"payload":    `{"environment_id":"env-1"}`,
		},
	}).Err(); err != nil {
		t.Fatalf("XAdd: %v", err)
	}

	waitOrFail(t, handled, "environment command handler")
	if gotType != EnvironmentCommandCreate {
		t.Errorf("gotType = %q, want %q", gotType, EnvironmentCommandCreate)
	}
	if gotPayload != `{"environment_id":"env-1"}` {
		t.Errorf("gotPayload = %q", gotPayload)
	}

	// Peek (don't pop) until the reply lands *and* its TTL is set, so this
	// can be checked while the key still holds it — BRPop below would
	// otherwise empty the list and Redis/miniredis auto-deletes an emptied
	// list key, making any TTL check after that point meaningless (always
	// "gone", never a real signal either way). Polls rather than a single
	// check: RPush and Expire are two separate calls in finalize, so a
	// single unlucky read could land in the brief gap between them (key
	// exists, TTL not yet set) — that's a timing artifact, not a real
	// "TTL never got set" failure.
	deadline := time.Now().Add(2 * time.Second)
	for {
		ttl, err := client.TTL(ctx, replyKey).Result()
		if err == nil && ttl > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("reply key TTL never became positive (last: %v, err: %v)", ttl, err)
		}
		time.Sleep(10 * time.Millisecond)
	}

	result, err := client.BRPop(ctx, 2*time.Second, replyKey).Result()
	if err != nil {
		t.Fatalf("BRPop reply: %v", err)
	}
	var reply EnvironmentCommandReply
	if err := json.Unmarshal([]byte(result[1]), &reply); err != nil {
		t.Fatalf("decode reply: %v", err)
	}
	if !reply.OK || reply.Error != "" {
		t.Errorf("reply = %+v, want OK with no error", reply)
	}
	if string(reply.Payload) != `{"backend_ref":"container-123"}` {
		t.Errorf("reply.Payload = %s", reply.Payload)
	}
}

// TestEnvironmentCommandConsumer_HandlerError confirms a failing handler
// produces an {"ok":false,"error":...} reply rather than dropping the
// command silently.
func TestEnvironmentCommandConsumer_HandlerError(t *testing.T) {
	client, cleanup := newTestRedisClient(t)
	defer cleanup()

	consumer := NewEnvironmentCommandConsumer(client, 5,
		func(_ context.Context, _ string, _ json.RawMessage) (json.RawMessage, error) {
			return nil, errBoom
		},
		testLogger(),
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go consumer.Run(ctx)
	waitForGroup(t, client, StreamAgentEnvironmentCommands, environmentCommandConsumerGroup)

	replyKey := EnvironmentReplyKey("req-2")
	if err := client.XAdd(ctx, &redis.XAddArgs{
		Stream: StreamAgentEnvironmentCommands,
		Values: map[string]any{
			"type":       EnvironmentCommandStart,
			"request_id": "req-2",
			"reply_key":  replyKey,
			"payload":    `{}`,
		},
	}).Err(); err != nil {
		t.Fatalf("XAdd: %v", err)
	}

	result, err := client.BRPop(ctx, 2*time.Second, replyKey).Result()
	if err != nil {
		t.Fatalf("BRPop reply: %v", err)
	}
	var reply EnvironmentCommandReply
	if err := json.Unmarshal([]byte(result[1]), &reply); err != nil {
		t.Fatalf("decode reply: %v", err)
	}
	if reply.OK || reply.Error != errBoom.Error() {
		t.Errorf("reply = %+v, want OK=false error=%q", reply, errBoom.Error())
	}
}

// TestEnvironmentCommandConsumer_BoundsConcurrency confirms maxConcurrency
// actually caps how many commands run their handler at once — today's HTTP
// handler-per-request model this consumer replaces had no such bound at
// all.
func TestEnvironmentCommandConsumer_BoundsConcurrency(t *testing.T) {
	client, cleanup := newTestRedisClient(t)
	defer cleanup()

	const maxConcurrency = 2
	const numCommands = 5

	var inFlight int32
	var maxObserved int32
	release := make(chan struct{})
	started := make(chan struct{}, numCommands)

	consumer := NewEnvironmentCommandConsumer(client, maxConcurrency,
		func(_ context.Context, _ string, _ json.RawMessage) (json.RawMessage, error) {
			n := atomic.AddInt32(&inFlight, 1)
			for {
				old := atomic.LoadInt32(&maxObserved)
				if n <= old || atomic.CompareAndSwapInt32(&maxObserved, old, n) {
					break
				}
			}
			started <- struct{}{}
			<-release
			atomic.AddInt32(&inFlight, -1)
			return json.RawMessage(`{}`), nil
		},
		testLogger(),
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go consumer.Run(ctx)
	waitForGroup(t, client, StreamAgentEnvironmentCommands, environmentCommandConsumerGroup)

	for i := 0; i < numCommands; i++ {
		if err := client.XAdd(ctx, &redis.XAddArgs{
			Stream: StreamAgentEnvironmentCommands,
			Values: map[string]any{
				"type":       EnvironmentCommandCreate,
				"request_id": uuidLike(i),
				"reply_key":  EnvironmentReplyKey(uuidLike(i)),
				"payload":    `{}`,
			},
		}).Err(); err != nil {
			t.Fatalf("XAdd %d: %v", i, err)
		}
	}

	// Exactly maxConcurrency handlers should start and then block on
	// release — wait for that many to actually report in.
	for i := 0; i < maxConcurrency; i++ {
		waitOrFail(t, started, "handler start")
	}
	// Give any (incorrectly) over-admitted handler a moment it could use
	// to start too, before asserting none did.
	select {
	case <-started:
		t.Fatal("more than maxConcurrency handlers started concurrently")
	case <-time.After(100 * time.Millisecond):
	}

	close(release)
	for i := maxConcurrency; i < numCommands; i++ {
		waitOrFail(t, started, "handler start")
	}

	if got := atomic.LoadInt32(&maxObserved); got > maxConcurrency {
		t.Errorf("max concurrent handlers = %d, want <= %d", got, maxConcurrency)
	}
}

// TestEnvironmentCommandConsumer_HandlerSurvivesRunCancellation is the
// regression test for the bug this consumer's design specifically avoids:
// today's http.Client.Timeout firing tears down the TCP connection, which
// cancels agent-runner's own r.Context() too, aborting in-flight k8s
// provisioning. Here, cancelling Run's ctx (agent-runner shutting down)
// while a handler is still in flight must NOT cancel that handler's own
// context, and the handler's result must still be delivered (RPushed +
// acked) once it finishes.
func TestEnvironmentCommandConsumer_HandlerSurvivesRunCancellation(t *testing.T) {
	client, cleanup := newTestRedisClient(t)
	defer cleanup()

	handlerStarted := make(chan struct{})
	release := make(chan struct{})
	var sawCancellation atomic.Bool

	consumer := NewEnvironmentCommandConsumer(client, 5,
		func(ctx context.Context, _ string, _ json.RawMessage) (json.RawMessage, error) {
			close(handlerStarted)
			<-release
			if ctx.Err() != nil {
				sawCancellation.Store(true)
			}
			return json.RawMessage(`{}`), nil
		},
		testLogger(),
	)

	runCtx, cancelRun := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		consumer.Run(runCtx)
	}()
	waitForGroup(t, client, StreamAgentEnvironmentCommands, environmentCommandConsumerGroup)

	replyKey := EnvironmentReplyKey("req-shutdown")
	if err := client.XAdd(context.Background(), &redis.XAddArgs{
		Stream: StreamAgentEnvironmentCommands,
		Values: map[string]any{
			"type":       EnvironmentCommandCreate,
			"request_id": "req-shutdown",
			"reply_key":  replyKey,
			"payload":    `{}`,
		},
	}).Err(); err != nil {
		t.Fatalf("XAdd: %v", err)
	}

	select {
	case <-handlerStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("handler never started")
	}

	// Simulate agent-runner shutting down mid-command: Run's read loop
	// stops, but the already-admitted handler goroutine above is still
	// blocked on release.
	cancelRun()
	wg.Wait()

	close(release)

	result, err := client.BRPop(context.Background(), 2*time.Second, replyKey).Result()
	if err != nil {
		t.Fatalf("BRPop reply (should still arrive after Run stopped): %v", err)
	}
	var reply EnvironmentCommandReply
	if err := json.Unmarshal([]byte(result[1]), &reply); err != nil {
		t.Fatalf("decode reply: %v", err)
	}
	if !reply.OK {
		t.Errorf("reply = %+v, want OK", reply)
	}
	if sawCancellation.Load() {
		t.Error("handler's own context was cancelled by Run's ctx — it should be independent")
	}
}

var errBoom = errBoomError("boom")

type errBoomError string

func (e errBoomError) Error() string { return string(e) }

func uuidLike(i int) string {
	return "00000000-0000-0000-0000-00000000000" + string(rune('0'+i))
}
