package handler

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/google/uuid"

	agentdom "github.com/Paca-AI/api/internal/domain/agent"
	"github.com/Paca-AI/api/internal/transport/http/dto"
	"github.com/Paca-AI/api/internal/transport/http/middleware"
	"github.com/Paca-AI/api/internal/transport/http/presenter"
)

// ConversationHandler handles agent conversation endpoints.
type ConversationHandler struct {
	svc agentdom.Service
}

// NewConversationHandler returns a ConversationHandler wired to the agent service.
func NewConversationHandler(svc agentdom.Service) *ConversationHandler {
	return &ConversationHandler{svc: svc}
}

// ListConversations handles GET /projects/:projectId/conversations.
func (h *ConversationHandler) ListConversations(w http.ResponseWriter, r *http.Request) {
	projectID, err := parseProjectID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}

	pageSize, _ := strconv.Atoi(defaultQuery(r, "page_size", "20"))
	if pageSize < 1 || pageSize > 200 {
		pageSize = 20
	}

	filter := agentdom.ListConversationsFilter{
		ProjectID: &projectID,
	}
	if agentIDStr := r.URL.Query().Get("agent_id"); agentIDStr != "" {
		if id, err := uuid.Parse(agentIDStr); err == nil {
			filter.AgentID = &id
		}
	}
	if statusStr := r.URL.Query().Get("status"); statusStr != "" {
		filter.Status = &statusStr
	}
	if cursorRaw := r.URL.Query().Get("cursor"); cursorRaw != "" {
		filter.CursorAfter = &cursorRaw
	}

	convs, hasMore, err := h.svc.ListConversations(r.Context(), filter, pageSize)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	resp := make([]dto.AgentConversationResponse, 0, len(convs))
	for _, conv := range convs {
		resp = append(resp, dto.ConversationFromEntity(conv))
	}

	var nextCursor *string
	if hasMore && len(convs) > 0 {
		s := agentdom.EncodeConversationCursor(convs[len(convs)-1])
		nextCursor = &s
	}
	presenter.OK(w, r, map[string]any{
		"items":       resp,
		"page_size":   pageSize,
		"next_cursor": nextCursor,
	})
}

// GetConversation handles GET /projects/:projectId/conversations/:conversationId.
func (h *ConversationHandler) GetConversation(w http.ResponseWriter, r *http.Request) {
	projectID, err := parseProjectID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	convID, err := parseParamUUID(r, "conversationId")
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	conv, err := h.svc.GetConversation(r.Context(), projectID, convID)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.OK(w, r, dto.ConversationFromEntity(conv))
}

// ListConversationEvents handles GET /projects/:projectId/conversations/:conversationId/events.
func (h *ConversationHandler) ListConversationEvents(w http.ResponseWriter, r *http.Request) {
	convID, err := parseParamUUID(r, "conversationId")
	if err != nil {
		presenter.Error(w, r, err)
		return
	}

	offset, limit := parseOffsetLimit(r)
	events, total, err := h.svc.ListConversationEvents(r.Context(), convID, offset, limit)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	resp := make([]dto.AgentConversationEventResponse, 0, len(events))
	for _, e := range events {
		resp = append(resp, dto.ConversationEventFromEntity(e))
	}
	presenter.OK(w, r, map[string]any{"items": resp, "total": total})
}

// StopConversation handles POST /projects/:projectId/conversations/:conversationId/stop.
func (h *ConversationHandler) StopConversation(w http.ResponseWriter, r *http.Request) {
	projectID, err := parseProjectID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	convID, err := parseParamUUID(r, "conversationId")
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	if err := h.svc.StopConversation(r.Context(), projectID, convID); err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.OK(w, r, map[string]any{"message": "conversation stopped"})
}

// PauseConversation handles POST /projects/:projectId/conversations/:conversationId/pause.
func (h *ConversationHandler) PauseConversation(w http.ResponseWriter, r *http.Request) {
	projectID, err := parseProjectID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	convID, err := parseParamUUID(r, "conversationId")
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	if err := h.svc.PauseConversation(r.Context(), projectID, convID); err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.OK(w, r, map[string]any{"message": "conversation pause requested"})
}

// Heartbeat handles POST /projects/:projectId/conversations/:conversationId/heartbeat.
func (h *ConversationHandler) Heartbeat(w http.ResponseWriter, r *http.Request) {
	projectID, err := parseProjectID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	convID, err := parseParamUUID(r, "conversationId")
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	if err := h.svc.Heartbeat(r.Context(), projectID, convID); err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.OK(w, r, map[string]any{"status": "ok"})
}

// SendConversationMessage handles POST /projects/:projectId/conversations/:conversationId/messages.
func (h *ConversationHandler) SendConversationMessage(w http.ResponseWriter, r *http.Request) {
	projectID, err := parseProjectID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	convID, err := parseParamUUID(r, "conversationId")
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	var req dto.SendMessageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		presenter.Error(w, r, err)
		return
	}
	claims := middleware.ClaimsFrom(r)
	memberID, _ := uuid.Parse(claims.Subject)

	if err := h.svc.SendConversationMessage(r.Context(), projectID, convID, req.Message, memberID); err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.OK(w, r, map[string]any{"message": "message sent"})
}
