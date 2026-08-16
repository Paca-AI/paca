package messaging

import (
	"sync"

	"github.com/google/uuid"
)

// conversationLocks serializes trigger handling per conversation_id: two
// triggers for the same conversation never run Handler concurrently, while
// triggers for different conversations are unaffected and still run in
// parallel up to Consumer's own semaphore limit.
//
// Without this, two triggers arriving close together for one conversation
// (e.g. a user sending two chat messages back to back, or a pause/resume
// racing a still-finishing turn) would each get their own goroutine and run
// handler.Handler.Handle concurrently — racing
// ConversationRepository.NextEventIndex (read once per turn, then
// incremented purely in-memory, so two concurrent turns can compute the
// same starting index and silently lose one turn's events to
// InsertEvent's ON CONFLICT DO NOTHING) and the in-flight registry's
// Register/Unregister pairing. Locking per conversation_id here removes the
// possibility at its root instead of only patching each downstream
// symptom.
//
// Locks are created on first use and removed once their last waiter is
// done, so this never grows unbounded with conversation history — only
// conversations with a turn actually in flight (or queued) hold an entry.
type conversationLocks struct {
	mu    sync.Mutex
	locks map[uuid.UUID]*refCountedMutex
}

type refCountedMutex struct {
	mu  sync.Mutex
	refs int
}

func newConversationLocks() *conversationLocks {
	return &conversationLocks{locks: make(map[uuid.UUID]*refCountedMutex)}
}

// Lock blocks until conversationID's lock is free, then returns an unlock
// function the caller must call exactly once (typically via defer) to
// release it.
func (c *conversationLocks) Lock(conversationID uuid.UUID) (unlock func()) {
	c.mu.Lock()
	l, ok := c.locks[conversationID]
	if !ok {
		l = &refCountedMutex{}
		c.locks[conversationID] = l
	}
	l.refs++
	c.mu.Unlock()

	l.mu.Lock()

	return func() {
		l.mu.Unlock()
		c.mu.Lock()
		l.refs--
		if l.refs == 0 {
			delete(c.locks, conversationID)
		}
		c.mu.Unlock()
	}
}
