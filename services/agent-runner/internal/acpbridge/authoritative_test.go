package acpbridge

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Paca-AI/agent-runner/internal/turnruntime"
)

type fakeAuthoritativeRuntime struct {
	events    []turnruntime.Event
	turnIDs   []uuid.UUID
	finalizes []turnruntime.FinalizeInput
	appendErr error
}

func (f *fakeAuthoritativeRuntime) AppendEvent(_ context.Context, turnID uuid.UUID, event turnruntime.Event) error {
	f.turnIDs = append(f.turnIDs, turnID)
	f.events = append(f.events, event)
	return f.appendErr
}

func (f *fakeAuthoritativeRuntime) Finalize(_ context.Context, turnID uuid.UUID, input turnruntime.FinalizeInput) error {
	f.turnIDs = append(f.turnIDs, turnID)
	f.finalizes = append(f.finalizes, input)
	return nil
}

func authoritativeMessageIDs() (uuid.UUID, uuid.UUID, uuid.UUID, uuid.UUID) {
	return uuid.New(), uuid.New(), uuid.New(), uuid.New()
}

func TestHandleAuthoritativeEventPreservesFencedIdentityAndSequence(t *testing.T) {
	runtime := &fakeAuthoritativeRuntime{}
	server := &Server{TurnRuntime: runtime, Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	turnID, runID, claimToken, eventID := authoritativeMessageIDs()
	createdAt := time.Now().UTC().Truncate(time.Microsecond)
	server.handleAuthoritativeEvent(context.Background(), uuid.New(), map[string]any{
		"turn_id": turnID.String(), "run_id": runID.String(), "claim_token": claimToken.String(),
		"sequence": 3, "event_id": eventID.String(), "created_at": createdAt.Format(time.RFC3339Nano),
		"event_type": "MessageEvent", "event_source": "agent", "payload": `{"content":"hello"}`,
	})
	if len(runtime.events) != 1 || runtime.turnIDs[0] != turnID {
		t.Fatalf("event was not persisted: ids=%v events=%+v", runtime.turnIDs, runtime.events)
	}
	event := runtime.events[0]
	if event.ID != eventID || event.RunID != runID || event.ClaimToken != claimToken || event.Sequence != 3 || string(event.Payload) != `{"content":"hello"}` {
		t.Fatalf("fenced event changed on ingress: %+v", event)
	}
}

func TestHandleAuthoritativeFinishedAppendsStableEventBeforeFinalize(t *testing.T) {
	runtime := &fakeAuthoritativeRuntime{}
	server := &Server{TurnRuntime: runtime, Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	turnID, runID, claimToken, stableID := authoritativeMessageIDs()
	agentID := uuid.New()
	createdAt := time.Now().UTC().Truncate(time.Microsecond)
	server.handleAuthoritativeStatus(context.Background(), agentID, map[string]any{
		"turn_id": turnID.String(), "run_id": runID.String(), "claim_token": claimToken.String(),
		"status": "finished", "stable_output": "stable answer", "stable_sequence": 4,
		"stable_event_id": stableID.String(), "stable_created_at": createdAt.Format(time.RFC3339Nano),
	})
	if len(runtime.events) != 1 || len(runtime.finalizes) != 1 {
		t.Fatalf("stable completion calls: events=%d finalizes=%d", len(runtime.events), len(runtime.finalizes))
	}
	event, final := runtime.events[0], runtime.finalizes[0]
	if event.EventType != turnruntime.StableOutputEventType || event.Sequence != 4 || event.ID != stableID {
		t.Fatalf("invalid stable event: %+v", event)
	}
	if final.TerminalStatus != "succeeded" || final.RuntimeDisposition != "retired" ||
		final.StableOutputEventID == nil || *final.StableOutputEventID != stableID ||
		final.FinalSequence == nil || *final.FinalSequence != 4 || final.GeneratedByAgentID != agentID {
		t.Fatalf("invalid stable finalization: %+v", final)
	}
}

func TestHandleAuthoritativeStatusDoesNotFinalizeWhenStableEventIsRejected(t *testing.T) {
	runtime := &fakeAuthoritativeRuntime{appendErr: errors.New("claim lost")}
	server := &Server{TurnRuntime: runtime, Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	turnID, runID, claimToken, stableID := authoritativeMessageIDs()
	server.handleAuthoritativeStatus(context.Background(), uuid.New(), map[string]any{
		"turn_id": turnID.String(), "run_id": runID.String(), "claim_token": claimToken.String(),
		"status": "finished", "stable_output": "late answer", "stable_sequence": 0,
		"stable_event_id": stableID.String(), "stable_created_at": time.Now().UTC().Format(time.RFC3339Nano),
	})
	if len(runtime.events) != 1 || len(runtime.finalizes) != 0 {
		t.Fatalf("rejected stable event still finalized: events=%d finalizes=%d", len(runtime.events), len(runtime.finalizes))
	}
}

func TestHandleAuthoritativeEmptyFinishBecomesNoOutput(t *testing.T) {
	runtime := &fakeAuthoritativeRuntime{}
	server := &Server{TurnRuntime: runtime, Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	turnID, runID, claimToken, _ := authoritativeMessageIDs()
	server.handleAuthoritativeStatus(context.Background(), uuid.New(), map[string]any{
		"turn_id": turnID.String(), "run_id": runID.String(), "claim_token": claimToken.String(),
		"status": "finished", "stable_output": "  ",
	})
	if len(runtime.events) != 0 || len(runtime.finalizes) != 1 || runtime.finalizes[0].TerminalStatus != "no_output" || runtime.finalizes[0].StableOutputEventID != nil {
		t.Fatalf("empty completion was publishable: events=%+v finalizes=%+v", runtime.events, runtime.finalizes)
	}
}

func TestHandleAuthoritativeFailureUsesFencedTurnResultContract(t *testing.T) {
	runtime := &fakeAuthoritativeRuntime{}
	server := &Server{TurnRuntime: runtime, Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	turnID, runID, claimToken, _ := authoritativeMessageIDs()
	agentID := uuid.New()
	if err := server.handleAuthoritativeStatus(context.Background(), agentID, map[string]any{
		"turn_id": turnID.String(), "run_id": runID.String(), "claim_token": claimToken.String(),
		"status": "failed", "error_message": "provider rejected the request",
	}); err != nil {
		t.Fatal(err)
	}
	if len(runtime.finalizes) != 1 {
		t.Fatalf("failure finalizations = %d, want 1", len(runtime.finalizes))
	}
	final := runtime.finalizes[0]
	if final.RunID != runID || final.ClaimToken != claimToken || final.GeneratedByAgentID != agentID ||
		final.TerminalStatus != "failed" || final.StableOutputEventID != nil ||
		final.ErrorCode == nil || *final.ErrorCode != "acp_execution_failed" ||
		final.ErrorMessage == nil || *final.ErrorMessage != "provider rejected the request" {
		t.Fatalf("invalid authoritative ACP failure: %+v", final)
	}
}

func TestAuthoritativeDeliveryACKOnlyDropsPermanentFailures(t *testing.T) {
	if !authoritativeDeliveryComplete(nil) {
		t.Fatal("successful delivery was not acknowledged")
	}
	if !authoritativeDeliveryComplete(&turnruntime.APIError{Status: 409, Code: "TURN_CLAIM_LOST"}) {
		t.Fatal("permanent stale claim would poison the bridge outbox")
	}
	if authoritativeDeliveryComplete(&turnruntime.APIError{Status: 503, Code: "INTERNAL_ERROR"}) {
		t.Fatal("transient control-plane failure was acknowledged")
	}
	if authoritativeDeliveryComplete(errors.New("connection reset")) {
		t.Fatal("network failure was acknowledged")
	}
	if !authoritativeDeliveryComplete(errors.New("invalid authoritative event id")) {
		t.Fatal("invalid wire data would poison the bridge outbox")
	}
}
