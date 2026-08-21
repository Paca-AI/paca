package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/Paca-AI/agent-runner/internal/acp"
	"github.com/Paca-AI/agent-runner/internal/agent"
	"github.com/Paca-AI/agent-runner/internal/repository/postgres"
	"github.com/Paca-AI/agent-runner/internal/turnruntime"
)

const authoritativeLease = 60 * time.Second

var errAuthoritativeTerminal = errors.New("authoritative turn reached terminal state")

type turnRuntimeClient interface {
	Claim(ctx context.Context, turnID uuid.UUID, workerID string, lease time.Duration) (*turnruntime.Envelope, error)
	Get(ctx context.Context, turnID uuid.UUID) (*turnruntime.Envelope, error)
	Renew(ctx context.Context, turnID, runID, claimToken uuid.UUID, lease time.Duration) (time.Time, error)
	AppendEvent(ctx context.Context, turnID uuid.UUID, event turnruntime.Event) error
	Finalize(ctx context.Context, turnID uuid.UUID, input turnruntime.FinalizeInput) error
}

// HandleTurn claims and runs one authoritative project-chat turn. The legacy
// Handler.Handle remains untouched for task/comment/automation executions.
func (h *Handler) HandleTurn(ctx context.Context, turnID uuid.UUID) error {
	if h.TurnRuntime == nil {
		return errors.New("authoritative turn runtime is not configured")
	}
	claim, err := h.TurnRuntime.Claim(ctx, turnID, h.WorkerID, authoritativeLease)
	if err != nil {
		if turnruntime.IsTerminalOrExpired(err) {
			return nil
		}
		return err
	}
	if claim.ClaimToken == nil || claim.TurnID != turnID || claim.RunID == uuid.Nil || claim.AgentID == uuid.Nil {
		return errors.New("authoritative turn claim returned an invalid envelope")
	}
	if !h.Gate.Allowed(claim.AgentID) {
		return h.finalizeAuthoritativeFailure(ctx, claim, "agent_not_allowed", "No runner is configured for this agent.")
	}
	if claim.Backend == "acp" {
		// A local coding CLI retains same-UID host filesystem, process, keychain,
		// and network access even when HOME and the session cwd are replaced.
		// OpenHands' ACP bridge also auto-approves provider permission requests.
		// Until the provider runs in a real filesystem/process/network sandbox
		// with a trusted entrypoint and short-lived credentials, executing a
		// private snapshot could recover a long-lived Paca credential and bypass
		// the turn-scoped task-mutation boundary. Fail before private input leaves
		// Paca; the turn still receives the same auditable result contract.
		return h.finalizeAuthoritativeFailure(ctx, claim, "acp_private_runtime_not_isolated",
			"Private ACP chat requires an isolated local runtime and is not available on this bridge.")
	}
	if claim.Backend != "llm" {
		return h.finalizeAuthoritativeFailure(ctx, claim, "unsupported_backend", "The configured agent backend is not supported.")
	}
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	unregister := h.registerAuthoritativeRun(claim, cancel)
	defer unregister()
	// Re-fence the exact run/claim immediately before any agent configuration,
	// private input, or sandbox work is loaded. A plain state GET can take the
	// runtime client's full retry budget and may return a newer attempt; Renew
	// both proves this attempt still owns the lease and gives the watcher a full
	// lease window. Keep the preflight short so a degraded control plane cannot
	// consume most of the lease before physical execution starts.
	preflightCtx, stopPreflight := context.WithTimeout(runCtx, 5*time.Second)
	_, err = h.TurnRuntime.Renew(preflightCtx, claim.TurnID, claim.RunID, *claim.ClaimToken, authoritativeLease)
	stopPreflight()
	if err != nil {
		if turnruntime.IsTerminalOrExpired(err) {
			return nil
		}
		return err
	}
	watchStop := make(chan struct{})
	watchExited := make(chan struct{})
	watchErr := make(chan error, 1)
	go func() {
		defer close(watchExited)
		h.watchAuthoritativeState(runCtx, claim, watchStop, watchErr, cancel)
	}()
	runErr := h.handleAuthoritativeLLM(runCtx, claim, cancel)
	close(watchStop)
	<-watchExited
	select {
	case stateErr := <-watchErr:
		if errors.Is(stateErr, errAuthoritativeTerminal) {
			return nil
		}
		return stateErr
	default:
		return runErr
	}
}

func (h *Handler) handleAuthoritativeLLM(ctx context.Context, claim *turnruntime.Envelope, cancel context.CancelFunc) error {
	cfg, err := h.AgentRepo.FindByID(ctx, claim.AgentID)
	if err != nil {
		if errors.Is(err, postgres.ErrNotLLMAgent) {
			return h.finalizeAuthoritativeFailure(ctx, claim, "backend_mismatch", "The agent backend changed before execution.")
		}
		return h.finalizeAuthoritativeFailure(ctx, claim, "agent_unavailable", "The configured agent is no longer available.")
	}
	if h.BundledSkills != nil {
		bundled, loadErr := h.BundledSkills.Load(ctx)
		if loadErr != nil {
			return h.finalizeAuthoritativeFailure(ctx, claim, "skills_unavailable", "The agent runtime could not load its bundled skills.")
		}
		cfg.Skills = append(bundled, cfg.Skills...)
	}
	trigger := authoritativeTrigger(claim)
	claimToken := *claim.ClaimToken
	sequence := 0
	appendEvent := func(eventType, source string, payload json.RawMessage) error {
		event := turnruntime.Event{
			ID: uuid.New(), RunID: claim.RunID, ClaimToken: claimToken,
			Sequence: sequence, EventType: eventType, EventSource: source,
			Payload: append(json.RawMessage(nil), payload...), CreatedAt: time.Now().UTC(),
		}
		if err := h.TurnRuntime.AppendEvent(ctx, claim.TurnID, event); err != nil {
			return err
		}
		sequence++
		return nil
	}
	userPayload, _ := json.Marshal(map[string]any{"content": map[string]any{"type": "text", "text": claim.InputText}})
	if err := appendEvent("user_message", "user", userPayload); err != nil {
		return err
	}

	var eventMu sync.Mutex
	var stable strings.Builder
	var sinkErr error
	onEvent := func(event acp.Event) {
		eventMu.Lock()
		defer eventMu.Unlock()
		if sinkErr != nil {
			return
		}
		if event.Kind == acp.UpdateAgentMessageChunk {
			var chunk acp.AgentMessageChunk
			if err := json.Unmarshal(event.Raw, &chunk); err == nil {
				stable.WriteString(chunk.Content.Text)
			}
		}
		if err := appendEvent(string(event.Kind), "agent", event.Raw); err != nil {
			sinkErr = err
			cancel()
		}
	}
	onReady := func() {
		eventMu.Lock()
		defer eventMu.Unlock()
		if sinkErr != nil {
			return
		}
		if err := appendEvent("environment_ready", "system", json.RawMessage(`{}`)); err != nil {
			sinkErr = err
			cancel()
		}
	}

	result, runErr := h.Executor.Run(ctx, *cfg, trigger, nil, onEvent, onReady)
	if result.Client != nil {
		result.Client.Close()
	}
	if result.Handle != nil {
		if stopErr := h.Executor.StopSandbox(context.WithoutCancel(ctx), result.Handle); stopErr != nil {
			h.Log.Warn("authoritative turn: failed to stop sandbox", "turn_id", claim.TurnID, "error", stopErr)
		}
	}
	eventMu.Lock()
	if sinkErr == nil && result.Usage != nil {
		usagePayload, _ := json.Marshal(map[string]int64{
			"input_tokens":  result.Usage.InputTokens,
			"output_tokens": result.Usage.OutputTokens,
			"total_tokens":  result.Usage.TotalTokens,
		})
		if err := appendEvent("turn_usage", "system", usagePayload); err != nil {
			sinkErr = err
		}
	}
	currentSinkErr := sinkErr
	stableOutput := strings.TrimSpace(stable.String())
	eventMu.Unlock()
	state, stateErr := h.TurnRuntime.Get(context.WithoutCancel(ctx), claim.TurnID)
	if stateErr == nil && state.TerminalStatus != nil {
		return nil
	}
	if currentSinkErr != nil {
		return currentSinkErr
	}
	if runErr != nil {
		if ctx.Err() != nil {
			// Process shutdown is not a user stop. Let the lease expire so a
			// later attempt can recover instead of forging a terminal result.
			return ctx.Err()
		}
		status, code := "failed", "execution_failed"
		if errors.Is(runErr, context.DeadlineExceeded) {
			status, code = "timed_out", "execution_timeout"
		} else if errors.Is(runErr, acp.ErrMaxToolCalls) {
			code = "max_tool_calls"
		}
		message := runErr.Error()
		return h.finalizeAuthoritative(ctx, claim, status, nil, &code, &message, "retired", nil)
	}
	if stableOutput == "" {
		code, message := "no_stable_output", "The agent completed without a stable response."
		return h.finalizeAuthoritative(ctx, claim, "no_output", nil, &code, &message, "retired", nil)
	}
	stablePayload, _ := json.Marshal(map[string]string{"text": stableOutput})
	stableEventID := uuid.New()
	stableSequence := sequence
	if err := h.TurnRuntime.AppendEvent(ctx, claim.TurnID, turnruntime.Event{
		ID: stableEventID, RunID: claim.RunID, ClaimToken: claimToken,
		Sequence: stableSequence, EventType: turnruntime.StableOutputEventType,
		EventSource: "agent", Payload: stablePayload, CreatedAt: time.Now().UTC(),
	}); err != nil {
		return err
	}
	return h.finalizeAuthoritative(ctx, claim, "succeeded", &stableEventID, nil, nil, "retired", &stableSequence)
}

func authoritativeTrigger(claim *turnruntime.Envelope) agent.Trigger {
	turnID := claim.TurnID
	runtimeID := claim.RunID
	return agent.Trigger{
		TurnID: &turnID, RuntimeID: &runtimeID, ConversationID: claim.ConversationID,
		ProjectID: claim.ProjectID, AgentID: claim.AgentID,
		ChatSessionID: claim.SessionID, ActorMemberID: claim.RequestedByMemberID,
		Message: claim.InputText, TriggerType: agent.TriggerChatMessage,
		ContextSnapshot:         claim.SnapshotRenderedText,
		ContextManifestSHA256:   claim.SnapshotManifestSHA256,
		AllowedToolCapabilities: append([]string(nil), claim.ToolPolicy.AllowedCapabilities...),
	}
}

func (h *Handler) watchAuthoritativeState(ctx context.Context, claim *turnruntime.Envelope, done <-chan struct{}, errCh chan<- error, cancel context.CancelFunc) {
	var wg sync.WaitGroup
	var reportOnce sync.Once
	report := func(err error) {
		reportOnce.Do(func() {
			select {
			case errCh <- err:
			default:
			}
			cancel()
		})
	}
	wg.Add(2)
	go func() {
		defer wg.Done()
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-done:
				return
			case <-ticker.C:
				pollCtx, stopPoll := context.WithTimeout(ctx, 5*time.Second)
				state, err := h.TurnRuntime.Get(pollCtx, claim.TurnID)
				stopPoll()
				if err != nil {
					report(err)
					return
				}
				if state.TerminalStatus != nil {
					report(errAuthoritativeTerminal)
					return
				}
			}
		}
	}()
	go func() {
		defer wg.Done()
		ticker := time.NewTicker(authoritativeLease / 3)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-done:
				return
			case <-ticker.C:
				renewCtx, stopRenew := context.WithTimeout(ctx, authoritativeLease/6)
				_, err := h.TurnRuntime.Renew(renewCtx, claim.TurnID, claim.RunID, *claim.ClaimToken, authoritativeLease)
				stopRenew()
				if err != nil {
					report(err)
					return
				}
			}
		}
	}()
	wg.Wait()
}

func (h *Handler) finalizeAuthoritativeFailure(ctx context.Context, claim *turnruntime.Envelope, code, message string) error {
	state, err := h.TurnRuntime.Get(context.WithoutCancel(ctx), claim.TurnID)
	if err == nil && state != nil && state.TerminalStatus != nil {
		return nil
	}
	return h.finalizeAuthoritative(ctx, claim, "failed", nil, &code, &message, "retired", nil)
}

func (h *Handler) finalizeAuthoritative(ctx context.Context, claim *turnruntime.Envelope, status string, stableEvent *uuid.UUID, code, message *string, disposition string, finalSequence *int) error {
	return h.TurnRuntime.Finalize(context.WithoutCancel(ctx), claim.TurnID, turnruntime.FinalizeInput{
		RunID: claim.RunID, ClaimToken: *claim.ClaimToken, TerminalStatus: status,
		StableOutputEventID: stableEvent, GeneratedByAgentID: claim.AgentID,
		ErrorCode: code, ErrorMessage: message, RuntimeDisposition: disposition,
		FinalSequence: finalSequence,
	})
}

// handleAuthoritativeACP is implemented in authoritative_acp.go. Keeping the
// backend split explicit prevents legacy conversation watchdog/status code from
// becoming an accidental second source of truth.
func (h *Handler) handleAuthoritativeACP(ctx context.Context, claim *turnruntime.Envelope) error {
	return h.runAuthoritativeACP(ctx, claim)
}

func unexpectedAuthoritativeState(claim *turnruntime.Envelope) error {
	return fmt.Errorf("authoritative turn %s is in unexpected state %q", claim.TurnID, claim.Status)
}
