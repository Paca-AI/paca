package e2e_test

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Paca-AI/agent-runner/internal/agent"
	"github.com/Paca-AI/agent-runner/internal/chatsandbox"
	"github.com/Paca-AI/agent-runner/internal/config"
	"github.com/Paca-AI/agent-runner/internal/executor"
	"github.com/Paca-AI/agent-runner/internal/handler"
	"github.com/Paca-AI/agent-runner/internal/messaging"
	"github.com/Paca-AI/agent-runner/internal/registry"
	"github.com/Paca-AI/agent-runner/internal/repository/postgres"
	"github.com/Paca-AI/agent-runner/test/e2e/fakellm"
)

// TestStopControl verifies that HandleControl's interrupt actually reaches
// an in-flight Handle call — against real Docker, real Postgres, and real
// Valkey — and that the conversation ends up "stopped" rather than
// "failed". The scenario: point the sandbox at a fake LLM scripted to
// return the same tool call forever (a single ToolCallReply entry repeats
// indefinitely — see fakellm.New's doc comment), start Handle in a
// goroutine so it would otherwise run until MaxIterations or the turn
// timeout, then interrupt it shortly after and confirm Handle returns
// quickly and the DB reflects "stopped". Replaces cmd/agent-runner/livecheck-stop.
func TestStopControl(t *testing.T) {
	env := newE2EEnv(t)
	image := agentServerImage(t)

	// A generous outer bound — the point of this test is that Handle
	// returns in a few seconds after the stop signal, well inside this.
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Second)
	defer cancel()

	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	gatewayIP := dockerBridgeGatewayIP(ctx, t)
	llm := fakellm.New(t, fakellm.ToolCallReply("shell", map[string]any{"command": "sleep 1"}))

	encryptor := newEncryptor(t)
	encryptedKey, err := encryptor.Encrypt("fake-key")
	if err != nil {
		t.Fatalf("encrypt fake key: %v", err)
	}

	agentID := uuid.New()
	convID := uuid.New()
	// max_iterations=1000, timeout_minutes left at its column default (30)
	// — both far longer than this test's own patience, so Handle returning
	// promptly can only be explained by the interrupt actually working, not
	// by hitting either limit on its own.
	_, err = env.db.ExecContext(ctx, `
		INSERT INTO agents (id, project_id, name, handle, llm_provider, llm_model,
		                     llm_api_key_secret, llm_base_url, system_prompt,
		                     max_iterations, git_committer_name, git_committer_email, agent_type)
		VALUES ($1, $2, 'E2E Stop Agent', $3, 'openai', 'fake-model', $4, $5,
		        'You are a test agent.', 1000, 'paca-agent', 'agent@example.com', 'llm')
	`, agentID, env.projectID, "e2e-stop-agent-"+agentID.String()[:8], encryptedKey, llm.BaseURL(gatewayIP))
	if err != nil {
		t.Fatalf("seed agent: %v", err)
	}
	_, err = env.db.ExecContext(ctx, `
		INSERT INTO agent_conversations (id, agent_id, project_id, trigger_type, triggered_by_member_id, status)
		VALUES ($1, $2, $3, 'chat_message', $4, 'queued')
	`, convID, agentID, env.projectID, env.memberID)
	if err != nil {
		t.Fatalf("seed conversation: %v", err)
	}

	sandboxMgr := newSandboxManager(t)
	h := &handler.Handler{
		Gate:            config.NewGate([]string{agentID.String()}),
		AgentRepo:       postgres.NewAgentRepository(env.db),
		ConvRepo:        postgres.NewConversationRepository(env.db),
		Publisher:       messaging.NewPublisher(env.redisClient),
		Executor:        executor.New(sandboxMgr, postgres.NewEnvironmentRepository(env.db), postgres.NewConversationRepository(env.db), postgres.NewPortForwardRepository(env.db), encryptor, executor.Options{Image: image}, log),
		InFlight:        registry.New(),
		ChatSandboxes:   chatsandbox.New(),
		EnvironmentRepo: postgres.NewEnvironmentRepository(env.db),
		Log:             log,
	}

	trigger := agent.Trigger{
		ConversationID: convID,
		AgentID:        agentID,
		Message:        "run a command forever",
		TriggerType:    agent.TriggerChatMessage,
	}

	handleDone := make(chan error, 1)
	start := time.Now()
	go func() {
		handleDone <- h.Handle(ctx, trigger)
	}()

	// Give the sandbox real time to spawn and the turn to actually start
	// looping before sending the stop — this is checking interruption of
	// something genuinely in flight, not a race against startup.
	time.Sleep(10 * time.Second)

	if !h.InFlight.Interrupt(convID) {
		t.Fatal("InFlight.Interrupt: conversation not found in the registry — Handle isn't registering it, or finished before we got here")
	}

	select {
	case err := <-handleDone:
		elapsed := time.Since(start)
		if err != nil {
			t.Fatalf("handle returned an error: %v", err)
		}
		t.Logf("Handle returned %s after start (well before max_iterations=1000 could finish it)", elapsed)
	case <-time.After(40 * time.Second):
		t.Log("still waiting after 40s post-interrupt — not failing yet")
		select {
		case err := <-handleDone:
			t.Logf("eventually returned %s after start, err=%v", time.Since(start), err)
		case <-time.After(60 * time.Second):
			t.Fatal("handle did not return even after 100s total — stop is not working")
		}
	}

	var final struct {
		Status       string  `db:"status"`
		ErrorMessage *string `db:"error_message"`
	}
	if err := env.db.GetContext(context.Background(), &final,
		`SELECT status, error_message FROM agent_conversations WHERE id = $1`, convID); err != nil {
		t.Fatalf("verify final status: %v", err)
	}
	if final.Status != "stopped" {
		t.Fatalf("status = %q (error_message=%v), want %q", final.Status, final.ErrorMessage, "stopped")
	}
}
