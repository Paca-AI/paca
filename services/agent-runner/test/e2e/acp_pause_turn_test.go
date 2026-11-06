package e2e_test

import (
	"context"
	"log/slog"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"

	"github.com/Paca-AI/agent-runner/internal/acpbridge"
	"github.com/Paca-AI/agent-runner/internal/agent"
	"github.com/Paca-AI/agent-runner/internal/messaging"
	"github.com/Paca-AI/agent-runner/internal/repository/postgres"
)

// TestACPPauseTurn covers the composer's stop button for an ACP agent (the
// pause_turn message, kept by that name on the wire): the control channel
// delivers pause_turn to the bridge, and the "finished" status the bridge
// reports back moves the conversation out of "running". The watchdog checks
// guard that this terminal status is never overwritten by its fail path.
func TestACPPauseTurn(t *testing.T) {
	env := newE2EEnv(t)
	ctx := context.Background()
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))

	agentID, convID := seedACPAgentAndConversation(t, env)

	publisher := messaging.NewPublisher(env.redisClient)
	convRepo := postgres.NewConversationRepository(env.db)
	acpRegistry := acpbridge.New(env.redisClient, publisher, log)

	_, thisFile, _, _ := runtime.Caller(0)
	acpServer := &acpbridge.Server{
		Registry:      acpRegistry,
		AgentRepo:     postgres.NewAgentRepository(env.db),
		ConvRepo:      convRepo,
		Publisher:     publisher,
		InternalToken: acpDispatchInternalToken,
		LLMModelsPath: filepath.Join(filepath.Dir(thisFile), "..", "..", "data", "llm_models.json"),
		Log:           log,
	}
	httpSrv := httptest.NewServer(acpServer.Routes())
	t.Cleanup(httpSrv.Close)

	// Connect a fake bridge daemon.
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
	if err := wsjson.Read(ctx, daemon, &ack); err != nil || ack["type"] != "hello_ack" {
		t.Fatalf("hello_ack: %v (err %v)", ack, err)
	}

	// Dispatch a turn; the conversation is "running".
	dispatcher := &acpbridge.Dispatcher{
		Registry: acpRegistry, ConvRepo: convRepo, Publisher: publisher, Log: log,
	}
	trigger := agent.Trigger{
		ConversationID: convID,
		ProjectID:      env.projectID,
		AgentID:        agentID,
		Message:        "work on this",
		TriggerType:    agent.TriggerChatMessage,
	}
	if err := dispatcher.DispatchTrigger(ctx, trigger, &agent.ACPConfig{
		ID: agentID, ACPProvider: "claude-code", TimeoutMinutes: 1,
	}); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	readMsg := func() map[string]any {
		t.Helper()
		readCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		var msg map[string]any
		if err := wsjson.Read(readCtx, daemon, &msg); err != nil {
			t.Fatalf("read from bridge: %v", err)
		}
		return msg
	}
	if msg := readMsg(); msg["type"] != "start_turn" {
		t.Fatalf("got %v, want start_turn", msg)
	}
	if err := waitForStatus(ctx, env.db, convID, "running"); err != nil {
		t.Fatal(err)
	}

	// The pause control reaches the bridge as pause_turn for this conversation.
	dispatched, err := acpRegistry.Dispatch(ctx, agentID, map[string]any{
		"type": "pause_turn", "conversation_id": convID.String(),
	})
	if err != nil || !dispatched {
		t.Fatalf("dispatch pause_turn: dispatched=%v err=%v", dispatched, err)
	}
	msg := readMsg()
	if msg["type"] != "pause_turn" || msg["conversation_id"] != convID.String() {
		t.Fatalf("got %v, want pause_turn for %s", msg, convID)
	}

	// The bridge reports "finished" once the turn unwinds: the row leaves "running".
	if err := wsjson.Write(ctx, daemon, map[string]string{
		"type": "turn_status", "conversation_id": convID.String(), "status": "finished",
	}); err != nil {
		t.Fatalf("send turn_status: %v", err)
	}
	if err := waitForStatus(ctx, env.db, convID, "finished"); err != nil {
		t.Fatal(err)
	}

	// The dispatch watchdog must leave a finished conversation alone...
	failed, err := convRepo.FailIfNotTerminal(ctx, convID, "ACP turn timed out")
	if err != nil {
		t.Fatalf("FailIfNotTerminal: %v", err)
	}
	if failed {
		t.Fatal("watchdog failed a finished conversation")
	}
	if status, _ := conversationStatus(ctx, env.db, convID); status != "finished" {
		t.Fatalf("status after watchdog = %q, want finished", status)
	}

	// ...but still fails a turn that is genuinely stuck in "running".
	if err := convRepo.UpdateStatus(ctx, convID, "running", nil); err != nil {
		t.Fatal(err)
	}
	failed, err = convRepo.FailIfNotTerminal(ctx, convID, "ACP turn timed out")
	if err != nil || !failed {
		t.Fatalf("watchdog on running conversation: failed=%v err=%v, want true", failed, err)
	}
}
