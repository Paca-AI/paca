package e2e_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/google/uuid"

	"github.com/Paca-AI/agent-runner/internal/acpbridge"
	"github.com/Paca-AI/agent-runner/internal/agent"
	"github.com/Paca-AI/agent-runner/internal/chatsandbox"
	"github.com/Paca-AI/agent-runner/internal/config"
	"github.com/Paca-AI/agent-runner/internal/executor"
	"github.com/Paca-AI/agent-runner/internal/handler"
	"github.com/Paca-AI/agent-runner/internal/messaging"
	"github.com/Paca-AI/agent-runner/internal/registry"
	"github.com/Paca-AI/agent-runner/internal/repository/postgres"
)

const (
	acpDispatchInternalToken = "e2e-acp-internal-token"
	acpDispatchBridgeToken   = "e2e-acp-bridge-token"
)

// TestACPBridgeDispatch exercises the ACP bridge end to end against real
// Docker-free infrastructure (real Postgres, real Valkey, a real
// HTTP+WebSocket server): a fake bridge daemon connects, authenticates with
// a bridge token, and receives a real "start_turn" dispatched through the
// actual production path (handler.Handler.Handle -> dispatchACP ->
// acpbridge.Dispatcher.DispatchTrigger), reports a turn_status back, and the
// internal status/disconnect REST endpoints are exercised against the same
// running server. Replaces cmd/agent-runner/livecheck-acp.
func TestACPBridgeDispatch(t *testing.T) {
	env := newE2EEnv(t)
	ctx := context.Background()
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))

	agentID, convID := seedACPAgentAndConversation(t, env)

	publisher := messaging.NewPublisher(env.redisClient)
	agentRepo := postgres.NewAgentRepository(env.db)
	convRepo := postgres.NewConversationRepository(env.db)
	acpRegistry := acpbridge.New(env.redisClient, publisher, log)

	_, thisFile, _, _ := runtime.Caller(0)
	llmModelsPath := filepath.Join(filepath.Dir(thisFile), "..", "..", "data", "llm_models.json")

	acpServer := &acpbridge.Server{
		Registry:      acpRegistry,
		AgentRepo:     agentRepo,
		ConvRepo:      convRepo,
		Publisher:     publisher,
		InternalToken: acpDispatchInternalToken,
		LLMModelsPath: llmModelsPath,
		Log:           log,
	}
	httpSrv := httptest.NewServer(acpServer.Routes())
	t.Cleanup(httpSrv.Close)

	// --- Connect a fake bridge daemon --------------------------------------

	wsURL := "ws" + strings.TrimPrefix(httpSrv.URL, "http") + "/agent-bridge/ws"
	daemon, dialResp, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("dial bridge ws: %v", err)
	}
	if dialResp != nil && dialResp.Body != nil {
		_ = dialResp.Body.Close()
	}
	t.Cleanup(func() { _ = daemon.CloseNow() })

	if err := wsjson.Write(ctx, daemon, map[string]string{
		"type": "hello", "agent_id": agentID.String(), "token": acpDispatchBridgeToken,
	}); err != nil {
		t.Fatalf("send hello: %v", err)
	}
	var ack map[string]string
	if err := wsjson.Read(ctx, daemon, &ack); err != nil {
		t.Fatalf("read hello_ack: %v", err)
	}
	if ack["type"] != "hello_ack" {
		t.Fatalf("got %v, want hello_ack", ack)
	}

	// --- Dispatch a trigger through the real production path ---------------

	sandboxMgr := newSandboxManager(t)
	encryptor := newEncryptor(t)
	h := &handler.Handler{
		Gate:          config.NewGate([]string{agentID.String()}),
		AgentRepo:     agentRepo,
		ConvRepo:      convRepo,
		Publisher:     publisher,
		Executor:      executor.New(sandboxMgr, postgres.NewEnvironmentRepository(env.db), postgres.NewConversationRepository(env.db), postgres.NewPortForwardRepository(env.db), encryptor, executor.Options{}, log),
		InFlight:      registry.New(),
		ChatSandboxes: chatsandbox.New(),
		ACPDispatcher: &acpbridge.Dispatcher{
			Registry: acpRegistry, ConvRepo: convRepo, Publisher: publisher, Log: log,
		},
		ACPRegistry:     acpRegistry,
		EnvironmentRepo: postgres.NewEnvironmentRepository(env.db),
		Log:             log,
	}

	trigger := agent.Trigger{
		ConversationID: convID,
		ProjectID:      env.projectID,
		AgentID:        agentID,
		Message:        "please look at this",
		TriggerType:    agent.TriggerChatMessage,
	}
	handleErrCh := make(chan error, 1)
	go func() { handleErrCh <- h.Handle(ctx, trigger) }()

	var startTurn map[string]any
	readCtx, readCancel := context.WithTimeout(ctx, 10*time.Second)
	err = wsjson.Read(readCtx, daemon, &startTurn)
	readCancel()
	if err != nil {
		t.Fatalf("read start_turn: %v", err)
	}
	if startTurn["type"] != "start_turn" {
		t.Fatalf("got message type %v, want start_turn: %+v", startTurn["type"], startTurn)
	}
	if startTurn["conversation_id"] != convID.String() {
		t.Fatalf("start_turn conversation_id = %v, want %s", startTurn["conversation_id"], convID)
	}

	if err := <-handleErrCh; err != nil {
		t.Fatalf("handle: %v", err)
	}

	status, err := conversationStatus(ctx, env.db, convID)
	if err != nil {
		t.Fatal(err)
	}
	if status != "running" {
		t.Fatalf("status after dispatch = %q, want %q", status, "running")
	}

	// --- Report a turn_status back, as the real daemon would ---------------

	if err := wsjson.Write(ctx, daemon, map[string]string{
		"type": "turn_status", "conversation_id": convID.String(), "status": "finished",
	}); err != nil {
		t.Fatalf("send turn_status: %v", err)
	}
	if err := waitForStatus(ctx, env.db, convID, "finished"); err != nil {
		t.Fatal(err)
	}

	// --- Report an event back too --------------------------------------------

	if err := wsjson.Write(ctx, daemon, map[string]any{
		"type": "event", "conversation_id": convID.String(),
		"event_type": "agent_message_chunk", "event_source": "agent",
		"payload": map[string]string{"text": "hello from the fake daemon"},
	}); err != nil {
		t.Fatalf("send event: %v", err)
	}
	if err := waitForEventCount(ctx, env.db, convID, 1); err != nil {
		t.Fatal(err)
	}

	// --- Internal status endpoint --------------------------------------------

	connected, err := getBridgeStatus(ctx, httpSrv.URL, agentID)
	if err != nil {
		t.Fatal(err)
	}
	if !connected {
		t.Fatalf("GET /agent-bridge/status/%s: connected = false, want true", agentID)
	}

	// --- Internal disconnect endpoint -----------------------------------------

	if err := postDisconnect(ctx, httpSrv.URL, agentID); err != nil {
		t.Fatal(err)
	}

	// The daemon's own read should now fail (server closed it) — confirms
	// the eviction control-channel path actually reaches a live connection,
	// not just the presence key.
	closedCtx, closedCancel := context.WithTimeout(ctx, 10*time.Second)
	var junk map[string]any
	err = wsjson.Read(closedCtx, daemon, &junk)
	closedCancel()
	if err == nil {
		t.Fatalf("expected the daemon connection to be closed after disconnect, but a read succeeded: %+v", junk)
	}

	_ = daemon.Close(websocket.StatusNormalClosure, "")

	if err := waitForBridgeOffline(ctx, httpSrv.URL, agentID); err != nil {
		t.Fatal(err)
	}
}

func seedACPAgentAndConversation(t *testing.T, env *e2eEnv) (agentID, convID uuid.UUID) {
	t.Helper()
	ctx := context.Background()

	agentID = uuid.New()
	convID = uuid.New()

	sum := sha256.Sum256([]byte(acpDispatchBridgeToken))
	tokenHash := hex.EncodeToString(sum[:])

	_, err := env.db.ExecContext(ctx, `
		INSERT INTO agents (id, project_id, name, handle, agent_type, acp_provider, acp_command,
		                     acp_bridge_token_hash, llm_provider, llm_model, llm_api_key_secret,
		                     system_prompt, max_iterations, timeout_minutes,
		                     git_committer_name, git_committer_email)
		VALUES ($1, $2, 'E2E ACP Agent', $3, 'acp', 'claude-code', '[]'::jsonb, $4,
		        'openai', 'unused', '', '', 5, 1, 'paca-agent', 'agent@example.com')
	`, agentID, env.projectID, "e2e-acp-agent-"+agentID.String()[:8], tokenHash)
	if err != nil {
		t.Fatalf("seed acp agent: %v", err)
	}

	_, err = env.db.ExecContext(ctx, `
		INSERT INTO agent_conversations (id, agent_id, project_id, trigger_type, triggered_by_member_id, status)
		VALUES ($1, $2, $3, 'chat_message', $4, 'queued')
	`, convID, agentID, env.projectID, env.memberID)
	if err != nil {
		t.Fatalf("seed acp conversation: %v", err)
	}

	return agentID, convID
}

func getBridgeStatus(ctx context.Context, baseURL string, agentID uuid.UUID) (bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/agent-bridge/status/"+agentID.String(), nil)
	if err != nil {
		return false, err
	}
	req.Header.Set("X-Internal-Token", acpDispatchInternalToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return false, fmt.Errorf("status endpoint returned %d: %s", resp.StatusCode, body)
	}
	var body struct {
		Connected bool `json:"connected"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return false, err
	}
	return body.Connected, nil
}

func waitForBridgeOffline(ctx context.Context, baseURL string, agentID uuid.UUID) error {
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		connected, err := getBridgeStatus(ctx, baseURL, agentID)
		if err == nil && !connected {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("bridge status for agent %s never went offline", agentID)
}

func postDisconnect(ctx context.Context, baseURL string, agentID uuid.UUID) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/agent-bridge/disconnect/"+agentID.String(), nil)
	if err != nil {
		return err
	}
	req.Header.Set("X-Internal-Token", acpDispatchInternalToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("disconnect endpoint returned %d: %s", resp.StatusCode, body)
	}
	return nil
}
