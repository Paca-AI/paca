package handler

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Paca-AI/agent-runner/internal/chatsandbox"
	"github.com/Paca-AI/agent-runner/internal/registry"
)

// TestResumeLock_ClosesTheHandleVsTeardownRace is a regression test for the
// race flagged in review between Handle's "Register with InFlight, then
// read the paused sandbox via ChatSandboxes.Get" sequence and
// TeardownPausedChatSandbox's "check InFlight.IsRegistered, then
// ChatSandboxes.Pop" sequence.
//
// Both sequences are individually check-then-act: without resumeLock making
// them mutually exclusive per conversation_id, a teardown call (from a
// concurrent "agent.stop" control message or the idle reaper — neither
// gated by messaging.Consumer's per-conversation trigger lock, which only
// serializes trigger-vs-trigger, not trigger-vs-control) could observe
// IsRegistered==false *before* a resuming Handle call's own Register runs,
// decide to proceed, and then Pop (and the caller then tears down) the very
// sandbox Handle's Get concurrently found and started relying on.
//
// The sleep between the teardown side's check and its Pop deliberately
// widens that window — without it, both critical sections are only a
// couple of fast map operations, and a buggy (lock-free) version can pass
// by sheer luck almost every run, exactly like consumer_test.go's own
// TestConsumer_SerializesTriggersForTheSameConversation notes for the
// analogous trigger-lock race. Confirmed this actually catches the bug: run
// with the two `h.resumeLock().Lock` calls below commented out, it fails on
// the first iteration; restored, it passes.
//
// Exercises the exact two call sequences using real, dependency-free
// registry.Conversations/chatsandbox.Registry instances — Handle and
// TeardownPausedChatSandbox's full bodies need Postgres/Docker via
// test/e2e.
func TestResumeLock_ClosesTheHandleVsTeardownRace(t *testing.T) {
	for i := range 50 {
		h := &Handler{InFlight: registry.New(), ChatSandboxes: chatsandbox.New()}
		convID := uuid.New()
		h.ChatSandboxes.Set(convID, &chatsandbox.State{})

		var wg sync.WaitGroup
		var resumeGotState, teardownPopped bool

		// Mirrors Handle's own critical section (handler.go's Handle,
		// the "Register-then-Get" block).
		wg.Add(1)
		go func() {
			defer wg.Done()
			unlock := h.resumeLock().Lock(convID)
			defer unlock()

			_, cancel := context.WithCancel(context.Background())
			defer cancel()
			h.InFlight.Register(convID, cancel)
			_, resumeGotState = h.ChatSandboxes.Get(convID)
		}()

		// Mirrors TeardownPausedChatSandbox's own critical section. The
		// sleep sits between the check and the Pop specifically to give a
		// concurrent Register (above) a real chance to land in that gap if
		// the lock isn't actually serializing the two sequences.
		wg.Add(1)
		go func() {
			defer wg.Done()
			unlock := h.resumeLock().Lock(convID)
			defer unlock()

			if !h.InFlight.IsRegistered(convID) {
				time.Sleep(20 * time.Millisecond)
				_, teardownPopped = h.ChatSandboxes.Pop(convID)
			}
		}()

		wg.Wait()

		if resumeGotState && teardownPopped {
			t.Fatalf("iteration %d: Handle's Get and TeardownPausedChatSandbox's Pop both succeeded for the same conversation — the sandbox a resuming turn started using would be torn down out from under it", i)
		}
	}
}

// TestResumeLock_LazilyInitializedAndSafeForConcurrentFirstUse confirms the
// lazy-init via sync.Once works under concurrent first callers — every
// existing Handler{...} struct literal (cmd/agent-runner/main.go and
// test/e2e) predates resumeLocks and never sets it explicitly.
func TestResumeLock_LazilyInitializedAndSafeForConcurrentFirstUse(t *testing.T) {
	h := &Handler{}
	convID := uuid.New()

	var wg sync.WaitGroup
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			unlock := h.resumeLock().Lock(convID)
			unlock()
		}()
	}
	wg.Wait()

	if h.resumeLocks == nil {
		t.Fatal("resumeLocks was never initialized")
	}
}
