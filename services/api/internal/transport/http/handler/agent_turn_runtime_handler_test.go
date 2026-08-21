package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	agentdom "github.com/Paca-AI/api/internal/domain/agent"
)

type fakeTurnRuntimeRepo struct {
	claimInput agentdom.ClaimTurnRunInput
	claim      *agentdom.ClaimedTurnRun
	claimErr   error
	finalized  int
	renewed    int
	appended   int
}

func (f *fakeTurnRuntimeRepo) ClaimTurnRun(_ context.Context, input agentdom.ClaimTurnRunInput) (*agentdom.ClaimedTurnRun, error) {
	f.claimInput = input
	return f.claim, f.claimErr
}
func (f *fakeTurnRuntimeRepo) GetTurnRuntime(context.Context, uuid.UUID) (*agentdom.TurnBundle, error) {
	if f.claim == nil {
		return nil, agentdom.ErrTurnNotFound
	}
	return &f.claim.Bundle, nil
}
func (f *fakeTurnRuntimeRepo) RenewTurnRunLease(context.Context, agentdom.RenewTurnRunLeaseInput) (time.Time, error) {
	f.renewed++
	return time.Now(), nil
}
func (f *fakeTurnRuntimeRepo) AppendTurnEvent(_ context.Context, input agentdom.AppendTurnEventInput) (*agentdom.AgentConversationEvent, error) {
	f.appended++
	return &agentdom.AgentConversationEvent{ID: input.ID}, nil
}
func (f *fakeTurnRuntimeRepo) FinalizeTurn(_ context.Context, input agentdom.FinalizeTurnInput) (*agentdom.TurnResult, error) {
	f.finalized++
	return &agentdom.TurnResult{TurnID: uuid.New(), RunID: input.RunID, TerminalStatus: input.TerminalStatus}, nil
}

func runtimeClaimFixture() *agentdom.ClaimedTurnRun {
	turnID, runID, conversationID := uuid.New(), uuid.New(), uuid.New()
	projectID, memberID, sessionID, agentID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	token := uuid.New()
	return &agentdom.ClaimedTurnRun{
		ClaimToken: token,
		Bundle: agentdom.TurnBundle{
			Turn: &agentdom.AgentTurn{
				ID: turnID, ConversationID: conversationID, SessionID: &sessionID,
				ProjectID: &projectID, AgentID: agentID, RequestedByMemberID: &memberID,
				InputText: "hello", Status: agentdom.TurnStatusRunning,
				ToolPolicy: agentdom.PrivateChatToolPolicy(), ToolPolicySHA256: strings.Repeat("a", 64),
			},
			Run:      &agentdom.TurnRun{ID: runID, TurnID: turnID, ConversationID: conversationID, Backend: agentdom.TurnBackendLLM, Attempt: 1, Status: agentdom.TurnStatusRunning},
			Snapshot: &agentdom.TurnContextSnapshot{Manifest: json.RawMessage(`[]`), ManifestSHA256: strings.Repeat("b", 64), RenderedText: "UNTRUSTED"},
		},
	}
}

func TestAgentTurnRuntimeClaimRequiresInternalToken(t *testing.T) {
	repo := &fakeTurnRuntimeRepo{claim: runtimeClaimFixture()}
	handler := NewAgentTurnRuntimeHandler(repo, "secret")
	router := chi.NewRouter()
	router.Post("/{turnId}/claim", handler.Claim)
	request := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/"+repo.claim.Bundle.Turn.ID.String()+"/claim", strings.NewReader(`{"worker_id":"w","lease_ms":60000}`))
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", response.Code)
	}
}

func TestAgentTurnRuntimeAllExecutionEndpointsRequireInternalToken(t *testing.T) {
	claim := runtimeClaimFixture()
	repo := &fakeTurnRuntimeRepo{claim: claim}
	handler := NewAgentTurnRuntimeHandler(repo, "secret")
	tests := []struct {
		name, method, suffix, body string
		handle                     http.HandlerFunc
	}{
		{"get", http.MethodGet, "", "", handler.Get},
		{"lease", http.MethodPost, "/lease", `{"run_id":"` + claim.Bundle.Run.ID.String() + `","claim_token":"` + claim.ClaimToken.String() + `","lease_ms":60000}`, handler.Renew},
		{"events", http.MethodPost, "/events", `{"id":"` + uuid.NewString() + `","run_id":"` + claim.Bundle.Run.ID.String() + `","claim_token":"` + claim.ClaimToken.String() + `","sequence":0,"event_type":"message_chunk","event_source":"agent","payload":{}}`, handler.AppendEvent},
		{"finalize", http.MethodPost, "/finalize", `{"run_id":"` + claim.Bundle.Run.ID.String() + `","claim_token":"` + claim.ClaimToken.String() + `","terminal_status":"failed","generated_by_agent_id":"` + claim.Bundle.Turn.AgentID.String() + `","runtime_disposition":"retired"}`, handler.Finalize},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			router := chi.NewRouter()
			router.Method(test.method, "/{turnId}"+test.suffix, test.handle)
			request := httptest.NewRequestWithContext(context.Background(), test.method, "/"+claim.Bundle.Turn.ID.String()+test.suffix, strings.NewReader(test.body))
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != http.StatusUnauthorized {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
		})
	}
	if repo.renewed != 0 || repo.appended != 0 || repo.finalized != 0 {
		t.Fatalf("unauthorized runtime calls reached repository: renew=%d append=%d finalize=%d",
			repo.renewed, repo.appended, repo.finalized)
	}
}

func TestAgentTurnRuntimeClaimReturnsCheckedExecutionEnvelope(t *testing.T) {
	repo := &fakeTurnRuntimeRepo{claim: runtimeClaimFixture()}
	handler := NewAgentTurnRuntimeHandler(repo, "secret")
	router := chi.NewRouter()
	router.Post("/{turnId}/claim", handler.Claim)
	request := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/"+repo.claim.Bundle.Turn.ID.String()+"/claim", strings.NewReader(`{"worker_id":"worker-1","lease_ms":60000}`))
	request.Header.Set("X-Internal-Token", "secret")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", response.Code, response.Body.String())
	}
	var body struct {
		Data runtimeEnvelope `json:"data"`
	}
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.Data.TurnID != repo.claim.Bundle.Turn.ID || body.Data.RunID != repo.claim.Bundle.Run.ID || body.Data.ClaimToken == nil || *body.Data.ClaimToken != repo.claim.ClaimToken {
		t.Fatalf("unexpected runtime envelope: %#v", body.Data)
	}
	if body.Data.SnapshotRenderedText != "UNTRUSTED" || body.Data.ToolPolicy.Mode != "deny_by_default" {
		t.Fatalf("missing snapshot/policy contract: %#v", body.Data)
	}
}

func TestAgentTurnRuntimeClaimBusyIsRetryableConflict(t *testing.T) {
	claim := runtimeClaimFixture()
	repo := &fakeTurnRuntimeRepo{claim: claim, claimErr: agentdom.ErrTurnBusy}
	handler := NewAgentTurnRuntimeHandler(repo, "secret")
	router := chi.NewRouter()
	router.Post("/{turnId}/claim", handler.Claim)
	request := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/"+claim.Bundle.Turn.ID.String()+"/claim", strings.NewReader(`{"worker_id":"w","lease_ms":60000}`))
	request.Header.Set("X-Internal-Token", "secret")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), "TURN_BUSY") {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestAgentTurnRuntimeFinalizeRejectsRunFromDifferentTurnURL(t *testing.T) {
	claim := runtimeClaimFixture()
	repo := &fakeTurnRuntimeRepo{claim: claim}
	handler := NewAgentTurnRuntimeHandler(repo, "secret")
	router := chi.NewRouter()
	router.Post("/{turnId}/finalize", handler.Finalize)
	request := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/"+claim.Bundle.Turn.ID.String()+"/finalize", strings.NewReader(`{
		"run_id":"`+uuid.NewString()+`",
		"claim_token":"`+claim.ClaimToken.String()+`",
		"terminal_status":"failed",
		"generated_by_agent_id":"`+claim.Bundle.Turn.AgentID.String()+`",
		"runtime_disposition":"retired"
	}`))
	request.Header.Set("X-Internal-Token", "secret")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), "TURN_CLAIM_LOST") {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if repo.finalized != 0 {
		t.Fatal("mismatched URL turn finalized a run")
	}
}
