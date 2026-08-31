package messaging

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

// environmentCommandConsumerGroup is this service's own consumer group for
// StreamAgentEnvironmentCommands — a separate stream/group from
// StreamAgentTriggers' consumerGroup above: environment provisioning is a
// different concern with a different concurrency profile (a handful of
// long-running Pod-wait loops, not many short conversation turns).
const environmentCommandConsumerGroup = "agent-runner-environment-commands"

const (
	environmentCommandReadBlock = 5 * time.Second
	environmentCommandReadCount = 20
	// environmentCommandReplyTTL bounds how long an orphaned reply key
	// lingers (the caller already gave up, or crashed before popping) —
	// comfortably longer than services/api's own BRPOP timeout
	// (aiAgentProvisionHTTPTimeout, 5 minutes) so it never expires while a
	// caller might still be waiting, short enough not to leak keys under
	// sustained load.
	environmentCommandReplyTTL = 10 * time.Minute
	// environmentCommandOpTimeout bounds each dispatched command's own
	// context — deliberately larger than services/api's paired BRPOP
	// timeout so this service never self-cancels an in-flight SandboxMgr
	// call at the exact moment the caller's own wait would already have
	// given up. See EnvironmentCommandConsumer's own doc comment on why
	// that context is independent of Run's.
	environmentCommandOpTimeout = 6 * time.Minute
	// environmentCommandFinalizeTimeout bounds the RPush/Expire/XAck calls
	// that finalize a command — independent of Run's ctx (see process's
	// own doc comment), so these need their own short-lived timeout rather
	// than running unbounded.
	environmentCommandFinalizeTimeout = 5 * time.Second
)

// EnvironmentCommandHandler processes one decoded environment command
// (create/start/restart_ports — see events.EnvironmentCommand* in
// topics.go) and returns its JSON response payload.
type EnvironmentCommandHandler func(ctx context.Context, cmdType string, payload json.RawMessage) (json.RawMessage, error)

// EnvironmentCommandReply is the JSON envelope RPushed onto a command's
// own reply_key — see EnvironmentReplyKey's doc comment.
type EnvironmentCommandReply struct {
	OK      bool            `json:"ok"`
	Error   string          `json:"error,omitempty"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

// EnvironmentCommandConsumer reads StreamAgentEnvironmentCommands via its
// own consumer group — a distinct type from Consumer above (different
// stream, different concern), not an extension of it.
//
// Unlike Consumer, this never replays its PEL backlog on startup
// (ensureGroup starts at "$", not "0" the way worker.EnvironmentCommandConsumer
// on the services/api side does): none of create/start/restart_ports has
// a row-status idempotency guard the way that api-side consumer's own
// ExecuteCreate/ExecuteStart do — replaying a stale command after a
// crash-restart could act on state a since-completed action already
// changed. A command lost to an agent-runner crash just times out the
// caller's own BRPOP — no worse than a mid-HTTP-request crash today.
type EnvironmentCommandConsumer struct {
	client       *redis.Client
	handler      EnvironmentCommandHandler
	log          *slog.Logger
	consumerName string
	sem          chan struct{}
}

// NewEnvironmentCommandConsumer builds a consumer bounding concurrent
// command handling to maxConcurrency — sized separately from
// WorkerConcurrency (agent conversation turns) since this consumer's work
// is a small number of long-running Pod-wait loops, not many short turns.
func NewEnvironmentCommandConsumer(client *redis.Client, maxConcurrency int, handler EnvironmentCommandHandler, log *slog.Logger) *EnvironmentCommandConsumer {
	hostname, err := os.Hostname()
	if err != nil || hostname == "" {
		hostname = "unknown"
	}
	if maxConcurrency <= 0 {
		maxConcurrency = 1
	}
	return &EnvironmentCommandConsumer{
		client:       client,
		handler:      handler,
		log:          log,
		consumerName: fmt.Sprintf("%s.%s", environmentCommandConsumerGroup, hostname),
		sem:          make(chan struct{}, maxConcurrency),
	}
}

// Run blocks, reading from StreamAgentEnvironmentCommands until ctx is
// cancelled. Mirrors Consumer.Run's shape and its identical shutdown
// caveat: this returns as soon as the read loop itself exits, and does
// not wait for in-flight process goroutines — each of those already
// carries its own independent context (see process's own doc comment),
// so a command already accepted keeps running, and attempts delivery of
// its result, regardless of this function having already returned.
func (c *EnvironmentCommandConsumer) Run(ctx context.Context) {
	if err := c.ensureGroup(ctx); err != nil {
		c.log.Error("environment command consumer: failed to create consumer group", "error", err)
		return
	}

	c.log.Info("environment command consumer: started", "stream", StreamAgentEnvironmentCommands, "group", environmentCommandConsumerGroup, "consumer", c.consumerName)

	for {
		select {
		case <-ctx.Done():
			c.log.Info("environment command consumer: stopping")
			return
		default:
		}

		streams, err := c.client.XReadGroup(ctx, &redis.XReadGroupArgs{
			Group:    environmentCommandConsumerGroup,
			Consumer: c.consumerName,
			Streams:  []string{StreamAgentEnvironmentCommands, ">"},
			Count:    environmentCommandReadCount,
			Block:    environmentCommandReadBlock,
		}).Result()
		if err != nil {
			if err == redis.Nil || ctx.Err() != nil {
				continue
			}
			c.log.Warn("environment command consumer: read error", "error", err)
			if strings.HasPrefix(err.Error(), "NOGROUP") {
				// Same self-heal reasoning as Consumer.Run's identical
				// branch — see that method's own doc comment.
				c.log.Warn("environment command consumer: self-healing consumer group after NOGROUP — any commands published in this gap were skipped, not replayed", "stream", StreamAgentEnvironmentCommands, "group", environmentCommandConsumerGroup)
				if reErr := c.ensureGroup(ctx); reErr != nil {
					c.log.Error("environment command consumer: failed to recreate consumer group after NOGROUP", "error", reErr)
				}
			}
			time.Sleep(time.Second)
			continue
		}

		for _, stream := range streams {
			for _, msg := range stream.Messages {
				go c.process(ctx, msg)
			}
		}
	}
}

// ensureGroup creates environmentCommandConsumerGroup on
// StreamAgentEnvironmentCommands if it doesn't already exist, starting
// from "$" — see EnvironmentCommandConsumer's own doc comment for why
// this never replays history, mirroring Consumer.ensureGroup's identical
// "$" choice and reasoning.
func (c *EnvironmentCommandConsumer) ensureGroup(ctx context.Context) error {
	if err := c.client.XGroupCreateMkStream(ctx, StreamAgentEnvironmentCommands, environmentCommandConsumerGroup, "$").Err(); err != nil {
		if err.Error() != "BUSYGROUP Consumer Group name already exists" {
			return err
		}
	}
	return nil
}

// process decodes msg, dispatches it to c.handler under the bounded
// semaphore, and RPushes a reply. Only the semaphore-acquisition wait
// uses ctx (Run's own, tied to agent-runner's shutdown signal) — refusing
// to *start* new work once shutdown begins. Everything after a slot is
// acquired deliberately uses independent contexts instead:
// environmentCommandOpTimeout (not ctx) for the handler call itself, so a
// caller's own BRPOP giving up — or this process's own shutdown signal —
// can never abort an in-flight SandboxMgr mutation; and a short
// background-derived timeout for the final RPush/Expire/XAck, so a
// shutdown racing a command's completion doesn't strand a successfully
// computed result un-delivered and unacked.
//
// Always acks, regardless of outcome: like services/api's own
// worker.EnvironmentCommandConsumer's identical reasoning, the reply's own
// ok/error is the only outcome that matters to the caller — there is
// nothing left in the PEL worth retrying automatically.
func (c *EnvironmentCommandConsumer) process(ctx context.Context, msg redis.XMessage) {
	cmdType, _ := msg.Values["type"].(string)
	replyKey, _ := msg.Values["reply_key"].(string)
	payloadRaw, _ := msg.Values["payload"].(string)

	finalize := func(reply EnvironmentCommandReply) {
		fctx, cancel := context.WithTimeout(context.Background(), environmentCommandFinalizeTimeout)
		defer cancel()
		if replyKey != "" {
			if replyBytes, err := json.Marshal(reply); err != nil {
				c.log.Error("environment command consumer: failed to marshal reply", "id", msg.ID, "type", cmdType, "error", err)
			} else if err := c.client.RPush(fctx, replyKey, replyBytes).Err(); err != nil {
				c.log.Warn("environment command consumer: failed to push reply", "id", msg.ID, "type", cmdType, "reply_key", replyKey, "error", err)
			} else if err := c.client.Expire(fctx, replyKey, environmentCommandReplyTTL).Err(); err != nil {
				c.log.Warn("environment command consumer: failed to set reply key ttl", "id", msg.ID, "reply_key", replyKey, "error", err)
			}
		}
		if err := c.client.XAck(fctx, StreamAgentEnvironmentCommands, environmentCommandConsumerGroup, msg.ID).Err(); err != nil {
			c.log.Warn("environment command consumer: ack failed", "id", msg.ID, "error", err)
		}
	}

	if replyKey == "" {
		c.log.Error("environment command consumer: dropping message with no reply_key", "id", msg.ID, "type", cmdType)
		finalize(EnvironmentCommandReply{OK: false, Error: "malformed command: no reply_key"})
		return
	}

	select {
	case c.sem <- struct{}{}:
	case <-ctx.Done():
		return
	}
	defer func() { <-c.sem }()

	opCtx, cancel := context.WithTimeout(context.Background(), environmentCommandOpTimeout)
	defer cancel()

	respPayload, err := c.handler(opCtx, cmdType, json.RawMessage(payloadRaw))
	reply := EnvironmentCommandReply{OK: err == nil, Payload: respPayload}
	if err != nil {
		reply.Error = err.Error()
	}
	finalize(reply)
}
