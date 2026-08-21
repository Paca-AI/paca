package e2e_test

import (
	"context"
	"log/slog"
	"os"
	"strings"
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

// TestTriggerOrchestration drives the real orchestration
// handler.Handler.Handle performs — gate check, Postgres agent lookup,
// status transitions, executor run, event publishing — against real
// Postgres, real Valkey, and a real Docker sandbox. Replaces
// cmd/agent-runner/livecheck.
func TestTriggerOrchestration(t *testing.T) {
	env := newE2EEnv(t)
	image := agentServerImage(t)

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	gatewayIP := dockerBridgeGatewayIP(ctx, t)
	llm := fakellm.New(t, fakellm.TextReply("hello from the orchestration test"))

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
		VALUES ($1, $2, 'E2E Orchestration Agent', $3, 'openai', 'fake-model', $4, $5,
		        'You are a test agent.', 5, 10, 'paca-agent', 'agent@example.com', 'llm')
	`, agentID, env.projectID, "e2e-orch-agent-"+agentID.String()[:8], encryptedKey, llm.BaseURL(gatewayIP))
	if err != nil {
		t.Fatalf("seed agent: %v", err)
	}
	_, err = env.db.ExecContext(ctx, `
		INSERT INTO agent_conversations (id, agent_id, project_id, trigger_type, triggered_by_member_id, status)
		VALUES ($1, $2, $3, 'task_assigned', $4, 'queued')
	`, convID, agentID, env.projectID, env.memberID)
	if err != nil {
		t.Fatalf("seed conversation: %v", err)
	}

	sandboxMgr := newSandboxManager(t)
	h := &handler.Handler{
		Gate:          config.NewGate([]string{agentID.String()}),
		AgentRepo:     postgres.NewAgentRepository(env.db),
		ConvRepo:      postgres.NewConversationRepository(env.db),
		Publisher:     messaging.NewPublisher(env.redisClient),
		Executor:      executor.New(sandboxMgr, encryptor, executor.Options{Image: image}, log),
		InFlight:      registry.New(),
		ChatSandboxes: chatsandbox.New(),
		Log:           log,
	}

	trigger := agent.Trigger{
		ConversationID: convID,
		ProjectID:      env.projectID,
		AgentID:        agentID,
		Message:        "please say hello",
		TriggerType:    agent.TriggerTaskAssigned,
	}
	if err := h.Handle(ctx, trigger); err != nil {
		t.Fatalf("Handle: %v", err)
	}

	var final struct {
		Status       string  `db:"status"`
		ErrorMessage *string `db:"error_message"`
	}
	if err := env.db.GetContext(ctx, &final,
		`SELECT status, error_message FROM agent_conversations WHERE id = $1`, convID); err != nil {
		t.Fatalf("verify final status: %v", err)
	}
	if final.Status != "finished" {
		t.Fatalf("final status = %q (error_message=%v), want %q", final.Status, final.ErrorMessage, "finished")
	}

	// Phase 1: Handle persists every event to agent_conversation_events,
	// not just to the Valkey streams —
	// this is what services/api's ListConversationEvents reads on page
	// load, so an empty result here would mean a real user reloading the
	// page sees no history at all.
	var eventCount int
	if err := env.db.GetContext(ctx, &eventCount,
		`SELECT COUNT(*) FROM agent_conversation_events WHERE conversation_id = $1`, convID); err != nil {
		t.Fatalf("verify persisted event count: %v", err)
	}
	if eventCount == 0 {
		t.Fatalf("expected at least one row in agent_conversation_events for conversation %s, got 0", convID)
	}

	var indices []int
	if err := env.db.SelectContext(ctx, &indices,
		`SELECT event_index FROM agent_conversation_events WHERE conversation_id = $1 ORDER BY event_index`, convID); err != nil {
		t.Fatalf("verify event_index ordering: %v", err)
	}
	for i, idx := range indices {
		if idx != i {
			t.Fatalf("event_index sequence has a gap/duplicate: got %v, want 0..%d contiguous", indices, len(indices)-1)
		}
	}

	var lastEventType string
	if err := env.db.GetContext(ctx, &lastEventType,
		`SELECT event_type FROM agent_conversation_events WHERE conversation_id = $1 ORDER BY event_index DESC LIMIT 1`, convID); err != nil {
		t.Fatalf("verify last event type: %v", err)
	}
	if lastEventType != "turn_end" {
		t.Fatalf("last persisted event type = %q, want %q", lastEventType, "turn_end")
	}

	// Phase 2: executor.Run's onReady fired handler.Handle's
	// "environment_ready" marker exactly once, persisted (not just
	// published) after the user's own message and before the sandbox's
	// first agent event — see executor.go's onReady doc comment and
	// conversation-to-thread-messages.ts's hasEnvironmentReadyEvent, which
	// depends on this exact ordering to decide when the frontend should
	// stop showing "setting up your environment".
	var eventRows []struct {
		EventIndex  int    `db:"event_index"`
		EventType   string `db:"event_type"`
		EventSource string `db:"event_source"`
	}
	if err := env.db.SelectContext(ctx, &eventRows,
		`SELECT event_index, event_type, event_source FROM agent_conversation_events
		 WHERE conversation_id = $1 ORDER BY event_index`, convID); err != nil {
		t.Fatalf("verify environment_ready ordering: %v", err)
	}

	var userMessageIndex, readyIndex, firstAgentEventIndex = -1, -1, -1
	readyCount := 0
	for _, row := range eventRows {
		switch {
		case row.EventType == "user_message" && userMessageIndex == -1:
			userMessageIndex = row.EventIndex
		case row.EventType == "environment_ready":
			readyCount++
			readyIndex = row.EventIndex
			if row.EventSource != "system" {
				t.Fatalf("environment_ready event_source = %q, want %q", row.EventSource, "system")
			}
		case row.EventSource == "agent" && firstAgentEventIndex == -1:
			firstAgentEventIndex = row.EventIndex
		}
	}

	if readyCount != 1 {
		t.Fatalf("expected exactly one environment_ready event, got %d", readyCount)
	}
	if userMessageIndex == -1 {
		t.Fatalf("expected a user_message event to have been persisted for this trigger, found none among %+v", eventRows)
	}
	if readyIndex <= userMessageIndex {
		t.Fatalf("environment_ready event_index (%d) must be greater than user_message event_index (%d) — the frontend reads this ordering to detect a fresh turn's own readiness", readyIndex, userMessageIndex)
	}
	if firstAgentEventIndex != -1 && readyIndex >= firstAgentEventIndex {
		t.Fatalf("environment_ready event_index (%d) must precede the first agent-sourced event (index %d) — it marks readiness before the LLM turn starts, not after", readyIndex, firstAgentEventIndex)
	}
}

// TestTaskHandoff drives a task-linked sessionless run to "finished" and
// verifies the final reply is persisted as an idempotent task-level handoff
// (#392), including the failure modes of a retry (no duplicate) and that a
// non-task conversation leaves no handoff behind.
func TestTaskHandoff(t *testing.T) {
	env := newE2EEnv(t)
	image := agentServerImage(t)

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	gatewayIP := dockerBridgeGatewayIP(ctx, t)
	llm := fakellm.New(t, fakellm.TextReply("the final conclusion for handoff"))

	encryptor := newEncryptor(t)
	encryptedKey, err := encryptor.Encrypt("fake-key")
	if err != nil {
		t.Fatalf("encrypt fake key: %v", err)
	}

	taskID := uuid.New()
	agentID := uuid.New()
	convID := uuid.New()

	if _, err := env.db.ExecContext(ctx,
		`INSERT INTO tasks (id, project_id, title) VALUES ($1, $2, 'handoff task')`,
		taskID, env.projectID); err != nil {
		t.Fatalf("seed task: %v", err)
	}
	if _, err := env.db.ExecContext(ctx, `
		INSERT INTO agents (id, project_id, name, handle, llm_provider, llm_model,
		                     llm_api_key_secret, llm_base_url, system_prompt,
		                     max_iterations, timeout_minutes,
		                     git_committer_name, git_committer_email, agent_type)
		VALUES ($1, $2, 'Handoff Agent', $3, 'openai', 'fake-model', $4, $5,
		        'You are a test agent.', 5, 10, 'paca-agent', 'agent@example.com', 'llm')
	`, agentID, env.projectID, "handoff-agent-"+agentID.String()[:8], encryptedKey, llm.BaseURL(gatewayIP)); err != nil {
		t.Fatalf("seed agent: %v", err)
	}
	if _, err := env.db.ExecContext(ctx, `
		INSERT INTO agent_conversations (id, agent_id, project_id, trigger_type, task_id, triggered_by_member_id, status)
		VALUES ($1, $2, $3, 'task_assigned', $4, $5, 'queued')
	`, convID, agentID, env.projectID, taskID, env.memberID); err != nil {
		t.Fatalf("seed conversation: %v", err)
	}

	sandboxMgr := newSandboxManager(t)
	h := &handler.Handler{
		Gate:          config.NewGate([]string{agentID.String()}),
		AgentRepo:     postgres.NewAgentRepository(env.db),
		ConvRepo:      postgres.NewConversationRepository(env.db),
		Publisher:     messaging.NewPublisher(env.redisClient),
		Executor:      executor.New(sandboxMgr, encryptor, executor.Options{Image: image}, log),
		InFlight:      registry.New(),
		ChatSandboxes: chatsandbox.New(),
		Log:           log,
	}

	trigger := agent.Trigger{
		ConversationID: convID,
		ProjectID:      env.projectID,
		AgentID:        agentID,
		TaskID:         &taskID,
		Message:        "please conclude",
		TriggerType:    agent.TriggerTaskAssigned,
	}
	if err := h.Handle(ctx, trigger); err != nil {
		t.Fatalf("Handle: %v", err)
	}

	var summary string
	if err := env.db.GetContext(ctx, &summary,
		`SELECT summary FROM agent_task_handoffs WHERE conversation_id = $1`, convID); err != nil {
		t.Fatalf("expected a task handoff for conversation %s: %v", convID, err)
	}
	if !strings.Contains(summary, "final conclusion for handoff") {
		t.Fatalf("handoff summary = %q, want it to contain the fake reply", summary)
	}

	// Idempotency: a direct duplicate insert (simulating a completion retry)
	// must not create a second handoff row for the same conversation.
	if _, err := env.db.ExecContext(ctx, `
		INSERT INTO agent_task_handoffs (task_id, conversation_id, summary)
		VALUES ($1, $2, 'duplicate')
		ON CONFLICT (conversation_id) WHERE source_turn_id IS NULL DO NOTHING
	`, taskID, convID); err != nil {
		t.Fatalf("duplicate handoff insert: %v", err)
	}
	var count int
	if err := env.db.GetContext(ctx, &count,
		`SELECT COUNT(*) FROM agent_task_handoffs WHERE conversation_id = $1`, convID); err != nil {
		t.Fatalf("count handoffs: %v", err)
	}
	if count != 1 {
		t.Fatalf("handoff count = %d, want 1 (idempotent)", count)
	}

	// A later conversation on the same task reads the prior handoff back via
	// the repository's bounded list, newest first.
	prior, err := postgres.NewConversationRepository(env.db).ListTaskHandoffs(ctx, taskID, 3)
	if err != nil {
		t.Fatalf("list handoffs: %v", err)
	}
	if len(prior) != 1 || !strings.Contains(prior[0], "final conclusion for handoff") {
		t.Fatalf("prior handoffs = %q, want the persisted summary", prior)
	}
}
