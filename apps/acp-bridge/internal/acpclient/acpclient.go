// Package acpclient is a minimal Agent Client Protocol client for a Go
// process that spawns an ACP-speaking coding CLI (Claude Code / Codex /
// Gemini CLI / Goose / a custom ACP server) as a local subprocess and talks
// to it over stdio.
//
// Unlike services/agent-runner/internal/acp (which speaks Goose's HTTP+SSE
// "one POST per call" transport to a container it doesn't own the process
// of), this package owns the subprocess directly and speaks the protocol's
// native stdio transport: newline-delimited JSON-RPC 2.0 over the child's
// stdin/stdout, one JSON value per line, in both directions. Verified
// against the reference Python SDK's own transport
// (acp/connection.py's `_reader.readuntil(b"\n")`) and method/field names
// (acp/meta.py, acp/schema.py) rather than guessed from the public spec
// alone.
//
// The connection is bidirectional: besides the requests this client sends
// (initialize, session/new, session/prompt, ...), the agent process calls
// back into methods this client must answer — most importantly
// session/request_permission, which this client always auto-approves (a
// headless daemon has no user to prompt), and fs/*/terminal/*, which it
// declines (every built-in provider does its own file/terminal I/O
// natively; advertising support for those would be a lie the agent might
// act on).
package acpclient

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"
	"time"
)

// protocolVersion is the ACP schema version this client speaks — a small
// integer (not a semver), pinned across every ACP participant in this
// codebase (see services/agent-runner/internal/acp/client.go's identical
// value, and acp/meta.py's PROTOCOL_VERSION = 1).
const protocolVersion = 1

// closeTimeout bounds how long Close waits for the subprocess to exit on
// its own (stdin closed) before it's killed outright.
const closeTimeout = 5 * time.Second

// RPCError is a JSON-RPC 2.0 error object.
type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

func (e *RPCError) Error() string {
	if e.Data != nil {
		return fmt.Sprintf("%s (code %d): %v", e.Message, e.Code, e.Data)
	}
	return fmt.Sprintf("%s (code %d)", e.Message, e.Code)
}

// inMessage is one line read off the subprocess's stdout, decoded loosely
// enough to tell a request (id+method), a notification (method only), and
// a response (id only) apart before committing to either shape.
type inMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
}

func (m inMessage) isResponse() bool     { return len(m.ID) > 0 && m.Method == "" }
func (m inMessage) isRequest() bool      { return len(m.ID) > 0 && m.Method != "" }
func (m inMessage) isNotification() bool { return len(m.ID) == 0 && m.Method != "" }

type outRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int64  `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type outNotification struct {
	JSONRPC string `json:"jsonrpc"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type outResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
}

// Update is one session/update notification, handed to the OnUpdate
// callback for every kind (agent_message_chunk, tool_call,
// tool_call_update, agent_thought_chunk, ...) verbatim — the caller decides
// which kinds matter and how to forward them; this package doesn't
// interpret the payload beyond pulling out the discriminator.
type Update struct {
	SessionID string
	Kind      string
	// Raw is the full raw `update` object exactly as the agent sent it
	// (camelCase field names, untouched) — forwarding this as-is is what
	// lets the receiving end reuse the same wire shape
	// services/agent-runner/internal/acp already established for
	// Goose-in-sandbox conversations.
	Raw json.RawMessage
}

type sessionUpdateParams struct {
	SessionID string         `json:"sessionId"`
	Update    updateEnvelope `json:"update"`
}

type updateEnvelope struct {
	Kind string
	Raw  json.RawMessage
}

func (e *updateEnvelope) UnmarshalJSON(data []byte) error {
	var disc struct {
		SessionUpdate string `json:"sessionUpdate"`
	}
	if err := json.Unmarshal(data, &disc); err != nil {
		return err
	}
	e.Kind = disc.SessionUpdate
	e.Raw = append(json.RawMessage(nil), data...)
	return nil
}

// SessionModeState mirrors NewSessionResponse.modes / SetSessionModeRequest
// — just enough to check whether a desired permission-bypass mode is
// actually on offer before trying to set it.
type SessionModeState struct {
	CurrentModeID  string `json:"currentModeId"`
	AvailableModes []struct {
		ID string `json:"id"`
	} `json:"availableModes"`
}

// Offers reports whether modeID is one of the session's available modes.
// A nil receiver (no modes reported at all) offers nothing.
func (s *SessionModeState) Offers(modeID string) bool {
	if s == nil || modeID == "" {
		return false
	}
	for _, m := range s.AvailableModes {
		if m.ID == modeID {
			return true
		}
	}
	return false
}

// Client is one subprocess's ACP connection — one built-in coding CLI
// spawned against one workspace, driving exactly one ACP session for the
// subprocess's whole lifetime (a chat conversation's later turns reattach
// to the same Client rather than spawning a new subprocess each time,
// mirroring the one-conversation-one-process model the old Python bridge
// used). Not safe for concurrent Prompt calls — callers must serialize
// their own turns; Cancel and Close, on the other hand, are safe to call
// while a Prompt is in flight (that's the whole point of Cancel).
type Client struct {
	stdin io.WriteCloser
	// wait blocks until the peer process has exited, exactly once
	// (subsequent calls return the same result) — the real subprocess case
	// wraps exec.Cmd.Wait behind a sync.Once; tests substitute a fake peer
	// with the same one-shot-wait contract. kill forces the peer to exit
	// (used by Close's timeout branch).
	wait func() error
	kill func()

	writeMu sync.Mutex
	nextID  atomic.Int64

	mu      sync.Mutex
	pending map[int64]chan inMessage

	closed   chan struct{}
	closeErr error

	onUpdate func(Update)
	log      *slog.Logger
}

// newClient wires up a Client around an already-established stdin/stdout
// pair — the shared core behind both Spawn (a real subprocess) and this
// package's own tests (an in-memory pipe standing in for one).
func newClient(stdin io.WriteCloser, stdout io.Reader, wait func() error, kill func(), onUpdate func(Update), log *slog.Logger) *Client {
	if log == nil {
		log = slog.Default()
	}
	c := &Client{
		stdin:    stdin,
		wait:     wait,
		kill:     kill,
		pending:  make(map[int64]chan inMessage),
		closed:   make(chan struct{}),
		onUpdate: onUpdate,
		log:      log,
	}
	go c.readLoop(stdout)
	return c
}

// Spawn starts command as a subprocess rooted at workspace and returns a
// Client ready for Initialize. onUpdate is called synchronously on the
// connection's single read goroutine for every session/update notification
// received for the lifetime of the Client — it must not block for long (queue
// work elsewhere if needed), since it holds up processing of the next line
// on the wire (including, e.g., the response to whatever request is
// currently in flight).
func Spawn(command []string, workspace string, onUpdate func(Update), log *slog.Logger) (*Client, error) {
	if len(command) == 0 {
		return nil, fmt.Errorf("acpclient: empty command")
	}
	cmd := exec.Command(command[0], command[1:]...)
	cmd.Dir = workspace
	cmd.Env = os.Environ()
	cmd.Stderr = os.Stderr

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("acpclient: stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("acpclient: stdout pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("acpclient: starting %v: %w", command, err)
	}

	var waitOnce sync.Once
	var waitErr error
	wait := func() error {
		waitOnce.Do(func() { waitErr = cmd.Wait() })
		return waitErr
	}
	kill := func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
	}
	return newClient(stdin, stdout, wait, kill, onUpdate, log), nil
}

// Initialize performs the ACP handshake. Must be called exactly once,
// before NewSession.
func (c *Client) Initialize(ctx context.Context) error {
	params := map[string]any{
		"protocolVersion": protocolVersion,
		"clientCapabilities": map[string]any{
			// Every built-in provider does its own file/terminal I/O
			// natively rather than delegating to the client — advertise
			// exactly that (false/false/false) rather than a capability
			// this client doesn't implement.
			"fs":       map[string]any{"readTextFile": false, "writeTextFile": false},
			"terminal": false,
		},
	}
	_, err := c.call(ctx, "initialize", params)
	if err != nil {
		return fmt.Errorf("acpclient: initialize: %w", err)
	}
	return nil
}

// NewSession opens a new ACP session rooted at cwd (must be absolute) and
// returns its id (used in every subsequent call) plus whatever session-mode
// state the agent reported, if any.
func (c *Client) NewSession(ctx context.Context, cwd string) (sessionID string, modes *SessionModeState, err error) {
	params := map[string]any{
		"cwd":        cwd,
		"mcpServers": []any{},
	}
	raw, err := c.call(ctx, "session/new", params)
	if err != nil {
		return "", nil, fmt.Errorf("acpclient: session/new: %w", err)
	}
	var result struct {
		SessionID string            `json:"sessionId"`
		Modes     *SessionModeState `json:"modes,omitempty"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return "", nil, fmt.Errorf("acpclient: session/new: decoding result: %w", err)
	}
	if result.SessionID == "" {
		return "", nil, fmt.Errorf("acpclient: session/new: empty sessionId in result")
	}
	return result.SessionID, result.Modes, nil
}

// SetMode requests the session switch to modeID (e.g. a permission-bypass
// mode). Best-effort by design — callers should only call this after
// confirming the mode is actually offered (see SessionModeState.Offers) and
// treat a failure as non-fatal, since providers vary in whether/how they
// support this.
func (c *Client) SetMode(ctx context.Context, sessionID, modeID string) error {
	_, err := c.call(ctx, "session/set_mode", map[string]any{
		"sessionId": sessionID,
		"modeId":    modeID,
	})
	if err != nil {
		return fmt.Errorf("acpclient: session/set_mode: %w", err)
	}
	return nil
}

// Usage is session/prompt's per-turn token accounting — ACP's
// PromptResponse.usage (schema.unstable.json gates it "UNSTABLE", but
// goose populates it unconditionally; other ACP agents may or may not).
// Reflects only THIS turn's tokens, not a running session total — nil when
// the agent reported none for this turn at all. Mirrors
// services/agent-runner/internal/acp.Usage field-for-field so both
// services persist the identical {input_tokens, output_tokens,
// total_tokens} shape into a 'turn_usage' event.
type Usage struct {
	TotalTokens  int64 `json:"totalTokens"`
	InputTokens  int64 `json:"inputTokens"`
	OutputTokens int64 `json:"outputTokens"`
}

// Prompt sends one turn's message and blocks until the agent's response
// arrives (after streaming zero or more session/update notifications to
// OnUpdate). Returns the turn's stopReason ("end_turn", "cancelled", ...)
// and, when the agent reported one, this turn's own token usage — see
// Usage's doc comment.
// A context cancellation or a call to Cancel unblocks this the same way: by
// the agent eventually returning a real response — see Cancel's doc comment.
func (c *Client) Prompt(ctx context.Context, sessionID, text string) (stopReason string, usage *Usage, err error) {
	params := map[string]any{
		"sessionId": sessionID,
		"prompt":    []map[string]any{{"type": "text", "text": text}},
	}
	raw, err := c.call(ctx, "session/prompt", params)
	if err != nil {
		return "", nil, fmt.Errorf("acpclient: session/prompt: %w", err)
	}
	var result struct {
		StopReason string `json:"stopReason"`
		Usage      *Usage `json:"usage,omitempty"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return "", nil, fmt.Errorf("acpclient: session/prompt: decoding result: %w", err)
	}
	return result.StopReason, result.Usage, nil
}

// Cancel sends session/cancel — a notification, not a request. Per the ACP
// spec this asks the agent to stop promptly; the in-flight Prompt call is
// expected to still return normally afterwards, with stopReason
// "cancelled", not via an abrupt disconnect. Safe to call while a Prompt
// call from another goroutine is in flight; callers that need a hard
// deadline (in case a provider doesn't honor cancellation) should race this
// against their own timeout and fall back to Close.
func (c *Client) Cancel(sessionID string) error {
	if err := c.writeLine(outNotification{
		JSONRPC: "2.0",
		Method:  "session/cancel",
		Params:  map[string]any{"sessionId": sessionID},
	}); err != nil {
		return fmt.Errorf("acpclient: session/cancel: %w", err)
	}
	return nil
}

// Close closes the subprocess's stdin (its usual cue to exit) and waits up
// to closeTimeout for it to do so, killing it outright if it doesn't. Safe
// to call more than once and safe to call while a Prompt call is blocked
// elsewhere — that call will observe the connection closing and return an
// error.
func (c *Client) Close() error {
	_ = c.stdin.Close()
	done := make(chan error, 1)
	go func() { done <- c.wait() }()
	select {
	case err := <-done:
		return err
	case <-time.After(closeTimeout):
		c.kill()
		return <-done
	}
}

// call sends a request and blocks for its matching response.
func (c *Client) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	id := c.nextID.Add(1)
	ch := make(chan inMessage, 1)
	c.mu.Lock()
	c.pending[id] = ch
	c.mu.Unlock()

	if err := c.writeLine(outRequest{JSONRPC: "2.0", ID: id, Method: method, Params: params}); err != nil {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return nil, err
	}

	select {
	case msg := <-ch:
		if msg.Error != nil {
			return nil, msg.Error
		}
		return msg.Result, nil
	case <-ctx.Done():
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return nil, ctx.Err()
	case <-c.closed:
		if c.closeErr != nil {
			return nil, fmt.Errorf("connection closed: %w", c.closeErr)
		}
		return nil, fmt.Errorf("connection closed")
	}
}

func (c *Client) writeLine(v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("encoding: %w", err)
	}
	b = append(b, '\n')
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	if _, err := c.stdin.Write(b); err != nil {
		return fmt.Errorf("writing: %w", err)
	}
	return nil
}

func (c *Client) writeResponse(id json.RawMessage, result any, rpcErr *RPCError) {
	if err := c.writeLine(outResponse{JSONRPC: "2.0", ID: id, Result: result, Error: rpcErr}); err != nil {
		c.log.Warn("acpclient: failed to write response", "error", err)
	}
}

// readLoop is the connection's single reader — every incoming line, in
// either direction of meaning (responses to our requests, notifications,
// requests the agent makes of us), is decoded here and routed accordingly.
// Exits when the subprocess closes stdout (normal exit or crash) or the
// stream otherwise errors.
func (c *Client) readLoop(stdout io.Reader) {
	defer func() {
		close(c.closed)
		go func() { _ = c.wait() }() // reap the process so it doesn't become a zombie even if Close is never called
	}()

	scanner := bufio.NewScanner(stdout)
	// A tool call's output (or a large diff) can comfortably exceed
	// bufio.Scanner's 64KiB default line limit; raise it well above
	// anything a single ACP line should plausibly need.
	scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var msg inMessage
		if err := json.Unmarshal(line, &msg); err != nil {
			c.log.Warn("acpclient: malformed line from subprocess", "error", err)
			continue
		}

		switch {
		case msg.isResponse():
			id, ok := parseID(msg.ID)
			if !ok {
				continue
			}
			c.mu.Lock()
			ch, found := c.pending[id]
			if found {
				delete(c.pending, id)
			}
			c.mu.Unlock()
			if found {
				ch <- msg
				close(ch)
			}

		case msg.isRequest():
			// Handled off the read loop so a slow write back (or a future
			// handler that does real work) never delays draining the next
			// line — most importantly, the session/update notifications
			// that keep streaming while a permission request is pending.
			go c.handleIncomingRequest(msg)

		case msg.isNotification():
			c.handleNotification(msg)
		}
	}
	c.closeErr = scanner.Err()
}

func parseID(raw json.RawMessage) (int64, bool) {
	var id int64
	if err := json.Unmarshal(raw, &id); err != nil {
		return 0, false
	}
	return id, true
}

func (c *Client) handleNotification(msg inMessage) {
	if msg.Method != "session/update" {
		return
	}
	var params sessionUpdateParams
	if err := json.Unmarshal(msg.Params, &params); err != nil {
		c.log.Warn("acpclient: malformed session/update", "error", err)
		return
	}
	if c.onUpdate != nil {
		c.onUpdate(Update{SessionID: params.SessionID, Kind: params.Update.Kind, Raw: params.Update.Raw})
	}
}

// permissionOption is the subset of PermissionOption this client needs.
type permissionOption struct {
	OptionID string `json:"optionId"`
}

type requestPermissionParams struct {
	Options []permissionOption `json:"options"`
}

func (c *Client) handleIncomingRequest(msg inMessage) {
	switch msg.Method {
	case "session/request_permission":
		var params requestPermissionParams
		_ = json.Unmarshal(msg.Params, &params)
		// Always auto-approve the first offered option ("usually allow
		// once") — there is no user in the loop for a headless daemon, and
		// the alternative is the agent hanging forever waiting for a
		// decision. Mirrors OpenHands SDK's ACPAgent.request_permission,
		// the reference implementation the old bridge relied on for this
		// exact behavior.
		optionID := "allow_once"
		if len(params.Options) > 0 {
			optionID = params.Options[0].OptionID
		}
		c.writeResponse(msg.ID, map[string]any{
			"outcome": map[string]any{"outcome": "selected", "optionId": optionID},
		}, nil)

	default:
		// fs/*, terminal/*, and anything else this client didn't advertise
		// support for in Initialize's clientCapabilities — a well-behaved
		// agent shouldn't call these, but answer with a proper JSON-RPC
		// error rather than leaving it hanging if one does anyway.
		c.writeResponse(msg.ID, nil, &RPCError{Code: -32601, Message: "Method not found: " + msg.Method})
	}
}
