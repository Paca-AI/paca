package acpclient

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"testing"
	"time"
)

const testTimeout = 5 * time.Second

// fakePeer emulates the far end of the stdio connection under test: it
// reads the NDJSON lines the Client under test writes (its "stdin") and
// lets the test script responses/notifications back, which the Client
// reads as its "stdout". Standing in for a real ACP-speaking subprocess so
// these tests run fast and in-process — see newTestClient.
//
// A dedicated goroutine (started by newTestClient) owns the actual pipe
// reader and forwards each line onto lines; when the Client closes its
// stdin (e.g. via Close), that goroutine sees EOF and closes the peer's
// output too — mirroring a real subprocess that exits (and so closes its
// own stdout) once its stdin closes. Without this cascade, Close wouldn't
// actually unblock anything in a test the way it does against a real
// subprocess.
type fakePeer struct {
	t     *testing.T
	lines chan []byte
	w     io.WriteCloser
}

type inLine struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// next reads and decodes the next line the Client sent as a request or
// notification (i.e. it has a method).
func (p *fakePeer) next() inLine {
	p.t.Helper()
	raw := p.rawLine()
	var msg inLine
	if err := json.Unmarshal(raw, &msg); err != nil {
		p.t.Fatalf("fakePeer: decoding line %q: %v", raw, err)
	}
	return msg
}

func (p *fakePeer) rawLine() []byte {
	p.t.Helper()
	select {
	case raw, ok := <-p.lines:
		if !ok {
			p.t.Fatalf("fakePeer: no more input (client closed its stdin)")
		}
		return raw
	case <-time.After(testTimeout):
		p.t.Fatalf("fakePeer: timed out waiting for a line from the client")
		return nil
	}
}

func (p *fakePeer) respond(id json.RawMessage, result any) {
	p.write(map[string]any{"jsonrpc": "2.0", "id": json.RawMessage(id), "result": result})
}

func (p *fakePeer) notify(method string, params any) {
	p.write(map[string]any{"jsonrpc": "2.0", "method": method, "params": params})
}

func (p *fakePeer) write(v any) {
	p.t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		p.t.Fatalf("fakePeer: encoding: %v", err)
	}
	b = append(b, '\n')
	if _, err := p.w.Write(b); err != nil {
		p.t.Fatalf("fakePeer: writing: %v", err)
	}
}

func newTestClient(t *testing.T, onUpdate func(Update)) (*Client, *fakePeer) {
	t.Helper()
	clientStdoutR, peerStdoutW := io.Pipe()
	peerStdinR, clientStdinW := io.Pipe()

	lines := make(chan []byte, 64)
	go func() {
		defer close(lines)
		defer func() { _ = peerStdoutW.Close() }()
		scanner := bufio.NewScanner(peerStdinR)
		scanner.Buffer(make([]byte, 64*1024), 1<<20)
		for scanner.Scan() {
			lines <- append([]byte(nil), scanner.Bytes()...)
		}
	}()

	peer := &fakePeer{t: t, lines: lines, w: peerStdoutW}

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	c := newClient(clientStdinW, clientStdoutR, func() error { return nil }, func() {}, onUpdate, log)

	t.Cleanup(func() {
		_ = clientStdinW.Close()
		_ = peerStdoutW.Close()
	})
	return c, peer
}

func withTimeout(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	t.Cleanup(cancel)
	return ctx
}

func TestInitializeSendsProtocolVersionAndDeclinesFsTerminal(t *testing.T) {
	c, peer := newTestClient(t, nil)
	ctx := withTimeout(t)

	errCh := make(chan error, 1)
	go func() { errCh <- c.Initialize(ctx) }()

	req := peer.next()
	if req.Method != "initialize" {
		t.Fatalf("method = %q, want %q", req.Method, "initialize")
	}
	var params struct {
		ProtocolVersion    int `json:"protocolVersion"`
		ClientCapabilities struct {
			FS struct {
				ReadTextFile  bool `json:"readTextFile"`
				WriteTextFile bool `json:"writeTextFile"`
			} `json:"fs"`
			Terminal bool `json:"terminal"`
		} `json:"clientCapabilities"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		t.Fatalf("decoding params: %v", err)
	}
	if params.ProtocolVersion != 1 {
		t.Errorf("protocolVersion = %d, want 1", params.ProtocolVersion)
	}
	if params.ClientCapabilities.FS.ReadTextFile || params.ClientCapabilities.FS.WriteTextFile || params.ClientCapabilities.Terminal {
		t.Errorf("expected fs/terminal capabilities to be declined, got %+v", params.ClientCapabilities)
	}

	peer.respond(req.ID, map[string]any{"protocolVersion": 1})

	if err := <-errCh; err != nil {
		t.Fatalf("Initialize returned error: %v", err)
	}
}

func TestNewSessionReturnsSessionIDAndModes(t *testing.T) {
	c, peer := newTestClient(t, nil)
	ctx := withTimeout(t)

	type result struct {
		sessionID string
		modes     *SessionModeState
		err       error
	}
	resCh := make(chan result, 1)
	go func() {
		sessionID, modes, err := c.NewSession(ctx, "/workspace")
		resCh <- result{sessionID, modes, err}
	}()

	req := peer.next()
	if req.Method != "session/new" {
		t.Fatalf("method = %q, want session/new", req.Method)
	}
	var params struct {
		Cwd        string `json:"cwd"`
		MCPServers []any  `json:"mcpServers"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		t.Fatalf("decoding params: %v", err)
	}
	if params.Cwd != "/workspace" {
		t.Errorf("cwd = %q, want /workspace", params.Cwd)
	}
	if params.MCPServers == nil || len(params.MCPServers) != 0 {
		t.Errorf("mcpServers = %v, want an empty (but present) array", params.MCPServers)
	}

	peer.respond(req.ID, map[string]any{
		"sessionId": "sess-1",
		"modes": map[string]any{
			"currentModeId":  "default",
			"availableModes": []map[string]any{{"id": "default"}, {"id": "bypassPermissions"}},
		},
	})

	res := <-resCh
	if res.err != nil {
		t.Fatalf("NewSession returned error: %v", res.err)
	}
	if res.sessionID != "sess-1" {
		t.Errorf("sessionID = %q, want sess-1", res.sessionID)
	}
	if !res.modes.Offers("bypassPermissions") {
		t.Errorf("expected modes to offer bypassPermissions, got %+v", res.modes)
	}
	if res.modes.Offers("agent-full-access") {
		t.Errorf("did not expect modes to offer agent-full-access")
	}
}

func TestPromptStreamsUpdatesInOrderThenReturnsStopReason(t *testing.T) {
	var got []Update
	c, peer := newTestClient(t, func(u Update) { got = append(got, u) })
	ctx := withTimeout(t)

	type result struct {
		stopReason string
		usage      *Usage
		err        error
	}
	resCh := make(chan result, 1)
	go func() {
		stopReason, usage, err := c.Prompt(ctx, "sess-1", "hello")
		resCh <- result{stopReason, usage, err}
	}()

	req := peer.next()
	if req.Method != "session/prompt" {
		t.Fatalf("method = %q, want session/prompt", req.Method)
	}
	var params struct {
		SessionID string `json:"sessionId"`
		Prompt    []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"prompt"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		t.Fatalf("decoding params: %v", err)
	}
	if params.SessionID != "sess-1" || len(params.Prompt) != 1 || params.Prompt[0].Text != "hello" {
		t.Fatalf("unexpected prompt params: %+v", params)
	}

	peer.notify("session/update", map[string]any{
		"sessionId": "sess-1",
		"update":    map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]any{"type": "text", "text": "thinking..."}},
	})
	peer.notify("session/update", map[string]any{
		"sessionId": "sess-1",
		"update":    map[string]any{"sessionUpdate": "tool_call", "toolCallId": "tc-1", "title": "Bash: ls"},
	})
	peer.respond(req.ID, map[string]any{
		"stopReason": "end_turn",
		"usage":      map[string]any{"totalTokens": 150, "inputTokens": 100, "outputTokens": 50},
	})

	res := <-resCh
	if res.err != nil {
		t.Fatalf("Prompt returned error: %v", res.err)
	}
	if res.stopReason != "end_turn" {
		t.Errorf("stopReason = %q, want end_turn", res.stopReason)
	}
	if res.usage == nil || res.usage.TotalTokens != 150 || res.usage.InputTokens != 100 || res.usage.OutputTokens != 50 {
		t.Errorf("usage = %+v, want {150 100 50}", res.usage)
	}

	// onUpdate must observe both notifications, in order, before Prompt's
	// own response unblocks it — this is what lets the caller build a
	// paragraph-buffered "agent_message_chunk" event before a following
	// tool_call event without a race.
	if len(got) != 2 {
		t.Fatalf("got %d updates, want 2: %+v", len(got), got)
	}
	if got[0].Kind != "agent_message_chunk" || got[1].Kind != "tool_call" {
		t.Errorf("update kinds = [%q, %q], want [agent_message_chunk, tool_call]", got[0].Kind, got[1].Kind)
	}
	if got[0].SessionID != "sess-1" {
		t.Errorf("sessionID = %q, want sess-1", got[0].SessionID)
	}
}

func TestPromptReturnsNilUsageWhenAgentReportsNone(t *testing.T) {
	c, peer := newTestClient(t, nil)
	ctx := withTimeout(t)

	type result struct {
		usage *Usage
		err   error
	}
	resCh := make(chan result, 1)
	go func() {
		_, usage, err := c.Prompt(ctx, "sess-1", "hello")
		resCh <- result{usage, err}
	}()

	req := peer.next()
	peer.respond(req.ID, map[string]any{"stopReason": "end_turn"})

	res := <-resCh
	if res.err != nil {
		t.Fatalf("Prompt returned error: %v", res.err)
	}
	if res.usage != nil {
		t.Errorf("usage = %+v, want nil", res.usage)
	}
}

func TestRequestPermissionRequestIsAutoApproved(t *testing.T) {
	c, peer := newTestClient(t, nil)
	_ = c

	// The agent sends this as a REQUEST (it expects a response), not a
	// notification — id 42 chosen arbitrarily by the "agent" side.
	peer.write(map[string]any{
		"jsonrpc": "2.0",
		"id":      42,
		"method":  "session/request_permission",
		"params": map[string]any{
			"sessionId": "sess-1",
			"toolCall":  map[string]any{"toolCallId": "tc-1"},
			"options": []map[string]any{
				{"optionId": "allow-once", "name": "Allow once", "kind": "allow_once"},
				{"optionId": "reject-once", "name": "Reject once", "kind": "reject_once"},
			},
		},
	})

	raw := peer.rawLine()
	var body struct {
		ID     json.RawMessage `json:"id"`
		Result *struct {
			Outcome struct {
				Outcome  string `json:"outcome"`
				OptionID string `json:"optionId"`
			} `json:"outcome"`
		} `json:"result"`
		Error any `json:"error"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if string(body.ID) != "42" {
		t.Fatalf("response id = %s, want 42", body.ID)
	}
	if body.Error != nil {
		t.Fatalf("got error response: %+v", body.Error)
	}
	if body.Result == nil || body.Result.Outcome.Outcome != "selected" || body.Result.Outcome.OptionID != "allow-once" {
		t.Fatalf("unexpected result: %+v", body.Result)
	}
}

func TestUnsupportedIncomingMethodGetsMethodNotFound(t *testing.T) {
	c, peer := newTestClient(t, nil)
	_ = c

	peer.write(map[string]any{
		"jsonrpc": "2.0",
		"id":      7,
		"method":  "fs/read_text_file",
		"params":  map[string]any{"path": "/etc/hosts", "sessionId": "sess-1"},
	})

	raw := peer.rawLine()
	var body struct {
		ID    json.RawMessage `json:"id"`
		Error *RPCError       `json:"error"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if body.Error == nil {
		t.Fatalf("expected an error response for an unsupported method, got none")
	}
	if body.Error.Code != -32601 {
		t.Errorf("error code = %d, want -32601 (method not found)", body.Error.Code)
	}
}

func TestCancelIsANotificationAndPromptReturnsOnSubsequentResponse(t *testing.T) {
	c, peer := newTestClient(t, nil)
	ctx := withTimeout(t)

	type result struct {
		stopReason string
		err        error
	}
	resCh := make(chan result, 1)
	go func() {
		stopReason, _, err := c.Prompt(ctx, "sess-1", "do something long-running")
		resCh <- result{stopReason, err}
	}()

	promptReq := peer.next()

	if err := c.Cancel("sess-1"); err != nil {
		t.Fatalf("Cancel returned error: %v", err)
	}
	cancelMsg := peer.next()
	if cancelMsg.Method != "session/cancel" {
		t.Fatalf("method = %q, want session/cancel", cancelMsg.Method)
	}
	if len(cancelMsg.ID) != 0 {
		t.Errorf("session/cancel must be a notification (no id), got id=%s", cancelMsg.ID)
	}

	// Per the ACP spec, the agent is expected to answer the ORIGINAL
	// session/prompt request with stopReason "cancelled" rather than
	// dropping the connection.
	peer.respond(promptReq.ID, map[string]any{"stopReason": "cancelled"})

	res := <-resCh
	if res.err != nil {
		t.Fatalf("Prompt returned error: %v", res.err)
	}
	if res.stopReason != "cancelled" {
		t.Errorf("stopReason = %q, want cancelled", res.stopReason)
	}
}

func TestCloseUnblocksAPendingCall(t *testing.T) {
	c, _ := newTestClient(t, nil)
	ctx := withTimeout(t)

	errCh := make(chan error, 1)
	go func() { errCh <- c.Initialize(ctx) }()

	// Give the goroutine a moment to actually reach the blocking wait
	// before closing out from under it.
	time.Sleep(20 * time.Millisecond)
	if err := c.Close(); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}

	select {
	case err := <-errCh:
		if err == nil {
			t.Fatalf("expected Initialize to fail once the connection closed")
		}
	case <-time.After(testTimeout):
		t.Fatal("Initialize did not unblock after Close")
	}
}
