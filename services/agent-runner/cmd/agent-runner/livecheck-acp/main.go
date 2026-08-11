// livecheck-acp exercises the ACP bridge end to end against real Docker-free
// infrastructure (real Postgres, real Valkey, a real HTTP+WebSocket
// server): a fake bridge daemon connects, authenticates with a bridge
// token, and receives a real "start_turn" dispatched through the actual
// production path (handler.Handler.Handle -> dispatchACP ->
// acpbridge.Dispatcher.DispatchTrigger), reports a turn_status back, and the
// internal status/disconnect REST endpoints are exercised against the same
// running server.
//
//	go run ./cmd/agent-runner/livecheck-acp <postgres-dsn> <redis-addr>
package main

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
	"strings"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/redis/go-redis/v9"

	"github.com/Paca-AI/agent-runner/internal/acpbridge"
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

const internalToken = "livecheck-acp-internal-token"
const bridgeToken = "livecheck-acp-bridge-token"

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "FAIL %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	if len(os.Args) != 3 {
		return fmt.Errorf("usage: livecheck-acp <postgres-dsn> <redis-addr>")
	}
	dsn, redisAddr := os.Args[1], os.Args[2]
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))

	db, err := postgres.Open(dsn)
	if err != nil {
		return fmt.Errorf("open: %w", err)
	}
	defer func() { _ = db.Close() }()

	redisClient := redis.NewClient(&redis.Options{Addr: redisAddr})
	defer func() { _ = redisClient.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	agentID, convID, projectID, err := seedThrowawayRows(ctx, db)
	if err != nil {
		return fmt.Errorf("seed: %w", err)
	}
	fmt.Printf("OK   seeded throwaway acp agent=%s conversation=%s project=%s\n", agentID, convID, projectID)
	defer func() {
		if _, err := db.ExecContext(context.Background(), `DELETE FROM agents WHERE id = $1`, agentID); err != nil {
			fmt.Fprintf(os.Stderr, "WARN cleanup failed, delete manually: DELETE FROM agents WHERE id = '%s'; -- %v\n", agentID, err)
		} else {
			fmt.Println("OK   cleaned up throwaway rows")
		}
	}()

	publisher := messaging.NewPublisher(redisClient)
	agentRepo := postgres.NewAgentRepository(db)
	convRepo := postgres.NewConversationRepository(db)
	acpRegistry := acpbridge.New(redisClient, publisher, log)

	acpServer := &acpbridge.Server{
		Registry:      acpRegistry,
		AgentRepo:     agentRepo,
		ConvRepo:      convRepo,
		Publisher:     publisher,
		InternalToken: internalToken,
		LLMModelsPath: "../../data/llm_models.json",
		Log:           log,
	}
	httpSrv := httptest.NewServer(acpServer.Routes())
	defer httpSrv.Close()
	fmt.Printf("OK   started acp bridge http server at %s\n", httpSrv.URL)

	// --- Connect a fake bridge daemon --------------------------------------

	wsURL := "ws" + strings.TrimPrefix(httpSrv.URL, "http") + "/agent-bridge/ws"
	daemon, dialResp, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		return fmt.Errorf("dial bridge ws: %w", err)
	}
	if dialResp != nil && dialResp.Body != nil {
		_ = dialResp.Body.Close()
	}
	defer func() { _ = daemon.CloseNow() }()

	if err := wsjson.Write(ctx, daemon, map[string]string{
		"type": "hello", "agent_id": agentID.String(), "token": bridgeToken,
	}); err != nil {
		return fmt.Errorf("send hello: %w", err)
	}
	var ack map[string]string
	if err := wsjson.Read(ctx, daemon, &ack); err != nil {
		return fmt.Errorf("read hello_ack: %w", err)
	}
	if ack["type"] != "hello_ack" {
		return fmt.Errorf("got %v, want hello_ack", ack)
	}
	fmt.Println("OK   fake bridge daemon connected and authenticated")

	// --- Dispatch a trigger through the real production path ---------------

	sandboxMgr, err := sandbox.NewManager(19500, 10)
	if err != nil {
		return fmt.Errorf("NewManager: %w", err)
	}
	key, err := secret.DecodeHexKey(strings.Repeat("ac", 32))
	if err != nil {
		return err
	}
	encryptor, err := secret.NewEncryptor(key)
	if err != nil {
		return err
	}
	h := &handler.Handler{
		Gate:          config.NewGate([]string{agentID.String()}),
		AgentRepo:     agentRepo,
		ConvRepo:      convRepo,
		Publisher:     publisher,
		Executor:      executor.New(sandboxMgr, encryptor, executor.Options{}, log),
		InFlight:      registry.New(),
		ChatSandboxes: chatsandbox.New(),
		ACPDispatcher: &acpbridge.Dispatcher{
			Registry: acpRegistry, ConvRepo: convRepo, Publisher: publisher, Log: log,
		},
		ACPRegistry: acpRegistry,
		Log:         log,
	}

	trigger := agent.Trigger{
		ConversationID: convID,
		ProjectID:      projectID,
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
		return fmt.Errorf("read start_turn: %w", err)
	}
	if startTurn["type"] != "start_turn" {
		return fmt.Errorf("got message type %v, want start_turn: %+v", startTurn["type"], startTurn)
	}
	if startTurn["conversation_id"] != convID.String() {
		return fmt.Errorf("start_turn conversation_id = %v, want %s", startTurn["conversation_id"], convID)
	}
	fmt.Printf("OK   received start_turn: %+v\n", startTurn)

	if err := <-handleErrCh; err != nil {
		return fmt.Errorf("handle: %w", err)
	}
	fmt.Println("OK   Handle (dispatchACP) returned without error")

	status, err := conversationStatus(ctx, db, convID)
	if err != nil {
		return err
	}
	if status != "running" {
		return fmt.Errorf("status after dispatch = %q, want %q", status, "running")
	}
	fmt.Println("OK   conversation status is \"running\" after dispatch")

	// --- Report a turn_status back, as the real daemon would ---------------

	if err := wsjson.Write(ctx, daemon, map[string]string{
		"type": "turn_status", "conversation_id": convID.String(), "status": "finished",
	}); err != nil {
		return fmt.Errorf("send turn_status: %w", err)
	}
	if err := waitForStatus(ctx, db, convID, "finished"); err != nil {
		return err
	}
	fmt.Println("OK   turn_status from the daemon updated conversation status to \"finished\"")

	// --- Report an event back too -------------------------------------------

	if err := wsjson.Write(ctx, daemon, map[string]any{
		"type": "event", "conversation_id": convID.String(),
		"event_type": "agent_message_chunk", "event_source": "agent",
		"payload": map[string]string{"text": "hello from the fake daemon"},
	}); err != nil {
		return fmt.Errorf("send event: %w", err)
	}
	if err := waitForEventCount(ctx, db, convID, 1); err != nil {
		return err
	}
	fmt.Println("OK   event from the daemon was persisted to agent_conversation_events")

	// --- Internal status endpoint --------------------------------------------

	connected, err := getBridgeStatus(ctx, httpSrv.URL, agentID)
	if err != nil {
		return err
	}
	if !connected {
		return fmt.Errorf("GET /agent-bridge/status/%s: connected = false, want true", agentID)
	}
	fmt.Println("OK   GET /agent-bridge/status reports connected=true while the daemon is up")

	// --- Internal disconnect endpoint ----------------------------------------

	if err := postDisconnect(ctx, httpSrv.URL, agentID); err != nil {
		return err
	}
	fmt.Println("OK   POST /agent-bridge/disconnect accepted")

	// The daemon's own read should now fail (server closed it) — confirms
	// the eviction control-channel path actually reaches a live connection,
	// not just the presence key.
	closedCtx, closedCancel := context.WithTimeout(ctx, 10*time.Second)
	var junk map[string]any
	err = wsjson.Read(closedCtx, daemon, &junk)
	closedCancel()
	if err == nil {
		return fmt.Errorf("expected the daemon connection to be closed after disconnect, but a read succeeded: %+v", junk)
	}
	fmt.Println("OK   daemon connection was actually closed by the disconnect call")

	// A real WebSocket client library completes the close handshake by
	// sending its own close frame back — good hygiene for this fake daemon
	// to match, though it turned out not to be the actual cause of a real
	// bug found while writing this check: server-side Unregister used to
	// hang indefinitely regardless of this, because of a go-redis PubSub
	// issue now fixed in acpbridge.drainMessages (see its doc comment) —
	// investigated live: without that fix, waitForBridgeOffline below never observed
	// connected=false even after 15s.
	_ = daemon.Close(websocket.StatusNormalClosure, "")

	if err := waitForBridgeOffline(ctx, httpSrv.URL, agentID); err != nil {
		return err
	}
	fmt.Println("OK   GET /agent-bridge/status reports connected=false after disconnect")

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

func waitForStatus(ctx context.Context, db *sqlx.DB, convID uuid.UUID, want string) error {
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		got, err := conversationStatus(ctx, db, convID)
		if err == nil && got == want {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	got, _ := conversationStatus(ctx, db, convID)
	return fmt.Errorf("status never reached %q, last seen %q", want, got)
}

func waitForEventCount(ctx context.Context, db *sqlx.DB, convID uuid.UUID, want int) error {
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		var count int
		if err := db.GetContext(ctx, &count,
			`SELECT COUNT(*) FROM agent_conversation_events WHERE conversation_id = $1`, convID); err == nil && count >= want {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("event count for conversation %s never reached %d", convID, want)
}

func getBridgeStatus(ctx context.Context, baseURL string, agentID uuid.UUID) (bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/agent-bridge/status/"+agentID.String(), nil)
	if err != nil {
		return false, err
	}
	req.Header.Set("X-Internal-Token", internalToken)
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
	req.Header.Set("X-Internal-Token", internalToken)
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

func seedThrowawayRows(ctx context.Context, db *sqlx.DB) (agentID, convID, projectID uuid.UUID, err error) {
	agentID = uuid.New()
	convID = uuid.New()

	if err := db.GetContext(ctx, &projectID, `SELECT id FROM projects LIMIT 1`); err != nil {
		return uuid.Nil, uuid.Nil, uuid.Nil, fmt.Errorf("find a project to seed against: %w", err)
	}

	sum := sha256.Sum256([]byte(bridgeToken))
	tokenHash := hex.EncodeToString(sum[:])

	_, err = db.ExecContext(ctx, `
		INSERT INTO agents (id, project_id, name, handle, agent_type, acp_provider, acp_command,
		                     acp_bridge_token_hash, llm_provider, llm_model, llm_api_key_secret,
		                     system_prompt, max_iterations, timeout_minutes,
		                     git_committer_name, git_committer_email)
		VALUES ($1, $2, 'Livecheck ACP Agent', $3, 'acp', 'claude-code', '[]'::jsonb, $4,
		        'openai', 'unused', '', '', 5, 1, 'paca-agent', 'agent@example.com')
	`, agentID, projectID, "livecheck-acp-agent-"+agentID.String()[:8], tokenHash)
	if err != nil {
		return uuid.Nil, uuid.Nil, uuid.Nil, fmt.Errorf("insert throwaway acp agent: %w", err)
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
