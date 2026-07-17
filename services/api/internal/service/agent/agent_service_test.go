package agentsvc

import (
	"context"
	"testing"

	agentdom "github.com/Paca-AI/api/internal/domain/agent"
	plugindom "github.com/Paca-AI/api/internal/domain/plugin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

type mockAgentRepo struct {
	findAgentByID                   func(ctx context.Context, id uuid.UUID) (*agentdom.Agent, error)
	findAgentByHandle               func(ctx context.Context, projectID uuid.UUID, handle string) (*agentdom.Agent, error)
	listAgents                      func(ctx context.Context, projectID uuid.UUID) ([]*agentdom.Agent, error)
	createAgent                     func(ctx context.Context, agent *agentdom.Agent) error
	createAgentWithMembership       func(ctx context.Context, agent *agentdom.Agent, memberID, projectID, projectRoleID uuid.UUID) error
	updateAgent                     func(ctx context.Context, agent *agentdom.Agent) error
	softDeleteAgent                 func(ctx context.Context, id uuid.UUID) error
	softDeleteAgentWithMembership   func(ctx context.Context, projectID, agentID uuid.UUID) error
	setAgentMemberID                func(ctx context.Context, agentID, memberID uuid.UUID) error
	setACPBridgeTokenHash           func(ctx context.Context, agentID uuid.UUID, hash string) error
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
}

func (m *mockAgentRepo) ListAgents(ctx context.Context, projectID uuid.UUID) ([]*agentdom.Agent, error) {
	if m.listAgents != nil {
		return m.listAgents(ctx, projectID)
	}
	return nil, nil
}

func (m *mockAgentRepo) FindAgentByID(ctx context.Context, id uuid.UUID) (*agentdom.Agent, error) {
	if m.findAgentByID != nil {
		return m.findAgentByID(ctx, id)
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
}

func (m *mockProjectRepo) InvalidateMembersCache(_ context.Context, _ uuid.UUID) error {
	m.invalidateMembersCacheCalled = true
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
		findAgentByID: func(_ context.Context, _ uuid.UUID) (*agentdom.Agent, error) {
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
	wrongProjectID := uuid.New()
	agentID := uuid.New()
	agent := &agentdom.Agent{
		ID:        agentID,
		ProjectID: wrongProjectID,
		Name:      "Test Agent",
		Handle:    "test-agent",
	}

	repo := &mockAgentRepo{
		findAgentByID: func(_ context.Context, _ uuid.UUID) (*agentdom.Agent, error) {
			return agent, nil
		},
	}
	projRepo := &mockProjectRepo{}
	pluginRepo := &mockPluginRepo{}
	svc := New(repo, projRepo, nil, pluginRepo)

	_, err := svc.GetAgent(context.Background(), projectID, agentID)

	assert.Error(t, err)
	assert.ErrorIs(t, err, agentdom.ErrAgentNotFound)
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
		listAgents: func(_ context.Context, pid uuid.UUID) ([]*agentdom.Agent, error) {
			if pid != projectID {
				t.Fatalf("expected projectID %v, got %v", projectID, pid)
			}
			return []*agentdom.Agent{agent1, agent2}, nil
		},
	}
	projRepo := &mockProjectRepo{}
	pluginRepo := &mockPluginRepo{}
	svc := New(repo, projRepo, nil, pluginRepo)

	result, err := svc.ListAgents(context.Background(), projectID)

	assert.NoError(t, err)
	assert.Len(t, result, 2)
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

	result, err := svc.UpdateAgent(context.Background(), projectID, agentID, agentdom.UpdateAgentInput{
		LLMModel:   &newModel,
		LLMAPIKey:  &newAPIKey,
		LLMBaseURL: &newBaseURL,
	})

	assert.NoError(t, err)
	assert.Empty(t, result.LLMModel)
	assert.Empty(t, result.LLMAPIKeySecret)
	assert.Empty(t, result.LLMBaseURL)
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

func TestSendConversationMessage_Success(t *testing.T) {
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
	wrongProjectID := uuid.New()
	agentID := uuid.New()
	provider := agentdom.ACPProviderClaudeCode
	agent := &agentdom.Agent{
		ID:          agentID,
		ProjectID:   wrongProjectID,
		AgentType:   agentdom.AgentTypeACP,
		ACPProvider: &provider,
	}

	repo := &mockAgentRepo{
		findAgentByID: func(_ context.Context, _ uuid.UUID) (*agentdom.Agent, error) {
			return agent, nil
		},
	}
	svc := New(repo, &mockProjectRepo{}, nil, &mockPluginRepo{})

	_, err := svc.GenerateACPBridgeToken(context.Background(), projectID, agentID)

	assert.ErrorIs(t, err, agentdom.ErrAgentNotFound)
}
