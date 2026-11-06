package acpbridge

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func newHelloTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	s := &Server{Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	ts := httptest.NewServer(s.Routes())
	t.Cleanup(ts.Close)
	return ts
}

func dialBridge(t *testing.T, ts *httptest.Server) *websocket.Conn {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, dialResp, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(ts.URL, "http")+"/agent-bridge/ws", nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	if dialResp != nil && dialResp.Body != nil {
		_ = dialResp.Body.Close()
	}
	t.Cleanup(func() { _ = conn.CloseNow() })
	return conn
}

// A peer that has not authenticated must not be able to write bridge-sized
// frames: the pre-auth limit is the only thing standing between an anonymous
// socket and the relay's 10 MiB buffer.
func TestHandleWSRejectsOversizedPreAuthFrame(t *testing.T) {
	ts := newHelloTestServer(t)
	conn := dialBridge(t, ts)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = conn.Write(ctx, websocket.MessageText, bytes.Repeat([]byte("a"), 2*helloReadLimit))
	_, _, err := conn.Read(ctx)
	// 1009 proves the frame was refused by the pre-auth limit; a malformed
	// hello under the bridge-sized limit closes with 1007 instead, because
	// wsjson.Read rejects the JSON before the handler's 4400 close runs.
	if status := websocket.CloseStatus(err); status != websocket.StatusMessageTooBig {
		t.Fatalf("close status = %d, want %d (err=%v)", status, websocket.StatusMessageTooBig, err)
	}
}

func TestHandleWSRefusesWhenHandshakeBudgetIsExhausted(t *testing.T) {
	ts := newHelloTestServer(t)
	for i := 0; i < maxPendingHellos; i++ {
		helloSlots <- struct{}{}
	}
	t.Cleanup(func() {
		for i := 0; i < maxPendingHellos; i++ {
			<-helloSlots
		}
	})

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, ts.URL+"/agent-bridge/ws", nil)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusServiceUnavailable)
	}
}

func TestHandleWSReleasesHandshakeSlotAfterRejectedHello(t *testing.T) {
	ts := newHelloTestServer(t)
	conn := dialBridge(t, ts)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = conn.Write(ctx, websocket.MessageText, []byte(`{"type":"hello","agent_id":"not-a-uuid","token":"x"}`))
	if _, _, err := conn.Read(ctx); err == nil {
		t.Fatal("expected the connection to be closed for an invalid agent_id")
	}

	deadline := time.Now().Add(2 * time.Second)
	for len(helloSlots) != 0 {
		if time.Now().After(deadline) {
			t.Fatalf("handshake slot not released, %d still held", len(helloSlots))
		}
		time.Sleep(10 * time.Millisecond)
	}
}
