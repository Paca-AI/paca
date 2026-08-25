package messaging

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/Paca-AI/agent-runner/internal/agent"
	"github.com/Paca-AI/agent-runner/internal/convlock"
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
	convLocks      *convlock.Locks
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
		convLocks:      convlock.New(),
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
	if err := c.ensureGroup(ctx); err != nil {
		c.log.Error("consumer: failed to create consumer group", "error", err)
		return
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
			if strings.HasPrefix(err.Error(), "NOGROUP") {
				// The stream and/or consumer group vanished out from under
				// an already-running consumer — a Valkey restart without
				// persistence, a FLUSHALL, or a manual XGROUP DESTROY.
				// Without this, every subsequent XReadGroup call keeps
				// failing the exact same way forever: nothing else ever
				// recreates the group, so this consumer would otherwise sit
				// here logging "read error" indefinitely and never process
				// another trigger again until the process itself restarts.
				// Self-heal the same way startup does.
				//
				// Logged at Warn, not just the read-error Warn above: see
				// ensureGroup's own doc comment on why recreating from "$"
				// can silently skip entries in one narrow case (the group
				// alone was destroyed, not the stream) — worth a
				// deliberately loud, greppable line so that case is at
				// least diagnosable after the fact, even though nothing
				// here can recover the skipped entries themselves.
				c.log.Warn("consumer: self-healing consumer group after NOGROUP — any triggers published in this gap were skipped, not replayed", "stream", StreamAgentTriggers, "group", consumerGroup)
				if reErr := c.ensureGroup(ctx); reErr != nil {
					c.log.Error("consumer: failed to recreate consumer group after NOGROUP", "error", reErr)
				}
			}
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

// ensureGroup creates consumerGroup on StreamAgentTriggers if it doesn't
// already exist — called once at Run's startup, and again by its read loop
// whenever a NOGROUP error shows the group (or the stream itself) has
// disappeared out from under an already-running consumer.
//
// "$" — only entries appended after this group is (re)created, mirroring
// core/streams.py's ensure_consumer_group (xgroup_create(..., id="$", ...))
// exactly. A prior version of this line used "0" (the entire stream from
// the beginning) — found live, the hard way: a first-ever deployment of
// this service against a real dev Valkey replayed every trigger the stream
// had ever held, since a brand-new consumer group has no delivery history
// of its own to resume from and "0" means "start from the very first entry
// still in the stream."
//
// The same reasoning fully justifies "$" for ordinary startup and for a
// self-heal recreate when the *stream itself* is gone (a Valkey restart
// without persistence, a FLUSHALL) — there is genuinely nothing left to
// resume from either way. It's weaker for a self-heal recreate triggered
// by a manual `XGROUP DESTROY` that leaves the stream itself intact: any
// entries still sitting in the stream, published before this group's
// (re)creation notices NOGROUP and calls this, are skipped rather than
// redelivered ("$" only ever means "start from now"), since nothing else
// remembers this consumer's prior read position once its group is gone.
// Accepted rather than fixed: the only alternative ("0") reintroduces the
// full-history-replay regression above on every ordinary startup, which is
// strictly the more common case. The read loop's own NOGROUP branch logs a
// Warn specifically so this narrower, rarer gap is at least diagnosable in
// production.
func (c *Consumer) ensureGroup(ctx context.Context) error {
	if err := c.client.XGroupCreateMkStream(ctx, StreamAgentTriggers, consumerGroup, "$").Err(); err != nil {
		if err.Error() != "BUSYGROUP Consumer Group name already exists" {
			return err
		}
	}
	return nil
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

// ack acknowledges msg, logging (not returning) a failure to do so — used
// identically by both processControl and processTrigger, on both the
// malformed-message-drop path and the successful-handling path.
func (c *Consumer) ack(ctx context.Context, msg redis.XMessage) {
	if err := c.client.XAck(ctx, StreamAgentTriggers, consumerGroup, msg.ID).Err(); err != nil {
		c.log.Warn("consumer: ack failed", "id", msg.ID, "error", err)
	}
}

func (c *Consumer) processControl(ctx context.Context, msg redis.XMessage, controlType ControlType) {
	control, err := decodeControl(controlType, msg.Values)
	if err != nil {
		c.log.Error("consumer: dropping malformed control message", "id", msg.ID, "error", err)
		c.ack(ctx, msg)
		return
	}
	if err := c.controlHandler(ctx, control); err != nil {
		c.log.Warn("consumer: control handler error — leaving unacknowledged for redelivery",
			"id", msg.ID, "conversation_id", control.ConversationID, "error", err)
		return
	}
	c.ack(ctx, msg)
}

func (c *Consumer) processTrigger(ctx context.Context, msg redis.XMessage) {
	trigger, err := decodeTrigger(msg.Values)
	if err != nil {
		// A malformed message can never become well-formed on redelivery —
		// ack it so it doesn't block the stream forever, and log loudly
		// since this indicates a real producer/consumer schema drift.
		c.log.Error("consumer: dropping malformed trigger", "id", msg.ID, "error", err)
		c.ack(ctx, msg)
		return
	}

	// Serializes trigger handling per conversation_id: two triggers for the
	// same conversation must never run Handler concurrently, or they race
	// ConversationRepository.NextEventIndex (read once per turn, then
	// incremented purely in-memory, so two concurrent turns can compute the
	// same starting index and silently lose one turn's events to
	// InsertEvent's ON CONFLICT DO NOTHING) and the in-flight registry's
	// Register/Unregister pairing. Triggers for different conversations are
	// unaffected and still run in parallel up to sem's limit.
	//
	// Acquired before the semaphore below, not after: a second trigger for
	// a conversation that already has a turn in flight should queue here
	// (a cheap, uncontended-for-everyone-else wait) rather than occupy one
	// of the limited semaphore slots while blocked.
	unlockConv := c.convLocks.Lock(trigger.ConversationID)
	defer unlockConv()

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

	c.ack(ctx, msg)
}
