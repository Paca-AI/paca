package acpbridge

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/Paca-AI/agent-runner/internal/agent"
	"github.com/Paca-AI/agent-runner/internal/messaging"
	"github.com/Paca-AI/agent-runner/internal/repository/postgres"
	"github.com/Paca-AI/agent-runner/internal/turnruntime"
)

const (
	offlineMessage = "Local ACP bridge is not connected. Run `paca-acp-bridge run --agent-id " +
		"<id> --token <token>` in your project folder, then try again."
	timeoutMessage = "ACP turn timed out waiting for a result from the local bridge. Check " +
		"that `paca-acp-bridge run` is still running and reachable, then retry."
)

// Dispatcher routes triggers for acp-type agents to their connected local
// bridge — mirrors agent/acp_dispatch.py's role for LLM agents
// (executor.Run/handler.Handler), but never spins up a sandbox: the ACP CLI
// runs entirely on the user's own machine via apps/acp-bridge, connected
// through Registry/server.go. Legacy executions use the user's local auth and
// tools. Authoritative private turns additionally carry an immutable policy;
// the bridge strips inherited Paca credentials and retires the subprocess at
// the end of the turn so that policy cannot leak into a later turn.
type Dispatcher struct {
	Registry  *Registry
	ConvRepo  *postgres.ConversationRepository
	Publisher *messaging.Publisher
	Log       Logger
}

// DispatchAuthoritative sends one already-claimed turn to the local bridge.
// DB lease/deadline recovery is the watchdog; the legacy conversation timer
// must never mutate an authoritative turn's underlying conversation.
func (d *Dispatcher) DispatchAuthoritative(ctx context.Context, claim *turnruntime.Envelope, trigger agent.Trigger, cfg *agent.ACPConfig) (bool, error) {
	online, err := d.Registry.IsOnline(ctx, cfg.ID)
	if err != nil || !online {
		return false, err
	}
	dispatched, err := d.Registry.DispatchWithAck(ctx, cfg.ID, map[string]any{
		"type":                      "start_turn",
		"conversation_id":           claim.ConversationID.String(),
		"project_id":                projectIDOrEmpty(claim.ProjectID),
		"turn_id":                   claim.TurnID.String(),
		"run_id":                    claim.RunID.String(),
		"claim_token":               claim.ClaimToken.String(),
		"attempt":                   claim.Attempt,
		"message":                   BuildAuthoritativeACPMessage(trigger),
		"trigger_type":              string(trigger.TriggerType),
		"acp_provider":              cfg.ACPProvider,
		"acp_command":               cfg.ACPCommand,
		"snapshot_manifest_sha256":  claim.SnapshotManifestSHA256,
		"turn_allowed_capabilities": []string{},
	}, 3*time.Second)
	if err != nil {
		return false, err
	}
	if !dispatched {
		return false, nil
	}
	online, err = d.Registry.IsOnline(ctx, cfg.ID)
	return online, err
}

func (d *Dispatcher) StopAuthoritative(ctx context.Context, control messaging.TurnControl) (bool, error) {
	return d.Registry.DispatchWithAck(ctx, control.AgentID, map[string]any{
		"type": "stop_turn", "conversation_id": control.ConversationID.String(),
		"turn_id": control.TurnID.String(), "run_id": control.RunID.String(),
		"claim_token": control.ClaimToken.String(), "attempt": control.Attempt,
		"reason": control.Reason,
	}, 3*time.Second)
}

// DispatchTrigger hands trigger off to cfg's connected local ACP bridge.
// Mirrors dispatch_acp_trigger: fails the conversation immediately if the
// bridge isn't connected (checked, then re-checked after the dispatch
// publish itself — Valkey Pub/Sub has no delivery guarantee, so a bridge
// that disconnects between the two checks needs the same failure path),
// otherwise schedules a watchdog to fail the conversation if the bridge
// never reports a terminal turn_status within its timeout.
func (d *Dispatcher) DispatchTrigger(ctx context.Context, trigger agent.Trigger, cfg *agent.ACPConfig) error {
	online, err := d.Registry.IsOnline(ctx, cfg.ID)
	if err != nil {
		return err
	}
	if !online {
		d.Log.Info("acpbridge: bridge offline; failing conversation",
			"agent_id", cfg.ID, "conversation_id", trigger.ConversationID)
		return d.failOffline(ctx, trigger)
	}

	if err := d.ConvRepo.UpdateStatus(ctx, trigger.ConversationID, "running", nil); err != nil {
		return err
	}

	dispatched, err := d.Registry.Dispatch(ctx, cfg.ID, map[string]any{
		"type":            "start_turn",
		"conversation_id": trigger.ConversationID.String(),
		"project_id":      projectIDOrEmpty(trigger.ProjectID),
		"message":         BuildACPMessage(trigger, cfg.ACPProvider),
		"trigger_type":    string(trigger.TriggerType),
		"acp_provider":    cfg.ACPProvider,
		"acp_command":     cfg.ACPCommand,
	})
	if err != nil {
		return err
	}
	if !dispatched {
		// Bridge disconnected between the IsOnline check above and here
		// (e.g. the daemon crashed mid-dispatch) — fail the same way.
		d.Log.Info("acpbridge: bridge went offline before dispatch; failing conversation",
			"agent_id", cfg.ID, "conversation_id", trigger.ConversationID)
		return d.failOffline(ctx, trigger)
	}

	timeoutMinutes := cfg.TimeoutMinutes
	if timeoutMinutes <= 0 {
		timeoutMinutes = 1
	}
	go d.watchdog(trigger.ConversationID, trigger.ProjectID, time.Duration(timeoutMinutes)*time.Minute, trigger.ActorUserID)
	return nil
}

func (d *Dispatcher) failOffline(ctx context.Context, trigger agent.Trigger) error {
	errMsg := offlineMessage
	if err := d.ConvRepo.UpdateStatus(ctx, trigger.ConversationID, "failed", &errMsg); err != nil {
		return err
	}
	if err := d.Publisher.PublishRealtime(ctx, trigger.ProjectID, trigger.ConversationID,
		"agent.conversation.failed", nil, trigger.ActorUserID); err != nil {
		d.Log.Warn("acpbridge: failed to publish realtime status",
			"conversation_id", trigger.ConversationID, "error", err)
	}
	return nil
}

// watchdog fails a dispatched ACP turn that never reported a terminal
// turn_status. Dispatch to the bridge goes over Valkey Pub/Sub (see
// Registry.Dispatch), which has no delivery guarantee — a start_turn
// published while the daemon is mid-reconnect is silently dropped, leaving
// the conversation stuck at "running" with nothing to ever move it forward.
// This is the backstop: after timeout, fail the conversation if it hasn't
// already reached a terminal status. Race-safe against a legitimate late
// turn_status via ConvRepo.FailIfNotTerminal's conditional UPDATE.
//
// Runs detached from the request that scheduled it (mirrors
// asyncio.create_task, not tied to the caller's own context) — a long-lived
// timer that must survive after DispatchTrigger itself has already
// returned.
func (d *Dispatcher) watchdog(conversationID, projectID uuid.UUID, timeout time.Duration, actorUserID *uuid.UUID) {
	time.Sleep(timeout)
	ctx := context.Background()
	failed, err := d.ConvRepo.FailIfNotTerminal(ctx, conversationID, timeoutMessage)
	if err != nil {
		d.Log.Warn("acpbridge: watchdog failed to check/update conversation status",
			"conversation_id", conversationID, "error", err)
		return
	}
	if !failed {
		return
	}
	d.Log.Warn("acpbridge: ACP turn timed out with no turn_status from the bridge",
		"conversation_id", conversationID, "timeout", timeout)
	if err := d.Publisher.PublishRealtime(ctx, projectID, conversationID,
		"agent.conversation.failed", nil, actorUserID); err != nil {
		d.Log.Warn("acpbridge: failed to publish realtime status",
			"conversation_id", conversationID, "error", err)
	}
}

func projectIDOrEmpty(id uuid.UUID) string {
	if id == uuid.Nil {
		return ""
	}
	return id.String()
}
