// Package acpbridge is the Go port of services/ai-agent's agent/acp_bridge.py
// (this file), routes/bridge.py (server.go), and agent/acp_dispatch.py
// (dispatch.go) — the trigger-routing path for acp-type agents. An
// acp-type agent's "sandbox" is a WebSocket connection from a daemon the
// user runs on their own machine (apps/acp-bridge), not a Docker
// container this process manages.
//
// Presence and dispatch both go through Valkey, using the exact same key
// and channel prefixes acp_bridge.py uses, so the two work correctly
// together regardless of which service's process currently holds a given
// agent's WebSocket connection during the migration window: PUBLISH on a
// per-agent channel is delivered to every subscriber across all replicas
// (and across both services), and a presence key set by either service is
// readable by the other. This is deliberate, not incidental.
package acpbridge

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"github.com/Paca-AI/agent-runner/internal/convlock"
	"github.com/Paca-AI/agent-runner/internal/messaging"
)

const (
	presencePrefix    = "paca:acp-bridge:online:"
	dispatchPrefix    = "paca:acp-bridge:dispatch:"
	controlPrefix     = "paca:acp-bridge:control:"
	dispatchAckPrefix = "paca:acp-bridge:dispatch-ack:"

	// presenceTTL — the daemon must ping (see server.go's "ping" handling)
	// well within this window; comfortably longer than the daemon's own
	// ~20s heartbeat interval so a couple of missed/delayed pings don't
	// flap presence.
	presenceTTL = 45 * time.Second

	// reconnectBackoff is how long to wait before re-subscribing after the
	// forwarder/eviction-watcher's Pub/Sub connection drops (e.g. a Valkey
	// restart) — keeps the WebSocket connection usable instead of silently
	// losing dispatch delivery for the rest of its lifetime.
	reconnectBackoff = 2 * time.Second

	// forceEvictSessionID is published on the control channel to force-close
	// *any* currently registered session for an agent, regardless of its
	// session_id — used when the agent's bridge token is regenerated (see
	// Evict). Never equal to a real session_id (those are uuid4 hex strings).
	forceEvictSessionID = "__force_evict__"
)

const replacePresenceScript = `
local previous = redis.call('GET', KEYS[1])
redis.call('SET', KEYS[1], ARGV[1], 'PX', ARGV[2])
return previous or ''`

const refreshPresenceScript = `
if redis.call('GET', KEYS[1]) == ARGV[1] then
  redis.call('PEXPIRE', KEYS[1], ARGV[2])
  return 1
end
return 0`

const deletePresenceScript = `
if redis.call('GET', KEYS[1]) == ARGV[1] then
  return redis.call('DEL', KEYS[1])
end
return 0`

// Conn is the subset of a WebSocket connection Registry needs — matches
// server.go's real connection type and acp_bridge.py's _SendsJSON Protocol,
// small enough that tests can substitute a fake without a real WebSocket.
// Close's code follows RFC 6455 (app-specific codes are 4000-4999) —
// mirrors routes/bridge.py's ws.close(code=...) calls, which use a distinct
// code for an evicted session (see forwarders.go's evictionCloseCode) so a
// future daemon revision could tell "you were superseded" apart from an
// ordinary disconnect, even though the current daemon doesn't inspect it.
type Conn interface {
	SendJSON(ctx context.Context, v any) error
	Close(code int, reason string) error
}

// connEntry is one registered agent's local (this-replica-only) state —
// mirrors acp_bridge.py's _connections/_sessions/_forward_tasks/
// _eviction_tasks module dicts, collapsed into one struct per agent.
type connEntry struct {
	conn      Conn
	sessionID string
	cancel    context.CancelFunc
	// done is closed once both the forwarder and eviction-watcher
	// goroutines have actually exited — Unregister waits on it before
	// returning, mirroring Python's `task.cancel(); await task`: without
	// this, a new Register for the same agent racing a still-shutting-down
	// old connection could have both touch the same WebSocket.
	done chan struct{}
}

// Registry tracks local ACP bridge connections and their Valkey-backed
// presence/dispatch/eviction. One instance is shared by every WebSocket
// connection this process holds.
type Registry struct {
	redis     *redis.Client
	publisher *messaging.Publisher
	log       Logger

	mu          sync.Mutex
	connections map[uuid.UUID]*connEntry

	// registerLocks serializes Register's check-then-act sequence (presence
	// check through the map writes, and evicting/waiting on any prior local
	// connection for the same agent_id) — without this, two Register calls
	// racing for the same agent_id (e.g. a reconnect overlapping the still-
	// live old connection) can both observe "not already online" and both
	// decide not to broadcastEviction, so a genuinely-superseded old
	// connection on this or another replica is never told to close. (The
	// map swap itself is separately made leak-safe below by cancelling and
	// waiting on any previously-registered local entry, regardless of this
	// lock.)
	//
	// Keyed per agent_id, not a single process-wide lock: Register now waits
	// on the evicted entry's forwarder/eviction-watcher goroutines actually
	// exiting (see the <-prev.done wait below), which — unlike the plain
	// presence-check-and-map-write this lock originally covered — isn't
	// bounded by anything this process controls (it depends on when those
	// goroutines notice ctx cancellation). A single global lock would let
	// one agent's slow reconnect stall every other agent's Register on this
	// replica for that whole wait; per-agent locking confines the wait to
	// the agent actually reconnecting. Mirrors acp_bridge.py's
	// _register_lock, generalized from a single lock to one per agent_id.
	registerLocks *convlock.Locks
}

// Logger is the small logging surface Registry needs — satisfied by
// *slog.Logger's Warn/Info/Error methods directly (no adapter needed), kept
// as an interface here so this package doesn't force a specific logger
// import shape on callers.
type Logger interface {
	Info(msg string, args ...any)
	Warn(msg string, args ...any)
	Error(msg string, args ...any)
}

// New builds a Registry ready to accept WebSocket connections.
func New(redisClient *redis.Client, publisher *messaging.Publisher, log Logger) *Registry {
	return &Registry{
		redis:         redisClient,
		publisher:     publisher,
		log:           log,
		connections:   make(map[uuid.UUID]*connEntry),
		registerLocks: convlock.New(),
	}
}

func presenceKey(agentID uuid.UUID) string        { return presencePrefix + agentID.String() }
func dispatchChannel(agentID uuid.UUID) string    { return dispatchPrefix + agentID.String() }
func controlChannel(agentID uuid.UUID) string     { return controlPrefix + agentID.String() }
func dispatchAckChannel(deliveryID string) string { return dispatchAckPrefix + deliveryID }

// Register marks agentID's local bridge as connected on this process.
// Publishes an "agent.acp_bridge.status" realtime event so the frontend can
// react immediately instead of polling the status endpoint.
//
// Returns a sessionID the caller (server.go) must pass back to Unregister —
// it's how a stale disconnect (a connection that just lost an eviction race
// to a newer one) knows not to tear down the newer session's state.
//
// If another bridge session is already registered for this agent_id
// (tracked by presence, so this catches a connection on a different
// replica — or the other service — too), it is evicted. Only one bridge
// session per agent is allowed at a time.
func (r *Registry) Register(ctx context.Context, agentID uuid.UUID, projectID *uuid.UUID, conn Conn) (string, error) {
	unlock := r.registerLocks.Lock(agentID)
	defer unlock()

	sessionID := uuid.New().String()
	previous, err := r.redis.Eval(ctx, replacePresenceScript,
		[]string{presenceKey(agentID)}, sessionID, presenceTTL.Milliseconds()).Text()
	if err != nil {
		return "", fmt.Errorf("acpbridge: acquire presence for agent %s: %w", agentID, err)
	}
	alreadyOnline := previous != ""

	connCtx, cancel := context.WithCancel(context.Background())
	entry := &connEntry{conn: conn, sessionID: sessionID, cancel: cancel, done: make(chan struct{})}

	r.mu.Lock()
	prev, hadPrev := r.connections[agentID]
	r.connections[agentID] = entry
	r.mu.Unlock()

	if hadPrev {
		// A same-process reconnect for this agent_id (or a Register that
		// still found an entry despite registerLocks serializing the
		// check-then-act sequence above — e.g. the old connection's own
		// disconnect hasn't reached Unregister yet). Without this, the
		// evicted entry's forwardDispatchedMessages/watchForEviction
		// goroutines and their Redis Pub/Sub subscription would run
		// forever: the old connection's own disconnect-driven Unregister
		// call carries its own (now stale) sessionID, which Unregister's
		// sessionID check correctly treats as a no-op against this newer
		// entry, so cancel() would otherwise never be called for it at
		// all.
		//
		// Closing prev.conn directly (rather than relying solely on
		// broadcastEviction's round trip through Redis, which
		// watchForEviction below would otherwise need to receive before it
		// calls Close) means this local eviction doesn't depend on Pub/Sub
		// delivery at all, and — importantly — cancelling ctx alone does
		// NOT close the connection: watchForEviction only calls conn.Close
		// when it *receives* an eviction message, not merely when its ctx
		// is cancelled (see forwarders.go's subscribeLoop). Order mirrors
		// Unregister's own cancel-then-wait sequence, plus the explicit
		// close watchForEviction would otherwise have been responsible for.
		prev.cancel()
		if err := prev.conn.Close(evictionCloseCode, "evicted"); err != nil {
			r.log.Warn("acpbridge: failed to close a superseded local connection",
				"agent_id", agentID, "error", err)
		}
		<-prev.done
	}

	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); r.forwardDispatchedMessages(connCtx, agentID, sessionID, conn) }()
	go func() { defer wg.Done(); r.watchForEviction(connCtx, agentID, sessionID, conn) }()
	go func() { wg.Wait(); close(entry.done) }()

	r.publishStatus(ctx, agentID, projectID, true)

	if alreadyOnline {
		if err := r.broadcastEviction(ctx, agentID, sessionID); err != nil {
			r.log.Warn("acpbridge: failed to broadcast eviction of a prior session",
				"agent_id", agentID, "error", err)
		}
	}
	return sessionID, nil
}

// Evict force-closes any bridge connection currently registered for
// agentID, regardless of session — called when the agent's bridge token is
// regenerated so a session still authenticated with the old token can't
// linger indefinitely.
func (r *Registry) Evict(ctx context.Context, agentID uuid.UUID) error {
	return r.broadcastEviction(ctx, agentID, forceEvictSessionID)
}

// Unregister tears down a bridge connection — called on WebSocket
// disconnect. No-ops if sessionID no longer matches the currently
// registered session for this agent: a stale connection's disconnect (e.g.
// it just lost an eviction race to a newer connection's Register) must not
// clear the newer session's presence/connection state out from under it.
func (r *Registry) Unregister(ctx context.Context, agentID uuid.UUID, projectID *uuid.UUID, sessionID string) {
	r.mu.Lock()
	entry, ok := r.connections[agentID]
	if !ok || entry.sessionID != sessionID {
		r.mu.Unlock()
		return
	}
	delete(r.connections, agentID)
	r.mu.Unlock()

	entry.cancel()
	<-entry.done

	deleted, err := r.redis.Eval(ctx, deletePresenceScript, []string{presenceKey(agentID)}, sessionID).Int()
	if err != nil {
		r.log.Warn("acpbridge: failed to clear presence", "agent_id", agentID, "error", err)
		return
	}
	if deleted == 1 {
		r.publishStatus(ctx, agentID, projectID, false)
	}
}

// Heartbeat refreshes agentID's presence TTL — called on each "ping" from
// the daemon.
func (r *Registry) Heartbeat(ctx context.Context, agentID uuid.UUID, sessionID string) error {
	refreshed, err := r.redis.Eval(ctx, refreshPresenceScript,
		[]string{presenceKey(agentID)}, sessionID, presenceTTL.Milliseconds()).Int()
	if err != nil {
		return fmt.Errorf("acpbridge: refresh presence for agent %s: %w", agentID, err)
	}
	if refreshed != 1 {
		return fmt.Errorf("acpbridge: bridge session %s no longer owns agent %s", sessionID, agentID)
	}
	return nil
}

// IsOnline reports whether *any* replica (of either service) currently
// holds a live bridge connection for agentID.
func (r *Registry) IsOnline(ctx context.Context, agentID uuid.UUID) (bool, error) {
	value, err := r.redis.Get(ctx, presenceKey(agentID)).Result()
	if err == redis.Nil {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("acpbridge: check presence for agent %s: %w", agentID, err)
	}
	return value != "", nil
}

// Dispatch publishes a message (start_turn/stop_turn/pause_turn) to
// agentID's bridge. Returns false without publishing if the agent isn't
// currently connected (checked first — Valkey Pub/Sub drops messages with
// no subscriber rather than queuing them, so callers must not rely on
// eventual delivery).
func (r *Registry) Dispatch(ctx context.Context, agentID uuid.UUID, message map[string]any) (bool, error) {
	sessionID, err := r.redis.Get(ctx, presenceKey(agentID)).Result()
	if err == redis.Nil || sessionID == "" {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("acpbridge: resolve dispatch owner for agent %s: %w", agentID, err)
	}
	payload := make(map[string]any, len(message)+1)
	for key, value := range message {
		payload[key] = value
	}
	payload["bridge_session_id"] = sessionID
	data, err := json.Marshal(payload)
	if err != nil {
		return false, fmt.Errorf("acpbridge: marshal dispatch message: %w", err)
	}
	subscribers, err := r.redis.Publish(ctx, dispatchChannel(agentID), data).Result()
	if err != nil {
		return false, fmt.Errorf("acpbridge: publish dispatch for agent %s: %w", agentID, err)
	}
	return subscribers > 0, nil
}

// DispatchWithAck retries a Pub/Sub dispatch while an acknowledgement
// subscription is already live. The daemon treats a repeated authoritative
// run identity as a no-op and acknowledges it again, so an ACK loss cannot
// repeat execution side effects.
func (r *Registry) DispatchWithAck(ctx context.Context, agentID uuid.UUID, message map[string]any, timeout time.Duration) (bool, error) {
	deliveryID := uuid.NewString()
	ackChannel := dispatchAckChannel(deliveryID)
	pubsub := r.redis.Subscribe(ctx, ackChannel)
	defer func() { _ = pubsub.Close() }()
	if _, err := pubsub.Receive(ctx); err != nil {
		return false, fmt.Errorf("acpbridge: subscribe dispatch ack: %w", err)
	}
	payload := make(map[string]any, len(message)+1)
	for key, value := range message {
		payload[key] = value
	}
	payload["delivery_id"] = deliveryID
	for attempt := 0; attempt < 3; attempt++ {
		dispatched, err := r.Dispatch(ctx, agentID, payload)
		if err != nil || !dispatched {
			return dispatched, err
		}
		waitCtx, cancel := context.WithTimeout(ctx, timeout)
		_, err = pubsub.ReceiveMessage(waitCtx)
		cancel()
		if err == nil {
			return true, nil
		}
		if ctx.Err() != nil {
			return false, ctx.Err()
		}
	}
	return false, fmt.Errorf("acpbridge: authoritative dispatch was not acknowledged")
}

func (r *Registry) AcknowledgeDispatch(ctx context.Context, agentID uuid.UUID, sessionID, deliveryID string) error {
	if _, err := uuid.Parse(deliveryID); err != nil {
		return fmt.Errorf("acpbridge: invalid dispatch acknowledgement")
	}
	current, err := r.redis.Get(ctx, presenceKey(agentID)).Result()
	if err != nil {
		return fmt.Errorf("acpbridge: resolve acknowledgement owner: %w", err)
	}
	if current != sessionID {
		return fmt.Errorf("acpbridge: stale bridge session cannot acknowledge dispatch")
	}
	return r.redis.Publish(ctx, dispatchAckChannel(deliveryID), sessionID).Err()
}

func (r *Registry) broadcastEviction(ctx context.Context, agentID uuid.UUID, winningSessionID string) error {
	data, err := json.Marshal(map[string]string{"session_id": winningSessionID})
	if err != nil {
		return fmt.Errorf("acpbridge: marshal eviction message: %w", err)
	}
	if err := r.redis.Publish(ctx, controlChannel(agentID), data).Err(); err != nil {
		return fmt.Errorf("acpbridge: publish eviction for agent %s: %w", agentID, err)
	}
	return nil
}

// publishStatus is best-effort — a failure here shouldn't fail the
// register/unregister it's part of. projectID nil is a global-scope ACP
// agent; its bridge status has no single project room to route to (see
// PublishRealtime's own doc comment on project_id/actor_user_id routing),
// a known best-effort gap for global agents carried over unchanged from
// acp_bridge.py's _publish_status.
func (r *Registry) publishStatus(ctx context.Context, agentID uuid.UUID, projectID *uuid.UUID, connected bool) {
	pid := uuid.Nil
	if projectID != nil {
		pid = *projectID
	}
	if err := r.publisher.PublishRealtime(ctx, pid, uuid.Nil, "agent.acp_bridge.status",
		map[string]any{"agent_id": agentID.String(), "connected": connected}, nil); err != nil {
		r.log.Warn("acpbridge: failed to publish bridge status", "agent_id", agentID, "error", err)
	}
}
