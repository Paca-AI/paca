package e2e_test

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Paca-AI/agent-runner/internal/acp"
	"github.com/Paca-AI/agent-runner/internal/agent"
	"github.com/Paca-AI/agent-runner/internal/executor"
	"github.com/Paca-AI/agent-runner/test/e2e/fakellm"
)

// TestExecutorRun runs Executor.Run against a real Docker daemon and a real
// goose container — not mocks — pointed at a scripted OpenAI-compatible
// fake LLM server, so it exercises the full path with no real API spend:
// secret decryption, provider env mapping, container spawn, ACP handshake,
// prompt building, and event delivery. Replaces internal/executor/livecheck
// (its no-MCP-args invocation); see TestExecutorRunWithMCP for the
// PacaAPIKey-set path that program's optional trailing args exercised.
func TestExecutorRun(t *testing.T) {
	if os.Getenv("PACA_E2E") != "1" {
		t.Skip("set PACA_E2E=1 to run e2e tests (requires Docker)")
	}
	checkDockerAvailable(t)
	image := agentServerImage(t)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	encryptor := newEncryptor(t)
	encryptedKey, err := encryptor.Encrypt("fake-key")
	if err != nil {
		t.Fatalf("encrypt fake key: %v", err)
	}

	gatewayIP := dockerBridgeGatewayIP(ctx, t)
	llm := fakellm.New(t, fakellm.TextReply("hello from the fake LLM"))

	sandboxMgr := newSandboxManager(t)
	exec := executor.New(sandboxMgr, encryptor, executor.Options{Image: image}, log)

	projectID := uuid.New()
	cfg := agent.Config{
		ID:                uuid.New(),
		Name:              "E2E Executor Agent",
		Handle:            "e2e-executor-agent",
		LLMProvider:       "openai",
		LLMModel:          "fake-model",
		LLMAPIKeySecret:   encryptedKey,
		LLMBaseURL:        llm.BaseURL(gatewayIP),
		SystemPrompt:      "You are a test agent.",
		MaxIterations:     5,
		GitCommitterName:  "paca-agent",
		GitCommitterEmail: "agent@example.com",
	}
	trigger := agent.Trigger{
		ConversationID: uuid.New(),
		ProjectID:      projectID,
		AgentID:        cfg.ID,
		Message:        "please say hello",
		TriggerType:    agent.TriggerChatMessage,
	}

	var events []acp.Event
	result, err := exec.Run(ctx, cfg, trigger, nil, func(e acp.Event) {
		events = append(events, e)
	})
	if result.Handle != nil {
		t.Cleanup(func() {
			if err := exec.StopSandbox(context.Background(), result.Handle); err != nil {
				t.Errorf("cleanup: stop sandbox: %v", err)
			}
		})
	}
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(events) == 0 {
		t.Fatal("Run produced zero ACP events")
	}

	var sawAgentReply bool
	for _, e := range events {
		if e.Kind == acp.UpdateAgentMessageChunk {
			var chunk acp.AgentMessageChunk
			if err := json.Unmarshal(e.Raw, &chunk); err == nil && chunk.Content.Text != "" {
				sawAgentReply = true
			}
		}
	}
	if !sawAgentReply {
		t.Fatalf("Run completed (stopReason=%s) but no agent_message_chunk with text was observed among %d events", result.StopReason, len(events))
	}
	if llm.CallCount() == 0 {
		t.Fatal("the fake LLM server was never called — the sandbox never reached it")
	}
}

// TestExecutorRunWithMCP is TestExecutorRun's PacaAPIKey-set counterpart —
// wires up the built-in Paca MCP server (executor.Options.PacaAPIKey/
// PacaAPIURL) so the spawned `paca-mcp` subprocess actually starts inside
// the sandbox, the same path a bare-digest, Node-less image can't take (see
// services/agent-server/Dockerfile's doc comment on the hang this once
// caused). Requires the Node+MCP-enabled AGENT_SERVER_IMAGE, unlike
// TestExecutorRun which only strictly needs it because both tests are kept
// on the same image tier for realism (see helpers_test.go's
// agentServerImage doc comment).
//
// executor.buildMCPServers always sets PACA_AGENT_ID, so the spawned
// paca-mcp process always calls PACA_API_URL's
// /api/v1/projects/{id}/members/me/permissions at startup (a project-scoped
// trigger, not a global-scope agent — see apps/mcp/src/permissions.ts's
// isGlobalAgent branch) — stubbed below so the run is deterministic instead
// of depending on that call's own timeout/retry behavior against an
// unreachable address.
func TestExecutorRunWithMCP(t *testing.T) {
	if os.Getenv("PACA_E2E") != "1" {
		t.Skip("set PACA_E2E=1 to run e2e tests (requires Docker)")
	}
	checkDockerAvailable(t)
	image := agentServerImage(t)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	encryptor := newEncryptor(t)
	encryptedKey, err := encryptor.Encrypt("fake-key")
	if err != nil {
		t.Fatalf("encrypt fake key: %v", err)
	}

	gatewayIP := dockerBridgeGatewayIP(ctx, t)
	llm := fakellm.New(t, fakellm.TextReply("hello with mcp wired up"))

	projectID := uuid.New()

	// httptest.NewServer's default listener binds 127.0.0.1 only, which the
	// sandbox container (reaching this process via the Docker bridge
	// gateway, not loopback) could never connect to — build our own
	// 0.0.0.0-bound listener first, same reasoning as fakellm.New's.
	permissionsLn, err := new(net.ListenConfig).Listen(ctx, "tcp", "0.0.0.0:0")
	if err != nil {
		t.Fatalf("listen for permissions stub: %v", err)
	}
	permissionsSrv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		wantPath := "/api/v1/projects/" + projectID.String() + "/members/me/permissions"
		if r.URL.Path != wantPath {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"permissions": map[string]bool{}})
	}))
	_ = permissionsSrv.Listener.Close()
	permissionsSrv.Listener = permissionsLn
	permissionsSrv.Start()
	t.Cleanup(permissionsSrv.Close)
	pacaAPIURL := fmt.Sprintf("http://%s:%d", gatewayIP, permissionsLn.Addr().(*net.TCPAddr).Port)

	sandboxMgr := newSandboxManager(t)
	exec := executor.New(sandboxMgr, encryptor, executor.Options{
		Image:      image,
		PacaAPIKey: "e2e-fake-paca-api-key",
		PacaAPIURL: pacaAPIURL,
	}, log)

	cfg := agent.Config{
		ID:                uuid.New(),
		Name:              "E2E Executor MCP Agent",
		Handle:            "e2e-executor-mcp-agent",
		LLMProvider:       "openai",
		LLMModel:          "fake-model",
		LLMAPIKeySecret:   encryptedKey,
		LLMBaseURL:        llm.BaseURL(gatewayIP),
		SystemPrompt:      "You are a test agent.",
		MaxIterations:     5,
		GitCommitterName:  "paca-agent",
		GitCommitterEmail: "agent@example.com",
	}
	trigger := agent.Trigger{
		ConversationID: uuid.New(),
		ProjectID:      projectID,
		AgentID:        cfg.ID,
		Message:        "please say hello",
		TriggerType:    agent.TriggerChatMessage,
	}

	var events []acp.Event
	result, err := exec.Run(ctx, cfg, trigger, nil, func(e acp.Event) {
		events = append(events, e)
	})
	if result.Handle != nil {
		t.Cleanup(func() {
			if err := exec.StopSandbox(context.Background(), result.Handle); err != nil {
				t.Errorf("cleanup: stop sandbox: %v", err)
			}
		})
	}
	if err != nil {
		t.Fatalf("Run (with MCP wired up): %v", err)
	}
	if len(events) == 0 {
		t.Fatal("Run produced zero ACP events")
	}
}
