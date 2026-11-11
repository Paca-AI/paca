// Package messaging provides Valkey-backed event publishing.
//
// Two delivery mechanisms are supported:
//   - Pub/Sub (Publish): immediate fan-out to real-time subscribers; used to
//     notify services/realtime so it can push updates to connected clients.
//   - Streams (Append): durable, ordered log; used for analytics and any
//     consumer that needs to replay or process events at its own pace.
package messaging

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	"github.com/redis/go-redis/v9"
)

// Publisher wraps a Valkey client and exposes both Pub/Sub and Stream publishing.
type Publisher struct {
	client *redis.Client
	log    *slog.Logger
}

// NewPublisher creates a Publisher backed by an existing Valkey client.
func NewPublisher(client *redis.Client, log *slog.Logger) *Publisher {
	log.Info("valkey publisher ready")
	return &Publisher{client: client, log: log}
}

// Publish serialises payload as JSON and sends it to a Valkey Pub/Sub channel.
// This is the primary path for real-time notifications: services/realtime
// subscribes to the channel and fans the event out to connected Socket.IO clients.
func (p *Publisher) Publish(ctx context.Context, channel string, payload any) error {
	if p == nil || p.client == nil {
		return errors.New("messaging: publisher not initialized")
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("messaging: marshal: %w", err)
	}

	if err := p.client.Publish(ctx, channel, string(body)).Err(); err != nil {
		return fmt.Errorf("messaging: publish %q: %w", channel, err)
	}

	return nil
}

// Append serialises payload as JSON and appends it to a Valkey Stream.
// The eventType is stored in the "type" field; the serialised body in "payload".
// Use this for analytics, audit logs, and any consumer that requires a durable
// ordered event log.
func (p *Publisher) Append(ctx context.Context, stream, eventType string, payload any) error {
	if p == nil || p.client == nil {
		return errors.New("messaging: publisher not initialized")
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("messaging: marshal: %w", err)
	}

	if err := p.client.XAdd(ctx, &redis.XAddArgs{
		Stream: stream,
		Values: map[string]any{
			"type":    eventType,
			"payload": string(body),
		},
	}).Err(); err != nil {
		return fmt.Errorf("messaging: append %q to %q: %w", eventType, stream, err)
	}

	return nil
}

// AppendFlat appends a message to a Valkey Stream writing the provided fields
// directly as top-level stream entry fields (i.e. not JSON-encoded under a
// "payload" key). Use this when the consumer (e.g. services/ai-agent) reads
// the individual fields without further deserialization.
func (p *Publisher) AppendFlat(ctx context.Context, stream string, fields map[string]any) error {
	if p == nil || p.client == nil {
		return errors.New("messaging: publisher not initialized")
	}

	if err := p.client.XAdd(ctx, &redis.XAddArgs{
		Stream: stream,
		Values: fields,
	}).Err(); err != nil {
		return fmt.Errorf("messaging: append flat to %q: %w", stream, err)
	}

	return nil
}

// IncrCounter atomically increments an integer key and returns the new value.
// It backs small shared counters, such as the IAM epoch services/realtime
// polls to notice a change whose Pub/Sub announcement it missed.
func (p *Publisher) IncrCounter(ctx context.Context, key string) (int64, error) {
	if p == nil || p.client == nil {
		return 0, errors.New("messaging: publisher not initialized")
	}
	n, err := p.client.Incr(ctx, key).Result()
	if err != nil {
		return 0, fmt.Errorf("messaging: incr %q: %w", key, err)
	}
	return n, nil
}

// Close is a no-op; the Valkey client lifecycle is managed by the owner.
func (p *Publisher) Close() {}
