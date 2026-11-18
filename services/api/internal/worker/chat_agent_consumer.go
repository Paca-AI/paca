package worker

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	agentdom "github.com/Paca-AI/api/internal/domain/agent"
	"github.com/Paca-AI/api/internal/events"
)

const (
	chatAgentConsumerGroup = "api.chat_agent_dm"
	chatAgentReadBlock     = 5 * time.Second
	chatAgentReadCount     = 100
)

type chatAgentConversations interface {
	FindConversationByID(ctx context.Context, id uuid.UUID) (*agentdom.AgentConversation, error)
}

// chatAgentBridge turns a finished agent turn into a chat message.
type chatAgentBridge interface {
	OnAgentTurnEnd(ctx context.Context, sessionID, conversationID uuid.UUID, turnEnd int) error
}

// ChatAgentConsumer posts the agent's reply into the direct message that talks to
// its chat session, once the turn ends. It reads the agent event stream through
// its own consumer group and only looks at turn_end events.
type ChatAgentConsumer struct {
	client       *redis.Client
	repo         chatAgentConversations
	bridge       chatAgentBridge
	log          *slog.Logger
	consumerName string
	stopCh       chan struct{}
	doneCh       chan struct{}
}

// NewChatAgentConsumer creates a consumer that is ready to be started.
func NewChatAgentConsumer(client *redis.Client, repo chatAgentConversations, bridge chatAgentBridge, log *slog.Logger) *ChatAgentConsumer {
	hostname, err := os.Hostname()
	if err != nil || hostname == "" {
		hostname = uuid.New().String()
	}
	return &ChatAgentConsumer{
		client: client, repo: repo, bridge: bridge, log: log,
		consumerName: fmt.Sprintf("%s.%s", chatAgentConsumerGroup, hostname),
		stopCh:       make(chan struct{}), doneCh: make(chan struct{}),
	}
}

// Start creates the consumer group if needed and reads in the background.
func (c *ChatAgentConsumer) Start(ctx context.Context) {
	if err := c.ensureGroup(ctx); err != nil {
		c.log.Warn("chat agent consumer: could not create consumer group, will retry on first read", "err", err)
	}
	go c.run()
}

// ensureGroup starts at "$": a turn that ended before this consumer existed has
// no direct message waiting for it.
func (c *ChatAgentConsumer) ensureGroup(ctx context.Context) error {
	err := c.client.XGroupCreateMkStream(ctx, events.StreamAgentEvents, chatAgentConsumerGroup, "$").Err()
	if err != nil && err.Error() != "BUSYGROUP Consumer Group name already exists" {
		return err
	}
	return nil
}

// Stop stops reading and waits for the loop to end.
func (c *ChatAgentConsumer) Stop() {
	close(c.stopCh)
	<-c.doneCh
}

func (c *ChatAgentConsumer) run() {
	defer close(c.doneCh)
	c.log.Info("chat agent consumer: started", "stream", events.StreamAgentEvents)
	_ = c.read("0", 0) // redeliver anything a previous run left unacked
	for {
		select {
		case <-c.stopCh:
			return
		default:
		}
		if err := c.read(">", chatAgentReadBlock); err != nil {
			if strings.Contains(err.Error(), "NOGROUP") {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				_ = c.ensureGroup(ctx)
				cancel()
			}
			time.Sleep(2 * time.Second)
		}
	}
}

func (c *ChatAgentConsumer) read(startID string, block time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), block+5*time.Second)
	defer cancel()
	msgs, err := c.client.XReadGroup(ctx, &redis.XReadGroupArgs{
		Group: chatAgentConsumerGroup, Consumer: c.consumerName,
		Streams: []string{events.StreamAgentEvents, startID}, Count: chatAgentReadCount, Block: block,
	}).Result()
	if err != nil {
		if err == redis.Nil {
			return nil
		}
		c.log.Error("chat agent consumer: xreadgroup error", "err", err)
		return err
	}
	for _, stream := range msgs {
		for _, msg := range stream.Messages {
			c.handle(msg)
		}
	}
	return nil
}

func (c *ChatAgentConsumer) ack(ctx context.Context, id string) {
	if err := c.client.XAck(ctx, events.StreamAgentEvents, chatAgentConsumerGroup, id).Err(); err != nil {
		c.log.Warn("chat agent consumer: xack failed", "id", id, "err", err)
	}
}

func (c *ChatAgentConsumer) handle(msg redis.XMessage) {
	ctx := context.Background()
	if eventType, _ := msg.Values["event_type"].(string); eventType != "turn_end" {
		c.ack(ctx, msg.ID)
		return
	}
	convID, err := uuid.Parse(fmt.Sprint(msg.Values["conversation_id"]))
	if err != nil {
		c.ack(ctx, msg.ID)
		return
	}
	turnEnd, err := strconv.Atoi(fmt.Sprint(msg.Values["event_index"]))
	if err != nil {
		c.ack(ctx, msg.ID)
		return
	}
	conv, err := c.repo.FindConversationByID(ctx, convID)
	if err != nil {
		c.log.Error("chat agent consumer: find conversation", "conversation_id", convID, "err", err)
		return // not acked: retried on restart
	}
	if conv.ChatSessionID != nil {
		if err := c.bridge.OnAgentTurnEnd(ctx, *conv.ChatSessionID, convID, turnEnd); err != nil {
			c.log.Error("chat agent consumer: post reply", "conversation_id", convID, "err", err)
			return
		}
	}
	c.ack(ctx, msg.ID)
}
