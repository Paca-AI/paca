package handler

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/Paca-AI/agent-runner/internal/messaging"
	"github.com/Paca-AI/agent-runner/internal/turnruntime"
)

type authoritativeRunCancel struct {
	turnID     uuid.UUID
	claimToken uuid.UUID
	attempt    int
	cancel     context.CancelFunc
}

func (h *Handler) registerAuthoritativeRun(claim *turnruntime.Envelope, cancel context.CancelFunc) func() {
	entry := authoritativeRunCancel{turnID: claim.TurnID, claimToken: *claim.ClaimToken,
		attempt: claim.Attempt, cancel: cancel}
	h.authoritativeRuns.Store(claim.RunID, entry)
	return func() { h.authoritativeRuns.Delete(claim.RunID) }
}

// HandleTurnControl is the durable stop consumer. The database is already
// terminal when this runs; this path only terminates the matching physical
// execution and is therefore safe to replay.
func (h *Handler) HandleTurnControl(ctx context.Context, control messaging.TurnControl) error {
	if raw, ok := h.authoritativeRuns.Load(control.RunID); ok {
		entry := raw.(authoritativeRunCancel)
		if entry.turnID == control.TurnID && entry.claimToken == control.ClaimToken && entry.attempt == control.Attempt {
			entry.cancel()
		}
	}
	if control.Backend != "acp" {
		return nil
	}
	if h.ACPDispatcher == nil {
		return fmt.Errorf("ACP dispatcher is not configured")
	}
	dispatched, err := h.ACPDispatcher.StopAuthoritative(ctx, control)
	if err != nil {
		return err
	}
	if !dispatched {
		return fmt.Errorf("ACP bridge is offline")
	}
	return nil
}

func turnControlFromClaim(claim *turnruntime.Envelope, reason string) messaging.TurnControl {
	return messaging.TurnControl{TurnID: claim.TurnID, RunID: claim.RunID,
		ConversationID: claim.ConversationID, AgentID: claim.AgentID,
		ClaimToken: *claim.ClaimToken, Attempt: claim.Attempt, Backend: claim.Backend, Reason: reason}
}
