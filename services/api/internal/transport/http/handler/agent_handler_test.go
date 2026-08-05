package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	agentdom "github.com/Paca-AI/api/internal/domain/agent"
	domainauth "github.com/Paca-AI/api/internal/domain/auth"
	projectdom "github.com/Paca-AI/api/internal/domain/project"
	"github.com/Paca-AI/api/internal/transport/http/handler"
	httpmw "github.com/Paca-AI/api/internal/transport/http/middleware"
)

// ---------------------------------------------------------------------------
// Minimal fake
// ---------------------------------------------------------------------------

type mockAgentSvc struct {
	getAgent                     func(ctx context.Context, projectID, agentID uuid.UUID) (*agentdom.Agent, error)
	createAgent                  func(ctx context.Context, projectID uuid.UUID, in agentdom.CreateAgentInput) (*agentdom.Agent, error)
	startChatSession             func(ctx context.Context, projectID, agentID, memberID uuid.UUID, message string) (*agentdom.AgentChatSession, *agentdom.AgentConversation, error)
	listConversations            func(ctx context.Context, filter agentdom.ListConversationsFilter, limit int) ([]*agentdom.AgentConversation, bool, error)
	listConversationEvents       func(ctx context.Context, conversationID uuid.UUID, offset, limit int) ([]*agentdom.AgentConversationEvent, int64, error)
	listAgentActivities          func(ctx context.Context, filter agentdom.ListAgentActivitiesFilter, limit int) ([]*agentdom.ActivityFeedItem, bool, error)
	getGlobalConversation        func(ctx context.Context, conversationID uuid.UUID) (*agentdom.AgentConversation, error)
	listGlobalConversations      func(ctx context.Context, actorUserID uuid.UUID, filter agentdom.ListConversationsFilter, limit int) ([]*agentdom.AgentConversation, bool, error)
	getGlobalAgent               func(ctx context.Context, agentID uuid.UUID) (*agentdom.Agent, error)
	generateGlobalACPBridgeToken func(ctx context.Context, agentID uuid.UUID) (string, error)
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
func (m *mockAgentSvc) ListConversations(ctx context.Context, filter agentdom.ListConversationsFilter, limit int) ([]*agentdom.AgentConversation, bool, error) {
	if m.listConversations != nil {
		return m.listConversations(ctx, filter, limit)
	}
	return nil, false, nil
}
func (m *mockAgentSvc) ListAgentActivities(ctx context.Context, filter agentdom.ListAgentActivitiesFilter, limit int) ([]*agentdom.ActivityFeedItem, bool, error) {
	if m.listAgentActivities != nil {
		return m.listAgentActivities(ctx, filter, limit)
	}
	return nil, false, nil
}
func (m *mockAgentSvc) GetConversation(_ context.Context, _, _ uuid.UUID) (*agentdom.AgentConversation, error) {
	return nil, errors.New("not found")
}
func (m *mockAgentSvc) ListConversationEvents(ctx context.Context, conversationID uuid.UUID, offset, limit int) ([]*agentdom.AgentConversationEvent, int64, error) {
	if m.listConversationEvents != nil {
		return m.listConversationEvents(ctx, conversationID, offset, limit)
	}
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

// --- Global agents / global chat (see agentdom.Service) --------------------

func (m *mockAgentSvc) ListGlobalAgents(_ context.Context) ([]*agentdom.Agent, error) {
	return nil, nil
}
func (m *mockAgentSvc) GetGlobalAgent(ctx context.Context, agentID uuid.UUID) (*agentdom.Agent, error) {
	if m.getGlobalAgent != nil {
		return m.getGlobalAgent(ctx, agentID)
	}
	return nil, nil
}
func (m *mockAgentSvc) CreateGlobalAgent(_ context.Context, _ agentdom.CreateGlobalAgentInput) (*agentdom.Agent, error) {
	return nil, nil
}
func (m *mockAgentSvc) UpdateGlobalAgent(_ context.Context, _ uuid.UUID, _ agentdom.UpdateAgentInput) (*agentdom.Agent, error) {
	return nil, nil
}
func (m *mockAgentSvc) DeleteGlobalAgent(_ context.Context, _ uuid.UUID) error { return nil }
func (m *mockAgentSvc) ListInvitedProjectIDs(_ context.Context, _ uuid.UUID) ([]uuid.UUID, error) {
	return nil, nil
}
func (m *mockAgentSvc) GenerateGlobalACPBridgeToken(ctx context.Context, agentID uuid.UUID) (string, error) {
	if m.generateGlobalACPBridgeToken != nil {
		return m.generateGlobalACPBridgeToken(ctx, agentID)
	}
	return "", nil
}
func (m *mockAgentSvc) ListGlobalConversations(ctx context.Context, actorUserID uuid.UUID, filter agentdom.ListConversationsFilter, limit int) ([]*agentdom.AgentConversation, bool, error) {
	if m.listGlobalConversations != nil {
		return m.listGlobalConversations(ctx, actorUserID, filter, limit)
	}
	return nil, false, nil
}
func (m *mockAgentSvc) GetGlobalConversation(ctx context.Context, conversationID uuid.UUID) (*agentdom.AgentConversation, error) {
	if m.getGlobalConversation != nil {
		return m.getGlobalConversation(ctx, conversationID)
	}
	return nil, nil
}
func (m *mockAgentSvc) StopGlobalConversation(_ context.Context, _ uuid.UUID) error  { return nil }
func (m *mockAgentSvc) PauseGlobalConversation(_ context.Context, _ uuid.UUID) error { return nil }
func (m *mockAgentSvc) GlobalHeartbeat(_ context.Context, _ uuid.UUID) error         { return nil }
func (m *mockAgentSvc) SendGlobalConversationMessage(_ context.Context, _ uuid.UUID, _ string, _ uuid.UUID) error {
	return nil
}
func (m *mockAgentSvc) ListGlobalChatSessions(_ context.Context, _, _ uuid.UUID) ([]*agentdom.AgentChatSession, error) {
	return nil, nil
}
func (m *mockAgentSvc) StartGlobalChatSession(_ context.Context, _, _ uuid.UUID, _ string) (*agentdom.AgentChatSession, *agentdom.AgentConversation, error) {
	return &agentdom.AgentChatSession{ID: uuid.New()}, &agentdom.AgentConversation{ID: uuid.New()}, nil
}
func (m *mockAgentSvc) SendGlobalChatMessage(_ context.Context, _, _ uuid.UUID, _ string) (*agentdom.AgentConversation, error) {
	return &agentdom.AgentConversation{ID: uuid.New()}, nil
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
			r.Get("/acp-bridge-status", h.GetACPBridgeStatus)
		})
		r.Route("/tasks/{taskId}", func(r chi.Router) {
			r.Post("/write-with-ai", h.WriteTaskDescriptionWithAI)
		})
	})
	r.Post("/projects/{projectId}/agents/{agentId}/chat-sessions/{sessionId}/messages", h.SendChatMessage)
	return r
}

func newGlobalAgentAcpRouter(svc agentdom.Service) chi.Router {
	h := handler.NewAgentHandler(svc, "", "", "")
	r := chi.NewRouter()
	r.Route("/admin/agents/{agentId}", func(r chi.Router) {
		r.Post("/acp-bridge-token", h.GenerateGlobalACPBridgeToken)
		r.Get("/acp-bridge-status", h.GetGlobalACPBridgeStatus)
	})
	return r
}

// fakeMemberRepo implements projectdom.MemberRepository, letting tests
// control FindMemberByUserProject (used by resolveMemberID) and
// FindMemberByAgent (used by ListAgentActivities). Every other method
// panics if invoked, since those tests should never reach them.
type fakeMemberRepo struct {
	findByUserProject func(ctx context.Context, userID, projectID uuid.UUID) (*projectdom.ProjectMember, error)
	findByAgent       func(ctx context.Context, projectID, agentID uuid.UUID) (*projectdom.ProjectMember, error)
}

func (f *fakeMemberRepo) ListMembers(context.Context, uuid.UUID) ([]*projectdom.ProjectMember, error) {
	panic("fakeMemberRepo: ListMembers not used by resolveMemberID tests")
}
func (f *fakeMemberRepo) FindMember(context.Context, uuid.UUID, uuid.UUID) (*projectdom.ProjectMember, error) {
	panic("fakeMemberRepo: FindMember not used by resolveMemberID tests")
}
func (f *fakeMemberRepo) FindMemberByAgent(ctx context.Context, projectID, agentID uuid.UUID) (*projectdom.ProjectMember, error) {
	if f.findByAgent != nil {
		return f.findByAgent(ctx, projectID, agentID)
	}
	panic("fakeMemberRepo: FindMemberByAgent not configured")
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
		r.Get("/activities", h.ListAgentActivities)
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

func TestCreateAgent_EmptyLLMBaseURL_Allowed(t *testing.T) {
	// llm_base_url is NOT required: the agents.llm_base_url column defaults
	// to '' and several LLM providers resolve their own default base URL, so
	// rejecting an empty value here would reject otherwise-valid requests.
	svc := &mockAgentSvc{
		createAgent: func(_ context.Context, projectID uuid.UUID, in agentdom.CreateAgentInput) (*agentdom.Agent, error) {
			return &agentdom.Agent{
				ID:         uuid.New(),
				ProjectID:  projectID,
				Name:       in.Name,
				Handle:     in.Handle,
				AgentType:  agentdom.AgentTypeLLM,
				LLMBaseURL: in.LLMBaseURL,
			}, nil
		},
	}
	h := handler.NewAgentHandler(svc, "", "", "")
	r := chi.NewRouter()
	r.Use(claimsMiddleware(uuid.New().String()))
	r.Post("/projects/{projectId}/agents", h.CreateAgent)
	projectID := uuid.New()
	w := doAgentRequest(t, r, http.MethodPost,
		"/projects/"+projectID.String()+"/agents",
		validCreateAgentBody(map[string]any{"llm_base_url": ""}))
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201 for empty llm_base_url, got %d: %s", w.Code, w.Body.String())
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

// ---------------------------------------------------------------------------
// GetACPBridgeStatus validation tests
// ---------------------------------------------------------------------------

func TestGetACPBridgeStatus_NonACPAgent_Returns400(t *testing.T) {
	r := newAgentRouter(validAgentSvc()) // validAgentSvc's agent has the zero-value (LLM) AgentType
	projectID := uuid.New()
	agentID := uuid.New()
	w := doAgentRequest(t, r, http.MethodGet,
		"/projects/"+projectID.String()+"/agents/"+agentID.String()+"/acp-bridge-status", nil)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for non-ACP agent, got %d: %s", w.Code, w.Body.String())
	}
}

func TestGetACPBridgeStatus_WrongProject_Returns404(t *testing.T) {
	svc := &mockAgentSvc{
		getAgent: func(_ context.Context, _, _ uuid.UUID) (*agentdom.Agent, error) {
			return nil, agentdom.ErrAgentNotFound
		},
	}
	r := newAgentRouter(svc)
	projectID := uuid.New()
	agentID := uuid.New()
	w := doAgentRequest(t, r, http.MethodGet,
		"/projects/"+projectID.String()+"/agents/"+agentID.String()+"/acp-bridge-status", nil)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for wrong-project agent, got %d: %s", w.Code, w.Body.String())
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

// ---------------------------------------------------------------------------
// ListAgentActivities tests
// ---------------------------------------------------------------------------

func TestListAgentActivities_NoMemberRepoConfigured_Returns500(t *testing.T) {
	r := newAgentRouterWithMemberRepo(validAgentSvc(), nil, uuid.New().String())
	projectID := uuid.New()
	agentID := uuid.New()

	w := doAgentRequest(t, r, http.MethodGet,
		"/projects/"+projectID.String()+"/agents/"+agentID.String()+"/activities", nil)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 when no member repo is configured, got %d: %s", w.Code, w.Body.String())
	}
}

func TestListAgentActivities_WrongProject_Returns404(t *testing.T) {
	svc := &mockAgentSvc{
		getAgent: func(_ context.Context, _, _ uuid.UUID) (*agentdom.Agent, error) {
			return nil, agentdom.ErrAgentNotFound
		},
	}
	memberRepo := &fakeMemberRepo{}
	r := newAgentRouterWithMemberRepo(svc, memberRepo, uuid.New().String())
	projectID := uuid.New()
	agentID := uuid.New()

	w := doAgentRequest(t, r, http.MethodGet,
		"/projects/"+projectID.String()+"/agents/"+agentID.String()+"/activities", nil)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for wrong-project agent, got %d: %s", w.Code, w.Body.String())
	}
}

func TestListAgentActivities_MemberNotFound_Returns404(t *testing.T) {
	memberRepo := &fakeMemberRepo{
		findByAgent: func(context.Context, uuid.UUID, uuid.UUID) (*projectdom.ProjectMember, error) {
			return nil, projectdom.ErrMemberNotFound
		},
	}
	r := newAgentRouterWithMemberRepo(validAgentSvc(), memberRepo, uuid.New().String())
	projectID := uuid.New()
	agentID := uuid.New()

	w := doAgentRequest(t, r, http.MethodGet,
		"/projects/"+projectID.String()+"/agents/"+agentID.String()+"/activities", nil)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 when the agent has no project membership, got %d: %s", w.Code, w.Body.String())
	}
}

func TestListAgentActivities_InvalidType_Returns400(t *testing.T) {
	memberID := uuid.New()
	memberRepo := &fakeMemberRepo{
		findByAgent: func(context.Context, uuid.UUID, uuid.UUID) (*projectdom.ProjectMember, error) {
			return &projectdom.ProjectMember{ID: memberID}, nil
		},
	}
	r := newAgentRouterWithMemberRepo(validAgentSvc(), memberRepo, uuid.New().String())
	projectID := uuid.New()
	agentID := uuid.New()

	w := doAgentRequest(t, r, http.MethodGet,
		"/projects/"+projectID.String()+"/agents/"+agentID.String()+"/activities?type=bogus", nil)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for an invalid type filter, got %d: %s", w.Code, w.Body.String())
	}
}

func TestListAgentActivities_Success_ResolvesMemberAndForwardsFilters(t *testing.T) {
	memberID := uuid.New()
	memberRepo := &fakeMemberRepo{
		findByAgent: func(context.Context, uuid.UUID, uuid.UUID) (*projectdom.ProjectMember, error) {
			return &projectdom.ProjectMember{ID: memberID}, nil
		},
	}
	var gotFilter agentdom.ListAgentActivitiesFilter
	svc := validAgentSvc()
	svc.listAgentActivities = func(_ context.Context, filter agentdom.ListAgentActivitiesFilter, _ int) ([]*agentdom.ActivityFeedItem, bool, error) {
		gotFilter = filter
		return []*agentdom.ActivityFeedItem{
			{ID: uuid.New(), SourceType: agentdom.ActivitySourceTask, ActivityType: "task.created"},
		}, false, nil
	}
	r := newAgentRouterWithMemberRepo(svc, memberRepo, uuid.New().String())
	projectID := uuid.New()
	agentID := uuid.New()

	w := doAgentRequest(t, r, http.MethodGet,
		"/projects/"+projectID.String()+"/agents/"+agentID.String()+"/activities?type=task,doc&search=hello", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if gotFilter.ActorMemberID != memberID {
		t.Fatalf("expected the resolved member ID %v to reach the service, got %v", memberID, gotFilter.ActorMemberID)
	}
	wantTypes := []agentdom.ActivitySourceType{agentdom.ActivitySourceTask, agentdom.ActivitySourceDoc}
	if len(gotFilter.SourceTypes) != len(wantTypes) || gotFilter.SourceTypes[0] != wantTypes[0] || gotFilter.SourceTypes[1] != wantTypes[1] {
		t.Fatalf("expected source types %v, got %v", wantTypes, gotFilter.SourceTypes)
	}
	if gotFilter.Search == nil || *gotFilter.Search != "hello" {
		t.Fatalf("expected search %q, got %v", "hello", gotFilter.Search)
	}

	var body struct {
		Data struct {
			Items []any `json:"items"`
		} `json:"data"`
	}
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response body: %v", err)
	}
	if len(body.Data.Items) != 1 {
		t.Fatalf("expected 1 item in response, got %d", len(body.Data.Items))
	}
}

// ---------------------------------------------------------------------------
// GenerateGlobalACPBridgeToken (POST /admin/agents/:agentId/acp-bridge-token)
// ---------------------------------------------------------------------------

func TestGenerateGlobalACPBridgeToken_ReturnsRunCommand(t *testing.T) {
	agentID := uuid.New()
	var gotAgentID uuid.UUID
	svc := &mockAgentSvc{
		generateGlobalACPBridgeToken: func(_ context.Context, id uuid.UUID) (string, error) {
			gotAgentID = id
			return "plaintext-token", nil
		},
	}
	r := newGlobalAgentAcpRouter(svc)
	w := doAgentRequest(t, r, http.MethodPost, "/admin/agents/"+agentID.String()+"/acp-bridge-token", nil)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if gotAgentID != agentID {
		t.Errorf("expected agentID %s to reach the service, got %s", agentID, gotAgentID)
	}
	var resp struct {
		Data struct {
			Token      string `json:"token"`
			RunCommand string `json:"run_command"`
		} `json:"data"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Data.Token != "plaintext-token" {
		t.Errorf("expected token %q, got %q", "plaintext-token", resp.Data.Token)
	}
	if !strings.Contains(resp.Data.RunCommand, agentID.String()) ||
		!strings.Contains(resp.Data.RunCommand, "plaintext-token") {
		t.Errorf("expected run_command to embed agent id and token, got %q", resp.Data.RunCommand)
	}
}

func TestGenerateGlobalACPBridgeToken_RejectsNonACPAgent(t *testing.T) {
	svc := &mockAgentSvc{
		generateGlobalACPBridgeToken: func(_ context.Context, _ uuid.UUID) (string, error) {
			return "", agentdom.ErrAgentTypeInvalid
		},
	}
	r := newGlobalAgentAcpRouter(svc)
	w := doAgentRequest(t, r, http.MethodPost, "/admin/agents/"+uuid.New().String()+"/acp-bridge-token", nil)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for a non-ACP agent, got %d: %s", w.Code, w.Body.String())
	}
}

// ---------------------------------------------------------------------------
// GetGlobalACPBridgeStatus (GET /admin/agents/:agentId/acp-bridge-status)
// ---------------------------------------------------------------------------

func TestGetGlobalACPBridgeStatus_RejectsNonACPAgent(t *testing.T) {
	agentID := uuid.New()
	svc := &mockAgentSvc{
		getGlobalAgent: func(_ context.Context, id uuid.UUID) (*agentdom.Agent, error) {
			if id != agentID {
				t.Fatalf("unexpected agent id %s", id)
			}
			return &agentdom.Agent{ID: agentID, AgentType: agentdom.AgentTypeLLM}, nil
		},
	}
	r := newGlobalAgentAcpRouter(svc)
	w := doAgentRequest(t, r, http.MethodGet, "/admin/agents/"+agentID.String()+"/acp-bridge-status", nil)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for a non-ACP agent, got %d: %s", w.Code, w.Body.String())
	}
}

func TestGetGlobalACPBridgeStatus_UnknownAgentNotFound(t *testing.T) {
	svc := &mockAgentSvc{
		getGlobalAgent: func(_ context.Context, _ uuid.UUID) (*agentdom.Agent, error) {
			return nil, agentdom.ErrAgentNotFound
		},
	}
	r := newGlobalAgentAcpRouter(svc)
	w := doAgentRequest(t, r, http.MethodGet, "/admin/agents/"+uuid.New().String()+"/acp-bridge-status", nil)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}
