package messaging

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/Paca-AI/agent-runner/internal/agent"
)

// consumerGroup is this service's own Valkey Stream consumer group name.
// agent-runner is the only consumer of paca:agent:triggers — see
// config.Gate's doc comment for the now-retired Python
// services/ai-agent counterpart this used to have to coordinate with.
const consumerGroup = "agent-runner-workers"

const (
	readBlock = 5 * time.Second
	readCount = 20
)

// Handler processes one decoded trigger. Returning an error leaves the
// message unacknowledged for redelivery — Consumer does not retry
// in-process, matching services/api's messaging.Consumer contract.
type Handler func(ctx context.Context, t agent.Trigger) error

// ControlHandler processes one decoded stop/pause/heartbeat directive for
// an already-running conversation.
type ControlHandler func(ctx context.Context, c Control) error

// Consumer reads paca:agent:triggers via consumerGroup and routes each
// entry to Handler (a new-conversation trigger) or ControlHandler (a
// directive for one already running) based on the entry's "type" field —
// mirrors core/streams.py's read_triggers, which does the same
// discrimination before choosing TriggerMessage vs. ControlMessage.
//
// Both handlers run in their own goroutine rather than blocking this
// struct's read loop — trigger goroutines gated by a bounded semaphore
// (maxConcurrency), control-message goroutines never gated (they must
// stay responsive even while every trigger slot is busy — a stop signal
// arriving for a conversation that's itself occupying the last slot would
// otherwise never be read at all). This is a deliberate departure from an
// earlier version of this Consumer, which processed messages one at a
// time inline: that meant this process could never see a stop/pause
// message for a conversation still in flight, since reading the *next*
// stream entry was blocked behind finishing the current one.
type Consumer struct {
	client         *redis.Client
	handler        Handler
	controlHandler ControlHandler
	log            *slog.Logger
	consumerName   string
	sem            chan struct{}
}

// NewConsumer builds a Consumer that reads paca:agent:triggers via a
// per-hostname consumer name, bounding concurrent trigger handling to
// maxConcurrency.
func NewConsumer(client *redis.Client, maxConcurrency int, handler Handler, controlHandler ControlHandler, log *slog.Logger) *Consumer {
	hostname, err := os.Hostname()
	if err != nil || hostname == "" {
		hostname = "unknown"
	}
	if maxConcurrency <= 0 {
		maxConcurrency = 1
	}
	return &Consumer{
		client:         client,
		handler:        handler,
		controlHandler: controlHandler,
		log:            log,
		consumerName:   fmt.Sprintf("%s.%s", consumerGroup, hostname),
		sem:            make(chan struct{}, maxConcurrency),
	}
}

// Run blocks, reading from StreamAgentTriggers until ctx is cancelled.
//
// Shutdown is deliberately not graceful in the sense of waiting for
// in-flight trigger goroutines to finish: this returns as soon as the read
// loop itself exits. Each in-flight executor.Run call still tears its own
// sandbox down correctly regardless (it uses context.WithoutCancel for
// that specific step — see executor.go) — what's not guaranteed is that
// this function's return means every conversation has actually stopped.
// Acceptable for now; revisit if a slow, coordinated shutdown ever matters
// more than a fast one.
func (c *Consumer) Run(ctx context.Context) {
	// "$" — only entries appended after this group is created, mirroring
	// core/streams.py's ensure_consumer_group (xgroup_create(..., id="$",
	// ...)) exactly. A prior version of this line used "0" (the entire
	// stream from the beginning) — found live, the hard way: a first-ever
	// deployment of this service against a real dev Valkey replayed every
	// trigger the stream had ever held, since a brand-new consumer group
	// has no delivery history of its own to resume from and "0" means
	// "start from the very first entry still in the stream."
	if err := c.client.XGroupCreateMkStream(ctx, StreamAgentTriggers, consumerGroup, "$").Err(); err != nil {
		if err.Error() != "BUSYGROUP Consumer Group name already exists" {
			c.log.Error("consumer: failed to create consumer group", "error", err)
			return
		}
	}

	c.log.Info("consumer: started", "stream", StreamAgentTriggers, "group", consumerGroup, "consumer", c.consumerName)

	for {
		select {
		case <-ctx.Done():
			c.log.Info("consumer: stopping")
			return
		default:
		}

		streams, err := c.client.XReadGroup(ctx, &redis.XReadGroupArgs{
			Group:    consumerGroup,
			Consumer: c.consumerName,
			Streams:  []string{StreamAgentTriggers, ">"},
			Count:    readCount,
			Block:    readBlock,
		}).Result()
		if err != nil {
			if err == redis.Nil || ctx.Err() != nil {
				continue
			}
			c.log.Warn("consumer: read error", "error", err)
			time.Sleep(time.Second)
			continue
		}

		for _, stream := range streams {
			for _, msg := range stream.Messages {
				c.dispatch(ctx, msg)
			}
		}
	}
}

// dispatch decides, cheaply and synchronously, whether msg is a control
// directive or a trigger, then hands it off to the right goroutine shape.
// The decode itself is cheap either way; only the handler call is
// deferred to a goroutine.
func (c *Consumer) dispatch(ctx context.Context, msg redis.XMessage) {
	rawType, _ := msg.Values["type"].(string)
	if controlType, ok := controlTypes[rawType]; ok {
		go c.processControl(ctx, msg, controlType)
		return
	}
	go c.processTrigger(ctx, msg)
}

func (c *Consumer) processControl(ctx context.Context, msg redis.XMessage, controlType ControlType) {
	control, err := decodeControl(controlType, msg.Values)
	if err != nil {
		c.log.Error("consumer: dropping malformed control message", "id", msg.ID, "error", err)
		_ = c.client.XAck(ctx, StreamAgentTriggers, consumerGroup, msg.ID).Err()
		return
	}
	if err := c.controlHandler(ctx, control); err != nil {
		c.log.Warn("consumer: control handler error — leaving unacknowledged for redelivery",
			"id", msg.ID, "conversation_id", control.ConversationID, "error", err)
		return
	}
	if err := c.client.XAck(ctx, StreamAgentTriggers, consumerGroup, msg.ID).Err(); err != nil {
		c.log.Warn("consumer: ack failed", "id", msg.ID, "error", err)
	}
}

func (c *Consumer) processTrigger(ctx context.Context, msg redis.XMessage) {
	trigger, err := decodeTrigger(msg.Values)
	if err != nil {
		// A malformed message can never become well-formed on redelivery —
		// ack it so it doesn't block the stream forever, and log loudly
		// since this indicates a real producer/consumer schema drift.
		c.log.Error("consumer: dropping malformed trigger", "id", msg.ID, "error", err)
		_ = c.client.XAck(ctx, StreamAgentTriggers, consumerGroup, msg.ID).Err()
		return
	}

	select {
	case c.sem <- struct{}{}:
	case <-ctx.Done():
		return
	}
	defer func() { <-c.sem }()

	if err := c.handler(ctx, trigger); err != nil {
		c.log.Warn("consumer: handler error — leaving unacknowledged for redelivery",
			"id", msg.ID, "conversation_id", trigger.ConversationID, "error", err)
		return
	}

	if err := c.client.XAck(ctx, StreamAgentTriggers, consumerGroup, msg.ID).Err(); err != nil {
		c.log.Warn("consumer: ack failed", "id", msg.ID, "error", err)
	}
}
