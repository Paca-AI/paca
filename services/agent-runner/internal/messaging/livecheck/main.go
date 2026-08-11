// livecheck verifies go-redis client wiring against the real dev Valkey
// instance, using a throwaway stream key (never the real
// paca:agent:triggers/paca:agent:events) so this test data never reaches
// the real consumer group.
//
//	go run ./internal/messaging/livecheck <redis-addr>
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/redis/go-redis/v9"
)

const testStream = "paca:agent:triggers:LIVECHECK-DELETE-ME"
const testGroup = "livecheck-group"

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "FAIL %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	if len(os.Args) != 2 {
		return fmt.Errorf("usage: livecheck <redis-addr>")
	}
	client := redis.NewClient(&redis.Options{Addr: os.Args[1]})
	defer func() { _ = client.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("ping: %w", err)
	}
	fmt.Println("OK   Ping")
	// testStream is a throwaway key scoped to this one run, so starting its
	// consumer group at "0" (as opposed to "$" — see
	// internal/messaging.Consumer.Run's own doc comment on why that
	// distinction is load-bearing for the *real* trigger stream) is fine
	// here: there's no pre-existing backlog on a key nothing else ever
	// wrote to before this line.
	defer func() { _ = client.Del(context.Background(), testStream).Err() }()

	err := client.XGroupCreateMkStream(ctx, testStream, testGroup, "0").Err()
	if err != nil && err.Error() != "BUSYGROUP Consumer Group name already exists" {
		return fmt.Errorf("x group create mk stream: %w", err)
	}
	fmt.Println("OK   XGroupCreateMkStream")

	id, err := client.XAdd(ctx, &redis.XAddArgs{
		Stream: testStream,
		Values: map[string]any{
			"conversation_id": "11111111-1111-1111-1111-111111111111",
			"agent_id":        "22222222-2222-2222-2222-222222222222",
			"trigger_type":    "chat_message",
			"message":         "livecheck",
		},
	}).Result()
	if err != nil {
		return fmt.Errorf("x add: %w", err)
	}
	fmt.Printf("OK   XAdd id=%s\n", id)

	streams, err := client.XReadGroup(ctx, &redis.XReadGroupArgs{
		Group:    testGroup,
		Consumer: "livecheck-consumer",
		Streams:  []string{testStream, ">"},
		Count:    10,
		Block:    2 * time.Second,
	}).Result()
	if err != nil {
		return fmt.Errorf("x read group: %w", err)
	}
	if len(streams) != 1 || len(streams[0].Messages) != 1 {
		return fmt.Errorf("x read group: got %d streams, expected 1 with 1 message", len(streams))
	}
	msg := streams[0].Messages[0]
	fmt.Printf("OK   XReadGroup id=%s values=%v\n", msg.ID, msg.Values)

	if err := client.XAck(ctx, testStream, testGroup, msg.ID).Err(); err != nil {
		return fmt.Errorf("x ack: %w", err)
	}
	fmt.Println("OK   XAck")
	fmt.Println("ALL CHECKS PASSED")
	return nil
}
