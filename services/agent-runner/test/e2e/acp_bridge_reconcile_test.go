package e2e_test

import (
	"context"
	"log/slog"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/google/uuid"

	"github.com/Paca-AI/agent-runner/internal/acpbridge"
	"github.com/Paca-AI/agent-runner/internal/messaging"
	"github.com/Paca-AI/agent-runner/internal/repository/postgres"
)

func TestACPBridgeReconnectPreservesReportedActiveConversation(t *testing.T) {
	env := newE2EEnv(t)
	ctx := context.Background()

	agentID, convID := seedACPAgentAndConversation(t, env)
	if _, err := env.db.ExecContext(ctx,
		`UPDATE agent_conversations SET status = 'running' WHERE id = $1`, convID); err != nil {
		t.Fatalf("mark conversation running: %v", err)
	}

	httpSrv := newACPBridgeReconcileServer(t, env)
	bridgeConn := connectACPBridgeForReconcile(t, ctx, httpSrv.URL, agentID, []string{convID.String()})

	// A pong proves the server has completed the synchronous reconnect
	// reconciliation and entered its relay loop before we inspect durable state.
	pingACPBridge(t, ctx, bridgeConn)

	status, err := conversationStatus(ctx, env.db, convID)
	if err != nil {
		t.Fatal(err)
	}
	if status != "running" {
		t.Fatalf("reported active conversation status = %q, want %q", status, "running")
	}
}

func TestACPBridgeLateTurnStatusDoesNotResurrectStoppedConversation(t *testing.T) {
	env := newE2EEnv(t)
	ctx := context.Background()

	agentID, convID := seedACPAgentAndConversation(t, env)
	if _, err := env.db.ExecContext(ctx,
		`UPDATE agent_conversations SET status = 'stopped' WHERE id = $1`, convID); err != nil {
		t.Fatalf("mark conversation stopped: %v", err)
	}

	httpSrv := newACPBridgeReconcileServer(t, env)
	bridgeConn := connectACPBridgeForReconcile(t, ctx, httpSrv.URL, agentID, []string{})

	if err := wsjson.Write(ctx, bridgeConn, map[string]string{
		"type": "turn_status", "conversation_id": convID.String(), "status": "running",
	}); err != nil {
		t.Fatalf("send late turn_status: %v", err)
	}

	// Messages are processed serially. Receiving pong after the turn_status
	// guarantees the late frame has been handled before checking the row.
	pingACPBridge(t, ctx, bridgeConn)

	status, err := conversationStatus(ctx, env.db, convID)
	if err != nil {
		t.Fatal(err)
	}
	if status != "stopped" {
		t.Fatalf("status after late running turn_status = %q, want %q", status, "stopped")
	}
}

func newACPBridgeReconcileServer(t *testing.T, env *e2eEnv) *httptest.Server {
	t.Helper()
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	publisher := messaging.NewPublisher(env.redisClient)
	server := &acpbridge.Server{
		Registry:      acpbridge.New(env.redisClient, publisher, log),
		AgentRepo:     postgres.NewAgentRepository(env.db),
		ConvRepo:      postgres.NewConversationRepository(env.db),
		Publisher:     publisher,
		InternalToken: acpDispatchInternalToken,
		Log:           log,
	}
	httpSrv := httptest.NewServer(server.Routes())
	t.Cleanup(httpSrv.Close)
	return httpSrv
}

func connectACPBridgeForReconcile(
	t *testing.T,
	ctx context.Context,
	baseURL string,
	agentID uuid.UUID,
	activeConversations []string,
) *websocket.Conn {
	t.Helper()

	wsURL := "ws" + strings.TrimPrefix(baseURL, "http") + "/agent-bridge/ws"
	conn, dialResp, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("dial bridge ws: %v", err)
	}
	if dialResp != nil && dialResp.Body != nil {
		_ = dialResp.Body.Close()
	}
	t.Cleanup(func() { _ = conn.CloseNow() })

	if err := wsjson.Write(ctx, conn, map[string]any{
		"type":                 "hello",
		"agent_id":             agentID.String(),
		"token":                acpDispatchBridgeToken,
		"active_conversations": activeConversations,
	}); err != nil {
		t.Fatalf("send hello: %v", err)
	}

	var ack map[string]string
	if err := wsjson.Read(ctx, conn, &ack); err != nil {
		t.Fatalf("read hello_ack: %v", err)
	}
	if ack["type"] != "hello_ack" {
		t.Fatalf("got %v, want hello_ack", ack)
	}
	return conn
}

func pingACPBridge(t *testing.T, ctx context.Context, conn *websocket.Conn) {
	t.Helper()
	if err := wsjson.Write(ctx, conn, map[string]string{"type": "ping"}); err != nil {
		t.Fatalf("send ping: %v", err)
	}
	var pong map[string]string
	if err := wsjson.Read(ctx, conn, &pong); err != nil {
		t.Fatalf("read pong: %v", err)
	}
	if pong["type"] != "pong" {
		t.Fatalf("got %v, want pong", pong)
	}
}
