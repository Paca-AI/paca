package handler

import (
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Paca-AI/api/internal/apierr"
	agentdom "github.com/Paca-AI/api/internal/domain/agent"
	"github.com/Paca-AI/api/internal/transport/http/dto"
	"github.com/Paca-AI/api/internal/transport/http/middleware"
	"github.com/Paca-AI/api/internal/transport/http/presenter"
)

// validConversationStatuses/validConversationTriggerTypes mirror the
// agent_conversations.status/trigger_type CHECK constraints (see migrations
// 000008 and 000011/000027). Rejecting unknown values here with a 400 gives
// callers a clear signal instead of a filter that silently matches nothing.
var (
	validConversationStatuses = []string{
		"queued", "running", "paused", "finished", "failed", "stopped",
	}
	validConversationTriggerTypes = []string{
		"task_assigned", "comment_mention", "chat_message", "description_write", "automation_message",
	}
)

// parseCreatedAfterBound parses a created_after query value into an
// inclusive lower bound on created_at. It accepts either a bare
// "YYYY-MM-DD" date — treated as UTC midnight of that day, for simple direct
// API use — or a full RFC3339 timestamp, which the frontend sends as the
// precise UTC instant marking the start of the user's local calendar day
// (avoiding the mismatch a bare date would have against a TIMESTAMPTZ
// column for users outside UTC).
func parseCreatedAfterBound(raw string) (*time.Time, bool) {
	if t, err := time.Parse("2006-01-02", raw); err == nil {
		return &t, true
	}
	if t, err := time.Parse(time.RFC3339, raw); err == nil {
		return &t, true
	}
	return nil, false
}

// parseCreatedBeforeBound parses a created_before query value into an
// exclusive upper bound on created_at. A bare "YYYY-MM-DD" date is treated
// as the whole UTC day inclusive, i.e. the bound is pushed to midnight the
// *following* UTC day. A full RFC3339 timestamp is used exactly as given —
// the frontend computes that exclusive boundary itself, from the start of
// the local day after the one the user picked.
func parseCreatedBeforeBound(raw string) (*time.Time, bool) {
	if t, err := time.Parse("2006-01-02", raw); err == nil {
		t = t.AddDate(0, 0, 1)
		return &t, true
	}
	if t, err := time.Parse(time.RFC3339, raw); err == nil {
		return &t, true
	}
	return nil, false
}

// ConversationHandler handles agent conversation endpoints.
type ConversationHandler struct {
	svc agentdom.Service
}

// NewConversationHandler returns a ConversationHandler wired to the agent service.
func NewConversationHandler(svc agentdom.Service) *ConversationHandler {
	return &ConversationHandler{svc: svc}
}

// ListConversations handles GET /projects/:projectId/conversations.
//
// Supported query params (all optional, combine with AND):
//   - agent_id=<uuid>|<uuid,uuid,...>       filter by one or more agents
//   - status=<status>|<status,status,...>   filter by one or more statuses
//   - trigger_type=<type>|<type,type,...>   filter by one or more trigger types
//   - created_after=<YYYY-MM-DD|RFC3339>     conversations created on/after this date/instant
//   - created_before=<YYYY-MM-DD|RFC3339>   conversations created on/before this date/instant
//   - search=<text>                         matches conversations with an event
//     containing this text (see agent_conversation_event_search_text)
//   - cursor=<opaque>, page_size=<1-200>    keyset pagination (see next_cursor in response)
func (h *ConversationHandler) ListConversations(w http.ResponseWriter, r *http.Request) {
	projectID, err := parseProjectID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}

	pageSize, err := parsePageSize(r, 20, 200)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}

	filter := agentdom.ListConversationsFilter{
		ProjectID: &projectID,
	}
	if agentIDsRaw := r.URL.Query().Get("agent_id"); agentIDsRaw != "" {
		for _, s := range strings.Split(agentIDsRaw, ",") {
			if id, err := uuid.Parse(strings.TrimSpace(s)); err == nil {
				filter.AgentIDs = append(filter.AgentIDs, id)
			}
		}
	}
	if statusesRaw := r.URL.Query().Get("status"); statusesRaw != "" {
		statuses := splitCommaList(statusesRaw)
		for _, s := range statuses {
			if !slices.Contains(validConversationStatuses, s) {
				presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "invalid status: "+s))
				return
			}
		}
		filter.Statuses = statuses
	}
	if triggerTypesRaw := r.URL.Query().Get("trigger_type"); triggerTypesRaw != "" {
		triggerTypes := splitCommaList(triggerTypesRaw)
		for _, tt := range triggerTypes {
			if !slices.Contains(validConversationTriggerTypes, tt) {
				presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "invalid trigger_type: "+tt))
				return
			}
		}
		filter.TriggerTypes = triggerTypes
	}
	if raw := strings.TrimSpace(r.URL.Query().Get("created_after")); raw != "" {
		t, ok := parseCreatedAfterBound(raw)
		if !ok {
			presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "invalid created_after"))
			return
		}
		filter.CreatedAfter = t
	}
	if raw := strings.TrimSpace(r.URL.Query().Get("created_before")); raw != "" {
		t, ok := parseCreatedBeforeBound(raw)
		if !ok {
			presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "invalid created_before"))
			return
		}
		filter.CreatedBefore = t
	}
	if search := strings.TrimSpace(r.URL.Query().Get("search")); search != "" {
		filter.Search = &search
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

	offset, limit, err := parseOffsetLimit(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
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
