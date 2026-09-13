package acpbridge

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"github.com/Paca-AI/agent-runner/internal/messaging"
)

// An ACP conversation's terminal status has to land on
// StreamAgentConversationStatus — services/api's AgentQueueConsumer runs
// AdvanceQueue only on that stream, so without the entry a conversation queued
// behind the same agent is never started.
func TestPublishQueueStatus_AppendsToConversationStatusStream(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis.Run: %v", err)
	}
	t.Cleanup(mr.Close)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { client.Close() })
	d := &Dispatcher{
		Publisher: messaging.NewPublisher(client),
		Log:       slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	convID := uuid.New()
	d.publishQueueStatus(context.Background(), convID, "failed")

	entries, err := client.XRange(context.Background(), messaging.StreamAgentConversationStatus, "-", "+").Result()
	if err != nil {
		t.Fatalf("XRange: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("stream has %d entries, want 1", len(entries))
	}
	if got := entries[0].Values["conversation_id"]; got != convID.String() {
		t.Errorf("conversation_id = %v, want %s", got, convID)
	}
	if got := entries[0].Values["status"]; got != "failed" {
		t.Errorf("status = %v, want failed", got)
	}
}

// A publish failure is logged, never panics or blocks the caller: the DB
// status is already written and stays the source of truth.
func TestPublishQueueStatus_ToleratesUnreachableValkey(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis.Run: %v", err)
	}
	client := redis.NewClient(&redis.Options{Addr: mr.Addr(), MaxRetries: -1})
	t.Cleanup(func() { client.Close() })
	mr.Close()
	d := &Dispatcher{
		Publisher: messaging.NewPublisher(client),
		Log:       slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	d.publishQueueStatus(context.Background(), uuid.New(), "finished")
}

func TestIsTerminalStatus(t *testing.T) {
	for status, want := range map[string]bool{
		"finished": true,
		"failed":   true,
		"stopped":  true,
		"paused":   false,
		"running":  false,
		"queued":   false,
		"":         false,
	} {
		if got := isTerminalStatus(status); got != want {
			t.Errorf("isTerminalStatus(%q) = %v, want %v", status, got, want)
		}
	}
}
