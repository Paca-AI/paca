package handler

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
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

type ProjectChatHandler struct {
	svc        agentdom.ProjectChatService
	memberRepo projectdom.MemberRepository
}

func NewProjectChatHandler(svc agentdom.ProjectChatService, memberRepo projectdom.MemberRepository) *ProjectChatHandler {
	return &ProjectChatHandler{svc: svc, memberRepo: memberRepo}
}

func (h *ProjectChatHandler) ListSessions(w http.ResponseWriter, r *http.Request) {
	projectID, actor, err := h.actor(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	filter := agentdom.ChatSessionListFilter{
		ProjectID: projectID, MemberID: actor.MemberID, Search: strings.TrimSpace(r.URL.Query().Get("search")),
	}
	filter.Limit, err = boundedQueryInt(r, "limit", 30, 1, 100)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	if value := r.URL.Query().Get("agent_id"); value != "" {
		agentID, parseErr := uuid.Parse(value)
		if parseErr != nil {
			presenter.Error(w, r, badRequest("invalid agent_id"))
			return
		}
		filter.AgentID = &agentID
	}
	if value := r.URL.Query().Get("cursor"); value != "" {
		filter.CursorTime, filter.CursorID, err = decodeProjectChatCursor(value)
		if err != nil {
			presenter.Error(w, r, badRequest("invalid cursor"))
			return
		}
	}
	items, hasMore, err := h.svc.ListChatSessions(r.Context(), filter, actor)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	responses := make([]dto.ProjectChatSessionSummaryResponse, 0, len(items))
	for _, item := range items {
		response := dto.ProjectChatSessionSummaryResponse{
			Session: dto.ProjectChatSessionFromEntity(&item.Session), AgentName: item.AgentName,
			AgentHandle: item.AgentHandle, HasLegacyExecutions: item.HasLegacyExecutions,
		}
		if item.LatestTurn != nil {
			turn := dto.ProjectChatTurnFromEntity(item.LatestTurn)
			response.LatestTurn = &turn
		}
		if item.LatestRun != nil {
			run := dto.ProjectChatRunFromEntity(item.LatestRun)
			response.LatestRun = &run
		}
		responses = append(responses, response)
	}
	var nextCursor *string
	if hasMore && len(items) > 0 {
		last := items[len(items)-1].Session
		at := last.CreatedAt
		if last.LastMessageAt != nil {
			at = *last.LastMessageAt
		}
		encoded := encodeProjectChatCursor(at, last.ID)
		nextCursor = &encoded
	}
	presenter.OK(w, r, map[string]any{"items": responses, "next_cursor": nextCursor})
}

func (h *ProjectChatHandler) CreateSession(w http.ResponseWriter, r *http.Request) {
	projectID, actor, err := h.actor(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	var request dto.CreateProjectChatRequest
	if err := decodeProjectChatJSON(r, &request); err != nil {
		presenter.Error(w, r, err)
		return
	}
	key, err := idempotencyKey(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	bundle, replayed, err := h.svc.CreateProjectChat(r.Context(), agentdom.CreateProjectChatInput{
		ProjectID: projectID, AgentID: request.AgentID, Actor: actor,
		Message: request.Message, Title: request.Title,
		ContextSources: dto.ContextSourceRefsFromRequest(request.ContextSources),
		IdempotencyKey: key, DeadlineAt: request.DeadlineAt,
	})
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.Created(w, r, map[string]any{"bundle": dto.ProjectChatBundleFromEntity(bundle), "replayed": replayed})
}

func (h *ProjectChatHandler) GetSession(w http.ResponseWriter, r *http.Request) {
	projectID, actor, err := h.actor(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	sessionID, err := parseParamUUID(r, "sessionId")
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	session, err := h.svc.GetChatSession(r.Context(), projectID, sessionID, actor)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.OK(w, r, dto.ProjectChatSessionFromEntity(session))
}

func (h *ProjectChatHandler) GetTurn(w http.ResponseWriter, r *http.Request) {
	projectID, actor, err := h.actor(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	sessionID, err := parseParamUUID(r, "sessionId")
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	turnID, err := parseParamUUID(r, "turnId")
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	bundle, err := h.svc.GetChatTurn(r.Context(), projectID, sessionID, turnID, actor)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.OK(w, r, dto.ProjectChatBundleFromEntity(bundle))
}

func (h *ProjectChatHandler) ListTurns(w http.ResponseWriter, r *http.Request) {
	projectID, actor, err := h.actor(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	sessionID, err := parseParamUUID(r, "sessionId")
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	limit, err := boundedQueryInt(r, "limit", 30, 1, 100)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	var before *int
	if value := r.URL.Query().Get("before_index"); value != "" {
		parsed, parseErr := strconv.Atoi(value)
		if parseErr != nil || parsed < 1 {
			presenter.Error(w, r, badRequest("invalid before_index"))
			return
		}
		before = &parsed
	}
	items, hasMore, err := h.svc.ListChatTurns(r.Context(), projectID, sessionID, actor, limit, before)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	responses := make([]dto.ProjectChatTurnHistoryResponse, 0, len(items))
	for _, item := range items {
		responses = append(responses, dto.ProjectChatTurnHistoryFromEntity(item))
	}
	var nextBeforeIndex *int
	if hasMore && len(items) > 0 {
		value := items[len(items)-1].Turn.TurnIndex
		nextBeforeIndex = &value
	}
	presenter.OK(w, r, map[string]any{"items": responses, "next_before_index": nextBeforeIndex})
}

func (h *ProjectChatHandler) AppendTurn(w http.ResponseWriter, r *http.Request) {
	projectID, actor, err := h.actor(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	sessionID, err := parseParamUUID(r, "sessionId")
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	var request dto.AppendProjectChatTurnRequest
	if err := decodeProjectChatJSON(r, &request); err != nil {
		presenter.Error(w, r, err)
		return
	}
	key, err := idempotencyKey(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	bundle, replayed, err := h.svc.AppendProjectChatTurn(r.Context(), agentdom.AppendProjectChatTurnInput{
		ProjectID: projectID, SessionID: sessionID, Actor: actor,
		Message: request.Message, IdempotencyKey: key, DeadlineAt: request.DeadlineAt,
	})
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.Created(w, r, map[string]any{"bundle": dto.ProjectChatBundleFromEntity(bundle), "replayed": replayed})
}

func (h *ProjectChatHandler) StopTurn(w http.ResponseWriter, r *http.Request) {
	projectID, actor, err := h.actor(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	sessionID, err := parseParamUUID(r, "sessionId")
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	turnID, err := parseParamUUID(r, "turnId")
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	result, err := h.svc.StopProjectChatTurn(r.Context(), agentdom.StopProjectChatTurnInput{
		ProjectID: projectID, SessionID: sessionID, TurnID: turnID, Actor: actor,
	})
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.OK(w, r, dto.ProjectChatTurnResultFromEntity(result))
}

func (h *ProjectChatHandler) ListTurnEvents(w http.ResponseWriter, r *http.Request) {
	projectID, actor, err := h.actor(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	turnID, err := parseParamUUID(r, "turnId")
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	limit, err := boundedQueryInt(r, "limit", 100, 1, 500)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	filter := agentdom.TurnEventListFilter{ProjectID: projectID, TurnID: turnID, Limit: limit}
	if value := r.URL.Query().Get("cursor"); value != "" {
		filter.Cursor, err = decodeProjectChatEventCursor(value)
		if err != nil {
			presenter.Error(w, r, badRequest("invalid cursor"))
			return
		}
	}
	events, hasMore, err := h.svc.ListChatTurnEvents(r.Context(), filter, actor)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	responses := make([]dto.ProjectChatEventResponse, 0, len(events))
	for _, event := range events {
		responses = append(responses, dto.ProjectChatEventResponse{
			ID: event.ID, ConversationID: event.ConversationID, EventIndex: event.EventIndex,
			TurnID: event.TurnID, TurnRunID: event.TurnRunID, TurnRunAttempt: event.TurnRunAttempt,
			TurnSequence: event.TurnSequence,
			EventType:    event.EventType, EventSource: event.EventSource,
			Payload: event.Payload, CreatedAt: event.CreatedAt,
		})
	}
	var nextCursor *string
	if hasMore && len(events) > 0 {
		last := events[len(events)-1]
		encoded := encodeProjectChatEventCursor(agentdom.TurnEventCursor{
			EventIndex: last.EventIndex, ID: last.ID,
		})
		nextCursor = &encoded
	}
	presenter.OK(w, r, map[string]any{"items": responses, "next_cursor": nextCursor})
}

func (h *ProjectChatHandler) ListLegacyExecutions(w http.ResponseWriter, r *http.Request) {
	projectID, actor, err := h.actor(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	sessionID, err := parseParamUUID(r, "sessionId")
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	filter := agentdom.LegacyExecutionListFilter{
		ProjectID: projectID, SessionID: sessionID,
	}
	filter.Limit, err = boundedQueryInt(r, "limit", 30, 1, 100)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	if value := r.URL.Query().Get("cursor"); value != "" {
		filter.CursorTime, filter.CursorID, err = decodeProjectChatCursor(value)
		if err != nil {
			presenter.Error(w, r, badRequest("invalid cursor"))
			return
		}
	}
	executions, hasMore, err := h.svc.ListLegacyChatExecutions(r.Context(), filter, actor)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	responses := make([]dto.LegacyChatExecutionResponse, 0, len(executions))
	for _, execution := range executions {
		responses = append(responses, dto.LegacyChatExecutionResponse{
			ConversationID: execution.ConversationID, Status: execution.Status,
			CreatedAt: execution.CreatedAt, FinishedAt: execution.FinishedAt,
		})
	}
	var nextCursor *string
	if hasMore && len(executions) > 0 {
		last := executions[len(executions)-1]
		encoded := encodeProjectChatCursor(last.CreatedAt, last.ConversationID)
		nextCursor = &encoded
	}
	presenter.OK(w, r, map[string]any{"items": responses, "next_cursor": nextCursor})
}

func (h *ProjectChatHandler) ListContextSources(w http.ResponseWriter, r *http.Request) {
	projectID, actor, err := h.actor(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	sessionID, err := parseParamUUID(r, "sessionId")
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	sources, err := h.svc.ListChatContextSources(r.Context(), projectID, sessionID, actor)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.OK(w, r, map[string]any{"items": contextSourceResponses(sources)})
}

func (h *ProjectChatHandler) ReplaceContextSources(w http.ResponseWriter, r *http.Request) {
	projectID, actor, err := h.actor(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	sessionID, err := parseParamUUID(r, "sessionId")
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	var request dto.ReplaceProjectChatContextRequest
	if err := decodeProjectChatJSON(r, &request); err != nil {
		presenter.Error(w, r, err)
		return
	}
	sources, err := h.svc.ReplaceChatContextSources(r.Context(), agentdom.ReplaceChatContextInput{
		ProjectID: projectID, SessionID: sessionID, Actor: actor,
		Sources: dto.ContextSourceRefsFromRequest(request.Sources),
	})
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.OK(w, r, map[string]any{"items": contextSourceResponses(sources)})
}

func (h *ProjectChatHandler) PrepareConclusion(w http.ResponseWriter, r *http.Request) {
	projectID, actor, err := h.actor(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	turnID, err := parseParamUUID(r, "turnId")
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	var request dto.PrepareProjectConclusionRequest
	if err := decodeProjectChatJSON(r, &request); err != nil {
		presenter.Error(w, r, err)
		return
	}
	key, err := idempotencyKey(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	preparation, replayed, err := h.svc.PrepareProjectConclusion(r.Context(), agentdom.PrepareProjectConclusionInput{
		ProjectID: projectID, SourceTurnID: turnID, TargetTaskID: request.TargetTaskID,
		Actor: actor, Kind: agentdom.ConclusionPublished,
		SummaryOverride: request.SummaryOverride, UpdateDescription: request.UpdateDescription,
		DescriptionBase:     request.DescriptionBase,
		ProposedDescription: request.ProposedDescription,
		IdempotencyKey:      key, ExpiresAt: request.ExpiresAt,
	})
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.Created(w, r, map[string]any{
		"preparation": dto.ConclusionPreparationFromEntity(preparation), "replayed": replayed,
	})
}

func (h *ProjectChatHandler) ConfirmConclusion(w http.ResponseWriter, r *http.Request) {
	projectID, actor, err := h.actor(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	var request dto.ConfirmProjectConclusionRequest
	if err := decodeProjectChatJSON(r, &request); err != nil {
		presenter.Error(w, r, err)
		return
	}
	key, err := idempotencyKey(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	view, replayed, err := h.svc.ConfirmProjectConclusion(r.Context(), agentdom.ConfirmProjectConclusionInput{
		ProjectID: projectID, PreparationID: request.PreparationID, Actor: actor,
		ExpectedVersion: request.ExpectedVersion, ExpectedSHA256: request.ExpectedSHA256,
		IdempotencyKey: key,
	})
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.Created(w, r, map[string]any{
		"publication": dto.ConclusionPublicationFromEntity(&view.Publication, view.SourceAccessible,
			view.SourceSessionID, view.SourceTurnID),
		"replayed": replayed,
	})
}

func (h *ProjectChatHandler) ListTaskConclusions(w http.ResponseWriter, r *http.Request) {
	projectID, actor, err := h.taskConclusionActor(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	taskID, err := parseParamUUID(r, "taskId")
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	filter := agentdom.ConclusionPublicationListFilter{ProjectID: projectID, TaskID: taskID}
	filter.Limit, err = boundedQueryInt(r, "limit", 50, 1, 200)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	if value := r.URL.Query().Get("cursor"); value != "" {
		filter.CursorTime, filter.CursorID, err = decodeProjectChatCursor(value)
		if err != nil {
			presenter.Error(w, r, badRequest("invalid cursor"))
			return
		}
	}
	views, hasMore, err := h.svc.ListProjectTaskConclusions(r.Context(), filter, actor)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	responses := make([]dto.ConclusionPublicationResponse, 0, len(views))
	for index := range views {
		view := &views[index]
		responses = append(responses, dto.ConclusionPublicationFromEntity(
			&view.Publication, view.SourceAccessible, view.SourceSessionID, view.SourceTurnID,
		))
	}
	var nextCursor *string
	if hasMore && len(views) > 0 {
		last := views[len(views)-1].Publication
		encoded := encodeProjectChatCursor(last.CreatedAt, last.ID)
		nextCursor = &encoded
	}
	presenter.OK(w, r, map[string]any{"items": responses, "next_cursor": nextCursor})
}

func (h *ProjectChatHandler) taskConclusionActor(r *http.Request) (uuid.UUID, agentdom.ChatActor, error) {
	projectID, err := parseProjectID(r)
	if err != nil {
		return uuid.Nil, agentdom.ChatActor{}, err
	}
	// API keys may read the same shared projection as the task route, but
	// never receive a private session/turn source link. Source routes are
	// intentionally human JWT-only.
	if middleware.IsAPIKeyAuth(r) {
		return projectID, agentdom.ChatActor{}, nil
	}
	claims := middleware.ClaimsFrom(r)
	if claims == nil {
		return projectID, agentdom.ChatActor{}, nil
	}
	userID, err := uuid.Parse(claims.Subject)
	if err != nil {
		return uuid.Nil, agentdom.ChatActor{}, badRequest("invalid subject claim")
	}
	member, err := h.memberRepo.FindMemberByUserProject(r.Context(), userID, projectID)
	if errors.Is(err, projectdom.ErrMemberNotFound) {
		return projectID, agentdom.ChatActor{}, nil
	}
	if err != nil {
		return uuid.Nil, agentdom.ChatActor{}, err
	}
	return projectID, agentdom.ChatActor{
		UserID: userID, MemberID: member.ID, LegacyRole: claims.Role,
	}, nil
}

func (h *ProjectChatHandler) actor(r *http.Request) (uuid.UUID, agentdom.ChatActor, error) {
	projectID, err := parseProjectID(r)
	if err != nil {
		return uuid.Nil, agentdom.ChatActor{}, err
	}
	claims := middleware.ClaimsFrom(r)
	if claims == nil {
		return uuid.Nil, agentdom.ChatActor{}, apierr.New(apierr.CodeUnauthenticated, "unauthenticated")
	}
	userID, err := uuid.Parse(claims.Subject)
	if err != nil {
		return uuid.Nil, agentdom.ChatActor{}, badRequest("invalid subject claim")
	}
	member, err := h.memberRepo.FindMemberByUserProject(r.Context(), userID, projectID)
	if err != nil {
		return uuid.Nil, agentdom.ChatActor{}, err
	}
	return projectID, agentdom.ChatActor{UserID: userID, MemberID: member.ID, LegacyRole: claims.Role}, nil
}

func decodeProjectChatJSON(r *http.Request, value any) error {
	decoder := json.NewDecoder(io.LimitReader(r.Body, 256*1024+1))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return badRequest("invalid request body")
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return badRequest("invalid request body")
	}
	return nil
}

func idempotencyKey(r *http.Request) (string, error) {
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if key == "" || len(key) > 200 {
		return "", badRequest("Idempotency-Key header is required")
	}
	return key, nil
}

func boundedQueryInt(r *http.Request, key string, fallback, minValue, maxValue int) (int, error) {
	value := r.URL.Query().Get(key)
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < minValue || parsed > maxValue {
		return 0, badRequest(fmt.Sprintf("invalid %s", key))
	}
	return parsed, nil
}

func contextSourceResponses(sources []agentdom.SessionContextSource) []dto.ProjectChatContextSourceResponse {
	responses := make([]dto.ProjectChatContextSourceResponse, 0, len(sources))
	for _, source := range sources {
		responses = append(responses, dto.ProjectChatContextSourceResponse{
			ID: source.ID, SourceType: source.SourceType, SourceID: source.SourceID,
			Ordinal: source.Ordinal, CreatedAt: source.CreatedAt,
		})
	}
	return responses
}

type projectChatCursor struct {
	At time.Time `json:"at"`
	ID uuid.UUID `json:"id"`
}

type projectChatEventCursor struct {
	EventIndex int       `json:"event_index"`
	ID         uuid.UUID `json:"id"`
}

func encodeProjectChatEventCursor(cursor agentdom.TurnEventCursor) string {
	payload, _ := json.Marshal(projectChatEventCursor{
		EventIndex: cursor.EventIndex, ID: cursor.ID,
	})
	return base64.RawURLEncoding.EncodeToString(payload)
}

func decodeProjectChatEventCursor(value string) (*agentdom.TurnEventCursor, error) {
	payload, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return nil, err
	}
	var cursor projectChatEventCursor
	if err := json.Unmarshal(payload, &cursor); err != nil || cursor.EventIndex < 0 || cursor.ID == uuid.Nil {
		return nil, fmt.Errorf("invalid event cursor")
	}
	return &agentdom.TurnEventCursor{
		EventIndex: cursor.EventIndex, ID: cursor.ID,
	}, nil
}

func encodeProjectChatCursor(at time.Time, id uuid.UUID) string {
	payload, _ := json.Marshal(projectChatCursor{At: at.UTC(), ID: id})
	return base64.RawURLEncoding.EncodeToString(payload)
}

func decodeProjectChatCursor(value string) (*time.Time, *uuid.UUID, error) {
	payload, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return nil, nil, err
	}
	var cursor projectChatCursor
	if err := json.Unmarshal(payload, &cursor); err != nil || cursor.ID == uuid.Nil || cursor.At.IsZero() {
		return nil, nil, fmt.Errorf("invalid cursor")
	}
	at := cursor.At.UTC()
	id := cursor.ID
	return &at, &id, nil
}

func badRequest(message string) error {
	return apierr.New(apierr.CodeBadRequest, message)
}
