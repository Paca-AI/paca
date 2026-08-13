package e2e_test

import (
	"context"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/Paca-AI/agent-runner/internal/sandbox"
)

// TestSandboxManagerLifecycle exercises sandbox.Manager against the real
// Docker daemon — not a mock. No LLM provider or MCP config is needed here:
// this only checks container start/health-check/stop, so the raw upstream
// goose image (no Node) is enough. Replaces internal/sandbox/livecheck.
func TestSandboxManagerLifecycle(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping Docker-heavy test in -short mode")
	}
	if os.Getenv("PACA_E2E") != "1" {
		t.Skip("set PACA_E2E=1 to run e2e tests (requires Docker)")
	}
	checkDockerAvailable(t)

	mgr := newSandboxManager(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	handle, err := mgr.Start(ctx, sandbox.Config{
		ConversationID:    "e2e-sandbox-conv-1",
		Image:             rawUpstreamGooseImage(t),
		Env:               map[string]string{"GOOSE_PROVIDER": "openai", "GOOSE_MODEL": "fake-model"},
		GitCommitterName:  "paca-agent",
		GitCommitterEmail: "agent@example.com",
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	stopped := false
	t.Cleanup(func() {
		if stopped {
			return
		}
		if err := mgr.Stop(context.Background(), handle); err != nil {
			t.Errorf("cleanup: stop sandbox: %v", err)
		}
	})
	if handle.ContainerID == "" || handle.BaseURL == "" || handle.SecretKey == "" {
		t.Fatalf("Start returned an incomplete handle: %+v", handle)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, handle.BaseURL+"/status", nil)
	if err != nil {
		t.Fatalf("build status request: %v", err)
	}
	req.Header.Set("X-Secret-Key", handle.SecretKey)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /status: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode >= 500 {
		t.Fatalf("GET /status = %d, want < 500", resp.StatusCode)
	}

	if err := mgr.Stop(ctx, handle); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	stopped = true
}
