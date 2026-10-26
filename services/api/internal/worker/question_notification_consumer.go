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
	notificationdom "github.com/Paca-AI/api/internal/domain/notification"
	"github.com/Paca-AI/api/internal/events"
)

const (
	questionConsumerGroup = "api.question_notifier"
	questionReadBlock     = 5 * time.Second
	questionReadCount     = 100

	// The ask_user_question MCP tool (apps/mcp/src/tools/ask-tools.ts) answers
	// a chat question with this text followed by a ```paca-ask block carrying
	// the question — present in the tool call's result whatever the agent
	// runtime does with the call's own input, so it is what is matched.
	askResultPrefix = "Question shown to the user"
	askResultFence  = "```paca-ask"
)

type questionConversationFinder interface {
	FindConversationByID(ctx context.Context, id uuid.UUID) (*agentdom.AgentConversation, error)
}

// QuestionNotificationConsumer watches the conversation event stream for an
// agent's ask_user_question result and notifies the conversation's owner that
// a question is waiting. It reads StreamAgentEvents through its own consumer
// group (the realtime service has another), starting from new events only.
type QuestionNotificationConsumer struct {
	client          *redis.Client
	repo            questionConversationFinder
	memberRepo      memberReader
	notificationSvc notificationdom.Service
	log             *slog.Logger
	consumerName    string
	stopCh          chan struct{}
	doneCh          chan struct{}
}

// NewQuestionNotificationConsumer creates a consumer that is ready to be started.
func NewQuestionNotificationConsumer(client *redis.Client, repo questionConversationFinder, memberRepo memberReader, notificationSvc notificationdom.Service, log *slog.Logger) *QuestionNotificationConsumer {
	hostname, err := os.Hostname()
	if err != nil || hostname == "" {
		hostname = uuid.New().String()
	}
	return &QuestionNotificationConsumer{
		client:          client,
		repo:            repo,
		memberRepo:      memberRepo,
		notificationSvc: notificationSvc,
		log:             log,
		consumerName:    fmt.Sprintf("%s.%s", questionConsumerGroup, hostname),
		stopCh:          make(chan struct{}),
		doneCh:          make(chan struct{}),
	}
}

// Start creates the consumer group if needed and reads in the background.
func (c *QuestionNotificationConsumer) Start(ctx context.Context) {
	if err := c.ensureGroup(ctx); err != nil {
		c.log.Warn("question notification consumer: could not create consumer group, will retry on first read", "err", err)
	}
	go c.run()
}

// ensureGroup starts at "$": a question asked before this consumer existed is
// not worth notifying about after the fact, and replaying the whole event
// stream would be expensive.
func (c *QuestionNotificationConsumer) ensureGroup(ctx context.Context) error {
	err := c.client.XGroupCreateMkStream(ctx, events.StreamAgentEvents, questionConsumerGroup, "$").Err()
	if err != nil && err.Error() != "BUSYGROUP Consumer Group name already exists" {
		return err
	}
	return nil
}

// Stop signals the consumer to stop and waits for the goroutine to exit.
func (c *QuestionNotificationConsumer) Stop() {
	close(c.stopCh)
	<-c.doneCh
}

func (c *QuestionNotificationConsumer) run() {
	defer close(c.doneCh)
	c.log.Info("question notification consumer: started", "stream", events.StreamAgentEvents)

	_ = c.read("0", 0) // redeliver anything left unacked by a previous run; errors are logged in read

	for {
		select {
		case <-c.stopCh:
			c.log.Info("question notification consumer: stopping")
			return
		default:
		}
		if err := c.read(">", questionReadBlock); err != nil {
			if strings.Contains(err.Error(), "NOGROUP") {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				_ = c.ensureGroup(ctx)
				cancel()
			}
			time.Sleep(2 * time.Second)
		}
	}
}

func (c *QuestionNotificationConsumer) read(startID string, block time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), block+5*time.Second)
	defer cancel()
	msgs, err := c.client.XReadGroup(ctx, &redis.XReadGroupArgs{
		Group:    questionConsumerGroup,
		Consumer: c.consumerName,
		Streams:  []string{events.StreamAgentEvents, startID},
		Count:    questionReadCount,
		Block:    block,
	}).Result()
	if err != nil {
		if err == redis.Nil {
			return nil
		}
		c.log.Error("question notification consumer: xreadgroup error", "err", err)
		return err
	}
	for _, stream := range msgs {
		for _, msg := range stream.Messages {
			c.handle(msg)
		}
	}
	return nil
}

func (c *QuestionNotificationConsumer) ack(ctx context.Context, id string) {
	if err := c.client.XAck(ctx, events.StreamAgentEvents, questionConsumerGroup, id).Err(); err != nil {
		c.log.Warn("question notification consumer: xack failed", "id", id, "err", err)
	}
}

// isAskQuestionResult reports whether an event is the result of an
// ask_user_question call. Checked on the raw payload so the common case (any
// other event) costs two substring scans and no JSON decoding.
func isAskQuestionResult(eventType, payload string) bool {
	return eventType == "tool_call_update" &&
		strings.Contains(payload, askResultPrefix) &&
		strings.Contains(payload, askResultFence)
}

func (c *QuestionNotificationConsumer) handle(msg redis.XMessage) {
	ctx := context.Background()

	eventType, _ := msg.Values["event_type"].(string)
	payload, _ := msg.Values["payload"].(string)
	if !isAskQuestionResult(eventType, payload) {
		c.ack(ctx, msg.ID)
		return
	}
	convID, err := uuid.Parse(fmt.Sprint(msg.Values["conversation_id"]))
	if err != nil {
		c.ack(ctx, msg.ID)
		return
	}
	conv, err := c.repo.FindConversationByID(ctx, convID)
	if err != nil {
		c.log.Error("question notification consumer: find conversation", "conversation_id", convID, "err", err)
		return // not acked — retried via the pending list on restart
	}

	var recipient uuid.UUID
	switch {
	case conv.ActorAccountID != nil:
		recipient = *conv.ActorAccountID
	case conv.TriggeredByMemberID != nil && c.memberRepo != nil:
		if m, err := c.memberRepo.FindMemberByID(ctx, *conv.TriggeredByMemberID); err == nil {
			recipient = m.AccountID
		}
	}
	if recipient != uuid.Nil {
		if err := c.notificationSvc.NotifyQuestion(ctx, notificationdom.NotifyQuestionInput{
			ConversationID:     conv.ID,
			ProjectID:          conv.ProjectID,
			AgentID:            conv.AgentID,
			RecipientAccountID: recipient,
		}); err != nil {
			c.log.Error("question notification consumer: notify", "conversation_id", convID, "err", err)
		}
	}
	c.ack(ctx, msg.ID)
}
