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
//
// ready, if non-nil, is called at most once — after the very first
// subscribe attempt's Receive call confirms the subscription actually
// reached Redis. go-redis's PubSub subscribes lazily: r.redis.Subscribe
// itself never touches the network, it's the first Receive-family call
// that actually sends SUBSCRIBE and waits for Redis's own confirmation.
// Callers that need to know the subscription is genuinely live before
// doing anything a concurrent PUBLISH could race against (Register, which
// passes this to close exactly that window) must wait for ready, not
// merely for the goroutine driving this loop to have been scheduled —
// found live via an -race-only CI failure, not guessed: Register was
// returning, and a Dispatch immediately following it publishing, before
// this loop's first subscription had actually reached Redis, silently
// losing that message. If that very first attempt fails instead, ready
// is never called at all — a caller waiting on it relies on its own
// bounded timeout (Register's subscribeReadyTimeout) rather than this
// loop's later, unbounded reconnect retries.
func (r *Registry) subscribeLoop(ctx context.Context, channel string, ready func(), handle func(data []byte) (stop bool)) {
	for first := true; ; first = false {
		if ctx.Err() != nil {
			return
		}
		pubsub := r.redis.Subscribe(ctx, channel)
		if first {
			_, err := pubsub.Receive(ctx)
			if err != nil {
				// Not actually subscribed — ready must not fire here, or
				// Register would consider this goroutine caught up while
				// it's really about to sleep for reconnectBackoff and try
				// again, reopening the exact message-loss window this
				// whole mechanism exists to close. Left uncalled for the
				// rest of this loop's life (first only guards the very
				// first iteration): Register's own bounded
				// subscribeReadyTimeout is what keeps it from waiting
				// forever if every attempt keeps failing.
				_ = pubsub.Close()
				if ctx.Err() != nil {
					return
				}
				select {
				case <-ctx.Done():
					return
				case <-time.After(reconnectBackoff):
				}
				continue
			}
			if ready != nil {
				ready()
			}
		}
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
// forced the issue.
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
// acp_bridge.py's _forward_dispatched_messages. ready is subscribeLoop's
// own parameter, passed straight through — see that doc comment.
func (r *Registry) forwardDispatchedMessages(ctx context.Context, agentID uuid.UUID, conn Conn, ready func()) {
	r.subscribeLoop(ctx, dispatchChannel(agentID), ready, func(data []byte) bool {
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
// ready is subscribeLoop's own parameter, passed straight through — see
// that doc comment.
func (r *Registry) watchForEviction(ctx context.Context, agentID uuid.UUID, sessionID string, conn Conn, ready func()) {
	r.subscribeLoop(ctx, controlChannel(agentID), ready, func(data []byte) bool {
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
