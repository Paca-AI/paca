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

// TestChatContinuity exercises chat conversation continuity end to end
// against real Docker, real Postgres, and real Valkey: two turns of the
// same chat conversation through the real handler.Handler.Handle, asserting
// the second turn reuses the first turn's sandbox container rather than
// cold-starting a new one, that the conversation sits at status "paused"
// between turns (not "finished"), that a heartbeat control message
// refreshes the paused sandbox's idle timer, and that the idle reaper's own
// logic (chatsandbox.Registry.FindIdle + Handler.TeardownPausedChatSandbox
// — the exact functions cmd/agent-runner's reaper goroutine calls, not a
// hand-copied subset) actually tears a stale sandbox down. Replaces
// cmd/agent-runner/livecheck-chat.
func TestChatContinuity(t *testing.T) {
	env := newE2EEnv(t)
	image := agentServerImage(t)

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	gatewayIP := dockerBridgeGatewayIP(ctx, t)
	llm := fakellm.New(t, fakellm.TextReply("hello from the chat continuity test"))

	encryptor := newEncryptor(t)
	encryptedKey, err := encryptor.Encrypt("fake-key")
	if err != nil {
		t.Fatalf("encrypt fake key: %v", err)
	}

	agentID := uuid.New()
	convID := uuid.New()
	_, err = env.db.ExecContext(ctx, `
		INSERT INTO agents (id, project_id, name, handle, llm_provider, llm_model,
		                     llm_api_key_secret, llm_base_url, system_prompt,
		                     max_iterations, timeout_minutes,
		                     git_committer_name, git_committer_email, agent_type)
		VALUES ($1, $2, 'E2E Chat Agent', $3, 'openai', 'fake-model', $4, $5,
		        'You are a test agent.', 5, 10, 'paca-agent', 'agent@example.com', 'llm')
	`, agentID, env.projectID, "e2e-chat-agent-"+agentID.String()[:8], encryptedKey, llm.BaseURL(gatewayIP))
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
	chatSandboxes := chatsandbox.New()
	h := &handler.Handler{
		Gate:            config.NewGate([]string{agentID.String()}),
		AgentRepo:       postgres.NewAgentRepository(env.db),
		ConvRepo:        postgres.NewConversationRepository(env.db),
		Publisher:       messaging.NewPublisher(env.redisClient),
		Executor:        executor.New(sandboxMgr, postgres.NewEnvironmentRepository(env.db), postgres.NewConversationRepository(env.db), postgres.NewPortForwardRepository(env.db), encryptor, executor.Options{Image: image}, log),
		InFlight:        registry.New(),
		ChatSandboxes:   chatSandboxes,
		EnvironmentRepo: postgres.NewEnvironmentRepository(env.db),
		Log:             log,
	}

	// A cleanup net for anything left running if a later assertion fails
	// before the deliberate idle-reaper teardown near the end of this test.
	t.Cleanup(func() {
		if state, ok := chatSandboxes.Pop(convID); ok {
			_ = h.Executor.StopSandbox(context.Background(), state.Handle)
		}
	})

	// --- Turn 1: cold start -------------------------------------------------

	turn1 := agent.Trigger{
		ConversationID: convID,
		ProjectID:      env.projectID,
		AgentID:        agentID,
		Message:        "turn one",
		TriggerType:    agent.TriggerChatMessage,
	}
	if err := h.Handle(ctx, turn1); err != nil {
		t.Fatalf("handle (turn 1): %v", err)
	}
	status, err := conversationStatus(ctx, env.db, convID)
	if err != nil {
		t.Fatal(err)
	}
	if status != "paused" {
		t.Fatalf("status after turn 1 = %q, want %q", status, "paused")
	}

	state1, ok := chatSandboxes.Get(convID)
	if !ok {
		t.Fatalf("no chat sandbox registered for %s after turn 1", convID)
	}
	containerID1 := state1.Handle.ContainerID

	// --- Heartbeat between turns ---------------------------------------------

	before := state1.LastActiveAt
	time.Sleep(50 * time.Millisecond) // ensure a measurable time.Now() delta
	if err := h.HandleControl(ctx, messaging.Control{
		Type: messaging.ControlHeartbeat, ConversationID: convID, ProjectID: env.projectID,
	}); err != nil {
		t.Fatalf("handle control (heartbeat): %v", err)
	}
	state1, _ = chatSandboxes.Get(convID)
	if !state1.LastActiveAt.After(before) {
		t.Fatalf("heartbeat did not refresh LastActiveAt: before=%v after=%v", before, state1.LastActiveAt)
	}

	// --- Turn 2: resume -------------------------------------------------------

	turn2 := agent.Trigger{
		ConversationID: convID,
		ProjectID:      env.projectID,
		AgentID:        agentID,
		Message:        "turn two",
		TriggerType:    agent.TriggerChatMessage,
	}
	if err := h.Handle(ctx, turn2); err != nil {
		t.Fatalf("handle (turn 2): %v", err)
	}
	status, err = conversationStatus(ctx, env.db, convID)
	if err != nil {
		t.Fatal(err)
	}
	if status != "paused" {
		t.Fatalf("status after turn 2 = %q, want %q", status, "paused")
	}

	state2, ok := chatSandboxes.Get(convID)
	if !ok {
		t.Fatalf("no chat sandbox registered for %s after turn 2", convID)
	}
	if state2.Handle.ContainerID != containerID1 {
		t.Fatalf("turn 2 used a different container (%s) than turn 1 (%s) — sandbox was not reused",
			state2.Handle.ContainerID, containerID1)
	}

	var eventIndices []int
	if err := env.db.SelectContext(ctx, &eventIndices,
		`SELECT event_index FROM agent_conversation_events WHERE conversation_id = $1 ORDER BY event_index`, convID); err != nil {
		t.Fatalf("verify event_index continuity: %v", err)
	}
	for i, idx := range eventIndices {
		if idx != i {
			t.Fatalf("event_index sequence across both turns has a gap/duplicate: got %v, want 0..%d contiguous",
				eventIndices, len(eventIndices)-1)
		}
	}

	// --- Idle reaper (exercising the exact functions the real reaper goroutine calls) ---

	farFuture := time.Now().Add(time.Hour)
	idle := chatSandboxes.FindIdle(farFuture, time.Minute, h.InFlight.IsRegistered)
	found := false
	for _, id := range idle {
		if id == convID {
			found = true
		}
	}
	if !found {
		t.Fatalf("FindIdle did not report %s as idle when it clearly is: %v", convID, idle)
	}

	if !h.TeardownPausedChatSandbox(ctx, convID) {
		t.Fatal("TeardownPausedChatSandbox returned false for a conversation known to have a paused sandbox")
	}
	if _, ok := chatSandboxes.Get(convID); ok {
		t.Fatal("chat sandbox still registered after TeardownPausedChatSandbox")
	}
	status, err = conversationStatus(ctx, env.db, convID)
	if err != nil {
		t.Fatal(err)
	}
	if status != "stopped" {
		t.Fatalf("status after idle teardown = %q, want %q", status, "stopped")
	}
}
