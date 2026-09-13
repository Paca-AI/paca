package handler

import (
	"encoding/json"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Paca-AI/api/internal/apierr"
	agentdom "github.com/Paca-AI/api/internal/domain/agent"
	projectdom "github.com/Paca-AI/api/internal/domain/project"
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
	svc        agentdom.Service
	memberRepo projectdom.MemberRepository
}

// NewConversationHandler returns a ConversationHandler wired to the agent service.
func NewConversationHandler(svc agentdom.Service) *ConversationHandler {
	return &ConversationHandler{svc: svc}
}

// WithMemberRepo attaches the project member repository used to resolve the
// authenticated caller's project_members.id (see resolveMemberID). Required
// for every project-scoped conversation endpoint that must compare the caller
// against an owner-private conversation's session owner.
func (h *ConversationHandler) WithMemberRepo(repo projectdom.MemberRepository) *ConversationHandler {
	h.memberRepo = repo
	return h
}

// resolveMemberID maps the authenticated caller's user ID to their
// project_members.id within projectID. Mirrors AgentHandler.resolveMemberID —
// agent_chat_sessions.member_id and agent_conversations.triggered_by_member_id
// both store a project_members.id, never the raw user id, so callers must not
// use claims.Subject directly.
func (h *ConversationHandler) resolveMemberID(r *http.Request, projectID uuid.UUID) (uuid.UUID, error) {
	if h.memberRepo == nil {
		return uuid.Nil, apierr.New(apierr.CodeInternalError, "member resolver not available")
	}
	claims := middleware.ClaimsFrom(r)
	userID, err := uuid.Parse(claims.Subject)
	if err != nil {
		return uuid.Nil, apierr.New(apierr.CodeBadRequest, "invalid subject claim")
	}
	member, err := h.memberRepo.FindMemberByUserProject(r.Context(), userID, projectID)
	if err != nil {
		return uuid.Nil, err
	}
	return member.ID, nil
}

// parseConversationListQuery parses the query params shared by
// ListConversations and ListGlobalConversations into a filter + page size.
// Callers are responsible for setting the scope fields (ProjectID/GlobalOnly/
// ActorUserID) on the returned filter themselves.
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
func parseConversationListQuery(r *http.Request) (agentdom.ListConversationsFilter, int, error) {
	pageSize, err := parsePageSize(r, 20, 200)
	if err != nil {
		return agentdom.ListConversationsFilter{}, 0, err
	}

	var filter agentdom.ListConversationsFilter
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
				return agentdom.ListConversationsFilter{}, 0, apierr.New(apierr.CodeBadRequest, "invalid status: "+s)
			}
		}
		filter.Statuses = statuses
	}
	if triggerTypesRaw := r.URL.Query().Get("trigger_type"); triggerTypesRaw != "" {
		triggerTypes := splitCommaList(triggerTypesRaw)
		for _, tt := range triggerTypes {
			if !slices.Contains(validConversationTriggerTypes, tt) {
				return agentdom.ListConversationsFilter{}, 0, apierr.New(apierr.CodeBadRequest, "invalid trigger_type: "+tt)
			}
		}
		filter.TriggerTypes = triggerTypes
	}
	if raw := strings.TrimSpace(r.URL.Query().Get("created_after")); raw != "" {
		t, ok := parseCreatedAfterBound(raw)
		if !ok {
			return agentdom.ListConversationsFilter{}, 0, apierr.New(apierr.CodeBadRequest, "invalid created_after")
		}
		filter.CreatedAfter = t
	}
	if raw := strings.TrimSpace(r.URL.Query().Get("created_before")); raw != "" {
		t, ok := parseCreatedBeforeBound(raw)
		if !ok {
			return agentdom.ListConversationsFilter{}, 0, apierr.New(apierr.CodeBadRequest, "invalid created_before")
		}
		filter.CreatedBefore = t
	}
	if search := strings.TrimSpace(r.URL.Query().Get("search")); search != "" {
		filter.Search = &search
	}
	if cursorRaw := r.URL.Query().Get("cursor"); cursorRaw != "" {
		filter.CursorAfter = &cursorRaw
	}
	return filter, pageSize, nil
}

// writeConversationListResponse encodes a conversation page in the shared
// {items, page_size, next_cursor} envelope used by both list endpoints.
func writeConversationListResponse(w http.ResponseWriter, r *http.Request, convs []*agentdom.AgentConversation, hasMore bool, pageSize int) {
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

// ListConversations handles GET /projects/:projectId/conversations.
func (h *ConversationHandler) ListConversations(w http.ResponseWriter, r *http.Request) {
	projectID, err := parseProjectID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	filter, pageSize, err := parseConversationListQuery(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	memberID, err := h.resolveMemberID(r, projectID)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	filter.ProjectID = &projectID
	filter.ViewerMemberID = &memberID

	convs, hasMore, err := h.svc.ListConversations(r.Context(), filter, pageSize)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	writeConversationListResponse(w, r, convs, hasMore, pageSize)
}

// ListGlobalConversations handles GET /agents/conversations. Returns only
// the caller's own global-chat conversations — see
// agentdom.Service.ListGlobalConversations.
func (h *ConversationHandler) ListGlobalConversations(w http.ResponseWriter, r *http.Request) {
	userID, err := callerUserID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	filter, pageSize, err := parseConversationListQuery(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}

	convs, hasMore, err := h.svc.ListGlobalConversations(r.Context(), userID, filter, pageSize)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	writeConversationListResponse(w, r, convs, hasMore, pageSize)
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
	memberID, err := h.resolveMemberID(r, projectID)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	conv, err := h.svc.GetConversation(r.Context(), projectID, convID, memberID)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.OK(w, r, dto.ConversationFromEntity(conv))
}

// currentConversationIDFromHeader parses X-Conversation-ID — set by
// agent-runner's MCP server (see executor.go's buildMCPServers and
// apps/mcp's conversation-client.ts) to the conversation the calling agent
// process is actually running as part of right now. Missing or malformed
// yields uuid.Nil, which GetConversationForAgent treats as "no trusted
// current context to check against" and fails closed, not as a wildcard.
func currentConversationIDFromHeader(r *http.Request) uuid.UUID {
	id, _ := uuid.Parse(r.Header.Get("X-Conversation-ID"))
	return id
}

// GetConversationForAgent handles GET /agents/me/conversations/:conversationId
// — an agent (X-Agent-ID authenticated, no human member/actor identity of
// its own) reading a conversation by ID, e.g. the read_conversation MCP
// tool. See agentdom.Service.GetConversationForAgent's doc comment for the
// full authorization rule; it's deliberately not projectId-scoped in the
// URL (unlike GetConversation) since the calling agent doesn't need to
// already know which project a referenced conversation lives in.
func (h *ConversationHandler) GetConversationForAgent(w http.ResponseWriter, r *http.Request) {
	agentID, ok := middleware.AgentIDFromRequest(r)
	if !ok {
		presenter.Error(w, r, apierr.New(apierr.CodeUnauthenticated, "agent identity required"))
		return
	}
	convID, err := parseParamUUID(r, "conversationId")
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	conv, err := h.svc.GetConversationForAgent(r.Context(), convID, agentID, currentConversationIDFromHeader(r))
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.OK(w, r, dto.ConversationFromEntity(conv))
}

// GetConversationEventsForAgent handles GET
// /agents/me/conversations/:conversationId/events — the agent-authenticated
// sibling of ListConversationEvents/GetGlobalConversationEvents. See
// GetConversationForAgent's doc comment for why this needs its own path.
func (h *ConversationHandler) GetConversationEventsForAgent(w http.ResponseWriter, r *http.Request) {
	agentID, ok := middleware.AgentIDFromRequest(r)
	if !ok {
		presenter.Error(w, r, apierr.New(apierr.CodeUnauthenticated, "agent identity required"))
		return
	}
	convID, err := parseParamUUID(r, "conversationId")
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	if _, err := h.svc.GetConversationForAgent(r.Context(), convID, agentID, currentConversationIDFromHeader(r)); err != nil {
		presenter.Error(w, r, err)
		return
	}

	window, err := parseConversationEventWindowQuery(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	events, total, err := h.svc.ListConversationEvents(r.Context(), convID, window)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	writeConversationEventWindowResponse(w, r, events, total)
}

// parseConversationEventWindowQuery parses the query params shared by
// ListConversationEvents and GetGlobalConversationEvents:
//   - after=<opaque cursor>   events after this point, ascending — pages
//     forward/catches up to newly arrived events.
//   - before=<opaque cursor>  the events immediately preceding this point —
//     pages backward into older history.
//   - limit=<1-200>           page size; defaults to 50.
//
// after and before are mutually exclusive; supplying both is a 400. Neither
// supplied selects the newest `limit` events, so a conversation can be
// opened on its tail without a separate round trip to learn its length.
func parseConversationEventWindowQuery(r *http.Request) (agentdom.ConversationEventWindow, error) {
	limit := 50
	if raw := r.URL.Query().Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > 200 {
			return agentdom.ConversationEventWindow{}, apierr.New(apierr.CodeBadRequest, "limit must be an integer between 1 and 200")
		}
		limit = n
	}

	window := agentdom.ConversationEventWindow{Limit: limit}
	if raw := r.URL.Query().Get("after"); raw != "" {
		window.After = &raw
	}
	if raw := r.URL.Query().Get("before"); raw != "" {
		window.Before = &raw
	}
	if window.After != nil && window.Before != nil {
		return agentdom.ConversationEventWindow{}, apierr.New(apierr.CodeBadRequest, "after and before are mutually exclusive")
	}
	return window, nil
}

// writeConversationEventWindowResponse encodes an events page in the
// {items, total, next_cursor, prev_cursor} envelope shared by
// ListConversationEvents and GetGlobalConversationEvents.
//
// prev_cursor and next_cursor are not symmetric. prev_cursor reports whether
// older events genuinely remain — event_index is gapless and 0-based, so
// first.EventIndex > 0 is a stable, permanent fact once true. next_cursor
// cannot make the equivalent claim about newer events: this is a live
// stream, so "nothing newer exists" is only ever true as of this query and
// can be stale by the time the response is read. next_cursor is therefore
// always present when the page is non-empty — a resume token for the
// forward direction, not a promise that resuming will find something —
// leaving the caller (useConversationEventWindow, weighing it against
// realtime's tail signal) to decide whether using it is worthwhile.
func writeConversationEventWindowResponse(w http.ResponseWriter, r *http.Request, events []*agentdom.AgentConversationEvent, total int64) {
	resp := make([]dto.AgentConversationEventResponse, 0, len(events))
	for _, e := range events {
		resp = append(resp, dto.ConversationEventFromEntity(e))
	}

	var nextCursor, prevCursor *string
	if len(events) > 0 {
		first, last := events[0], events[len(events)-1]
		if first.EventIndex > 0 {
			s := agentdom.EncodeConversationEventCursor(first)
			prevCursor = &s
		}
		s := agentdom.EncodeConversationEventCursor(last)
		nextCursor = &s
	}
	presenter.OK(w, r, map[string]any{
		"items":       resp,
		"total":       total,
		"next_cursor": nextCursor,
		"prev_cursor": prevCursor,
	})
}

// ListConversationEvents handles GET /projects/:projectId/conversations/:conversationId/events.
func (h *ConversationHandler) ListConversationEvents(w http.ResponseWriter, r *http.Request) {
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
	memberID, err := h.resolveMemberID(r, projectID)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	// Gate on project + audience ownership before listing events — the events
	// query itself is conversation-scoped only (same shape as the global
	// sibling GetGlobalConversationEvents, which gates via GetGlobalConversation).
	if _, err := h.svc.GetConversation(r.Context(), projectID, convID, memberID); err != nil {
		presenter.Error(w, r, err)
		return
	}

	window, err := parseConversationEventWindowQuery(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	events, total, err := h.svc.ListConversationEvents(r.Context(), convID, window)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	writeConversationEventWindowResponse(w, r, events, total)
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
	memberID, err := h.resolveMemberID(r, projectID)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	if err := h.svc.StopConversation(r.Context(), projectID, convID, memberID); err != nil {
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
	memberID, err := h.resolveMemberID(r, projectID)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	if err := h.svc.PauseConversation(r.Context(), projectID, convID, memberID); err != nil {
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
	memberID, err := h.resolveMemberID(r, projectID)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	if err := h.svc.Heartbeat(r.Context(), projectID, convID, memberID); err != nil {
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
	if err := agentdom.ValidateContextItems(req.ContextItems); err != nil {
		presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, err.Error()))
		return
	}
	// Resolve the caller to a project_members.id — agent_conversations store a
	// member id, not the raw user id (see resolveMemberID). Using claims.Subject
	// here directly would compare/send the wrong identity on every owner check.
	memberID, err := h.resolveMemberID(r, projectID)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}

	if err := h.svc.SendConversationMessage(r.Context(), projectID, convID, req.Message, memberID, req.ContextItems, req.OnBusy); err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.OK(w, r, map[string]any{"message": "message sent"})
}

// --- Global Conversations ------------------------------------------------
//
// Siblings of the methods above, scoped to a global-chat conversation
// (ProjectID == uuid.Nil) instead of a given project — no project context,
// so there is no projectId route param to parse.

// GetGlobalConversation handles GET /agents/conversations/:conversationId.
// Scoped to the caller's own conversation — see
// agentdom.Service.GetGlobalConversation's doc comment.
func (h *ConversationHandler) GetGlobalConversation(w http.ResponseWriter, r *http.Request) {
	convID, err := parseParamUUID(r, "conversationId")
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	userID, err := callerUserID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	conv, err := h.svc.GetGlobalConversation(r.Context(), convID, userID)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.OK(w, r, dto.ConversationFromEntity(conv))
}

// GetGlobalConversationEvents handles GET /agents/conversations/:conversationId/events.
// ListConversationEvents itself is conversation-scoped only (no ownership
// check of its own, see the project-scoped ListConversationEvents above) —
// GetGlobalConversation is called first to gate on both "is this a global
// conversation" and "does it belong to the caller" before ever reaching it.
func (h *ConversationHandler) GetGlobalConversationEvents(w http.ResponseWriter, r *http.Request) {
	convID, err := parseParamUUID(r, "conversationId")
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	userID, err := callerUserID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	if _, err := h.svc.GetGlobalConversation(r.Context(), convID, userID); err != nil {
		presenter.Error(w, r, err)
		return
	}
	window, err := parseConversationEventWindowQuery(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	events, total, err := h.svc.ListConversationEvents(r.Context(), convID, window)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	writeConversationEventWindowResponse(w, r, events, total)
}

// StopGlobalConversation handles POST /agents/conversations/:conversationId/stop.
func (h *ConversationHandler) StopGlobalConversation(w http.ResponseWriter, r *http.Request) {
	convID, err := parseParamUUID(r, "conversationId")
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	userID, err := callerUserID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	if err := h.svc.StopGlobalConversation(r.Context(), convID, userID); err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.OK(w, r, map[string]any{"message": "conversation stopped"})
}

// PauseGlobalConversation handles POST /agents/conversations/:conversationId/pause.
func (h *ConversationHandler) PauseGlobalConversation(w http.ResponseWriter, r *http.Request) {
	convID, err := parseParamUUID(r, "conversationId")
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	userID, err := callerUserID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	if err := h.svc.PauseGlobalConversation(r.Context(), convID, userID); err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.OK(w, r, map[string]any{"message": "conversation pause requested"})
}

// GlobalConversationHeartbeat handles POST /agents/conversations/:conversationId/heartbeat.
func (h *ConversationHandler) GlobalConversationHeartbeat(w http.ResponseWriter, r *http.Request) {
	convID, err := parseParamUUID(r, "conversationId")
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	userID, err := callerUserID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	if err := h.svc.GlobalHeartbeat(r.Context(), convID, userID); err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.OK(w, r, map[string]any{"status": "ok"})
}

// SendGlobalConversationMessage handles POST /agents/conversations/:conversationId/messages.
func (h *ConversationHandler) SendGlobalConversationMessage(w http.ResponseWriter, r *http.Request) {
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
	if err := agentdom.ValidateContextItems(req.ContextItems); err != nil {
		presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, err.Error()))
		return
	}
	userID, err := callerUserID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}

	if err := h.svc.SendGlobalConversationMessage(r.Context(), convID, req.Message, userID, req.ContextItems, req.OnBusy); err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.OK(w, r, map[string]any{"message": "message sent"})
}
