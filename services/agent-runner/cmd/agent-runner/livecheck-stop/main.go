// livecheck-stop verifies that HandleControl's interrupt actually reaches
// an in-flight Handle call — against real Docker, real Postgres, and a
// real Valkey — and that the conversation ends up "stopped" rather than
// "failed". The scenario: point the sandbox at a fake LLM scripted to
// return the same tool call forever (the exact non-converging shape from
// the mcpServers wire-format spike, see docs/ai-agent/goose-migration.md),
// start Handle in a goroutine so it would otherwise run until
// MaxIterations or the turn timeout, then call HandleControl with an
// agent.stop directive shortly after and confirm Handle returns quickly
// and the DB reflects "stopped".
//
//	go run ./cmd/agent-runner/livecheck-stop <postgres-dsn> <redis-addr> <image-ref> <fake-llm-base-url>
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

var livecheckHexKey = strings.Repeat("cd", 32)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "FAIL %v\n", err)
		os.Exit(1)
	}
}

// run holds everything that used to live directly in main, specifically so
// the deferred throwaway-row cleanup below actually executes on a failure
// path: os.Exit (what the old fatalf helper called) skips deferred
// functions entirely, so a failed check used to leak the throwaway agent
// and conversation rows into the dev DB — caught for real: an earlier run
// of this exact check, on a fake LLM server that didn't actually loop
// (a harness mistake unrelated to this bug), exited via the old fatalf
// path and left both a throwaway agent row and a running sandbox container
// behind. Same fix as internal/executor/livecheck.
func run() error {
	if len(os.Args) != 5 {
		return fmt.Errorf("usage: livecheck-stop <postgres-dsn> <redis-addr> <image-ref> <fake-llm-base-url>")
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

	// A generous outer bound — the point of this check is that Handle
	// returns in a few seconds after the stop signal, well inside this.
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Second)
	defer cancel()

	agentID, convID, err := seedThrowawayRows(ctx, db, fakeLLMBaseURL)
	if err != nil {
		return fmt.Errorf("seed: %w", err)
	}
	fmt.Printf("OK   seeded throwaway agent=%s conversation=%s (max_iterations=1000, so it won't stop on its own)\n", agentID, convID)
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

	sandboxMgr, err := sandbox.NewManager(19300, 100)
	if err != nil {
		return fmt.Errorf("new manager: %w", err)
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
		return fmt.Errorf("InFlight.Interrupt: conversation not found in the registry — Handle isn't registering it, or finished before we got here")
	}
	fmt.Println("OK   sent interrupt (simulating HandleControl's agent.stop path)")

	select {
	case err := <-handleDone:
		elapsed := time.Since(start)
		if err != nil {
			return fmt.Errorf("handle returned an error: %w", err)
		}
		fmt.Printf("OK   Handle returned %s after start (well before max_iterations=1000 could finish it)\n", elapsed)
	case <-time.After(40 * time.Second):
		fmt.Println("STILL WAITING after 40s post-interrupt — not failing yet, just reporting")
		select {
		case err := <-handleDone:
			elapsed := time.Since(start)
			fmt.Printf("   (eventually returned %s after start, err=%v)\n", elapsed, err)
		case <-time.After(60 * time.Second):
			return fmt.Errorf("handle did not return even after 100s total — stop is not working")
		}
	}

	var final struct {
		Status       string  `db:"status"`
		ErrorMessage *string `db:"error_message"`
	}
	if err := db.GetContext(context.Background(), &final,
		`SELECT status, error_message FROM agent_conversations WHERE id = $1`, convID); err != nil {
		return fmt.Errorf("verify final status: %w", err)
	}
	fmt.Printf("OK   final DB status=%q error_message=%v\n", final.Status, final.ErrorMessage)
	if final.Status != "stopped" {
		return fmt.Errorf("status = %q, want \"stopped\"", final.Status)
	}

	fmt.Println("ALL CHECKS PASSED")
	return nil
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

	// max_iterations=1000 and timeout_minutes=30 (the column defaults,
	// left unset here) — both far longer than this check's own patience,
	// so Handle returning promptly can only be explained by the interrupt
	// actually working, not by hitting either limit on its own.
	_, err = db.ExecContext(ctx, `
		INSERT INTO agents (id, project_id, name, handle, llm_provider, llm_model,
		                     llm_api_key_secret, llm_base_url, system_prompt,
		                     max_iterations, git_committer_name, git_committer_email, agent_type)
		SELECT $1, id, 'Livecheck Stop Agent', $2, 'openai', 'fake-model', $3, $4,
		       'You are a test agent.', 1000, 'paca-agent', 'agent@example.com', 'llm'
		FROM projects LIMIT 1
	`, agentID, "livecheck-stop-agent-"+agentID.String()[:8], encryptedKey, fakeLLMBaseURL)
	if err != nil {
		return uuid.Nil, uuid.Nil, fmt.Errorf("insert throwaway agent (is there at least one project in this DB?): %w", err)
	}

	_, err = db.ExecContext(ctx, `
		INSERT INTO agent_conversations (id, agent_id, project_id, trigger_type, triggered_by_member_id, status)
		SELECT $1, a.id, a.project_id, 'chat_message', pm.id, 'queued'
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
