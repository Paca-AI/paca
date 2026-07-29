package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"github.com/Paca-AI/api/internal/events"
)

const (
	pluginEventConsumerGroup = "api.plugin_dispatcher"
	pluginEventReadBlock     = 5 * time.Second
	pluginEventReadCount     = 50
)

// PluginEventEmitter dispatches a topic+payload to every loaded plugin that
// has subscribed to it. Implemented by *plugin.Runtime; kept as a narrow
// interface here to avoid a dependency from the worker package onto the
// plugin platform package.
type PluginEventEmitter interface {
	EmitEvent(ctx context.Context, topic string, payload any)
}

// PluginEventConsumer reads every activity event (task activities, comments,
// links, etc.) from the StreamPluginEvents Valkey stream — written by
// ActivitySvc.fanout — and dispatches each one to the plugin runtime.
//
// The plugin runtime is intentionally just another stream subscriber here,
// on equal footing with ActivityConsumer (DB persistence) and
// services/realtime (live UI updates): the API never calls into the plugin
// runtime directly when recording an activity.
type PluginEventConsumer struct {
	client       *redis.Client
	emitter      PluginEventEmitter
	log          *slog.Logger
	consumerName string // unique per instance, derived from hostname
	stopCh       chan struct{}
	doneCh       chan struct{}
}

// NewPluginEventConsumer creates a consumer that is ready to be started.
// The consumer name is derived from the hostname so it is unique per pod/instance.
// If hostname retrieval fails, a random UUID suffix is used as fallback.
func NewPluginEventConsumer(client *redis.Client, emitter PluginEventEmitter, log *slog.Logger) *PluginEventConsumer {
	hostname, err := os.Hostname()
	if err != nil || hostname == "" {
		hostname = uuid.New().String()
	}
	return &PluginEventConsumer{
		client:       client,
		emitter:      emitter,
		log:          log,
		consumerName: fmt.Sprintf("%s.%s", pluginEventConsumerGroup, hostname),
		stopCh:       make(chan struct{}),
		doneCh:       make(chan struct{}),
	}
}

// Start creates the consumer group if needed, then begins reading from the
// stream in a background goroutine. Call Stop to drain and exit cleanly.
func (c *PluginEventConsumer) Start(ctx context.Context) {
	err := c.client.XGroupCreateMkStream(ctx, events.StreamPluginEvents, pluginEventConsumerGroup, "0").Err()
	if err != nil && err.Error() != "BUSYGROUP Consumer Group name already exists" {
		c.log.Warn("plugin event consumer: could not create consumer group", "err", err)
		// Non-fatal — we still attempt to read below.
	}

	go c.run()
}

// Stop signals the consumer to stop and waits for the goroutine to exit.
func (c *PluginEventConsumer) Stop() {
	close(c.stopCh)
	<-c.doneCh
}

// run is the main loop executed in a goroutine by Start.
func (c *PluginEventConsumer) run() {
	defer close(c.doneCh)
	c.log.Info("plugin event consumer: started", "stream", events.StreamPluginEvents)

	// On startup, replay any pending messages (PEL) that were delivered but
	// never acknowledged (e.g. after a crash). "0" fetches the backlog.
	c.processPending(context.Background())

	for {
		select {
		case <-c.stopCh:
			c.log.Info("plugin event consumer: stopping")
			return
		default:
		}

		ctx, cancel := context.WithTimeout(context.Background(), pluginEventReadBlock+time.Second)
		msgs, err := c.client.XReadGroup(ctx, &redis.XReadGroupArgs{
			Group:    pluginEventConsumerGroup,
			Consumer: c.consumerName,
			Streams:  []string{events.StreamPluginEvents, ">"},
			Count:    pluginEventReadCount,
			Block:    pluginEventReadBlock,
		}).Result()
		cancel()

		if err != nil {
			if err == redis.Nil {
				// Timeout with no new messages — loop and check stopCh.
				continue
			}
			c.log.Error("plugin event consumer: xreadgroup error", "err", err)
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
func (c *PluginEventConsumer) processPending(ctx context.Context) {
	msgs, err := c.client.XReadGroup(ctx, &redis.XReadGroupArgs{
		Group:    pluginEventConsumerGroup,
		Consumer: c.consumerName,
		Streams:  []string{events.StreamPluginEvents, "0"},
		Count:    pluginEventReadCount,
	}).Result()
	if err != nil && err != redis.Nil {
		c.log.Warn("plugin event consumer: could not read pending messages", "err", err)
		return
	}
	for _, stream := range msgs {
		for _, msg := range stream.Messages {
			c.handle(msg)
		}
	}
}

// handle dispatches one stream message to the plugin runtime.
func (c *PluginEventConsumer) handle(msg redis.XMessage) {
	ctx := context.Background()

	eventType, _ := msg.Values["type"].(string)
	if eventType == "" {
		c.log.Warn("plugin event consumer: message has no type field", "id", msg.ID)
		c.ack(ctx, msg.ID)
		return
	}

	// The Publisher.Append method stores the body in a "payload" field as a
	// JSON-encoded string. Passing it through as json.RawMessage lets
	// Runtime.EmitEvent re-marshal it verbatim instead of double-encoding it.
	raw, _ := msg.Values["payload"].(string)
	c.emitter.EmitEvent(ctx, eventType, json.RawMessage(raw))

	c.ack(ctx, msg.ID)
}

func (c *PluginEventConsumer) ack(ctx context.Context, id string) {
	if err := c.client.XAck(ctx, events.StreamPluginEvents, pluginEventConsumerGroup, id).Err(); err != nil {
		c.log.Warn("plugin event consumer: xack failed", "id", id, "err", err)
	}
}
