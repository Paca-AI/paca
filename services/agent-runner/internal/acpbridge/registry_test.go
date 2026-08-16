package acpbridge

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"github.com/Paca-AI/agent-runner/internal/messaging"
)

func newTestRegistry(t *testing.T) (*Registry, *redis.Client) {
	t.Helper()
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis.Run: %v", err)
	}
	t.Cleanup(mr.Close)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { client.Close() })
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	return New(client, messaging.NewPublisher(client), log), client
}

// fakeConn is a Conn test double recording every call, safe for concurrent
// use since Registry's background goroutines call it from outside the test
// body's own goroutine.
type fakeConn struct {
	mu        sync.Mutex
	sent      []any
	closed    bool
	closeCode int
	closeMsg  string
	closeCh   chan struct{}
}

func newFakeConn() *fakeConn {
	return &fakeConn{closeCh: make(chan struct{})}
}

func (f *fakeConn) SendJSON(_ context.Context, v any) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, v)
	return nil
}

func (f *fakeConn) Close(code int, reason string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.closed {
		f.closed = true
		f.closeCode = code
		f.closeMsg = reason
		close(f.closeCh)
	}
	return nil
}

func (f *fakeConn) wasClosed(t *testing.T, within time.Duration) bool {
	t.Helper()
	select {
	case <-f.closeCh:
		return true
	case <-time.After(within):
		return false
	}
}

func (f *fakeConn) messagesSent() []any {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]any, len(f.sent))
	copy(out, f.sent)
	return out
}

func TestRegister_SetsPresenceAndReturnsASessionID(t *testing.T) {
	r, _ := newTestRegistry(t)
	ctx := context.Background()
	agentID := uuid.New()
	conn := newFakeConn()

	sessionID, err := r.Register(ctx, agentID, nil, conn)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if sessionID == "" {
		t.Error("Register returned an empty sessionID")
	}

	online, err := r.IsOnline(ctx, agentID)
	if err != nil {
		t.Fatalf("IsOnline: %v", err)
	}
	if !online {
		t.Error("IsOnline = false immediately after Register")
	}
}

func TestIsOnline_FalseForAnUnregisteredAgent(t *testing.T) {
	r, _ := newTestRegistry(t)
	online, err := r.IsOnline(context.Background(), uuid.New())
	if err != nil {
		t.Fatalf("IsOnline: %v", err)
	}
	if online {
		t.Error("IsOnline = true for an agent that was never registered")
	}
}

func TestUnregister_ClearsPresence(t *testing.T) {
	r, _ := newTestRegistry(t)
	ctx := context.Background()
	agentID := uuid.New()
	conn := newFakeConn()

	sessionID, err := r.Register(ctx, agentID, nil, conn)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	r.Unregister(ctx, agentID, nil, sessionID)

	online, err := r.IsOnline(ctx, agentID)
	if err != nil {
		t.Fatalf("IsOnline: %v", err)
	}
	if online {
		t.Error("IsOnline = true after Unregister")
	}
}

// TestUnregister_ReturnsPromptly pins the real bug found live while
// building this package: PubSub.ReceiveMessage(ctx) does not reliably
// abort on context cancellation alone (see drainMessages's doc comment) —
// Unregister used to be able to hang indefinitely waiting for its
// background goroutines to notice. A generous bound, not a tight one: the
// point is "returns at all in a reasonable time," not exact timing.
func TestUnregister_ReturnsPromptly(t *testing.T) {
	r, _ := newTestRegistry(t)
	ctx := context.Background()
	agentID := uuid.New()
	conn := newFakeConn()

	sessionID, err := r.Register(ctx, agentID, nil, conn)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	done := make(chan struct{})
	go func() {
		r.Unregister(ctx, agentID, nil, sessionID)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Unregister did not return within 3s")
	}
}

func TestUnregister_StaleSessionIDIsANoOp(t *testing.T) {
	r, _ := newTestRegistry(t)
	ctx := context.Background()
	agentID := uuid.New()
	conn := newFakeConn()

	if _, err := r.Register(ctx, agentID, nil, conn); err != nil {
		t.Fatalf("Register: %v", err)
	}
	r.Unregister(ctx, agentID, nil, "not-the-real-session-id")

	online, err := r.IsOnline(ctx, agentID)
	if err != nil {
		t.Fatalf("IsOnline: %v", err)
	}
	if !online {
		t.Error("IsOnline = false after Unregister with a stale sessionID — should have been a no-op")
	}
}

func TestRegister_ASecondRegistrationEvictsTheFirstConnection(t *testing.T) {
	r, _ := newTestRegistry(t)
	ctx := context.Background()
	agentID := uuid.New()
	first := newFakeConn()

	if _, err := r.Register(ctx, agentID, nil, first); err != nil {
		t.Fatalf("Register (first): %v", err)
	}
	// Eviction is fire-and-forget Pub/Sub (both here and in acp_bridge.py):
	// a message published before the first connection's watchForEviction
	// goroutine has actually completed its Subscribe call is silently
	// dropped, not queued. In real usage the several other awaited I/O
	// calls Register makes before this point give that goroutine's first
	// scheduling slot time to run; back-to-back calls in a test with no
	// other work in between don't have that natural buffer, so give it one
	// explicitly rather than produce a flaky test.
	time.Sleep(50 * time.Millisecond)

	second := newFakeConn()
	secondSessionID, err := r.Register(ctx, agentID, nil, second)
	if err != nil {
		t.Fatalf("Register (second): %v", err)
	}
	if secondSessionID == "" {
		t.Error("second Register returned an empty sessionID")
	}

	if !first.wasClosed(t, 2*time.Second) {
		t.Error("the first connection was never closed after a second Register for the same agent")
	}
	if second.closed {
		t.Error("the second (winning) connection should not have been closed")
	}
}

// TestRegister_SecondRegistrationCancelsAndClosesThePreviousEntry is a
// regression test: a same-process reconnect (a second Register for the same
// agent_id) must cancel the old entry's context so its
// forwardDispatchedMessages/watchForEviction goroutines actually exit and
// release their Redis Pub/Sub subscriptions — not just evict the old
// WebSocket connection while leaving those goroutines running forever
// against an orphaned connection. Unlike
// TestRegister_ASecondRegistrationEvictsTheFirstConnection, this doesn't
// need the eviction-broadcast settle time: the cancel-and-close happens
// synchronously inside the second Register call, before it returns.
func TestRegister_SecondRegistrationCancelsAndClosesThePreviousEntry(t *testing.T) {
	r, _ := newTestRegistry(t)
	ctx := context.Background()
	agentID := uuid.New()
	first := newFakeConn()

	if _, err := r.Register(ctx, agentID, nil, first); err != nil {
		t.Fatalf("Register (first): %v", err)
	}

	r.mu.Lock()
	firstEntry := r.connections[agentID]
	r.mu.Unlock()
	if firstEntry == nil {
		t.Fatal("no entry recorded in the registry after the first Register")
	}

	second := newFakeConn()
	if _, err := r.Register(ctx, agentID, nil, second); err != nil {
		t.Fatalf("Register (second): %v", err)
	}

	select {
	case <-firstEntry.done:
	case <-time.After(2 * time.Second):
		t.Fatal("the first entry's forwarder/eviction-watcher goroutines never exited after a second Register — leaked")
	}

	if !first.wasClosed(t, 2*time.Second) {
		t.Error("the first connection was never closed by the second Register")
	}
}

func TestDispatch_ReturnsFalseWithoutPublishingWhenOffline(t *testing.T) {
	r, client := newTestRegistry(t)
	ctx := context.Background()
	agentID := uuid.New()

	sub := client.Subscribe(ctx, dispatchChannel(agentID))
	defer sub.Close()

	delivered, err := r.Dispatch(ctx, agentID, map[string]any{"type": "start_turn"})
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if delivered {
		t.Error("Dispatch returned true for an agent that was never registered")
	}
}

func TestDispatch_DeliversToARegisteredAgentsDispatchChannel(t *testing.T) {
	r, client := newTestRegistry(t)
	ctx := context.Background()
	agentID := uuid.New()
	conn := newFakeConn()

	if _, err := r.Register(ctx, agentID, nil, conn); err != nil {
		t.Fatalf("Register: %v", err)
	}

	sub := client.Subscribe(ctx, dispatchChannel(agentID))
	defer sub.Close()
	if _, err := sub.Receive(ctx); err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	delivered, err := r.Dispatch(ctx, agentID, map[string]any{
		"type": "start_turn", "conversation_id": "c1",
	})
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if !delivered {
		t.Fatal("Dispatch returned false for a registered agent")
	}

	select {
	case msg := <-sub.Channel():
		var payload map[string]any
		if err := json.Unmarshal([]byte(msg.Payload), &payload); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if payload["type"] != "start_turn" {
			t.Errorf("type = %v, want start_turn", payload["type"])
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for the dispatched message on the raw subscriber")
	}

	// Also confirms Registry's own forwarder (not just a raw subscriber)
	// delivers it to the registered connection.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if len(conn.messagesSent()) > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	sent := conn.messagesSent()
	if len(sent) != 1 {
		t.Fatalf("connection received %d message(s), want 1: %+v", len(sent), sent)
	}
}

func TestEvict_ClosesTheRegisteredConnectionRegardlessOfSession(t *testing.T) {
	r, _ := newTestRegistry(t)
	ctx := context.Background()
	agentID := uuid.New()
	conn := newFakeConn()

	if _, err := r.Register(ctx, agentID, nil, conn); err != nil {
		t.Fatalf("Register: %v", err)
	}
	// See TestRegister_ASecondRegistrationEvictsTheFirstConnection's
	// comment on this sleep — same fire-and-forget Pub/Sub subscription
	// race, not specific to eviction-via-second-Register.
	time.Sleep(50 * time.Millisecond)
	if err := r.Evict(ctx, agentID); err != nil {
		t.Fatalf("Evict: %v", err)
	}

	if !conn.wasClosed(t, 2*time.Second) {
		t.Error("Evict did not close the registered connection")
	}
	if conn.closeCode != evictionCloseCode {
		t.Errorf("close code = %d, want %d", conn.closeCode, evictionCloseCode)
	}
}

func TestEvict_UnregisteredAgentIsANoOp(t *testing.T) {
	r, _ := newTestRegistry(t)
	if err := r.Evict(context.Background(), uuid.New()); err != nil {
		t.Fatalf("Evict: %v", err)
	}
}

func TestHeartbeat_UnregisteredAgentDoesNotError(t *testing.T) {
	r, _ := newTestRegistry(t)
	if err := r.Heartbeat(context.Background(), uuid.New()); err != nil {
		t.Fatalf("Heartbeat: %v", err)
	}
}
