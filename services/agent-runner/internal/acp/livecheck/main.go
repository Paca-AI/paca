// livecheck is a throwaway program (not part of the package build) used to
// verify the acp.Client sketch against a real, running `goose serve`
// container — not just the httptest-mocked transcripts in client_test.go,
// which were typed by hand from the spike and could silently diverge from
// reality. Run manually, pointed at the spike's container:
//
//	go run ./internal/acp/livecheck <baseURL> <secretKey>
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/Paca-AI/agent-runner/internal/acp"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "FAIL %v\n", err)
		os.Exit(1)
	}
}

// run holds everything that used to live directly in main, so os.Exit never
// runs past a defer that matters (see cmd/agent-runner/livecheck-stop's
// identical fix, and its doc comment on why that mattered for real once).
func run() error {
	if len(os.Args) != 3 {
		return fmt.Errorf("usage: livecheck <baseURL> <secretKey>")
	}
	baseURL, secretKey := os.Args[1], os.Args[2]

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	c := acp.NewClient(baseURL, secretKey, nil)

	if err := c.Initialize(ctx); err != nil {
		return fmt.Errorf("initialize: %w", err)
	}
	fmt.Println("OK   Initialize")

	sessionID, err := c.NewSession(ctx, "/home/goose", nil)
	if err != nil {
		return fmt.Errorf("new session: %w", err)
	}
	fmt.Printf("OK   NewSession sessionId=%s\n", sessionID)

	var toolCallEvents, otherEvents int
	stopReason, err := c.Prompt(ctx, sessionID, []acp.ContentBlock{acp.TextBlock("please run: echo real-go-client-check")}, 5,
		func(e acp.Event) {
			switch e.Kind {
			case acp.UpdateToolCall:
				toolCallEvents++
			case acp.UpdateToolCallUpdate:
				var u acp.ToolCallUpdate
				if err := json.Unmarshal(e.Raw, &u); err != nil {
					fmt.Fprintf(os.Stderr, "   (failed to decode tool_call_update: %v)\n", err)
					return
				}
				fmt.Printf("   tool_call_update status=%s text=%q\n", u.Status, u.Text())
			default:
				otherEvents++
			}
		})

	if errors.Is(err, acp.ErrMaxToolCalls) {
		fmt.Printf("OK   Prompt hit ErrMaxToolCalls as expected against a live, non-converging mock (toolCallEvents=%d)\n", toolCallEvents)
		return nil
	}
	if err != nil {
		return fmt.Errorf("prompt: %w", err)
	}
	fmt.Printf("OK   Prompt stopReason=%s toolCallEvents=%d otherEvents=%d\n", stopReason, toolCallEvents, otherEvents)
	return nil
}
