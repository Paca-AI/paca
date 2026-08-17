package runner

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/Paca-AI/paca/apps/acp-bridge/internal/acpclient"
)

// testTimeout bounds both this file's message-wait helpers (waitFor and
// friends) and the two end-to-end interrupt tests' own turnRunning polling
// loops. For the latter it must clear runner.go's interruptGracePeriod
// (15s) — Interrupt's own documented worst-case bound for how long an
// uncooperative agent can legitimately take to resolve — plus real headroom
// for a loaded CI runner (these tests spawn actual subprocesses under the
// race detector, on a shared 2-core GitHub Actions runner running every
// other package in this module concurrently). 10s used to be enough because
// the fake agent always answers session/cancel near-instantly, but that
// left zero margin against interruptGracePeriod itself, let alone CI
// slowness — see the "timed out waiting for the interrupted turn to
// finish" flake this replaced.
const testTimeout = 30 * time.Second

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

	// reportStatus enqueues onto state's own local outbound queue (see
	// ensureOutbound) rather than delivering synchronously, so this waits
	// for the dedicated forwarder goroutine to actually send it.
	waitForStatus(t, sent, "failed")
	msgs := sent.all()
	if len(msgs) != 1 {
		t.Fatalf("got %d messages, want 1: %+v", len(msgs), msgs)
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

	// reportStatus enqueues onto state's own local outbound queue (see
	// ensureOutbound) rather than delivering synchronously, so this waits
	// for the dedicated forwarder goroutine to actually send it.
	waitForStatus(t, sent, "failed")
	msgs := sent.all()
	if len(msgs) != 1 {
		t.Fatalf("got %d messages, want 1: %+v", len(msgs), msgs)
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

// TestHandleUpdateNeverBlocksOnAStalledSend is the core guarantee
// ensureOutbound exists for: handleUpdate runs synchronously on the ACP
// client's own stdout-reading goroutine (see acpclient.Spawn's doc
// comment), so it must return promptly even if delivery to the bridge is
// completely stuck — otherwise a prolonged bridge outage would stall
// draining the ACP subprocess's stdout pipe and could deadlock it.
func TestHandleUpdateNeverBlocksOnAStalledSend(t *testing.T) {
	blockSend := make(chan struct{})
	unblockSend := make(chan struct{})
	r := &Runner{
		workspace: t.TempDir(),
		log:       slog.New(slog.NewTextHandler(io.Discard, nil)),
		send: func(ctx context.Context, msg map[string]any) error {
			close(blockSend) // signals the first send actually started
			<-unblockSend    // and now hangs until the test releases it
			return nil
		},
		conversations: make(map[string]*conversationState),
	}
	t.Cleanup(func() { close(unblockSend) })

	state := &conversationState{chunks: newChunkBuffer()}

	done := make(chan struct{})
	go func() {
		defer close(done)
		r.handleUpdate(state, "conv-1", "proj-1", acpclient.Update{
			SessionID: "sess-1",
			Kind:      "tool_call",
			Raw:       json.RawMessage(`{"sessionUpdate":"tool_call","toolCallId":"tc-1"}`),
		})
	}()

	select {
	case <-done:
	case <-time.After(testTimeout):
		t.Fatal("handleUpdate blocked on a stalled send instead of returning promptly")
	}

	// Confirm the send genuinely was attempted (and is genuinely stuck) —
	// otherwise this test would trivially pass for the wrong reason.
	select {
	case <-blockSend:
	case <-time.After(testTimeout):
		t.Fatal("forwardEvents never attempted to send the enqueued update")
	}
}

// --- Usage/cost accounting tests ---------------------------------------

func TestHandleUpdateRecordsUsageCostAndSuppressesEvent(t *testing.T) {
	r, sent := newTestRunner(t)
	state := &conversationState{chunks: newChunkBuffer()}

	r.handleUpdate(state, "conv-1", "proj-1", acpclient.Update{
		SessionID: "sess-1",
		Kind:      "usage_update",
		Raw: json.RawMessage(
			`{"sessionUpdate":"usage_update","used":100,"size":1000,"cost":{"amount":0.0042,"currency":"USD"}}`),
	})

	state.mu.Lock()
	cost := state.turnCostUSD
	state.mu.Unlock()
	if cost == nil || *cost != 0.0042 {
		t.Fatalf("turnCostUSD = %v, want 0.0042", cost)
	}
	// usage_update must never reach the transcript — handleUpdate returns
	// before ever enqueueing anything for it, so this is deterministic, not
	// a race that needs a sleep.
	if msgs := sent.all(); len(msgs) != 0 {
		t.Fatalf("expected usage_update to produce no outbound message, got %+v", msgs)
	}
}

func TestHandleUpdateIgnoresUsageUpdateWithNoCost(t *testing.T) {
	r, _ := newTestRunner(t)
	state := &conversationState{chunks: newChunkBuffer()}

	r.handleUpdate(state, "conv-1", "proj-1", acpclient.Update{
		SessionID: "sess-1",
		Kind:      "usage_update",
		Raw:       json.RawMessage(`{"sessionUpdate":"usage_update","used":100,"size":1000}`),
	})

	state.mu.Lock()
	cost := state.turnCostUSD
	state.mu.Unlock()
	if cost != nil {
		t.Fatalf("turnCostUSD = %v, want nil", cost)
	}
}

func TestEmitTurnUsageIncludesTokensAndCost(t *testing.T) {
	r, sent := newTestRunner(t)
	cost := 0.0042
	state := &conversationState{chunks: newChunkBuffer(), turnCostUSD: &cost}

	r.emitTurnUsage(state, "conv-1", "proj-1", &acpclient.Usage{TotalTokens: 150, InputTokens: 100, OutputTokens: 50})

	msg := waitForEventType(t, sent, "turn_usage")
	if msg["event_source"] != "system" {
		t.Errorf("event_source = %v, want system", msg["event_source"])
	}
	payload, ok := msg["payload"].(json.RawMessage)
	if !ok {
		t.Fatalf("payload is not json.RawMessage: %T", msg["payload"])
	}
	var decoded struct {
		InputTokens  int64   `json:"input_tokens"`
		OutputTokens int64   `json:"output_tokens"`
		TotalTokens  int64   `json:"total_tokens"`
		CostUSD      float64 `json:"cost_usd"`
	}
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatalf("decoding payload: %v", err)
	}
	if decoded.InputTokens != 100 || decoded.OutputTokens != 50 || decoded.TotalTokens != 150 || decoded.CostUSD != 0.0042 {
		t.Fatalf("unexpected payload: %+v", decoded)
	}
}

func TestEmitTurnUsageIsNoOpWhenNothingReported(t *testing.T) {
	r, sent := newTestRunner(t)
	state := &conversationState{chunks: newChunkBuffer()}

	r.emitTurnUsage(state, "conv-1", "proj-1", nil)

	if msgs := sent.all(); len(msgs) != 0 {
		t.Fatalf("expected no message when neither usage nor cost was reported, got %+v", msgs)
	}
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
			if behavior == "stall_init" {
				continue // never respond, simulating a slow npx cold start
			}
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
			if behavior == "usage" {
				write(map[string]any{"jsonrpc": "2.0", "method": "session/update", "params": map[string]any{
					"sessionId": "sess-1",
					"update": map[string]any{"sessionUpdate": "agent_message_chunk",
						"content": map[string]any{"type": "text", "text": "Done."}},
				}})
				write(map[string]any{"jsonrpc": "2.0", "method": "session/update", "params": map[string]any{
					"sessionId": "sess-1",
					"update": map[string]any{"sessionUpdate": "usage_update", "used": 100, "size": 1000,
						"cost": map[string]any{"amount": 0.0042, "currency": "USD"}},
				}})
				write(map[string]any{"jsonrpc": "2.0", "id": json.RawMessage(msg.ID),
					"result": map[string]any{"stopReason": "end_turn",
						"usage": map[string]any{"totalTokens": 150, "inputTokens": 100, "outputTokens": 50}}})
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

// TestEndToEndTurnEmitsTurnUsageWithTokensAndCost covers the full pipeline
// this feature adds: a real subprocess reporting both a "usage_update"
// session/update notification (cost) and a session/prompt result usage
// field (tokens) for the same turn, verifying they're combined into one
// "turn_usage" event — the same shape
// services/agent-runner/internal/handler.Handler persists for llm-type
// agents — rather than the usage_update notification leaking into the
// transcript as its own event.
func TestEndToEndTurnEmitsTurnUsageWithTokensAndCost(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns a real subprocess")
	}
	setFakeAgentEnv(t, "usage")

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
	want := []string{"user_message", "agent_message_chunk", "turn_end", "turn_usage"}
	if !reflect.DeepEqual(eventTypes, want) {
		t.Fatalf("event types = %v, want %v", eventTypes, want)
	}

	var usageMsg map[string]any
	for _, m := range sent.all() {
		if m["type"] == "event" && m["event_type"] == "turn_usage" {
			usageMsg = m
		}
	}
	if usageMsg["event_source"] != "system" {
		t.Errorf("event_source = %v, want system", usageMsg["event_source"])
	}
	payload, ok := usageMsg["payload"].(json.RawMessage)
	if !ok {
		t.Fatalf("payload is not json.RawMessage: %T", usageMsg["payload"])
	}
	var decoded struct {
		InputTokens  int64   `json:"input_tokens"`
		OutputTokens int64   `json:"output_tokens"`
		TotalTokens  int64   `json:"total_tokens"`
		CostUSD      float64 `json:"cost_usd"`
	}
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatalf("decoding payload: %v", err)
	}
	if decoded.InputTokens != 100 || decoded.OutputTokens != 50 || decoded.TotalTokens != 150 || decoded.CostUSD != 0.0042 {
		t.Fatalf("unexpected payload: %+v", decoded)
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

// TestEndToEndInterruptDuringSessionSetupSuppressesTurnStatus covers the
// other half of Interrupt's two branches: interrupting while state.client
// is still nil (still spawning/initializing) must be just as silent as
// interrupting an in-flight Prompt call above — see Interrupt's own
// "still spawning/initializing" comment. ensureSession's ctx-cancellation
// error needs the same errors.Is(err, context.Canceled) treatment runTurn
// already gives client.Prompt's error.
func TestEndToEndInterruptDuringSessionSetupSuppressesTurnStatus(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns a real subprocess")
	}
	setFakeAgentEnv(t, "stall_init")

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
		"message":         "hi",
	})

	// Give the subprocess a moment to actually be spawned and stuck inside
	// Initialize (state.client is still nil at this point) before
	// interrupting — otherwise this could race and hit the client.Prompt
	// path TestEndToEndInterruptSuppressesTurnStatus already covers.
	deadline := time.Now().Add(testTimeout)
	for {
		state.mu.Lock()
		spawning := state.turnRunning && state.client == nil
		state.mu.Unlock()
		if spawning {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for the turn to be mid session-setup")
		}
		time.Sleep(5 * time.Millisecond)
	}

	r.Interrupt("conv-1")

	deadline = time.Now().Add(testTimeout)
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
			t.Fatalf("expected no turn_status for an interrupt during session setup, got %+v", m)
		}
	}
}
