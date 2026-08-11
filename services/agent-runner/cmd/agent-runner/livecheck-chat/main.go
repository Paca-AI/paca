// livecheck-chat exercises chat conversation continuity end to end against
// real Docker, real Postgres, and a real Valkey: two turns of the same chat
// conversation through the real handler.Handler.Handle, asserting the
// second turn reuses the first turn's sandbox container rather than
// cold-starting a new one, that the conversation sits at status "paused"
// between turns (not "finished"), that a heartbeat control message refreshes
// the paused sandbox's idle timer, and that the idle reaper's own logic
// (chatsandbox.Registry.FindIdle + Handler.TeardownPausedChatSandbox — the
// exact functions cmd/agent-runner's reaper goroutine calls, not a
// hand-copied subset) actually tears a stale sandbox down.
//
//	go run ./cmd/agent-runner/livecheck-chat <postgres-dsn> <redis-addr> <image-ref> <fake-llm-base-url>
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/redis/go-redis/v9"

	"github.com/Paca-AI/agent-runner/internal/agent"
	"github.com/Paca-AI/agent-runner/internal/chatsandbox"
	"github.com/Paca-AI/agent-runner/internal/config"
	"github.com/Paca-AI/agent-runner/internal/executor"
	"github.com/Paca-AI/agent-runner/internal/handler"
	"github.com/Paca-AI/agent-runner/internal/messaging"
	"github.com/Paca-AI/agent-runner/internal/registry"
	"github.com/Paca-AI/agent-runner/internal/repository/postgres"
	"github.com/Paca-AI/agent-runner/internal/sandbox"
	"github.com/Paca-AI/agent-runner/internal/secret"
)

var livecheckHexKey = strings.Repeat("17", 32)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "FAIL %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	if len(os.Args) != 5 {
		return fmt.Errorf("usage: livecheck-chat <postgres-dsn> <redis-addr> <image-ref> <fake-llm-base-url>")
	}
	dsn, redisAddr, image, fakeLLMBaseURL := os.Args[1], os.Args[2], os.Args[3], os.Args[4]

	log := slog.New(slog.NewTextHandler(os.Stderr, nil))

	db, err := postgres.Open(dsn)
	if err != nil {
		return fmt.Errorf("open: %w", err)
	}
	defer func() { _ = db.Close() }()

	redisClient := redis.NewClient(&redis.Options{Addr: redisAddr})
	defer func() { _ = redisClient.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	agentID, convID, projectID, err := seedThrowawayRows(ctx, db, fakeLLMBaseURL)
	if err != nil {
		return fmt.Errorf("seed: %w", err)
	}
	fmt.Printf("OK   seeded throwaway agent=%s conversation=%s project=%s\n", agentID, convID, projectID)
	defer func() {
		if _, err := db.ExecContext(context.Background(), `DELETE FROM agents WHERE id = $1`, agentID); err != nil {
			fmt.Fprintf(os.Stderr, "WARN cleanup failed, delete manually: DELETE FROM agents WHERE id = '%s'; -- %v\n", agentID, err)
		} else {
			fmt.Println("OK   cleaned up throwaway rows")
		}
	}()

	key, err := secret.DecodeHexKey(livecheckHexKey)
	if err != nil {
		return err
	}
	encryptor, err := secret.NewEncryptor(key)
	if err != nil {
		return err
	}

	sandboxMgr, err := sandbox.NewManager(19400, 100)
	if err != nil {
		return fmt.Errorf("new manager: %w", err)
	}

	chatSandboxes := chatsandbox.New()
	h := &handler.Handler{
		Gate:          config.NewGate([]string{agentID.String()}),
		AgentRepo:     postgres.NewAgentRepository(db),
		ConvRepo:      postgres.NewConversationRepository(db),
		Publisher:     messaging.NewPublisher(redisClient),
		Executor:      executor.New(sandboxMgr, encryptor, executor.Options{Image: image}, log),
		InFlight:      registry.New(),
		ChatSandboxes: chatSandboxes,
		Log:           log,
	}

	// A cleanup net for anything left running if a later assertion fails
	// before the deliberate teardown at the end of this check.
	defer func() {
		if state, ok := chatSandboxes.Pop(convID); ok {
			_ = h.Executor.StopSandbox(context.Background(), state.Handle)
		}
	}()

	// --- Turn 1: cold start -------------------------------------------------

	turn1 := agent.Trigger{
		ConversationID: convID,
		ProjectID:      projectID,
		AgentID:        agentID,
		Message:        "turn one",
		TriggerType:    agent.TriggerChatMessage,
	}
	if err := h.Handle(ctx, turn1); err != nil {
		return fmt.Errorf("handle (turn 1): %w", err)
	}
	status, err := conversationStatus(ctx, db, convID)
	if err != nil {
		return err
	}
	if status != "paused" {
		return fmt.Errorf("status after turn 1 = %q, want %q", status, "paused")
	}
	fmt.Println("OK   turn 1: status is \"paused\"")

	state1, ok := chatSandboxes.Get(convID)
	if !ok {
		return fmt.Errorf("no chat sandbox registered for %s after turn 1", convID)
	}
	containerID1 := state1.Handle.ContainerID
	fmt.Printf("OK   turn 1: chat sandbox registered, container=%s\n", containerID1)

	// --- Heartbeat between turns ---------------------------------------------

	before := state1.LastActiveAt
	time.Sleep(50 * time.Millisecond) // ensure a measurable time.Now() delta
	if err := h.HandleControl(ctx, messaging.Control{
		Type: messaging.ControlHeartbeat, ConversationID: convID, ProjectID: projectID,
	}); err != nil {
		return fmt.Errorf("handle control (heartbeat): %w", err)
	}
	state1, _ = chatSandboxes.Get(convID)
	if !state1.LastActiveAt.After(before) {
		return fmt.Errorf("heartbeat did not refresh LastActiveAt: before=%v after=%v", before, state1.LastActiveAt)
	}
	fmt.Println("OK   heartbeat control message refreshed LastActiveAt")

	// --- Turn 2: resume -------------------------------------------------------

	turn2 := agent.Trigger{
		ConversationID: convID,
		ProjectID:      projectID,
		AgentID:        agentID,
		Message:        "turn two",
		TriggerType:    agent.TriggerChatMessage,
	}
	if err := h.Handle(ctx, turn2); err != nil {
		return fmt.Errorf("handle (turn 2): %w", err)
	}
	status, err = conversationStatus(ctx, db, convID)
	if err != nil {
		return err
	}
	if status != "paused" {
		return fmt.Errorf("status after turn 2 = %q, want %q", status, "paused")
	}
	fmt.Println("OK   turn 2: status is \"paused\"")

	state2, ok := chatSandboxes.Get(convID)
	if !ok {
		return fmt.Errorf("no chat sandbox registered for %s after turn 2", convID)
	}
	if state2.Handle.ContainerID != containerID1 {
		return fmt.Errorf("turn 2 used a different container (%s) than turn 1 (%s) — sandbox was not reused",
			state2.Handle.ContainerID, containerID1)
	}
	fmt.Printf("OK   turn 2: reused turn 1's container (%s)\n", containerID1)

	var eventIndices []int
	if err := db.SelectContext(ctx, &eventIndices,
		`SELECT event_index FROM agent_conversation_events WHERE conversation_id = $1 ORDER BY event_index`, convID); err != nil {
		return fmt.Errorf("verify event_index continuity: %w", err)
	}
	for i, idx := range eventIndices {
		if idx != i {
			return fmt.Errorf("event_index sequence across both turns has a gap/duplicate: got %v, want 0..%d contiguous",
				eventIndices, len(eventIndices)-1)
		}
	}
	fmt.Printf("OK   event_index sequence spans both turns contiguously: %v\n", eventIndices)

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
		return fmt.Errorf("FindIdle did not report %s as idle when it clearly is: %v", convID, idle)
	}
	fmt.Println("OK   FindIdle reports the paused conversation as idle once past the timeout")

	if !h.TeardownPausedChatSandbox(ctx, convID) {
		return fmt.Errorf("TeardownPausedChatSandbox returned false for a conversation known to have a paused sandbox")
	}
	if _, ok := chatSandboxes.Get(convID); ok {
		return fmt.Errorf("chat sandbox still registered after TeardownPausedChatSandbox")
	}
	status, err = conversationStatus(ctx, db, convID)
	if err != nil {
		return err
	}
	if status != "stopped" {
		return fmt.Errorf("status after idle teardown = %q, want %q", status, "stopped")
	}
	fmt.Println("OK   idle teardown stopped the sandbox and recorded status \"stopped\"")

	fmt.Println("ALL CHECKS PASSED")
	return nil
}

func conversationStatus(ctx context.Context, db *sqlx.DB, convID uuid.UUID) (string, error) {
	var status string
	if err := db.GetContext(ctx, &status, `SELECT status FROM agent_conversations WHERE id = $1`, convID); err != nil {
		return "", fmt.Errorf("query conversation status: %w", err)
	}
	return status, nil
}

func seedThrowawayRows(ctx context.Context, db *sqlx.DB, fakeLLMBaseURL string) (agentID, convID, projectID uuid.UUID, err error) {
	key, err := secret.DecodeHexKey(livecheckHexKey)
	if err != nil {
		return uuid.Nil, uuid.Nil, uuid.Nil, err
	}
	encryptor, err := secret.NewEncryptor(key)
	if err != nil {
		return uuid.Nil, uuid.Nil, uuid.Nil, err
	}
	encryptedKey, err := encryptor.Encrypt("fake-key")
	if err != nil {
		return uuid.Nil, uuid.Nil, uuid.Nil, err
	}

	agentID = uuid.New()
	convID = uuid.New()

	if err := db.GetContext(ctx, &projectID, `SELECT id FROM projects LIMIT 1`); err != nil {
		return uuid.Nil, uuid.Nil, uuid.Nil, fmt.Errorf("find a project to seed against: %w", err)
	}

	_, err = db.ExecContext(ctx, `
		INSERT INTO agents (id, project_id, name, handle, llm_provider, llm_model,
		                     llm_api_key_secret, llm_base_url, system_prompt,
		                     max_iterations, timeout_minutes,
		                     git_committer_name, git_committer_email, agent_type)
		VALUES ($1, $2, 'Livecheck Chat Agent', $3, 'openai', 'fake-model', $4, $5,
		        'You are a test agent.', 5, 10, 'paca-agent', 'agent@example.com', 'llm')
	`, agentID, projectID, "livecheck-chat-agent-"+agentID.String()[:8], encryptedKey, fakeLLMBaseURL)
	if err != nil {
		return uuid.Nil, uuid.Nil, uuid.Nil, fmt.Errorf("insert throwaway agent: %w", err)
	}

	_, err = db.ExecContext(ctx, `
		INSERT INTO agent_conversations (id, agent_id, project_id, trigger_type, triggered_by_member_id, status)
		SELECT $1, $2, $3, 'chat_message', pm.id, 'queued'
		FROM project_members pm WHERE pm.project_id = $3 LIMIT 1
	`, convID, agentID, projectID)
	if err != nil {
		return uuid.Nil, uuid.Nil, uuid.Nil, fmt.Errorf("insert throwaway conversation: %w", err)
	}

	return agentID, convID, projectID, nil
}
