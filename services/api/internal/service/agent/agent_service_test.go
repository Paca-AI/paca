package agentsvc

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"

	agentdom "github.com/Paca-AI/api/internal/domain/agent"
	plugindom "github.com/Paca-AI/api/internal/domain/plugin"
)

// findAgentByIDReturning stubs mockAgentRepo.findAgentByID to return a
// minimal agent of the given type, regardless of the requested id — used by
// tests exercising MCP server / skill / env var writes, which now check the
// owning agent's type via requireNonACPAgent before touching the repo.
func findAgentByIDReturning(agentType string) func(context.Context, uuid.UUID) (*agentdom.Agent, error) {
	return func(_ context.Context, id uuid.UUID) (*agentdom.Agent, error) {
		return &agentdom.Agent{ID: id, AgentType: agentType}, nil
	}
}

type mockAgentRepo struct {
	findAgentByID                   func(ctx context.Context, id uuid.UUID) (*agentdom.Agent, error)
	findVisibleAgentInProject       func(ctx context.Context, projectID, agentID uuid.UUID) (*agentdom.Agent, error)
	findAgentByHandle               func(ctx context.Context, projectID uuid.UUID, handle string) (*agentdom.Agent, error)
	listAgents                      func(ctx context.Context, projectID uuid.UUID, scope agentdom.AgentScope) ([]*agentdom.Agent, error)
	createAgent                     func(ctx context.Context, agent *agentdom.Agent) error
	createAgentWithMembership       func(ctx context.Context, agent *agentdom.Agent, memberID, projectID, projectRoleID uuid.UUID) error
	updateAgent                     func(ctx context.Context, agent *agentdom.Agent) error
	softDeleteAgent                 func(ctx context.Context, id uuid.UUID) error
	softDeleteAgentWithMembership   func(ctx context.Context, projectID, agentID uuid.UUID) error
	setAgentMemberID                func(ctx context.Context, agentID, memberID uuid.UUID) error
	setACPBridgeTokenHash           func(ctx context.Context, agentID uuid.UUID, hash string) error
	setMCPAPIKeyHash                func(ctx context.Context, agentID uuid.UUID, hash string) error
	findAgentByMCPAPIKeyHash        func(ctx context.Context, hash string) (*agentdom.Agent, error)
	listMCPServers                  func(ctx context.Context, agentID uuid.UUID) ([]*agentdom.AgentMCPServer, error)
	findMCPServerByID               func(ctx context.Context, id uuid.UUID) (*agentdom.AgentMCPServer, error)
	createMCPServer                 func(ctx context.Context, server *agentdom.AgentMCPServer) error
	updateMCPServer                 func(ctx context.Context, server *agentdom.AgentMCPServer) error
	deleteMCPServer                 func(ctx context.Context, id uuid.UUID) error
	listSkills                      func(ctx context.Context, agentID uuid.UUID) ([]*agentdom.AgentSkill, error)
	findSkillByID                   func(ctx context.Context, id uuid.UUID) (*agentdom.AgentSkill, error)
	createSkill                     func(ctx context.Context, skill *agentdom.AgentSkill) error
	updateSkill                     func(ctx context.Context, skill *agentdom.AgentSkill) error
	deleteSkill                     func(ctx context.Context, id uuid.UUID) error
	listEnvVars                     func(ctx context.Context, agentID uuid.UUID) ([]*agentdom.AgentEnvironmentVariable, error)
	findEnvVarByID                  func(ctx context.Context, id uuid.UUID) (*agentdom.AgentEnvironmentVariable, error)
	findEnvVarByKey                 func(ctx context.Context, agentID uuid.UUID, key string) (*agentdom.AgentEnvironmentVariable, error)
	createEnvVar                    func(ctx context.Context, v *agentdom.AgentEnvironmentVariable) error
	updateEnvVar                    func(ctx context.Context, v *agentdom.AgentEnvironmentVariable) error
	deleteEnvVar                    func(ctx context.Context, id uuid.UUID) error
	listConversations               func(ctx context.Context, filter agentdom.ListConversationsFilter, limit int) ([]*agentdom.AgentConversation, bool, error)
	findConversationByID            func(ctx context.Context, id uuid.UUID) (*agentdom.AgentConversation, error)
	findLatestConversationBySession func(ctx context.Context, chatSessionID uuid.UUID) (*agentdom.AgentConversation, error)
	createConversation              func(ctx context.Context, conv *agentdom.AgentConversation) error
	updateConversationStatus        func(ctx context.Context, id uuid.UUID, status string) error
	claimConversationStatus         func(ctx context.Context, id uuid.UUID, fromStatus, toStatus string) (bool, error)
	updateConversation              func(ctx context.Context, conv *agentdom.AgentConversation) error
	listConversationEvents          func(ctx context.Context, conversationID uuid.UUID, offset, limit int) ([]*agentdom.AgentConversationEvent, int64, error)
	createConversationEvent         func(ctx context.Context, event *agentdom.AgentConversationEvent) error
	listChatSessions                func(ctx context.Context, agentID, memberID uuid.UUID) ([]*agentdom.AgentChatSession, error)
	findChatSessionByID             func(ctx context.Context, id uuid.UUID) (*agentdom.AgentChatSession, error)
	createChatSession               func(ctx context.Context, session *agentdom.AgentChatSession) error
	updateChatSession               func(ctx context.Context, session *agentdom.AgentChatSession) error
	listAgentActivities             func(ctx context.Context, filter agentdom.ListAgentActivitiesFilter, limit int) ([]*agentdom.ActivityFeedItem, bool, error)
	listGlobalAgents                func(ctx context.Context) ([]*agentdom.Agent, error)
	findGlobalAgentByHandle         func(ctx context.Context, handle string) (*agentdom.Agent, error)
	createGlobalAgent               func(ctx context.Context, agent *agentdom.Agent) error
	softDeleteGlobalAgentCascade    func(ctx context.Context, agentID uuid.UUID) error
	listInvitedProjectIDs           func(ctx context.Context, agentID uuid.UUID) ([]uuid.UUID, error)
	listGlobalChatSessions          func(ctx context.Context, agentID, actorUserID uuid.UUID) ([]*agentdom.AgentChatSession, error)
	hasActiveGlobalChatSession      func(ctx context.Context, agentID, actorUserID uuid.UUID) (bool, error)
}

func (m *mockAgentRepo) ListAgents(ctx context.Context, projectID uuid.UUID, scope agentdom.AgentScope) ([]*agentdom.Agent, error) {
	if m.listAgents != nil {
		return m.listAgents(ctx, projectID, scope)
	}
	return nil, nil
}

func (m *mockAgentRepo) FindAgentByID(ctx context.Context, id uuid.UUID) (*agentdom.Agent, error) {
	if m.findAgentByID != nil {
		return m.findAgentByID(ctx, id)
	}
	return nil, agentdom.ErrAgentNotFound
}

func (m *mockAgentRepo) FindVisibleAgentInProject(ctx context.Context, projectID, agentID uuid.UUID) (*agentdom.Agent, error) {
	if m.findVisibleAgentInProject != nil {
		return m.findVisibleAgentInProject(ctx, projectID, agentID)
	}
	// Most tests exercising UpdateAgent/DeleteAgent/GenerateACPBridgeToken
	// (which all call Service.GetAgent -> FindVisibleAgentInProject) only
	// care about agent-scoped behavior, not the project-visibility join
	// itself — falling back to findAgentByID keeps them working without
	// every one of them having to wire findVisibleAgentInProject explicitly.
	// Tests that specifically exercise visibility (e.g.
	// TestGetAgent_ResolvesInvitedGlobalAgent, TestGetAgent_WrongProject)
	// set findVisibleAgentInProject directly instead.
	if m.findAgentByID != nil {
		return m.findAgentByID(ctx, agentID)
	}
	return nil, agentdom.ErrAgentNotFound
}

func (m *mockAgentRepo) FindAgentByHandle(ctx context.Context, projectID uuid.UUID, handle string) (*agentdom.Agent, error) {
	if m.findAgentByHandle != nil {
		return m.findAgentByHandle(ctx, projectID, handle)
	}
	return nil, nil
}

func (m *mockAgentRepo) CreateAgent(ctx context.Context, agent *agentdom.Agent) error {
	if m.createAgent != nil {
		return m.createAgent(ctx, agent)
	}
	return nil
}

func (m *mockAgentRepo) CreateAgentWithMembership(ctx context.Context, agent *agentdom.Agent, memberID, projectID, projectRoleID uuid.UUID) error {
	if m.createAgentWithMembership != nil {
		return m.createAgentWithMembership(ctx, agent, memberID, projectID, projectRoleID)
	}
	return nil
}

func (m *mockAgentRepo) UpdateAgent(ctx context.Context, agent *agentdom.Agent) error {
	if m.updateAgent != nil {
		return m.updateAgent(ctx, agent)
	}
	return nil
}

func (m *mockAgentRepo) SoftDeleteAgent(ctx context.Context, id uuid.UUID) error {
	if m.softDeleteAgent != nil {
		return m.softDeleteAgent(ctx, id)
	}
	return nil
}

func (m *mockAgentRepo) SoftDeleteAgentWithMembership(ctx context.Context, projectID, agentID uuid.UUID) error {
	if m.softDeleteAgentWithMembership != nil {
		return m.softDeleteAgentWithMembership(ctx, projectID, agentID)
	}
	return nil
}

func (m *mockAgentRepo) ListGlobalAgents(ctx context.Context) ([]*agentdom.Agent, error) {
	if m.listGlobalAgents != nil {
		return m.listGlobalAgents(ctx)
	}
	return nil, nil
}

func (m *mockAgentRepo) FindGlobalAgentByHandle(ctx context.Context, handle string) (*agentdom.Agent, error) {
	if m.findGlobalAgentByHandle != nil {
		return m.findGlobalAgentByHandle(ctx, handle)
	}
	return nil, agentdom.ErrAgentNotFound
}

func (m *mockAgentRepo) CreateGlobalAgent(ctx context.Context, agent *agentdom.Agent) error {
	if m.createGlobalAgent != nil {
		return m.createGlobalAgent(ctx, agent)
	}
	return nil
}

func (m *mockAgentRepo) SoftDeleteGlobalAgentCascade(ctx context.Context, agentID uuid.UUID) error {
	if m.softDeleteGlobalAgentCascade != nil {
		return m.softDeleteGlobalAgentCascade(ctx, agentID)
	}
	return nil
}

func (m *mockAgentRepo) ListInvitedProjectIDs(ctx context.Context, agentID uuid.UUID) ([]uuid.UUID, error) {
	if m.listInvitedProjectIDs != nil {
		return m.listInvitedProjectIDs(ctx, agentID)
	}
	return nil, nil
}

func (m *mockAgentRepo) SetAgentMemberID(ctx context.Context, agentID, memberID uuid.UUID) error {
	if m.setAgentMemberID != nil {
		return m.setAgentMemberID(ctx, agentID, memberID)
	}
	return nil
}

func (m *mockAgentRepo) SetACPBridgeTokenHash(ctx context.Context, agentID uuid.UUID, hash string) error {
	if m.setACPBridgeTokenHash != nil {
		return m.setACPBridgeTokenHash(ctx, agentID, hash)
	}
	return nil
}

func (m *mockAgentRepo) SetMCPAPIKeyHash(ctx context.Context, agentID uuid.UUID, hash string) error {
	if m.setMCPAPIKeyHash != nil {
		return m.setMCPAPIKeyHash(ctx, agentID, hash)
	}
	return nil
}

func (m *mockAgentRepo) FindAgentByMCPAPIKeyHash(ctx context.Context, hash string) (*agentdom.Agent, error) {
	if m.findAgentByMCPAPIKeyHash != nil {
		return m.findAgentByMCPAPIKeyHash(ctx, hash)
	}
	return nil, agentdom.ErrAgentNotFound
}

func (m *mockAgentRepo) ListMCPServers(ctx context.Context, agentID uuid.UUID) ([]*agentdom.AgentMCPServer, error) {
	if m.listMCPServers != nil {
		return m.listMCPServers(ctx, agentID)
	}
	return nil, nil
}

func (m *mockAgentRepo) FindMCPServerByID(ctx context.Context, id uuid.UUID) (*agentdom.AgentMCPServer, error) {
	if m.findMCPServerByID != nil {
		return m.findMCPServerByID(ctx, id)
	}
	return nil, agentdom.ErrMCPServerNotFound
}

func (m *mockAgentRepo) CreateMCPServer(ctx context.Context, server *agentdom.AgentMCPServer) error {
	if m.createMCPServer != nil {
		return m.createMCPServer(ctx, server)
	}
	return nil
}

func (m *mockAgentRepo) UpdateMCPServer(ctx context.Context, server *agentdom.AgentMCPServer) error {
	if m.updateMCPServer != nil {
		return m.updateMCPServer(ctx, server)
	}
	return nil
}

func (m *mockAgentRepo) DeleteMCPServer(ctx context.Context, id uuid.UUID) error {
	if m.deleteMCPServer != nil {
		return m.deleteMCPServer(ctx, id)
	}
	return nil
}

func (m *mockAgentRepo) ListSkills(ctx context.Context, agentID uuid.UUID) ([]*agentdom.AgentSkill, error) {
	if m.listSkills != nil {
		return m.listSkills(ctx, agentID)
	}
	return nil, nil
}

func (m *mockAgentRepo) FindSkillByID(ctx context.Context, id uuid.UUID) (*agentdom.AgentSkill, error) {
	if m.findSkillByID != nil {
		return m.findSkillByID(ctx, id)
	}
	return nil, agentdom.ErrSkillNotFound
}

func (m *mockAgentRepo) CreateSkill(ctx context.Context, skill *agentdom.AgentSkill) error {
	if m.createSkill != nil {
		return m.createSkill(ctx, skill)
	}
	return nil
}

func (m *mockAgentRepo) UpdateSkill(ctx context.Context, skill *agentdom.AgentSkill) error {
	if m.updateSkill != nil {
		return m.updateSkill(ctx, skill)
	}
	return nil
}

func (m *mockAgentRepo) DeleteSkill(ctx context.Context, id uuid.UUID) error {
	if m.deleteSkill != nil {
		return m.deleteSkill(ctx, id)
	}
	return nil
}

func (m *mockAgentRepo) ListEnvVars(ctx context.Context, agentID uuid.UUID) ([]*agentdom.AgentEnvironmentVariable, error) {
	if m.listEnvVars != nil {
		return m.listEnvVars(ctx, agentID)
	}
	return nil, nil
}

func (m *mockAgentRepo) FindEnvVarByID(ctx context.Context, id uuid.UUID) (*agentdom.AgentEnvironmentVariable, error) {
	if m.findEnvVarByID != nil {
		return m.findEnvVarByID(ctx, id)
	}
	return nil, agentdom.ErrEnvVarNotFound
}

func (m *mockAgentRepo) FindEnvVarByKey(ctx context.Context, agentID uuid.UUID, key string) (*agentdom.AgentEnvironmentVariable, error) {
	if m.findEnvVarByKey != nil {
		return m.findEnvVarByKey(ctx, agentID, key)
	}
	return nil, agentdom.ErrEnvVarNotFound
}

func (m *mockAgentRepo) CreateEnvVar(ctx context.Context, v *agentdom.AgentEnvironmentVariable) error {
	if m.createEnvVar != nil {
		return m.createEnvVar(ctx, v)
	}
	return nil
}

func (m *mockAgentRepo) UpdateEnvVar(ctx context.Context, v *agentdom.AgentEnvironmentVariable) error {
	if m.updateEnvVar != nil {
		return m.updateEnvVar(ctx, v)
	}
	return nil
}

func (m *mockAgentRepo) DeleteEnvVar(ctx context.Context, id uuid.UUID) error {
	if m.deleteEnvVar != nil {
		return m.deleteEnvVar(ctx, id)
	}
	return nil
}

func (m *mockAgentRepo) ListConversations(ctx context.Context, filter agentdom.ListConversationsFilter, limit int) ([]*agentdom.AgentConversation, bool, error) {
	if m.listConversations != nil {
		return m.listConversations(ctx, filter, limit)
	}
	return nil, false, nil
}

func (m *mockAgentRepo) ListAgentActivities(ctx context.Context, filter agentdom.ListAgentActivitiesFilter, limit int) ([]*agentdom.ActivityFeedItem, bool, error) {
	if m.listAgentActivities != nil {
		return m.listAgentActivities(ctx, filter, limit)
	}
	return nil, false, nil
}

func (m *mockAgentRepo) FindConversationByID(ctx context.Context, id uuid.UUID) (*agentdom.AgentConversation, error) {
	if m.findConversationByID != nil {
		return m.findConversationByID(ctx, id)
	}
	return nil, agentdom.ErrConversationNotFound
}

func (m *mockAgentRepo) FindLatestConversationByChatSession(ctx context.Context, chatSessionID uuid.UUID) (*agentdom.AgentConversation, error) {
	if m.findLatestConversationBySession != nil {
		return m.findLatestConversationBySession(ctx, chatSessionID)
	}
	return nil, nil
}

func (m *mockAgentRepo) CreateConversation(ctx context.Context, conv *agentdom.AgentConversation) error {
	if m.createConversation != nil {
		return m.createConversation(ctx, conv)
	}
	return nil
}

func (m *mockAgentRepo) UpdateConversationStatus(ctx context.Context, id uuid.UUID, status string) error {
	if m.updateConversationStatus != nil {
		return m.updateConversationStatus(ctx, id, status)
	}
	return nil
}

func (m *mockAgentRepo) ClaimConversationStatus(ctx context.Context, id uuid.UUID, fromStatus, toStatus string) (bool, error) {
	if m.claimConversationStatus != nil {
		return m.claimConversationStatus(ctx, id, fromStatus, toStatus)
	}
	return true, nil
}

func (m *mockAgentRepo) UpdateConversation(ctx context.Context, conv *agentdom.AgentConversation) error {
	if m.updateConversation != nil {
		return m.updateConversation(ctx, conv)
	}
	return nil
}

func (m *mockAgentRepo) ListConversationEvents(ctx context.Context, conversationID uuid.UUID, offset, limit int) ([]*agentdom.AgentConversationEvent, int64, error) {
	if m.listConversationEvents != nil {
		return m.listConversationEvents(ctx, conversationID, offset, limit)
	}
	return nil, 0, nil
}

func (m *mockAgentRepo) CreateConversationEvent(ctx context.Context, event *agentdom.AgentConversationEvent) error {
	if m.createConversationEvent != nil {
		return m.createConversationEvent(ctx, event)
	}
	return nil
}

func (m *mockAgentRepo) ListChatSessions(ctx context.Context, agentID, memberID uuid.UUID) ([]*agentdom.AgentChatSession, error) {
	if m.listChatSessions != nil {
		return m.listChatSessions(ctx, agentID, memberID)
	}
	return nil, nil
}

func (m *mockAgentRepo) ListGlobalChatSessions(ctx context.Context, agentID, actorUserID uuid.UUID) ([]*agentdom.AgentChatSession, error) {
	if m.listGlobalChatSessions != nil {
		return m.listGlobalChatSessions(ctx, agentID, actorUserID)
	}
	return nil, nil
}

func (m *mockAgentRepo) HasActiveGlobalChatSession(ctx context.Context, agentID, actorUserID uuid.UUID) (bool, error) {
	if m.hasActiveGlobalChatSession != nil {
		return m.hasActiveGlobalChatSession(ctx, agentID, actorUserID)
	}
	return false, nil
}

func (m *mockAgentRepo) FindChatSessionByID(ctx context.Context, id uuid.UUID) (*agentdom.AgentChatSession, error) {
	if m.findChatSessionByID != nil {
		return m.findChatSessionByID(ctx, id)
	}
	return nil, agentdom.ErrChatSessionNotFound
}

func (m *mockAgentRepo) CreateChatSession(ctx context.Context, session *agentdom.AgentChatSession) error {
	if m.createChatSession != nil {
		return m.createChatSession(ctx, session)
	}
	return nil
}

func (m *mockAgentRepo) UpdateChatSession(ctx context.Context, session *agentdom.AgentChatSession) error {
	if m.updateChatSession != nil {
		return m.updateChatSession(ctx, session)
	}
	return nil
}

var _ agentdom.Repository = (*mockAgentRepo)(nil)

type mockProjectRepo struct {
	invalidateMembersCacheCalled bool
	invalidatedProjectIDs        []uuid.UUID
}

func (m *mockProjectRepo) InvalidateMembersCache(_ context.Context, projectID uuid.UUID) error {
	m.invalidateMembersCacheCalled = true
	m.invalidatedProjectIDs = append(m.invalidatedProjectIDs, projectID)
	return nil
}

var _ projectMemberWriter = (*mockProjectRepo)(nil)

type mockPluginRepo struct {
	findByName       func(ctx context.Context, name string) (*plugindom.Plugin, error)
	findByCapability func(ctx context.Context, capability string) ([]*plugindom.Plugin, error)
}

func (m *mockPluginRepo) FindByName(ctx context.Context, name string) (*plugindom.Plugin, error) {
	if m.findByName != nil {
		return m.findByName(ctx, name)
	}
	return nil, nil
}

func (m *mockPluginRepo) FindByCapability(ctx context.Context, capability string) ([]*plugindom.Plugin, error) {
	if m.findByCapability != nil {
		return m.findByCapability(ctx, capability)
	}
	return nil, nil
}

var _ pluginFinder = (*mockPluginRepo)(nil)

func TestGetAgent_Success(t *testing.T) {
	projectID := uuid.New()
	agentID := uuid.New()
	agent := &agentdom.Agent{
		ID:        agentID,
		ProjectID: projectID,
		Name:      "Test Agent",
		Handle:    "test-agent",
	}

	repo := &mockAgentRepo{
		findVisibleAgentInProject: func(_ context.Context, gotProjectID, gotAgentID uuid.UUID) (*agentdom.Agent, error) {
			if gotProjectID != projectID || gotAgentID != agentID {
				t.Fatalf("unexpected lookup (%s, %s)", gotProjectID, gotAgentID)
			}
			return agent, nil
		},
	}
	projRepo := &mockProjectRepo{}
	pluginRepo := &mockPluginRepo{}
	svc := New(repo, projRepo, nil, pluginRepo)

	result, err := svc.GetAgent(context.Background(), projectID, agentID)

	assert.NoError(t, err)
	assert.Equal(t, agentID, result.ID)
	assert.Equal(t, projectID, result.ProjectID)
}

func TestGetAgent_WrongProject(t *testing.T) {
	projectID := uuid.New()
	agentID := uuid.New()

	// The repo layer (FindVisibleAgentInProject) is the one that actually
	// enforces visibility via its project_members join — a project this
	// agent isn't visible in simply yields no row, which the postgres impl
	// maps to ErrAgentNotFound (see agent_repository.go). The service just
	// has to propagate that, not re-check ProjectID itself.
	repo := &mockAgentRepo{
		findVisibleAgentInProject: func(context.Context, uuid.UUID, uuid.UUID) (*agentdom.Agent, error) {
			return nil, agentdom.ErrAgentNotFound
		},
	}
	projRepo := &mockProjectRepo{}
	pluginRepo := &mockPluginRepo{}
	svc := New(repo, projRepo, nil, pluginRepo)

	_, err := svc.GetAgent(context.Background(), projectID, agentID)

	assert.Error(t, err)
	assert.ErrorIs(t, err, agentdom.ErrAgentNotFound)
}

// TestGetAgent_ResolvesInvitedGlobalAgent is the regression test for the
// gap Pullfrog's review flagged: a global agent has ProjectID == uuid.Nil,
// never equal to any real project — the old ProjectID-equality check in
// GetAgent would 404 on it even when the project's own agent list (which
// joins through project_members, see ListAgents) shows it as a member. The
// fix delegates entirely to FindVisibleAgentInProject, so this just asserts
// the service returns whatever that lookup finds without re-applying the
// old ownership check.
func TestGetAgent_ResolvesInvitedGlobalAgent(t *testing.T) {
	projectID := uuid.New()
	agentID := uuid.New()
	globalAgent := &agentdom.Agent{
		ID:         agentID,
		ProjectID:  uuid.Nil, // global agents never have a project of their own
		AgentScope: agentdom.AgentScopeGlobal,
		Name:       "Global Bot",
		Handle:     "global-bot",
	}

	repo := &mockAgentRepo{
		findVisibleAgentInProject: func(_ context.Context, gotProjectID, gotAgentID uuid.UUID) (*agentdom.Agent, error) {
			if gotProjectID == projectID && gotAgentID == agentID {
				return globalAgent, nil
			}
			return nil, agentdom.ErrAgentNotFound
		},
	}
	svc := New(repo, &mockProjectRepo{}, nil, &mockPluginRepo{})

	result, err := svc.GetAgent(context.Background(), projectID, agentID)

	assert.NoError(t, err)
	assert.Equal(t, agentID, result.ID)
	assert.Equal(t, agentdom.AgentScopeGlobal, result.AgentScope)
}

func TestListAgents_Success(t *testing.T) {
	projectID := uuid.New()
	agent1 := &agentdom.Agent{
		ID:        uuid.New(),
		ProjectID: projectID,
		Name:      "Agent 1",
		Handle:    "agent-1",
	}
	agent2 := &agentdom.Agent{
		ID:        uuid.New(),
		ProjectID: projectID,
		Name:      "Agent 2",
		Handle:    "agent-2",
	}

	repo := &mockAgentRepo{
		listAgents: func(_ context.Context, pid uuid.UUID, scope agentdom.AgentScope) ([]*agentdom.Agent, error) {
			if pid != projectID {
				t.Fatalf("expected projectID %v, got %v", projectID, pid)
			}
			if scope != "" {
				t.Fatalf("expected no scope filter, got %q", scope)
			}
			return []*agentdom.Agent{agent1, agent2}, nil
		},
	}
	projRepo := &mockProjectRepo{}
	pluginRepo := &mockPluginRepo{}
	svc := New(repo, projRepo, nil, pluginRepo)

	result, err := svc.ListAgents(context.Background(), projectID, "")

	assert.NoError(t, err)
	assert.Len(t, result, 2)
}

func TestListAgents_ScopeFilterPassedThrough(t *testing.T) {
	projectID := uuid.New()
	var gotScope agentdom.AgentScope
	repo := &mockAgentRepo{
		listAgents: func(_ context.Context, _ uuid.UUID, scope agentdom.AgentScope) ([]*agentdom.Agent, error) {
			gotScope = scope
			return nil, nil
		},
	}
	svc := New(repo, &mockProjectRepo{}, nil, &mockPluginRepo{})

	_, err := svc.ListAgents(context.Background(), projectID, agentdom.AgentScopeProject)

	assert.NoError(t, err)
	assert.Equal(t, agentdom.AgentScopeProject, gotScope)
}

func TestCreateAgent_Success(t *testing.T) {
	projectID := uuid.New()
	projectRoleID := uuid.New()
	userID := uuid.New()

	repo := &mockAgentRepo{
		findAgentByHandle: func(_ context.Context, _ uuid.UUID, _ string) (*agentdom.Agent, error) {
			return nil, agentdom.ErrAgentNotFound
		},
		createAgentWithMembership: func(_ context.Context, _ *agentdom.Agent, _ uuid.UUID, pid, roleID uuid.UUID) error {
			if pid != projectID || roleID != projectRoleID {
				t.Fatalf("unexpected projectID or roleID")
			}
			return nil
		},
	}
	projRepo := &mockProjectRepo{}
	pluginRepo := &mockPluginRepo{}
	svc := New(repo, projRepo, nil, pluginRepo)

	result, err := svc.CreateAgent(context.Background(), projectID, agentdom.CreateAgentInput{
		Name:          "New Agent",
		Handle:        "new-agent",
		LLMProvider:   "openai",
		LLMModel:      "gpt-4",
		LLMAPIKey:     "sk-test",
		ProjectRoleID: projectRoleID,
		CreatedBy:     &userID,
	})

	assert.NoError(t, err)
	assert.Equal(t, "New Agent", result.Name)
	assert.Equal(t, "new-agent", result.Handle)
	assert.Equal(t, "openai", result.LLMProvider)
	assert.Equal(t, "gpt-4", result.LLMModel)
	assert.True(t, projRepo.invalidateMembersCacheCalled)
}

func TestCreateAgent_EmptyHandle(t *testing.T) {
	projectID := uuid.New()
	projectRoleID := uuid.New()

	repo := &mockAgentRepo{}
	projRepo := &mockProjectRepo{}
	pluginRepo := &mockPluginRepo{}
	svc := New(repo, projRepo, nil, pluginRepo)

	_, err := svc.CreateAgent(context.Background(), projectID, agentdom.CreateAgentInput{
		Name:          "New Agent",
		Handle:        "",
		ProjectRoleID: projectRoleID,
	})

	assert.Error(t, err)
	assert.ErrorIs(t, err, agentdom.ErrAgentHandleInvalid)
}

func TestCreateAgent_EmptyName(t *testing.T) {
	projectID := uuid.New()
	projectRoleID := uuid.New()

	repo := &mockAgentRepo{}
	projRepo := &mockProjectRepo{}
	pluginRepo := &mockPluginRepo{}
	svc := New(repo, projRepo, nil, pluginRepo)

	_, err := svc.CreateAgent(context.Background(), projectID, agentdom.CreateAgentInput{
		Name:          "",
		Handle:        "new-agent",
		ProjectRoleID: projectRoleID,
	})

	assert.Error(t, err)
	assert.ErrorIs(t, err, agentdom.ErrAgentNameInvalid)
}

func TestCreateAgent_HandleTaken(t *testing.T) {
	projectID := uuid.New()
	projectRoleID := uuid.New()
	existingAgent := &agentdom.Agent{
		ID:        uuid.New(),
		ProjectID: projectID,
		Handle:    "new-agent",
	}

	repo := &mockAgentRepo{
		findAgentByHandle: func(_ context.Context, _ uuid.UUID, _ string) (*agentdom.Agent, error) {
			return existingAgent, nil
		},
	}
	projRepo := &mockProjectRepo{}
	pluginRepo := &mockPluginRepo{}
	svc := New(repo, projRepo, nil, pluginRepo)

	_, err := svc.CreateAgent(context.Background(), projectID, agentdom.CreateAgentInput{
		Name:          "New Agent",
		Handle:        "new-agent",
		ProjectRoleID: projectRoleID,
	})

	assert.Error(t, err)
	assert.ErrorIs(t, err, agentdom.ErrAgentHandleTaken)
}

func TestUpdateAgent_Success(t *testing.T) {
	projectID := uuid.New()
	agentID := uuid.New()
	agent := &agentdom.Agent{
		ID:        agentID,
		ProjectID: projectID,
		Name:      "Old Name",
		Handle:    "old-handle",
		LLMModel:  "gpt-3.5",
	}

	repo := &mockAgentRepo{
		findAgentByID: func(_ context.Context, _ uuid.UUID) (*agentdom.Agent, error) {
			return agent, nil
		},
		findAgentByHandle: func(_ context.Context, _ uuid.UUID, _ string) (*agentdom.Agent, error) {
			return nil, agentdom.ErrAgentNotFound
		},
		updateAgent: func(_ context.Context, a *agentdom.Agent) error {
			if a.ID != agentID {
				t.Fatalf("unexpected agent ID")
			}
			return nil
		},
	}
	projRepo := &mockProjectRepo{}
	pluginRepo := &mockPluginRepo{}
	svc := New(repo, projRepo, nil, pluginRepo)

	newName := "New Name"
	newHandle := "new-handle"
	newModel := "gpt-4"

	result, err := svc.UpdateAgent(context.Background(), projectID, agentID, agentdom.UpdateAgentInput{
		Name:     &newName,
		Handle:   &newHandle,
		LLMModel: &newModel,
	})

	assert.NoError(t, err)
	assert.Equal(t, newName, result.Name)
	assert.Equal(t, newHandle, result.Handle)
	assert.Equal(t, newModel, result.LLMModel)
}

func TestUpdateAgent_ACPAgentIgnoresLLMFields(t *testing.T) {
	projectID := uuid.New()
	agentID := uuid.New()
	provider := agentdom.ACPProviderClaudeCode
	agent := &agentdom.Agent{
		ID:          agentID,
		ProjectID:   projectID,
		Name:        "ACP Agent",
		Handle:      "acp-agent",
		AgentType:   agentdom.AgentTypeACP,
		ACPProvider: &provider,
	}

	repo := &mockAgentRepo{
		findAgentByID: func(_ context.Context, _ uuid.UUID) (*agentdom.Agent, error) {
			return agent, nil
		},
		updateAgent: func(_ context.Context, _ *agentdom.Agent) error {
			return nil
		},
	}
	svc := New(repo, &mockProjectRepo{}, nil, &mockPluginRepo{})

	newModel := "gpt-4"
	newAPIKey := "sk-leaked-onto-acp-agent"
	newBaseURL := "https://api.openai.com/v1"
	newSystemPrompt := "you are a helpful assistant"
	newCommitterName := "someone"
	newCommitterEmail := "someone@example.com"

	result, err := svc.UpdateAgent(context.Background(), projectID, agentID, agentdom.UpdateAgentInput{
		LLMModel:          &newModel,
		LLMAPIKey:         &newAPIKey,
		LLMBaseURL:        &newBaseURL,
		SystemPrompt:      &newSystemPrompt,
		GitCommitterName:  &newCommitterName,
		GitCommitterEmail: &newCommitterEmail,
	})

	assert.NoError(t, err)
	assert.Empty(t, result.LLMModel)
	assert.Empty(t, result.LLMAPIKeySecret)
	assert.Empty(t, result.LLMBaseURL)
	assert.Empty(t, result.SystemPrompt)
	assert.Empty(t, result.GitCommitterName)
	assert.Empty(t, result.GitCommitterEmail)
}

func TestUpdateAgent_LLMAgentIgnoresACPFields(t *testing.T) {
	projectID := uuid.New()
	agentID := uuid.New()
	agent := &agentdom.Agent{
		ID:        agentID,
		ProjectID: projectID,
		Name:      "LLM Agent",
		Handle:    "llm-agent",
		AgentType: agentdom.AgentTypeLLM,
	}

	repo := &mockAgentRepo{
		findAgentByID: func(_ context.Context, _ uuid.UUID) (*agentdom.Agent, error) {
			return agent, nil
		},
		updateAgent: func(_ context.Context, _ *agentdom.Agent) error {
			return nil
		},
	}
	svc := New(repo, &mockProjectRepo{}, nil, &mockPluginRepo{})

	newProvider := agentdom.ACPProviderCustom

	result, err := svc.UpdateAgent(context.Background(), projectID, agentID, agentdom.UpdateAgentInput{
		ACPProvider: &newProvider,
		ACPCommand:  []string{"my-server"},
	})

	assert.NoError(t, err)
	assert.Nil(t, result.ACPProvider)
	assert.Empty(t, result.ACPCommand)
}

func TestUpdateAgent_HandleTaken(t *testing.T) {
	projectID := uuid.New()
	agentID := uuid.New()
	agent := &agentdom.Agent{
		ID:        agentID,
		ProjectID: projectID,
		Name:      "Test Agent",
		Handle:    "current-handle",
	}

	existingAgent := &agentdom.Agent{
		ID:        uuid.New(),
		ProjectID: projectID,
		Handle:    "new-handle",
	}

	repo := &mockAgentRepo{
		findAgentByID: func(_ context.Context, _ uuid.UUID) (*agentdom.Agent, error) {
			return agent, nil
		},
		findAgentByHandle: func(_ context.Context, _ uuid.UUID, _ string) (*agentdom.Agent, error) {
			return existingAgent, nil
		},
	}
	projRepo := &mockProjectRepo{}
	pluginRepo := &mockPluginRepo{}
	svc := New(repo, projRepo, nil, pluginRepo)

	newHandle := "new-handle"

	_, err := svc.UpdateAgent(context.Background(), projectID, agentID, agentdom.UpdateAgentInput{
		Handle: &newHandle,
	})

	assert.Error(t, err)
	assert.ErrorIs(t, err, agentdom.ErrAgentHandleTaken)
}

func TestDeleteAgent_Success(t *testing.T) {
	projectID := uuid.New()
	agentID := uuid.New()
	agent := &agentdom.Agent{
		ID:        agentID,
		ProjectID: projectID,
		Name:      "Test Agent",
	}

	repo := &mockAgentRepo{
		findAgentByID: func(_ context.Context, _ uuid.UUID) (*agentdom.Agent, error) {
			return agent, nil
		},
		softDeleteAgentWithMembership: func(_ context.Context, pid, aid uuid.UUID) error {
			if pid != projectID || aid != agentID {
				t.Fatalf("unexpected projectID or agentID")
			}
			return nil
		},
	}
	projRepo := &mockProjectRepo{}
	pluginRepo := &mockPluginRepo{}
	svc := New(repo, projRepo, nil, pluginRepo)

	err := svc.DeleteAgent(context.Background(), projectID, agentID)

	assert.NoError(t, err)
	assert.True(t, projRepo.invalidateMembersCacheCalled)
}

// ---------------------------------------------------------------------------
// Global agents
// ---------------------------------------------------------------------------

func TestCreateGlobalAgent_Success(t *testing.T) {
	roleID := uuid.New()
	userID := uuid.New()
	var created *agentdom.Agent

	repo := &mockAgentRepo{
		findGlobalAgentByHandle: func(_ context.Context, _ string) (*agentdom.Agent, error) {
			return nil, agentdom.ErrAgentNotFound
		},
		createGlobalAgent: func(_ context.Context, a *agentdom.Agent) error {
			created = a
			return nil
		},
	}
	svc := New(repo, &mockProjectRepo{}, nil, &mockPluginRepo{})

	result, err := svc.CreateGlobalAgent(context.Background(), agentdom.CreateGlobalAgentInput{
		Name:         "Global Bot",
		Handle:       "global-bot",
		LLMProvider:  "openai",
		LLMModel:     "gpt-4",
		LLMAPIKey:    "sk-test",
		GlobalRoleID: &roleID,
		CreatedBy:    &userID,
	})

	assert.NoError(t, err)
	assert.Equal(t, "Global Bot", result.Name)
	assert.Equal(t, agentdom.AgentScopeGlobal, result.AgentScope)
	assert.Equal(t, uuid.Nil, result.ProjectID)
	if assert.NotNil(t, result.GlobalRoleID) {
		assert.Equal(t, roleID, *result.GlobalRoleID)
	}
	// The agent handed to the repo must carry the same scope/role, not just
	// the returned value — CreateGlobalAgent must never fall back to
	// CreateAgentWithMembership's project-scoped insert path.
	if assert.NotNil(t, created) {
		assert.Equal(t, agentdom.AgentScopeGlobal, created.AgentScope)
		assert.Equal(t, uuid.Nil, created.ProjectID)
	}
}

func TestCreateGlobalAgent_HandleTaken(t *testing.T) {
	repo := &mockAgentRepo{
		findGlobalAgentByHandle: func(_ context.Context, handle string) (*agentdom.Agent, error) {
			return &agentdom.Agent{ID: uuid.New(), Handle: handle, AgentScope: agentdom.AgentScopeGlobal}, nil
		},
	}
	svc := New(repo, &mockProjectRepo{}, nil, &mockPluginRepo{})

	_, err := svc.CreateGlobalAgent(context.Background(), agentdom.CreateGlobalAgentInput{
		Name:        "Global Bot",
		Handle:      "global-bot",
		LLMProvider: "openai",
		LLMModel:    "gpt-4",
		LLMAPIKey:   "sk-test",
	})

	assert.ErrorIs(t, err, agentdom.ErrAgentHandleTaken)
}

func TestGetGlobalAgent_RejectsProjectScopedAgent(t *testing.T) {
	agentID := uuid.New()
	repo := &mockAgentRepo{
		findAgentByID: func(_ context.Context, _ uuid.UUID) (*agentdom.Agent, error) {
			return &agentdom.Agent{ID: agentID, ProjectID: uuid.New(), AgentScope: agentdom.AgentScopeProject}, nil
		},
	}
	svc := New(repo, &mockProjectRepo{}, nil, &mockPluginRepo{})

	_, err := svc.GetGlobalAgent(context.Background(), agentID)

	assert.ErrorIs(t, err, agentdom.ErrAgentNotFound)
}

func TestDeleteGlobalAgent_Success(t *testing.T) {
	agentID := uuid.New()
	project1, project2 := uuid.New(), uuid.New()
	cascadeDeleted := false

	repo := &mockAgentRepo{
		findAgentByID: func(_ context.Context, _ uuid.UUID) (*agentdom.Agent, error) {
			return &agentdom.Agent{ID: agentID, AgentScope: agentdom.AgentScopeGlobal}, nil
		},
		listInvitedProjectIDs: func(_ context.Context, aid uuid.UUID) ([]uuid.UUID, error) {
			assert.Equal(t, agentID, aid)
			return []uuid.UUID{project1, project2}, nil
		},
		softDeleteGlobalAgentCascade: func(_ context.Context, aid uuid.UUID) error {
			assert.Equal(t, agentID, aid)
			cascadeDeleted = true
			return nil
		},
	}
	projRepo := &mockProjectRepo{}
	svc := New(repo, projRepo, nil, &mockPluginRepo{})

	err := svc.DeleteGlobalAgent(context.Background(), agentID)

	assert.NoError(t, err)
	assert.True(t, cascadeDeleted)
	// Every project the agent was invited into gets its member cache
	// invalidated, not just one.
	assert.ElementsMatch(t, []uuid.UUID{project1, project2}, projRepo.invalidatedProjectIDs)
}

func TestDeleteGlobalAgent_RejectsProjectScopedAgent(t *testing.T) {
	agentID := uuid.New()
	repo := &mockAgentRepo{
		findAgentByID: func(_ context.Context, _ uuid.UUID) (*agentdom.Agent, error) {
			return &agentdom.Agent{ID: agentID, ProjectID: uuid.New(), AgentScope: agentdom.AgentScopeProject}, nil
		},
	}
	svc := New(repo, &mockProjectRepo{}, nil, &mockPluginRepo{})

	err := svc.DeleteGlobalAgent(context.Background(), agentID)

	assert.ErrorIs(t, err, agentdom.ErrAgentNotFound)
}

func TestStartGlobalChatSession_Success(t *testing.T) {
	agentID := uuid.New()
	actorUserID := uuid.New()
	var createdSession *agentdom.AgentChatSession
	var createdConv *agentdom.AgentConversation

	repo := &mockAgentRepo{
		createChatSession: func(_ context.Context, s *agentdom.AgentChatSession) error {
			createdSession = s
			return nil
		},
		createConversation: func(_ context.Context, c *agentdom.AgentConversation) error {
			createdConv = c
			return nil
		},
	}
	svc := New(repo, &mockProjectRepo{}, nil, &mockPluginRepo{})

	session, conv, err := svc.StartGlobalChatSession(context.Background(), agentID, actorUserID, "hello")

	assert.NoError(t, err)
	assert.NotNil(t, session)
	assert.NotNil(t, conv)
	// The session/conversation actually persisted must carry no project and
	// the human's raw user ID as the actor — not a resolved project member.
	if assert.NotNil(t, createdSession) {
		assert.Equal(t, uuid.Nil, createdSession.ProjectID)
		assert.Equal(t, uuid.Nil, createdSession.MemberID)
		if assert.NotNil(t, createdSession.ActorUserID) {
			assert.Equal(t, actorUserID, *createdSession.ActorUserID)
		}
	}
	if assert.NotNil(t, createdConv) {
		assert.Equal(t, uuid.Nil, createdConv.ProjectID)
		assert.Nil(t, createdConv.TriggeredByMemberID)
		if assert.NotNil(t, createdConv.ActorUserID) {
			assert.Equal(t, actorUserID, *createdConv.ActorUserID)
		}
	}
}

func TestListMCPServers_Success(t *testing.T) {
	agentID := uuid.New()
	servers := []*agentdom.AgentMCPServer{
		{ID: uuid.New(), AgentID: agentID, ServerName: "Server 1"},
		{ID: uuid.New(), AgentID: agentID, ServerName: "Server 2"},
	}

	repo := &mockAgentRepo{
		listMCPServers: func(_ context.Context, aid uuid.UUID) ([]*agentdom.AgentMCPServer, error) {
			if aid != agentID {
				t.Fatalf("expected agentID %v, got %v", agentID, aid)
			}
			return servers, nil
		},
	}
	projRepo := &mockProjectRepo{}
	pluginRepo := &mockPluginRepo{}
	svc := New(repo, projRepo, nil, pluginRepo)

	result, err := svc.ListMCPServers(context.Background(), agentID)

	assert.NoError(t, err)
	assert.Len(t, result, 2)
}

func TestAddMCPServer_Success(t *testing.T) {
	agentID := uuid.New()
	command := "python"
	url := "http://localhost:8080"

	repo := &mockAgentRepo{
		findAgentByID: findAgentByIDReturning(agentdom.AgentTypeLLM),
		createMCPServer: func(_ context.Context, server *agentdom.AgentMCPServer) error {
			if server.AgentID != agentID {
				t.Fatalf("unexpected agentID")
			}
			return nil
		},
	}
	projRepo := &mockProjectRepo{}
	pluginRepo := &mockPluginRepo{}
	svc := New(repo, projRepo, nil, pluginRepo)

	result, err := svc.AddMCPServer(context.Background(), agentID, agentdom.AddMCPServerInput{
		ServerName: "Test Server",
		Transport:  "stdio",
		Command:    &command,
		Args:       []string{"-m", "server"},
		URL:        &url,
	})

	assert.NoError(t, err)
	assert.Equal(t, "Test Server", result.ServerName)
	assert.Equal(t, "stdio", result.Transport)
}

func TestListSkills_Success(t *testing.T) {
	agentID := uuid.New()
	skills := []*agentdom.AgentSkill{
		{ID: uuid.New(), AgentID: agentID, SkillName: "Skill 1"},
		{ID: uuid.New(), AgentID: agentID, SkillName: "Skill 2"},
	}

	repo := &mockAgentRepo{
		listSkills: func(_ context.Context, aid uuid.UUID) ([]*agentdom.AgentSkill, error) {
			if aid != agentID {
				t.Fatalf("expected agentID %v, got %v", agentID, aid)
			}
			return skills, nil
		},
	}
	projRepo := &mockProjectRepo{}
	pluginRepo := &mockPluginRepo{}
	svc := New(repo, projRepo, nil, pluginRepo)

	result, err := svc.ListSkills(context.Background(), agentID)

	assert.NoError(t, err)
	assert.Len(t, result, 2)
}

func TestAddSkill_Success(t *testing.T) {
	agentID := uuid.New()

	repo := &mockAgentRepo{
		findAgentByID: findAgentByIDReturning(agentdom.AgentTypeLLM),
		createSkill: func(_ context.Context, skill *agentdom.AgentSkill) error {
			if skill.AgentID != agentID {
				t.Fatalf("unexpected agentID")
			}
			return nil
		},
	}
	projRepo := &mockProjectRepo{}
	pluginRepo := &mockPluginRepo{}
	svc := New(repo, projRepo, nil, pluginRepo)

	result, err := svc.AddSkill(context.Background(), agentID, agentdom.AddSkillInput{
		SkillName:    "Test Skill",
		SkillSource:  "file",
		SkillContent: "skill content",
	})

	assert.NoError(t, err)
	assert.Equal(t, "Test Skill", result.SkillName)
}

func TestAddSkill_ReservedName_ReturnsError(t *testing.T) {
	reservedNames := []string{
		"paca-trigger-task-assigned",
		"paca-trigger-doc-comment",
		"paca-trigger-chat",
		"paca-trigger-description-write",
	}

	for _, name := range reservedNames {
		t.Run(name, func(t *testing.T) {
			agentID := uuid.New()
			repo := &mockAgentRepo{
				createSkill: func(_ context.Context, _ *agentdom.AgentSkill) error {
					t.Fatal("createSkill should not be called for a reserved name")
					return nil
				},
			}
			projRepo := &mockProjectRepo{}
			pluginRepo := &mockPluginRepo{}
			svc := New(repo, projRepo, nil, pluginRepo)

			result, err := svc.AddSkill(context.Background(), agentID, agentdom.AddSkillInput{
				SkillName:    name,
				SkillSource:  "file",
				SkillContent: "skill content",
			})

			assert.Nil(t, result)
			assert.ErrorIs(t, err, agentdom.ErrSkillNameReserved)
		})
	}
}

func TestGetConversation_Success(t *testing.T) {
	projectID := uuid.New()
	conversationID := uuid.New()
	conversation := &agentdom.AgentConversation{
		ID:        conversationID,
		ProjectID: projectID,
		Status:    "running",
	}

	repo := &mockAgentRepo{
		findConversationByID: func(_ context.Context, _ uuid.UUID) (*agentdom.AgentConversation, error) {
			return conversation, nil
		},
	}
	projRepo := &mockProjectRepo{}
	pluginRepo := &mockPluginRepo{}
	svc := New(repo, projRepo, nil, pluginRepo)

	result, err := svc.GetConversation(context.Background(), projectID, conversationID)

	assert.NoError(t, err)
	assert.Equal(t, conversationID, result.ID)
	assert.Equal(t, projectID, result.ProjectID)
}

func TestGetConversation_WrongProject(t *testing.T) {
	projectID := uuid.New()
	wrongProjectID := uuid.New()
	conversationID := uuid.New()
	conversation := &agentdom.AgentConversation{
		ID:        conversationID,
		ProjectID: wrongProjectID,
		Status:    "running",
	}

	repo := &mockAgentRepo{
		findConversationByID: func(_ context.Context, _ uuid.UUID) (*agentdom.AgentConversation, error) {
			return conversation, nil
		},
	}
	projRepo := &mockProjectRepo{}
	pluginRepo := &mockPluginRepo{}
	svc := New(repo, projRepo, nil, pluginRepo)

	_, err := svc.GetConversation(context.Background(), projectID, conversationID)

	assert.Error(t, err)
	assert.ErrorIs(t, err, agentdom.ErrConversationNotFound)
}

func TestGetGlobalConversation_Success(t *testing.T) {
	actorUserID := uuid.New()
	conversationID := uuid.New()
	conversation := &agentdom.AgentConversation{
		ID:          conversationID,
		ActorUserID: &actorUserID,
		Status:      "running",
	}

	repo := &mockAgentRepo{
		findConversationByID: func(_ context.Context, _ uuid.UUID) (*agentdom.AgentConversation, error) {
			return conversation, nil
		},
	}
	svc := New(repo, &mockProjectRepo{}, nil, &mockPluginRepo{})

	result, err := svc.GetGlobalConversation(context.Background(), conversationID, actorUserID)

	assert.NoError(t, err)
	assert.Equal(t, conversationID, result.ID)
}

// TestGetGlobalConversation_WrongActor is the regression test for the IDOR
// where GetGlobalConversation checked only "is this a global conversation"
// (ProjectID == uuid.Nil) and never "does it belong to the caller" — any
// authenticated user could read/stop/pause/heartbeat/message any other
// user's global conversation just by knowing its ID. See the doc comment on
// agentdom.Service.GetGlobalConversation.
func TestGetGlobalConversation_WrongActor(t *testing.T) {
	realOwner := uuid.New()
	attacker := uuid.New()
	conversationID := uuid.New()
	conversation := &agentdom.AgentConversation{
		ID:          conversationID,
		ActorUserID: &realOwner,
		Status:      "running",
	}

	repo := &mockAgentRepo{
		findConversationByID: func(_ context.Context, _ uuid.UUID) (*agentdom.AgentConversation, error) {
			return conversation, nil
		},
	}
	svc := New(repo, &mockProjectRepo{}, nil, &mockPluginRepo{})

	_, err := svc.GetGlobalConversation(context.Background(), conversationID, attacker)

	assert.Error(t, err)
	assert.ErrorIs(t, err, agentdom.ErrConversationNotFound)
}

// TestGetGlobalConversation_RejectsProjectScopedConversation guards the
// other half of the scope check: a project-scoped conversation must never
// be reachable through the global-chat endpoints, even if ActorUserID were
// somehow populated on it.
func TestGetGlobalConversation_RejectsProjectScopedConversation(t *testing.T) {
	actorUserID := uuid.New()
	projectID := uuid.New()
	conversationID := uuid.New()
	conversation := &agentdom.AgentConversation{
		ID:        conversationID,
		ProjectID: projectID,
		Status:    "running",
	}

	repo := &mockAgentRepo{
		findConversationByID: func(_ context.Context, _ uuid.UUID) (*agentdom.AgentConversation, error) {
			return conversation, nil
		},
	}
	svc := New(repo, &mockProjectRepo{}, nil, &mockPluginRepo{})

	_, err := svc.GetGlobalConversation(context.Background(), conversationID, actorUserID)

	assert.Error(t, err)
	assert.ErrorIs(t, err, agentdom.ErrConversationNotFound)
}

// TestGlobalConversationMutators_RejectWrongActor covers
// Stop/Pause/GlobalHeartbeat/SendGlobalConversationMessage — all four funnel
// through GetGlobalConversation for their existence+ownership gate, so a
// caller who isn't the conversation's actor must be denied by every one of
// them, not just the read path.
func TestGlobalConversationMutators_RejectWrongActor(t *testing.T) {
	realOwner := uuid.New()
	attacker := uuid.New()
	conversationID := uuid.New()
	conversation := &agentdom.AgentConversation{
		ID:          conversationID,
		ActorUserID: &realOwner,
		Status:      "running",
	}
	repo := &mockAgentRepo{
		findConversationByID: func(_ context.Context, _ uuid.UUID) (*agentdom.AgentConversation, error) {
			return conversation, nil
		},
		updateConversationStatus: func(_ context.Context, _ uuid.UUID, _ string) error {
			t.Fatal("must not mutate a conversation the caller doesn't own")
			return nil
		},
	}
	svc := New(repo, &mockProjectRepo{}, nil, &mockPluginRepo{})

	t.Run("stop", func(t *testing.T) {
		err := svc.StopGlobalConversation(context.Background(), conversationID, attacker)
		assert.ErrorIs(t, err, agentdom.ErrConversationNotFound)
	})
	t.Run("pause", func(t *testing.T) {
		err := svc.PauseGlobalConversation(context.Background(), conversationID, attacker)
		assert.ErrorIs(t, err, agentdom.ErrConversationNotFound)
	})
	t.Run("heartbeat", func(t *testing.T) {
		err := svc.GlobalHeartbeat(context.Background(), conversationID, attacker)
		assert.ErrorIs(t, err, agentdom.ErrConversationNotFound)
	})
	t.Run("send message", func(t *testing.T) {
		err := svc.SendGlobalConversationMessage(context.Background(), conversationID, "hi", attacker)
		assert.ErrorIs(t, err, agentdom.ErrConversationNotFound)
	})
}

func TestListConversations_Success(t *testing.T) {
	projectID := uuid.New()
	agentID := uuid.New()
	convs := []*agentdom.AgentConversation{
		{ID: uuid.New(), ProjectID: projectID, Status: "running"},
		{ID: uuid.New(), ProjectID: projectID, Status: "queued"},
	}

	var gotFilter agentdom.ListConversationsFilter
	var gotLimit int
	repo := &mockAgentRepo{
		listConversations: func(_ context.Context, filter agentdom.ListConversationsFilter, limit int) ([]*agentdom.AgentConversation, bool, error) {
			gotFilter = filter
			gotLimit = limit
			return convs, true, nil
		},
	}
	svc := New(repo, &mockProjectRepo{}, nil, &mockPluginRepo{})

	cursor := "some-cursor"
	filter := agentdom.ListConversationsFilter{ProjectID: &projectID, AgentIDs: []uuid.UUID{agentID}, CursorAfter: &cursor}
	result, hasMore, err := svc.ListConversations(context.Background(), filter, 20)

	assert.NoError(t, err)
	assert.True(t, hasMore)
	assert.Equal(t, convs, result)
	assert.Equal(t, 20, gotLimit)
	assert.Equal(t, &projectID, gotFilter.ProjectID)
	assert.Equal(t, []uuid.UUID{agentID}, gotFilter.AgentIDs)
	assert.Equal(t, &cursor, gotFilter.CursorAfter)
}

func TestListConversations_PropagatesRepoError(t *testing.T) {
	repo := &mockAgentRepo{
		listConversations: func(_ context.Context, _ agentdom.ListConversationsFilter, _ int) ([]*agentdom.AgentConversation, bool, error) {
			return nil, false, agentdom.ErrConversationInvalidCursor
		},
	}
	svc := New(repo, &mockProjectRepo{}, nil, &mockPluginRepo{})

	_, hasMore, err := svc.ListConversations(context.Background(), agentdom.ListConversationsFilter{}, 20)

	assert.ErrorIs(t, err, agentdom.ErrConversationInvalidCursor)
	assert.False(t, hasMore)
}

func TestListAgentActivities_Success(t *testing.T) {
	memberID := uuid.New()
	items := []*agentdom.ActivityFeedItem{
		{ID: uuid.New(), SourceType: agentdom.ActivitySourceTask, ActivityType: "task.created"},
		{ID: uuid.New(), SourceType: agentdom.ActivitySourceDoc, ActivityType: "doc.updated"},
	}

	var gotFilter agentdom.ListAgentActivitiesFilter
	var gotLimit int
	repo := &mockAgentRepo{
		listAgentActivities: func(_ context.Context, filter agentdom.ListAgentActivitiesFilter, limit int) ([]*agentdom.ActivityFeedItem, bool, error) {
			gotFilter = filter
			gotLimit = limit
			return items, true, nil
		},
	}
	svc := New(repo, &mockProjectRepo{}, nil, &mockPluginRepo{})

	filter := agentdom.ListAgentActivitiesFilter{ActorMemberID: memberID}
	result, hasMore, err := svc.ListAgentActivities(context.Background(), filter, 20)

	assert.NoError(t, err)
	assert.True(t, hasMore)
	assert.Equal(t, items, result)
	assert.Equal(t, 20, gotLimit)
	assert.Equal(t, memberID, gotFilter.ActorMemberID)
}

func TestListAgentActivities_PropagatesRepoError(t *testing.T) {
	repo := &mockAgentRepo{
		listAgentActivities: func(_ context.Context, _ agentdom.ListAgentActivitiesFilter, _ int) ([]*agentdom.ActivityFeedItem, bool, error) {
			return nil, false, agentdom.ErrActivityFeedInvalidCursor
		},
	}
	svc := New(repo, &mockProjectRepo{}, nil, &mockPluginRepo{})

	_, hasMore, err := svc.ListAgentActivities(context.Background(), agentdom.ListAgentActivitiesFilter{}, 20)

	assert.ErrorIs(t, err, agentdom.ErrActivityFeedInvalidCursor)
	assert.False(t, hasMore)
}

func TestSendConversationMessage_Success(t *testing.T) {
	projectID := uuid.New()
	conversationID := uuid.New()
	conversation := &agentdom.AgentConversation{
		ID:        conversationID,
		ProjectID: projectID,
		Status:    "running",
	}

	repo := &mockAgentRepo{
		findAgentByID: findAgentByIDReturning(agentdom.AgentTypeLLM),
		findConversationByID: func(_ context.Context, _ uuid.UUID) (*agentdom.AgentConversation, error) {
			return conversation, nil
		},
	}
	projRepo := &mockProjectRepo{}
	pluginRepo := &mockPluginRepo{}
	svc := New(repo, projRepo, nil, pluginRepo)

	err := svc.SendConversationMessage(context.Background(), projectID, conversationID, "test message", uuid.New())

	assert.NoError(t, err)
}

func TestSendConversationMessage_NotRunning(t *testing.T) {
	projectID := uuid.New()
	conversationID := uuid.New()
	conversation := &agentdom.AgentConversation{
		ID:        conversationID,
		ProjectID: projectID,
		Status:    "finished",
	}

	repo := &mockAgentRepo{
		findAgentByID: findAgentByIDReturning(agentdom.AgentTypeLLM),
		findConversationByID: func(_ context.Context, _ uuid.UUID) (*agentdom.AgentConversation, error) {
			return conversation, nil
		},
	}
	projRepo := &mockProjectRepo{}
	pluginRepo := &mockPluginRepo{}
	svc := New(repo, projRepo, nil, pluginRepo)

	err := svc.SendConversationMessage(context.Background(), projectID, conversationID, "test message", uuid.New())

	assert.Error(t, err)
	assert.ErrorIs(t, err, agentdom.ErrConversationNotRunning)
}

// TestSendConversationMessage_ACPResumesAnyTriggerType covers ACP agents'
// exception: a message can resume a conversation of *any* trigger type
// (task_assigned, comment_mention, etc.), not just chat_message, once it's
// no longer actively running — the local bridge daemon keeps it alive by
// conversation_id regardless of what started it.
func TestSendConversationMessage_ACPResumesAnyTriggerType(t *testing.T) {
	for _, status := range []string{"paused", "finished", "failed", "stopped"} {
		t.Run(status, func(t *testing.T) {
			projectID := uuid.New()
			conversationID := uuid.New()
			conversation := &agentdom.AgentConversation{
				ID:          conversationID,
				ProjectID:   projectID,
				TriggerType: "task_assigned",
				Status:      status,
			}

			var claimedFrom, claimedTo string
			repo := &mockAgentRepo{
				findAgentByID: findAgentByIDReturning(agentdom.AgentTypeACP),
				findConversationByID: func(_ context.Context, _ uuid.UUID) (*agentdom.AgentConversation, error) {
					return conversation, nil
				},
				claimConversationStatus: func(_ context.Context, id uuid.UUID, from, to string) (bool, error) {
					if id != conversationID {
						t.Fatalf("unexpected conversation id claimed: %s", id)
					}
					claimedFrom, claimedTo = from, to
					return true, nil
				},
			}
			projRepo := &mockProjectRepo{}
			pluginRepo := &mockPluginRepo{}
			svc := New(repo, projRepo, nil, pluginRepo)

			err := svc.SendConversationMessage(context.Background(), projectID, conversationID, "keep going", uuid.New())

			assert.NoError(t, err)
			assert.Equal(t, status, claimedFrom)
			assert.Equal(t, "running", claimedTo)
		})
	}
}

func TestSendConversationMessage_ACPBusyWhenRunning(t *testing.T) {
	projectID := uuid.New()
	conversationID := uuid.New()
	conversation := &agentdom.AgentConversation{
		ID:          conversationID,
		ProjectID:   projectID,
		TriggerType: "comment_mention",
		Status:      "running",
	}

	claimCalled := false
	repo := &mockAgentRepo{
		findAgentByID: findAgentByIDReturning(agentdom.AgentTypeACP),
		findConversationByID: func(_ context.Context, _ uuid.UUID) (*agentdom.AgentConversation, error) {
			return conversation, nil
		},
		claimConversationStatus: func(_ context.Context, _ uuid.UUID, _, _ string) (bool, error) {
			claimCalled = true
			return true, nil
		},
	}
	projRepo := &mockProjectRepo{}
	pluginRepo := &mockPluginRepo{}
	svc := New(repo, projRepo, nil, pluginRepo)

	err := svc.SendConversationMessage(context.Background(), projectID, conversationID, "are you there?", uuid.New())

	assert.ErrorIs(t, err, agentdom.ErrConversationBusy)
	assert.False(t, claimCalled, "must not attempt to claim/dispatch on top of an in-flight turn")
}

func TestSendConversationMessage_ACPBusyWhenQueued(t *testing.T) {
	projectID := uuid.New()
	conversationID := uuid.New()
	conversation := &agentdom.AgentConversation{
		ID:          conversationID,
		ProjectID:   projectID,
		TriggerType: "task_assigned",
		Status:      "queued",
	}

	repo := &mockAgentRepo{
		findAgentByID: findAgentByIDReturning(agentdom.AgentTypeACP),
		findConversationByID: func(_ context.Context, _ uuid.UUID) (*agentdom.AgentConversation, error) {
			return conversation, nil
		},
	}
	projRepo := &mockProjectRepo{}
	pluginRepo := &mockPluginRepo{}
	svc := New(repo, projRepo, nil, pluginRepo)

	err := svc.SendConversationMessage(context.Background(), projectID, conversationID, "are you there?", uuid.New())

	assert.ErrorIs(t, err, agentdom.ErrConversationBusy)
}

func TestSendConversationMessage_ACPResumeRaceLoses(t *testing.T) {
	projectID := uuid.New()
	conversationID := uuid.New()
	conversation := &agentdom.AgentConversation{
		ID:          conversationID,
		ProjectID:   projectID,
		TriggerType: "task_assigned",
		Status:      "finished",
	}

	repo := &mockAgentRepo{
		findAgentByID: findAgentByIDReturning(agentdom.AgentTypeACP),
		findConversationByID: func(_ context.Context, _ uuid.UUID) (*agentdom.AgentConversation, error) {
			return conversation, nil
		},
		claimConversationStatus: func(_ context.Context, _ uuid.UUID, _, _ string) (bool, error) {
			// Another concurrent request already claimed the resume.
			return false, nil
		},
	}
	projRepo := &mockProjectRepo{}
	pluginRepo := &mockPluginRepo{}
	svc := New(repo, projRepo, nil, pluginRepo)

	err := svc.SendConversationMessage(context.Background(), projectID, conversationID, "keep going", uuid.New())

	assert.ErrorIs(t, err, agentdom.ErrConversationBusy)
}

func TestStopConversation_Success(t *testing.T) {
	projectID := uuid.New()
	conversationID := uuid.New()
	conversation := &agentdom.AgentConversation{
		ID:        conversationID,
		ProjectID: projectID,
		Status:    "running",
	}
	var updatedStatus string

	repo := &mockAgentRepo{
		findConversationByID: func(_ context.Context, _ uuid.UUID) (*agentdom.AgentConversation, error) {
			return conversation, nil
		},
		updateConversationStatus: func(_ context.Context, _ uuid.UUID, status string) error {
			updatedStatus = status
			return nil
		},
	}
	projRepo := &mockProjectRepo{}
	pluginRepo := &mockPluginRepo{}
	svc := New(repo, projRepo, nil, pluginRepo)

	err := svc.StopConversation(context.Background(), projectID, conversationID)

	assert.NoError(t, err)
	assert.Equal(t, "stopped", updatedStatus)
}

func TestStopConversation_AlreadyStopped(t *testing.T) {
	for _, status := range []string{"finished", "stopped", "failed"} {
		t.Run(status, func(t *testing.T) {
			projectID := uuid.New()
			conversationID := uuid.New()
			conversation := &agentdom.AgentConversation{
				ID:        conversationID,
				ProjectID: projectID,
				Status:    status,
			}
			updateCalled := false

			repo := &mockAgentRepo{
				findConversationByID: func(_ context.Context, _ uuid.UUID) (*agentdom.AgentConversation, error) {
					return conversation, nil
				},
				updateConversationStatus: func(_ context.Context, _ uuid.UUID, _ string) error {
					updateCalled = true
					return nil
				},
			}
			projRepo := &mockProjectRepo{}
			pluginRepo := &mockPluginRepo{}
			svc := New(repo, projRepo, nil, pluginRepo)

			err := svc.StopConversation(context.Background(), projectID, conversationID)

			assert.Error(t, err)
			assert.ErrorIs(t, err, agentdom.ErrConversationAlreadyStopped)
			assert.False(t, updateCalled)
		})
	}
}

func TestPauseConversation_Success(t *testing.T) {
	projectID := uuid.New()
	conversationID := uuid.New()
	conversation := &agentdom.AgentConversation{
		ID:        conversationID,
		ProjectID: projectID,
		Status:    "running",
	}
	updateCalled := false

	repo := &mockAgentRepo{
		findConversationByID: func(_ context.Context, _ uuid.UUID) (*agentdom.AgentConversation, error) {
			return conversation, nil
		},
		updateConversationStatus: func(_ context.Context, _ uuid.UUID, _ string) error {
			updateCalled = true
			return nil
		},
	}
	projRepo := &mockProjectRepo{}
	pluginRepo := &mockPluginRepo{}
	svc := New(repo, projRepo, nil, pluginRepo)

	err := svc.PauseConversation(context.Background(), projectID, conversationID)

	// No DB write: ai-agent owns writing "paused" itself once the turn
	// actually pauses, so PauseConversation must not touch Postgres.
	assert.NoError(t, err)
	assert.False(t, updateCalled)
}

func TestPauseConversation_NotRunning(t *testing.T) {
	projectID := uuid.New()
	conversationID := uuid.New()
	conversation := &agentdom.AgentConversation{
		ID:        conversationID,
		ProjectID: projectID,
		Status:    "paused",
	}

	repo := &mockAgentRepo{
		findConversationByID: func(_ context.Context, _ uuid.UUID) (*agentdom.AgentConversation, error) {
			return conversation, nil
		},
	}
	projRepo := &mockProjectRepo{}
	pluginRepo := &mockPluginRepo{}
	svc := New(repo, projRepo, nil, pluginRepo)

	err := svc.PauseConversation(context.Background(), projectID, conversationID)

	assert.Error(t, err)
	assert.ErrorIs(t, err, agentdom.ErrConversationNotRunning)
}

func TestHeartbeat_Success(t *testing.T) {
	projectID := uuid.New()
	conversationID := uuid.New()
	conversation := &agentdom.AgentConversation{
		ID:        conversationID,
		ProjectID: projectID,
		Status:    "running",
	}
	updateCalled := false

	repo := &mockAgentRepo{
		findConversationByID: func(_ context.Context, _ uuid.UUID) (*agentdom.AgentConversation, error) {
			return conversation, nil
		},
		updateConversationStatus: func(_ context.Context, _ uuid.UUID, _ string) error {
			updateCalled = true
			return nil
		},
	}
	projRepo := &mockProjectRepo{}
	pluginRepo := &mockPluginRepo{}
	svc := New(repo, projRepo, nil, pluginRepo)

	err := svc.Heartbeat(context.Background(), projectID, conversationID)

	// Heartbeat fires every ~30s per open tab — no Postgres round trip beyond
	// the ownership lookup.
	assert.NoError(t, err)
	assert.False(t, updateCalled)
}

func TestHeartbeat_WrongProject(t *testing.T) {
	projectID := uuid.New()
	wrongProjectID := uuid.New()
	conversationID := uuid.New()
	conversation := &agentdom.AgentConversation{
		ID:        conversationID,
		ProjectID: wrongProjectID,
		Status:    "running",
	}

	repo := &mockAgentRepo{
		findConversationByID: func(_ context.Context, _ uuid.UUID) (*agentdom.AgentConversation, error) {
			return conversation, nil
		},
	}
	projRepo := &mockProjectRepo{}
	pluginRepo := &mockPluginRepo{}
	svc := New(repo, projRepo, nil, pluginRepo)

	err := svc.Heartbeat(context.Background(), projectID, conversationID)

	// A conversation belonging to a different project must not be kept alive
	// by a heartbeat scoped to this project.
	assert.ErrorIs(t, err, agentdom.ErrConversationNotFound)
}

func TestListChatSessions_Success(t *testing.T) {
	agentID := uuid.New()
	memberID := uuid.New()
	sessions := []*agentdom.AgentChatSession{
		{ID: uuid.New(), AgentID: agentID, MemberID: memberID},
		{ID: uuid.New(), AgentID: agentID, MemberID: memberID},
	}

	repo := &mockAgentRepo{
		listChatSessions: func(_ context.Context, aid, mid uuid.UUID) ([]*agentdom.AgentChatSession, error) {
			if aid != agentID || mid != memberID {
				t.Fatalf("unexpected agentID or memberID")
			}
			return sessions, nil
		},
	}
	projRepo := &mockProjectRepo{}
	pluginRepo := &mockPluginRepo{}
	svc := New(repo, projRepo, nil, pluginRepo)

	result, err := svc.ListChatSessions(context.Background(), uuid.Nil, agentID, memberID)

	assert.NoError(t, err)
	assert.Len(t, result, 2)
}

func TestStartChatSession_Success(t *testing.T) {
	projectID := uuid.New()
	agentID := uuid.New()
	memberID := uuid.New()

	repo := &mockAgentRepo{
		createChatSession: func(_ context.Context, session *agentdom.AgentChatSession) error {
			if session.AgentID != agentID || session.ProjectID != projectID || session.MemberID != memberID {
				t.Fatalf("unexpected session fields")
			}
			return nil
		},
		createConversation: func(_ context.Context, conv *agentdom.AgentConversation) error {
			if conv.AgentID != agentID || conv.ProjectID != projectID || conv.TriggeredByMemberID == nil || *conv.TriggeredByMemberID != memberID {
				t.Fatalf("unexpected conversation fields")
			}
			return nil
		},
	}
	projRepo := &mockProjectRepo{}
	pluginRepo := &mockPluginRepo{}
	svc := New(repo, projRepo, nil, pluginRepo)

	resultSession, resultConv, err := svc.StartChatSession(context.Background(), projectID, agentID, memberID, "Hello")

	assert.NoError(t, err)
	assert.NotNil(t, resultSession)
	assert.NotNil(t, resultConv)
	assert.Equal(t, agentID, resultSession.AgentID)
	assert.Equal(t, projectID, resultSession.ProjectID)
}

func TestSendChatMessage_Success(t *testing.T) {
	projectID := uuid.New()
	agentID := uuid.New()
	memberID := uuid.New()
	sessionID := uuid.New()
	session := &agentdom.AgentChatSession{
		ID:        sessionID,
		AgentID:   agentID,
		ProjectID: projectID,
	}

	repo := &mockAgentRepo{
		findChatSessionByID: func(_ context.Context, _ uuid.UUID) (*agentdom.AgentChatSession, error) {
			return session, nil
		},
		createConversation: func(_ context.Context, conv *agentdom.AgentConversation) error {
			if conv.AgentID != agentID || conv.ProjectID != projectID || conv.TriggeredByMemberID == nil || *conv.TriggeredByMemberID != memberID {
				t.Fatalf("unexpected conversation fields")
			}
			return nil
		},
		updateChatSession: func(_ context.Context, _ *agentdom.AgentChatSession) error {
			return nil
		},
	}
	projRepo := &mockProjectRepo{}
	pluginRepo := &mockPluginRepo{}
	svc := New(repo, projRepo, nil, pluginRepo)

	resultConv, err := svc.SendChatMessage(context.Background(), projectID, sessionID, memberID, "Hello")

	assert.NoError(t, err)
	assert.NotNil(t, resultConv)
	assert.Equal(t, agentID, resultConv.AgentID)
}

func TestSendChatMessage_ResumesPausedConversation(t *testing.T) {
	projectID := uuid.New()
	agentID := uuid.New()
	memberID := uuid.New()
	sessionID := uuid.New()
	pausedConvID := uuid.New()
	session := &agentdom.AgentChatSession{
		ID:        sessionID,
		AgentID:   agentID,
		ProjectID: projectID,
	}
	paused := &agentdom.AgentConversation{
		ID:            pausedConvID,
		AgentID:       agentID,
		ProjectID:     projectID,
		ChatSessionID: &sessionID,
		Status:        "paused",
	}

	createCalled := false
	var claimedFrom, claimedTo string
	repo := &mockAgentRepo{
		findChatSessionByID: func(_ context.Context, _ uuid.UUID) (*agentdom.AgentChatSession, error) {
			return session, nil
		},
		findLatestConversationBySession: func(_ context.Context, _ uuid.UUID) (*agentdom.AgentConversation, error) {
			return paused, nil
		},
		claimConversationStatus: func(_ context.Context, id uuid.UUID, from, to string) (bool, error) {
			if id != pausedConvID {
				t.Fatalf("unexpected conversation id claimed: %s", id)
			}
			claimedFrom, claimedTo = from, to
			return true, nil
		},
		createConversation: func(_ context.Context, _ *agentdom.AgentConversation) error {
			createCalled = true
			return nil
		},
		updateChatSession: func(_ context.Context, _ *agentdom.AgentChatSession) error {
			return nil
		},
	}
	projRepo := &mockProjectRepo{}
	pluginRepo := &mockPluginRepo{}
	svc := New(repo, projRepo, nil, pluginRepo)

	resultConv, err := svc.SendChatMessage(context.Background(), projectID, sessionID, memberID, "Continuing…")

	assert.NoError(t, err)
	assert.False(t, createCalled, "resuming a paused conversation must not create a new one")
	assert.Equal(t, pausedConvID, resultConv.ID)
	assert.Equal(t, "paused", claimedFrom)
	assert.Equal(t, "running", claimedTo)
}

// TestSendChatMessage_ACPResumesTerminalConversation covers the ACP-specific
// exception to IsTerminal: replying to a finished/failed/stopped
// conversation must resume the same conversation_id (the local bridge daemon
// keeps it alive indefinitely) instead of creating a new one.
func TestSendChatMessage_ACPResumesTerminalConversation(t *testing.T) {
	for _, status := range []string{"finished", "failed", "stopped"} {
		t.Run(status, func(t *testing.T) {
			projectID := uuid.New()
			agentID := uuid.New()
			memberID := uuid.New()
			sessionID := uuid.New()
			terminalConvID := uuid.New()
			session := &agentdom.AgentChatSession{
				ID:        sessionID,
				AgentID:   agentID,
				ProjectID: projectID,
			}
			terminal := &agentdom.AgentConversation{
				ID:            terminalConvID,
				AgentID:       agentID,
				ProjectID:     projectID,
				ChatSessionID: &sessionID,
				Status:        status,
			}

			createCalled := false
			var claimedFrom, claimedTo string
			repo := &mockAgentRepo{
				findAgentByID: findAgentByIDReturning(agentdom.AgentTypeACP),
				findChatSessionByID: func(_ context.Context, _ uuid.UUID) (*agentdom.AgentChatSession, error) {
					return session, nil
				},
				findLatestConversationBySession: func(_ context.Context, _ uuid.UUID) (*agentdom.AgentConversation, error) {
					return terminal, nil
				},
				claimConversationStatus: func(_ context.Context, id uuid.UUID, from, to string) (bool, error) {
					if id != terminalConvID {
						t.Fatalf("unexpected conversation id claimed: %s", id)
					}
					claimedFrom, claimedTo = from, to
					return true, nil
				},
				createConversation: func(_ context.Context, _ *agentdom.AgentConversation) error {
					createCalled = true
					return nil
				},
				updateChatSession: func(_ context.Context, _ *agentdom.AgentChatSession) error {
					return nil
				},
			}
			projRepo := &mockProjectRepo{}
			pluginRepo := &mockPluginRepo{}
			svc := New(repo, projRepo, nil, pluginRepo)

			resultConv, err := svc.SendChatMessage(context.Background(), projectID, sessionID, memberID, "Continuing…")

			assert.NoError(t, err)
			assert.False(t, createCalled, "resuming a terminal ACP conversation must not create a new one")
			assert.Equal(t, terminalConvID, resultConv.ID)
			assert.Equal(t, status, claimedFrom)
			assert.Equal(t, "running", claimedTo)
		})
	}
}

// TestSendChatMessage_ACPResumeRaceLoses mirrors
// TestSendChatMessage_ResumeRaceLoses for the terminal-ACP resume path: two
// concurrent replies to the same terminal conversation must not both win.
func TestSendChatMessage_ACPResumeRaceLoses(t *testing.T) {
	projectID := uuid.New()
	agentID := uuid.New()
	memberID := uuid.New()
	sessionID := uuid.New()
	terminalConvID := uuid.New()
	session := &agentdom.AgentChatSession{
		ID:        sessionID,
		AgentID:   agentID,
		ProjectID: projectID,
	}
	terminal := &agentdom.AgentConversation{
		ID:            terminalConvID,
		AgentID:       agentID,
		ProjectID:     projectID,
		ChatSessionID: &sessionID,
		Status:        "finished",
	}

	repo := &mockAgentRepo{
		findAgentByID: findAgentByIDReturning(agentdom.AgentTypeACP),
		findChatSessionByID: func(_ context.Context, _ uuid.UUID) (*agentdom.AgentChatSession, error) {
			return session, nil
		},
		findLatestConversationBySession: func(_ context.Context, _ uuid.UUID) (*agentdom.AgentConversation, error) {
			return terminal, nil
		},
		claimConversationStatus: func(_ context.Context, _ uuid.UUID, _, _ string) (bool, error) {
			// Another concurrent request already claimed the resume.
			return false, nil
		},
	}
	projRepo := &mockProjectRepo{}
	pluginRepo := &mockPluginRepo{}
	svc := New(repo, projRepo, nil, pluginRepo)

	_, err := svc.SendChatMessage(context.Background(), projectID, sessionID, memberID, "Continuing…")

	assert.ErrorIs(t, err, agentdom.ErrConversationBusy)
}

// TestSendChatMessage_LLMTerminalCreatesNewConversation is a regression guard
// for the non-ACP path: an LLM agent's terminal conversation must still
// create a brand new conversation, unlike the ACP resume behavior above.
func TestSendChatMessage_LLMTerminalCreatesNewConversation(t *testing.T) {
	projectID := uuid.New()
	agentID := uuid.New()
	memberID := uuid.New()
	sessionID := uuid.New()
	oldConvID := uuid.New()
	session := &agentdom.AgentChatSession{
		ID:        sessionID,
		AgentID:   agentID,
		ProjectID: projectID,
	}
	finished := &agentdom.AgentConversation{
		ID:            oldConvID,
		AgentID:       agentID,
		ProjectID:     projectID,
		ChatSessionID: &sessionID,
		Status:        "finished",
	}

	createCalled := false
	claimCalled := false
	repo := &mockAgentRepo{
		findAgentByID: findAgentByIDReturning(agentdom.AgentTypeLLM),
		findChatSessionByID: func(_ context.Context, _ uuid.UUID) (*agentdom.AgentChatSession, error) {
			return session, nil
		},
		findLatestConversationBySession: func(_ context.Context, _ uuid.UUID) (*agentdom.AgentConversation, error) {
			return finished, nil
		},
		claimConversationStatus: func(_ context.Context, _ uuid.UUID, _, _ string) (bool, error) {
			claimCalled = true
			return true, nil
		},
		createConversation: func(_ context.Context, conv *agentdom.AgentConversation) error {
			createCalled = true
			if conv.ID == oldConvID {
				t.Fatalf("expected a freshly generated conversation id, got the old one")
			}
			return nil
		},
		updateChatSession: func(_ context.Context, _ *agentdom.AgentChatSession) error {
			return nil
		},
	}
	projRepo := &mockProjectRepo{}
	pluginRepo := &mockPluginRepo{}
	svc := New(repo, projRepo, nil, pluginRepo)

	resultConv, err := svc.SendChatMessage(context.Background(), projectID, sessionID, memberID, "Hello again")

	assert.NoError(t, err)
	assert.True(t, createCalled, "a terminal LLM conversation must create a new conversation")
	assert.False(t, claimCalled, "must not attempt to claim/resume a terminal LLM conversation")
	assert.NotEqual(t, oldConvID, resultConv.ID)
}

func TestSendChatMessage_ResumeRaceLoses(t *testing.T) {
	projectID := uuid.New()
	agentID := uuid.New()
	memberID := uuid.New()
	sessionID := uuid.New()
	pausedConvID := uuid.New()
	session := &agentdom.AgentChatSession{
		ID:        sessionID,
		AgentID:   agentID,
		ProjectID: projectID,
	}
	paused := &agentdom.AgentConversation{
		ID:            pausedConvID,
		AgentID:       agentID,
		ProjectID:     projectID,
		ChatSessionID: &sessionID,
		Status:        "paused",
	}

	repo := &mockAgentRepo{
		findChatSessionByID: func(_ context.Context, _ uuid.UUID) (*agentdom.AgentChatSession, error) {
			return session, nil
		},
		findLatestConversationBySession: func(_ context.Context, _ uuid.UUID) (*agentdom.AgentConversation, error) {
			return paused, nil
		},
		claimConversationStatus: func(_ context.Context, _ uuid.UUID, _, _ string) (bool, error) {
			// Another concurrent request already claimed the resume.
			return false, nil
		},
	}
	projRepo := &mockProjectRepo{}
	pluginRepo := &mockPluginRepo{}
	svc := New(repo, projRepo, nil, pluginRepo)

	_, err := svc.SendChatMessage(context.Background(), projectID, sessionID, memberID, "Continuing…")

	assert.ErrorIs(t, err, agentdom.ErrConversationBusy)
}

func TestSendChatMessage_BusyWhenQueued(t *testing.T) {
	projectID := uuid.New()
	agentID := uuid.New()
	memberID := uuid.New()
	sessionID := uuid.New()
	session := &agentdom.AgentChatSession{
		ID:        sessionID,
		AgentID:   agentID,
		ProjectID: projectID,
	}
	queued := &agentdom.AgentConversation{
		ID:        uuid.New(),
		AgentID:   agentID,
		ProjectID: projectID,
		Status:    "queued",
	}

	repo := &mockAgentRepo{
		findChatSessionByID: func(_ context.Context, _ uuid.UUID) (*agentdom.AgentChatSession, error) {
			return session, nil
		},
		findLatestConversationBySession: func(_ context.Context, _ uuid.UUID) (*agentdom.AgentConversation, error) {
			return queued, nil
		},
	}
	projRepo := &mockProjectRepo{}
	pluginRepo := &mockPluginRepo{}
	svc := New(repo, projRepo, nil, pluginRepo)

	// A conversation that hasn't been dequeued yet must not let a second
	// message create a duplicate conversation/sandbox for the same session.
	_, err := svc.SendChatMessage(context.Background(), projectID, sessionID, memberID, "Are you there?")

	assert.ErrorIs(t, err, agentdom.ErrConversationBusy)
}

func TestSendChatMessage_BusyWhenRunning(t *testing.T) {
	projectID := uuid.New()
	agentID := uuid.New()
	memberID := uuid.New()
	sessionID := uuid.New()
	session := &agentdom.AgentChatSession{
		ID:        sessionID,
		AgentID:   agentID,
		ProjectID: projectID,
	}
	running := &agentdom.AgentConversation{
		ID:        uuid.New(),
		AgentID:   agentID,
		ProjectID: projectID,
		Status:    "running",
	}

	repo := &mockAgentRepo{
		findChatSessionByID: func(_ context.Context, _ uuid.UUID) (*agentdom.AgentChatSession, error) {
			return session, nil
		},
		findLatestConversationBySession: func(_ context.Context, _ uuid.UUID) (*agentdom.AgentConversation, error) {
			return running, nil
		},
	}
	projRepo := &mockProjectRepo{}
	pluginRepo := &mockPluginRepo{}
	svc := New(repo, projRepo, nil, pluginRepo)

	_, err := svc.SendChatMessage(context.Background(), projectID, sessionID, memberID, "Are you there?")

	assert.ErrorIs(t, err, agentdom.ErrConversationBusy)
}

func TestSendChatMessage_WrongProject(t *testing.T) {
	projectID := uuid.New()
	wrongProjectID := uuid.New()
	agentID := uuid.New()
	memberID := uuid.New()
	sessionID := uuid.New()
	session := &agentdom.AgentChatSession{
		ID:        sessionID,
		AgentID:   agentID,
		ProjectID: wrongProjectID,
	}

	repo := &mockAgentRepo{
		findChatSessionByID: func(_ context.Context, _ uuid.UUID) (*agentdom.AgentChatSession, error) {
			return session, nil
		},
	}
	projRepo := &mockProjectRepo{}
	pluginRepo := &mockPluginRepo{}
	svc := New(repo, projRepo, nil, pluginRepo)

	_, err := svc.SendChatMessage(context.Background(), projectID, sessionID, memberID, "Hello")

	assert.Error(t, err)
	assert.ErrorIs(t, err, agentdom.ErrChatSessionNotFound)
}

func TestDeleteMCPServer_Success(t *testing.T) {
	agentID := uuid.New()
	serverID := uuid.New()
	server := &agentdom.AgentMCPServer{
		ID:      serverID,
		AgentID: agentID,
	}

	repo := &mockAgentRepo{
		findAgentByID: findAgentByIDReturning(agentdom.AgentTypeLLM),
		findMCPServerByID: func(_ context.Context, _ uuid.UUID) (*agentdom.AgentMCPServer, error) {
			return server, nil
		},
		deleteMCPServer: func(_ context.Context, id uuid.UUID) error {
			if id != serverID {
				t.Fatalf("unexpected server ID")
			}
			return nil
		},
	}
	projRepo := &mockProjectRepo{}
	pluginRepo := &mockPluginRepo{}
	svc := New(repo, projRepo, nil, pluginRepo)

	err := svc.DeleteMCPServer(context.Background(), agentID, serverID)

	assert.NoError(t, err)
}

func TestUpdateSkill_Success(t *testing.T) {
	agentID := uuid.New()
	skillID := uuid.New()
	skill := &agentdom.AgentSkill{
		ID:           skillID,
		AgentID:      agentID,
		SkillName:    "Old Skill",
		SkillContent: "old content",
	}

	repo := &mockAgentRepo{
		findAgentByID: findAgentByIDReturning(agentdom.AgentTypeLLM),
		findSkillByID: func(_ context.Context, _ uuid.UUID) (*agentdom.AgentSkill, error) {
			return skill, nil
		},
		updateSkill: func(_ context.Context, s *agentdom.AgentSkill) error {
			if s.ID != skillID || s.AgentID != agentID {
				t.Fatalf("unexpected skill ID or agent ID")
			}
			return nil
		},
	}
	projRepo := &mockProjectRepo{}
	pluginRepo := &mockPluginRepo{}
	svc := New(repo, projRepo, nil, pluginRepo)

	newContent := "new content"

	result, err := svc.UpdateSkill(context.Background(), agentID, skillID, agentdom.UpdateSkillInput{
		SkillContent: &newContent,
	})

	assert.NoError(t, err)
	assert.Equal(t, newContent, result.SkillContent)
}

func TestTriggerTaskAssigned_Success(t *testing.T) {
	projectID := uuid.New()
	agentID := uuid.New()
	taskID := uuid.New()
	memberID := uuid.New()

	repo := &mockAgentRepo{
		createConversation: func(_ context.Context, conv *agentdom.AgentConversation) error {
			if conv.AgentID != agentID || conv.ProjectID != projectID || conv.TriggeredByMemberID == nil || *conv.TriggeredByMemberID != memberID {
				t.Fatalf("unexpected conversation fields")
			}
			return nil
		},
	}
	projRepo := &mockProjectRepo{}
	pluginRepo := &mockPluginRepo{}
	svc := New(repo, projRepo, nil, pluginRepo)

	result, err := svc.TriggerTaskAssigned(context.Background(), projectID, agentID, taskID, &memberID, "")

	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, "task_assigned", result.TriggerType)
}

func TestTriggerDirectMessage_Success(t *testing.T) {
	projectID := uuid.New()
	agentID := uuid.New()

	repo := &mockAgentRepo{
		createConversation: func(_ context.Context, conv *agentdom.AgentConversation) error {
			if conv.AgentID != agentID || conv.ProjectID != projectID || conv.TaskID != nil {
				t.Fatalf("unexpected conversation fields: %+v", conv)
			}
			return nil
		},
	}
	projRepo := &mockProjectRepo{}
	pluginRepo := &mockPluginRepo{}
	svc := New(repo, projRepo, nil, pluginRepo)

	result, err := svc.TriggerDirectMessage(context.Background(), projectID, agentID, nil, "do the thing")

	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, "automation_message", result.TriggerType)
	assert.Nil(t, result.TaskID)
}

func TestTriggerCommentMention_Success(t *testing.T) {
	projectID := uuid.New()
	agentID := uuid.New()
	taskID := uuid.New()
	commentID := uuid.New()
	memberID := uuid.New()

	repo := &mockAgentRepo{
		createConversation: func(_ context.Context, conv *agentdom.AgentConversation) error {
			if conv.AgentID != agentID || conv.ProjectID != projectID || conv.TriggeredByMemberID == nil || *conv.TriggeredByMemberID != memberID {
				t.Fatalf("unexpected conversation fields")
			}
			return nil
		},
	}
	projRepo := &mockProjectRepo{}
	pluginRepo := &mockPluginRepo{}
	svc := New(repo, projRepo, nil, pluginRepo)

	result, err := svc.TriggerCommentMention(context.Background(), projectID, agentID, taskID, commentID, memberID, "test comment")

	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, "comment_mention", result.TriggerType)
}

func TestCreateAgent_ACPInvalidAgentType(t *testing.T) {
	projectID := uuid.New()

	repo := &mockAgentRepo{
		findAgentByHandle: func(_ context.Context, _ uuid.UUID, _ string) (*agentdom.Agent, error) {
			return nil, agentdom.ErrAgentNotFound
		},
	}
	svc := New(repo, &mockProjectRepo{}, nil, &mockPluginRepo{})

	_, err := svc.CreateAgent(context.Background(), projectID, agentdom.CreateAgentInput{
		Name:      "Bad Agent",
		Handle:    "bad-agent",
		AgentType: "not-a-real-type",
	})

	assert.ErrorIs(t, err, agentdom.ErrAgentTypeInvalid)
}

func TestCreateAgent_ACPMissingProvider(t *testing.T) {
	projectID := uuid.New()

	repo := &mockAgentRepo{
		findAgentByHandle: func(_ context.Context, _ uuid.UUID, _ string) (*agentdom.Agent, error) {
			return nil, agentdom.ErrAgentNotFound
		},
	}
	svc := New(repo, &mockProjectRepo{}, nil, &mockPluginRepo{})

	_, err := svc.CreateAgent(context.Background(), projectID, agentdom.CreateAgentInput{
		Name:      "ACP Agent",
		Handle:    "acp-agent",
		AgentType: agentdom.AgentTypeACP,
	})

	assert.ErrorIs(t, err, agentdom.ErrACPProviderInvalid)
}

func TestCreateAgent_ACPCustomProviderMissingCommand(t *testing.T) {
	projectID := uuid.New()

	repo := &mockAgentRepo{
		findAgentByHandle: func(_ context.Context, _ uuid.UUID, _ string) (*agentdom.Agent, error) {
			return nil, agentdom.ErrAgentNotFound
		},
	}
	svc := New(repo, &mockProjectRepo{}, nil, &mockPluginRepo{})

	_, err := svc.CreateAgent(context.Background(), projectID, agentdom.CreateAgentInput{
		Name:        "Custom ACP Agent",
		Handle:      "custom-acp-agent",
		AgentType:   agentdom.AgentTypeACP,
		ACPProvider: agentdom.ACPProviderCustom,
	})

	assert.ErrorIs(t, err, agentdom.ErrACPCommandRequired)
}

func TestCreateAgent_ACPCustomProviderSuccess(t *testing.T) {
	projectID := uuid.New()
	projectRoleID := uuid.New()

	repo := &mockAgentRepo{
		findAgentByHandle: func(_ context.Context, _ uuid.UUID, _ string) (*agentdom.Agent, error) {
			return nil, agentdom.ErrAgentNotFound
		},
		createAgentWithMembership: func(_ context.Context, _ *agentdom.Agent, _ uuid.UUID, _, _ uuid.UUID) error {
			return nil
		},
	}
	svc := New(repo, &mockProjectRepo{}, nil, &mockPluginRepo{})

	result, err := svc.CreateAgent(context.Background(), projectID, agentdom.CreateAgentInput{
		Name:          "Custom ACP Agent",
		Handle:        "custom-acp-agent",
		AgentType:     agentdom.AgentTypeACP,
		ACPProvider:   agentdom.ACPProviderCustom,
		ACPCommand:    []string{"npx", "-y", "my-acp-server"},
		ProjectRoleID: projectRoleID,
	})

	assert.NoError(t, err)
	assert.Equal(t, agentdom.AgentTypeACP, result.AgentType)
	assert.Equal(t, []string{"npx", "-y", "my-acp-server"}, result.ACPCommand)
}

func TestCreateAgent_ACPIgnoresSystemPromptAndGitCommitterFields(t *testing.T) {
	projectID := uuid.New()
	projectRoleID := uuid.New()

	repo := &mockAgentRepo{
		findAgentByHandle: func(_ context.Context, _ uuid.UUID, _ string) (*agentdom.Agent, error) {
			return nil, agentdom.ErrAgentNotFound
		},
		createAgentWithMembership: func(_ context.Context, _ *agentdom.Agent, _ uuid.UUID, _, _ uuid.UUID) error {
			return nil
		},
	}
	svc := New(repo, &mockProjectRepo{}, nil, &mockPluginRepo{})

	result, err := svc.CreateAgent(context.Background(), projectID, agentdom.CreateAgentInput{
		Name:              "ACP Agent",
		Handle:            "acp-agent",
		AgentType:         agentdom.AgentTypeACP,
		ACPProvider:       agentdom.ACPProviderClaudeCode,
		ProjectRoleID:     projectRoleID,
		SystemPrompt:      "you are a helpful assistant",
		GitCommitterName:  "someone",
		GitCommitterEmail: "someone@example.com",
	})

	assert.NoError(t, err)
	assert.Empty(t, result.SystemPrompt)
	assert.Empty(t, result.GitCommitterName)
	assert.Empty(t, result.GitCommitterEmail)
}

func TestGenerateACPBridgeToken_Success(t *testing.T) {
	projectID := uuid.New()
	agentID := uuid.New()
	provider := agentdom.ACPProviderClaudeCode
	agent := &agentdom.Agent{
		ID:          agentID,
		ProjectID:   projectID,
		AgentType:   agentdom.AgentTypeACP,
		ACPProvider: &provider,
	}

	var storedHash string
	repo := &mockAgentRepo{
		findAgentByID: func(_ context.Context, _ uuid.UUID) (*agentdom.Agent, error) {
			return agent, nil
		},
	}
	repo.setACPBridgeTokenHash = func(_ context.Context, id uuid.UUID, hash string) error {
		if id != agentID {
			t.Fatalf("expected agentID %v, got %v", agentID, id)
		}
		storedHash = hash
		return nil
	}
	svc := New(repo, &mockProjectRepo{}, nil, &mockPluginRepo{})

	token, err := svc.GenerateACPBridgeToken(context.Background(), projectID, agentID)

	assert.NoError(t, err)
	assert.NotEmpty(t, token)
	assert.NotEmpty(t, storedHash)
	// The stored value must be a hash, never the plaintext token itself.
	assert.NotEqual(t, token, storedHash)
}

func TestGenerateACPBridgeToken_NonACPAgent(t *testing.T) {
	projectID := uuid.New()
	agentID := uuid.New()
	agent := &agentdom.Agent{
		ID:        agentID,
		ProjectID: projectID,
		AgentType: agentdom.AgentTypeLLM,
	}

	repo := &mockAgentRepo{
		findAgentByID: func(_ context.Context, _ uuid.UUID) (*agentdom.Agent, error) {
			return agent, nil
		},
	}
	svc := New(repo, &mockProjectRepo{}, nil, &mockPluginRepo{})

	_, err := svc.GenerateACPBridgeToken(context.Background(), projectID, agentID)

	assert.ErrorIs(t, err, agentdom.ErrAgentTypeInvalid)
}

func TestGenerateACPBridgeToken_WrongProject(t *testing.T) {
	projectID := uuid.New()
	agentID := uuid.New()

	// Project-scope enforcement now lives in the repo's
	// FindVisibleAgentInProject join (see TestGetAgent_WrongProject) — a
	// project the agent isn't visible in simply yields no row.
	repo := &mockAgentRepo{
		findVisibleAgentInProject: func(context.Context, uuid.UUID, uuid.UUID) (*agentdom.Agent, error) {
			return nil, agentdom.ErrAgentNotFound
		},
	}
	svc := New(repo, &mockProjectRepo{}, nil, &mockPluginRepo{})

	_, err := svc.GenerateACPBridgeToken(context.Background(), projectID, agentID)

	assert.ErrorIs(t, err, agentdom.ErrAgentNotFound)
}

// -------------------------------------------------------------------------
// GenerateAgentMCPKey / GenerateGlobalAgentMCPKey — same shape as
// GenerateACPBridgeToken above, but persisted via SetMCPAPIKeyHash and
// resolved later by FindAgentByMCPAPIKeyHash (see the authn middleware's
// agentClaimsForKey).
// -------------------------------------------------------------------------

func TestGenerateAgentMCPKey_Success(t *testing.T) {
	projectID := uuid.New()
	agentID := uuid.New()
	provider := agentdom.ACPProviderClaudeCode
	agent := &agentdom.Agent{
		ID:          agentID,
		ProjectID:   projectID,
		AgentType:   agentdom.AgentTypeACP,
		ACPProvider: &provider,
	}

	var storedHash string
	repo := &mockAgentRepo{
		findAgentByID: func(_ context.Context, _ uuid.UUID) (*agentdom.Agent, error) {
			return agent, nil
		},
	}
	repo.setMCPAPIKeyHash = func(_ context.Context, id uuid.UUID, hash string) error {
		if id != agentID {
			t.Fatalf("expected agentID %v, got %v", agentID, id)
		}
		storedHash = hash
		return nil
	}
	svc := New(repo, &mockProjectRepo{}, nil, &mockPluginRepo{})

	key, err := svc.GenerateAgentMCPKey(context.Background(), projectID, agentID)

	assert.NoError(t, err)
	assert.NotEmpty(t, key)
	assert.NotEmpty(t, storedHash)
	// The stored value must be a hash, never the plaintext key itself.
	assert.NotEqual(t, key, storedHash)
}

func TestGenerateAgentMCPKey_NonACPAgent(t *testing.T) {
	projectID := uuid.New()
	agentID := uuid.New()
	agent := &agentdom.Agent{
		ID:        agentID,
		ProjectID: projectID,
		AgentType: agentdom.AgentTypeLLM,
	}

	repo := &mockAgentRepo{
		findAgentByID: func(_ context.Context, _ uuid.UUID) (*agentdom.Agent, error) {
			return agent, nil
		},
	}
	svc := New(repo, &mockProjectRepo{}, nil, &mockPluginRepo{})

	_, err := svc.GenerateAgentMCPKey(context.Background(), projectID, agentID)

	assert.ErrorIs(t, err, agentdom.ErrAgentTypeInvalid)
}

func TestGenerateAgentMCPKey_WrongProject(t *testing.T) {
	projectID := uuid.New()
	agentID := uuid.New()

	// Same project-scope enforcement as TestGenerateACPBridgeToken_WrongProject.
	repo := &mockAgentRepo{
		findVisibleAgentInProject: func(context.Context, uuid.UUID, uuid.UUID) (*agentdom.Agent, error) {
			return nil, agentdom.ErrAgentNotFound
		},
	}
	svc := New(repo, &mockProjectRepo{}, nil, &mockPluginRepo{})

	_, err := svc.GenerateAgentMCPKey(context.Background(), projectID, agentID)

	assert.ErrorIs(t, err, agentdom.ErrAgentNotFound)
}

func TestGenerateGlobalAgentMCPKey_Success(t *testing.T) {
	agentID := uuid.New()
	provider := agentdom.ACPProviderClaudeCode
	agent := &agentdom.Agent{
		ID:          agentID,
		AgentScope:  agentdom.AgentScopeGlobal,
		AgentType:   agentdom.AgentTypeACP,
		ACPProvider: &provider,
	}

	var storedHash string
	repo := &mockAgentRepo{
		findAgentByID: func(_ context.Context, _ uuid.UUID) (*agentdom.Agent, error) {
			return agent, nil
		},
	}
	repo.setMCPAPIKeyHash = func(_ context.Context, id uuid.UUID, hash string) error {
		if id != agentID {
			t.Fatalf("expected agentID %v, got %v", agentID, id)
		}
		storedHash = hash
		return nil
	}
	svc := New(repo, &mockProjectRepo{}, nil, &mockPluginRepo{})

	key, err := svc.GenerateGlobalAgentMCPKey(context.Background(), agentID)

	assert.NoError(t, err)
	assert.NotEmpty(t, key)
	assert.NotEmpty(t, storedHash)
	assert.NotEqual(t, key, storedHash)
}

func TestGenerateGlobalAgentMCPKey_NonACPAgent(t *testing.T) {
	agentID := uuid.New()
	agent := &agentdom.Agent{
		ID:         agentID,
		AgentScope: agentdom.AgentScopeGlobal,
		AgentType:  agentdom.AgentTypeLLM,
	}

	repo := &mockAgentRepo{
		findAgentByID: func(_ context.Context, _ uuid.UUID) (*agentdom.Agent, error) {
			return agent, nil
		},
	}
	svc := New(repo, &mockProjectRepo{}, nil, &mockPluginRepo{})

	_, err := svc.GenerateGlobalAgentMCPKey(context.Background(), agentID)

	assert.ErrorIs(t, err, agentdom.ErrAgentTypeInvalid)
}

func TestGenerateGlobalAgentMCPKey_NotGlobalScope(t *testing.T) {
	agentID := uuid.New()
	// A project-scoped agent must not be reachable through the global
	// endpoint — GetGlobalAgent rejects it as not found.
	agent := &agentdom.Agent{
		ID:         agentID,
		AgentScope: agentdom.AgentScopeProject,
		AgentType:  agentdom.AgentTypeACP,
	}

	repo := &mockAgentRepo{
		findAgentByID: func(_ context.Context, _ uuid.UUID) (*agentdom.Agent, error) {
			return agent, nil
		},
	}
	svc := New(repo, &mockProjectRepo{}, nil, &mockPluginRepo{})

	_, err := svc.GenerateGlobalAgentMCPKey(context.Background(), agentID)

	assert.ErrorIs(t, err, agentdom.ErrAgentNotFound)
}

// -------------------------------------------------------------------------
// requireNonACPAgent — MCP servers / skills / env vars are meaningless for
// ACP-type agents (services/ai-agent's acp_dispatch.py never reads any of
// these tables), so every write path must reject them outright instead of
// silently accepting a change that will never take effect.
// -------------------------------------------------------------------------

func TestAddMCPServer_ACPAgent_ReturnsError(t *testing.T) {
	agentID := uuid.New()
	command := "python"
	repo := &mockAgentRepo{
		findAgentByID: findAgentByIDReturning(agentdom.AgentTypeACP),
		createMCPServer: func(context.Context, *agentdom.AgentMCPServer) error {
			t.Fatal("createMCPServer should not be called for an ACP-type agent")
			return nil
		},
	}
	svc := New(repo, &mockProjectRepo{}, nil, &mockPluginRepo{})

	_, err := svc.AddMCPServer(context.Background(), agentID, agentdom.AddMCPServerInput{
		ServerName: "Test Server",
		Transport:  "stdio",
		Command:    &command,
	})

	assert.ErrorIs(t, err, agentdom.ErrNotSupportedForACPAgent)
}

func TestUpdateMCPServer_ACPAgent_ReturnsError(t *testing.T) {
	agentID := uuid.New()
	serverID := uuid.New()
	server := &agentdom.AgentMCPServer{ID: serverID, AgentID: agentID}
	repo := &mockAgentRepo{
		findAgentByID: findAgentByIDReturning(agentdom.AgentTypeACP),
		findMCPServerByID: func(context.Context, uuid.UUID) (*agentdom.AgentMCPServer, error) {
			return server, nil
		},
		updateMCPServer: func(context.Context, *agentdom.AgentMCPServer) error {
			t.Fatal("updateMCPServer should not be called for an ACP-type agent")
			return nil
		},
	}
	svc := New(repo, &mockProjectRepo{}, nil, &mockPluginRepo{})

	_, err := svc.UpdateMCPServer(context.Background(), agentID, serverID, agentdom.UpdateMCPServerInput{})

	assert.ErrorIs(t, err, agentdom.ErrNotSupportedForACPAgent)
}

func TestDeleteMCPServer_ACPAgent_ReturnsError(t *testing.T) {
	agentID := uuid.New()
	serverID := uuid.New()
	server := &agentdom.AgentMCPServer{ID: serverID, AgentID: agentID}
	repo := &mockAgentRepo{
		findAgentByID: findAgentByIDReturning(agentdom.AgentTypeACP),
		findMCPServerByID: func(context.Context, uuid.UUID) (*agentdom.AgentMCPServer, error) {
			return server, nil
		},
		deleteMCPServer: func(context.Context, uuid.UUID) error {
			t.Fatal("deleteMCPServer should not be called for an ACP-type agent")
			return nil
		},
	}
	svc := New(repo, &mockProjectRepo{}, nil, &mockPluginRepo{})

	err := svc.DeleteMCPServer(context.Background(), agentID, serverID)

	assert.ErrorIs(t, err, agentdom.ErrNotSupportedForACPAgent)
}

func TestAddSkill_ACPAgent_ReturnsError(t *testing.T) {
	agentID := uuid.New()
	repo := &mockAgentRepo{
		findAgentByID: findAgentByIDReturning(agentdom.AgentTypeACP),
		createSkill: func(context.Context, *agentdom.AgentSkill) error {
			t.Fatal("createSkill should not be called for an ACP-type agent")
			return nil
		},
	}
	svc := New(repo, &mockProjectRepo{}, nil, &mockPluginRepo{})

	_, err := svc.AddSkill(context.Background(), agentID, agentdom.AddSkillInput{
		SkillName:    "Test Skill",
		SkillSource:  "file",
		SkillContent: "skill content",
	})

	assert.ErrorIs(t, err, agentdom.ErrNotSupportedForACPAgent)
}

func TestUpdateSkill_ACPAgent_ReturnsError(t *testing.T) {
	agentID := uuid.New()
	skillID := uuid.New()
	skill := &agentdom.AgentSkill{ID: skillID, AgentID: agentID, SkillName: "Skill"}
	repo := &mockAgentRepo{
		findAgentByID: findAgentByIDReturning(agentdom.AgentTypeACP),
		findSkillByID: func(context.Context, uuid.UUID) (*agentdom.AgentSkill, error) {
			return skill, nil
		},
		updateSkill: func(context.Context, *agentdom.AgentSkill) error {
			t.Fatal("updateSkill should not be called for an ACP-type agent")
			return nil
		},
	}
	svc := New(repo, &mockProjectRepo{}, nil, &mockPluginRepo{})

	_, err := svc.UpdateSkill(context.Background(), agentID, skillID, agentdom.UpdateSkillInput{})

	assert.ErrorIs(t, err, agentdom.ErrNotSupportedForACPAgent)
}

func TestDeleteSkill_ACPAgent_ReturnsError(t *testing.T) {
	agentID := uuid.New()
	skillID := uuid.New()
	skill := &agentdom.AgentSkill{ID: skillID, AgentID: agentID, SkillName: "Skill"}
	repo := &mockAgentRepo{
		findAgentByID: findAgentByIDReturning(agentdom.AgentTypeACP),
		findSkillByID: func(context.Context, uuid.UUID) (*agentdom.AgentSkill, error) {
			return skill, nil
		},
		deleteSkill: func(context.Context, uuid.UUID) error {
			t.Fatal("deleteSkill should not be called for an ACP-type agent")
			return nil
		},
	}
	svc := New(repo, &mockProjectRepo{}, nil, &mockPluginRepo{})

	err := svc.DeleteSkill(context.Background(), agentID, skillID)

	assert.ErrorIs(t, err, agentdom.ErrNotSupportedForACPAgent)
}

func TestAddEnvVar_ACPAgent_ReturnsError(t *testing.T) {
	agentID := uuid.New()
	repo := &mockAgentRepo{
		findAgentByID: findAgentByIDReturning(agentdom.AgentTypeACP),
		createEnvVar: func(context.Context, *agentdom.AgentEnvironmentVariable) error {
			t.Fatal("createEnvVar should not be called for an ACP-type agent")
			return nil
		},
	}
	svc := New(repo, &mockProjectRepo{}, nil, &mockPluginRepo{})

	_, err := svc.AddEnvVar(context.Background(), agentID, agentdom.AddEnvVarInput{
		Key:   "MY_VAR",
		Value: "secret",
	})

	assert.ErrorIs(t, err, agentdom.ErrNotSupportedForACPAgent)
}

func TestUpdateEnvVar_ACPAgent_ReturnsError(t *testing.T) {
	agentID := uuid.New()
	envVarID := uuid.New()
	v := &agentdom.AgentEnvironmentVariable{ID: envVarID, AgentID: agentID, Key: "MY_VAR"}
	repo := &mockAgentRepo{
		findAgentByID: findAgentByIDReturning(agentdom.AgentTypeACP),
		findEnvVarByID: func(context.Context, uuid.UUID) (*agentdom.AgentEnvironmentVariable, error) {
			return v, nil
		},
		updateEnvVar: func(context.Context, *agentdom.AgentEnvironmentVariable) error {
			t.Fatal("updateEnvVar should not be called for an ACP-type agent")
			return nil
		},
	}
	svc := New(repo, &mockProjectRepo{}, nil, &mockPluginRepo{})

	_, err := svc.UpdateEnvVar(context.Background(), agentID, envVarID, agentdom.UpdateEnvVarInput{Value: "new-secret"})

	assert.ErrorIs(t, err, agentdom.ErrNotSupportedForACPAgent)
}

func TestDeleteEnvVar_ACPAgent_ReturnsError(t *testing.T) {
	agentID := uuid.New()
	envVarID := uuid.New()
	v := &agentdom.AgentEnvironmentVariable{ID: envVarID, AgentID: agentID, Key: "MY_VAR"}
	repo := &mockAgentRepo{
		findAgentByID: findAgentByIDReturning(agentdom.AgentTypeACP),
		findEnvVarByID: func(context.Context, uuid.UUID) (*agentdom.AgentEnvironmentVariable, error) {
			return v, nil
		},
		deleteEnvVar: func(context.Context, uuid.UUID) error {
			t.Fatal("deleteEnvVar should not be called for an ACP-type agent")
			return nil
		},
	}
	svc := New(repo, &mockProjectRepo{}, nil, &mockPluginRepo{})

	err := svc.DeleteEnvVar(context.Background(), agentID, envVarID)

	assert.ErrorIs(t, err, agentdom.ErrNotSupportedForACPAgent)
}
