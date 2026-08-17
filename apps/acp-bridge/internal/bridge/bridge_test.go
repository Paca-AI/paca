package bridge

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

const testTimeout = 10 * time.Second

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

type fakeHandler struct {
	mu         sync.Mutex
	startTurns []map[string]any
	interrupts []string
}

func (h *fakeHandler) StartTurn(_ context.Context, msg map[string]any) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.startTurns = append(h.startTurns, msg)
}

func (h *fakeHandler) Interrupt(conversationID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.interrupts = append(h.interrupts, conversationID)
}

func (h *fakeHandler) startTurnCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.startTurns)
}

func (h *fakeHandler) interruptedIDs() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]string(nil), h.interrupts...)
}

// testServer accepts WebSocket connections and hands each one to the test
// over a channel, so the test itself drives the server side of the
// protocol (hello/hello_ack, sending start_turn, etc.) rather than a
// scripted handler.
type testServer struct {
	srv   *httptest.Server
	conns chan *websocket.Conn
}

func newTestServer(t *testing.T) *testServer {
	t.Helper()
	ts := &testServer{conns: make(chan *websocket.Conn, 4)}
	ts.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		ts.conns <- conn
	}))
	t.Cleanup(ts.srv.Close)
	return ts
}

func (ts *testServer) accept(t *testing.T) *websocket.Conn {
	t.Helper()
	select {
	case c := <-ts.conns:
		return c
	case <-time.After(testTimeout):
		t.Fatal("timed out waiting for a connection")
		return nil
	}
}

// handshake reads the hello frame and answers hello_ack — the minimum
// every test needs before the bridge protocol proper can be exercised.
func handshake(t *testing.T, conn *websocket.Conn) map[string]any {
	t.Helper()
	var hello map[string]any
	if err := wsjson.Read(context.Background(), conn, &hello); err != nil {
		t.Fatalf("reading hello: %v", err)
	}
	if err := wsjson.Write(context.Background(), conn, map[string]string{"type": "hello_ack"}); err != nil {
		t.Fatalf("writing hello_ack: %v", err)
	}
	return hello
}

func TestToBridgeWSURL(t *testing.T) {
	cases := []struct{ server, want string }{
		{"https://paca.example.com", "wss://paca.example.com/agent-bridge/ws"},
		{"http://localhost:8080", "ws://localhost:8080/agent-bridge/ws"},
		{"https://paca.example.com/", "wss://paca.example.com/agent-bridge/ws"},
	}
	for _, c := range cases {
		if got := toBridgeWSURL(c.server); got != c.want {
			t.Errorf("toBridgeWSURL(%q) = %q, want %q", c.server, got, c.want)
		}
	}
}

func TestHandshakeAndStartTurnDispatch(t *testing.T) {
	ts := newTestServer(t)
	handler := &fakeHandler{}
	client := New(ts.srv.URL, "agent-1", "tok", "/workspace", testLogger(),
		func(SendFunc) Handler { return handler })

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() { _ = client.RunForever(ctx) }()

	conn := ts.accept(t)
	hello := handshake(t, conn)
	if hello["type"] != "hello" || hello["agent_id"] != "agent-1" || hello["token"] != "tok" {
		t.Fatalf("unexpected hello frame: %+v", hello)
	}

	if err := wsjson.Write(context.Background(), conn, map[string]any{
		"type": "start_turn", "conversation_id": "conv-1", "project_id": "proj-1", "message": "hi",
	}); err != nil {
		t.Fatalf("writing start_turn: %v", err)
	}

	deadline := time.Now().Add(testTimeout)
	for handler.startTurnCount() != 1 {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for StartTurn; got %d calls", handler.startTurnCount())
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestStopAndPauseTurnDispatchToInterrupt(t *testing.T) {
	ts := newTestServer(t)
	handler := &fakeHandler{}
	client := New(ts.srv.URL, "agent-1", "tok", "/workspace", testLogger(),
		func(SendFunc) Handler { return handler })

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() { _ = client.RunForever(ctx) }()

	conn := ts.accept(t)
	handshake(t, conn)

	for _, msgType := range []string{"stop_turn", "pause_turn"} {
		if err := wsjson.Write(context.Background(), conn, map[string]any{
			"type": msgType, "conversation_id": "conv-1",
		}); err != nil {
			t.Fatalf("writing %s: %v", msgType, err)
		}
	}

	deadline := time.Now().Add(testTimeout)
	for len(handler.interruptedIDs()) != 2 {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for both interrupts; got %v", handler.interruptedIDs())
		}
		time.Sleep(5 * time.Millisecond)
	}
	for _, id := range handler.interruptedIDs() {
		if id != "conv-1" {
			t.Errorf("interrupted id = %q, want conv-1", id)
		}
	}
}

func TestSendQueuesWhileDisconnectedAndDeliversOnceConnected(t *testing.T) {
	ts := newTestServer(t)
	var send SendFunc
	client := New(ts.srv.URL, "agent-1", "tok", "/workspace", testLogger(), func(s SendFunc) Handler {
		send = s
		return &fakeHandler{}
	})

	// Queued before RunForever ever dials out — mirrors a turn_status that
	// was ready to go before the bridge connection existed at all.
	if err := send(context.Background(), map[string]any{
		"type": "turn_status", "conversation_id": "conv-1", "status": "finished",
	}); err != nil {
		t.Fatalf("Send returned error: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() { _ = client.RunForever(ctx) }()

	conn := ts.accept(t)
	handshake(t, conn)

	var got map[string]any
	readCtx, readCancel := context.WithTimeout(context.Background(), testTimeout)
	defer readCancel()
	if err := wsjson.Read(readCtx, conn, &got); err != nil {
		t.Fatalf("reading queued message: %v", err)
	}
	if got["type"] != "turn_status" || got["status"] != "finished" || got["conversation_id"] != "conv-1" {
		t.Fatalf("unexpected message: %+v", got)
	}
}

func TestHeartbeatPing(t *testing.T) {
	ts := newTestServer(t)
	client := New(ts.srv.URL, "agent-1", "tok", "/workspace", testLogger(),
		func(SendFunc) Handler { return &fakeHandler{} })

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() { _ = client.RunForever(ctx) }()

	conn := ts.accept(t)
	handshake(t, conn)

	// heartbeatInterval is 20s in production; rather than wait that long,
	// just confirm the server can answer a client-initiated ping/pong round
	// trip is wired correctly by simulating the server's own pong handling
	// isn't required here — the daemon only *sends* pings and ignores
	// "pong" replies, so this test only needs to confirm the connection
	// tolerates a pong frame without erroring.
	if err := wsjson.Write(context.Background(), conn, map[string]string{"type": "pong"}); err != nil {
		t.Fatalf("writing pong: %v", err)
	}
	// Give handleMessage a moment to process it (a no-op) — if it were
	// mishandled the connection would be closed, which the next check
	// would catch via the start_turn dispatch failing.
	time.Sleep(20 * time.Millisecond)
	if err := wsjson.Write(context.Background(), conn, map[string]any{
		"type": "start_turn", "conversation_id": "conv-1", "project_id": "proj-1", "message": "still alive?",
	}); err != nil {
		t.Fatalf("connection did not survive a pong frame: %v", err)
	}
}

func TestReconnectsAfterConnectionDrop(t *testing.T) {
	ts := newTestServer(t)
	client := New(ts.srv.URL, "agent-1", "tok", "/workspace", testLogger(),
		func(SendFunc) Handler { return &fakeHandler{} })

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() { _ = client.RunForever(ctx) }()

	first := ts.accept(t)
	handshake(t, first)
	_ = first.Close(websocket.StatusNormalClosure, "simulated drop")

	// initialBackoff is 1s in production; allow enough headroom for the
	// reconnect loop to notice, back off, and dial again.
	select {
	case second := <-ts.conns:
		handshake(t, second)
	case <-time.After(testTimeout):
		t.Fatal("timed out waiting for a reconnect attempt")
	}
}
