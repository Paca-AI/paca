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
	r.Register(convID, cancel)

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
	r.Register(convID, cancel)
	r.Unregister(convID)

	if r.Interrupt(convID) {
		t.Error("Interrupt returned true after Unregister")
	}
}

func TestInterrupt_RecordsStopAsTheDefaultReason(t *testing.T) {
	r := New()
	convID := uuid.New()
	_, cancel := context.WithCancel(context.Background())
	r.Register(convID, cancel)

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
	r.Register(convID, cancel)

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
	r.Register(convID, cancel)
	r.Interrupt(convID)
	r.Unregister(convID)

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
			r.Register(id, cancel)
			r.Interrupt(id)
			r.Unregister(id)
			done <- struct{}{}
		}()
	}
	for range 50 {
		<-done
	}
}
