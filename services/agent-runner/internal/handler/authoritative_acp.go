package handler

import (
	"context"
	"database/sql"
	"errors"

	"github.com/Paca-AI/agent-runner/internal/turnruntime"
)

func (h *Handler) runAuthoritativeACP(ctx context.Context, claim *turnruntime.Envelope) error {
	if h.AgentRepo == nil || h.ACPDispatcher == nil {
		return h.finalizeAuthoritativeFailure(ctx, claim, "acp_runtime_unavailable",
			"The isolated ACP runtime is not configured on this runner.")
	}
	if claim.LeaseExpiresAt == nil {
		return errors.New("authoritative ACP claim did not include a lease expiration")
	}
	cfg, err := h.AgentRepo.FindACPByID(ctx, claim.AgentID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return h.finalizeAuthoritativeFailure(ctx, claim, "agent_unavailable", "The configured ACP agent is no longer available.")
		}
		return err
	}
	trigger := authoritativeTrigger(claim)
	dispatched, err := h.ACPDispatcher.DispatchAuthoritative(ctx, claim, trigger, cfg)
	if err != nil {
		return err
	}
	if !dispatched {
		return h.finalizeAuthoritativeFailure(ctx, claim, "acp_bridge_offline",
			"The local ACP bridge is not connected. Reconnect it and start a new turn.")
	}
	// ACP completion is persisted by the bridge server through the same
	// fenced turn runtime contract as LLM completion. Reuse the same split
	// poll/renew watcher so a slow state read cannot starve the lease.
	done := make(chan struct{})
	exited := make(chan struct{})
	watchErr := make(chan error, 1)
	watchCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() {
		defer close(exited)
		h.watchAuthoritativeState(watchCtx, claim, *claim.LeaseExpiresAt, authoritativeLease, done, watchErr, cancel)
	}()
	<-watchCtx.Done()
	close(done)
	<-exited
	select {
	case stateErr := <-watchErr:
		if errors.Is(stateErr, errAuthoritativeTerminal) {
			h.stopAuthoritativeACP(context.WithoutCancel(ctx), claim, "turn_terminal")
			return nil
		}
		h.stopAuthoritativeACP(context.WithoutCancel(ctx), claim, "claim_lost")
		return stateErr
	default:
		h.stopAuthoritativeACP(context.WithoutCancel(ctx), claim, "worker_shutdown")
		return ctx.Err()
	}
}

func (h *Handler) stopAuthoritativeACP(ctx context.Context, claim *turnruntime.Envelope, reason string) {
	if h.ACPDispatcher == nil {
		return
	}
	if dispatched, err := h.ACPDispatcher.StopAuthoritative(ctx, turnControlFromClaim(claim, reason)); err != nil || !dispatched {
		h.Log.Warn("authoritative ACP: failed to dispatch fenced stop", "turn_id", claim.TurnID,
			"run_id", claim.RunID, "dispatched", dispatched, "error", err)
	}
}
