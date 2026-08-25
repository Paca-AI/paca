package acp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
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
// Since goose switched its ACP HTTP transport to the official
// agent-client-protocol-http crate (see types.go's doc comment on the
// goose version this was re-verified against), a POST no longer carries its
// own response inline: `initialize` alone still gets a synchronous JSON
// body, but every other call gets back a bare `202 Accepted` and its actual
// response (and, for session/prompt, every session/update notification
// along the way) arrives asynchronously over a *separate*, long-lived SSE
// stream opened via `GET /acp` — one connection-scoped stream (no
// Acp-Session-Id header) established right after Initialize, and one
// session-scoped stream (with Acp-Session-Id) established once NewSession's
// result is observed to carry a sessionId. Frames are correlated back to
// the request that's waiting for them by JSON-RPC id. Verified directly
// against both a real running container and the exact pinned commit of
// github.com/agentclientprotocol/rust-sdk goose depends on (its own
// client.rs and http_server.rs) — not guessed from the "202" status code
// alone.
//
// Both streams are read by a background goroutine each (started in
// Initialize and NewSession respectively) that outlives any single turn —
// necessary because a chat conversation reattaches to the same Client
// across multiple turns (see chatsandbox.State) without calling Initialize
// or NewSession again. Close tears them down; callers must call it once
// this Client is done for good (see internal/handler's tearDownSandbox and
// TeardownPausedChatSandbox).
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
	// supportsLoadSession records initialize's agentCapabilities.loadSession
	// — see LoadSession's own doc comment for why this is checked rather
	// than assumed to be true.
	supportsLoadSession bool
	// sessionID/sessionStream are set once, by NewSession or LoadSession —
	// this Client only ever drives exactly one ACP session for its
	// container's whole lifetime (including across resumed chat turns),
	// never session/fork or multiple concurrent sessions, so a single field
	// each is enough; no need for the map-of-sessions generality the
	// reference client needs.
	sessionID     string
	sessionStream *frameStream
	// cancelSession tears down just sessionStream's underlying GET request
	// — see attachSessionStream's own doc comment for why this Client
	// needs to be able to replace that stream (a failed LoadSession falling
	// back to a fresh NewSession) without cancelStreams' whole-Client
	// teardown.
	cancelSession context.CancelFunc

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

// Close ends this client's background SSE streams. Safe to call on a nil
// *Client (no-op, so callers don't need a separate nil check at every
// teardown call site) and safe to call more than once.
func (c *Client) Close() {
	if c == nil {
		return
	}
	c.cancelStreams()
}

// Initialize performs the ACP handshake and captures the connection id.
// Must be called exactly once, before NewSession. Unlike every other call
// in this package, initialize's response arrives synchronously in this
// POST's own body (still plain JSON, not SSE-framed) — see the package doc
// comment on Client for why every other call differs.
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
	// application/json only, not "application/json, text/event-stream" —
	// initialize's response is never SSE-framed regardless of what's
	// accepted here, and every other call in this package sends the same
	// application/json-only Accept (see post's doc comment).
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Secret-Key", c.secretKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("acp: initialize: sending request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	connectionID := resp.Header.Get("Acp-Connection-Id")

	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("acp: initialize: unexpected status %d: %s", resp.StatusCode, msg)
	}

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("acp: initialize: reading response: %w", err)
	}
	var frame rpcFrame
	if err := json.Unmarshal(respBody, &frame); err != nil {
		return fmt.Errorf("acp: initialize: decoding response: %w", err)
	}
	if frame.Error != nil {
		return fmt.Errorf("acp: initialize: %w", frame.Error)
	}
	if connectionID == "" {
		return errors.New("acp: initialize response missing Acp-Connection-Id header")
	}

	// Best-effort: this package ignored frame.Result entirely before
	// LoadSession needed to know about this one capability, so a decode
	// failure here (an unexpected result shape from some future goose
	// version) falls back to false rather than failing Initialize itself —
	// the caller still gets a working connection, just one LoadSession
	// correctly refuses to use.
	var result initializeResult
	_ = json.Unmarshal(frame.Result, &result)
	c.supportsLoadSession = result.AgentCapabilities.LoadSession

	c.connectionID = connectionID
	c.connStream = c.startStream(c.streamCtx, "")
	return nil
}

// NewSession opens a new ACP conversation session inside the container and
// returns its sessionId, used in every subsequent Prompt call. cwd must be
// a directory the container's own user can access — /root is a common trap
// on the stock goose image (uid 1000, user "goose"); see the migration
// doc's session/new gotcha.
func (c *Client) NewSession(ctx context.Context, cwd string, mcpServers []MCPServerConfig) (string, error) {
	if c.connectionID == "" {
		return "", errors.New("acp: NewSession called before Initialize")
	}
	id := c.nextID.Add(1)
	params := NewSessionParams{
		Cwd:        cwd,
		MCPServers: []MCPServerConfig{},
		Meta:       &NewSessionMeta{EnabledExtensions: buildEnabledExtensions(mcpServers)},
	}
	if err := c.post(ctx, id, "session/new", params, ""); err != nil {
		return "", fmt.Errorf("acp: session/new: %w", err)
	}

	frame, err := c.awaitResponse(ctx, id, c.connStream)
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
	c.attachSessionStream(result.SessionID)
	return result.SessionID, nil
}

// LoadSession resumes sessionID (a value NewSession previously returned,
// persisted by the caller across this Client's own lifetime — see
// executor.Executor.attachEnvironmentSession) instead of starting a blank
// one, giving goose its own conversation memory back across turns rather
// than a fresh, context-less session every time.
//
// Requires goose to have advertised initialize's agentCapabilities.
// loadSession as true (it does, as of goose 1.46.0 — verified directly
// against a real goose serve instance, not assumed from the ACP spec
// alone) — checked explicitly here rather than just letting a
// non-supporting agent's session/load response come back some
// unpredictable way.
//
// Per the ACP spec (and verified the same way, including that it works
// from a *different* connection than the one that originally created the
// session — the expected shape here, since every turn builds a fresh
// Client/connection), the Agent replays the session's entire history as
// session/update notifications on the session-scoped stream before
// responding on that same stream — never the connection-scoped one, unlike
// session/new's response (there is no session-scoped stream yet at that
// point; here there already is, since the caller supplies the id instead
// of receiving one back). Those replayed notifications are intentionally
// discarded (this call doesn't accept an onEvent callback the way Prompt
// does): they're goose's own internal context rebuild, not new activity,
// and every one of those turns' events is already persisted in Paca's own
// agent_conversation_events from when they first actually happened —
// awaitResponse already skips anything that isn't the matching response
// id, the same way it does for NewSession.
func (c *Client) LoadSession(ctx context.Context, sessionID, cwd string, mcpServers []MCPServerConfig) error {
	if c.connectionID == "" {
		return errors.New("acp: LoadSession called before Initialize")
	}
	if !c.supportsLoadSession {
		return errors.New("acp: LoadSession: agent did not advertise the loadSession capability at initialize")
	}

	stream := c.attachSessionStream(sessionID)

	id := c.nextID.Add(1)
	params := LoadSessionParams{
		SessionID:  sessionID,
		Cwd:        cwd,
		MCPServers: []MCPServerConfig{},
		Meta:       &NewSessionMeta{EnabledExtensions: buildEnabledExtensions(mcpServers)},
	}
	if err := c.post(ctx, id, "session/load", params, sessionID); err != nil {
		return fmt.Errorf("acp: session/load: %w", err)
	}

	frame, err := c.awaitResponse(ctx, id, stream)
	if err != nil {
		return fmt.Errorf("acp: session/load: %w", err)
	}
	if frame.Error != nil {
		return fmt.Errorf("acp: session/load: %w", frame.Error)
	}

	c.sessionID = sessionID
	return nil
}

// attachSessionStream (re)establishes this Client's one session-scoped SSE
// stream for sessionID, tearing down any previous one first via
// cancelSession — needed because a caller can fall back from a failed
// LoadSession to a fresh NewSession on the same Client (see
// executor.Executor.attachEnvironmentSession): without explicitly closing
// the abandoned stream first, its still-open GET request would leak for
// this Client's entire remaining lifetime, since goose's ACP transport
// allows only one active subscriber per session scope — the old stream
// can't just be silently replaced, it has to actually be torn down.
// Derives its own context from streamCtx (not a plain streamCtx reuse the
// way connStream gets away with, since connStream is only ever established
// once) so that teardown is possible without cancelling connStream or
// requiring the whole Client to Close.
func (c *Client) attachSessionStream(sessionID string) *frameStream {
	if c.cancelSession != nil {
		c.cancelSession()
	}
	sessionCtx, cancel := context.WithCancel(c.streamCtx)
	c.cancelSession = cancel
	c.sessionStream = c.startStream(sessionCtx, sessionID)
	return c.sessionStream
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
	if c.connectionID == "" {
		return "", nil, errors.New("acp: Prompt called before Initialize")
	}
	if c.sessionStream == nil {
		return "", nil, errors.New("acp: Prompt called before NewSession/LoadSession")
	}

	id := c.nextID.Add(1)
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

// post sends one JSON-RPC request over POST and confirms goose accepted it
// — the actual response arrives asynchronously on the relevant stream
// (connection-scoped for session/new, session-scoped for everything else),
// never in this call's own HTTP response body. Accept is application/json
// only (not text/event-stream) since a POST's own response is now always a
// bare status with no body worth negotiating a format for.
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
	req.Header.Set("Accept", "application/json")
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

// awaitResponse blocks until the frame matching id arrives on stream,
// silently discarding anything else along the way — used by NewSession
// (which never expects any notifications first) and LoadSession (which
// does, its history-replay session/update notifications, deliberately
// discarded here rather than forwarded anywhere; see LoadSession's own doc
// comment), unlike Prompt, which forwards every notification to its
// caller's onEvent.
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
