package acp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// writeSSE writes one SSE `data:` frame and flushes it immediately — tests
// that care about streaming behavior (notifications before the terminal
// response, or a server that never sends one) depend on frames actually
// reaching the client as they're written, not buffered until the handler
// returns.
func writeSSE(t *testing.T, w http.ResponseWriter, v any) {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshaling SSE frame: %v", err)
	}
	fmt.Fprintf(w, "data: %s\n\n", b)
	w.(http.Flusher).Flush()
}

// requireHeaders checks the two headers every /acp call in this package
// must send, mirroring what the spike found goose serve actually enforces.
func requireHeaders(t *testing.T, r *http.Request, wantSecret string) {
	t.Helper()
	if got := r.Header.Get("X-Secret-Key"); got != wantSecret {
		t.Errorf("X-Secret-Key = %q, want %q", got, wantSecret)
	}
	if got := r.Header.Get("Accept"); got != "application/json, text/event-stream" {
		t.Errorf("Accept = %q, want both application/json and text/event-stream", got)
	}
}

func decodeBody(t *testing.T, r *http.Request) rpcRequest {
	t.Helper()
	var req rpcRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		t.Fatalf("decoding request body: %v", err)
	}
	return req
}

const testSecret = "spike-secret-test"

func TestInitialize(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requireHeaders(t, r, testSecret)
		req := decodeBody(t, r)
		if req.Method != "initialize" {
			t.Fatalf("method = %q, want initialize", req.Method)
		}
		if r.Header.Get("Acp-Session-Id") != "" {
			t.Errorf("initialize must not send Acp-Session-Id — it's the response that establishes one")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Acp-Session-Id", "803b1ade-8b25-416d-98a5-ba7cabcca107")
		// Captured verbatim from the spike (see docs/ai-agent/goose-migration.md).
		writeSSE(t, w, json.RawMessage(`{"jsonrpc":"2.0","result":{"protocolVersion":1,"agentCapabilities":{"loadSession":true,"promptCapabilities":{"image":true,"audio":false,"embeddedContext":true},"mcpCapabilities":{"http":true,"sse":false},"sessionCapabilities":{"list":{},"close":{}},"auth":{}},"authMethods":[{"id":"goose-provider","name":"Configure Provider","description":"Run `+"`"+`goose configure`+"`"+` to set up your AI provider and API key"}]},"id":1}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, testSecret, nil)
	if err := c.Initialize(context.Background()); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	if c.transportSessionID != "803b1ade-8b25-416d-98a5-ba7cabcca107" {
		t.Errorf("transportSessionID = %q, want the Acp-Session-Id from the response header", c.transportSessionID)
	}
}

func TestNewSession_MissingProvider(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req := decodeBody(t, r)
		switch req.Method {
		case "initialize":
			w.Header().Set("Acp-Session-Id", "conn-1")
			writeSSE(t, w, json.RawMessage(`{"jsonrpc":"2.0","result":{"protocolVersion":1},"id":1}`))
		case "session/new":
			if got := r.Header.Get("Acp-Session-Id"); got != "conn-1" {
				t.Errorf("session/new Acp-Session-Id = %q, want the value from initialize's response", got)
			}
			// Captured verbatim: session/new against a container with no
			// GOOSE_PROVIDER configured.
			writeSSE(t, w, json.RawMessage(`{"jsonrpc":"2.0","error":{"code":-32603,"message":"Internal error","data":"Failed to set provider: Could not configure agent: missing provider"},"id":2}`))
		}
	}))
	defer srv.Close()

	c := NewClient(srv.URL, testSecret, nil)
	if err := c.Initialize(context.Background()); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	_, err := c.NewSession(context.Background(), "/home/goose", nil)
	if err == nil {
		t.Fatal("NewSession: want an error for a container with no provider configured, got nil")
	}
}

func TestNewSession_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req := decodeBody(t, r)
		switch req.Method {
		case "initialize":
			w.Header().Set("Acp-Session-Id", "conn-1")
			writeSSE(t, w, json.RawMessage(`{"jsonrpc":"2.0","result":{"protocolVersion":1},"id":1}`))
		case "session/new":
			var params NewSessionParams
			raw, _ := json.Marshal(req.Params)
			_ = json.Unmarshal(raw, &params)
			if params.Cwd != "/home/goose" {
				t.Errorf("session/new cwd = %q, want /home/goose", params.Cwd)
			}
			// Captured verbatim (trimmed of the unused configOptions block).
			writeSSE(t, w, json.RawMessage(`{"jsonrpc":"2.0","result":{"sessionId":"20260810_1","modes":{"currentModeId":"auto"},"models":{"currentModelId":"fake-model"}},"id":2}`))
		}
	}))
	defer srv.Close()

	c := NewClient(srv.URL, testSecret, nil)
	if err := c.Initialize(context.Background()); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	sessionID, err := c.NewSession(context.Background(), "/home/goose", nil)
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	if sessionID != "20260810_1" {
		t.Errorf("sessionID = %q, want 20260810_1", sessionID)
	}
}

func TestPrompt_AgentMessageChunk(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req := decodeBody(t, r)
		switch req.Method {
		case "initialize":
			w.Header().Set("Acp-Session-Id", "conn-1")
			writeSSE(t, w, json.RawMessage(`{"jsonrpc":"2.0","result":{},"id":1}`))
		case "session/prompt":
			// Captured verbatim.
			writeSSE(t, w, json.RawMessage(`{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"20260810_1","update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"Spike confirmed: ACP session/prompt round-trip through goose serve works."}}}}`))
			writeSSE(t, w, json.RawMessage(`{"jsonrpc":"2.0","result":{"stopReason":"end_turn"},"id":2}`))
		}
	}))
	defer srv.Close()

	c := NewClient(srv.URL, testSecret, nil)
	if err := c.Initialize(context.Background()); err != nil {
		t.Fatalf("Initialize: %v", err)
	}

	var events []Event
	stopReason, err := c.Prompt(context.Background(), "20260810_1", []ContentBlock{TextBlock("hi")}, 0, func(e Event) {
		events = append(events, e)
	})
	if err != nil {
		t.Fatalf("Prompt: %v", err)
	}
	if stopReason != "end_turn" {
		t.Errorf("stopReason = %q, want end_turn", stopReason)
	}
	if len(events) != 1 || events[0].Kind != UpdateAgentMessageChunk {
		t.Fatalf("events = %+v, want exactly one agent_message_chunk", events)
	}
	var chunk AgentMessageChunk
	if err := json.Unmarshal(events[0].Raw, &chunk); err != nil {
		t.Fatalf("decoding AgentMessageChunk: %v", err)
	}
	if chunk.Content.Text != "Spike confirmed: ACP session/prompt round-trip through goose serve works." {
		t.Errorf("chunk text = %q", chunk.Content.Text)
	}
}

func TestPrompt_ToolCallSequence(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req := decodeBody(t, r)
		switch req.Method {
		case "initialize":
			w.Header().Set("Acp-Session-Id", "conn-1")
			writeSSE(t, w, json.RawMessage(`{"jsonrpc":"2.0","result":{},"id":1}`))
		case "session/prompt":
			// Captured verbatim, post-cwd-fix (real "completed" shell run).
			writeSSE(t, w, json.RawMessage(`{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"20260810_1","update":{"sessionUpdate":"tool_call","toolCallId":"call_fake_1","title":"Developer: Shell"}}}`))
			writeSSE(t, w, json.RawMessage(`{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"20260810_1","update":{"sessionUpdate":"tool_call_update","toolCallId":"call_fake_1","status":"completed","content":[{"type":"content","content":{"type":"text","text":"hello-from-goose-acp-spike"}}]}}}`))
			writeSSE(t, w, json.RawMessage(`{"jsonrpc":"2.0","result":{"stopReason":"end_turn"},"id":2}`))
		}
	}))
	defer srv.Close()

	c := NewClient(srv.URL, testSecret, nil)
	if err := c.Initialize(context.Background()); err != nil {
		t.Fatalf("Initialize: %v", err)
	}

	var events []Event
	stopReason, err := c.Prompt(context.Background(), "20260810_1", []ContentBlock{TextBlock("run echo")}, 5, func(e Event) {
		events = append(events, e)
	})
	if err != nil {
		t.Fatalf("Prompt: %v", err)
	}
	if stopReason != "end_turn" {
		t.Errorf("stopReason = %q, want end_turn", stopReason)
	}
	if len(events) != 2 || events[0].Kind != UpdateToolCall || events[1].Kind != UpdateToolCallUpdate {
		t.Fatalf("events = %+v, want [tool_call, tool_call_update]", events)
	}
	var update ToolCallUpdate
	if err := json.Unmarshal(events[1].Raw, &update); err != nil {
		t.Fatalf("decoding ToolCallUpdate: %v", err)
	}
	if update.Status != "completed" {
		t.Errorf("status = %q, want completed", update.Status)
	}
	if update.Text() != "hello-from-goose-acp-spike" {
		t.Errorf("Text() = %q, want hello-from-goose-acp-spike", update.Text())
	}
}

// TestPrompt_MaxToolCallsExceeded reproduces the spike's runaway-loop
// scenario (a non-converging scripted reply produced 600+ tool-call cycles
// with no server-side cap) and asserts the client's own limit actually cuts
// the turn off instead of reading forever.
func TestPrompt_MaxToolCallsExceeded(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req := decodeBody(t, r)
		switch req.Method {
		case "initialize":
			w.Header().Set("Acp-Session-Id", "conn-1")
			writeSSE(t, w, json.RawMessage(`{"jsonrpc":"2.0","result":{},"id":1}`))
		case "session/prompt":
			// Never sends a terminal response — same shape as the spike's
			// non-converging loop. The client must give up on its own.
			for i := 0; i < 50; i++ {
				writeSSE(t, w, json.RawMessage(`{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"s","update":{"sessionUpdate":"tool_call","toolCallId":"call_fake_1","title":"Developer: Shell"}}}`))
				writeSSE(t, w, json.RawMessage(`{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"s","update":{"sessionUpdate":"tool_call_update","toolCallId":"call_fake_1","status":"completed","content":[]}}}`))
			}
		}
	}))
	defer srv.Close()

	c := NewClient(srv.URL, testSecret, nil)
	if err := c.Initialize(context.Background()); err != nil {
		t.Fatalf("Initialize: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	toolCalls := 0
	_, err := c.Prompt(ctx, "s", []ContentBlock{TextBlock("loop forever")}, 3, func(e Event) {
		if e.Kind == UpdateToolCall {
			toolCalls++
		}
	})
	if err != ErrMaxToolCalls {
		t.Fatalf("err = %v, want ErrMaxToolCalls", err)
	}
	if toolCalls != 3 {
		// Prompt checks the counter before dispatching to onEvent, so the
		// 4th (limit-tripping) tool_call is never delivered to the
		// callback — only the 3 allowed ones are.
		t.Errorf("toolCalls = %d, want 3 (the 4th trips the limit before it's delivered to onEvent)", toolCalls)
	}
}

// TestPrompt_RespectsContextDeadlineOnAHungServer is the mechanism
// executor.go's turn-level timeout actually relies on (see its
// defaultTimeoutMinutes doc comment): a server that accepts the connection
// and then never writes a single byte back reproduces, in miniature, the
// real bug found while building the sandbox image — a wrong mcpServers
// wire format made a real goose serve's session/new hang forever rather
// than return an ACP error. Confirms Prompt actually returns once the
// caller's context expires, instead of blocking for however long the
// server chooses never to respond.
func TestPrompt_RespectsContextDeadlineOnAHungServer(t *testing.T) {
	initialized := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req := decodeBody(t, r)
		switch req.Method {
		case "initialize":
			w.Header().Set("Acp-Session-Id", "conn-1")
			writeSSE(t, w, json.RawMessage(`{"jsonrpc":"2.0","result":{},"id":1}`))
			close(initialized)
		case "session/prompt":
			// Accepts the connection, sends response headers (so the
			// client's Do() call returns and it starts reading the body),
			// then never writes another byte — the server-side shape of
			// the real hang, independent of whatever upstream cause
			// produced it that day.
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
			<-r.Context().Done() // hang until the client gives up
		}
	}))
	defer srv.Close()

	c := NewClient(srv.URL, testSecret, nil)
	if err := c.Initialize(context.Background()); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	<-initialized

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err := c.Prompt(ctx, "s", []ContentBlock{TextBlock("hello")}, 0, nil)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("Prompt: want a context-deadline error against a server that never responds, got nil")
	}
	if elapsed > 2*time.Second {
		t.Errorf("Prompt took %s to return after a 300ms context deadline — it isn't actually bounded by the context", elapsed)
	}
}

// TestPrompt_CancelledMidBacklogReturnsPromptly reproduces the actual live
// bug found running this against a real goose serve driving a
// non-converging tool-call loop: the server writes a burst of frames
// rapidly, several land in the client's read buffer before it's even
// consumed the first one, and — if ctx is only checked when sse.Next()
// itself returns a read error, not on every loop iteration — cancelling
// mid-backlog had no effect until every already-buffered frame was
// processed first. Against the real server, with onEvent's two blocking
// Redis calls per event, that turned a "stop" button press into a ~30s
// delay. This confirms Prompt actually bails out between frames instead.
func TestPrompt_CancelledMidBacklogReturnsPromptly(t *testing.T) {
	const backlogSize = 300
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req := decodeBody(t, r)
		switch req.Method {
		case "initialize":
			w.Header().Set("Acp-Session-Id", "conn-1")
			writeSSE(t, w, json.RawMessage(`{"jsonrpc":"2.0","result":{},"id":1}`))
		case "session/prompt":
			// Written and flushed as fast as the loopback connection
			// allows — by the time a slow-consuming client reads its
			// first frame, most or all of this is already sitting in the
			// client's read buffer, not pending on the network.
			for range backlogSize {
				writeSSE(t, w, json.RawMessage(`{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"s","update":{"sessionUpdate":"tool_call","toolCallId":"call_fake_1","title":"Developer: Shell"}}}`))
			}
			<-r.Context().Done()
		}
	}))
	defer srv.Close()

	c := NewClient(srv.URL, testSecret, nil)
	if err := c.Initialize(context.Background()); err != nil {
		t.Fatalf("Initialize: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	// A slow onEvent, standing in for onEvent's two blocking Redis calls
	// in the real handler.Handler — see the doc comment above.
	start := time.Now()
	_, err := c.Prompt(ctx, "s", []ContentBlock{TextBlock("hello")}, 0, func(Event) {
		time.Sleep(20 * time.Millisecond)
	})
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("Prompt: want an error from the cancelled context, got nil")
	}
	// backlogSize * 20ms would be 6s if every buffered frame had to drain
	// through the slow callback before cancellation was noticed — the
	// actual bug this test reproduces. A generous bound well under that,
	// not a tight one, since this is timing-sensitive by nature.
	if elapsed > 2*time.Second {
		t.Errorf("Prompt took %s to return after cancelling 20ms in — wants roughly one onEvent call's worth of delay, not the whole %d-frame backlog (%s worth at 20ms/frame)",
			elapsed, backlogSize, time.Duration(backlogSize)*20*time.Millisecond)
	}
}
