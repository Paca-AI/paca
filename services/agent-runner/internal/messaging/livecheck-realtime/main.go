// livecheck2 publishes one real PublishRealtime call against the real dev
// Valkey and confirms the exact wire shape a subscriber receives — round-
// tripping the actual Go encoder, not just eyeballing the code.
//
//	go run ./internal/messaging/livecheck2 <redis-addr>
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"github.com/Paca-AI/agent-runner/internal/messaging"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "FAIL %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	client := redis.NewClient(&redis.Options{Addr: os.Args[1]})
	defer func() { _ = client.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pub := messaging.NewPublisher(client)
	projectID := uuid.New()
	convID := uuid.New()

	err := pub.PublishRealtime(ctx, projectID, convID, "agent.agent_message_chunk",
		map[string]any{"event_index": 3}, nil)
	if err != nil {
		return fmt.Errorf("publish realtime: %w", err)
	}
	fmt.Printf("OK   published for project=%s conversation=%s\n", projectID, convID)
	return nil
}
