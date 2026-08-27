package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"github.com/Paca-AI/api/internal/events"
)

const (
	environmentCommandConsumerGroup = "api.environment_commands"
	environmentCommandReadBlock     = 5 * time.Second
	environmentCommandReadCount     = 10
)

// environmentLifecycleExecutor is the minimal environmentsvc.Service
// surface this consumer needs to actually execute a previously-queued
// create/start/stop command — see environmentsvc.Service.ExecuteStart's
// own doc comment for why these methods aren't part of
// environmentdom.Service (and so aren't reachable through the
// notificationdom.Service-style full-domain-interface pattern other
// consumers in this package use).
type environmentLifecycleExecutor interface {
	ExecuteCreate(ctx context.Context, environmentID uuid.UUID) error
	ExecuteStart(ctx context.Context, environmentID uuid.UUID) error
	ExecuteStop(ctx context.Context, environmentID uuid.UUID) error
}

// EnvironmentCommandConsumer reads queued environment lifecycle commands —
// "create", "start", and "stop" — from StreamEnvironmentCommands and
// executes them. This is the asynchronous counterpart to
// environmentsvc.Service.CreateEnvironment/StartEnvironment/
// StopEnvironment, which only validate and queue: moving the actual
// (potentially slow) agent-runner call here means the HTTP request that
// triggered it returns immediately regardless of how long provisioning/
// starting/stopping the backing container/Pod takes, instead of holding
// the connection open for it — which, past the server's own WriteTimeout,
// showed up to callers as a 502 for a start that was actually still
// succeeding server-side a few seconds later.
type EnvironmentCommandConsumer struct {
	client       *redis.Client
	svc          environmentLifecycleExecutor
	log          *slog.Logger
	consumerName string // unique per instance, derived from hostname
	stopCh       chan struct{}
	doneCh       chan struct{}
}

// NewEnvironmentCommandConsumer creates a consumer that is ready to be
// started. The consumer name is derived from the hostname so it is unique
// per pod/instance.
func NewEnvironmentCommandConsumer(client *redis.Client, svc environmentLifecycleExecutor, log *slog.Logger) *EnvironmentCommandConsumer {
	hostname, err := os.Hostname()
	if err != nil || hostname == "" {
		hostname = uuid.New().String()
	}
	return &EnvironmentCommandConsumer{
		client:       client,
		svc:          svc,
		log:          log,
		consumerName: fmt.Sprintf("%s.%s", environmentCommandConsumerGroup, hostname),
		stopCh:       make(chan struct{}),
		doneCh:       make(chan struct{}),
	}
}

// Start creates the consumer group if needed, then begins reading from the
// stream in a background goroutine. Call Stop to drain and exit cleanly.
func (c *EnvironmentCommandConsumer) Start(ctx context.Context) {
	// "0": first-ever creation processes any command that arrived before
	// the group existed (see ensureGroup's own doc comment on the
	// distinction from the NOGROUP recovery path in run() below).
	if err := c.ensureGroup(ctx, "0"); err != nil {
		c.log.Warn("environment command consumer: could not create consumer group, will retry on first read", "err", err)
	}

	go c.run()
}

// ensureGroup creates the consumer group at startID if it doesn't already
// exist. MKSTREAM ensures the stream key is created if it doesn't exist
// yet. startID is "0" only for Start's own first-ever creation — the
// NOGROUP recovery path in run() passes "$" instead, since recreating at
// "0" there would redeliver every command still retained on the stream
// (including ones already executed before the group vanished) with no
// idempotency guard: re-executing an old "start" is at worst a harmless
// no-op against an already-running environment, but re-executing an old
// "stop" is not — the environment could have been legitimately restarted
// in the meantime, and a stale replay would stop it again out from under
// whoever just started it. "$" (from now) avoids both concerns.
func (c *EnvironmentCommandConsumer) ensureGroup(ctx context.Context, startID string) error {
	err := c.client.XGroupCreateMkStream(ctx, events.StreamEnvironmentCommands, environmentCommandConsumerGroup, startID).Err()
	if err != nil && err.Error() != "BUSYGROUP Consumer Group name already exists" {
		return err
	}
	return nil
}

// Stop signals the consumer to stop and waits for the goroutine to exit.
func (c *EnvironmentCommandConsumer) Stop() {
	close(c.stopCh)
	<-c.doneCh
}

// run is the main loop executed in a goroutine by Start.
func (c *EnvironmentCommandConsumer) run() {
	defer close(c.doneCh)
	c.log.Info("environment command consumer: started", "stream", events.StreamEnvironmentCommands)

	// On startup, replay any pending messages (PEL) that were delivered but
	// never acknowledged (e.g. after a crash mid-execution). "0" fetches
	// the backlog.
	c.processPending(context.Background())

	for {
		select {
		case <-c.stopCh:
			c.log.Info("environment command consumer: stopping")
			return
		default:
		}

		ctx, cancel := context.WithTimeout(context.Background(), environmentCommandReadBlock+time.Second)
		msgs, err := c.client.XReadGroup(ctx, &redis.XReadGroupArgs{
			Group:    environmentCommandConsumerGroup,
			Consumer: c.consumerName,
			Streams:  []string{events.StreamEnvironmentCommands, ">"},
			Count:    environmentCommandReadCount,
			Block:    environmentCommandReadBlock,
		}).Result()
		cancel()

		if err != nil {
			if err == redis.Nil {
				continue
			}
			c.log.Error("environment command consumer: xreadgroup error", "err", err)
			if strings.Contains(err.Error(), "NOGROUP") {
				recoverCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				geErr := c.ensureGroup(recoverCtx, "$")
				cancel()
				if geErr != nil {
					c.log.Warn("environment command consumer: failed to recreate consumer group", "err", geErr)
				}
			}
			time.Sleep(2 * time.Second)
			continue
		}

		for _, stream := range msgs {
			for _, msg := range stream.Messages {
				c.handle(msg)
			}
		}
	}
}

// processPending re-delivers and acknowledges any messages in the PEL that
// were not acked during a previous run. Loops in batches of
// environmentCommandReadCount rather than reading once: each XReadGroup
// call from ID "0" starts at the head of the PEL, so once a batch is
// handled (and acked — see handle's own doc comment) the next call
// naturally advances to whatever's left. Without the loop, a backlog of
// more than one batch would leave the excess pending indefinitely — run()
// switches to reading only new messages (">") right after this returns, so
// nothing else would ever revisit them until the next process restart.
func (c *EnvironmentCommandConsumer) processPending(ctx context.Context) {
	for {
		msgs, err := c.client.XReadGroup(ctx, &redis.XReadGroupArgs{
			Group:    environmentCommandConsumerGroup,
			Consumer: c.consumerName,
			Streams:  []string{events.StreamEnvironmentCommands, "0"},
			Count:    environmentCommandReadCount,
		}).Result()
		if err != nil && err != redis.Nil {
			c.log.Warn("environment command consumer: could not read pending messages", "err", err)
			return
		}
		delivered := 0
		for _, stream := range msgs {
			for _, msg := range stream.Messages {
				c.handle(msg)
				delivered++
			}
		}
		if delivered < environmentCommandReadCount {
			return
		}
	}
}

// handle deserialises one stream message and executes the command it
// carries. Always acks, regardless of outcome: ExecuteCreate/ExecuteStart/
// ExecuteStop already persist StatusError with the failure reason
// directly onto the environment row on failure, so there is nothing left
// in the PEL worth retrying automatically — a user sees the error status
// and retries
// explicitly, which queues a fresh command.
func (c *EnvironmentCommandConsumer) handle(msg redis.XMessage) {
	ctx := context.Background()

	eventType, _ := msg.Values["type"].(string)
	raw, ok := msg.Values["payload"].(string)
	if !ok {
		c.log.Warn("environment command consumer: message has no payload field", "id", msg.ID)
		c.ack(ctx, msg.ID)
		return
	}

	var p environmentCommandPayload
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		c.log.Warn("environment command consumer: failed to decode payload", "id", msg.ID, "err", err)
		c.ack(ctx, msg.ID)
		return
	}
	environmentID, err := uuid.Parse(p.EnvironmentID)
	if err != nil {
		c.log.Warn("environment command consumer: invalid environment_id", "id", msg.ID, "environment_id", p.EnvironmentID)
		c.ack(ctx, msg.ID)
		return
	}

	switch eventType {
	case events.TopicEnvironmentCreate:
		if err := c.svc.ExecuteCreate(ctx, environmentID); err != nil {
			c.log.Error("environment command consumer: create failed", "environment_id", environmentID, "err", err)
		}
	case events.TopicEnvironmentStart:
		if err := c.svc.ExecuteStart(ctx, environmentID); err != nil {
			c.log.Error("environment command consumer: start failed", "environment_id", environmentID, "err", err)
		}
	case events.TopicEnvironmentStop:
		if err := c.svc.ExecuteStop(ctx, environmentID); err != nil {
			c.log.Error("environment command consumer: stop failed", "environment_id", environmentID, "err", err)
		}
	default:
		c.log.Warn("environment command consumer: unknown event type", "id", msg.ID, "type", eventType)
	}

	c.ack(ctx, msg.ID)
}

func (c *EnvironmentCommandConsumer) ack(ctx context.Context, id string) {
	if err := c.client.XAck(ctx, events.StreamEnvironmentCommands, environmentCommandConsumerGroup, id).Err(); err != nil {
		c.log.Warn("environment command consumer: xack failed", "id", id, "err", err)
	}
}

// environmentCommandPayload mirrors the JSON shape both
// environmentsvc.Service.StartEnvironment and StopEnvironment publish to
// StreamEnvironmentCommands — every command here concerns exactly one
// environment, so environment_id is all either one carries.
type environmentCommandPayload struct {
	EnvironmentID string `json:"environment_id"`
}
