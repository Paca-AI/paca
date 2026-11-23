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

	plugindom "github.com/Paca-AI/api/internal/domain/plugin"
	"github.com/Paca-AI/api/internal/events"
)

const (
	pluginEventConsumerGroup = "api.plugin_dispatcher"
	pluginEventReadBlock     = 5 * time.Second
	pluginEventReadCount     = 50

	// pluginRetryKeyPrefix names the Valkey set holding, for one stream entry,
	// the plugins whose handling failed in a way worth retrying. A redelivery
	// of the entry runs only those plugins, so a plugin that already handled
	// the event (sent its email, called its webhook) is not run twice.
	pluginRetryKeyPrefix = "paca:plugin_event_retry:"
	// pluginRetryKeyTTL outlives the longest a redelivery cycle can take
	// (reclaimMaxDelivered x reclaimMinIdle, plus slack for a stalled reclaimer).
	pluginRetryKeyTTL = time.Hour
)

// PluginEventEmitter dispatches a topic+payload to the loaded plugins that
// have subscribed to it, and reports how each plugin's handling went. When only
// is non-empty it dispatches to just those plugins. Implemented by
// *plugin.Runtime; kept as a narrow interface here to avoid a dependency from
// the worker package onto the plugin platform package.
type PluginEventEmitter interface {
	DeliverEvent(ctx context.Context, topic string, payload any, only []string) []plugindom.EventDelivery
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
	reclaim      *streamReclaimer
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
	consumerName := fmt.Sprintf("%s.%s", pluginEventConsumerGroup, hostname)
	return &PluginEventConsumer{
		client:       client,
		emitter:      emitter,
		log:          log,
		consumerName: consumerName,
		reclaim:      newStreamReclaimer(client, log, events.StreamPluginEvents, pluginEventConsumerGroup, consumerName),
		stopCh:       make(chan struct{}),
		doneCh:       make(chan struct{}),
	}
}

// Start creates the consumer group if needed, then begins reading from the
// stream in a background goroutine. Call Stop to drain and exit cleanly.
func (c *PluginEventConsumer) Start(ctx context.Context) {
	// "0": see ActivityConsumer.Start's identical comment.
	if err := c.ensureGroup(ctx, "0"); err != nil {
		c.log.Warn("plugin event consumer: could not create consumer group, will retry on first read", "err", err)
	}

	go c.run()
}

// ensureGroup creates the consumer group at startID if it doesn't already
// exist — see ActivityConsumer.ensureGroup's doc comment for why startID
// differs between Start's first-ever creation ("0") and the NOGROUP
// recovery path in run() below ("$").
func (c *PluginEventConsumer) ensureGroup(ctx context.Context, startID string) error {
	err := c.client.XGroupCreateMkStream(ctx, events.StreamPluginEvents, pluginEventConsumerGroup, startID).Err()
	if err != nil && err.Error() != "BUSYGROUP Consumer Group name already exists" {
		return err
	}
	return nil
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

		// Pick up entries a dead pod's consumer name left pending: that name
		// is never read again, so its startup replay can't recover them.
		if c.reclaim.due(time.Now()) {
			c.reclaim.run(context.Background(), c.handle)
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
			if strings.Contains(err.Error(), "NOGROUP") {
				// "$", not "0": see ActivityConsumer's identical recovery
				// comment — this handler has no event-level dedup either.
				recoverCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				geErr := c.ensureGroup(recoverCtx, "$")
				cancel()
				if geErr != nil {
					c.log.Warn("plugin event consumer: failed to recreate consumer group", "err", geErr)
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

	// A redelivery after a failed attempt runs only the plugins that failed.
	retryKey := pluginRetryKeyPrefix + msg.ID
	only, err := c.client.SMembers(ctx, retryKey).Result()
	if err != nil {
		// Running every plugin again could repeat what the others already did,
		// so wait and try again rather than guess.
		c.log.Error("plugin event consumer: could not read retry state; leaving entry pending", "id", msg.ID, "err", err)
		return
	}

	var retry []string
	for _, d := range c.emitter.DeliverEvent(ctx, eventType, json.RawMessage(raw), only) {
		if d.Err == nil {
			continue
		}
		if d.Retryable {
			c.log.Warn("plugin event consumer: plugin could not handle event; will retry", "id", msg.ID, "topic", eventType, "plugin", d.Plugin, "err", d.Err)
			retry = append(retry, d.Plugin)
			continue
		}
		// A retry would fail the same way. Keep a durable record next to the
		// other dead letters, and move on.
		c.log.Error("plugin event consumer: plugin could not handle event; not retrying", "id", msg.ID, "topic", eventType, "plugin", d.Plugin, "err", d.Err)
		c.reclaim.recordDead(ctx, msg, d.Plugin+": "+d.Err.Error())
	}

	if len(retry) == 0 {
		if len(only) > 0 {
			_ = c.client.Del(ctx, retryKey).Err()
		}
		c.ack(ctx, msg.ID)
		return
	}

	// Remember who to retry, then leave the entry pending: the reclaimer
	// redelivers it after reclaimMinIdle and dead-letters it once it has failed
	// reclaimMaxDelivered times. If the state cannot be saved a retry could not
	// be targeted, so give up on it instead of re-running every plugin.
	pipe := c.client.TxPipeline()
	pipe.Del(ctx, retryKey)
	pipe.SAdd(ctx, retryKey, stringsToAny(retry)...)
	pipe.Expire(ctx, retryKey, pluginRetryKeyTTL)
	if _, err := pipe.Exec(ctx); err != nil {
		c.log.Error("plugin event consumer: could not save retry state; giving up on retry", "id", msg.ID, "err", err)
		c.reclaim.recordDead(ctx, msg, "retry state not saved: "+err.Error())
		c.ack(ctx, msg.ID)
	}
}

func stringsToAny(in []string) []any {
	out := make([]any, len(in))
	for i, s := range in {
		out[i] = s
	}
	return out
}

func (c *PluginEventConsumer) ack(ctx context.Context, id string) {
	if err := c.client.XAck(ctx, events.StreamPluginEvents, pluginEventConsumerGroup, id).Err(); err != nil {
		c.log.Warn("plugin event consumer: xack failed", "id", id, "err", err)
	}
}
