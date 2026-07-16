package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	agentdom "github.com/Paca-AI/api/internal/domain/agent"
	domainauth "github.com/Paca-AI/api/internal/domain/auth"
	projectdom "github.com/Paca-AI/api/internal/domain/project"
	"github.com/Paca-AI/api/internal/transport/http/handler"
	httpmw "github.com/Paca-AI/api/internal/transport/http/middleware"
	"github.com/go-chi/chi/v5"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

// ---------------------------------------------------------------------------
// Minimal fake
// ---------------------------------------------------------------------------

type mockAgentSvc struct {
	getAgent         func(ctx context.Context, projectID, agentID uuid.UUID) (*agentdom.Agent, error)
	createAgent      func(ctx context.Context, projectID uuid.UUID, in agentdom.CreateAgentInput) (*agentdom.Agent, error)
	startChatSession func(ctx context.Context, projectID, agentID, memberID uuid.UUID, message string) (*agentdom.AgentChatSession, *agentdom.AgentConversation, error)
}

func (m *mockAgentSvc) ListAgents(_ context.Context, _ uuid.UUID) ([]*agentdom.Agent, error) {
	return nil, nil
}
func (m *mockAgentSvc) GetAgent(ctx context.Context, projectID, agentID uuid.UUID) (*agentdom.Agent, error) {
	if m.getAgent != nil {
		return m.getAgent(ctx, projectID, agentID)
	}
	return nil, errors.New("mock: GetAgent not configured")
}
func (m *mockAgentSvc) CreateAgent(ctx context.Context, projectID uuid.UUID, in agentdom.CreateAgentInput) (*agentdom.Agent, error) {
	if m.createAgent != nil {
		return m.createAgent(ctx, projectID, in)
	}
	return nil, errors.New("mock: CreateAgent not configured")
}
func (m *mockAgentSvc) UpdateAgent(_ context.Context, _, _ uuid.UUID, _ agentdom.UpdateAgentInput) (*agentdom.Agent, error) {
	return nil, agentdom.ErrAgentNotFound
}
func (m *mockAgentSvc) GenerateACPBridgeToken(_ context.Context, _, _ uuid.UUID) (string, error) {
	return "", nil
}
func (m *mockAgentSvc) DeleteAgent(_ context.Context, _, _ uuid.UUID) error {
	return agentdom.ErrAgentNotFound
}
func (m *mockAgentSvc) TriggerDescriptionWrite(_ context.Context, _, _, _, _ uuid.UUID) (*agentdom.AgentConversation, error) {
	return nil, agentdom.ErrAgentNotFound
}
func (m *mockAgentSvc) ListMCPServers(_ context.Context, _ uuid.UUID) ([]*agentdom.AgentMCPServer, error) {
	return nil, nil
}
func (m *mockAgentSvc) AddMCPServer(_ context.Context, _ uuid.UUID, _ agentdom.AddMCPServerInput) (*agentdom.AgentMCPServer, error) {
	return &agentdom.AgentMCPServer{ID: uuid.New()}, nil
}
func (m *mockAgentSvc) UpdateMCPServer(_ context.Context, _, _ uuid.UUID, _ agentdom.UpdateMCPServerInput) (*agentdom.AgentMCPServer, error) {
	return nil, errors.New("not found")
}
func (m *mockAgentSvc) DeleteMCPServer(_ context.Context, _, _ uuid.UUID) error { return nil }
func (m *mockAgentSvc) ListSkills(_ context.Context, _ uuid.UUID) ([]*agentdom.AgentSkill, error) {
	return nil, nil
}
func (m *mockAgentSvc) AddSkill(_ context.Context, _ uuid.UUID, _ agentdom.AddSkillInput) (*agentdom.AgentSkill, error) {
	return &agentdom.AgentSkill{ID: uuid.New()}, nil
}
func (m *mockAgentSvc) UpdateSkill(_ context.Context, _, _ uuid.UUID, _ agentdom.UpdateSkillInput) (*agentdom.AgentSkill, error) {
	return nil, errors.New("not found")
}
func (m *mockAgentSvc) DeleteSkill(_ context.Context, _, _ uuid.UUID) error { return nil }
func (m *mockAgentSvc) ListEnvVars(_ context.Context, _ uuid.UUID) ([]*agentdom.AgentEnvironmentVariable, error) {
	return nil, nil
}
func (m *mockAgentSvc) AddEnvVar(_ context.Context, _ uuid.UUID, _ agentdom.AddEnvVarInput) (*agentdom.AgentEnvironmentVariable, error) {
	return &agentdom.AgentEnvironmentVariable{ID: uuid.New()}, nil
}
func (m *mockAgentSvc) UpdateEnvVar(_ context.Context, _, _ uuid.UUID, _ agentdom.UpdateEnvVarInput) (*agentdom.AgentEnvironmentVariable, error) {
	return nil, errors.New("not found")
}
func (m *mockAgentSvc) DeleteEnvVar(_ context.Context, _, _ uuid.UUID) error { return nil }
func (m *mockAgentSvc) ListConversations(_ context.Context, _ agentdom.ListConversationsFilter) ([]*agentdom.AgentConversation, int64, error) {
	return nil, 0, nil
}
func (m *mockAgentSvc) GetConversation(_ context.Context, _, _ uuid.UUID) (*agentdom.AgentConversation, error) {
	return nil, errors.New("not found")
}
func (m *mockAgentSvc) ListConversationEvents(_ context.Context, _ uuid.UUID, _, _ int) ([]*agentdom.AgentConversationEvent, int64, error) {
	return nil, 0, nil
}
func (m *mockAgentSvc) StopConversation(_ context.Context, _, _ uuid.UUID) error  { return nil }
func (m *mockAgentSvc) PauseConversation(_ context.Context, _, _ uuid.UUID) error { return nil }
func (m *mockAgentSvc) Heartbeat(_ context.Context, _, _ uuid.UUID) error         { return nil }
func (m *mockAgentSvc) SendConversationMessage(_ context.Context, _, _ uuid.UUID, _ string, _ uuid.UUID) error {
	return nil
}
func (m *mockAgentSvc) ListChatSessions(_ context.Context, _, _, _ uuid.UUID) ([]*agentdom.AgentChatSession, error) {
	return nil, nil
}
func (m *mockAgentSvc) StartChatSession(ctx context.Context, projectID, agentID, memberID uuid.UUID, message string) (*agentdom.AgentChatSession, *agentdom.AgentConversation, error) {
	if m.startChatSession != nil {
		return m.startChatSession(ctx, projectID, agentID, memberID, message)
	}
	return &agentdom.AgentChatSession{ID: uuid.New()}, &agentdom.AgentConversation{ID: uuid.New()}, nil
}
func (m *mockAgentSvc) SendChatMessage(_ context.Context, _, _, _ uuid.UUID, _ string) (*agentdom.AgentConversation, error) {
	return &agentdom.AgentConversation{ID: uuid.New()}, nil
}
func (m *mockAgentSvc) ListChatMessages(_ context.Context, _ uuid.UUID, _, _ int) ([]*agentdom.AgentConversationEvent, int64, error) {
	return nil, 0, nil
}

var _ agentdom.Service = (*mockAgentSvc)(nil)

// ---------------------------------------------------------------------------
// Router helpers
// ---------------------------------------------------------------------------

func newAgentRouter(svc agentdom.Service) chi.Router {
	h := handler.NewAgentHandler(svc, "", "", "")
	r := chi.NewRouter()
	r.Route("/projects/{projectId}", func(r chi.Router) {
		r.Post("/agents", h.CreateAgent)
		r.Route("/agents/{agentId}", func(r chi.Router) {
			r.Post("/mcp-servers", h.AddMCPServer)
			r.Post("/skills", h.AddSkill)
			r.Post("/chat-sessions", h.StartChatSession)
		})
		r.Route("/tasks/{taskId}", func(r chi.Router) {
			r.Post("/write-with-ai", h.WriteTaskDescriptionWithAI)
		})
	})
	r.Post("/projects/{projectId}/agents/{agentId}/chat-sessions/{sessionId}/messages", h.SendChatMessage)
	return r
}

// fakeMemberRepo implements projectdom.MemberRepository, letting tests
// control FindMemberByUserProject — the only method resolveMemberID calls.
// Every other method panics if invoked, since resolveMemberID tests should
// never reach them.
type fakeMemberRepo struct {
	findByUserProject func(ctx context.Context, userID, projectID uuid.UUID) (*projectdom.ProjectMember, error)
}

func (f *fakeMemberRepo) ListMembers(context.Context, uuid.UUID) ([]*projectdom.ProjectMember, error) {
	panic("fakeMemberRepo: ListMembers not used by resolveMemberID tests")
}
func (f *fakeMemberRepo) FindMember(context.Context, uuid.UUID, uuid.UUID) (*projectdom.ProjectMember, error) {
	panic("fakeMemberRepo: FindMember not used by resolveMemberID tests")
}
func (f *fakeMemberRepo) FindMemberByAgent(context.Context, uuid.UUID, uuid.UUID) (*projectdom.ProjectMember, error) {
	panic("fakeMemberRepo: FindMemberByAgent not used by resolveMemberID tests")
}
func (f *fakeMemberRepo) FindMemberByUserProject(ctx context.Context, userID, projectID uuid.UUID) (*projectdom.ProjectMember, error) {
	return f.findByUserProject(ctx, userID, projectID)
}
func (f *fakeMemberRepo) FindMemberByActor(context.Context, uuid.UUID, uuid.UUID, *uuid.UUID) (*projectdom.ProjectMember, error) {
	panic("fakeMemberRepo: FindMemberByActor not used by resolveMemberID tests")
}
func (f *fakeMemberRepo) FindMemberByID(context.Context, uuid.UUID) (*projectdom.ProjectMember, error) {
	panic("fakeMemberRepo: FindMemberByID not used by resolveMemberID tests")
}
func (f *fakeMemberRepo) AddMember(context.Context, *projectdom.ProjectMember) error {
	panic("fakeMemberRepo: AddMember not used by resolveMemberID tests")
}
func (f *fakeMemberRepo) UpdateMemberRole(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) error {
	panic("fakeMemberRepo: UpdateMemberRole not used by resolveMemberID tests")
}
func (f *fakeMemberRepo) RemoveMember(context.Context, uuid.UUID, uuid.UUID) error {
	panic("fakeMemberRepo: RemoveMember not used by resolveMemberID tests")
}
func (f *fakeMemberRepo) UpdateMemberRoleByMemberID(context.Context, uuid.UUID, uuid.UUID) error {
	panic("fakeMemberRepo: UpdateMemberRoleByMemberID not used by resolveMemberID tests")
}
func (f *fakeMemberRepo) RemoveMemberByMemberID(context.Context, uuid.UUID) error {
	panic("fakeMemberRepo: RemoveMemberByMemberID not used by resolveMemberID tests")
}
func (f *fakeMemberRepo) AddAgentMember(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, uuid.UUID) error {
	panic("fakeMemberRepo: AddAgentMember not used by resolveMemberID tests")
}
func (f *fakeMemberRepo) RemoveAgentMember(context.Context, uuid.UUID, uuid.UUID) error {
	panic("fakeMemberRepo: RemoveAgentMember not used by resolveMemberID tests")
}

var _ projectdom.MemberRepository = (*fakeMemberRepo)(nil)

// claimsMiddleware injects a synthetic access-token claims with the given
// subject, so resolveMemberID has something to parse.
func claimsMiddleware(subject string) func(http.Handler) http.Handler {
	claims := &domainauth.Claims{
		RegisteredClaims: jwt.RegisteredClaims{Subject: subject},
		Kind:             "access",
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := context.WithValue(r.Context(), httpmw.ClaimsContextKey(), claims)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// newAgentRouterWithMemberRepo mirrors newAgentRouter but additionally wires
// a member repo (nil leaves it unset, like NewAgentHandler's zero value) and
// injects claims with the given subject, so resolveMemberID's branches can
// be exercised end-to-end through the HTTP layer.
func newAgentRouterWithMemberRepo(svc agentdom.Service, memberRepo projectdom.MemberRepository, subject string) chi.Router {
	h := handler.NewAgentHandler(svc, "", "", "")
	if memberRepo != nil {
		h = h.WithMemberRepo(memberRepo)
	}
	r := chi.NewRouter()
	r.Use(claimsMiddleware(subject))
	r.Route("/projects/{projectId}/agents/{agentId}", func(r chi.Router) {
		r.Post("/chat-sessions", h.StartChatSession)
	})
	return r
}

func doAgentRequest(t *testing.T, r chi.Router, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var buf *bytes.Buffer
	if body != nil {
		b, _ := json.Marshal(body)
		buf = bytes.NewBuffer(b)
	} else {
		buf = bytes.NewBuffer(nil)
	}
	req := httptest.NewRequestWithContext(context.Background(), method, path, buf)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// validCreateAgentBody returns a body with all required fields filled in.
func validCreateAgentBody(overrides map[string]any) map[string]any {
	base := map[string]any{
		"name":            "Test Agent",
		"handle":          "test-agent",
		"llm_provider":    "openai",
		"llm_model":       "gpt-4",
		"llm_api_key":     "sk-test",
		"llm_base_url":    "https://api.openai.com/v1",
		"project_role_id": uuid.New(),
	}
	for k, v := range overrides {
		base[k] = v
	}
	return base
}

// ---------------------------------------------------------------------------
// CreateAgent validation tests
// ---------------------------------------------------------------------------

func TestCreateAgent_MissingName_Returns400(t *testing.T) {
	r := newAgentRouter(&mockAgentSvc{})
	projectID := uuid.New()
	w := doAgentRequest(t, r, http.MethodPost,
		"/projects/"+projectID.String()+"/agents",
		validCreateAgentBody(map[string]any{"name": ""}))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing name, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCreateAgent_MissingHandle_Returns400(t *testing.T) {
	r := newAgentRouter(&mockAgentSvc{})
	projectID := uuid.New()
	w := doAgentRequest(t, r, http.MethodPost,
		"/projects/"+projectID.String()+"/agents",
		validCreateAgentBody(map[string]any{"handle": ""}))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing handle, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCreateAgent_MissingLLMProvider_Returns400(t *testing.T) {
	r := newAgentRouter(&mockAgentSvc{})
	projectID := uuid.New()
	w := doAgentRequest(t, r, http.MethodPost,
		"/projects/"+projectID.String()+"/agents",
		validCreateAgentBody(map[string]any{"llm_provider": ""}))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing llm_provider, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCreateAgent_MissingLLMModel_Returns400(t *testing.T) {
	r := newAgentRouter(&mockAgentSvc{})
	projectID := uuid.New()
	w := doAgentRequest(t, r, http.MethodPost,
		"/projects/"+projectID.String()+"/agents",
		validCreateAgentBody(map[string]any{"llm_model": ""}))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing llm_model, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCreateAgent_MissingLLMAPIKey_Returns400(t *testing.T) {
	r := newAgentRouter(&mockAgentSvc{})
	projectID := uuid.New()
	w := doAgentRequest(t, r, http.MethodPost,
		"/projects/"+projectID.String()+"/agents",
		validCreateAgentBody(map[string]any{"llm_api_key": ""}))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing llm_api_key, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCreateAgent_MissingLLMBaseURL_Returns400(t *testing.T) {
	r := newAgentRouter(&mockAgentSvc{})
	projectID := uuid.New()
	w := doAgentRequest(t, r, http.MethodPost,
		"/projects/"+projectID.String()+"/agents",
		validCreateAgentBody(map[string]any{"llm_base_url": ""}))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing llm_base_url, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCreateAgent_MissingProjectRoleID_Returns400(t *testing.T) {
	r := newAgentRouter(&mockAgentSvc{})
	projectID := uuid.New()
	body := validCreateAgentBody(nil)
	delete(body, "project_role_id")
	w := doAgentRequest(t, r, http.MethodPost,
		"/projects/"+projectID.String()+"/agents", body)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing project_role_id, got %d: %s", w.Code, w.Body.String())
	}
}

// ---------------------------------------------------------------------------
// AddMCPServer validation tests
// ---------------------------------------------------------------------------

// validAgentSvc returns a mockAgentSvc whose GetAgent always succeeds.
func validAgentSvc() *mockAgentSvc {
	return &mockAgentSvc{
		getAgent: func(_ context.Context, projectID, agentID uuid.UUID) (*agentdom.Agent, error) {
			return &agentdom.Agent{ID: agentID, ProjectID: projectID}, nil
		},
	}
}

func TestAddMCPServer_MissingServerName_Returns400(t *testing.T) {
	r := newAgentRouter(validAgentSvc())
	projectID := uuid.New()
	agentID := uuid.New()

	w := doAgentRequest(t, r, http.MethodPost,
		"/projects/"+projectID.String()+"/agents/"+agentID.String()+"/mcp-servers",
		map[string]any{"server_name": "", "transport": "stdio"})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing server_name, got %d: %s", w.Code, w.Body.String())
	}
}

func TestAddMCPServer_InvalidTransport_Returns400(t *testing.T) {
	r := newAgentRouter(validAgentSvc())
	projectID := uuid.New()
	agentID := uuid.New()

	w := doAgentRequest(t, r, http.MethodPost,
		"/projects/"+projectID.String()+"/agents/"+agentID.String()+"/mcp-servers",
		map[string]any{"server_name": "my-server", "transport": "websocket"})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid transport, got %d: %s", w.Code, w.Body.String())
	}
}

// ---------------------------------------------------------------------------
// AddSkill validation tests
// ---------------------------------------------------------------------------

func TestAddSkill_MissingSkillName_Returns400(t *testing.T) {
	r := newAgentRouter(validAgentSvc())
	projectID := uuid.New()
	agentID := uuid.New()

	w := doAgentRequest(t, r, http.MethodPost,
		"/projects/"+projectID.String()+"/agents/"+agentID.String()+"/skills",
		map[string]any{"skill_name": "", "skill_source": "inline"})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing skill_name, got %d: %s", w.Code, w.Body.String())
	}
}

func TestAddSkill_InvalidSkillSource_Returns400(t *testing.T) {
	r := newAgentRouter(validAgentSvc())
	projectID := uuid.New()
	agentID := uuid.New()

	w := doAgentRequest(t, r, http.MethodPost,
		"/projects/"+projectID.String()+"/agents/"+agentID.String()+"/skills",
		map[string]any{"skill_name": "my-skill", "skill_source": "unknown"})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid skill_source, got %d: %s", w.Code, w.Body.String())
	}
}

// ---------------------------------------------------------------------------
// Chat session validation tests
// ---------------------------------------------------------------------------

func TestStartChatSession_EmptyMessage_Returns400(t *testing.T) {
	r := newAgentRouter(&mockAgentSvc{})
	projectID := uuid.New()
	agentID := uuid.New()

	w := doAgentRequest(t, r, http.MethodPost,
		"/projects/"+projectID.String()+"/agents/"+agentID.String()+"/chat-sessions",
		map[string]any{"message": ""})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for empty message, got %d: %s", w.Code, w.Body.String())
	}
}

func TestSendChatMessage_EmptyMessage_Returns400(t *testing.T) {
	r := newAgentRouter(&mockAgentSvc{})
	projectID := uuid.New()
	agentID := uuid.New()
	sessionID := uuid.New()

	w := doAgentRequest(t, r, http.MethodPost,
		"/projects/"+projectID.String()+"/agents/"+agentID.String()+"/chat-sessions/"+sessionID.String()+"/messages",
		map[string]any{"message": ""})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for empty message, got %d: %s", w.Code, w.Body.String())
	}
}

// ---------------------------------------------------------------------------
// WriteTaskDescriptionWithAI validation test
// ---------------------------------------------------------------------------

func TestWriteTaskDescriptionWithAI_MissingAgentID_Returns400(t *testing.T) {
	r := newAgentRouter(&mockAgentSvc{})
	projectID := uuid.New()
	taskID := uuid.New()

	// agent_id absent → decodes to uuid.Nil → handler returns 400
	w := doAgentRequest(t, r, http.MethodPost,
		"/projects/"+projectID.String()+"/tasks/"+taskID.String()+"/write-with-ai",
		map[string]any{})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing agent_id, got %d: %s", w.Code, w.Body.String())
	}
}

// ---------------------------------------------------------------------------
// resolveMemberID tests — exercised through StartChatSession, one of its
// four callers, since resolveMemberID itself is unexported.
// ---------------------------------------------------------------------------

func TestResolveMemberID_NoMemberRepoConfigured_Returns500(t *testing.T) {
	r := newAgentRouterWithMemberRepo(&mockAgentSvc{}, nil, uuid.New().String())
	projectID := uuid.New()
	agentID := uuid.New()

	w := doAgentRequest(t, r, http.MethodPost,
		"/projects/"+projectID.String()+"/agents/"+agentID.String()+"/chat-sessions",
		map[string]any{"message": "hello"})
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 when no member repo is configured, got %d: %s", w.Code, w.Body.String())
	}
}

func TestResolveMemberID_InvalidSubjectClaim_Returns400(t *testing.T) {
	memberRepo := &fakeMemberRepo{
		findByUserProject: func(context.Context, uuid.UUID, uuid.UUID) (*projectdom.ProjectMember, error) {
			t.Fatal("FindMemberByUserProject should not be called when the subject claim itself is unparsable")
			return nil, nil
		},
	}
	r := newAgentRouterWithMemberRepo(&mockAgentSvc{}, memberRepo, "not-a-uuid")
	projectID := uuid.New()
	agentID := uuid.New()

	w := doAgentRequest(t, r, http.MethodPost,
		"/projects/"+projectID.String()+"/agents/"+agentID.String()+"/chat-sessions",
		map[string]any{"message": "hello"})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for an unparsable subject claim, got %d: %s", w.Code, w.Body.String())
	}
}

func TestResolveMemberID_LookupFails_PropagatesError(t *testing.T) {
	memberRepo := &fakeMemberRepo{
		findByUserProject: func(context.Context, uuid.UUID, uuid.UUID) (*projectdom.ProjectMember, error) {
			return nil, errors.New("db unavailable")
		},
	}
	r := newAgentRouterWithMemberRepo(&mockAgentSvc{}, memberRepo, uuid.New().String())
	projectID := uuid.New()
	agentID := uuid.New()

	w := doAgentRequest(t, r, http.MethodPost,
		"/projects/"+projectID.String()+"/agents/"+agentID.String()+"/chat-sessions",
		map[string]any{"message": "hello"})
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected the lookup error to surface as a 500, got %d: %s", w.Code, w.Body.String())
	}
}

func TestResolveMemberID_Resolved_PassesMemberIDToService(t *testing.T) {
	userID := uuid.New()
	resolvedMemberID := uuid.New()
	var gotMemberID uuid.UUID
	memberRepo := &fakeMemberRepo{
		findByUserProject: func(_ context.Context, gotUserID, _ uuid.UUID) (*projectdom.ProjectMember, error) {
			if gotUserID != userID {
				t.Fatalf("expected lookup for user %v, got %v", userID, gotUserID)
			}
			return &projectdom.ProjectMember{ID: resolvedMemberID}, nil
		},
	}
	svc := &mockAgentSvc{
		startChatSession: func(_ context.Context, _, _, memberID uuid.UUID, _ string) (*agentdom.AgentChatSession, *agentdom.AgentConversation, error) {
			gotMemberID = memberID
			return &agentdom.AgentChatSession{ID: uuid.New()}, &agentdom.AgentConversation{ID: uuid.New()}, nil
		},
	}
	r := newAgentRouterWithMemberRepo(svc, memberRepo, userID.String())
	projectID := uuid.New()
	agentID := uuid.New()

	w := doAgentRequest(t, r, http.MethodPost,
		"/projects/"+projectID.String()+"/agents/"+agentID.String()+"/chat-sessions",
		map[string]any{"message": "hello"})
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	if gotMemberID != resolvedMemberID {
		t.Fatalf("expected resolved member ID %v to reach the service, got %v", resolvedMemberID, gotMemberID)
	}
}
