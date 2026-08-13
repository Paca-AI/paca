package e2e_test

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/Paca-AI/agent-runner/internal/acp"
	"github.com/Paca-AI/agent-runner/internal/sandbox"
	"github.com/Paca-AI/agent-runner/test/e2e/fakellm"
)

// TestACPClientAgainstRealGooseContainer verifies acp.Client against a real,
// running `goose serve` container — not just the httptest-mocked
// transcripts in internal/acp/client_test.go, which were typed by hand from
// a protocol spike and could silently diverge from reality. Replaces
// internal/acp/livecheck.
//
// Unlike that program (which expected an operator to hand it an
// already-running container's baseURL/secretKey), this test starts its own
// container via sandbox.Manager — the same production container-lifecycle
// code internal/sandbox/livecheck (see sandbox_manager_test.go) already
// exercises directly, rather than a second, testcontainers-driven
// reimplementation of `goose serve`'s invocation that could drift from it.
// No Node/MCP is needed for a plain ACP handshake, so the raw upstream
// image is enough.
func TestACPClientAgainstRealGooseContainer(t *testing.T) {
	if os.Getenv("PACA_E2E") != "1" {
		t.Skip("set PACA_E2E=1 to run e2e tests (requires Docker)")
	}
	checkDockerAvailable(t)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	gatewayIP := dockerBridgeGatewayIP(ctx, t)
	// A single scripted tool-call reply — the last (only) script entry
	// repeats forever, so the LLM never actually converges on a final
	// answer. This mirrors what the original livecheck program actually
	// observed running against a real spike container ("Prompt hit
	// ErrMaxToolCalls as expected against a live, non-converging mock"),
	// not an assumption: whether goose can actually execute the requested
	// tool doesn't matter here, since either outcome (tool succeeds or
	// errors) still reports back to the LLM for another turn.
	llm := fakellm.New(t, fakellm.ToolCallReply("shell", map[string]any{"command": "echo e2e-acp-client-check"}))

	mgr := newSandboxManager(t)
	handle, err := mgr.Start(ctx, sandbox.Config{
		ConversationID: "e2e-acp-client-conv",
		Image:          rawUpstreamGooseImage(t),
		Env: map[string]string{
			"GOOSE_PROVIDER": "openai",
			"GOOSE_MODEL":    "fake-model",
			"OPENAI_API_KEY": "fake-key",
			"OPENAI_HOST":    llm.BaseURL(gatewayIP),
		},
		GitCommitterName:  "paca-agent",
		GitCommitterEmail: "agent@example.com",
	})
	if err != nil {
		t.Fatalf("start sandbox: %v", err)
	}
	t.Cleanup(func() {
		if err := mgr.Stop(context.Background(), handle); err != nil {
			t.Errorf("cleanup: stop sandbox: %v", err)
		}
	})

	c := acp.NewClient(handle.BaseURL, handle.SecretKey, nil)

	if err := c.Initialize(ctx); err != nil {
		t.Fatalf("Initialize: %v", err)
	}

	sessionID, err := c.NewSession(ctx, "/home/goose", nil)
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	if sessionID == "" {
		t.Fatal("NewSession returned an empty session ID")
	}

	var toolCallEvents int
	_, err = c.Prompt(ctx, sessionID, []acp.ContentBlock{acp.TextBlock("please run: echo e2e-acp-client-check")}, 5,
		func(e acp.Event) {
			if e.Kind == acp.UpdateToolCall {
				toolCallEvents++
			}
		})

	if errors.Is(err, acp.ErrMaxToolCalls) {
		if toolCallEvents == 0 {
			t.Fatal("hit ErrMaxToolCalls but observed zero tool_call events — Prompt's max-tool-calls cutoff fired without the client ever seeing a tool call")
		}
		return
	}
	if err != nil {
		t.Fatalf("Prompt: %v", err)
	}
	// A real Goose build could plausibly converge instead of hitting the
	// cap (e.g. if it treats an unrecognized tool as an immediate error and
	// stops) — either outcome demonstrates a working ACP round trip against
	// a real container, so this isn't treated as a failure, only logged.
	t.Logf("Prompt converged without hitting ErrMaxToolCalls (toolCallEvents=%d)", toolCallEvents)
}
