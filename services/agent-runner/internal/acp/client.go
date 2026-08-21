package acp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
)

// Event is what Prompt hands back to its callback for every session/update
// notification received during a turn — the Go analog of services/ai-agent's
// make_event_callback, meant to be forwarded as-is into
// AgentConversationEvent.Payload on the paca:agent:events stream.
type Event struct {
	Kind SessionUpdateKind
	Raw  json.RawMessage // the full "update" object, e.g. {"sessionUpdate":"tool_call",...}
}

// ErrMaxToolCalls is returned by Prompt when a turn is cancelled for
// exceeding maxToolCalls. Goose enforces no turn/iteration cap of its own —
// confirmed in the spike, where a non-terminating scripted reply produced
// 600+ tool-call cycles with no backoff in 20 seconds — so this client owns
// that limit, mirroring what agent_config.MaxIterations enforces today via
// OpenHands' conversation.run(max_iterations=...).
var ErrMaxToolCalls = errors.New("acp: exceeded max tool calls for this turn")

// Client is a single conversation's connection to one container's
// `goose serve --host 0.0.0.0 --port <p>` instance.
//
// Not safe for concurrent use — mirrors the one-container-per-conversation
// model already in place for OpenHands (see docker_workspace.py), so
// there's exactly one caller driving one Client for the container's
// lifetime.
//
// Current pinned Goose uses the official Streamable HTTP shape: every POST
// returns an SSE body containing zero or more notifications followed by the
// matching JSON-RPC response, while Acp-Session-Id identifies the transport
// session established by initialize. Frames are correlated by JSON-RPC id.
// This was verified against a real container, not inferred from status codes.
//
// The connectionID and background GET streams below preserve compatibility
// with the older Goose hybrid transport, where initialize returned JSON plus
// Acp-Connection-Id and later calls returned 202 while their frames arrived
// on long-lived connection/session GET streams. Close tears those legacy
// streams down; current inline-SSE calls close each POST body as soon as its
// terminal response is observed.
type Client struct {
	baseURL    string
	secretKey  string
	httpClient *http.Client

	// connectionID is the Acp-Connection-Id returned by initialize's
	// response header — required on every following request and on the
	// connection-scoped SSE stream. Distinct from the ACP-level sessionId
	// returned by session/new; see the migration doc's "two distinct
	// session concepts" note.
	connectionID string
	// inlineSSE is the current official Streamable HTTP shape used by pinned
	// Goose: each POST returns its JSON-RPC frames in that response's SSE body
	// and Acp-Session-Id identifies the transport session. connectionID and
	// the background GET streams remain as compatibility for older servers.
	inlineSSE          bool
	transportSessionID string
	// sessionID/sessionStream are set once, by NewSession — this Client
	// only ever drives exactly one ACP session for its container's whole
	// lifetime (including across resumed chat turns), never session/fork or
	// multiple concurrent sessions, so a single field each is enough; no
	// need for the map-of-sessions generality the reference client needs.
	sessionID     string
	sessionStream *frameStream

	// connStream is the connection-scoped SSE stream established by
	// Initialize — carries session/new's response (and, in principle, any
	// other connection-scoped traffic, though this Client never sends any).
	connStream *frameStream

	// streamCtx/cancelStreams bound both background streams' lifetime —
	// deliberately independent of any single turn's context (which ends
	// when that turn's Prompt call returns), since the streams must survive
	// across every turn of a chat conversation reattaching to this same
	// Client. Close cancels it, ending both streams' underlying GET
	// requests.
	streamCtx     context.Context
	cancelStreams context.CancelFunc

	nextID atomic.Int64
}

// The official ACP HTTP transport follows Streamable HTTP negotiation: every
// POST must advertise both response media types. Current Goose answers with
// SSE; older compatible servers may answer initialize with JSON and later
// calls with 202. Newer Goose rejects application/json-only clients with 406.
const acpAccept = "application/json, text/event-stream"

// NewClient builds a client for a container reachable at baseURL (e.g.
// "http://172.18.0.5:3284"), authenticated with the same secret passed to
// that container as GOOSE_SERVER__SECRET_KEY.
func NewClient(baseURL, secretKey string, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	streamCtx, cancel := context.WithCancel(context.Background())
	return &Client{
		baseURL:       baseURL,
		secretKey:     secretKey,
		httpClient:    httpClient,
		streamCtx:     streamCtx,
		cancelStreams: cancel,
	}
}

// Close ends any legacy background SSE streams. Current inline-SSE response
// bodies are scoped to their calls and already closed on return. Safe to call
// on a nil *Client and safe to call more than once.
func (c *Client) Close() {
	if c == nil {
		return
	}
	c.cancelStreams()
}

// Initialize performs the ACP handshake and captures the transport session.
// Must be called exactly once, before NewSession. It prefers current inline
// SSE and detects the older JSON/Acp-Connection-Id transport for compatibility.
func (c *Client) Initialize(ctx context.Context) error {
	id := c.nextID.Add(1)
	body, err := json.Marshal(rpcRequest{
		JSONRPC: "2.0",
		ID:      id,
		Method:  "initialize",
		Params:  initializeParams{ProtocolVersion: 1, ClientCapabilities: map[string]any{}},
	})
	if err != nil {
		return fmt.Errorf("acp: initialize: encoding request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/acp", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("acp: initialize: building request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", acpAccept)
	req.Header.Set("X-Secret-Key", c.secretKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("acp: initialize: sending request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("acp: initialize: unexpected status %d: %s", resp.StatusCode, msg)
	}

	var frame rpcFrame
	if strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
		frame, err = awaitInlineResponse(ctx, id, resp.Body)
		if err != nil {
			return fmt.Errorf("acp: initialize: %w", err)
		}
		transportSessionID := resp.Header.Get("Acp-Session-Id")
		if transportSessionID == "" {
			return errors.New("acp: initialize response missing Acp-Session-Id header")
		}
		c.inlineSSE = true
		c.transportSessionID = transportSessionID
	} else {
		respBody, readErr := io.ReadAll(resp.Body)
		if readErr != nil {
			return fmt.Errorf("acp: initialize: reading response: %w", readErr)
		}
		if err := json.Unmarshal(respBody, &frame); err != nil {
			return fmt.Errorf("acp: initialize: decoding response: %w", err)
		}
		connectionID := resp.Header.Get("Acp-Connection-Id")
		if connectionID == "" {
			return errors.New("acp: initialize response missing Acp-Connection-Id header")
		}
		c.connectionID = connectionID
	}
	if frame.Error != nil {
		return fmt.Errorf("acp: initialize: %w", frame.Error)
	}
	if !c.inlineSSE {
		c.connStream = c.startStream(c.streamCtx, "")
	}
	return nil
}

// NewSession opens a new ACP conversation session inside the container and
// returns its sessionId, used in every subsequent Prompt call. cwd must be
// a directory the container's own user can access — /root is a common trap
// on the stock goose image (uid 1000, user "goose"); see the migration
// doc's session/new gotcha.
func (c *Client) NewSession(ctx context.Context, cwd string, mcpServers []MCPServerConfig) (string, error) {
	if c.connectionID == "" && !c.inlineSSE {
		return "", errors.New("acp: NewSession called before Initialize")
	}
	id := c.nextID.Add(1)
	params := NewSessionParams{
		Cwd:        cwd,
		MCPServers: []MCPServerConfig{},
		Meta:       &NewSessionMeta{EnabledExtensions: buildEnabledExtensions(mcpServers)},
	}
	var frame rpcFrame
	var err error
	if c.inlineSSE {
		frame, err = c.callInline(ctx, id, "session/new", params)
	} else {
		if err = c.post(ctx, id, "session/new", params, ""); err == nil {
			frame, err = c.awaitResponse(ctx, id, c.connStream)
		}
	}
	if err != nil {
		return "", fmt.Errorf("acp: session/new: %w", err)
	}
	if frame.Error != nil {
		return "", fmt.Errorf("acp: session/new: %w", frame.Error)
	}
	var result newSessionResult
	if err := json.Unmarshal(frame.Result, &result); err != nil {
		return "", fmt.Errorf("acp: session/new: decoding result: %w", err)
	}
	if result.SessionID == "" {
		return "", errors.New("acp: session/new: empty sessionId in result")
	}

	// session/prompt's response and every session/update notification for
	// this session arrive on this session's own stream, never the
	// connection-scoped one — must be established before the first Prompt
	// call, so do it now rather than lazily.
	c.sessionID = result.SessionID
	if !c.inlineSSE {
		c.sessionStream = c.startStream(c.streamCtx, result.SessionID)
	}
	return result.SessionID, nil
}

// buildEnabledExtensions turns mcpServers into session/new's
// _meta.enabledExtensions, always prepending the "skills" platform
// extension — see GooseExtension's doc comment for why both must travel
// together here instead of mcpServers going through the plain top-level
// field.
func buildEnabledExtensions(mcpServers []MCPServerConfig) []GooseExtension {
	extensions := make([]GooseExtension, 0, len(mcpServers)+1)
	extensions = append(extensions, GooseExtension{Type: "platform", Name: "skills"})
	for _, s := range mcpServers {
		if ext, ok := mcpServerToGooseExtension(s); ok {
			extensions = append(extensions, ext)
		}
	}
	return extensions
}

// mcpServerToGooseExtension converts one MCPServerConfig into the "mcp"
// variant of GooseExtension. ok is false for a transport this variant has
// no representation for (see UntaggedMcpServer's doc comment on "sse") —
// the server is dropped rather than sent malformed, matching
// executor.buildMCPServers's own tolerance for skipping an unsupported
// transport ("oauth") rather than erroring.
func mcpServerToGooseExtension(s MCPServerConfig) (ext GooseExtension, ok bool) {
	switch s.Type {
	case McpServerStdio:
		args := []string{}
		if s.Args != nil {
			args = *s.Args
		}
		envKeys := make([]string, 0)
		if s.Env != nil {
			for _, ev := range *s.Env {
				envKeys = append(envKeys, ev.Name)
			}
		}
		return GooseExtension{
			Type: "mcp",
			Server: &UntaggedMcpServer{
				Name:    s.Name,
				Command: s.Command,
				Args:    &args,
				Env:     &[]EnvVariable{},
			},
			EnvKeys: envKeys,
		}, true

	case McpServerHTTP:
		// Headers travel inline with real values, unlike stdio's env — see
		// GooseExtension's doc comment.
		return GooseExtension{
			Type: "mcp",
			Server: &UntaggedMcpServer{
				Name:    s.Name,
				URL:     s.URL,
				Headers: s.Headers,
			},
		}, true

	default:
		return GooseExtension{}, false
	}
}

// Prompt sends one turn's message and streams session/update notifications
// to onEvent as they arrive. Returns the turn's stopReason and, when goose
// reported one, this turn's token usage (see Usage's doc comment — nil on
// any error return, and possibly nil even on success per promptResult.Usage's
// doc comment).
//
// maxToolCalls bounds the number of tool_call notifications this turn may
// receive before Prompt cancels the request and returns ErrMaxToolCalls —
// see ErrMaxToolCalls's doc comment for why this exists at all. Pass 0 for
// no limit (not recommended against a real, paid LLM).
//
// ctx cancellation (e.g. from a user-initiated "stop") stops Prompt from
// waiting any further; callers should treat ctx.Err() coming back through
// the returned error as a clean stop, not a failure. Note this only ends
// *this call's* wait — the underlying session/prompt request already sent
// to goose isn't itself cancelled (this transport has no session/cancel
// call), so a late response or trailing notifications for this same
// request may still arrive on the session stream and are silently ignored
// by the next Prompt call's own wait (see awaitResponse's tolerance for
// non-matching frames).
func (c *Client) Prompt(
	ctx context.Context,
	sessionID string,
	prompt []ContentBlock,
	maxToolCalls int,
	onEvent func(Event),
) (stopReason string, usage *Usage, err error) {
	if c.connectionID == "" && !c.inlineSSE {
		return "", nil, errors.New("acp: Prompt called before Initialize")
	}
	if c.sessionID == "" || (!c.inlineSSE && c.sessionStream == nil) {
		return "", nil, errors.New("acp: Prompt called before NewSession")
	}

	id := c.nextID.Add(1)
	if c.inlineSSE {
		return c.promptInline(ctx, id, sessionID, prompt, maxToolCalls, onEvent)
	}
	if err := c.post(ctx, id, "session/prompt", promptParams{SessionID: sessionID, Prompt: prompt}, sessionID); err != nil {
		return "", nil, fmt.Errorf("acp: session/prompt: %w", err)
	}

	toolCalls := 0
	stream := c.sessionStream
	for {
		// Checked at the top of every iteration, not only when a channel
		// receive itself fails — mirrors the previous single-stream
		// client's identical guard (see its history): a channel receive
		// racing against an already-cancelled ctx isn't guaranteed to pick
		// the ctx.Done() case first if a frame is also immediately
		// available, which previously (against a real non-converging
		// server producing frames faster than onEvent's own blocking Redis
		// publishes could drain them) turned a "stop" press into a ~30s
		// delay.
		if ctx.Err() != nil {
			return "", nil, ctx.Err()
		}

		select {
		case frame, ok := <-stream.Frames:
			if !ok {
				return "", nil, fmt.Errorf("session stream ended before a terminal response for id=%d: %w", id, stream.Err())
			}

			switch {
			case frame.isNotification() && frame.Method == "session/update":
				var note sessionUpdateNotification
				if err := json.Unmarshal(frame.Params, &note); err != nil {
					return "", nil, fmt.Errorf("decoding session/update: %w", err)
				}
				if note.Update.Kind == UpdateToolCall {
					toolCalls++
					if maxToolCalls > 0 && toolCalls > maxToolCalls {
						return "", nil, ErrMaxToolCalls
					}
				}
				if onEvent != nil {
					onEvent(Event{Kind: note.Update.Kind, Raw: note.Update.raw})
				}

			case frame.isResponse() && frame.ID != nil && *frame.ID == id:
				if frame.Error != nil {
					return "", nil, fmt.Errorf("%w", frame.Error)
				}
				var result promptResult
				if err := json.Unmarshal(frame.Result, &result); err != nil {
					return "", nil, fmt.Errorf("decoding result: %w", err)
				}
				return result.StopReason, result.Usage, nil

			default:
				// A response to some other (e.g. an earlier, since-abandoned)
				// request id, or any other unrecognized shape — not expected
				// given this Client drives exactly one request at a time per
				// session, but not fatal either; keep waiting for the
				// response that matters.
			}

		case <-ctx.Done():
			return "", nil, ctx.Err()
		}
	}
}

func (c *Client) promptInline(
	ctx context.Context,
	id int64,
	sessionID string,
	prompt []ContentBlock,
	maxToolCalls int,
	onEvent func(Event),
) (string, *Usage, error) {
	resp, err := c.postInline(ctx, id, "session/prompt", promptParams{SessionID: sessionID, Prompt: prompt})
	if err != nil {
		return "", nil, fmt.Errorf("acp: session/prompt: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	sse := newSSEReader(resp.Body)
	toolCalls := 0
	for {
		frame, readErr := nextInlineFrame(ctx, sse)
		if readErr != nil {
			return "", nil, readErr
		}
		switch {
		case frame.isNotification() && frame.Method == "session/update":
			var note sessionUpdateNotification
			if err := json.Unmarshal(frame.Params, &note); err != nil {
				return "", nil, fmt.Errorf("decoding session/update: %w", err)
			}
			if note.Update.Kind == UpdateToolCall {
				toolCalls++
				if maxToolCalls > 0 && toolCalls > maxToolCalls {
					return "", nil, ErrMaxToolCalls
				}
			}
			if onEvent != nil {
				onEvent(Event{Kind: note.Update.Kind, Raw: note.Update.raw})
			}
		case frame.isResponse() && frame.ID != nil && *frame.ID == id:
			if frame.Error != nil {
				return "", nil, fmt.Errorf("%w", frame.Error)
			}
			var result promptResult
			if err := json.Unmarshal(frame.Result, &result); err != nil {
				return "", nil, fmt.Errorf("decoding result: %w", err)
			}
			return result.StopReason, result.Usage, nil
		}
	}
}

func (c *Client) callInline(ctx context.Context, id int64, method string, params any) (rpcFrame, error) {
	resp, err := c.postInline(ctx, id, method, params)
	if err != nil {
		return rpcFrame{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	return awaitInlineResponse(ctx, id, resp.Body)
}

func (c *Client) postInline(ctx context.Context, id int64, method string, params any) (*http.Response, error) {
	body, err := json.Marshal(rpcRequest{JSONRPC: "2.0", ID: id, Method: method, Params: params})
	if err != nil {
		return nil, fmt.Errorf("encoding request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/acp", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("building request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", acpAccept)
	req.Header.Set("X-Secret-Key", c.secretKey)
	req.Header.Set("Acp-Session-Id", c.transportSessionID)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("sending request: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		defer func() { _ = resp.Body.Close() }()
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("unexpected status %d: %s", resp.StatusCode, msg)
	}
	if !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("unexpected content type %q", resp.Header.Get("Content-Type"))
	}
	return resp, nil
}

func awaitInlineResponse(ctx context.Context, id int64, body io.Reader) (rpcFrame, error) {
	sse := newSSEReader(body)
	for {
		frame, err := nextInlineFrame(ctx, sse)
		if err != nil {
			return rpcFrame{}, err
		}
		if frame.isResponse() && frame.ID != nil && *frame.ID == id {
			return frame, nil
		}
	}
}

func nextInlineFrame(ctx context.Context, sse *sseReader) (rpcFrame, error) {
	if ctx.Err() != nil {
		return rpcFrame{}, ctx.Err()
	}
	data, err := sse.Next()
	if len(data) == 0 && err != nil {
		return rpcFrame{}, err
	}
	var frame rpcFrame
	if jsonErr := json.Unmarshal(data, &frame); jsonErr != nil {
		return rpcFrame{}, fmt.Errorf("decoding SSE frame: %w", jsonErr)
	}
	return frame, nil
}

// post is the older hybrid-transport compatibility path: it sends one
// JSON-RPC request and confirms Goose accepted it while the actual response
// arrives asynchronously on the relevant background stream. The transport
// still requires both JSON and SSE in Accept; see acpAccept.
func (c *Client) post(ctx context.Context, id int64, method string, params any, sessionID string) error {
	body, err := json.Marshal(rpcRequest{JSONRPC: "2.0", ID: id, Method: method, Params: params})
	if err != nil {
		return fmt.Errorf("encoding request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/acp", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("building request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", acpAccept)
	req.Header.Set("X-Secret-Key", c.secretKey)
	req.Header.Set("Acp-Connection-Id", c.connectionID)
	if sessionID != "" {
		req.Header.Set("Acp-Session-Id", sessionID)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("sending request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusAccepted && (resp.StatusCode < 200 || resp.StatusCode >= 300) {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("unexpected status %d: %s", resp.StatusCode, msg)
	}
	return nil
}

// awaitResponse blocks until the frame matching id arrives on stream —
// used only by NewSession, which (unlike Prompt) neither expects nor needs
// to forward any notifications while waiting.
func (c *Client) awaitResponse(ctx context.Context, id int64, stream *frameStream) (rpcFrame, error) {
	for {
		if ctx.Err() != nil {
			return rpcFrame{}, ctx.Err()
		}
		select {
		case frame, ok := <-stream.Frames:
			if !ok {
				return rpcFrame{}, fmt.Errorf("connection stream ended before a response for id=%d: %w", id, stream.Err())
			}
			if frame.isResponse() && frame.ID != nil && *frame.ID == id {
				return frame, nil
			}
			// Not expected on a fresh connection stream with exactly one
			// pending request, but tolerated rather than treated as fatal —
			// same rationale as Prompt's identical default case.
		case <-ctx.Done():
			return rpcFrame{}, ctx.Err()
		}
	}
}

// frameStream reads SSE-framed JSON-RPC messages off a long-lived `GET
// /acp` request in the background, forwarding each decoded frame on
// Frames. Frames is closed once the stream ends for any reason (server
// close, network error, or ctx cancellation) — Err returns why, and is
// only meaningful once Frames has been drained and found closed.
type frameStream struct {
	Frames chan rpcFrame

	done chan struct{}
	err  error
}

// Err returns the reason this stream ended. Only valid to call after
// Frames has been observed closed (reading it any earlier is a data race
// with the goroutine that still owns it).
func (s *frameStream) Err() error {
	<-s.done
	return s.err
}

// startStream opens the connection-scoped (sessionID == "") or
// session-scoped SSE stream and reads it in the background until ctx is
// done or the stream itself ends. goose's ACP HTTP transport allows
// exactly one active subscriber per logical stream — a second concurrent
// GET for the same scope gets 409 Conflict — so callers must not call this
// twice for the same connection/session.
func (c *Client) startStream(ctx context.Context, sessionID string) *frameStream {
	s := &frameStream{Frames: make(chan rpcFrame), done: make(chan struct{})}
	go func() {
		defer close(s.done)
		defer close(s.Frames)

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/acp", nil)
		if err != nil {
			s.err = fmt.Errorf("acp: stream: building request: %w", err)
			return
		}
		req.Header.Set("Accept", "text/event-stream")
		req.Header.Set("X-Secret-Key", c.secretKey)
		req.Header.Set("Acp-Connection-Id", c.connectionID)
		if sessionID != "" {
			req.Header.Set("Acp-Session-Id", sessionID)
		}

		resp, err := c.httpClient.Do(req)
		if err != nil {
			s.err = fmt.Errorf("acp: stream: sending request: %w", err)
			return
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusOK {
			msg, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
			s.err = fmt.Errorf("acp: stream: unexpected status %d: %s", resp.StatusCode, msg)
			return
		}

		sse := newSSEReader(resp.Body)
		for {
			data, readErr := sse.Next()
			if len(data) > 0 {
				var frame rpcFrame
				if jsonErr := json.Unmarshal(data, &frame); jsonErr != nil {
					s.err = fmt.Errorf("acp: stream: decoding frame: %w", jsonErr)
					return
				}
				select {
				case s.Frames <- frame:
				case <-ctx.Done():
					s.err = ctx.Err()
					return
				}
			}
			if readErr != nil {
				switch {
				case errors.Is(readErr, io.EOF):
					s.err = errors.New("acp: stream: closed by server")
				case ctx.Err() != nil:
					s.err = ctx.Err()
				default:
					s.err = fmt.Errorf("acp: stream: reading stream: %w", readErr)
				}
				return
			}
		}
	}()
	return s
}
