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
type Client struct {
	baseURL    string
	secretKey  string
	httpClient *http.Client

	// transportSessionID is the Acp-Session-Id returned by initialize's
	// response header — required on every following request. Distinct from
	// the ACP-level sessionId returned by session/new; see the migration
	// doc's "two distinct session concepts" note.
	transportSessionID string

	nextID atomic.Int64
}

// NewClient builds a client for a container reachable at baseURL (e.g.
// "http://172.18.0.5:3284"), authenticated with the same secret passed to
// that container as GOOSE_SERVER__SECRET_KEY.
func NewClient(baseURL, secretKey string, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &Client{baseURL: baseURL, secretKey: secretKey, httpClient: httpClient}
}

// Initialize performs the ACP handshake and captures the transport session
// id. Must be called exactly once, before NewSession.
func (c *Client) Initialize(ctx context.Context) error {
	frame, headers, err := c.call(ctx, "initialize", initializeParams{
		ProtocolVersion:    1,
		ClientCapabilities: map[string]any{},
	})
	if err != nil {
		return fmt.Errorf("acp: initialize: %w", err)
	}
	if frame.Error != nil {
		return fmt.Errorf("acp: initialize: %w", frame.Error)
	}
	sessID := headers.Get("Acp-Session-Id")
	if sessID == "" {
		return errors.New("acp: initialize response missing Acp-Session-Id header")
	}
	c.transportSessionID = sessID
	return nil
}

// NewSession opens a new ACP conversation session inside the container and
// returns its sessionId, used in every subsequent Prompt call. cwd must be
// a directory the container's own user can access — /root is a common trap
// on the stock goose image (uid 1000, user "goose"); see the migration
// doc's session/new gotcha.
func (c *Client) NewSession(ctx context.Context, cwd string, mcpServers []MCPServerConfig) (string, error) {
	if c.transportSessionID == "" {
		return "", errors.New("acp: NewSession called before Initialize")
	}
	if mcpServers == nil {
		mcpServers = []MCPServerConfig{}
	}
	frame, _, err := c.call(ctx, "session/new", NewSessionParams{Cwd: cwd, MCPServers: mcpServers})
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
	return result.SessionID, nil
}

// Prompt sends one turn's message and streams session/update notifications
// to onEvent as they arrive. Returns the turn's stopReason on success.
//
// maxToolCalls bounds the number of tool_call notifications this turn may
// receive before Prompt cancels the request and returns ErrMaxToolCalls —
// see ErrMaxToolCalls's doc comment for why this exists at all. Pass 0 for
// no limit (not recommended against a real, paid LLM).
//
// ctx cancellation (e.g. from a user-initiated "stop") aborts the in-flight
// HTTP request; callers should treat ctx.Err() coming back through the
// returned error as a clean stop, not a failure.
func (c *Client) Prompt(
	ctx context.Context,
	sessionID string,
	prompt []ContentBlock,
	maxToolCalls int,
	onEvent func(Event),
) (stopReason string, err error) {
	if c.transportSessionID == "" {
		return "", errors.New("acp: Prompt called before Initialize")
	}

	id := c.nextID.Add(1)
	body, err := json.Marshal(rpcRequest{
		JSONRPC: "2.0",
		ID:      id,
		Method:  "session/prompt",
		Params:  promptParams{SessionID: sessionID, Prompt: prompt},
	})
	if err != nil {
		return "", fmt.Errorf("acp: session/prompt: encoding request: %w", err)
	}

	resp, err := c.post(ctx, body)
	if err != nil {
		return "", fmt.Errorf("acp: session/prompt: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	toolCalls := 0
	sse := newSSEReader(resp.Body)
	for {
		// Checked at the top of every iteration, not only when sse.Next()
		// itself returns an error — found live, against a real goose serve
		// producing a rapid-fire non-converging tool-call loop (the exact
		// scenario ErrMaxToolCalls exists for): a bufio.Reader can have
		// several already-received frames sitting in its buffer, and
		// sse.Next() keeps returning them successfully (no read error)
		// purely from that buffer, with no new network read and therefore
		// no chance for a cancelled ctx to surface as a read error. Without
		// this check, a caller cancelling ctx (e.g. a stop/pause control
		// message — see handler.Handler.HandleControl) only actually
		// interrupted the loop once the buffer ran dry and a real network
		// read finally failed — observed as a ~30s delay between sending
		// an interrupt and Prompt actually returning, against a server
		// producing frames faster than onEvent's own (blocking) publish
		// calls could drain them.
		if ctx.Err() != nil {
			return "", ctx.Err()
		}

		data, readErr := sse.Next()
		if len(data) > 0 {
			var frame rpcFrame
			if err := json.Unmarshal(data, &frame); err != nil {
				return "", fmt.Errorf("acp: session/prompt: decoding frame: %w", err)
			}

			switch {
			case frame.isNotification() && frame.Method == "session/update":
				var note sessionUpdateNotification
				if err := json.Unmarshal(frame.Params, &note); err != nil {
					return "", fmt.Errorf("acp: session/prompt: decoding session/update: %w", err)
				}
				if note.Update.Kind == UpdateToolCall {
					toolCalls++
					if maxToolCalls > 0 && toolCalls > maxToolCalls {
						return "", ErrMaxToolCalls
					}
				}
				if onEvent != nil {
					onEvent(Event{Kind: note.Update.Kind, Raw: note.Update.raw})
				}

			case frame.isResponse() && *frame.ID == id:
				if frame.Error != nil {
					return "", fmt.Errorf("acp: session/prompt: %w", frame.Error)
				}
				var result promptResult
				if err := json.Unmarshal(frame.Result, &result); err != nil {
					return "", fmt.Errorf("acp: session/prompt: decoding result: %w", err)
				}
				return result.StopReason, nil

			default:
				// Any other notification/response shape (e.g. a future
				// permission-request method) is intentionally ignored
				// rather than treated as fatal — session/new's default
				// "auto" currentModeId (confirmed in the spike) means
				// Prompt doesn't expect to need to answer one, but a
				// protocol addition here shouldn't take the whole turn
				// down.
			}
		}

		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				return "", fmt.Errorf("acp: session/prompt: stream closed before a terminal response for id=%d", id)
			}
			if ctx.Err() != nil {
				return "", ctx.Err()
			}
			return "", fmt.Errorf("acp: session/prompt: reading stream: %w", readErr)
		}
	}
}

// call performs a single request/response round trip (used for initialize
// and session/new, neither of which produced intermediate notifications in
// the spike) and returns the terminal frame plus the raw response headers
// (initialize needs Acp-Session-Id off of these; session/new doesn't).
func (c *Client) call(ctx context.Context, method string, params any) (rpcFrame, http.Header, error) {
	id := c.nextID.Add(1)
	body, err := json.Marshal(rpcRequest{JSONRPC: "2.0", ID: id, Method: method, Params: params})
	if err != nil {
		return rpcFrame{}, nil, fmt.Errorf("encoding request: %w", err)
	}

	resp, err := c.post(ctx, body)
	if err != nil {
		return rpcFrame{}, nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	sse := newSSEReader(resp.Body)
	for {
		// See Prompt's identical check for why this has to be at the top
		// of the loop, not only inside the readErr branch below.
		if ctx.Err() != nil {
			return rpcFrame{}, nil, ctx.Err()
		}

		data, readErr := sse.Next()
		if len(data) > 0 {
			var frame rpcFrame
			if err := json.Unmarshal(data, &frame); err != nil {
				return rpcFrame{}, nil, fmt.Errorf("decoding frame: %w", err)
			}
			if frame.isResponse() && *frame.ID == id {
				return frame, resp.Header, nil
			}
			// A notification arriving before the terminal response to a
			// single-shot call isn't expected per the spike, but isn't
			// fatal either — keep reading for the response that matters.
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				return rpcFrame{}, nil, fmt.Errorf("stream closed before a response for id=%d", id)
			}
			return rpcFrame{}, nil, fmt.Errorf("reading stream: %w", readErr)
		}
	}
}

func (c *Client) post(ctx context.Context, body []byte) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/acp", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("building request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("X-Secret-Key", c.secretKey)
	if c.transportSessionID != "" {
		req.Header.Set("Acp-Session-Id", c.transportSessionID)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("sending request: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		defer func() { _ = resp.Body.Close() }()
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("unexpected status %d: %s", resp.StatusCode, msg)
	}
	return resp, nil
}
