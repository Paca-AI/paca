// Package convlock provides a per-key mutex: independent keys never
// contend with each other, but two callers locking the same key are
// serialized. Used wherever two different code paths (a new-conversation
// trigger vs. a control message, a WebSocket reconnect vs. its own
// eviction of the prior connection) need to be mutually exclusive for one
// entity's ID without forcing unrelated entities to wait on each other.
package convlock

import (
	"sync"

	"github.com/google/uuid"
)

// Locks is a map of key to an independent mutex. Safe for concurrent use.
//
// Locks are created on first use and removed once their last waiter is
// done, so this never grows unbounded with key history — only keys with a
// lock actually held (or queued) have an entry.
type Locks struct {
	mu    sync.Mutex
	locks map[uuid.UUID]*refCountedMutex
}

type refCountedMutex struct {
	mu   sync.Mutex
	refs int
}

// New builds an empty Locks.
func New() *Locks {
	return &Locks{locks: make(map[uuid.UUID]*refCountedMutex)}
}

// Lock blocks until key's lock is free, then returns an unlock function the
// caller must call exactly once (typically via defer) to release it.
func (l *Locks) Lock(key uuid.UUID) (unlock func()) {
	l.mu.Lock()
	e, ok := l.locks[key]
	if !ok {
		e = &refCountedMutex{}
		l.locks[key] = e
	}
	e.refs++
	l.mu.Unlock()

	e.mu.Lock()

	return func() {
		e.mu.Unlock()
		l.mu.Lock()
		e.refs--
		if e.refs == 0 {
			delete(l.locks, key)
		}
		l.mu.Unlock()
	}
}
