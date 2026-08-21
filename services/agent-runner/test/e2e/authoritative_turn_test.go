package e2e_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"github.com/Paca-AI/agent-runner/internal/config"
	"github.com/Paca-AI/agent-runner/internal/executor"
	"github.com/Paca-AI/agent-runner/internal/handler"
	"github.com/Paca-AI/agent-runner/internal/messaging"
	"github.com/Paca-AI/agent-runner/internal/repository/postgres"
	"github.com/Paca-AI/agent-runner/internal/turnruntime"
	"github.com/Paca-AI/agent-runner/test/e2e/fakellm"
)

// TestAuthoritativeTurnDurableDelivery covers the production runner seam that
// unit tests cannot: a real Valkey consumer group delivers the same durable
// turn request twice, the authoritative handler executes/finalizes exactly
// once, and the terminal replay is acknowledged without a second result. The
// API suite separately covers transaction -> outbox -> real Valkey; together
// the two blocking suites cover both sides of that durable boundary.
func TestAuthoritativeTurnDurableDelivery(t *testing.T) {
	t.Run("llm stable result", testAuthoritativeLLMDurableDelivery)
	t.Run("acp fail closed", testAuthoritativeACPFailClosedDelivery)
}

func testAuthoritativeLLMDurableDelivery(t *testing.T) {
	env := newE2EEnv(t)
	image := agentServerImage(t)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	gatewayIP := dockerBridgeGatewayIP(ctx, t)
	llm := fakellm.New(t, fakellm.TextReply("authoritative stable answer"))
	encryptor := newEncryptor(t)
	encryptedKey, err := encryptor.Encrypt("fake-provider-key")
	if err != nil {
		t.Fatal(err)
	}
	agentID := uuid.New()
	if _, err := env.db.ExecContext(ctx, `INSERT INTO agents
		(id,project_id,name,handle,llm_provider,llm_model,llm_api_key_secret,llm_base_url,
		 system_prompt,max_iterations,timeout_minutes,git_committer_name,git_committer_email,agent_type)
		VALUES ($1,$2,'Authoritative E2E',$3,'openai','fake-model',$4,$5,'',5,10,
		        'paca-agent','agent@example.com','llm')`,
		agentID, env.projectID, "authoritative-e2e-"+agentID.String()[:8], encryptedKey, llm.BaseURL(gatewayIP)); err != nil {
		t.Fatal(err)
	}
	sessionID, claimToken := uuid.New(), uuid.New()
	runtime := newDurableTurnRuntime(&turnruntime.Envelope{
		TurnID: uuid.New(), RunID: uuid.New(), ClaimToken: &claimToken,
		ConversationID: uuid.New(), SessionID: &sessionID,
		ProjectID: env.projectID, AgentID: agentID, RequestedByMemberID: &env.memberID,
		InputText: "return the scripted stable answer", Backend: "llm", Attempt: 1, Status: "running",
		ToolPolicy:       turnruntime.ToolPolicy{Version: 1, Mode: "deny_by_default", ContextMayGrant: false},
		SnapshotManifest: json.RawMessage(`{"schema_version":1}`), SnapshotRenderedText: "",
	})
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	h := &handler.Handler{
		TurnRuntime: runtime, WorkerID: "authoritative-e2e", Gate: config.NewGate([]string{"*"}),
		AgentRepo: postgres.NewAgentRepository(env.db),
		Executor:  executor.New(newSandboxManager(t), encryptor, executor.Options{Image: image}, log),
		Log:       log,
	}
	runDurableTurnMessages(t, ctx, env.redisClient, runtime.envelope.TurnID, h.HandleTurn, runtime.done)

	final, events, claims := runtime.snapshot()
	if final == nil || final.TerminalStatus != "succeeded" || final.StableOutputEventID == nil {
		t.Fatalf("authoritative LLM finalization = %+v", final)
	}
	if claims != 2 {
		t.Fatalf("claim calls = %d, want initial delivery plus terminal replay", claims)
	}
	stable := false
	for _, event := range events {
		if event.EventType == turnruntime.StableOutputEventType {
			stable = true
			var payload map[string]string
			if err := json.Unmarshal(event.Payload, &payload); err != nil || payload["text"] != "authoritative stable answer" {
				t.Fatalf("stable output payload=%s err=%v", event.Payload, err)
			}
		}
	}
	if !stable {
		t.Fatal("authoritative LLM did not persist a stable output event")
	}
}

func testAuthoritativeACPFailClosedDelivery(t *testing.T) {
	client := newE2ERedisClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	claimToken := uuid.New()
	runtime := newDurableTurnRuntime(&turnruntime.Envelope{
		TurnID: uuid.New(), RunID: uuid.New(), ClaimToken: &claimToken,
		ConversationID: uuid.New(), ProjectID: uuid.New(), AgentID: uuid.New(),
		InputText: "private input must not leave Paca", Backend: "acp", Attempt: 1, Status: "running",
	})
	h := &handler.Handler{
		TurnRuntime: runtime, WorkerID: "authoritative-acp-e2e",
		Gate: config.NewGate([]string{"*"}), Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	runDurableTurnMessages(t, ctx, client, runtime.envelope.TurnID, h.HandleTurn, runtime.done)

	final, events, claims := runtime.snapshot()
	if final == nil || final.TerminalStatus != "failed" || final.ErrorCode == nil ||
		*final.ErrorCode != "acp_private_runtime_not_isolated" {
		t.Fatalf("authoritative ACP finalization = %+v", final)
	}
	if len(events) != 0 {
		t.Fatalf("ACP private input produced execution events before fail-close: %+v", events)
	}
	if claims != 2 {
		t.Fatalf("claim calls = %d, want initial delivery plus terminal replay", claims)
	}
}

func runDurableTurnMessages(
	t *testing.T,
	ctx context.Context,
	client *redis.Client,
	turnID uuid.UUID,
	handlerFn messaging.TurnHandler,
	finalized <-chan struct{},
) {
	t.Helper()
	const group = "agent-turn-workers"
	_ = client.XGroupDestroy(ctx, messaging.StreamAgentTurnRequests, group).Err()
	_ = client.Del(ctx, messaging.StreamAgentTurnRequests).Err()
	t.Cleanup(func() {
		_ = client.XGroupDestroy(context.Background(), messaging.StreamAgentTurnRequests, group).Err()
		_ = client.Del(context.Background(), messaging.StreamAgentTurnRequests).Err()
	})
	for i := 0; i < 2; i++ {
		if err := client.XAdd(ctx, &redis.XAddArgs{
			Stream: messaging.StreamAgentTurnRequests,
			Values: map[string]any{"turn_id": turnID.String(), "outbox_event_id": "duplicate-e2e"},
		}).Err(); err != nil {
			t.Fatal(err)
		}
	}
	consumerCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	consumer := messaging.NewTurnConsumer(client, 2, handlerFn, slog.New(slog.NewTextHandler(os.Stderr, nil)))
	go func() {
		defer close(done)
		consumer.Run(consumerCtx)
	}()
	select {
	case <-finalized:
	case <-ctx.Done():
		t.Fatalf("turn did not finalize: %v", ctx.Err())
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		pending, pendingErr := client.XPending(ctx, messaging.StreamAgentTurnRequests, group).Result()
		length, lengthErr := client.XLen(ctx, messaging.StreamAgentTurnRequests).Result()
		if pendingErr == nil && lengthErr == nil && pending.Count == 0 && length == 0 {
			cancel()
			<-done
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	cancel()
	<-done
	t.Fatal("duplicate durable turn messages were not both acknowledged")
}

type durableTurnRuntime struct {
	mu       sync.Mutex
	envelope *turnruntime.Envelope
	events   []turnruntime.Event
	final    *turnruntime.FinalizeInput
	claims   int
	done     chan struct{}
	doneOnce sync.Once
}

func newDurableTurnRuntime(envelope *turnruntime.Envelope) *durableTurnRuntime {
	return &durableTurnRuntime{envelope: envelope, done: make(chan struct{})}
}

func (r *durableTurnRuntime) Claim(_ context.Context, turnID uuid.UUID, _ string, _ time.Duration) (*turnruntime.Envelope, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.claims++
	if turnID != r.envelope.TurnID {
		return nil, &turnruntime.APIError{Status: 404, Code: "TURN_NOT_FOUND", Message: "not found"}
	}
	if r.final != nil {
		return nil, &turnruntime.APIError{Status: 409, Code: "TURN_FINALIZED", Message: "terminal"}
	}
	copy := *r.envelope
	return &copy, nil
}

func (r *durableTurnRuntime) Get(_ context.Context, turnID uuid.UUID) (*turnruntime.Envelope, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if turnID != r.envelope.TurnID {
		return nil, &turnruntime.APIError{Status: 404, Code: "TURN_NOT_FOUND", Message: "not found"}
	}
	copy := *r.envelope
	if r.final != nil {
		status := r.final.TerminalStatus
		copy.TerminalStatus = &status
		copy.Status = status
	}
	return &copy, nil
}

func (r *durableTurnRuntime) Renew(_ context.Context, turnID, runID, token uuid.UUID, lease time.Duration) (time.Time, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.final != nil {
		return time.Time{}, &turnruntime.APIError{Status: 409, Code: "TURN_FINALIZED", Message: "terminal"}
	}
	if turnID != r.envelope.TurnID || runID != r.envelope.RunID || r.envelope.ClaimToken == nil || token != *r.envelope.ClaimToken {
		return time.Time{}, &turnruntime.APIError{Status: 409, Code: "TURN_CLAIM_LOST", Message: "stale claim"}
	}
	return time.Now().Add(lease), nil
}

func (r *durableTurnRuntime) AppendEvent(_ context.Context, turnID uuid.UUID, event turnruntime.Event) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.final != nil || turnID != r.envelope.TurnID || event.RunID != r.envelope.RunID ||
		r.envelope.ClaimToken == nil || event.ClaimToken != *r.envelope.ClaimToken {
		return &turnruntime.APIError{Status: 409, Code: "TURN_CLAIM_LOST", Message: "stale event"}
	}
	r.events = append(r.events, event)
	return nil
}

func (r *durableTurnRuntime) Finalize(_ context.Context, turnID uuid.UUID, input turnruntime.FinalizeInput) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.final != nil {
		return &turnruntime.APIError{Status: 409, Code: "TURN_FINALIZED", Message: "terminal"}
	}
	if turnID != r.envelope.TurnID || input.RunID != r.envelope.RunID ||
		r.envelope.ClaimToken == nil || input.ClaimToken != *r.envelope.ClaimToken {
		return &turnruntime.APIError{Status: 409, Code: "TURN_CLAIM_LOST", Message: "stale finalize"}
	}
	copy := input
	r.final = &copy
	r.doneOnce.Do(func() { close(r.done) })
	return nil
}

func (r *durableTurnRuntime) snapshot() (*turnruntime.FinalizeInput, []turnruntime.Event, int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var final *turnruntime.FinalizeInput
	if r.final != nil {
		copy := *r.final
		final = &copy
	}
	events := append([]turnruntime.Event(nil), r.events...)
	return final, events, r.claims
}
