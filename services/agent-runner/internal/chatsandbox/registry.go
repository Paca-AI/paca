// Package chatsandbox keeps a chat conversation's sandbox container alive
// between turns instead of tearing it down after every single message — the
// Go analog of services/ai-agent's core/registry.py (chat_sandboxes,
// ChatSandboxState) and the idle-reaping half of executor.py
// (_find_idle_chat_sandboxes/reap_idle_chat_sandboxes).
package chatsandbox

import (
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/Paca-AI/agent-runner/internal/acp"
	"github.com/Paca-AI/agent-runner/internal/sandbox"
)

// State is one paused chat conversation's live sandbox, kept around for its
// next reply. Populated when a chat_message conversation reaches a natural
// per-turn finish or an interrupt-only pause instead of being torn down;
// consumed on the next reply to reattach without re-cloning/re-starting the
// container or re-running the ACP handshake.
//
// Client is the already-Initialize()'d ACP client for this sandbox, not
// just its connection details — Prompt takes sessionID as a plain
// parameter with no other client-side state, so there's no need to
// re-Initialize (and no need to find out whether goose serve treats a
// second initialize call as resetting prior session/new state — a question
// this design avoids rather than answers) on a resumed turn. Keeping the
// object also avoids re-deriving the transport-level Acp-Session-Id.
type State struct {
	Handle    *sandbox.Handle
	Client    *acp.Client
	SessionID string
	// ProjectID is uuid.Nil for a global-chat conversation — see
	// agent.Trigger.ProjectID's own doc comment on the same convention.
	ProjectID uuid.UUID
	// ActorUserID is set only for a global-chat conversation — mirrors
	// agent.Trigger.ActorUserID.
	ActorUserID  *uuid.UUID
	LastActiveAt time.Time
}

// Registry is a process-local map of conversation_id to its paused chat
// sandbox. Safe for concurrent use.
//
// Single-process only, same limitation as internal/registry.Conversations —
// see that package's doc comment.
type Registry struct {
	mu      sync.Mutex
	entries map[uuid.UUID]*State
}

// New builds an empty Registry.
func New() *Registry {
	return &Registry{entries: make(map[uuid.UUID]*State)}
}

// Get returns the paused sandbox for conversationID, if one exists — the Go
// analog of chat_sandboxes.get(trigger.conversation_id) in run_conversation.
func (r *Registry) Get(conversationID uuid.UUID) (*State, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.entries[conversationID]
	return s, ok
}

// Set registers (or replaces) conversationID's paused sandbox — call after a
// turn ends in a natural finish or an interrupt-only pause.
func (r *Registry) Set(conversationID uuid.UUID, s *State) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.entries[conversationID] = s
}

// Pop removes and returns conversationID's paused sandbox, if any — call
// when tearing it down for good (explicit stop, error, or the idle reaper).
func (r *Registry) Pop(conversationID uuid.UUID) (*State, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.entries[conversationID]
	delete(r.entries, conversationID)
	return s, ok
}

// Touch refreshes conversationID's LastActiveAt — called on an
// "agent.heartbeat" control message. projectID is cross-checked against the
// stored entry (cheap, in-memory) so a heartbeat can't keep alive a sandbox
// from a conversation the caller wasn't scoped to, mirroring worker.py's
// _handle_control heartbeat branch. Returns false if no entry exists (or the
// project doesn't match) — a no-op either way from the caller's
// perspective, but useful for tests/logging.
func (r *Registry) Touch(conversationID, projectID uuid.UUID) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.entries[conversationID]
	if !ok || s.ProjectID != projectID {
		return false
	}
	s.LastActiveAt = time.Now()
	return true
}

// FindIdle returns conversation IDs whose paused sandbox has been idle
// longer than timeout, excluding any conversationID for which inFlight
// returns true — mirrors _find_idle_chat_sandboxes's exclusion of
// conversations present in stop_events: last_active_at isn't updated while
// a turn is actually running (only at turn-end and by heartbeats), so it
// can look stale by this check alone without actually being idle.
func (r *Registry) FindIdle(now time.Time, timeout time.Duration, inFlight func(uuid.UUID) bool) []uuid.UUID {
	r.mu.Lock()
	defer r.mu.Unlock()
	var idle []uuid.UUID
	for id, s := range r.entries {
		if now.Sub(s.LastActiveAt) > timeout && !inFlight(id) {
			idle = append(idle, id)
		}
	}
	return idle
}
