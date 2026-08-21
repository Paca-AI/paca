package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/jmoiron/sqlx"

	agentdom "github.com/Paca-AI/api/internal/domain/agent"
	apikeydom "github.com/Paca-AI/api/internal/domain/apikey"
	"github.com/Paca-AI/api/internal/platform/authz"
	jwttoken "github.com/Paca-AI/api/internal/platform/token"
	"github.com/Paca-AI/api/internal/repository/postgres"
	agentturnsvc "github.com/Paca-AI/api/internal/service/agentturn"
	"github.com/Paca-AI/api/internal/transport/http/dto"
	httpmw "github.com/Paca-AI/api/internal/transport/http/middleware"
)

func TestProjectChatHTTPCreateSessionIdempotentRetryUsesFrozenBundle(t *testing.T) {
	dsn := os.Getenv("PACA_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("PACA_TEST_DATABASE_URL is not set")
	}
	db, err := sqlx.Connect("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx := context.Background()
	projectID, userID, memberID, agentID, taskID := seedProjectChatHTTPTestScope(t, ctx, db)

	turnRepo := postgres.NewAgentTurnRepository(db)
	authorizer := authz.NewAuthorizer(postgres.NewAuthzPermissionStore(db))
	service := agentturnsvc.New(turnRepo, postgres.NewAgentRepository(db), authorizer)
	h := NewProjectChatHandler(service, postgres.NewProjectRepository(db))
	tokens := jwttoken.New("project-chat-http-test", 15*time.Minute, time.Hour)
	token, err := tokens.IssueAccess(userID.String(), "project-chat-http-user", "USER", uuid.NewString(), false)
	if err != nil {
		t.Fatal(err)
	}
	router := chi.NewRouter()
	router.Use(httpmw.Authn(tokens))
	router.Use(httpmw.RequireJWTAuth())
	router.Post("/projects/{projectId}/chat-sessions", h.CreateSession)
	router.Post("/projects/{projectId}/chat-sessions/{sessionId}/turns", h.AppendTurn)
	router.Post("/projects/{projectId}/chat-sessions/{sessionId}/turns/{turnId}/stop", h.StopTurn)
	router.Post("/projects/{projectId}/turns/{turnId}/conclusion-publications/prepare", h.PrepareConclusion)
	router.Post("/projects/{projectId}/conclusion-publications/confirm", h.ConfirmConclusion)

	body := map[string]any{
		"agent_id": agentID, "message": "analyze this task",
		"context_sources": []map[string]any{{"type": "task", "id": taskID}},
	}
	request := func(value map[string]any, key string) (int, createProjectChatHTTPEnvelope) {
		t.Helper()
		encoded, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(http.MethodPost,
			"/projects/"+projectID.String()+"/chat-sessions", bytes.NewReader(encoded))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Idempotency-Key", key)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, req)
		var payload createProjectChatHTTPEnvelope
		if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
			t.Fatalf("decode response %d: %v body=%s", response.Code, err, response.Body.String())
		}
		return response.Code, payload
	}

	status, first := request(body, "http-retry-1")
	if status != http.StatusCreated || !first.Success || first.Data.Replayed {
		t.Fatalf("first create: status=%d response=%+v", status, first)
	}
	if first.Data.Bundle.Turn.DeadlineAt == nil || len(first.Data.Bundle.Snapshot.Items) != 1 {
		t.Fatalf("first response missing frozen execution data: %+v", first.Data.Bundle)
	}
	stopStatus, stopBundle := request(body, "http-stop-session-1")
	if stopStatus != http.StatusCreated || !stopBundle.Success {
		t.Fatalf("create turn to stop: status=%d response=%+v", stopStatus, stopBundle)
	}
	stopRequest := func(authToken string, sessionID, turnID uuid.UUID) (int, stopProjectChatHTTPEnvelope) {
		t.Helper()
		req := httptest.NewRequest(http.MethodPost, "/projects/"+projectID.String()+
			"/chat-sessions/"+sessionID.String()+"/turns/"+turnID.String()+"/stop", nil)
		req.Header.Set("Authorization", "Bearer "+authToken)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, req)
		var payload stopProjectChatHTTPEnvelope
		if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
			t.Fatalf("decode stop response %d: %v body=%s", response.Code, err, response.Body.String())
		}
		return response.Code, payload
	}
	stopStatus, stopped := stopRequest(token, stopBundle.Data.Bundle.Session.ID, stopBundle.Data.Bundle.Turn.ID)
	if stopStatus != http.StatusOK || !stopped.Success || stopped.Data.TerminalStatus != agentdom.TurnStatusCancelled ||
		stopped.Data.ErrorCode == nil || *stopped.Data.ErrorCode != "cancelled_by_user" {
		t.Fatalf("queued owner stop: status=%d response=%+v", stopStatus, stopped)
	}
	stopStatus, stoppedReplay := stopRequest(token, stopBundle.Data.Bundle.Session.ID, stopBundle.Data.Bundle.Turn.ID)
	if stopStatus != http.StatusOK || !stoppedReplay.Success || stoppedReplay.Data.TurnID != stopped.Data.TurnID ||
		stoppedReplay.Data.RunID != stopped.Data.RunID {
		t.Fatalf("repeated owner stop: status=%d response=%+v", stopStatus, stoppedReplay)
	}
	stopStatus, wrongSession := stopRequest(token, uuid.New(), stopBundle.Data.Bundle.Turn.ID)
	if stopStatus != http.StatusNotFound || wrongSession.Success || wrongSession.ErrorCode != "AGENT_TURN_NOT_FOUND" {
		t.Fatalf("wrong-session stop leaked turn: status=%d response=%+v", stopStatus, wrongSession)
	}
	otherUserID := seedProjectChatHTTPOtherMember(t, ctx, db, projectID)
	otherToken, err := tokens.IssueAccess(otherUserID.String(), "project-chat-http-other", "USER", uuid.NewString(), false)
	if err != nil {
		t.Fatal(err)
	}
	stopStatus, nonOwner := stopRequest(otherToken, stopBundle.Data.Bundle.Session.ID, stopBundle.Data.Bundle.Turn.ID)
	if stopStatus != http.StatusNotFound || nonOwner.Success || nonOwner.ErrorCode != "AGENT_TURN_NOT_FOUND" {
		t.Fatalf("non-owner stop leaked turn: status=%d response=%+v", stopStatus, nonOwner)
	}
	finalizeProjectChatHTTPTurn(t, ctx, turnRepo, first.Data.Bundle, agentID)
	sessionID := first.Data.Bundle.Session.ID
	appendBody := map[string]any{"message": "follow up with the frozen task"}
	appendRequest := func(value map[string]any, key string) (int, createProjectChatHTTPEnvelope) {
		t.Helper()
		encoded, _ := json.Marshal(value)
		req := httptest.NewRequest(http.MethodPost, "/projects/"+projectID.String()+
			"/chat-sessions/"+sessionID.String()+"/turns", bytes.NewReader(encoded))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Idempotency-Key", key)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, req)
		var payload createProjectChatHTTPEnvelope
		if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
			t.Fatalf("decode append response %d: %v body=%s", response.Code, err, response.Body.String())
		}
		return response.Code, payload
	}
	status, firstAppend := appendRequest(appendBody, "http-append-retry-1")
	if status != http.StatusCreated || !firstAppend.Success || firstAppend.Data.Replayed ||
		len(firstAppend.Data.Bundle.Snapshot.Items) != 2 ||
		firstAppend.Data.Bundle.Snapshot.Items[0].SourceType != agentdom.ContextSourceSession ||
		firstAppend.Data.Bundle.Snapshot.Items[0].SourceID != sessionID ||
		firstAppend.Data.Bundle.Snapshot.Items[1].SourceType != agentdom.ContextSourceTask {
		t.Fatalf("first append: status=%d response=%+v", status, firstAppend)
	}
	if _, err := db.ExecContext(ctx, `UPDATE tasks SET title='changed after first request',updated_at=NOW() WHERE id=$1`, taskID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE agents SET deleted_at=NOW() WHERE id=$1`, agentID); err != nil {
		t.Fatal(err)
	}

	status, replay := request(body, "http-retry-1")
	if status != http.StatusCreated || !replay.Success || !replay.Data.Replayed ||
		replay.Data.Bundle.Session == nil || first.Data.Bundle.Session == nil ||
		replay.Data.Bundle.Session.ID != first.Data.Bundle.Session.ID ||
		replay.Data.Bundle.Turn.ID != first.Data.Bundle.Turn.ID ||
		replay.Data.Bundle.Snapshot.ManifestSHA256 != first.Data.Bundle.Snapshot.ManifestSHA256 ||
		replay.Data.Bundle.Turn.DeadlineAt == nil ||
		!replay.Data.Bundle.Turn.DeadlineAt.Equal(*first.Data.Bundle.Turn.DeadlineAt) {
		t.Fatalf("retry did not return original frozen bundle: status=%d first=%+v replay=%+v", status, first, replay)
	}

	conflictBody := map[string]any{
		"agent_id": agentID, "message": "different message",
		"context_sources": []map[string]any{{"type": "task", "id": taskID}},
	}
	status, conflict := request(conflictBody, "http-retry-1")
	if status != http.StatusConflict || conflict.Success || conflict.ErrorCode != "IDEMPOTENCY_CONFLICT" {
		t.Fatalf("same key with different body: status=%d response=%+v", status, conflict)
	}
	status, appendReplay := appendRequest(appendBody, "http-append-retry-1")
	if status != http.StatusCreated || !appendReplay.Success || !appendReplay.Data.Replayed ||
		appendReplay.Data.Bundle.Turn.ID != firstAppend.Data.Bundle.Turn.ID ||
		appendReplay.Data.Bundle.Snapshot.ManifestSHA256 != firstAppend.Data.Bundle.Snapshot.ManifestSHA256 {
		t.Fatalf("append retry did not return frozen bundle: status=%d response=%+v", status, appendReplay)
	}
	status, appendConflict := appendRequest(map[string]any{"message": "different follow up"}, "http-append-retry-1")
	if status != http.StatusConflict || appendConflict.Success || appendConflict.ErrorCode != "IDEMPOTENCY_CONFLICT" {
		t.Fatalf("append key conflict: status=%d response=%+v", status, appendConflict)
	}

	expiresAt := time.Now().UTC().Add(30 * time.Minute).Truncate(time.Microsecond)
	prepareBody := map[string]any{
		"target_task_id": taskID, "expires_at": expiresAt,
	}
	prepareRequest := func(key string) (int, prepareProjectConclusionHTTPEnvelope) {
		t.Helper()
		encoded, _ := json.Marshal(prepareBody)
		req := httptest.NewRequest(http.MethodPost, "/projects/"+projectID.String()+
			"/turns/"+first.Data.Bundle.Turn.ID.String()+"/conclusion-publications/prepare", bytes.NewReader(encoded))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Idempotency-Key", key)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, req)
		var payload prepareProjectConclusionHTTPEnvelope
		if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
			t.Fatalf("decode prepare response %d: %v body=%s", response.Code, err, response.Body.String())
		}
		return response.Code, payload
	}
	status, prepared := prepareRequest("http-prepare-retry-1")
	if status != http.StatusCreated || !prepared.Success || prepared.Data.Replayed || !prepared.Data.Preparation.IsFrozen {
		t.Fatalf("first prepare: status=%d response=%+v", status, prepared)
	}
	status, preparedReplay := prepareRequest("http-prepare-retry-1")
	if status != http.StatusCreated || !preparedReplay.Success || !preparedReplay.Data.Replayed ||
		preparedReplay.Data.Preparation.ID != prepared.Data.Preparation.ID {
		t.Fatalf("prepare replay: status=%d response=%+v", status, preparedReplay)
	}

	confirmRequest := func(key, expectedSHA string) (int, confirmProjectConclusionHTTPEnvelope) {
		t.Helper()
		encoded, _ := json.Marshal(map[string]any{
			"preparation_id":   prepared.Data.Preparation.ID,
			"expected_version": prepared.Data.Preparation.SummaryVersion,
			"expected_sha256":  expectedSHA,
		})
		req := httptest.NewRequest(http.MethodPost, "/projects/"+projectID.String()+
			"/conclusion-publications/confirm", bytes.NewReader(encoded))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Idempotency-Key", key)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, req)
		var payload confirmProjectConclusionHTTPEnvelope
		if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
			t.Fatalf("decode confirm response %d: %v body=%s", response.Code, err, response.Body.String())
		}
		return response.Code, payload
	}
	status, confirmed := confirmRequest("http-confirm-retry-1", prepared.Data.Preparation.SummarySHA256)
	if status != http.StatusCreated || !confirmed.Success || confirmed.Data.Replayed ||
		!confirmed.Data.Publication.SourceAccessible || confirmed.Data.Publication.SourceSessionID == nil {
		t.Fatalf("first confirm: status=%d response=%+v", status, confirmed)
	}
	status, confirmedReplay := confirmRequest("http-confirm-retry-1", prepared.Data.Preparation.SummarySHA256)
	if status != http.StatusCreated || !confirmedReplay.Success || !confirmedReplay.Data.Replayed ||
		confirmedReplay.Data.Publication.ID != confirmed.Data.Publication.ID {
		t.Fatalf("confirm replay: status=%d response=%+v", status, confirmedReplay)
	}
	status, confirmConflict := confirmRequest("http-confirm-retry-1", strings.Repeat("0", 64))
	if status != http.StatusConflict || confirmConflict.Success || confirmConflict.ErrorCode != "IDEMPOTENCY_CONFLICT" {
		t.Fatalf("confirm key conflict: status=%d response=%+v", status, confirmConflict)
	}
	apiKeyReadRouter := chi.NewRouter()
	apiKeyReadRouter.Use(httpmw.Authn(tokens, projectChatTestAPIKeyAuth{userID: userID}))
	apiKeyReadRouter.Get("/projects/{projectId}/tasks/{taskId}/agent-conclusions", h.ListTaskConclusions)
	apiKeyReadRequest := httptest.NewRequest(http.MethodGet, "/projects/"+projectID.String()+
		"/tasks/"+taskID.String()+"/agent-conclusions", nil)
	apiKeyReadRequest.Header.Set("Authorization", "ApiKey project-chat-test-key")
	apiKeyReadResponse := httptest.NewRecorder()
	apiKeyReadRouter.ServeHTTP(apiKeyReadResponse, apiKeyReadRequest)
	var apiKeyRead struct {
		Success bool `json:"success"`
		Data    struct {
			Items []dto.ConclusionPublicationResponse `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal(apiKeyReadResponse.Body.Bytes(), &apiKeyRead); err != nil {
		t.Fatal(err)
	}
	if apiKeyReadResponse.Code != http.StatusOK || !apiKeyRead.Success || len(apiKeyRead.Data.Items) != 1 ||
		apiKeyRead.Data.Items[0].SourceAccessible || apiKeyRead.Data.Items[0].SourceSessionID != nil ||
		apiKeyRead.Data.Items[0].SourceTurnID != nil {
		t.Fatalf("API key task conclusion leaked private source: status=%d response=%+v",
			apiKeyReadResponse.Code, apiKeyRead)
	}
	apiKeyRouter := chi.NewRouter()
	apiKeyRouter.Use(httpmw.Authn(tokens, projectChatTestAPIKeyAuth{userID: userID}))
	apiKeyRouter.Use(httpmw.RequireJWTAuth())
	apiKeyRouter.Post("/projects/{projectId}/chat-sessions", h.CreateSession)
	encoded, _ := json.Marshal(body)
	apiKeyRequest := httptest.NewRequest(http.MethodPost,
		"/projects/"+projectID.String()+"/chat-sessions", bytes.NewReader(encoded))
	apiKeyRequest.Header.Set("Authorization", "ApiKey project-chat-test-key")
	apiKeyRequest.Header.Set("Idempotency-Key", "api-key-forbidden")
	apiKeyResponse := httptest.NewRecorder()
	apiKeyRouter.ServeHTTP(apiKeyResponse, apiKeyRequest)
	if apiKeyResponse.Code != http.StatusForbidden {
		t.Fatalf("project chat accepted API key authentication: status=%d body=%s",
			apiKeyResponse.Code, apiKeyResponse.Body.String())
	}
	_ = memberID
}

type projectChatTestAPIKeyAuth struct{ userID uuid.UUID }

func (f projectChatTestAPIKeyAuth) Authenticate(context.Context, string) (*apikeydom.APIKey, error) {
	return &apikeydom.APIKey{ID: uuid.New(), UserID: f.userID, Name: "test"}, nil
}

type createProjectChatHTTPEnvelope struct {
	Success   bool   `json:"success"`
	ErrorCode string `json:"error_code"`
	Data      struct {
		Bundle   dto.ProjectChatTurnBundleResponse `json:"bundle"`
		Replayed bool                              `json:"replayed"`
	} `json:"data"`
}

type prepareProjectConclusionHTTPEnvelope struct {
	Success   bool   `json:"success"`
	ErrorCode string `json:"error_code"`
	Data      struct {
		Preparation dto.ConclusionPreparationResponse `json:"preparation"`
		Replayed    bool                              `json:"replayed"`
	} `json:"data"`
}

type confirmProjectConclusionHTTPEnvelope struct {
	Success   bool   `json:"success"`
	ErrorCode string `json:"error_code"`
	Data      struct {
		Publication dto.ConclusionPublicationResponse `json:"publication"`
		Replayed    bool                              `json:"replayed"`
	} `json:"data"`
}

type stopProjectChatHTTPEnvelope struct {
	Success   bool                              `json:"success"`
	ErrorCode string                            `json:"error_code"`
	Data      dto.ProjectChatTurnResultResponse `json:"data"`
}

func finalizeProjectChatHTTPTurn(t *testing.T, ctx context.Context, repo *postgres.AgentTurnRepository, bundle dto.ProjectChatTurnBundleResponse, agentID uuid.UUID) {
	t.Helper()
	claim, err := repo.ClaimTurnRun(ctx, agentdom.ClaimTurnRunInput{
		TurnID: bundle.Turn.ID, WorkerID: "project-chat-http-worker", LeaseDuration: time.Minute,
	})
	if err != nil {
		t.Fatalf("claim HTTP turn: %v", err)
	}
	eventID := uuid.New()
	if _, err := repo.AppendTurnEvent(ctx, agentdom.AppendTurnEventInput{
		ID: eventID, TurnID: bundle.Turn.ID, RunID: claim.Bundle.Run.ID,
		ClaimToken: claim.ClaimToken, TurnSequence: 0,
		EventType: agentdom.StableOutputEventType, EventSource: "agent",
		Payload: json.RawMessage(`{"text":"frozen HTTP conclusion"}`), CreatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("append HTTP stable result: %v", err)
	}
	sequence := 0
	if _, err := repo.FinalizeTurn(ctx, agentdom.FinalizeTurnInput{
		RunID: claim.Bundle.Run.ID, ClaimToken: claim.ClaimToken,
		TerminalStatus: agentdom.TurnStatusSucceeded, StableOutputEvent: &eventID,
		GeneratedByAgentID: agentID, Disposition: agentdom.RuntimeRetired,
		FinalEventSequence: &sequence,
	}); err != nil {
		t.Fatalf("finalize HTTP turn: %v", err)
	}
}

func seedProjectChatHTTPTestScope(t *testing.T, ctx context.Context, db *sqlx.DB) (uuid.UUID, uuid.UUID, uuid.UUID, uuid.UUID, uuid.UUID) {
	t.Helper()
	projectID, userID, memberID, agentID, taskID := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	globalRoleID, projectRoleID, agentMemberID := uuid.New(), uuid.New(), uuid.New()
	statements := []struct {
		query string
		args  []any
	}{
		{`INSERT INTO global_roles (id,name,permissions) VALUES ($1,$2,'{"*":true}')`, []any{globalRoleID, "project-chat-http-" + globalRoleID.String()}},
		{`INSERT INTO users (id,username,password_hash,full_name,role_id) VALUES ($1,$2,'x','HTTP Test',$3)`, []any{userID, "project-chat-http-" + userID.String(), globalRoleID}},
		{`INSERT INTO projects (id,name,task_id_prefix,created_by) VALUES ($1,'HTTP Test','HTTP',$2)`, []any{projectID, userID}},
		{`INSERT INTO project_roles (id,project_id,role_name,permissions) VALUES ($1,$2,'owner','{"*":true}')`, []any{projectRoleID, projectID}},
		{`INSERT INTO project_members (id,project_id,user_id,project_role_id,member_type) VALUES ($1,$2,$3,$4,'human')`, []any{memberID, projectID, userID, projectRoleID}},
		{`INSERT INTO agents (id,project_id,name,handle,llm_provider,llm_model,llm_api_key_secret,llm_base_url,created_by) VALUES ($1,$2,'HTTP Agent',$3,'openai','test','secret','',$4)`, []any{agentID, projectID, "http-agent-" + agentID.String(), userID}},
		{`INSERT INTO project_members (id,project_id,user_id,project_role_id,member_type,agent_id) VALUES ($1,$2,NULL,$3,'agent',$4)`, []any{agentMemberID, projectID, projectRoleID, agentID}},
		{`INSERT INTO tasks (id,project_id,task_number,title,reporter_id) VALUES ($1,$2,1,'HTTP task',$3)`, []any{taskID, projectID, memberID}},
	}
	for _, statement := range statements {
		if _, err := db.ExecContext(ctx, statement.query, statement.args...); err != nil {
			t.Fatalf("seed project chat HTTP scope: %v", err)
		}
	}
	return projectID, userID, memberID, agentID, taskID
}

func seedProjectChatHTTPOtherMember(t *testing.T, ctx context.Context, db *sqlx.DB, projectID uuid.UUID) uuid.UUID {
	t.Helper()
	globalRoleID, userID, memberID := uuid.New(), uuid.New(), uuid.New()
	if _, err := db.ExecContext(ctx, `INSERT INTO global_roles (id,name,permissions) VALUES ($1,$2,'{"*":true}')`,
		globalRoleID, "project-chat-http-other-"+globalRoleID.String()); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO users (id,username,password_hash,full_name,role_id)
		VALUES ($1,$2,'x','HTTP Other',$3)`, userID, "project-chat-http-other-"+userID.String(), globalRoleID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO project_members
		(id,project_id,user_id,project_role_id,member_type)
		SELECT $1,$2,$3,id,'human' FROM project_roles WHERE project_id=$2 ORDER BY created_at LIMIT 1`,
		memberID, projectID, userID); err != nil {
		t.Fatal(err)
	}
	return userID
}
