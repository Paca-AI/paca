package messaging

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

// Publisher writes to the three Valkey destinations a conversation's
// lifecycle needs — see topics.go's doc comments for what each one is
// actually for (verified against services/ai-agent's real Python source,
// not the docs, which conflated StreamAgentEvents with the realtime path).
type Publisher struct {
	client *redis.Client
}

// NewPublisher builds a Publisher writing through the given Valkey client.
func NewPublisher(client *redis.Client) *Publisher {
	return &Publisher{client: client}
}

// PublishEvent appends one conversation event to the durable
// StreamAgentEvents log. payload should be the raw JSON of the underlying
// update (e.g. an acp.Event's Raw field) — passed through as-is, matching
// how executor.py's _persist_event forwards event.model_dump_json()
// untouched.
func (p *Publisher) PublishEvent(
	ctx context.Context,
	conversationID, projectID uuid.UUID,
	eventType, eventSource string,
	eventIndex int,
	payload []byte,
	status string,
) error {
	fields := map[string]any{
		"conversation_id": conversationID.String(),
		"event_type":      eventType,
		"event_source":    eventSource,
		"event_index":     eventIndex,
		"payload":         string(payload),
		"status":          status,
	}
	if projectID != uuid.Nil {
		fields["project_id"] = projectID.String()
	}

	if err := p.client.XAdd(ctx, &redis.XAddArgs{
		Stream: StreamAgentEvents,
		Values: fields,
	}).Err(); err != nil {
		return fmt.Errorf("messaging: publish event for conversation %s: %w", conversationID, err)
	}
	return nil
}

// PublishRealtime is the actual live-UI-update path — mirrors
// core/streams.py's publish_realtime field-for-field. Called both per
// individual conversation event (so the UI streams tool calls/messages as
// they happen) and on every status transition (started/finished/failed/
// paused/stopped).
//
// projectID zero (uuid.Nil) is a global-chat conversation and is omitted
// from the payload entirely, matching Python's `if project_id is not
// None:` — not sent as an empty string. actorUserID is nil for a
// project-scoped conversation; services/realtime's routeEvent needs at
// least one of project_id/actor_user_id present to know which room to
// fan this out to (see the Python doc comment this mirrors).
//
// conversationID zero (uuid.Nil), unlike projectID, is sent as a literal
// empty string rather than omitted — matching internal/acpbridge's status
// event, the one caller with no real conversation to report
// (publish_realtime's own Python callers pass conversation_id="" for that
// same event; Python's str type has no separate "unset" to omit).
func (p *Publisher) PublishRealtime(
	ctx context.Context,
	projectID uuid.UUID,
	conversationID uuid.UUID,
	eventType string,
	extraPayload map[string]any,
	actorUserID *uuid.UUID,
) error {
	conversationIDStr := conversationID.String()
	if conversationID == uuid.Nil {
		conversationIDStr = ""
	}
	payload := map[string]any{"conversation_id": conversationIDStr}
	if projectID != uuid.Nil {
		payload["project_id"] = projectID.String()
	}
	if actorUserID != nil {
		payload["actor_user_id"] = actorUserID.String()
	}
	for k, v := range extraPayload {
		payload[k] = v
	}

	message, err := json.Marshal(map[string]any{"type": eventType, "payload": payload})
	if err != nil {
		return fmt.Errorf("messaging: marshal realtime event: %w", err)
	}
	if err := p.client.Publish(ctx, ChannelRealtime, message).Err(); err != nil {
		return fmt.Errorf("messaging: publish realtime event for conversation %s: %w", conversationID, err)
	}
	return nil
}

// PublishConversationStatus durably records that conversationID reached a
// terminal status (finished/failed/stopped — never "paused", which isn't
// terminal). Mirrors core/streams.py's publish_conversation_status; see
// StreamAgentConversationStatus's doc comment for who consumes this and
// why it can't just be PublishRealtime's fire-and-forget pub/sub.
func (p *Publisher) PublishConversationStatus(ctx context.Context, conversationID uuid.UUID, status string) error {
	if err := p.client.XAdd(ctx, &redis.XAddArgs{
		Stream: StreamAgentConversationStatus,
		Values: map[string]any{
			"conversation_id": conversationID.String(),
			"status":          status,
		},
	}).Err(); err != nil {
		return fmt.Errorf("messaging: publish conversation status for %s: %w", conversationID, err)
	}
	return nil
}
