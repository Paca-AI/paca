// Package registry tracks in-flight conversations so a control message
// (stop/pause) arriving on a different goroutine than the one running the
// conversation can find and interrupt it. Go analog of services/ai-agent's
// core/registry.py (active_conversations, stop_events, pause_events),
// collapsed into one map since Go's context.CancelFunc already unifies
// "signal this run to stop" the way three separate dicts did in Python.
package registry

import (
	"context"
	"sync"

	"github.com/google/uuid"
)

// InterruptReason distinguishes why a running turn was cancelled — added for
// chat conversation continuity (see docs/ai-agent/goose-migration.md): a
// chat conversation's turn ending via ReasonPause keeps its sandbox alive
// for the next reply, the same run ending via ReasonStop tears it down for
// good. Non-chat conversations never pause, so ReasonPause is meaningless
// for them (handler.Handle only consults this for chat triggers).
type InterruptReason string

const (
	// ReasonStop tears the sandbox down for good.
	ReasonStop InterruptReason = "stop"
	// ReasonPause keeps the sandbox alive for the next reply.
	ReasonPause InterruptReason = "pause"
)

// Conversations is a process-local map of conversation_id to that
// conversation's cancel function, live only while a turn is actually
// running. Safe for concurrent use.
//
// Single-process only: in a multi-replica deployment, a stop/pause control
// message is only actionable on whichever replica is actually running that
// conversation. Routing a control message to the right replica isn't
// solved here; see docs/ai-agent/goose-migration.md.
type Conversations struct {
	mu      sync.Mutex
	cancels map[uuid.UUID]context.CancelFunc
	reasons map[uuid.UUID]InterruptReason
}

// New builds an empty Conversations registry.
func New() *Conversations {
	return &Conversations{
		cancels: make(map[uuid.UUID]context.CancelFunc),
		reasons: make(map[uuid.UUID]InterruptReason),
	}
}

// Register records cancel as the way to interrupt conversationID's
// in-flight turn. Call Unregister (typically via defer) once the turn
// ends, whether it finished, failed, or was itself cancelled.
func (c *Conversations) Register(conversationID uuid.UUID, cancel context.CancelFunc) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cancels[conversationID] = cancel
}

// Unregister also clears any recorded InterruptReason — callers that care
// about the reason (see TakeReason) must read it before Unregister runs
// (typically: read it, then let the caller's own deferred Unregister fire
// as usual), or it's lost.
func (c *Conversations) Unregister(conversationID uuid.UUID) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.cancels, conversationID)
	delete(c.reasons, conversationID)
}

// Interrupt cancels conversationID's in-flight turn if one is running on
// this process. Returns false if none was found (already finished, never
// started here, or running on a different replica). Equivalent to
// InterruptWithReason(conversationID, ReasonStop) — kept as the simple form
// for callers (and tests) that don't care about the pause/stop distinction.
func (c *Conversations) Interrupt(conversationID uuid.UUID) bool {
	return c.InterruptWithReason(conversationID, ReasonStop)
}

// InterruptWithReason cancels conversationID's in-flight turn (same
// semantics as Interrupt) and records reason so the goroutine actually
// running that turn can later read back, via TakeReason, why it was
// cancelled.
func (c *Conversations) InterruptWithReason(conversationID uuid.UUID, reason InterruptReason) bool {
	c.mu.Lock()
	cancel, ok := c.cancels[conversationID]
	if ok {
		c.reasons[conversationID] = reason
	}
	c.mu.Unlock()
	if !ok {
		return false
	}
	cancel()
	return true
}

// IsRegistered reports whether conversationID currently has an in-flight
// turn registered on this process — used as chatsandbox.Registry.FindIdle's
// inFlight predicate by the idle reaper (cmd/agent-runner/main.go), so a
// conversation whose LastActiveAt looks stale purely because a turn is
// actually running right now isn't mistaken for abandoned.
func (c *Conversations) IsRegistered(conversationID uuid.UUID) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	_, ok := c.cancels[conversationID]
	return ok
}

// TakeReason returns the InterruptReason recorded for conversationID by the
// most recent InterruptWithReason call, if any. Must be called before this
// conversation's own Unregister (typically deferred) runs — see
// Unregister's doc comment.
func (c *Conversations) TakeReason(conversationID uuid.UUID) (InterruptReason, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	r, ok := c.reasons[conversationID]
	return r, ok
}
