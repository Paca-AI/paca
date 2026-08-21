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
	"strings"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/google/uuid"

	"github.com/Paca-AI/agent-runner/internal/messaging"
	"github.com/Paca-AI/agent-runner/internal/repository/postgres"
	"github.com/Paca-AI/agent-runner/internal/turnruntime"
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
	TurnRuntime   authoritativeTurnRuntime
	InternalToken string
	// LLMModelsPath is the path to the static provider/model catalog file
	// (data/llm_models.json) served verbatim at GET /llm/models.
	LLMModelsPath string
	Log           Logger
}

type authoritativeTurnRuntime interface {
	AppendEvent(context.Context, uuid.UUID, turnruntime.Event) error
	Finalize(context.Context, uuid.UUID, turnruntime.FinalizeInput) error
}

// Routes returns the HTTP handler for all of the bridge's endpoints.
func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /agent-bridge/ws", s.handleWS)
	mux.HandleFunc("GET /agent-bridge/status/{agentId}", s.requireInternalToken(s.handleStatus))
	mux.HandleFunc("POST /agent-bridge/disconnect/{agentId}", s.requireInternalToken(s.handleDisconnect))
	mux.HandleFunc("GET /llm/models", s.handleLLMModels)
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
	Type    string `json:"type"`
	AgentID string `json:"agent_id"`
	Token   string `json:"token"`
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

	s.relayMessages(r.Context(), conn, agentID, sessionID)

	s.Registry.Unregister(context.WithoutCancel(r.Context()), agentID, cfg.ProjectID, sessionID)
	s.Log.Info("acpbridge: bridge disconnected", "agent_id", agentID)
}

// relayMessages reads event/turn_status/ping messages from the daemon until
// the connection closes — mirrors bridge_ws's `while True: ... receive_json()`
// loop.
func (s *Server) relayMessages(ctx context.Context, conn *websocket.Conn, agentID uuid.UUID, sessionID string) {
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
			if _, authoritative := msg["turn_id"].(string); authoritative {
				s.ackAuthoritativeDelivery(ctx, conn, msg, s.handleAuthoritativeEvent(ctx, agentID, msg))
			} else {
				s.handleEventMessage(ctx, agentID, msg)
			}
		case "turn_status":
			if _, authoritative := msg["turn_id"].(string); authoritative {
				s.ackAuthoritativeDelivery(ctx, conn, msg, s.handleAuthoritativeStatus(ctx, agentID, msg))
			} else {
				s.handleTurnStatusMessage(ctx, agentID, msg)
			}
		case "dispatch_ack":
			deliveryID, _ := msg["ack_delivery_id"].(string)
			if err := s.Registry.AcknowledgeDispatch(ctx, agentID, sessionID, deliveryID); err != nil {
				s.Log.Warn("acpbridge: rejected start acknowledgement", "agent_id", agentID, "error", err)
			}
		case "ping":
			if err := s.Registry.Heartbeat(ctx, agentID, sessionID); err != nil {
				s.Log.Warn("acpbridge: failed to refresh heartbeat", "agent_id", agentID, "error", err)
				return
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
	if err := s.ConvRepo.UpdateStatus(ctx, convID, statusStr, errMsg); err != nil {
		s.Log.Warn("acpbridge: failed to record turn_status", "conversation_id", convID, "error", err)
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

	// Task-level handoff (#392): a successful task-linked acp run persists its
	// final reply idempotently, same as the llm path in handler.Handle.
	if statusStr == "finished" {
		taskID, err := s.ConvRepo.GetConversationTaskID(ctx, convID)
		if err != nil {
			s.Log.Warn("acpbridge: failed to resolve task id for handoff", "conversation_id", convID, "error", err)
		} else if taskID != uuid.Nil {
			summary, err := s.ConvRepo.LatestAgentReply(ctx, convID)
			if err != nil {
				s.Log.Warn("acpbridge: failed to read final reply for handoff", "conversation_id", convID, "error", err)
			} else if summary != "" {
				if err := s.ConvRepo.InsertTaskHandoff(ctx, taskID, convID, summary); err != nil {
					s.Log.Warn("acpbridge: failed to persist task handoff", "task_id", taskID, "conversation_id", convID, "error", err)
				} else {
					s.Log.Info("acpbridge: task handoff persisted", "task_id", taskID, "conversation_id", convID)
				}
			}
		}
	}

	// Durable terminal status mirrors handler.publishTerminalStatus so a
	// trigger_ai_agent automation walk paused on this execution can resume.
	// The internal handoff above is not projected to task activity.
	if statusStr == "finished" || statusStr == "failed" || statusStr == "stopped" {
		if err := s.Publisher.PublishConversationStatus(ctx, convID, statusStr); err != nil {
			s.Log.Warn("acpbridge: failed to publish conversation status",
				"conversation_id", convID, "status", statusStr, "error", err)
		}
	}
}

func (s *Server) handleAuthoritativeEvent(ctx context.Context, agentID uuid.UUID, msg map[string]any) error {
	if s.TurnRuntime == nil {
		return errors.New("authoritative turn runtime is unavailable")
	}
	turnID, runID, claimToken, ok := authoritativeIDs(msg)
	if !ok {
		return errors.New("invalid authoritative turn identity")
	}
	sequence, ok := messageInt(msg["sequence"])
	if !ok || sequence < 0 {
		return errors.New("invalid authoritative event sequence")
	}
	eventID, err := uuid.Parse(stringValue(msg["event_id"]))
	if err != nil {
		return errors.New("invalid authoritative event id")
	}
	eventType := stringValue(msg["event_type"])
	if eventType == "" {
		eventType = "ACPEvent"
	}
	eventSource := stringValue(msg["event_source"])
	if eventSource == "" {
		eventSource = "agent"
	}
	payload := rawBridgePayload(msg["payload"])
	createdAt, err := time.Parse(time.RFC3339Nano, stringValue(msg["created_at"]))
	if err != nil {
		return errors.New("invalid authoritative event timestamp")
	}
	if err := s.TurnRuntime.AppendEvent(ctx, turnID, turnruntime.Event{
		ID: eventID, RunID: runID, ClaimToken: claimToken, Sequence: sequence,
		EventType: eventType, EventSource: eventSource, Payload: payload, CreatedAt: createdAt,
	}); err != nil {
		s.Log.Warn("acpbridge: rejected authoritative turn event", "turn_id", turnID, "run_id", runID, "agent_id", agentID, "error", err)
		return err
	}
	return nil
}

func (s *Server) handleAuthoritativeStatus(ctx context.Context, agentID uuid.UUID, msg map[string]any) error {
	if s.TurnRuntime == nil {
		return errors.New("authoritative turn runtime is unavailable")
	}
	turnID, runID, claimToken, ok := authoritativeIDs(msg)
	if !ok {
		return errors.New("invalid authoritative turn identity")
	}
	status := stringValue(msg["status"])
	disposition := "retired"
	var stableEventID *uuid.UUID
	var finalSequence *int
	var errorCode, errorMessage *string
	switch status {
	case "finished":
		stable := strings.TrimSpace(stringValue(msg["stable_output"]))
		if stable == "" {
			status = "no_output"
			code, message := "no_stable_output", "The ACP agent completed without a stable response."
			errorCode, errorMessage = &code, &message
			break
		}
		sequence, sequenceOK := messageInt(msg["stable_sequence"])
		eventID, idErr := uuid.Parse(stringValue(msg["stable_event_id"]))
		createdAt, timeErr := time.Parse(time.RFC3339Nano, stringValue(msg["stable_created_at"]))
		if !sequenceOK || sequence < 0 || idErr != nil || timeErr != nil {
			return errors.New("invalid authoritative stable output metadata")
		}
		payload, _ := json.Marshal(map[string]string{"text": stable})
		if err := s.TurnRuntime.AppendEvent(ctx, turnID, turnruntime.Event{
			ID: eventID, RunID: runID, ClaimToken: claimToken, Sequence: sequence,
			EventType: turnruntime.StableOutputEventType, EventSource: "agent",
			Payload: payload, CreatedAt: createdAt,
		}); err != nil {
			s.Log.Warn("acpbridge: rejected authoritative stable event", "turn_id", turnID, "error", err)
			return err
		}
		// The local ACP subprocess environment is turn-scoped. Retire it after
		// success until the bridge can rotate policy/credentials inside a live
		// runtime without inheriting the prior turn's authority.
		status, disposition, stableEventID, finalSequence = "succeeded", "retired", &eventID, &sequence
	case "failed":
		code, message := "acp_execution_failed", stringValue(msg["error_message"])
		if message == "" {
			message = "The ACP agent failed to complete this turn."
		}
		errorCode, errorMessage = &code, &message
	case "stopped", "cancelled":
		code, message := "acp_"+status, "The ACP turn was "+status+"."
		errorCode, errorMessage = &code, &message
	default:
		return errors.New("invalid authoritative terminal status")
	}
	if err := s.TurnRuntime.Finalize(ctx, turnID, turnruntime.FinalizeInput{
		RunID: runID, ClaimToken: claimToken, TerminalStatus: status,
		StableOutputEventID: stableEventID, GeneratedByAgentID: agentID,
		ErrorCode: errorCode, ErrorMessage: errorMessage,
		RuntimeDisposition: disposition, FinalSequence: finalSequence,
	}); err != nil {
		s.Log.Warn("acpbridge: rejected authoritative turn status", "turn_id", turnID, "run_id", runID, "agent_id", agentID, "status", status, "error", err)
		return err
	}
	return nil
}

func (s *Server) ackAuthoritativeDelivery(ctx context.Context, conn *websocket.Conn, msg map[string]any, deliveryErr error) {
	deliveryID := stringValue(msg["delivery_id"])
	if deliveryID == "" || !authoritativeDeliveryComplete(deliveryErr) {
		return
	}
	ack := map[string]any{"type": "delivery_ack", "delivery_id": deliveryID, "accepted": deliveryErr == nil}
	if deliveryErr != nil {
		code := turnruntime.ErrorCode(deliveryErr)
		if code == "" {
			code = "INVALID_DELIVERY"
		}
		ack["error_code"] = code
	}
	if err := wsjson.Write(ctx, conn, ack); err != nil {
		s.Log.Warn("acpbridge: failed to acknowledge authoritative delivery", "delivery_id", deliveryID, "error", err)
	}
}

func authoritativeDeliveryComplete(err error) bool {
	if err == nil {
		return true
	}
	var apiErr *turnruntime.APIError
	if errors.As(err, &apiErr) {
		return apiErr.Status >= 400 && apiErr.Status < 500
	}
	// Locally invalid wire data is permanent. Network/control-plane failures
	// arrive as other concrete errors from the runtime client and are retried.
	return strings.HasPrefix(err.Error(), "invalid authoritative")
}

func authoritativeIDs(msg map[string]any) (uuid.UUID, uuid.UUID, uuid.UUID, bool) {
	turnID, turnErr := uuid.Parse(stringValue(msg["turn_id"]))
	runID, runErr := uuid.Parse(stringValue(msg["run_id"]))
	claimToken, claimErr := uuid.Parse(stringValue(msg["claim_token"]))
	return turnID, runID, claimToken, turnErr == nil && runErr == nil && claimErr == nil
}

func rawBridgePayload(value any) json.RawMessage {
	if raw, ok := value.(string); ok && json.Valid([]byte(raw)) {
		return json.RawMessage(raw)
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return json.RawMessage(`{}`)
	}
	return encoded
}

func stringValue(value any) string {
	text, _ := value.(string)
	return text
}

func messageInt(value any) (int, bool) {
	switch number := value.(type) {
	case float64:
		return int(number), number == float64(int(number))
	case int:
		return number, true
	case json.Number:
		integer, err := number.Int64()
		return int(integer), err == nil
	default:
		return 0, false
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
