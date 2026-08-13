package e2e_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"github.com/Paca-AI/agent-runner/internal/messaging"
)

// TestValkeyStreamRoundTrip verifies go-redis client wiring against a real
// Valkey instance: XGroupCreateMkStream, XAdd, XReadGroup, XAck — the exact
// primitives internal/messaging.Consumer builds on. Replaces
// internal/messaging/livecheck. Runs against a throwaway stream/group name
// scoped to this test, not the real paca:agent:triggers stream (this test's
// container is ephemeral and isolated per CI run, so the original
// livecheck's "never touch the real consumer group" rationale no longer
// applies literally, but a unique name still keeps this test independent of
// any other test sharing the same Valkey container).
func TestValkeyStreamRoundTrip(t *testing.T) {
	t.Parallel()
	client := newE2ERedisClient(t)
	ctx := context.Background()

	if err := client.Ping(ctx).Err(); err != nil {
		t.Fatalf("ping: %v", err)
	}

	stream := "e2e:messaging:stream:" + uuid.New().String()
	group := "e2e-group"
	t.Cleanup(func() { _ = client.Del(context.Background(), stream).Err() })

	if err := client.XGroupCreateMkStream(ctx, stream, group, "0").Err(); err != nil {
		t.Fatalf("XGroupCreateMkStream: %v", err)
	}

	id, err := client.XAdd(ctx, &redis.XAddArgs{
		Stream: stream,
		Values: map[string]any{
			"conversation_id": "11111111-1111-1111-1111-111111111111",
			"agent_id":        "22222222-2222-2222-2222-222222222222",
			"trigger_type":    "chat_message",
			"message":         "e2e",
		},
	}).Result()
	if err != nil {
		t.Fatalf("XAdd: %v", err)
	}
	if id == "" {
		t.Fatal("XAdd returned an empty ID")
	}

	streams, err := client.XReadGroup(ctx, &redis.XReadGroupArgs{
		Group:    group,
		Consumer: "e2e-consumer",
		Streams:  []string{stream, ">"},
		Count:    10,
		Block:    2 * time.Second,
	}).Result()
	if err != nil {
		t.Fatalf("XReadGroup: %v", err)
	}
	if len(streams) != 1 || len(streams[0].Messages) != 1 {
		t.Fatalf("XReadGroup: got %d streams, want 1 with 1 message", len(streams))
	}
	msg := streams[0].Messages[0]
	if msg.Values["message"] != "e2e" {
		t.Fatalf("XReadGroup: values = %v, want message=e2e", msg.Values)
	}

	if err := client.XAck(ctx, stream, group, msg.ID).Err(); err != nil {
		t.Fatalf("XAck: %v", err)
	}
}

// TestPublishRealtimeWireFormat publishes one real Publisher.PublishRealtime
// call against a real Valkey and confirms the exact wire shape a subscriber
// receives — round-tripping the actual Go JSON encoder, not just eyeballing
// the code. Replaces internal/messaging/livecheck-realtime.
func TestPublishRealtimeWireFormat(t *testing.T) {
	t.Parallel()
	client := newE2ERedisClient(t)
	ctx := context.Background()

	sub := client.Subscribe(ctx, messaging.ChannelRealtime)
	t.Cleanup(func() { _ = sub.Close() })
	if _, err := sub.Receive(ctx); err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	msgCh := sub.Channel()

	pub := messaging.NewPublisher(client)
	projectID := uuid.New()
	convID := uuid.New()

	if err := pub.PublishRealtime(ctx, projectID, convID, "agent.agent_message_chunk",
		map[string]any{"event_index": 3}, nil); err != nil {
		t.Fatalf("PublishRealtime: %v", err)
	}

	select {
	case msg := <-msgCh:
		var decoded struct {
			Type    string `json:"type"`
			Payload struct {
				ConversationID string `json:"conversation_id"`
				ProjectID      string `json:"project_id"`
				EventIndex     int    `json:"event_index"`
			} `json:"payload"`
		}
		if err := json.Unmarshal([]byte(msg.Payload), &decoded); err != nil {
			t.Fatalf("decode published message: %v (raw: %s)", err, msg.Payload)
		}
		if decoded.Type != "agent.agent_message_chunk" {
			t.Fatalf("type = %q, want %q", decoded.Type, "agent.agent_message_chunk")
		}
		if decoded.Payload.ConversationID != convID.String() {
			t.Fatalf("payload.conversation_id = %q, want %q", decoded.Payload.ConversationID, convID.String())
		}
		if decoded.Payload.ProjectID != projectID.String() {
			t.Fatalf("payload.project_id = %q, want %q", decoded.Payload.ProjectID, projectID.String())
		}
		if decoded.Payload.EventIndex != 3 {
			t.Fatalf("payload.event_index = %d, want 3", decoded.Payload.EventIndex)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("did not receive the published realtime message within 5s")
	}
}
