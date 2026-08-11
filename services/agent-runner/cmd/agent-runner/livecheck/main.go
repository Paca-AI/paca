// livecheck exercises the same orchestration cmd/agent-runner's
// triggerHandler.Handle performs — gate check, Postgres agent lookup,
// status transitions, executor run, event publishing — against the real
// dev Postgres and a real Docker sandbox.
//
// Deliberately does NOT go through messaging.Consumer or the real
// paca:agent:triggers stream: that stream is also read by the live
// services/ai-agent Python service (a different consumer group gets an
// independent full copy of every message — see messaging.consumerGroup's
// doc comment), which would try to act on a throwaway test trigger too.
// The Valkey stream mechanics themselves were already verified for real in
// internal/messaging/livecheck, against a throwaway stream key instead.
//
// Publishing to the real paca:agent:events stream is left in, though —
// unlike the triggers stream, nothing but services/realtime reads it, and
// it only fans events out to a Socket.IO room for this test's own
// fabricated conversation_id, which no real browser tab is subscribed to.
//
// Inserts one throwaway agent + conversation row into the real dev
// Postgres, deletes the agent (cascades) when done.
//
//	go run ./cmd/agent-runner/livecheck <postgres-dsn> <redis-addr> <image-ref> <fake-llm-base-url>
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

// livecheckHexKey is 32 bytes hex-encoded (64 chars) — built with Repeat
// rather than a hand-typed literal after an earlier version of this exact
// pattern elsewhere in this session had a silently-wrong length from manual
// counting.
var livecheckHexKey = strings.Repeat("ef", 32)

func main() {
	if len(os.Args) != 5 {
		fmt.Fprintln(os.Stderr, "usage: livecheck <postgres-dsn> <redis-addr> <image-ref> <fake-llm-base-url>")
		os.Exit(2)
	}
	dsn, redisAddr, image, fakeLLMBaseURL := os.Args[1], os.Args[2], os.Args[3], os.Args[4]

	log := slog.New(slog.NewTextHandler(os.Stderr, nil))

	db, err := postgres.Open(dsn)
	if err != nil {
		fatalf("Open: %v", err)
	}
	defer func() { _ = db.Close() }()

	redisClient := redis.NewClient(&redis.Options{Addr: redisAddr})
	defer func() { _ = redisClient.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	agentID, convID, err := seedThrowawayRows(ctx, db, fakeLLMBaseURL)
	if err != nil {
		fatalf("seed: %v", err)
	}
	fmt.Printf("OK   seeded throwaway agent=%s conversation=%s\n", agentID, convID)
	defer func() {
		if _, err := db.ExecContext(context.Background(), `DELETE FROM agents WHERE id = $1`, agentID); err != nil {
			fmt.Fprintf(os.Stderr, "WARN cleanup failed, delete manually: DELETE FROM agents WHERE id = '%s'; -- %v\n", agentID, err)
		} else {
			fmt.Println("OK   cleaned up throwaway rows")
		}
	}()

	key, err := secret.DecodeHexKey(livecheckHexKey)
	if err != nil {
		panic(err)
	}
	encryptor, err := secret.NewEncryptor(key)
	if err != nil {
		panic(err)
	}

	sandboxMgr, err := sandbox.NewManager(19200, 100)
	if err != nil {
		fatalf("NewManager: %v", err)
	}

	h := &handler.Handler{
		Gate:          config.NewGate([]string{agentID.String()}),
		AgentRepo:     postgres.NewAgentRepository(db),
		ConvRepo:      postgres.NewConversationRepository(db),
		Publisher:     messaging.NewPublisher(redisClient),
		Executor:      executor.New(sandboxMgr, encryptor, executor.Options{Image: image}, log),
		InFlight:      registry.New(),
		ChatSandboxes: chatsandbox.New(),
		Log:           log,
	}

	// TriggerTaskAssigned (a non-chat trigger), not TriggerChatMessage:
	// this program's own purpose is the general orchestration path (gate
	// check, agent lookup, status transitions, executor run, event
	// publishing), not chat continuity specifically — a chat trigger now
	// pauses (keeps its sandbox alive) instead of tearing down on a natural
	// finish, which would leak a running container here since nothing in
	// this short-lived program ever reattaches to or stops it. See the
	// dedicated cmd/agent-runner/livecheck-chat for the chat-continuity
	// behavior this deliberately no longer exercises.
	trigger := agent.Trigger{
		ConversationID: convID,
		AgentID:        agentID,
		Message:        "please say hello",
		TriggerType:    agent.TriggerTaskAssigned,
	}

	// Drives the real production handler.Handler.Handle directly — not a
	// hand-copied subset of its logic — so this stays accurate as that
	// logic evolves instead of silently drifting out of sync with it.
	if err := h.Handle(ctx, trigger); err != nil {
		fatalf("Handle: %v", err)
	}
	fmt.Println("OK   Handle completed")

	var final struct {
		Status       string  `db:"status"`
		ErrorMessage *string `db:"error_message"`
	}
	if err := db.GetContext(ctx, &final,
		`SELECT status, error_message FROM agent_conversations WHERE id = $1`, convID); err != nil {
		fatalf("verify final status: %v", err)
	}
	fmt.Printf("OK   final DB status=%q error_message=%v\n", final.Status, final.ErrorMessage)

	// Phase 1 (see docs/ai-agent/goose-migration.md): Handle now persists
	// every event to agent_conversation_events, not just to the Valkey
	// streams above — this is what services/api's ListConversationEvents
	// reads on page load, so an empty result here would mean a real user
	// reloading the page sees no history at all.
	var eventCount int
	if err := db.GetContext(ctx, &eventCount,
		`SELECT COUNT(*) FROM agent_conversation_events WHERE conversation_id = $1`, convID); err != nil {
		fatalf("verify persisted event count: %v", err)
	}
	if eventCount == 0 {
		fatalf("expected at least one row in agent_conversation_events for conversation %s, got 0", convID)
	}
	fmt.Printf("OK   %d event(s) persisted to agent_conversation_events\n", eventCount)

	var indices []int
	if err := db.SelectContext(ctx, &indices,
		`SELECT event_index FROM agent_conversation_events WHERE conversation_id = $1 ORDER BY event_index`, convID); err != nil {
		fatalf("verify event_index ordering: %v", err)
	}
	for i, idx := range indices {
		if idx != i {
			fatalf("event_index sequence has a gap/duplicate: got %v, want 0..%d contiguous", indices, len(indices)-1)
		}
	}
	fmt.Printf("OK   event_index sequence is contiguous: %v\n", indices)

	var lastEventType string
	if err := db.GetContext(ctx, &lastEventType,
		`SELECT event_type FROM agent_conversation_events WHERE conversation_id = $1 ORDER BY event_index DESC LIMIT 1`, convID); err != nil {
		fatalf("verify last event type: %v", err)
	}
	if lastEventType != "turn_end" {
		fatalf("expected the last persisted event to be turn_end, got %q", lastEventType)
	}
	fmt.Println("OK   last persisted event is turn_end")

	fmt.Println("ALL CHECKS PASSED")
}

func seedThrowawayRows(ctx context.Context, db *sqlx.DB, fakeLLMBaseURL string) (agentID, convID uuid.UUID, err error) {
	key, err := secret.DecodeHexKey(livecheckHexKey)
	if err != nil {
		return uuid.Nil, uuid.Nil, err
	}
	encryptor, err := secret.NewEncryptor(key)
	if err != nil {
		return uuid.Nil, uuid.Nil, err
	}
	encryptedKey, err := encryptor.Encrypt("fake-key")
	if err != nil {
		return uuid.Nil, uuid.Nil, err
	}

	agentID = uuid.New()
	convID = uuid.New()

	_, err = db.ExecContext(ctx, `
		INSERT INTO agents (id, project_id, name, handle, llm_provider, llm_model,
		                     llm_api_key_secret, llm_base_url, system_prompt,
		                     max_iterations, timeout_minutes,
		                     git_committer_name, git_committer_email, agent_type)
		SELECT $1, id, 'Livecheck Agent', $2, 'openai', 'fake-model', $3, $4,
		       'You are a test agent.', 5, 10, 'paca-agent', 'agent@example.com', 'llm'
		FROM projects LIMIT 1
	`, agentID, "livecheck-agent-"+agentID.String()[:8], encryptedKey, fakeLLMBaseURL)
	if err != nil {
		return uuid.Nil, uuid.Nil, fmt.Errorf("insert throwaway agent (is there at least one project in this DB?): %w", err)
	}

	_, err = db.ExecContext(ctx, `
		INSERT INTO agent_conversations (id, agent_id, project_id, trigger_type, triggered_by_member_id, status)
		SELECT $1, a.id, a.project_id, 'task_assigned', pm.id, 'queued'
		FROM agents a
		JOIN project_members pm ON pm.project_id = a.project_id
		WHERE a.id = $2
		LIMIT 1
	`, convID, agentID)
	if err != nil {
		return uuid.Nil, uuid.Nil, fmt.Errorf("insert throwaway conversation: %w", err)
	}

	return agentID, convID, nil
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "FAIL "+format+"\n", args...)
	os.Exit(1)
}
