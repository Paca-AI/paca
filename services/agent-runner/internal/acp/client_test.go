package acp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

const testSecret = "spike-secret-test"

func decodeBody(t *testing.T, r *http.Request) rpcRequest {
	t.Helper()
	var req rpcRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		t.Fatalf("decoding request body: %v", err)
	}
	return req
}

// acpMockServer is a minimal server-side mock of the older async ACP HTTP
// transport used by the compatibility tests in this file: POST /acp
// enqueues a frame (session/new's onto the connection stream, everything
// else's onto its session stream) that GET /acp then delivers over SSE —
// mirroring exactly the split Client itself expects (see client.go's
// package doc comment on why: `initialize` alone gets a synchronous JSON
// body, every other call gets a bare 202 and its real response arrives
// asynchronously). Just enough to drive Client's real request/response
// correlation logic, not a reimplementation of goose's own server.
type acpMockServer struct {
	t *testing.T

	connID string

	mu       sync.Mutex
	connCh   chan string
	sessions map[string]chan string

	// onInitialize returns the synchronous response body for POST
	// initialize (still not SSE-framed on this protocol) and its HTTP
	// status.
	onInitialize func(req rpcRequest) (body string, status int)
	// onPost is called for every non-initialize POST — implementations
	// enqueue whatever frame(s) that call should eventually produce via
	// enqueueConn/enqueueSession. hdrSessionID is the request's
	// Acp-Session-Id header, if any.
	onPost func(s *acpMockServer, req rpcRequest, hdrSessionID string)
}

func newACPMockServer(t *testing.T) *acpMockServer {
	return &acpMockServer{
		t:        t,
		connID:   "conn-1",
		connCh:   make(chan string, 512),
		sessions: make(map[string]chan string),
	}
}

func (s *acpMockServer) sessionChan(sessionID string) chan string {
	s.mu.Lock()
	defer s.mu.Unlock()
	ch, ok := s.sessions[sessionID]
	if !ok {
		ch = make(chan string, 512)
		s.sessions[sessionID] = ch
	}
	return ch
}

func (s *acpMockServer) enqueueConn(raw string) { s.connCh <- raw }
func (s *acpMockServer) enqueueSession(sessionID, raw string) {
	s.sessionChan(sessionID) <- raw
}

func (s *acpMockServer) handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			s.handlePost(w, r)
		case http.MethodGet:
			s.handleGet(w, r)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}
}

func (s *acpMockServer) handlePost(w http.ResponseWriter, r *http.Request) {
	if got := r.Header.Get("X-Secret-Key"); got != testSecret {
		s.t.Errorf("POST X-Secret-Key = %q, want %q", got, testSecret)
	}
	if got := r.Header.Get("Accept"); got != acpAccept {
		s.t.Errorf("POST Accept = %q, want %q", got, acpAccept)
	}
	req := decodeBody(s.t, r)
	if req.Method == "initialize" {
		if got := r.Header.Get("Acp-Connection-Id"); got != "" {
			s.t.Errorf("initialize must not send Acp-Connection-Id — it's the response that establishes one")
		}
		body, status := s.onInitialize(req)
		w.Header().Set("Acp-Connection-Id", s.connID)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
		return
	}
	if got := r.Header.Get("Acp-Connection-Id"); got != s.connID {
		s.t.Errorf("POST %s Acp-Connection-Id = %q, want %q", req.Method, got, s.connID)
	}
	if s.onPost != nil {
		s.onPost(s, req, r.Header.Get("Acp-Session-Id"))
	}
	w.WriteHeader(http.StatusAccepted)
}

func (s *acpMockServer) handleGet(w http.ResponseWriter, r *http.Request) {
	if got := r.Header.Get("Acp-Connection-Id"); got != s.connID {
		s.t.Errorf("GET Acp-Connection-Id = %q, want %q", got, s.connID)
	}
	sessionID := r.Header.Get("Acp-Session-Id")
	var ch chan string
	if sessionID != "" {
		ch = s.sessionChan(sessionID)
	} else {
		ch = s.connCh
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(http.StatusOK)
	flusher, ok := w.(http.Flusher)
	if !ok {
		s.t.Fatal("httptest ResponseWriter does not implement http.Flusher")
	}
	flusher.Flush()
	for {
		select {
		case msg := <-ch:
			fmt.Fprintf(w, "data: %s\n\n", msg)
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}

// standardSessionNew replies to session/new with sessionID immediately —
// shared by every Prompt test, all of which need a real session
// established first: the legacy Prompt path requires NewSession to have
// already opened this session's own SSE stream.
func standardSessionNew(sessionID string) func(s *acpMockServer, req rpcRequest, hdrSessionID string) {
	return func(s *acpMockServer, req rpcRequest, hdrSessionID string) {
		if req.Method != "session/new" {
			return
		}
		s.enqueueConn(fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"result":{"sessionId":%q}}`, req.ID, sessionID))
	}
}

func initializeOK(req rpcRequest) (string, int) {
	return fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"result":{}}`, req.ID), http.StatusOK
}

func TestInitialize(t *testing.T) {
	srv := newACPMockServer(t)
	srv.onInitialize = func(req rpcRequest) (string, int) {
		if req.Method != "initialize" {
			t.Fatalf("method = %q, want initialize", req.Method)
		}
		// Captured verbatim from the spike, re-verified against goose 1.46.0.
		return fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"result":{"protocolVersion":1,"agentCapabilities":{"loadSession":true,"promptCapabilities":{"image":true,"audio":false,"embeddedContext":true},"mcpCapabilities":{"http":true,"sse":false},"sessionCapabilities":{"list":{},"close":{}},"auth":{}},"authMethods":[{"id":"goose-provider","name":"Configure Provider","description":"Run `+"`"+`goose configure`+"`"+` to set up your AI provider and API key"}]}}`, req.ID), http.StatusOK
	}
	ts := httptest.NewServer(srv.handler())
	defer ts.Close()

	c := NewClient(ts.URL, testSecret, nil)
	defer c.Close()
	if err := c.Initialize(context.Background()); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	if c.connectionID != "conn-1" {
		t.Errorf("connectionID = %q, want the Acp-Connection-Id from the response header", c.connectionID)
	}
}

func TestStreamableHTTPInlineSSEFlow(t *testing.T) {
	const (
		transportSessionID = "transport-1"
		conversationID     = "conversation-1"
	)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("X-Secret-Key"); got != testSecret {
			t.Errorf("X-Secret-Key = %q, want %q", got, testSecret)
		}
		if got := r.Header.Get("Accept"); got != acpAccept {
			t.Errorf("Accept = %q, want %q", got, acpAccept)
		}
		req := decodeBody(t, r)
		if req.Method == "initialize" {
			if got := r.Header.Get("Acp-Session-Id"); got != "" {
				t.Errorf("initialize Acp-Session-Id = %q, want empty", got)
			}
			w.Header().Set("Acp-Session-Id", transportSessionID)
		} else if got := r.Header.Get("Acp-Session-Id"); got != transportSessionID {
			t.Errorf("%s Acp-Session-Id = %q, want transport id %q", req.Method, got, transportSessionID)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		switch req.Method {
		case "initialize":
			fmt.Fprintf(w, "data: {\"jsonrpc\":\"2.0\",\"id\":%d,\"result\":{}}\n\n", req.ID)
		case "session/new":
			fmt.Fprintf(w, "data: {\"jsonrpc\":\"2.0\",\"id\":%d,\"result\":{\"sessionId\":%q}}\n\n", req.ID, conversationID)
		case "session/prompt":
			fmt.Fprintf(w, "data: {\"jsonrpc\":\"2.0\",\"method\":\"session/update\",\"params\":{\"sessionId\":%q,\"update\":{\"sessionUpdate\":\"agent_message_chunk\",\"content\":{\"type\":\"text\",\"text\":\"inline-sse-ok\"}}}}\n\n", conversationID)
			fmt.Fprintf(w, "data: {\"jsonrpc\":\"2.0\",\"id\":%d,\"result\":{\"stopReason\":\"end_turn\",\"usage\":{\"totalTokens\":9,\"inputTokens\":5,\"outputTokens\":4}}}\n\n", req.ID)
		default:
			t.Errorf("unexpected method %q", req.Method)
		}
	}))
	defer ts.Close()

	c := NewClient(ts.URL, testSecret, nil)
	defer c.Close()
	if err := c.Initialize(context.Background()); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	if !c.inlineSSE || c.transportSessionID != transportSessionID {
		t.Fatalf("transport = inline:%t session:%q, want inline session %q", c.inlineSSE, c.transportSessionID, transportSessionID)
	}
	sessionID, err := c.NewSession(context.Background(), "/home/goose", nil)
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	if sessionID != conversationID {
		t.Fatalf("sessionID = %q, want %q", sessionID, conversationID)
	}

	var events []Event
	stopReason, usage, err := c.Prompt(context.Background(), sessionID, []ContentBlock{TextBlock("hello")}, 0, func(event Event) {
		events = append(events, event)
	})
	if err != nil {
		t.Fatalf("Prompt: %v", err)
	}
	if stopReason != "end_turn" {
		t.Errorf("stopReason = %q, want end_turn", stopReason)
	}
	if usage == nil || usage.TotalTokens != 9 || usage.InputTokens != 5 || usage.OutputTokens != 4 {
		t.Errorf("usage = %+v, want {9 5 4}", usage)
	}
	if len(events) != 1 || events[0].Kind != UpdateAgentMessageChunk {
		t.Fatalf("events = %+v, want one agent_message_chunk", events)
	}
}

func TestNewSession_MissingProvider(t *testing.T) {
	srv := newACPMockServer(t)
	srv.onInitialize = initializeOK
	srv.onPost = func(s *acpMockServer, req rpcRequest, hdrSessionID string) {
		if req.Method != "session/new" {
			return
		}
		// Captured verbatim: session/new against a container with no
		// GOOSE_PROVIDER configured.
		s.enqueueConn(fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"error":{"code":-32603,"message":"Internal error","data":"Failed to set provider: Could not configure agent: missing provider"}}`, req.ID))
	}
	ts := httptest.NewServer(srv.handler())
	defer ts.Close()

	c := NewClient(ts.URL, testSecret, nil)
	defer c.Close()
	if err := c.Initialize(context.Background()); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	_, err := c.NewSession(context.Background(), "/home/goose", nil)
	if err == nil {
		t.Fatal("NewSession: want an error for a container with no provider configured, got nil")
	}
}

func TestNewSession_Success(t *testing.T) {
	srv := newACPMockServer(t)
	srv.onInitialize = initializeOK
	srv.onPost = func(s *acpMockServer, req rpcRequest, hdrSessionID string) {
		if req.Method != "session/new" {
			return
		}
		var params NewSessionParams
		raw, _ := json.Marshal(req.Params)
		_ = json.Unmarshal(raw, &params)
		if params.Cwd != "/home/goose" {
			t.Errorf("session/new cwd = %q, want /home/goose", params.Cwd)
		}
		if len(params.MCPServers) != 0 {
			t.Errorf("session/new mcpServers = %+v, want empty — real servers travel in _meta.enabledExtensions", params.MCPServers)
		}
		if params.Meta == nil || len(params.Meta.EnabledExtensions) != 1 {
			t.Fatalf("session/new _meta.enabledExtensions = %+v, want exactly one entry for a call with no mcp servers", params.Meta)
		}
		if got := params.Meta.EnabledExtensions[0]; got.Type != "platform" || got.Name != "skills" {
			t.Errorf("session/new _meta.enabledExtensions[0] = %+v, want {platform skills}", got)
		}
		// Captured verbatim (trimmed of the unused configOptions block).
		s.enqueueConn(fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"result":{"sessionId":"20260810_1","modes":{"currentModeId":"auto"},"models":{"currentModelId":"fake-model"}}}`, req.ID))
	}
	ts := httptest.NewServer(srv.handler())
	defer ts.Close()

	c := NewClient(ts.URL, testSecret, nil)
	defer c.Close()
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

// TestNewSession_StdioServerEnvNeverTravelsInline is the regression guard
// for the actual security-relevant property this conversion depends on:
// a stdio MCPServerConfig's env VALUES (e.g. a real PACA_API_KEY) must
// never appear in the request body at all, only the variable NAMES via
// envKeys — goose itself enforces this server-side (rejects any inline env
// value reaching it through _meta.enabledExtensions), but this asserts the
// client never even attempts to send one, and that the "paca" server's
// secret survives the round trip as a name, not a value, callers can
// inspect on the wire.
func TestNewSession_StdioServerEnvNeverTravelsInline(t *testing.T) {
	const secretValue = "sk-super-secret-do-not-leak"
	srv := newACPMockServer(t)
	srv.onInitialize = initializeOK
	var capturedBody string
	srv.onPost = func(s *acpMockServer, req rpcRequest, hdrSessionID string) {
		if req.Method != "session/new" {
			return
		}
		raw, _ := json.Marshal(req)
		capturedBody = string(raw)

		var params NewSessionParams
		paramsRaw, _ := json.Marshal(req.Params)
		_ = json.Unmarshal(paramsRaw, &params)
		if params.Meta == nil || len(params.Meta.EnabledExtensions) != 2 {
			t.Fatalf("enabledExtensions = %+v, want [skills, paca]", params.Meta)
		}
		mcpExt := params.Meta.EnabledExtensions[1]
		if mcpExt.Type != "mcp" || mcpExt.Server == nil || mcpExt.Server.Name != "paca" {
			t.Fatalf("second extension = %+v, want the paca mcp server", mcpExt)
		}
		if mcpExt.Server.Env == nil || len(*mcpExt.Server.Env) != 0 {
			t.Errorf("Server.Env = %v, want a present-but-empty array", mcpExt.Server.Env)
		}
		if len(mcpExt.EnvKeys) != 1 || mcpExt.EnvKeys[0] != "PACA_API_KEY" {
			t.Errorf("EnvKeys = %v, want [PACA_API_KEY]", mcpExt.EnvKeys)
		}
		s.enqueueConn(fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"result":{"sessionId":"s1"}}`, req.ID))
	}
	ts := httptest.NewServer(srv.handler())
	defer ts.Close()

	c := NewClient(ts.URL, testSecret, nil)
	defer c.Close()
	if err := c.Initialize(context.Background()); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	env := []EnvVariable{{Name: "PACA_API_KEY", Value: secretValue}}
	pacaServer := MCPServerConfig{
		Type:    McpServerStdio,
		Name:    "paca",
		Command: "/usr/bin/paca",
		Args:    &[]string{},
		Env:     &env,
	}
	if _, err := c.NewSession(context.Background(), "/home/goose", []MCPServerConfig{pacaServer}); err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	if strings.Contains(capturedBody, secretValue) {
		t.Errorf("session/new request body contained the raw secret value:\n%s", capturedBody)
	}
}

func TestPrompt_AgentMessageChunk(t *testing.T) {
	const sessionID = "20260810_1"
	srv := newACPMockServer(t)
	srv.onInitialize = initializeOK
	newSession := standardSessionNew(sessionID)
	srv.onPost = func(s *acpMockServer, req rpcRequest, hdrSessionID string) {
		newSession(s, req, hdrSessionID)
		if req.Method != "session/prompt" {
			return
		}
		// Captured verbatim.
		s.enqueueSession(sessionID, `{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"20260810_1","update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"Spike confirmed: ACP session/prompt round-trip through goose serve works."}}}}`)
		s.enqueueSession(sessionID, fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"result":{"stopReason":"end_turn"}}`, req.ID))
	}
	ts := httptest.NewServer(srv.handler())
	defer ts.Close()

	c := NewClient(ts.URL, testSecret, nil)
	defer c.Close()
	if err := c.Initialize(context.Background()); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	if _, err := c.NewSession(context.Background(), "/home/goose", nil); err != nil {
		t.Fatalf("NewSession: %v", err)
	}

	var events []Event
	stopReason, _, err := c.Prompt(context.Background(), sessionID, []ContentBlock{TextBlock("hi")}, 0, func(e Event) {
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
	const sessionID = "20260810_1"
	srv := newACPMockServer(t)
	srv.onInitialize = initializeOK
	newSession := standardSessionNew(sessionID)
	srv.onPost = func(s *acpMockServer, req rpcRequest, hdrSessionID string) {
		newSession(s, req, hdrSessionID)
		if req.Method != "session/prompt" {
			return
		}
		// Captured verbatim, post-cwd-fix (real "completed" shell run).
		s.enqueueSession(sessionID, `{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"20260810_1","update":{"sessionUpdate":"tool_call","toolCallId":"call_fake_1","title":"Developer: Shell"}}}`)
		s.enqueueSession(sessionID, `{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"20260810_1","update":{"sessionUpdate":"tool_call_update","toolCallId":"call_fake_1","status":"completed","content":[{"type":"content","content":{"type":"text","text":"hello-from-goose-acp-spike"}}]}}}`)
		s.enqueueSession(sessionID, fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"result":{"stopReason":"end_turn"}}`, req.ID))
	}
	ts := httptest.NewServer(srv.handler())
	defer ts.Close()

	c := NewClient(ts.URL, testSecret, nil)
	defer c.Close()
	if err := c.Initialize(context.Background()); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	if _, err := c.NewSession(context.Background(), "/home/goose", nil); err != nil {
		t.Fatalf("NewSession: %v", err)
	}

	var events []Event
	stopReason, _, err := c.Prompt(context.Background(), sessionID, []ContentBlock{TextBlock("run echo")}, 5, func(e Event) {
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

// TestPrompt_UsageUpdateAndPromptResponseUsage covers the two usage-carrying
// wire shapes handler.Handler relies on for token/cost accounting: a
// "usage_update" session/update notification (forwarded to onEvent like any
// other notification kind — see UpdateUsage's doc comment on why its Cost is
// session-cumulative) and session/prompt's own terminal result carrying a
// "usage" object (this-turn-only tokens — see promptResult.Usage's doc
// comment). Shapes below mirror ACP's real schema field names, not guessed.
func TestPrompt_UsageUpdateAndPromptResponseUsage(t *testing.T) {
	const sessionID = "s"
	srv := newACPMockServer(t)
	srv.onInitialize = initializeOK
	newSession := standardSessionNew(sessionID)
	srv.onPost = func(s *acpMockServer, req rpcRequest, hdrSessionID string) {
		newSession(s, req, hdrSessionID)
		if req.Method != "session/prompt" {
			return
		}
		s.enqueueSession(sessionID, `{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"s","update":{"sessionUpdate":"usage_update","used":1200,"size":128000,"cost":{"amount":0.0034,"currency":"USD"}}}}`)
		s.enqueueSession(sessionID, fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"result":{"stopReason":"end_turn","usage":{"totalTokens":120,"inputTokens":80,"outputTokens":40}}}`, req.ID))
	}
	ts := httptest.NewServer(srv.handler())
	defer ts.Close()

	c := NewClient(ts.URL, testSecret, nil)
	defer c.Close()
	if err := c.Initialize(context.Background()); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	if _, err := c.NewSession(context.Background(), "/home/goose", nil); err != nil {
		t.Fatalf("NewSession: %v", err)
	}

	var events []Event
	stopReason, usage, err := c.Prompt(context.Background(), sessionID, []ContentBlock{TextBlock("hi")}, 0, func(e Event) {
		events = append(events, e)
	})
	if err != nil {
		t.Fatalf("Prompt: %v", err)
	}
	if stopReason != "end_turn" {
		t.Errorf("stopReason = %q, want end_turn", stopReason)
	}

	if len(events) != 1 || events[0].Kind != UpdateUsage {
		t.Fatalf("events = %+v, want exactly one usage_update", events)
	}
	var update UsageUpdate
	if err := json.Unmarshal(events[0].Raw, &update); err != nil {
		t.Fatalf("decoding UsageUpdate: %v", err)
	}
	if update.Used != 1200 || update.Size != 128000 {
		t.Errorf("UsageUpdate.{Used,Size} = {%d,%d}, want {1200,128000}", update.Used, update.Size)
	}
	if update.Cost == nil || update.Cost.Amount != 0.0034 || update.Cost.Currency != "USD" {
		t.Errorf("UsageUpdate.Cost = %+v, want {0.0034 USD}", update.Cost)
	}

	if usage == nil {
		t.Fatal("Prompt returned nil usage, want promptResult.usage decoded")
	}
	if usage.TotalTokens != 120 || usage.InputTokens != 80 || usage.OutputTokens != 40 {
		t.Errorf("usage = %+v, want {120 80 40}", usage)
	}
}

// TestPrompt_MaxToolCallsExceeded reproduces the spike's runaway-loop
// scenario (a non-converging scripted reply produced 600+ tool-call cycles
// with no server-side cap) and asserts the client's own limit actually cuts
// the turn off instead of reading forever.
func TestPrompt_MaxToolCallsExceeded(t *testing.T) {
	const sessionID = "s"
	srv := newACPMockServer(t)
	srv.onInitialize = initializeOK
	newSession := standardSessionNew(sessionID)
	srv.onPost = func(s *acpMockServer, req rpcRequest, hdrSessionID string) {
		newSession(s, req, hdrSessionID)
		if req.Method != "session/prompt" {
			return
		}
		// Never sends a terminal response — same shape as the spike's
		// non-converging loop. The client must give up on its own.
		for range 50 {
			s.enqueueSession(sessionID, `{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"s","update":{"sessionUpdate":"tool_call","toolCallId":"call_fake_1","title":"Developer: Shell"}}}`)
			s.enqueueSession(sessionID, `{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"s","update":{"sessionUpdate":"tool_call_update","toolCallId":"call_fake_1","status":"completed","content":[]}}}`)
		}
	}
	ts := httptest.NewServer(srv.handler())
	defer ts.Close()

	c := NewClient(ts.URL, testSecret, nil)
	defer c.Close()
	if err := c.Initialize(context.Background()); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	if _, err := c.NewSession(context.Background(), "/home/goose", nil); err != nil {
		t.Fatalf("NewSession: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	toolCalls := 0
	_, _, err := c.Prompt(ctx, sessionID, []ContentBlock{TextBlock("loop forever")}, 3, func(e Event) {
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
// defaultTimeoutMinutes doc comment): a server that never sends a terminal
// response on the session stream reproduces, in miniature, the real bug
// found while building the sandbox image — a wrong mcpServers wire format
// made a real goose serve's session/new hang forever rather than return an
// ACP error. Confirms Prompt actually returns once the caller's context
// expires, instead of blocking for however long the server chooses never to
// respond.
func TestPrompt_RespectsContextDeadlineOnAHungServer(t *testing.T) {
	const sessionID = "s"
	srv := newACPMockServer(t)
	srv.onInitialize = initializeOK
	srv.onPost = standardSessionNew(sessionID)
	// No "session/prompt" case at all: the session's SSE stream (already
	// opened by NewSession, before this Prompt call even starts) accepts
	// the connection and then never delivers anything — the server-side
	// shape of the real hang, independent of whatever upstream cause
	// produced it that day.
	ts := httptest.NewServer(srv.handler())
	defer ts.Close()

	c := NewClient(ts.URL, testSecret, nil)
	defer c.Close()
	if err := c.Initialize(context.Background()); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	if _, err := c.NewSession(context.Background(), "/home/goose", nil); err != nil {
		t.Fatalf("NewSession: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, _, err := c.Prompt(ctx, sessionID, []ContentBlock{TextBlock("hello")}, 0, nil)
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
// consumed the first one, and — if ctx is only checked when the frame
// source itself signals an error, not on every loop iteration —
// cancelling mid-backlog had no effect until every already-buffered frame
// was processed first. Against the real server, with onEvent's two
// blocking Redis calls per event, that turned a "stop" button press into a
// ~30s delay. This confirms Prompt actually bails out between frames
// instead.
func TestPrompt_CancelledMidBacklogReturnsPromptly(t *testing.T) {
	const sessionID = "s"
	const backlogSize = 300
	srv := newACPMockServer(t)
	srv.onInitialize = initializeOK
	newSession := standardSessionNew(sessionID)
	srv.onPost = func(s *acpMockServer, req rpcRequest, hdrSessionID string) {
		newSession(s, req, hdrSessionID)
		if req.Method != "session/prompt" {
			return
		}
		// Enqueued as fast as this handler can push them — by the time a
		// slow-consuming client reads its first frame, most or all of this
		// is already sitting in the client's own channel buffer, not
		// pending on the network. No terminal response ever follows.
		for range backlogSize {
			s.enqueueSession(sessionID, `{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"s","update":{"sessionUpdate":"tool_call","toolCallId":"call_fake_1","title":"Developer: Shell"}}}`)
		}
	}
	ts := httptest.NewServer(srv.handler())
	defer ts.Close()

	c := NewClient(ts.URL, testSecret, nil)
	defer c.Close()
	if err := c.Initialize(context.Background()); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	if _, err := c.NewSession(context.Background(), "/home/goose", nil); err != nil {
		t.Fatalf("NewSession: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	// A slow onEvent, standing in for onEvent's two blocking Redis calls in
	// the real handler.Handler — see the doc comment above.
	start := time.Now()
	_, _, err := c.Prompt(ctx, sessionID, []ContentBlock{TextBlock("hello")}, 0, func(Event) {
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
