// livecheck exercises sandbox.Manager against the real Docker daemon — not
// a mock — before anything is built on top of it. Run manually:
//
//	go run ./internal/sandbox/livecheck <image-ref>
package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/Paca-AI/agent-runner/internal/sandbox"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "FAIL %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	if len(os.Args) != 2 {
		return fmt.Errorf("usage: livecheck <image-ref>")
	}
	image := os.Args[1]

	mgr, err := sandbox.NewManager(19000, 100)
	if err != nil {
		return fmt.Errorf("new manager: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	fmt.Println("Starting sandbox...")
	handle, err := mgr.Start(ctx, sandbox.Config{
		ConversationID:    "livecheck-conv-1",
		Image:             image,
		Env:               map[string]string{"GOOSE_PROVIDER": "openai", "GOOSE_MODEL": "fake-model"},
		GitCommitterName:  "paca-agent",
		GitCommitterEmail: "agent@example.com",
	})
	if err != nil {
		return fmt.Errorf("start: %w", err)
	}
	fmt.Printf("OK   Start: containerID=%s baseURL=%s\n", handle.ContainerID[:12], handle.BaseURL)

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, handle.BaseURL+"/status", nil)
	req.Header.Set("X-Secret-Key", handle.SecretKey)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		fmt.Fprintf(os.Stderr, "FAIL /status request: %v\n", err)
	} else {
		fmt.Printf("OK   /status -> HTTP %d\n", resp.StatusCode)
		_ = resp.Body.Close()
	}

	fmt.Println("Stopping sandbox...")
	if err := mgr.Stop(ctx, handle); err != nil {
		return fmt.Errorf("stop: %w", err)
	}
	fmt.Println("OK   Stop")
	return nil
}
