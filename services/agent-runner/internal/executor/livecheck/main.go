// livecheck runs Executor.Run against a real Docker daemon and a real
// goose container — not mocks — pointed at a scripted OpenAI-compatible
// fake LLM server (the same one services/ai-agent's e2e tests use) so it
// exercises the full path with no real API spend: secret decryption,
// provider env mapping, container spawn, ACP handshake, prompt building,
// and event delivery.
//
//	go run ./internal/executor/livecheck <image-ref> <fake-llm-base-url> [paca-api-key] [paca-api-url]
//
// The two optional trailing args wire up the built-in Paca MCP server (see
// executor.Options) — pass them to exercise that path; omit to skip it
// (executor.Run's PacaAPIKey=="" gate), matching every prior livecheck run
// in docs/ai-agent/goose-migration.md before the mcpServers wire-format fix.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/google/uuid"

	"github.com/Paca-AI/agent-runner/internal/acp"
	"github.com/Paca-AI/agent-runner/internal/agent"
	"github.com/Paca-AI/agent-runner/internal/executor"
	"github.com/Paca-AI/agent-runner/internal/sandbox"
	"github.com/Paca-AI/agent-runner/internal/secret"
)

// A throwaway key — this program never touches a real secret store.
const livecheckHexKey = "cdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcd"

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "FAIL %v\n", err)
		os.Exit(1)
	}
}

// run holds everything that used to live in main directly, specifically so
// deferred cleanup (stopping the sandbox Run leaves running — see Result's
// doc comment) actually executes on an error path: os.Exit skips deferred
// calls entirely, so main itself must never call it after a defer that
// matters has been registered.
func run() error {
	if len(os.Args) != 3 && len(os.Args) != 5 {
		return fmt.Errorf("usage: livecheck <image-ref> <fake-llm-base-url> [paca-api-key] [paca-api-url]")
	}
	image, fakeLLMBaseURL := os.Args[1], os.Args[2]
	var pacaAPIKey, pacaAPIURL string
	if len(os.Args) == 5 {
		pacaAPIKey, pacaAPIURL = os.Args[3], os.Args[4]
	}

	log := slog.New(slog.NewTextHandler(os.Stderr, nil))

	key, err := secret.DecodeHexKey(livecheckHexKey)
	if err != nil {
		return err
	}
	encryptor, err := secret.NewEncryptor(key)
	if err != nil {
		return err
	}
	encryptedKey, err := encryptor.Encrypt("fake-key")
	if err != nil {
		return err
	}

	sandboxMgr, err := sandbox.NewManager(19100, 100)
	if err != nil {
		return fmt.Errorf("new manager: %w", err)
	}

	exec := executor.New(sandboxMgr, encryptor, executor.Options{
		Image:      image,
		PacaAPIKey: pacaAPIKey,
		PacaAPIURL: pacaAPIURL,
	}, log)

	projectID := uuid.New()
	cfg := agent.Config{
		ID:                uuid.New(),
		Name:              "Livecheck Agent",
		Handle:            "livecheck-agent",
		LLMProvider:       "openai",
		LLMModel:          "fake-model",
		LLMAPIKeySecret:   encryptedKey,
		LLMBaseURL:        fakeLLMBaseURL,
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

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	var events []acp.Event
	result, err := exec.Run(ctx, cfg, trigger, nil, func(e acp.Event) {
		events = append(events, e)
		fmt.Printf("   event: kind=%s raw=%s\n", e.Kind, string(e.Raw))
	})
	// Run no longer tears the sandbox down itself (see Result's doc comment
	// — that decision moved to the caller once chat continuity needed it),
	// so this livecheck must do it explicitly now, regardless of outcome.
	if result.Handle != nil {
		defer func() {
			if stopErr := exec.StopSandbox(context.WithoutCancel(ctx), result.Handle); stopErr != nil {
				fmt.Fprintf(os.Stderr, "WARN failed to stop sandbox: %v\n", stopErr)
			}
		}()
	}
	if err != nil {
		return fmt.Errorf("run: %w", err)
	}

	fmt.Printf("OK   Run: stopReason=%s events=%d\n", result.StopReason, len(events))

	for _, e := range events {
		if e.Kind == acp.UpdateAgentMessageChunk {
			var chunk acp.AgentMessageChunk
			if err := json.Unmarshal(e.Raw, &chunk); err == nil {
				fmt.Printf("OK   agent said: %q\n", chunk.Content.Text)
			}
		}
	}
	return nil
}
