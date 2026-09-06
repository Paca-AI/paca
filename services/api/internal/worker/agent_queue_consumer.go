package worker

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	agentdom "github.com/Paca-AI/api/internal/domain/agent"
	"github.com/Paca-AI/api/internal/events"
)

const (
	agentQueueConsumerGroup = "api.agent_queue"
	agentQueueReadBlock     = 5 * time.Second
	agentQueueReadCount     = 50
)

// agentQueueConversationFinder is the minimal repository surface this
// consumer needs: resolving a terminal-status event's conversation_id back
// to the agent it belongs to (the status payload carries only
// conversation_id/status — see agentConversationStatusPayload).
type agentQueueConversationFinder interface {
	FindConversationByID(ctx context.Context, id uuid.UUID) (*agentdom.AgentConversation, error)
}

// agentQueueAdvancer is the minimal agentsvc.Service surface this consumer
// needs — see agentsvc.Service.AdvanceQueue and AdvanceFolderQueue's doc
// comments.
type agentQueueAdvancer interface {
	AdvanceQueue(ctx context.Context, agentID uuid.UUID, maxDispatch int) (dispatched int, err error)
	AdvanceFolderQueue(ctx context.Context, environmentID uuid.UUID, folderID *uuid.UUID) (dispatchedOne bool, err error)
}

// AgentQueueConsumer reads StreamAgentConversationStatus (the same stream
// worker.AutomationConsumer reads for trigger_ai_agent resume, via its own
// independent consumer group — Valkey Streams support many independent
// groups on one stream) and, whenever a conversation reaches a terminal
// status, calls AdvanceQueue for its agent: exactly one running slot just
// freed, so at most one backlogged conversation (agentdom.PendingTrigger)
// is dispatched in response — see AdvanceQueue's doc comment for why
// "exactly one" matters here (running conversation counts don't reflect a
// conversation this same call just dispatched).
//
// A raised agents.parallelism_limit is the other way a slot can free up —
// that one has no per-slot event to react to, so it's instead handled
// synchronously and immediately inside agentsvc.Service.UpdateAgent/
// UpdateGlobalAgent. Between the two, every way capacity can increase is
// covered without a poller.
type AgentQueueConsumer struct {
	client       *redis.Client
	repo         agentQueueConversationFinder
	advancer     agentQueueAdvancer
	log          *slog.Logger
	consumerName string
	stopCh       chan struct{}
	doneCh       chan struct{}
}

// NewAgentQueueConsumer creates a consumer that is ready to be started.
func NewAgentQueueConsumer(client *redis.Client, repo agentQueueConversationFinder, advancer agentQueueAdvancer, log *slog.Logger) *AgentQueueConsumer {
	hostname, err := os.Hostname()
	if err != nil || hostname == "" {
		hostname = uuid.New().String()
	}
	return &AgentQueueConsumer{
		client:       client,
		repo:         repo,
		advancer:     advancer,
		log:          log,
		consumerName: fmt.Sprintf("%s.%s", agentQueueConsumerGroup, hostname),
		stopCh:       make(chan struct{}),
		doneCh:       make(chan struct{}),
	}
}

// Start creates the consumer group if needed, then begins reading from the
// stream in a background goroutine. Call Stop to drain and exit cleanly.
func (c *AgentQueueConsumer) Start(ctx context.Context) {
	if err := c.ensureGroup(ctx, "0"); err != nil {
		c.log.Warn("agent queue consumer: could not create consumer group, will retry on first read", "err", err)
	}
	go c.run()
}

// ensureGroup creates the consumer group at startID if it doesn't already
// exist — "0" on ordinary startup (so a fresh deployment still picks up any
// terminal-status entries published before this consumer group first
// existed, unlike a brand-new group elsewhere in this package that starts
// from "$"); "$" on the NOGROUP self-heal in run() below, since there is
// nothing left to resume from once the group (or stream) itself is gone.
func (c *AgentQueueConsumer) ensureGroup(ctx context.Context, startID string) error {
	err := c.client.XGroupCreateMkStream(ctx, events.StreamAgentConversationStatus, agentQueueConsumerGroup, startID).Err()
	if err != nil && err.Error() != "BUSYGROUP Consumer Group name already exists" {
		return err
	}
	return nil
}

// Stop signals the consumer to stop and waits for the goroutine to exit.
func (c *AgentQueueConsumer) Stop() {
	close(c.stopCh)
	<-c.doneCh
}

func (c *AgentQueueConsumer) run() {
	defer close(c.doneCh)
	c.log.Info("agent queue consumer: started", "stream", events.StreamAgentConversationStatus)

	c.processPending(context.Background())

	for {
		select {
		case <-c.stopCh:
			c.log.Info("agent queue consumer: stopping")
			return
		default:
		}

		ctx, cancel := context.WithTimeout(context.Background(), agentQueueReadBlock+time.Second)
		msgs, err := c.client.XReadGroup(ctx, &redis.XReadGroupArgs{
			Group:    agentQueueConsumerGroup,
			Consumer: c.consumerName,
			Streams:  []string{events.StreamAgentConversationStatus, ">"},
			Count:    agentQueueReadCount,
			Block:    agentQueueReadBlock,
		}).Result()
		cancel()

		if err != nil {
			if err == redis.Nil {
				continue
			}
			c.log.Error("agent queue consumer: xreadgroup error", "err", err)
			if strings.Contains(err.Error(), "NOGROUP") {
				recoverCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				geErr := c.ensureGroup(recoverCtx, "$")
				cancel()
				if geErr != nil {
					c.log.Warn("agent queue consumer: failed to recreate consumer group", "err", geErr)
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
// were not acked during a previous run.
func (c *AgentQueueConsumer) processPending(ctx context.Context) {
	msgs, err := c.client.XReadGroup(ctx, &redis.XReadGroupArgs{
		Group:    agentQueueConsumerGroup,
		Consumer: c.consumerName,
		Streams:  []string{events.StreamAgentConversationStatus, "0"},
		Count:    agentQueueReadCount,
	}).Result()
	if err != nil && err != redis.Nil {
		c.log.Warn("agent queue consumer: could not read pending messages", "err", err)
		return
	}
	for _, stream := range msgs {
		for _, msg := range stream.Messages {
			c.handle(msg)
		}
	}
}

func (c *AgentQueueConsumer) ack(ctx context.Context, id string) {
	if err := c.client.XAck(ctx, events.StreamAgentConversationStatus, agentQueueConsumerGroup, id).Err(); err != nil {
		c.log.Warn("agent queue consumer: xack failed", "id", id, "err", err)
	}
}

// handle mirrors automation_consumer.go's handleAgentConversationStatus own
// decode of this same stream's payload shape (conversation_id/status).
func (c *AgentQueueConsumer) handle(msg redis.XMessage) {
	ctx := context.Background()

	convIDStr, _ := msg.Values["conversation_id"].(string)
	status, _ := msg.Values["status"].(string)
	convID, err := uuid.Parse(convIDStr)
	if err != nil {
		c.ack(ctx, msg.ID)
		return
	}
	if status != "finished" && status != "failed" && status != "stopped" {
		// Defensive: this stream only ever carries a terminal status (see
		// agentdom.ConversationStatus.IsTerminal) — a malformed/unexpected
		// value shouldn't be retried forever.
		c.ack(ctx, msg.ID)
		return
	}

	conv, err := c.repo.FindConversationByID(ctx, convID)
	if err != nil {
		c.log.Error("agent queue consumer: find conversation", "conversation_id", convID, "err", err)
		return // not acked — retried via processPending
	}

	if _, err := c.advancer.AdvanceQueue(ctx, conv.AgentID, 1); err != nil {
		c.log.Error("agent queue consumer: advance queue", "agent_id", conv.AgentID, "err", err)
		return // not acked — retried via processPending
	}
	// The conversation that just went terminal may have been attached to a
	// static environment folder — if so, it might not have been the only
	// thing blocking that folder's next occupant, and that occupant isn't
	// necessarily even queued on THIS agent (see checkFolderCapacity's doc
	// comment: two different agents can share one folder). AdvanceQueue
	// above only ever looks at conv.AgentID's own backlog, so this is a
	// second, independent advance — safe to call even when nothing is
	// actually waiting on the folder (a no-op in that case).
	if conv.EnvironmentID != nil {
		if _, err := c.advancer.AdvanceFolderQueue(ctx, *conv.EnvironmentID, conv.EnvironmentFolderID); err != nil {
			c.log.Error("agent queue consumer: advance folder queue", "environment_id", *conv.EnvironmentID, "err", err)
			return // not acked — retried via processPending
		}
	}
	c.ack(ctx, msg.ID)
}
