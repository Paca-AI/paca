package handler

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	agentdom "github.com/Paca-AI/api/internal/domain/agent"
	"github.com/Paca-AI/api/internal/transport/http/httpx"
)

const maxRuntimeBodyBytes = 2 << 20

type AgentTurnRuntimeHandler struct {
	repo  agentTurnRuntimeRepository
	token string
}

type agentTurnRuntimeRepository interface {
	ClaimTurnRun(context.Context, agentdom.ClaimTurnRunInput) (*agentdom.ClaimedTurnRun, error)
	GetTurnRuntime(context.Context, uuid.UUID) (*agentdom.TurnBundle, error)
	RenewTurnRunLease(context.Context, agentdom.RenewTurnRunLeaseInput) (time.Time, error)
	AppendTurnEvent(context.Context, agentdom.AppendTurnEventInput) (*agentdom.AgentConversationEvent, error)
	FinalizeTurn(context.Context, agentdom.FinalizeTurnInput) (*agentdom.TurnResult, error)
}

func NewAgentTurnRuntimeHandler(repo agentTurnRuntimeRepository, token string) *AgentTurnRuntimeHandler {
	return &AgentTurnRuntimeHandler{repo: repo, token: token}
}

type runtimeEnvelope struct {
	TurnID                 uuid.UUID               `json:"turn_id"`
	RunID                  uuid.UUID               `json:"run_id"`
	ClaimToken             *uuid.UUID              `json:"claim_token,omitempty"`
	ConversationID         uuid.UUID               `json:"conversation_id"`
	SessionID              *uuid.UUID              `json:"session_id,omitempty"`
	ProjectID              uuid.UUID               `json:"project_id"`
	AgentID                uuid.UUID               `json:"agent_id"`
	RequestedByMemberID    *uuid.UUID              `json:"requested_by_member_id,omitempty"`
	InputText              string                  `json:"input_text"`
	Backend                agentdom.TurnBackend    `json:"backend"`
	Attempt                int                     `json:"attempt"`
	Status                 agentdom.TurnStatus     `json:"status"`
	DeadlineAt             *time.Time              `json:"deadline_at,omitempty"`
	LeaseExpiresAt         *time.Time              `json:"lease_expires_at,omitempty"`
	ToolPolicy             agentdom.TurnToolPolicy `json:"tool_policy"`
	ToolPolicySHA256       string                  `json:"tool_policy_sha256"`
	SnapshotManifest       json.RawMessage         `json:"snapshot_manifest"`
	SnapshotManifestSHA256 string                  `json:"snapshot_manifest_sha256"`
	SnapshotRenderedText   string                  `json:"snapshot_rendered_text"`
	TerminalStatus         *agentdom.TurnStatus    `json:"terminal_status,omitempty"`
}

func runtimeEnvelopeFromBundle(bundle *agentdom.TurnBundle, claimToken *uuid.UUID) runtimeEnvelope {
	envelope := runtimeEnvelope{ClaimToken: claimToken}
	if bundle == nil || bundle.Turn == nil || bundle.Run == nil {
		return envelope
	}
	envelope.TurnID = bundle.Turn.ID
	envelope.RunID = bundle.Run.ID
	envelope.ConversationID = bundle.Turn.ConversationID
	envelope.SessionID = bundle.Turn.SessionID
	if bundle.Turn.ProjectID != nil {
		envelope.ProjectID = *bundle.Turn.ProjectID
	}
	envelope.AgentID = bundle.Turn.AgentID
	envelope.RequestedByMemberID = bundle.Turn.RequestedByMemberID
	envelope.InputText = bundle.Turn.InputText
	envelope.Backend = bundle.Run.Backend
	envelope.Attempt = bundle.Run.Attempt
	envelope.Status = bundle.Turn.Status
	envelope.DeadlineAt = bundle.Turn.DeadlineAt
	envelope.LeaseExpiresAt = bundle.Run.LeaseExpiresAt
	envelope.ToolPolicy = bundle.Turn.ToolPolicy
	envelope.ToolPolicySHA256 = bundle.Turn.ToolPolicySHA256
	if bundle.Snapshot != nil {
		envelope.SnapshotManifest = append(json.RawMessage(nil), bundle.Snapshot.Manifest...)
		envelope.SnapshotManifestSHA256 = bundle.Snapshot.ManifestSHA256
		envelope.SnapshotRenderedText = bundle.Snapshot.RenderedText
	}
	if bundle.Result != nil {
		status := bundle.Result.TerminalStatus
		envelope.TerminalStatus = &status
	}
	return envelope
}

func (h *AgentTurnRuntimeHandler) Claim(w http.ResponseWriter, r *http.Request) {
	if !h.authorized(r) {
		runtimeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "invalid internal token")
		return
	}
	turnID, ok := runtimeTurnID(w, r)
	if !ok {
		return
	}
	var request struct {
		WorkerID string `json:"worker_id"`
		LeaseMS  int64  `json:"lease_ms"`
	}
	if !decodeRuntimeJSON(w, r, &request) || strings.TrimSpace(request.WorkerID) == "" || request.LeaseMS < 1000 || request.LeaseMS > int64((5*time.Minute)/time.Millisecond) {
		runtimeError(w, http.StatusBadRequest, "INVALID_REQUEST", "invalid claim request")
		return
	}
	claim, err := h.repo.ClaimTurnRun(r.Context(), agentdom.ClaimTurnRunInput{
		TurnID: turnID, WorkerID: request.WorkerID, LeaseDuration: time.Duration(request.LeaseMS) * time.Millisecond,
	})
	if err != nil {
		writeRuntimeDomainError(w, err)
		return
	}
	writeRuntimeOK(w, runtimeEnvelopeFromBundle(&claim.Bundle, &claim.ClaimToken))
}

func (h *AgentTurnRuntimeHandler) Get(w http.ResponseWriter, r *http.Request) {
	if !h.authorized(r) {
		runtimeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "invalid internal token")
		return
	}
	turnID, ok := runtimeTurnID(w, r)
	if !ok {
		return
	}
	bundle, err := h.repo.GetTurnRuntime(r.Context(), turnID)
	if err != nil {
		writeRuntimeDomainError(w, err)
		return
	}
	writeRuntimeOK(w, runtimeEnvelopeFromBundle(bundle, nil))
}

func (h *AgentTurnRuntimeHandler) Renew(w http.ResponseWriter, r *http.Request) {
	if !h.authorized(r) {
		runtimeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "invalid internal token")
		return
	}
	turnID, ok := runtimeTurnID(w, r)
	if !ok {
		return
	}
	var request struct {
		RunID      uuid.UUID `json:"run_id"`
		ClaimToken uuid.UUID `json:"claim_token"`
		LeaseMS    int64     `json:"lease_ms"`
	}
	if !decodeRuntimeJSON(w, r, &request) || request.RunID == uuid.Nil || request.ClaimToken == uuid.Nil || request.LeaseMS < 1000 || request.LeaseMS > int64((5*time.Minute)/time.Millisecond) {
		runtimeError(w, http.StatusBadRequest, "INVALID_REQUEST", "invalid lease request")
		return
	}
	if err := h.validateRuntimeRun(r.Context(), turnID, request.RunID); err != nil {
		writeRuntimeDomainError(w, err)
		return
	}
	expiresAt, err := h.repo.RenewTurnRunLease(r.Context(), agentdom.RenewTurnRunLeaseInput{
		RunID: request.RunID, ClaimToken: request.ClaimToken,
		LeaseDuration: time.Duration(request.LeaseMS) * time.Millisecond,
	})
	if err != nil {
		writeRuntimeDomainError(w, err)
		return
	}
	writeRuntimeOK(w, map[string]any{"lease_expires_at": expiresAt})
}

func (h *AgentTurnRuntimeHandler) AppendEvent(w http.ResponseWriter, r *http.Request) {
	if !h.authorized(r) {
		runtimeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "invalid internal token")
		return
	}
	turnID, ok := runtimeTurnID(w, r)
	if !ok {
		return
	}
	var request struct {
		ID          uuid.UUID       `json:"id"`
		RunID       uuid.UUID       `json:"run_id"`
		ClaimToken  uuid.UUID       `json:"claim_token"`
		Sequence    int             `json:"sequence"`
		EventType   string          `json:"event_type"`
		EventSource string          `json:"event_source"`
		Payload     json.RawMessage `json:"payload"`
		CreatedAt   *time.Time      `json:"created_at,omitempty"`
	}
	if !decodeRuntimeJSON(w, r, &request) || request.ID == uuid.Nil || request.RunID == uuid.Nil || request.ClaimToken == uuid.Nil || request.Sequence < 0 || strings.TrimSpace(request.EventType) == "" || strings.TrimSpace(request.EventSource) == "" || !json.Valid(request.Payload) {
		runtimeError(w, http.StatusBadRequest, "INVALID_REQUEST", "invalid turn event")
		return
	}
	createdAt := time.Now().UTC()
	if request.CreatedAt != nil {
		createdAt = request.CreatedAt.UTC()
	}
	event, err := h.repo.AppendTurnEvent(r.Context(), agentdom.AppendTurnEventInput{
		ID: request.ID, TurnID: turnID, RunID: request.RunID, ClaimToken: request.ClaimToken,
		TurnSequence: request.Sequence, EventType: request.EventType,
		EventSource: request.EventSource, Payload: request.Payload, CreatedAt: createdAt,
	})
	if err != nil {
		writeRuntimeDomainError(w, err)
		return
	}
	writeRuntimeOK(w, map[string]any{"id": event.ID, "event_index": event.EventIndex, "sequence": request.Sequence})
}

func (h *AgentTurnRuntimeHandler) Finalize(w http.ResponseWriter, r *http.Request) {
	if !h.authorized(r) {
		runtimeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "invalid internal token")
		return
	}
	turnID, ok := runtimeTurnID(w, r)
	if !ok {
		return
	}
	var request struct {
		RunID              uuid.UUID                   `json:"run_id"`
		ClaimToken         uuid.UUID                   `json:"claim_token"`
		TerminalStatus     agentdom.TurnStatus         `json:"terminal_status"`
		StableOutputEvent  *uuid.UUID                  `json:"stable_output_event_id,omitempty"`
		GeneratedByAgentID uuid.UUID                   `json:"generated_by_agent_id"`
		ErrorCode          *string                     `json:"error_code,omitempty"`
		ErrorMessage       *string                     `json:"error_message,omitempty"`
		Disposition        agentdom.RuntimeDisposition `json:"runtime_disposition"`
		FinalSequence      *int                        `json:"final_sequence,omitempty"`
	}
	if !decodeRuntimeJSON(w, r, &request) || request.RunID == uuid.Nil || request.ClaimToken == uuid.Nil || request.GeneratedByAgentID == uuid.Nil {
		runtimeError(w, http.StatusBadRequest, "INVALID_REQUEST", "invalid finalization request")
		return
	}
	if err := h.validateRuntimeRun(r.Context(), turnID, request.RunID); err != nil {
		writeRuntimeDomainError(w, err)
		return
	}
	result, err := h.repo.FinalizeTurn(r.Context(), agentdom.FinalizeTurnInput{
		RunID: request.RunID, ClaimToken: request.ClaimToken,
		TerminalStatus: request.TerminalStatus, StableOutputEvent: request.StableOutputEvent,
		GeneratedByAgentID: request.GeneratedByAgentID, ErrorCode: request.ErrorCode,
		ErrorMessage: request.ErrorMessage, Disposition: request.Disposition,
		FinalEventSequence: request.FinalSequence,
	})
	if err != nil {
		writeRuntimeDomainError(w, err)
		return
	}
	writeRuntimeOK(w, map[string]any{
		"turn_id": result.TurnID, "run_id": result.RunID,
		"terminal_status": result.TerminalStatus,
	})
}

func (h *AgentTurnRuntimeHandler) validateRuntimeRun(ctx context.Context, turnID, runID uuid.UUID) error {
	bundle, err := h.repo.GetTurnRuntime(ctx, turnID)
	if err != nil {
		return err
	}
	if bundle == nil || bundle.Turn == nil || bundle.Run == nil || bundle.Turn.ID != turnID || bundle.Run.ID != runID {
		return agentdom.ErrTurnClaimLost
	}
	return nil
}

func (h *AgentTurnRuntimeHandler) authorized(r *http.Request) bool {
	provided := r.Header.Get("X-Internal-Token")
	return h.token != "" && len(provided) == len(h.token) && subtle.ConstantTimeCompare([]byte(provided), []byte(h.token)) == 1
}

func runtimeTurnID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, "turnId"))
	if err != nil {
		runtimeError(w, http.StatusBadRequest, "INVALID_REQUEST", "invalid turn id")
		return uuid.Nil, false
	}
	return id, true
}

func decodeRuntimeJSON(w http.ResponseWriter, r *http.Request, out any) bool {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxRuntimeBodyBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(out); err != nil {
		runtimeError(w, http.StatusBadRequest, "INVALID_REQUEST", "invalid JSON body")
		return false
	}
	return true
}

func writeRuntimeOK(w http.ResponseWriter, data any) {
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"success": true, "data": data})
}

func runtimeError(w http.ResponseWriter, status int, code, message string) {
	httpx.WriteJSON(w, status, map[string]any{"success": false, "error_code": code, "error": message})
}

func writeRuntimeDomainError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, agentdom.ErrTurnNotFound):
		runtimeError(w, http.StatusNotFound, "TURN_NOT_FOUND", "turn not found")
	case errors.Is(err, agentdom.ErrTurnBusy):
		runtimeError(w, http.StatusConflict, "TURN_BUSY", "turn already has a live claim")
	case errors.Is(err, agentdom.ErrTurnAlreadyFinalized):
		runtimeError(w, http.StatusConflict, "TURN_FINALIZED", "turn is already terminal")
	case errors.Is(err, agentdom.ErrTurnDeadlineExceeded):
		runtimeError(w, http.StatusGone, "TURN_DEADLINE_EXCEEDED", "turn deadline exceeded")
	case errors.Is(err, agentdom.ErrTurnAuthorizationRevoked):
		runtimeError(w, http.StatusGone, "TURN_AUTHORIZATION_REVOKED", "turn execution authorization was revoked")
	case errors.Is(err, agentdom.ErrTurnClaimLost):
		runtimeError(w, http.StatusConflict, "TURN_CLAIM_LOST", "turn claim was lost")
	case errors.Is(err, agentdom.ErrTurnEventInvalid):
		runtimeError(w, http.StatusUnprocessableEntity, "TURN_EVENT_INVALID", "turn event is invalid")
	case errors.Is(err, agentdom.ErrTurnResultNotPublishable):
		runtimeError(w, http.StatusUnprocessableEntity, "TURN_RESULT_NOT_PUBLISHABLE", "turn has no stable output")
	case errors.Is(err, agentdom.ErrIdempotencyConflict):
		runtimeError(w, http.StatusConflict, "IDEMPOTENCY_CONFLICT", "idempotency conflict")
	default:
		runtimeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "internal server error")
	}
}
