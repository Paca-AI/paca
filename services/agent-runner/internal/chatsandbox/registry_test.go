package chatsandbox

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestGet_UnknownConversationReturnsFalse(t *testing.T) {
	r := New()
	if _, ok := r.Get(uuid.New()); ok {
		t.Error("Get returned ok=true for a conversation never Set")
	}
}

func TestSetThenGet_RoundTrips(t *testing.T) {
	r := New()
	convID := uuid.New()
	s := &State{SessionID: "sess-1", LastActiveAt: time.Now()}
	r.Set(convID, s)

	got, ok := r.Get(convID)
	if !ok {
		t.Fatal("Get returned ok=false after Set")
	}
	if got != s {
		t.Errorf("Get returned a different *State than was Set")
	}
}

func TestSet_ReplacesAnExistingEntry(t *testing.T) {
	r := New()
	convID := uuid.New()
	r.Set(convID, &State{SessionID: "old"})
	r.Set(convID, &State{SessionID: "new"})

	got, _ := r.Get(convID)
	if got.SessionID != "new" {
		t.Errorf("SessionID = %q, want %q", got.SessionID, "new")
	}
}

func TestPop_RemovesAndReturnsTheEntry(t *testing.T) {
	r := New()
	convID := uuid.New()
	s := &State{SessionID: "sess-1"}
	r.Set(convID, s)

	got, ok := r.Pop(convID)
	if !ok || got != s {
		t.Fatalf("Pop returned (%v, %v), want (%v, true)", got, ok, s)
	}
	if _, ok := r.Get(convID); ok {
		t.Error("entry still present after Pop")
	}
}

func TestPop_UnknownConversationReturnsFalse(t *testing.T) {
	r := New()
	if _, ok := r.Pop(uuid.New()); ok {
		t.Error("Pop returned ok=true for a conversation never Set")
	}
}

func TestTouch_RefreshesLastActiveAtWhenProjectMatches(t *testing.T) {
	r := New()
	convID := uuid.New()
	projectID := uuid.New()
	old := time.Now().Add(-time.Hour)
	r.Set(convID, &State{ProjectID: projectID, LastActiveAt: old})

	if !r.Touch(convID, projectID) {
		t.Fatal("Touch returned false for a matching project")
	}
	got, _ := r.Get(convID)
	if !got.LastActiveAt.After(old) {
		t.Errorf("LastActiveAt not refreshed: got %v, want after %v", got.LastActiveAt, old)
	}
}

func TestTouch_RejectsAMismatchedProject(t *testing.T) {
	r := New()
	convID := uuid.New()
	old := time.Now().Add(-time.Hour)
	r.Set(convID, &State{ProjectID: uuid.New(), LastActiveAt: old})

	if r.Touch(convID, uuid.New()) {
		t.Error("Touch returned true for a mismatched project")
	}
	got, _ := r.Get(convID)
	if !got.LastActiveAt.Equal(old) {
		t.Error("LastActiveAt was refreshed despite a project mismatch")
	}
}

func TestTouch_UnknownConversationReturnsFalse(t *testing.T) {
	r := New()
	if r.Touch(uuid.New(), uuid.New()) {
		t.Error("Touch returned true for a conversation never Set")
	}
}

func TestFindIdle_ReturnsOnlyEntriesPastTheTimeout(t *testing.T) {
	r := New()
	now := time.Now()

	idleID := uuid.New()
	r.Set(idleID, &State{LastActiveAt: now.Add(-10 * time.Minute)})

	freshID := uuid.New()
	r.Set(freshID, &State{LastActiveAt: now.Add(-1 * time.Minute)})

	idle := r.FindIdle(now, 5*time.Minute, func(uuid.UUID) bool { return false })
	if len(idle) != 1 || idle[0] != idleID {
		t.Errorf("FindIdle = %v, want [%v]", idle, idleID)
	}
}

func TestFindIdle_ExcludesConversationsWithAnInFlightTurn(t *testing.T) {
	r := New()
	now := time.Now()
	convID := uuid.New()
	r.Set(convID, &State{LastActiveAt: now.Add(-10 * time.Minute)})

	idle := r.FindIdle(now, 5*time.Minute, func(id uuid.UUID) bool { return id == convID })
	if len(idle) != 0 {
		t.Errorf("FindIdle = %v, want empty (conversation has an in-flight turn)", idle)
	}
}

func TestFindIdle_EmptyRegistryReturnsNil(t *testing.T) {
	r := New()
	idle := r.FindIdle(time.Now(), 5*time.Minute, func(uuid.UUID) bool { return false })
	if len(idle) != 0 {
		t.Errorf("FindIdle = %v, want empty", idle)
	}
}
