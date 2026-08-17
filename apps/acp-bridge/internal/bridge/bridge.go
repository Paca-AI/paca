// Package bridge is the WebSocket client half of the daemon: it connects to
// Paca's ACP bridge endpoint, reconnects with backoff, and dispatches
// start_turn/stop_turn/pause_turn messages to a Handler. Port of the old
// Python bridge's bridge_client.py — none of this depends on OpenHands or
// even on ACP; it's purely the wire protocol described in
// services/agent-runner/internal/acpbridge/server.go.
package bridge

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

const (
	heartbeatInterval = 20 * time.Second
	initialBackoff    = 1 * time.Second
	maxBackoff        = 30 * time.Second
	// sendRetryInterval is how long the sender loop waits between retries
	// of the head-of-line outbox message while disconnected (or right after
	// a send attempt failed) — short enough to pick up a reconnect
	// quickly, long enough not to spin tightly.
	sendRetryInterval = 500 * time.Millisecond
	// outboxSize bounds the outbox so a long disconnect during an
	// event-heavy conversation can't grow it without limit. Send still
	// blocks rather than dropping on overflow (deliberate backpressure on
	// the caller instead of unbounded memory growth).
	outboxSize = 5000
)

// SendFunc enqueues one outbound message (an "event" or "turn_status"
// frame). It blocks if the outbox is full, and returns ctx.Err() if ctx is
// done first.
type SendFunc func(ctx context.Context, msg map[string]any) error

// Handler reacts to messages the server sends down the bridge connection.
// Both methods must return quickly — they're called inline from the
// connection's read loop, so any real work (spawning a subprocess, driving
// an ACP turn) belongs on a goroutine each kicks off itself, not inline.
type Handler interface {
	StartTurn(ctx context.Context, msg map[string]any)
	Interrupt(conversationID string)
}

// Client is the bridge daemon's connection to one Paca instance.
type Client struct {
	url       string
	agentID   string
	token     string
	workspace string
	handler   Handler
	log       *slog.Logger

	conn   atomic.Pointer[websocket.Conn]
	outbox chan map[string]any
}

// New builds a Client for the given Paca server, agent id, and bridge
// token. newHandler constructs the message handler, given this Client's own
// Send method — mirrors the old bridge_client.py wiring its
// ConversationRunner up with `self._send` before the object is otherwise
// complete.
func New(server, agentID, token, workspace string, log *slog.Logger, newHandler func(SendFunc) Handler) *Client {
	if log == nil {
		log = slog.Default()
	}
	c := &Client{
		url:       toBridgeWSURL(server),
		agentID:   agentID,
		token:     token,
		workspace: workspace,
		log:       log,
		outbox:    make(chan map[string]any, outboxSize),
	}
	c.handler = newHandler(c.Send)
	return c
}

// toBridgeWSURL turns a Paca base URL (http(s)://host) into the bridge
// WebSocket URL, matching bridge_client.py's to_bridge_ws_url exactly
// (including discarding any path component of server).
func toBridgeWSURL(server string) string {
	u, err := url.Parse(server)
	if err != nil {
		return server
	}
	scheme := "ws"
	if u.Scheme == "https" || u.Scheme == "wss" {
		scheme = "wss"
	}
	out := url.URL{Scheme: scheme, Host: u.Host, Path: "/agent-bridge/ws"}
	return out.String()
}

// Send enqueues msg for delivery by the sender loop, retried across
// reconnects until it succeeds — see outboxSize's doc comment for why this
// blocks rather than drops on a full outbox.
func (c *Client) Send(ctx context.Context, msg map[string]any) error {
	select {
	case c.outbox <- msg:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// RunForever connects, reconnects with exponential backoff on any failure,
// and dispatches inbound messages to the Handler until ctx is done.
func (c *Client) RunForever(ctx context.Context) error {
	senderDone := make(chan struct{})
	go func() { defer close(senderDone); c.senderLoop(ctx) }()
	defer func() { <-senderDone }()

	backoff := initialBackoff
	for {
		err := c.connectOnce(ctx)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil {
			c.log.Warn("bridge: connection lost, reconnecting", "error", err, "backoff", backoff)
		} else {
			backoff = initialBackoff
		}
		select {
		case <-time.After(backoff):
		case <-ctx.Done():
			return ctx.Err()
		}
		if next := backoff * 2; next <= maxBackoff {
			backoff = next
		} else {
			backoff = maxBackoff
		}
	}
}

func (c *Client) connectOnce(ctx context.Context) error {
	c.log.Info("bridge: connecting", "url", c.url)
	conn, _, err := websocket.Dial(ctx, c.url, nil)
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}
	defer func() { _ = conn.CloseNow() }()

	c.conn.Store(conn)
	defer c.conn.Store(nil)

	if err := wsjson.Write(ctx, conn, map[string]string{
		"type": "hello", "agent_id": c.agentID, "token": c.token,
	}); err != nil {
		return fmt.Errorf("sending hello: %w", err)
	}
	var ack map[string]any
	if err := wsjson.Read(ctx, conn, &ack); err != nil {
		return fmt.Errorf("reading hello_ack: %w", err)
	}
	if t, _ := ack["type"].(string); t != "hello_ack" {
		return fmt.Errorf("bridge rejected connection: %v", ack)
	}
	c.log.Info("bridge: connected — serving ACP conversations", "workspace", c.workspace)

	heartbeatCtx, cancelHeartbeat := context.WithCancel(ctx)
	defer cancelHeartbeat()
	go c.heartbeatLoop(heartbeatCtx, conn)

	for {
		var msg map[string]any
		if err := wsjson.Read(ctx, conn, &msg); err != nil {
			return fmt.Errorf("reading message: %w", err)
		}
		c.handleMessage(ctx, msg)
	}
}

func (c *Client) heartbeatLoop(ctx context.Context, conn *websocket.Conn) {
	ticker := time.NewTicker(heartbeatInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			if err := wsjson.Write(ctx, conn, map[string]string{"type": "ping"}); err != nil {
				return
			}
		case <-ctx.Done():
			return
		}
	}
}

func (c *Client) handleMessage(ctx context.Context, msg map[string]any) {
	msgType, _ := msg["type"].(string)
	switch msgType {
	case "start_turn":
		c.handler.StartTurn(ctx, msg)
	case "stop_turn", "pause_turn":
		convID, _ := msg["conversation_id"].(string)
		c.handler.Interrupt(convID)
	case "pong":
		// no-op
	default:
		c.log.Warn("bridge: unknown message type from server", "type", msgType)
	}
}

// senderLoop drains the outbox in order, retrying a message across
// reconnects instead of dropping it. Runs for the lifetime of the process —
// one instance survives every individual WebSocket connection.
func (c *Client) senderLoop(ctx context.Context) {
	for {
		var msg map[string]any
		select {
		case msg = <-c.outbox:
		case <-ctx.Done():
			return
		}
		for {
			conn := c.conn.Load()
			if conn == nil {
				select {
				case <-time.After(sendRetryInterval):
					continue
				case <-ctx.Done():
					return
				}
			}
			if err := wsjson.Write(ctx, conn, msg); err != nil {
				c.log.Warn("bridge: failed to send, will retry once reconnected",
					"type", msg["type"], "error", err)
				select {
				case <-time.After(sendRetryInterval):
					continue
				case <-ctx.Done():
					return
				}
			}
			break
		}
	}
}
