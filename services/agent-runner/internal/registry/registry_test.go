package registry

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

func TestInterrupt_CancelsRegisteredConversation(t *testing.T) {
	r := New()
	convID := uuid.New()
	ctx, cancel := context.WithCancel(context.Background())
	_ = r.Register(convID, cancel)

	if ctx.Err() != nil {
		t.Fatalf("ctx already done before Interrupt: %v", ctx.Err())
	}

	if !r.Interrupt(convID) {
		t.Fatal("Interrupt returned false for a registered conversation")
	}
	if ctx.Err() == nil {
		t.Error("ctx not cancelled after Interrupt")
	}
}

func TestInterrupt_UnknownConversationReturnsFalse(t *testing.T) {
	r := New()
	if r.Interrupt(uuid.New()) {
		t.Error("Interrupt returned true for a conversation never registered")
	}
}

func TestUnregister_MakesInterruptReturnFalse(t *testing.T) {
	r := New()
	convID := uuid.New()
	_, cancel := context.WithCancel(context.Background())
	tok := r.Register(convID, cancel)
	r.Unregister(convID, tok)

	if r.Interrupt(convID) {
		t.Error("Interrupt returned true after Unregister")
	}
}

func TestUnregister_StaleTokenDoesNotClearANewerRegistration(t *testing.T) {
	// Regression test for the overwrite-then-steal-unregister race: turn 1
	// registers, turn 2 registers for the same conversationID before turn
	// 1's own (deferred) Unregister runs. Turn 1's Unregister must be a
	// no-op against turn 2's still-live entry — a subsequent Interrupt must
	// still be able to cancel turn 2.
	r := New()
	convID := uuid.New()

	ctx1, cancel1 := context.WithCancel(context.Background())
	tok1 := r.Register(convID, cancel1)

	ctx2, cancel2 := context.WithCancel(context.Background())
	tok2 := r.Register(convID, cancel2)

	if tok1 == tok2 {
		t.Fatal("two Register calls for the same conversationID returned the same token")
	}

	// Turn 1 finishes and runs its deferred cleanup using its own (now
	// stale) token — this must NOT remove turn 2's entry.
	r.Unregister(convID, tok1)

	if !r.IsRegistered(convID) {
		t.Fatal("turn 1's stale Unregister removed turn 2's live registration")
	}
	if !r.Interrupt(convID) {
		t.Fatal("Interrupt returned false — turn 2 should still be cancellable after turn 1's stale Unregister")
	}
	if ctx1.Err() != nil {
		t.Error("turn 1's context was cancelled by an Interrupt meant for turn 2")
	}
	if ctx2.Err() == nil {
		t.Error("turn 2's context was not cancelled by Interrupt")
	}

	// Turn 2's own Unregister (matching token) does clear the entry.
	r.Unregister(convID, tok2)
	if r.IsRegistered(convID) {
		t.Error("turn 2's own Unregister should have cleared the entry")
	}
}

func TestInterrupt_RecordsStopAsTheDefaultReason(t *testing.T) {
	r := New()
	convID := uuid.New()
	_, cancel := context.WithCancel(context.Background())
	_ = r.Register(convID, cancel)

	r.Interrupt(convID)

	reason, ok := r.TakeReason(convID)
	if !ok {
		t.Fatal("TakeReason found no reason after Interrupt")
	}
	if reason != ReasonStop {
		t.Errorf("reason = %q, want %q", reason, ReasonStop)
	}
}

func TestInterruptWithReason_RecordsPause(t *testing.T) {
	r := New()
	convID := uuid.New()
	ctx, cancel := context.WithCancel(context.Background())
	_ = r.Register(convID, cancel)

	if !r.InterruptWithReason(convID, ReasonPause) {
		t.Fatal("InterruptWithReason returned false for a registered conversation")
	}
	if ctx.Err() == nil {
		t.Error("ctx not cancelled after InterruptWithReason")
	}

	reason, ok := r.TakeReason(convID)
	if !ok {
		t.Fatal("TakeReason found no reason after InterruptWithReason")
	}
	if reason != ReasonPause {
		t.Errorf("reason = %q, want %q", reason, ReasonPause)
	}
}

func TestTakeReason_UnknownConversationReturnsFalse(t *testing.T) {
	r := New()
	if _, ok := r.TakeReason(uuid.New()); ok {
		t.Error("TakeReason returned ok=true for a conversation never interrupted")
	}
}

func TestUnregister_ClearsTheRecordedReason(t *testing.T) {
	r := New()
	convID := uuid.New()
	_, cancel := context.WithCancel(context.Background())
	tok := r.Register(convID, cancel)
	r.Interrupt(convID)
	r.Unregister(convID, tok)

	if _, ok := r.TakeReason(convID); ok {
		t.Error("TakeReason returned ok=true after Unregister")
	}
}

func TestInterruptWithReason_UnknownConversationReturnsFalseAndRecordsNothing(t *testing.T) {
	r := New()
	convID := uuid.New()
	if r.InterruptWithReason(convID, ReasonPause) {
		t.Error("InterruptWithReason returned true for a conversation never registered")
	}
	if _, ok := r.TakeReason(convID); ok {
		t.Error("TakeReason found a reason for a conversation that was never actually interrupted")
	}
}

func TestRegister_ConcurrentUseIsSafe(t *testing.T) {
	r := New()
	done := make(chan struct{})
	for range 50 {
		go func() {
			id := uuid.New()
			_, cancel := context.WithCancel(context.Background())
			tok := r.Register(id, cancel)
			r.Interrupt(id)
			r.Unregister(id, tok)
			done <- struct{}{}
		}()
	}
	for range 50 {
		<-done
	}
}
