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
// chat conversation continuity: a chat conversation's turn ending via
// ReasonPause keeps its sandbox alive for the next reply, the same run
// ending via ReasonStop tears it down for good. Non-chat conversations
// never pause, so ReasonPause is meaningless for them (handler.Handle only
// consults this for chat triggers).
type InterruptReason string

const (
	// ReasonStop tears the sandbox down for good.
	ReasonStop InterruptReason = "stop"
	// ReasonPause keeps the sandbox alive for the next reply.
	ReasonPause InterruptReason = "pause"
)

// entry is one conversation's in-flight-turn bookkeeping: the way to cancel
// it, plus the reason it was last interrupted (if any). token identifies
// which specific Register call created this entry — see Register/Unregister.
type entry struct {
	token     uint64
	cancel    context.CancelFunc
	reason    InterruptReason
	hasReason bool
}

// Conversations is a process-local map of conversation_id to that
// conversation's in-flight-turn entry, live only while a turn is actually
// running. Safe for concurrent use.
//
// Single-process only: in a multi-replica deployment, a stop/pause control
// message is only actionable on whichever replica is actually running that
// conversation. Routing a control message to the right replica isn't
// solved here.
type Conversations struct {
	mu      sync.Mutex
	entries map[uuid.UUID]*entry
	lastTok uint64
}

// New builds an empty Conversations registry.
func New() *Conversations {
	return &Conversations{entries: make(map[uuid.UUID]*entry)}
}

// Register records cancel as the way to interrupt conversationID's
// in-flight turn and returns a token identifying this specific
// registration. Call Unregister with that same token (typically via defer)
// once the turn ends, whether it finished, failed, or was itself cancelled.
//
// The token exists because a conversation can have a second turn registered
// for the same conversationID before the first turn's own deferred
// Unregister runs — e.g. turn 1 pauses and turn 2 (a resumed chat turn) is
// dispatched and registers before turn 1's Handle() goroutine reaches its
// deferred cleanup. Without a token, turn 1's Unregister would delete turn
// 2's live entry regardless of who owns it, leaving turn 2 uncancellable by
// any later stop/pause. Unregister only clears an entry that still matches
// the token it was handed, so a turn only ever removes the registration it
// itself created.
func (c *Conversations) Register(conversationID uuid.UUID, cancel context.CancelFunc) uint64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.lastTok++
	tok := c.lastTok
	c.entries[conversationID] = &entry{token: tok, cancel: cancel}
	return tok
}

// Unregister clears conversationID's registry entry, but only if it's still
// the same registration identified by token — see Register's doc comment.
// A stale token (the entry has since been overwritten by a newer Register)
// is a silent no-op: the newer registration is left untouched.
func (c *Conversations) Unregister(conversationID uuid.UUID, token uint64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if e, ok := c.entries[conversationID]; ok && e.token == token {
		delete(c.entries, conversationID)
	}
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
	e, ok := c.entries[conversationID]
	if ok {
		e.reason = reason
		e.hasReason = true
	}
	c.mu.Unlock()
	if !ok {
		return false
	}
	e.cancel()
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
	_, ok := c.entries[conversationID]
	return ok
}

// TakeReason returns the InterruptReason recorded for conversationID by the
// most recent InterruptWithReason call, if any. Must be called before this
// conversation's own Unregister (typically deferred) runs — see
// Unregister's doc comment.
func (c *Conversations) TakeReason(conversationID uuid.UUID) (InterruptReason, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[conversationID]
	if !ok || !e.hasReason {
		return "", false
	}
	return e.reason, true
}
