package messaging

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

const (
	turnControlGroup       = "agent-turn-control-workers"
	turnControlReclaimIdle = 15 * time.Second
)

type TurnControl struct {
	TurnID         uuid.UUID
	RunID          uuid.UUID
	ConversationID uuid.UUID
	AgentID        uuid.UUID
	ClaimToken     uuid.UUID
	Attempt        int
	Backend        string
	Reason         string
}

type TurnControlHandler func(context.Context, TurnControl) error

// TurnControlConsumer delivers durable owner/deadline/revocation stops. The
// stream is a work queue because any runner can forward an ACP stop through
// the shared bridge registry; active LLM workers additionally poll the
// authoritative API, so delivery to a different replica cannot lose a stop.
type TurnControlConsumer struct {
	client       *redis.Client
	handler      TurnControlHandler
	log          *slog.Logger
	consumerName string
}

func NewTurnControlConsumer(client *redis.Client, handler TurnControlHandler, log *slog.Logger) *TurnControlConsumer {
	host, err := os.Hostname()
	if err != nil || host == "" {
		host = "unknown"
	}
	return &TurnControlConsumer{client: client, handler: handler, log: log,
		consumerName: fmt.Sprintf("%s.%s.%s", turnControlGroup, host, uuid.NewString())}
}

func (c *TurnControlConsumer) Run(ctx context.Context) {
	if !ensureStreamGroup(ctx, c.client, StreamAgentTurnControls, turnControlGroup, c.log, "turn control consumer") {
		return
	}
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	reclaimCursor := "0-0"
	for ctx.Err() == nil {
		streams, err := c.client.XReadGroup(ctx, &redis.XReadGroupArgs{
			Group: turnControlGroup, Consumer: c.consumerName,
			Streams: []string{StreamAgentTurnControls, ">"}, Count: 20, Block: 5 * time.Second,
		}).Result()
		if err != nil && err != redis.Nil && ctx.Err() == nil {
			c.log.Warn("turn control consumer: read error", "error", err)
		}
		for _, stream := range streams {
			for _, message := range stream.Messages {
				c.process(ctx, message)
			}
		}
		select {
		case <-ticker.C:
			reclaimCursor = c.reclaimPending(ctx, reclaimCursor, turnControlReclaimIdle)
		default:
		}
	}
}

func (c *TurnControlConsumer) reclaimPending(ctx context.Context, start string, minIdle time.Duration) string {
	messages, next, err := c.client.XAutoClaim(ctx, &redis.XAutoClaimArgs{
		Stream: StreamAgentTurnControls, Group: turnControlGroup,
		Consumer: c.consumerName, MinIdle: minIdle, Start: start, Count: 20,
	}).Result()
	if err != nil {
		if err != redis.Nil && ctx.Err() == nil {
			c.log.Warn("turn control consumer: reclaim error", "error", err)
		}
		return "0-0"
	}
	for _, message := range messages {
		c.process(ctx, message)
	}
	if next == "0" || next == "0-0" {
		return "0-0"
	}
	return next
}

func (c *TurnControlConsumer) process(ctx context.Context, message redis.XMessage) {
	control, err := decodeTurnControl(message.Values)
	if err != nil {
		c.log.Error("turn control consumer: dropping malformed control", "id", message.ID, "error", err)
		_ = c.client.XAck(ctx, StreamAgentTurnControls, turnControlGroup, message.ID).Err()
		return
	}
	if err := c.handler(ctx, control); err != nil {
		c.log.Warn("turn control consumer: handler error; leaving pending", "id", message.ID, "turn_id", control.TurnID, "error", err)
		return
	}
	if err := c.client.XAck(ctx, StreamAgentTurnControls, turnControlGroup, message.ID).Err(); err != nil && ctx.Err() == nil {
		c.log.Warn("turn control consumer: ack failed", "id", message.ID, "error", err)
		return
	}
	if err := c.client.XDel(ctx, StreamAgentTurnControls, message.ID).Err(); err != nil && ctx.Err() == nil {
		c.log.Warn("turn control consumer: stream retention delete failed", "id", message.ID, "error", err)
	}
}

func decodeTurnControl(values map[string]any) (TurnControl, error) {
	parseID := func(key string) (uuid.UUID, error) {
		return uuid.Parse(fmt.Sprint(values[key]))
	}
	turnID, err := parseID("turn_id")
	if err != nil {
		return TurnControl{}, fmt.Errorf("turn_id: %w", err)
	}
	runID, err := parseID("run_id")
	if err != nil {
		return TurnControl{}, fmt.Errorf("run_id: %w", err)
	}
	conversationID, err := parseID("conversation_id")
	if err != nil {
		return TurnControl{}, fmt.Errorf("conversation_id: %w", err)
	}
	agentID, err := parseID("agent_id")
	if err != nil {
		return TurnControl{}, fmt.Errorf("agent_id: %w", err)
	}
	claimToken, err := parseID("claim_token")
	if err != nil {
		return TurnControl{}, fmt.Errorf("claim_token: %w", err)
	}
	attempt, err := strconv.Atoi(fmt.Sprint(values["attempt"]))
	if err != nil || attempt < 1 {
		return TurnControl{}, fmt.Errorf("attempt is invalid")
	}
	backend, reason := fmt.Sprint(values["backend"]), fmt.Sprint(values["reason"])
	if backend == "" || reason == "" {
		return TurnControl{}, fmt.Errorf("backend and reason are required")
	}
	return TurnControl{TurnID: turnID, RunID: runID, ConversationID: conversationID,
		AgentID: agentID, ClaimToken: claimToken, Attempt: attempt, Backend: backend, Reason: reason}, nil
}
