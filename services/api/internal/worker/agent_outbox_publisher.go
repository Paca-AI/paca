package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"sync"
	"time"

	"github.com/google/uuid"

	agentdom "github.com/Paca-AI/api/internal/domain/agent"
	"github.com/Paca-AI/api/internal/events"
)

const (
	agentOutboxBatch       = 50
	agentOutboxLease       = 30 * time.Second
	agentOutboxPoll        = time.Second
	agentOutboxMaxBackoff  = time.Minute
	agentOutboxWorkerLabel = "api-agent-outbox"
)

type permanentOutboxError struct{ error }

func permanentOutboxf(format string, args ...any) error {
	return permanentOutboxError{fmt.Errorf(format, args...)}
}

type agentOutboxRepository interface {
	ClaimOutbox(ctx context.Context, workerID string, limit int, lease time.Duration) ([]*agentdom.OutboxEvent, error)
	MarkOutboxPublished(ctx context.Context, eventID, lockToken uuid.UUID, at time.Time) error
	RetryOutbox(ctx context.Context, eventID, lockToken uuid.UUID, next time.Time, lastError string, dead bool) error
	ResolveOutboxAudience(ctx context.Context, event *agentdom.OutboxEvent) (*agentdom.OutboxAudience, error)
}

type agentOutboxPublisher interface {
	AppendFlat(ctx context.Context, stream string, fields map[string]any) error
	Publish(ctx context.Context, channel string, payload any) error
}

// AgentOutboxPublisher is the durable bridge from PostgreSQL's transaction
// outbox to Valkey. It never publishes private input, snapshots, transcripts,
// or stable output: workers fetch those only after an authenticated, fenced
// claim through the internal runtime API.
type AgentOutboxPublisher struct {
	repo      agentOutboxRepository
	publisher agentOutboxPublisher
	workerID  string
	log       *slog.Logger
	cancel    context.CancelFunc
	wg        sync.WaitGroup
}

func NewAgentOutboxPublisher(repo agentOutboxRepository, publisher agentOutboxPublisher, log *slog.Logger) *AgentOutboxPublisher {
	return &AgentOutboxPublisher{
		repo: repo, publisher: publisher,
		workerID: agentOutboxWorkerLabel + ":" + uuid.NewString(), log: log,
	}
}

func (w *AgentOutboxPublisher) Start(parent context.Context) {
	if w == nil || w.repo == nil || w.publisher == nil || w.cancel != nil {
		return
	}
	ctx, cancel := context.WithCancel(parent)
	w.cancel = cancel
	w.wg.Add(1)
	go func() {
		defer w.wg.Done()
		w.run(ctx)
	}()
}

func (w *AgentOutboxPublisher) Stop() {
	if w == nil || w.cancel == nil {
		return
	}
	w.cancel()
	w.wg.Wait()
	w.cancel = nil
}

func (w *AgentOutboxPublisher) run(ctx context.Context) {
	ticker := time.NewTicker(agentOutboxPoll)
	defer ticker.Stop()
	for {
		if err := w.publishBatch(ctx); err != nil && ctx.Err() == nil {
			w.log.Warn("agent outbox publish batch failed", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (w *AgentOutboxPublisher) publishBatch(ctx context.Context) error {
	claimed, err := w.repo.ClaimOutbox(ctx, w.workerID, agentOutboxBatch, agentOutboxLease)
	if err != nil {
		return err
	}
	for _, event := range claimed {
		if event.LockToken == nil {
			continue
		}
		if err := w.publishOne(ctx, event); err != nil {
			delay := time.Duration(math.Min(math.Pow(2, float64(event.Attempts)), agentOutboxMaxBackoff.Seconds())) * time.Second
			var permanent permanentOutboxError
			// Transport failures remain retryable with a capped delay. A temporary
			// Valkey outage must not silently turn a durable requested/control
			// event into an unrecoverable dead letter after an arbitrary count.
			// Only a payload/schema error that can never succeed without operator
			// repair is terminal.
			dead := errors.As(err, &permanent)
			if retryErr := w.repo.RetryOutbox(ctx, event.ID, *event.LockToken, time.Now().UTC().Add(delay), err.Error(), dead); retryErr != nil {
				w.log.Warn("agent outbox retry scheduling failed", "event_id", event.ID, "error", retryErr)
			} else if dead {
				w.log.Error("agent outbox moved to dead letter", "event_id", event.ID, "event_type", event.EventType, "attempts", event.Attempts, "error", err)
			}
			continue
		}
		if err := w.repo.MarkOutboxPublished(ctx, event.ID, *event.LockToken, time.Now().UTC()); err != nil {
			// XADD/PUBLISH may already have succeeded. Leaving the lease to
			// expire deliberately causes a duplicate delivery; downstream turn
			// claim and activity IDs are the idempotency boundary.
			w.log.Warn("agent outbox mark published failed", "event_id", event.ID, "error", err)
		}
	}
	return nil
}

func (w *AgentOutboxPublisher) publishOne(ctx context.Context, event *agentdom.OutboxEvent) error {
	var body map[string]any
	if err := json.Unmarshal(event.Payload, &body); err != nil {
		return permanentOutboxError{fmt.Errorf("decode outbox payload: %w", err)}
	}
	switch event.EventType {
	case "agent.turn.requested":
		turnID, _ := body["turn_id"].(string)
		if turnID == "" {
			return permanentOutboxf("turn requested outbox is missing turn_id")
		}
		return w.publisher.AppendFlat(ctx, events.StreamAgentTurnRequests, map[string]any{
			"type":            event.EventType,
			"outbox_event_id": event.ID.String(),
			"idempotency_key": event.IdempotencyKey,
			"turn_id":         turnID,
		})
	case "agent.turn.control.requested":
		required := []string{"turn_id", "run_id", "conversation_id", "agent_id", "backend", "claim_token", "reason"}
		fields := map[string]any{
			"type": event.EventType, "outbox_event_id": event.ID.String(),
			"idempotency_key": event.IdempotencyKey,
		}
		for _, key := range required {
			value, _ := body[key].(string)
			if value == "" {
				return permanentOutboxf("turn control outbox is missing %s", key)
			}
			fields[key] = value
		}
		attempt, ok := body["attempt"].(float64)
		if !ok || attempt < 1 {
			return permanentOutboxf("turn control outbox has invalid attempt")
		}
		fields["attempt"] = int(attempt)
		return w.publisher.AppendFlat(ctx, events.StreamAgentTurnControls, fields)
	case "agent.turn.finished":
		audience, err := w.repo.ResolveOutboxAudience(ctx, event)
		if err != nil {
			return err
		}
		body["project_id"] = audience.ProjectID.String()
		if audience.ActorUserID != nil {
			body["actor_user_id"] = audience.ActorUserID.String()
		}
		if audience.SessionID != nil {
			body["session_id"] = audience.SessionID.String()
		}
		return w.publisher.Publish(ctx, events.ChannelRealtime, map[string]any{
			"type": event.EventType, "payload": body,
		})
	case "agent.conclusion.published", "agent.conclusion.revised", "agent.conclusion.withdrawn":
		audience, err := w.repo.ResolveOutboxAudience(ctx, event)
		if err != nil {
			return err
		}
		body["project_id"] = audience.ProjectID.String()
		if audience.TaskID != nil {
			body["task_id"] = audience.TaskID.String()
		}
		return w.publisher.Publish(ctx, events.ChannelRealtime, map[string]any{
			"type": event.EventType, "payload": body,
		})
	default:
		return permanentOutboxf("unsupported agent outbox event type %q", event.EventType)
	}
}
