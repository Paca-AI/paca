package handler

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Paca-AI/agent-runner/internal/config"
	"github.com/Paca-AI/agent-runner/internal/turnruntime"
)

type fakeTurnRuntimeClient struct {
	claim         *turnruntime.Envelope
	claimErr      error
	claimWorkerID string
	finalized     []turnruntime.FinalizeInput
	renewErr      error
	renewCalls    int
	renewTurnID   uuid.UUID
	renewRunID    uuid.UUID
	renewToken    uuid.UUID
}

type fakeRenewResult struct {
	expiresAt time.Time
	err       error
}

type scriptedRenewRuntimeClient struct {
	*fakeTurnRuntimeClient
	mu         sync.Mutex
	results    []fakeRenewResult
	defaultErr error
	renewed    chan struct{}
}

func (f *scriptedRenewRuntimeClient) Renew(_ context.Context, turnID, runID, claimToken uuid.UUID, _ time.Duration) (time.Time, error) {
	f.mu.Lock()
	f.renewCalls++
	f.renewTurnID = turnID
	f.renewRunID = runID
	f.renewToken = claimToken
	result := fakeRenewResult{err: f.defaultErr}
	if len(f.results) > 0 {
		result = f.results[0]
		f.results = f.results[1:]
	}
	f.mu.Unlock()
	select {
	case f.renewed <- struct{}{}:
	default:
	}
	return result.expiresAt, result.err
}

func (f *fakeTurnRuntimeClient) Claim(_ context.Context, _ uuid.UUID, workerID string, _ time.Duration) (*turnruntime.Envelope, error) {
	f.claimWorkerID = workerID
	return f.claim, f.claimErr
}

func (f *fakeTurnRuntimeClient) Get(context.Context, uuid.UUID) (*turnruntime.Envelope, error) {
	return f.claim, nil
}

func (f *fakeTurnRuntimeClient) Renew(_ context.Context, turnID, runID, claimToken uuid.UUID, _ time.Duration) (time.Time, error) {
	f.renewCalls++
	f.renewTurnID = turnID
	f.renewRunID = runID
	f.renewToken = claimToken
	return time.Now().Add(time.Minute), f.renewErr
}

func (f *fakeTurnRuntimeClient) AppendEvent(context.Context, uuid.UUID, turnruntime.Event) error {
	return nil
}

func (f *fakeTurnRuntimeClient) Finalize(_ context.Context, _ uuid.UUID, input turnruntime.FinalizeInput) error {
	f.finalized = append(f.finalized, input)
	return nil
}

func authoritativeClaimFixture() *turnruntime.Envelope {
	token := uuid.New()
	return &turnruntime.Envelope{
		TurnID: uuid.New(), RunID: uuid.New(), ClaimToken: &token,
		ConversationID: uuid.New(), ProjectID: uuid.New(), AgentID: uuid.New(),
		Backend: "llm", Status: "running", Attempt: 1,
	}
}

func TestHandleTurnDisallowedAgentEndsWithAuditableFailure(t *testing.T) {
	claim := authoritativeClaimFixture()
	runtime := &fakeTurnRuntimeClient{claim: claim}
	handler := &Handler{TurnRuntime: runtime, WorkerID: "runner-test", Gate: config.NewGate(nil)}
	if err := handler.HandleTurn(context.Background(), claim.TurnID); err != nil {
		t.Fatal(err)
	}
	if runtime.claimWorkerID != "runner-test" || len(runtime.finalized) != 1 {
		t.Fatalf("claim/finalize calls: worker=%q finalizations=%+v", runtime.claimWorkerID, runtime.finalized)
	}
	final := runtime.finalized[0]
	if final.TerminalStatus != "failed" || final.StableOutputEventID != nil || final.ErrorCode == nil || *final.ErrorCode != "agent_not_allowed" {
		t.Fatalf("disallowed turn was not failed safely: %+v", final)
	}
}

func TestHandleTurnAuthoritativeACPIsFailedBeforePrivateInputLeavesPaca(t *testing.T) {
	claim := authoritativeClaimFixture()
	claim.Backend = "acp"
	claim.InputText = "private conversation input"
	runtime := &fakeTurnRuntimeClient{claim: claim}
	handler := &Handler{TurnRuntime: runtime, WorkerID: "runner-test", Gate: config.NewGate([]string{"*"})}

	if err := handler.HandleTurn(context.Background(), claim.TurnID); err != nil {
		t.Fatal(err)
	}
	if len(runtime.finalized) != 1 {
		t.Fatalf("finalizations = %+v, want one auditable failure", runtime.finalized)
	}
	final := runtime.finalized[0]
	if final.TerminalStatus != "failed" || final.StableOutputEventID != nil || final.ErrorCode == nil ||
		*final.ErrorCode != "acp_private_runtime_not_isolated" {
		t.Fatalf("authoritative ACP was not failed closed: %+v", final)
	}
}

func TestHandleTurnFencesExactAttemptBeforeLoadingAgentOrStartingSandbox(t *testing.T) {
	claim := authoritativeClaimFixture()
	runtime := &fakeTurnRuntimeClient{
		claim: claim,
		renewErr: &turnruntime.APIError{
			Status: 409, Code: "TURN_FINALIZED", Message: "terminal",
		},
	}
	handler := &Handler{TurnRuntime: runtime, WorkerID: "runner-test", Gate: config.NewGate([]string{"*"})}

	if err := handler.HandleTurn(context.Background(), claim.TurnID); err != nil {
		t.Fatal(err)
	}
	if runtime.renewCalls != 1 || runtime.renewTurnID != claim.TurnID || runtime.renewRunID != claim.RunID ||
		claim.ClaimToken == nil || runtime.renewToken != *claim.ClaimToken {
		t.Fatalf("preflight renew did not fence the exact attempt: calls=%d turn=%s run=%s token=%s",
			runtime.renewCalls, runtime.renewTurnID, runtime.renewRunID, runtime.renewToken)
	}
	if len(runtime.finalized) != 0 {
		t.Fatalf("terminal turn was finalized again: %+v", runtime.finalized)
	}
}

func TestHandleTurnAcknowledgesAlreadyTerminalDelivery(t *testing.T) {
	runtime := &fakeTurnRuntimeClient{claimErr: &turnruntime.APIError{Status: 409, Code: "TURN_FINALIZED", Message: "terminal"}}
	handler := &Handler{TurnRuntime: runtime, WorkerID: "runner-test", Gate: config.NewGate([]string{"*"})}
	if err := handler.HandleTurn(context.Background(), uuid.New()); err != nil {
		t.Fatalf("terminal redelivery should be acknowledged: %v", err)
	}
	if len(runtime.finalized) != 0 {
		t.Fatalf("terminal redelivery finalized twice: %+v", runtime.finalized)
	}
}

func TestHandleTurnLeavesTransientClaimFailurePending(t *testing.T) {
	want := errors.New("api unavailable")
	runtime := &fakeTurnRuntimeClient{claimErr: want}
	handler := &Handler{TurnRuntime: runtime, WorkerID: "runner-test", Gate: config.NewGate([]string{"*"})}
	if err := handler.HandleTurn(context.Background(), uuid.New()); !errors.Is(err, want) {
		t.Fatalf("transient claim error = %v, want %v", err, want)
	}
}

func TestWatchAuthoritativeStateRetriesTransientRenewFailureWithinLiveLease(t *testing.T) {
	claim := authoritativeClaimFixture()
	lease := 150 * time.Millisecond
	now := time.Now()
	runtime := &scriptedRenewRuntimeClient{
		fakeTurnRuntimeClient: &fakeTurnRuntimeClient{claim: claim},
		results: []fakeRenewResult{
			{err: errors.New("temporary control-plane outage")},
			{expiresAt: now.Add(3 * lease)},
		},
		renewed: make(chan struct{}, 4),
	}
	handler := &Handler{TurnRuntime: runtime}
	watchCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	errCh := make(chan error, 1)
	exited := make(chan struct{})
	go func() {
		handler.watchAuthoritativeState(watchCtx, claim, now.Add(lease), lease, done, errCh, cancel)
		close(exited)
	}()

	for call := 0; call < 2; call++ {
		select {
		case <-runtime.renewed:
		case <-time.After(time.Second):
			t.Fatalf("renew call %d did not occur", call+1)
		}
	}
	close(done)
	select {
	case <-exited:
	case <-time.After(time.Second):
		t.Fatal("state watcher did not stop")
	}
	if watchCtx.Err() != nil {
		t.Fatalf("transient renewal failure cancelled a live execution: %v", watchCtx.Err())
	}
	select {
	case err := <-errCh:
		t.Fatalf("transient renewal failure was reported as ownership loss: %v", err)
	default:
	}
}

func TestWatchAuthoritativeStateCancelsAfterConfirmedLeaseExpires(t *testing.T) {
	claim := authoritativeClaimFixture()
	lease := 90 * time.Millisecond
	runtime := &scriptedRenewRuntimeClient{
		fakeTurnRuntimeClient: &fakeTurnRuntimeClient{claim: claim},
		defaultErr:            errors.New("control plane unavailable"),
		renewed:               make(chan struct{}, 4),
	}
	handler := &Handler{TurnRuntime: runtime}
	watchCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errCh := make(chan error, 1)
	exited := make(chan struct{})
	go func() {
		handler.watchAuthoritativeState(watchCtx, claim, time.Now().Add(lease), lease, make(chan struct{}), errCh, cancel)
		close(exited)
	}()

	select {
	case err := <-errCh:
		if !errors.Is(err, errAuthoritativeLeaseExpired) {
			t.Fatalf("watch error = %v, want lease expiration", err)
		}
	case <-time.After(time.Second):
		t.Fatal("state watcher did not stop after lease expiration")
	}
	select {
	case <-exited:
	case <-time.After(time.Second):
		t.Fatal("state watcher goroutines did not exit")
	}
	if watchCtx.Err() == nil {
		t.Fatal("expired lease did not cancel execution")
	}
}
