package acpbridge

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

// evictionCloseCode mirrors routes/bridge.py's ws.close(code=4409) — an
// app-specific WebSocket close code (RFC 6455 reserves 4000-4999) marking
// "closed because a newer session took over", distinct from an ordinary
// disconnect.
const evictionCloseCode = 4409

// subscribeLoop subscribes to channel and hands each message's payload to
// handle until ctx is cancelled or handle itself signals stop=true.
// Reconnects with backoff on a dropped Pub/Sub connection (e.g. a Valkey
// restart) rather than giving up for the rest of the caller's lifetime —
// mirrors the identical `while True: ... except Exception: ... sleep(...)`
// shape both acp_bridge.py's _forward_dispatched_messages and
// _watch_for_eviction use.
func (r *Registry) subscribeLoop(ctx context.Context, channel string, handle func(data []byte) (stop bool)) {
	for {
		if ctx.Err() != nil {
			return
		}
		pubsub := r.redis.Subscribe(ctx, channel)
		stopped := r.drainMessages(ctx, pubsub, handle)
		_ = pubsub.Close()
		if stopped || ctx.Err() != nil {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(reconnectBackoff):
		}
	}
}

// drainMessages reads messages from pubsub until it errors (connection
// drop) or handle signals stop=true. Returns true only for the latter — the
// caller uses that to distinguish "done for good" from "reconnect".
//
// Also forcibly closes pubsub the moment ctx is cancelled, in a side
// goroutine — found live, and not obvious from go-redis's docs:
// PubSub.ReceiveMessage(ctx) does NOT reliably abort on context
// cancellation alone. It sets a socket read deadline from ctx.Deadline()
// (internal/pool.Conn.deadline), and a plain context.WithCancel-derived
// ctx (this package's connCtx, and Registry.Unregister's ctx before
// that — see registry.go) has no Deadline() at all, so the deadline
// resolves to "no deadline" and the underlying blocking socket read waits
// forever for either a message or the connection being torn down some
// other way. Without this, Unregister's `<-entry.done` — reached on every
// bridge disconnect, not just eviction — hung indefinitely in a live test,
// only ever unblocking because the test's own redis.Client.Close() call
// forced the issue. See docs/ai-agent/goose-migration.md.
func (r *Registry) drainMessages(ctx context.Context, pubsub *redis.PubSub, handle func(data []byte) (stop bool)) bool {
	closeOnCancel := make(chan struct{})
	defer close(closeOnCancel)
	go func() {
		select {
		case <-ctx.Done():
			_ = pubsub.Close()
		case <-closeOnCancel:
		}
	}()

	for {
		msg, err := pubsub.ReceiveMessage(ctx)
		if err != nil {
			return false
		}
		if handle([]byte(msg.Payload)) {
			return true
		}
	}
}

// forwardDispatchedMessages subscribes to agentID's dispatch channel and
// forwards each message to conn until ctx is cancelled — mirrors
// acp_bridge.py's _forward_dispatched_messages.
func (r *Registry) forwardDispatchedMessages(ctx context.Context, agentID uuid.UUID, conn Conn) {
	r.subscribeLoop(ctx, dispatchChannel(agentID), func(data []byte) bool {
		var payload map[string]any
		if err := json.Unmarshal(data, &payload); err != nil {
			r.log.Warn("acpbridge: dropping malformed dispatch message", "agent_id", agentID, "error", err)
			return false
		}
		if err := conn.SendJSON(ctx, payload); err != nil {
			r.log.Warn("acpbridge: failed to forward dispatch message to connection",
				"agent_id", agentID, "error", err)
		}
		return false
	})
}

// watchForEviction closes conn when a message on the control channel names
// a different session_id than sessionID — either a newer bridge session
// registering for this same agent_id (possibly on a different replica, or
// the other service), or a forced eviction from Evict (bridge-token
// regeneration). Enforces at most one active bridge connection per agent.
// Stops watching (rather than continuing to reconnect and watch a
// connection it just closed) once it evicts itself — mirrors
// acp_bridge.py's _watch_for_eviction returning immediately after closing.
func (r *Registry) watchForEviction(ctx context.Context, agentID uuid.UUID, sessionID string, conn Conn) {
	r.subscribeLoop(ctx, controlChannel(agentID), func(data []byte) bool {
		var payload struct {
			SessionID string `json:"session_id"`
		}
		if err := json.Unmarshal(data, &payload); err != nil {
			return false
		}
		if payload.SessionID == sessionID {
			return false
		}
		r.log.Info("acpbridge: evicting session (superseded by a newer connection or a bridge-token regeneration)",
			"agent_id", agentID)
		if err := conn.Close(evictionCloseCode, "evicted"); err != nil {
			r.log.Warn("acpbridge: failed to close evicted connection", "agent_id", agentID, "error", err)
		}
		return true
	})
}
