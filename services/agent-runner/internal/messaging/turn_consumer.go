package messaging

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"github.com/Paca-AI/agent-runner/internal/convlock"
)

const (
	turnConsumerGroup = "agent-turn-workers"
	turnReadBlock     = 5 * time.Second
	turnReadCount     = 20
	turnReclaimIdle   = 45 * time.Second
	turnReclaimPoll   = 10 * time.Second
)

type TurnHandler func(ctx context.Context, turnID uuid.UUID) error

// TurnConsumer is deliberately separate from the legacy conversation trigger
// consumer. It starts at 0-0, reclaims abandoned pending entries, and does not
// ACK until the authoritative turn is terminal (or already terminal).
type TurnConsumer struct {
	client       *redis.Client
	handler      TurnHandler
	log          *slog.Logger
	consumerName string
	sem          chan struct{}
	turnLocks    *convlock.Locks
	active       sync.Map
}

func NewTurnConsumer(client *redis.Client, maxConcurrency int, handler TurnHandler, log *slog.Logger) *TurnConsumer {
	hostname, err := os.Hostname()
	if err != nil || hostname == "" {
		hostname = "unknown"
	}
	if maxConcurrency <= 0 {
		maxConcurrency = 1
	}
	return &TurnConsumer{
		client: client, handler: handler, log: log,
		consumerName: fmt.Sprintf("%s.%s.%s", turnConsumerGroup, hostname, uuid.NewString()),
		sem:          make(chan struct{}, maxConcurrency), turnLocks: convlock.New(),
	}
}

func (c *TurnConsumer) Run(ctx context.Context) {
	if !ensureStreamGroup(ctx, c.client, StreamAgentTurnRequests, turnConsumerGroup, c.log, "turn consumer") {
		return
	}
	c.log.Info("turn consumer: started", "stream", StreamAgentTurnRequests, "group", turnConsumerGroup, "consumer", c.consumerName)
	go c.reclaimLoop(ctx)
	for ctx.Err() == nil {
		streams, err := c.client.XReadGroup(ctx, &redis.XReadGroupArgs{
			Group: turnConsumerGroup, Consumer: c.consumerName,
			Streams: []string{StreamAgentTurnRequests, ">"}, Count: turnReadCount, Block: turnReadBlock,
		}).Result()
		if err != nil {
			if err == redis.Nil || ctx.Err() != nil {
				continue
			}
			c.log.Warn("turn consumer: read error", "error", err)
			continue
		}
		for _, stream := range streams {
			for _, message := range stream.Messages {
				c.dispatch(ctx, message)
			}
		}
	}
}

func (c *TurnConsumer) reclaimLoop(ctx context.Context) {
	ticker := time.NewTicker(turnReclaimPoll)
	defer ticker.Stop()
	start := "0-0"
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		start = c.reclaimPending(ctx, start, turnReclaimIdle)
	}
}

func (c *TurnConsumer) reclaimPending(ctx context.Context, start string, minIdle time.Duration) string {
	messages, next, err := c.client.XAutoClaim(ctx, &redis.XAutoClaimArgs{
		Stream: StreamAgentTurnRequests, Group: turnConsumerGroup,
		Consumer: c.consumerName, MinIdle: minIdle, Start: start, Count: turnReadCount,
	}).Result()
	if err != nil {
		if err != redis.Nil && ctx.Err() == nil {
			c.log.Warn("turn consumer: reclaim error", "error", err)
		}
		return "0-0"
	}
	for _, message := range messages {
		c.dispatch(ctx, message)
	}
	if next == "0-0" || next == "0" {
		return "0-0"
	}
	return next
}

func (c *TurnConsumer) dispatch(ctx context.Context, message redis.XMessage) {
	raw, _ := message.Values["turn_id"].(string)
	turnID, err := uuid.Parse(raw)
	if err != nil {
		c.log.Error("turn consumer: dropping malformed request", "id", message.ID, "turn_id", raw)
		c.ack(ctx, message.ID)
		return
	}
	if _, loaded := c.active.LoadOrStore(message.ID, struct{}{}); loaded {
		return
	}
	go func() {
		defer c.active.Delete(message.ID)
		heartbeatCtx, stopHeartbeat := context.WithCancel(ctx)
		heartbeatDone := make(chan struct{})
		go func() {
			defer close(heartbeatDone)
			c.heartbeatPending(heartbeatCtx, message.ID)
		}()
		defer func() {
			stopHeartbeat()
			<-heartbeatDone
		}()
		unlock := c.turnLocks.Lock(turnID)
		defer unlock()
		select {
		case c.sem <- struct{}{}:
		case <-ctx.Done():
			return
		}
		defer func() { <-c.sem }()
		err = c.handler(ctx, turnID)
		if err != nil {
			c.log.Warn("turn consumer: handler error; leaving request pending", "id", message.ID, "turn_id", turnID, "error", err)
			return
		}
		c.ack(ctx, message.ID)
	}()
}

func ensureStreamGroup(ctx context.Context, client *redis.Client, stream, group string, log *slog.Logger, component string) bool {
	options := *client.Options()
	options.MaxRetries = 0
	options.Protocol = 2
	options.DialTimeout = 2 * time.Second
	options.ReadTimeout = 2 * time.Second
	options.WriteTimeout = 2 * time.Second
	options.ContextTimeoutEnabled = true
	bootstrapClient := redis.NewClient(&options)
	defer func() { _ = bootstrapClient.Close() }()
	backoff := 100 * time.Millisecond
	for ctx.Err() == nil {
		attemptCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		err := bootstrapClient.XGroupCreateMkStream(attemptCtx, stream, group, "0-0").Err()
		cancel()
		if err == nil || strings.Contains(err.Error(), "BUSYGROUP") {
			return true
		}
		log.Warn(component+": failed to create group; retrying", "stream", stream, "error", err, "backoff", backoff)
		timer := time.NewTimer(backoff)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return false
		case <-timer.C:
		}
		if backoff < 5*time.Second {
			backoff *= 2
			if backoff > 5*time.Second {
				backoff = 5 * time.Second
			}
		}
	}
	return false
}

func (c *TurnConsumer) heartbeatPending(ctx context.Context, messageID string) {
	ticker := time.NewTicker(turnReclaimIdle / 3)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_, err := c.client.XClaimJustID(ctx, &redis.XClaimArgs{
				Stream: StreamAgentTurnRequests, Group: turnConsumerGroup,
				Consumer: c.consumerName, MinIdle: 0, Messages: []string{messageID},
			}).Result()
			if err != nil && err != redis.Nil && ctx.Err() == nil {
				c.log.Warn("turn consumer: pending heartbeat failed", "id", messageID, "error", err)
			}
		}
	}
}

func (c *TurnConsumer) ack(ctx context.Context, id string) {
	if err := c.client.XAck(ctx, StreamAgentTurnRequests, turnConsumerGroup, id).Err(); err != nil && ctx.Err() == nil {
		c.log.Warn("turn consumer: ack failed", "id", id, "error", err)
		return
	}
	if err := c.client.XDel(ctx, StreamAgentTurnRequests, id).Err(); err != nil && ctx.Err() == nil {
		c.log.Warn("turn consumer: stream retention delete failed", "id", id, "error", err)
	}
}
