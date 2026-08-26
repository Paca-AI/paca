package messaging

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	consumerGroup = "paca-api-workers"
	// blockDuration is how long XREADGROUP blocks waiting for new messages.
	blockDuration = 5 * time.Second
	// maxMessages is the max batch size per read.
	maxMessages = 10
)

// Consumer reads events from a Valkey Stream using a consumer group.
// It is intended to be run as a long-lived goroutine via Run().
type Consumer struct {
	client   *redis.Client
	stream   string
	handler  StreamHandler
	log      *slog.Logger
	consumer string // unique consumer name within the group
}

// StreamHandler processes a single stream event.
// Returning an error causes the message to be re-delivered (not acknowledged).
type StreamHandler func(ctx context.Context, eventType string, payload []byte) error

// NewConsumer creates a Consumer for the given stream.
// The consumer name is derived from the hostname so it is unique per pod.
func NewConsumer(client *redis.Client, stream string, handler StreamHandler, log *slog.Logger) *Consumer {
	hostname, _ := os.Hostname()
	return &Consumer{
		client:   client,
		stream:   stream,
		handler:  handler,
		log:      log,
		consumer: fmt.Sprintf("paca-api-%s", hostname),
	}
}

// Run blocks and continuously reads from the stream until ctx is cancelled.
// It creates the consumer group if it does not yet exist.
func (c *Consumer) Run(ctx context.Context) {
	if err := c.ensureGroup(ctx); err != nil {
		c.log.Error("consumer: failed to create consumer group", "err", err)
		return
	}

	c.log.Info("consumer: started", "stream", c.stream, "group", consumerGroup, "consumer", c.consumer)

	for {
		select {
		case <-ctx.Done():
			c.log.Info("consumer: stopping", "stream", c.stream)
			return
		default:
		}

		messages, err := c.client.XReadGroup(ctx, &redis.XReadGroupArgs{
			Group:    consumerGroup,
			Consumer: c.consumer,
			Streams:  []string{c.stream, ">"},
			Count:    maxMessages,
			Block:    blockDuration,
		}).Result()

		if err != nil {
			if err == redis.Nil || err == context.Canceled || err == context.DeadlineExceeded {
				continue
			}
			c.log.Warn("consumer: read error", "stream", c.stream, "err", err)
			if strings.HasPrefix(err.Error(), "NOGROUP") {
				// The stream and/or consumer group vanished out from under
				// an already-running consumer (a Valkey restart without
				// persistence, a FLUSHALL, a manual XGROUP DESTROY) —
				// without this, every subsequent XReadGroup call keeps
				// failing the exact same way forever, since nothing else
				// ever recreates the group. Self-heal the same way startup
				// does.
				if reErr := c.ensureGroup(ctx); reErr != nil {
					c.log.Error("consumer: failed to recreate consumer group after NOGROUP", "stream", c.stream, "err", reErr)
				}
			}
			time.Sleep(time.Second) // back off on repeated errors
			continue
		}

		for _, stream := range messages {
			for _, msg := range stream.Messages {
				c.processMessage(ctx, msg)
			}
		}
	}
}

func (c *Consumer) processMessage(ctx context.Context, msg redis.XMessage) {
	eventType, _ := msg.Values["type"].(string)
	payload, _ := msg.Values["payload"].(string)

	if err := c.handler(ctx, eventType, []byte(payload)); err != nil {
		c.log.Warn("consumer: handler error",
			"stream", c.stream, "id", msg.ID, "type", eventType, "err", err)
		// Do not ACK — message will be re-delivered after PEL timeout.
		return
	}

	if err := c.client.XAck(ctx, c.stream, consumerGroup, msg.ID).Err(); err != nil {
		c.log.Warn("consumer: ack failed", "id", msg.ID, "err", err)
	}
}

func (c *Consumer) ensureGroup(ctx context.Context) error {
	err := c.client.XGroupCreateMkStream(ctx, c.stream, consumerGroup, "0").Err()
	if err != nil && err.Error() != "BUSYGROUP Consumer Group name already exists" {
		return fmt.Errorf("consumer: ensure group: %w", err)
	}
	return nil
}
