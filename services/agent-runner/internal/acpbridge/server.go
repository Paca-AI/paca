package acpbridge

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/google/uuid"

	"github.com/Paca-AI/agent-runner/internal/messaging"
	"github.com/Paca-AI/agent-runner/internal/repository/postgres"
	"github.com/Paca-AI/agent-runner/internal/sandbox"
)

// helloTimeout bounds how long an unauthenticated WebSocket connection can
// sit open waiting for its first frame — mirrors routes/bridge.py's
// _HELLO_TIMEOUT_SECONDS.
const helloTimeout = 10 * time.Second

// bridgeMessageReadLimit overrides coder/websocket's 32KiB default (see
// handleWS) — generous enough for a large tool-call payload (verbose
// command output, a sizeable file diff) without leaving this per-connection
// read buffer unbounded.
const bridgeMessageReadLimit = 10 * 1024 * 1024 // 10 MiB

const staleACPExecutorMessage = "ACP executor was lost while the bridge was restarting; retry the conversation."

// Server exposes the same HTTP surface routes/bridge.py does:
//   - GET  /agent-bridge/ws                    — the bridge daemon's WebSocket
//   - GET  /agent-bridge/status/{agentId}      — internal, proxied by services/api
//   - POST /agent-bridge/disconnect/{agentId}  — internal, ditto
//   - GET  /llm/models                         — proxied by services/api's
//     GetLLMModels; a static catalog, not a live LLM call
//
// Run in its own goroutine alongside messaging.Consumer.Run in
// cmd/agent-runner/main.go.
type Server struct {
	Registry      *Registry
	AgentRepo     *postgres.AgentRepository
	ConvRepo      *postgres.ConversationRepository
	Publisher     *messaging.Publisher
	InternalToken string
	// LLMModelsPath is the path to the static provider/model catalog file
	// (data/llm_models.json) served verbatim at GET /llm/models.
	LLMModelsPath string

	// SandboxMgr/EnvironmentRepo back the static-environment endpoints in
	// environment_handlers.go (server-to-server, internal-token-guarded)
	// and the browser terminal WebSocket in terminal.go (public, ticket-
	// guarded) — see docs/ai-agent/environment-management.md. Both nil in
	// tests/tooling that never exercise either surface, which is safe:
	// every handler that reads them checks for nil first and fails the
	// individual request, not server startup.
	SandboxMgr      sandbox.FullBackend
	EnvironmentRepo *postgres.EnvironmentRepository
	// SSHKeyRepo backs both handleCreateEnvironment's post-create sshd
	// bootstrap and the ssh-keys/sync endpoint (rendering
	// authorized_keys). SSHPortRangeStart/End gate the whole SSH feature —
	// both zero (the default) means handleCreateEnvironment never assigns
	// a port at all (docs/ai-agent/environment-management.md's "Terminal /
	// SSH Access" section) — the environment's SSH port, once assigned, is
	// published directly by the backend (a Docker -p binding or a
	// Kubernetes NodePort Service entry — see
	// sandbox.EnvironmentConfig.PortMappings), never relayed through this
	// process.
	//
	// PortForwardRepo is the same idea for user-managed port forwards
	// (docs/ai-agent/environment-management.md's "Port Forwarding"
	// section) — reads/writes the environment_port_forwards rows
	// handlePortForwardsAssign/handleRestartEnvironmentPorts assign host
	// ports to and read back to build a full PortMappings set.
	// PortForwardRangeStart/End is the same "both zero means never
	// configured" convention as SSH's own range.
	SSHKeyRepo            *postgres.SSHKeyRepository
	SSHPortRangeStart     int
	SSHPortRangeEnd       int
	PortForwardRepo       *postgres.PortForwardRepository
	PortForwardRangeStart int
	PortForwardRangeEnd   int
	// Backend is settings.SandboxBackend ("docker" or "kubernetes") —
	// echoed verbatim in POST /internal/environments' response. Not
	// derivable from a sandbox.EnvironmentHandle (which carries only
	// BackendRef/BaseURL, no backend-kind field), so this is threaded
	// through from config.Settings instead — see
	// environment_handlers.go's handleCreateEnvironment.
	Backend string
	// MCPDevSourceDir mirrors config.Settings.MCPDevSourceDir — forwarded
	// into every sandbox.EnvironmentConfig this creates so a brand-new
	// environment gets the dev-mode bind mount from its very first
	// container, the same as executor.Executor's own copy does for every
	// later StartEnvironment call. See sandbox.EnvironmentConfig.
	// MCPDevSourceDir's doc comment.
	MCPDevSourceDir string

	Log Logger
}

// Routes returns the HTTP handler for all of the bridge's endpoints.
func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /agent-bridge/ws", s.handleWS)
	mux.HandleFunc("GET /agent-bridge/status/{agentId}", s.requireInternalToken(s.handleStatus))
	mux.HandleFunc("POST /agent-bridge/disconnect/{agentId}", s.requireInternalToken(s.handleDisconnect))
	mux.HandleFunc("GET /llm/models", s.handleLLMModels)
	s.registerEnvironmentRoutes(mux)
	s.registerTerminalRoute(mux)
	s.registerEnvironmentStatsRoute(mux)
	return mux
}

// requireInternalToken rejects a request missing the correct
// X-Internal-Token header — mirrors routes/bridge.py's
// _require_internal_key dependency. Constant-time comparison, matching
// Python's secrets.compare_digest.
func (s *Server) requireInternalToken(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := r.Header.Get("X-Internal-Token")
		if subtle.ConstantTimeCompare([]byte(token), []byte(s.InternalToken)) != 1 {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

type helloFrame struct {
	Type                string   `json:"type"`
	AgentID             string   `json:"agent_id"`
	Token               string   `json:"token"`
	ActiveConversations []string `json:"active_conversations"`
}

// handleWS is the bridge daemon's WebSocket endpoint — mirrors
// routes/bridge.py's bridge_ws. The agent id and bridge token travel as a
// first WebSocket frame ("hello") sent right after the handshake
// completes, not as connection headers validated before accept — see
// routes/bridge.py's own doc comment on why (a wire-compatible,
// self-describing handshake that degrades gracefully across daemon/server
// version skew, which headers rejected pre-accept wouldn't).
func (s *Server) handleWS(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	// coder/websocket defaults to a 32KiB read limit — comfortably too
	// small for a bridge daemon relaying a raw ACP event verbatim (a large
	// command's output, or a sizeable file diff, both routinely exceed it),
	// which surfaced as the daemon getting disconnected mid-conversation
	// with "received close frame: status = StatusMessageTooBig". Bridge
	// events are the only traffic this endpoint ever reads at any real
	// size (hello/ping frames are tiny), so this is scoped to the whole
	// connection rather than per-message.
	conn.SetReadLimit(bridgeMessageReadLimit)

	helloCtx, cancel := context.WithTimeout(r.Context(), helloTimeout)
	var hello helloFrame
	err = wsjson.Read(helloCtx, conn, &hello)
	cancel()
	if err != nil || hello.Type != "hello" || hello.AgentID == "" || hello.Token == "" {
		_ = conn.Close(4400, "invalid or missing hello frame")
		return
	}

	agentID, err := uuid.Parse(hello.AgentID)
	if err != nil {
		_ = conn.Close(4400, "invalid agent_id")
		return
	}

	tokenHash := hashToken(hello.Token)
	cfg, err := s.AgentRepo.FindByBridgeTokenHash(r.Context(), tokenHash)
	if err != nil {
		s.Log.Warn("acpbridge: bridge token lookup failed", "error", err)
		_ = conn.Close(4401, "unauthorized")
		return
	}
	if cfg == nil || cfg.ID != agentID {
		_ = conn.Close(4401, "unauthorized")
		return
	}

	// Capture the reconciliation cutoff from PostgreSQL before the bridge is
	// registered as online. agent_conversations.updated_at is also generated
	// by PostgreSQL, so using the database clock avoids cross-host clock skew;
	// taking the cutoff before Register ensures any newly dispatched turn that
	// starts after this bridge becomes authoritative sorts after the cutoff.
	var reconcileCutoff time.Time
	if hello.ActiveConversations != nil {
		reconcileCutoff, err = s.ConvRepo.CurrentDatabaseTime(r.Context())
		if err != nil {
			s.Log.Error("acpbridge: failed to capture reconciliation cutoff", "agent_id", agentID, "error", err)
			_ = conn.Close(websocket.StatusInternalError, "reconciliation setup failed")
			return
		}
	}

	sessionID, err := s.Registry.Register(r.Context(), agentID, cfg.ProjectID, wsConn{conn})
	if err != nil {
		s.Log.Error("acpbridge: failed to register bridge connection", "agent_id", agentID, "error", err)
		_ = conn.Close(websocket.StatusInternalError, "registration failed")
		return
	}
	s.Log.Info("acpbridge: bridge connected", "agent_id", agentID)

	if err := wsjson.Write(r.Context(), conn, map[string]string{"type": "hello_ack"}); err != nil {
		s.Registry.Unregister(context.WithoutCancel(r.Context()), agentID, cfg.ProjectID, sessionID)
		return
	}
	// Reconciliation relies on Registry's one-bridge-session-per-agent
	// contract: once Register succeeds, this connection is authoritative for
	// the agent's process-owned ACP sessions. Supporting concurrent bridge
	// daemons would require per-run executor ownership rather than this
	// agent-wide snapshot.
	//
	// A nil slice means an older bridge that does not know the reconciliation
	// field; an explicit [] means a current bridge with no surviving ACP
	// sessions, which is exactly the restart/orphan signal we need.
	if hello.ActiveConversations != nil {
		s.reconcileStaleConversations(context.WithoutCancel(r.Context()), agentID, hello.ActiveConversations, reconcileCutoff)
	}

	s.relayMessages(r.Context(), conn, agentID)

	s.Registry.Unregister(context.WithoutCancel(r.Context()), agentID, cfg.ProjectID, sessionID)
	s.Log.Info("acpbridge: bridge disconnected", "agent_id", agentID)
}

// relayMessages reads event/turn_status/ping messages from the daemon until
// the connection closes — mirrors bridge_ws's `while True: ... receive_json()`
// loop.
func (s *Server) relayMessages(ctx context.Context, conn *websocket.Conn, agentID uuid.UUID) {
	for {
		var msg map[string]any
		if err := wsjson.Read(ctx, conn, &msg); err != nil {
			var closeErr websocket.CloseError
			if !errors.As(err, &closeErr) {
				s.Log.Warn("acpbridge: bridge connection read failed", "agent_id", agentID, "error", err)
			}
			return
		}
		switch msgType, _ := msg["type"].(string); msgType {
		case "event":
			s.handleEventMessage(ctx, agentID, msg)
		case "turn_status":
			s.handleTurnStatusMessage(ctx, agentID, msg)
		case "ping":
			if err := s.Registry.Heartbeat(ctx, agentID); err != nil {
				s.Log.Warn("acpbridge: failed to refresh heartbeat", "agent_id", agentID, "error", err)
			}
			if err := wsjson.Write(ctx, conn, map[string]string{"type": "pong"}); err != nil {
				return
			}
		default:
			s.Log.Warn("acpbridge: unknown bridge message type", "agent_id", agentID, "type", msgType)
		}
	}
}

func (s *Server) handleEventMessage(ctx context.Context, agentID uuid.UUID, msg map[string]any) {
	convIDStr, _ := msg["conversation_id"].(string)
	if convIDStr == "" {
		return
	}
	convID, err := uuid.Parse(convIDStr)
	if err != nil {
		return
	}

	ownerID, _, err := s.ConvRepo.GetConversationAgentType(ctx, convID)
	if err != nil || ownerID != agentID {
		s.Log.Warn("acpbridge: dropping event for a conversation not owned by this agent",
			"conversation_id", convID, "agent_id", agentID)
		return
	}

	projectID, ownerUserID, err := s.ConvRepo.GetConversationRealtimeContext(ctx, convID)
	if err != nil {
		s.Log.Warn("acpbridge: failed to resolve realtime context", "conversation_id", convID, "error", err)
		return
	}
	eventIndex, err := s.ConvRepo.NextEventIndex(ctx, convID)
	if err != nil {
		s.Log.Warn("acpbridge: failed to allocate event index", "conversation_id", convID, "error", err)
		return
	}

	eventType, _ := msg["event_type"].(string)
	if eventType == "" {
		eventType = "ACPEvent"
	}
	eventSource, _ := msg["event_source"].(string)
	if eventSource == "" {
		eventSource = "agent"
	}
	// paca_acp_bridge.runner._event_payload serializes the ACP event once
	// with Pydantic and sends it as a JSON *string* (byte-for-byte, so it
	// never diverges from the SDK's own encoding — see commit 59fea550's
	// message on apps/acp-bridge). Re-marshaling that string here would
	// wrap it in another layer of quotes, turning the stored/broadcast
	// payload into a JSON string instead of the object it actually is —
	// which is exactly what made GET .../events 500 with "cannot unmarshal
	// string into ... map[string]interface{}" for every event a bridged
	// (acp-type) conversation persisted. Use the string's bytes directly;
	// only non-string payloads (or a missing one) need marshaling.
	var payload []byte
	if s, ok := msg["payload"].(string); ok {
		payload = []byte(s)
	} else {
		var err error
		payload, err = json.Marshal(msg["payload"])
		if err != nil {
			payload = []byte("{}")
		}
	}

	id := uuid.New()
	createdAt := time.Now().UTC()
	if err := s.ConvRepo.InsertEvent(ctx, id, convID, eventType, eventSource, eventIndex, payload, createdAt); err != nil {
		s.Log.Warn("acpbridge: failed to persist bridge event", "conversation_id", convID, "error", err)
	}
	if err := s.Publisher.PublishEvent(ctx, convID, projectID, eventType, eventSource, eventIndex, payload, "running"); err != nil {
		s.Log.Warn("acpbridge: failed to publish bridge event", "conversation_id", convID, "error", err)
	}
	if eventType == "turn_usage" {
		// Durably persisted/streamed above (so services/api's
		// conversationCols can sum/read it the same way it already does for
		// llm-type agents' turn_usage rows), but never broadcast to the live
		// chat transcript — mirrors internal/handler.Handler's own
		// turn_usage persistence, which likewise skips PublishRealtime: a
		// usage snapshot has no place there.
		return
	}
	// Full row, not just event_index — see
	// internal/handler.Handler.persistAndPublish's doc comment on why: the
	// frontend needs enough here to append the event directly instead of
	// re-fetching from GET .../events on every message.
	if err := s.Publisher.PublishRealtime(ctx, projectID, convID,
		fmt.Sprintf("agent.%s", eventType), map[string]any{
			"id":           id.String(),
			"event_index":  eventIndex,
			"event_type":   eventType,
			"event_source": eventSource,
			"payload":      json.RawMessage(payload),
			"created_at":   createdAt.Format(time.RFC3339Nano),
		}, ownerUserID); err != nil {
		s.Log.Warn("acpbridge: failed to publish realtime bridge event", "conversation_id", convID, "error", err)
	}
}

func (s *Server) handleTurnStatusMessage(ctx context.Context, agentID uuid.UUID, msg map[string]any) {
	convIDStr, _ := msg["conversation_id"].(string)
	statusStr, hasStatus := msg["status"].(string)
	if convIDStr == "" || !hasStatus {
		return
	}
	convID, err := uuid.Parse(convIDStr)
	if err != nil {
		return
	}

	ownerID, _, err := s.ConvRepo.GetConversationAgentType(ctx, convID)
	if err != nil || ownerID != agentID {
		s.Log.Warn("acpbridge: dropping turn_status for a conversation not owned by this agent",
			"conversation_id", convID, "agent_id", agentID)
		return
	}

	var errMsg *string
	if m, ok := msg["error_message"].(string); ok && m != "" {
		errMsg = &m
	}
	updated, err := s.ConvRepo.UpdateStatusIfNotTerminal(ctx, convID, statusStr, errMsg)
	if err != nil {
		s.Log.Warn("acpbridge: failed to record turn_status", "conversation_id", convID, "error", err)
		return
	}
	if !updated {
		s.Log.Info("acpbridge: ignoring late turn_status for terminal conversation", "conversation_id", convID, "status", statusStr)
		return
	}

	projectID, ownerUserID, err := s.ConvRepo.GetConversationRealtimeContext(ctx, convID)
	if err != nil {
		s.Log.Warn("acpbridge: failed to resolve realtime context", "conversation_id", convID, "error", err)
		return
	}
	if err := s.Publisher.PublishRealtime(ctx, projectID, convID,
		fmt.Sprintf("agent.conversation.%s", statusStr), nil, ownerUserID); err != nil {
		s.Log.Warn("acpbridge: failed to publish turn_status realtime event", "conversation_id", convID, "error", err)
	}
}

func (s *Server) reconcileStaleConversations(ctx context.Context, agentID uuid.UUID, activeStrings []string, connectedAt time.Time) {
	active := make([]uuid.UUID, 0, len(activeStrings))
	for _, raw := range activeStrings {
		id, err := uuid.Parse(raw)
		if err != nil {
			s.Log.Warn("acpbridge: ignoring invalid active conversation id", "agent_id", agentID, "conversation_id", raw, "error", err)
			continue
		}
		active = append(active, id)
	}

	stale, err := s.ConvRepo.ReconcileStaleACPConversations(ctx, agentID, active, connectedAt, staleACPExecutorMessage)
	if err != nil {
		s.Log.Warn("acpbridge: failed to reconcile stale ACP conversations", "agent_id", agentID, "error", err)
		return
	}
	for _, conversation := range stale {
		s.Log.Warn("acpbridge: marked orphaned ACP conversation failed", "agent_id", agentID, "conversation_id", conversation.ID)
		projectID, ownerUserID, contextErr := s.ConvRepo.GetConversationRealtimeContext(ctx, conversation.ID)
		if contextErr != nil {
			s.Log.Warn("acpbridge: failed to resolve stale conversation realtime context", "conversation_id", conversation.ID, "error", contextErr)
		} else if err := s.Publisher.PublishRealtime(ctx, projectID, conversation.ID,
			"agent.conversation.failed", nil, ownerUserID); err != nil {
			s.Log.Warn("acpbridge: failed to publish stale conversation realtime status", "conversation_id", conversation.ID, "error", err)
		}
		if err := s.Publisher.PublishConversationStatus(ctx, conversation.ID, "failed"); err != nil {
			s.Log.Warn("acpbridge: failed to publish stale conversation durable status", "conversation_id", conversation.ID, "error", err)
		}
	}
}

// handleStatus handles GET /agent-bridge/status/{agentId} — internal,
// proxied by services/api's GetACPBridgeStatus/GetGlobalACPBridgeStatus.
func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	agentID, err := uuid.Parse(r.PathValue("agentId"))
	if err != nil {
		http.Error(w, "invalid agentId", http.StatusBadRequest)
		return
	}
	online, err := s.Registry.IsOnline(r.Context(), agentID)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]bool{"connected": online})
}

// handleDisconnect handles POST /agent-bridge/disconnect/{agentId} —
// internal, called by services/api right after a bridge token is
// regenerated (see agent_handler.go's GenerateACPBridgeToken).
func (s *Server) handleDisconnect(w http.ResponseWriter, r *http.Request) {
	agentID, err := uuid.Parse(r.PathValue("agentId"))
	if err != nil {
		http.Error(w, "invalid agentId", http.StatusBadRequest)
		return
	}
	if err := s.Registry.Evict(r.Context(), agentID); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]bool{"ok": true})
}

// handleLLMModels serves the static provider/model catalog from
// data/llm_models.json — used by the frontend's provider dropdown, proxied
// through services/api's GetLLMModels.
func (s *Server) handleLLMModels(w http.ResponseWriter, r *http.Request) {
	f, err := os.Open(s.LLMModelsPath)
	if err != nil {
		http.Error(w, "model catalog not available", http.StatusInternalServerError)
		return
	}
	defer func() { _ = f.Close() }()
	w.Header().Set("Content-Type", "application/json")
	_, _ = io.Copy(w, f)
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
