package runner

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"reflect"
	"sync"
	"testing"
	"time"
)

const testTimeout = 10 * time.Second

// capturedSends is a fake bridge.SendFunc that records every message and
// also fans it out on notify so tests can wait for a specific message
// instead of polling.
type capturedSends struct {
	mu     sync.Mutex
	msgs   []map[string]any
	notify chan map[string]any
}

func newCapturedSends() *capturedSends {
	return &capturedSends{notify: make(chan map[string]any, 256)}
}

func (c *capturedSends) send(_ context.Context, msg map[string]any) error {
	c.mu.Lock()
	c.msgs = append(c.msgs, msg)
	c.mu.Unlock()
	select {
	case c.notify <- msg:
	default:
	}
	return nil
}

func (c *capturedSends) all() []map[string]any {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]map[string]any(nil), c.msgs...)
}

func newTestRunner(t *testing.T) (*Runner, *capturedSends) {
	t.Helper()
	sent := newCapturedSends()
	handler := New(t.TempDir(), nil)(sent.send)
	r, ok := handler.(*Runner)
	if !ok {
		t.Fatalf("New's handler is not *Runner (got %T)", handler)
	}
	return r, sent
}

func waitFor(t *testing.T, sent *capturedSends, match func(map[string]any) bool) map[string]any {
	t.Helper()
	deadline := time.After(testTimeout)
	for {
		select {
		case msg := <-sent.notify:
			if match(msg) {
				return msg
			}
		case <-deadline:
			t.Fatalf("timed out waiting for a matching message; got %+v", sent.all())
			return nil
		}
	}
}

func waitForStatus(t *testing.T, sent *capturedSends, status string) map[string]any {
	t.Helper()
	return waitFor(t, sent, func(m map[string]any) bool {
		return m["type"] == "turn_status" && m["status"] == status
	})
}

func waitForEventType(t *testing.T, sent *capturedSends, eventType string) map[string]any {
	t.Helper()
	return waitFor(t, sent, func(m map[string]any) bool {
		return m["type"] == "event" && m["event_type"] == eventType
	})
}

// --- Guard-rail tests: no subprocess involved -------------------------------

func TestStartTurnRejectsWhenPreviousTurnStillRunning(t *testing.T) {
	r, sent := newTestRunner(t)
	state := &conversationState{chunks: newChunkBuffer(), turnRunning: true, turnDone: make(chan struct{})}
	r.conversations["conv-1"] = state

	r.StartTurn(context.Background(), map[string]any{
		"conversation_id": "conv-1",
		"project_id":      "proj-1",
		"message":         "a follow-up message",
	})

	msgs := sent.all()
	if len(msgs) != 1 {
		t.Fatalf("got %d messages, want 1: %+v", len(msgs), msgs)
	}
	if msgs[0]["type"] != "turn_status" || msgs[0]["status"] != "failed" {
		t.Fatalf("unexpected message: %+v", msgs[0])
	}
	if r.conversations["conv-1"] != state {
		t.Fatalf("expected the still-running conversation's state to be left untouched")
	}
}

func TestStartTurnReportsFailureForUnresolvableCustomProvider(t *testing.T) {
	r, sent := newTestRunner(t)

	r.StartTurn(context.Background(), map[string]any{
		"conversation_id": "conv-1",
		"project_id":      "proj-1",
		"message":         "hi",
		"acp_provider":    "custom",
		"acp_command":     []any{},
	})

	msgs := sent.all()
	if len(msgs) != 1 {
		t.Fatalf("got %d messages, want 1: %+v", len(msgs), msgs)
	}
	if msgs[0]["type"] != "turn_status" || msgs[0]["status"] != "failed" {
		t.Fatalf("unexpected message: %+v", msgs[0])
	}

	r.mu.Lock()
	_, exists := r.conversations["conv-1"]
	r.mu.Unlock()
	if exists {
		t.Fatalf("expected the conversation state to be removed after a resolve failure")
	}
}

func TestInterruptIsANoOpForUnknownOrIdleConversation(t *testing.T) {
	r, _ := newTestRunner(t)
	r.Interrupt("does-not-exist")
	r.Interrupt("")

	r.conversations["conv-idle"] = &conversationState{chunks: newChunkBuffer()}
	r.Interrupt("conv-idle") // turnRunning is false; must not panic or block
}

func TestChunkBufferAccumulatesAndTakes(t *testing.T) {
	b := newChunkBuffer()
	b.append("agent_message_chunk", "Hello, ")
	b.append("agent_message_chunk", "world.")
	if got := b.take("agent_message_chunk"); got != "Hello, world." {
		t.Fatalf("got %q", got)
	}
	if got := b.take("agent_message_chunk"); got != "" {
		t.Fatalf("expected empty after take, got %q", got)
	}
}

func TestChunkBufferKeepsKindsSeparate(t *testing.T) {
	b := newChunkBuffer()
	b.append("agent_message_chunk", "reply text")
	b.append("agent_thought_chunk", "reasoning text")
	if got := b.take("agent_message_chunk"); got != "reply text" {
		t.Fatalf("got %q", got)
	}
	if got := b.take("agent_thought_chunk"); got != "reasoning text" {
		t.Fatalf("got %q", got)
	}
}

func TestExtractChunkText(t *testing.T) {
	text := extractChunkText(json.RawMessage(
		`{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"hi there"}}`))
	if text != "hi there" {
		t.Fatalf("got %q", text)
	}
	nonText := extractChunkText(json.RawMessage(
		`{"sessionUpdate":"agent_message_chunk","content":{"type":"image"}}`))
	if nonText != "" {
		t.Fatalf("expected empty for non-text content, got %q", nonText)
	}
}

func TestStringSlice(t *testing.T) {
	got := stringSlice([]any{"a", "b", 3, "c"})
	want := []string{"a", "b", "c"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	if stringSlice(nil) != nil {
		t.Fatalf("expected nil for nil input")
	}
}

// --- End-to-end tests: a real subprocess, re-exec'ing this test binary -----
//
// TestMain intercepts and runs as a scripted fake ACP agent instead of the
// test suite when PACA_ACPCLIENT_FAKE_AGENT=1 is set — the standard
// os/exec "TestHelperProcess" pattern, used here so the runner is exercised
// against a real stdio subprocess (real pipes, real JSON-RPC framing) for
// at least the two scenarios that matter most: a full turn, and one
// interrupted mid-flight.

func TestMain(m *testing.M) {
	if os.Getenv("PACA_ACPCLIENT_FAKE_AGENT") == "1" {
		runFakeACPAgent(os.Getenv("PACA_ACPCLIENT_FAKE_AGENT_BEHAVIOR"))
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func runFakeACPAgent(behavior string) {
	in := bufio.NewScanner(os.Stdin)
	in.Buffer(make([]byte, 64*1024), 1<<20)
	write := func(v any) {
		b, _ := json.Marshal(v)
		_, _ = os.Stdout.Write(append(b, '\n'))
	}

	for in.Scan() {
		var msg struct {
			ID     json.RawMessage `json:"id,omitempty"`
			Method string          `json:"method"`
		}
		if err := json.Unmarshal(in.Bytes(), &msg); err != nil {
			continue
		}
		switch msg.Method {
		case "initialize":
			write(map[string]any{"jsonrpc": "2.0", "id": json.RawMessage(msg.ID),
				"result": map[string]any{"protocolVersion": 1}})
		case "session/new":
			write(map[string]any{"jsonrpc": "2.0", "id": json.RawMessage(msg.ID),
				"result": map[string]any{"sessionId": "sess-1"}})
		case "session/set_mode":
			write(map[string]any{"jsonrpc": "2.0", "id": json.RawMessage(msg.ID), "result": map[string]any{}})
		case "session/prompt":
			if behavior == "cancel" {
				// Wait for the client to actually send session/cancel
				// before answering, so the test's Interrupt call is what
				// unblocks Prompt — not a race with an immediate reply.
				for in.Scan() {
					var note struct {
						Method string `json:"method"`
					}
					if json.Unmarshal(in.Bytes(), &note) == nil && note.Method == "session/cancel" {
						break
					}
				}
				write(map[string]any{"jsonrpc": "2.0", "id": json.RawMessage(msg.ID),
					"result": map[string]any{"stopReason": "cancelled"}})
				continue
			}
			write(map[string]any{"jsonrpc": "2.0", "method": "session/update", "params": map[string]any{
				"sessionId": "sess-1",
				"update":    map[string]any{"sessionUpdate": "tool_call", "toolCallId": "tc-1", "title": "Bash: ls"},
			}})
			write(map[string]any{"jsonrpc": "2.0", "method": "session/update", "params": map[string]any{
				"sessionId": "sess-1",
				"update": map[string]any{"sessionUpdate": "agent_message_chunk",
					"content": map[string]any{"type": "text", "text": "Done."}},
			}})
			write(map[string]any{"jsonrpc": "2.0", "id": json.RawMessage(msg.ID),
				"result": map[string]any{"stopReason": "end_turn"}})
		}
	}
}

func setFakeAgentEnv(t *testing.T, behavior string) {
	t.Helper()
	t.Setenv("PACA_ACPCLIENT_FAKE_AGENT", "1")
	t.Setenv("PACA_ACPCLIENT_FAKE_AGENT_BEHAVIOR", behavior)
}

func TestEndToEndTurnEmitsEventsAndFinishes(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns a real subprocess")
	}
	setFakeAgentEnv(t, "normal")

	r, sent := newTestRunner(t)
	r.mu.Lock()
	r.conversations["conv-1"] = &conversationState{
		chunks:      newChunkBuffer(),
		acpProvider: "custom",
		command:     []string{os.Args[0]},
	}
	r.mu.Unlock()

	r.StartTurn(context.Background(), map[string]any{
		"conversation_id": "conv-1",
		"project_id":      "proj-1",
		"message":         "do something",
	})

	waitForStatus(t, sent, "finished")

	var eventTypes []string
	for _, m := range sent.all() {
		if m["type"] == "event" {
			eventTypes = append(eventTypes, m["event_type"].(string))
		}
	}
	want := []string{"user_message", "tool_call", "agent_message_chunk", "turn_end"}
	if !reflect.DeepEqual(eventTypes, want) {
		t.Fatalf("event types = %v, want %v", eventTypes, want)
	}
}

func TestEndToEndInterruptSuppressesTurnStatus(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns a real subprocess")
	}
	setFakeAgentEnv(t, "cancel")

	r, sent := newTestRunner(t)
	state := &conversationState{
		chunks:      newChunkBuffer(),
		acpProvider: "custom",
		command:     []string{os.Args[0]},
	}
	r.mu.Lock()
	r.conversations["conv-1"] = state
	r.mu.Unlock()

	r.StartTurn(context.Background(), map[string]any{
		"conversation_id": "conv-1",
		"project_id":      "proj-1",
		"message":         "do something long-running",
	})

	// Confirms the ACP session actually started (and the fake agent is
	// sitting inside session/prompt) before interrupting it.
	waitForEventType(t, sent, "user_message")

	r.Interrupt("conv-1")

	deadline := time.Now().Add(testTimeout)
	for {
		state.mu.Lock()
		running := state.turnRunning
		state.mu.Unlock()
		if !running {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for the interrupted turn to finish")
		}
		time.Sleep(10 * time.Millisecond)
	}

	for _, m := range sent.all() {
		if m["type"] == "turn_status" {
			t.Fatalf("expected no turn_status for an interrupted turn, got %+v", m)
		}
	}
}
