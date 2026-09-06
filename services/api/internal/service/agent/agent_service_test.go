package agentsvc

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"

	"github.com/Paca-AI/api/internal/apierr"
	agentdom "github.com/Paca-AI/api/internal/domain/agent"
	attachmentdom "github.com/Paca-AI/api/internal/domain/attachment"
	environmentdom "github.com/Paca-AI/api/internal/domain/environment"
	plugindom "github.com/Paca-AI/api/internal/domain/plugin"
	"github.com/Paca-AI/api/internal/platform/authz"
)

// ---------------------------------------------------------------------------
// Minimal fake avatar service
// ---------------------------------------------------------------------------

// fakeAvatarService is a bare-bones attachmentdom.AvatarService double —
// CompleteAvatarUpload always returns nextKeys, and DeleteAvatarObjects
// records what it was asked to delete so tests can assert the *previous*
// avatar's keys were cleaned up after a replace.
type fakeAvatarService struct {
	mu             sync.Mutex
	nextKeys       *attachmentdom.AvatarKeys
	deletedKeys    []string
	initiateCalled bool
}

func (f *fakeAvatarService) InitiateAvatarUpload(context.Context, attachmentdom.AvatarUploadInput) (*attachmentdom.UploadSession, error) {
	f.mu.Lock()
	f.initiateCalled = true
	f.mu.Unlock()
	return &attachmentdom.UploadSession{FileID: uuid.New(), UploadURL: "https://fake/upload"}, nil
}

func (f *fakeAvatarService) CompleteAvatarUpload(context.Context, attachmentdom.AvatarCompleteInput) (*attachmentdom.AvatarKeys, error) {
	return f.nextKeys, nil
}

func (f *fakeAvatarService) ResolveAvatarURL(context.Context, *string) (*string, error) {
	return nil, nil
}

func (f *fakeAvatarService) DeleteAvatarObjects(_ context.Context, keys ...*string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, k := range keys {
		if k != nil && *k != "" {
			f.deletedKeys = append(f.deletedKeys, *k)
		}
	}
}

// findAgentByIDReturning stubs mockAgentRepo.findAgentByID to return a
// minimal agent of the given type, regardless of the requested id — used by
// tests exercising MCP server / skill / env var writes, which now check the
// owning agent's type via requireGooseManagedAgent before touching the repo.
func findAgentByIDReturning(agentType string) func(context.Context, uuid.UUID) (*agentdom.Agent, error) {
	return func(_ context.Context, id uuid.UUID) (*agentdom.Agent, error) {
		return &agentdom.Agent{ID: id, AgentType: agentType}, nil
	}
}

type mockAgentRepo struct {
	findAgentByID                        func(ctx context.Context, id uuid.UUID) (*agentdom.Agent, error)
	findVisibleAgentInProject            func(ctx context.Context, projectID, agentID uuid.UUID) (*agentdom.Agent, error)
	findAgentByHandle                    func(ctx context.Context, projectID uuid.UUID, handle string) (*agentdom.Agent, error)
	listAgents                           func(ctx context.Context, projectID uuid.UUID, scope agentdom.AgentScope) ([]*agentdom.Agent, error)
	createAgent                          func(ctx context.Context, agent *agentdom.Agent) error
	createAgentWithMembership            func(ctx context.Context, agent *agentdom.Agent, memberID, projectID, projectRoleID uuid.UUID) error
	updateAgent                          func(ctx context.Context, agent *agentdom.Agent) error
	softDeleteAgent                      func(ctx context.Context, id uuid.UUID) error
	softDeleteAgentWithMembership        func(ctx context.Context, projectID, agentID uuid.UUID) error
	setAgentMemberID                     func(ctx context.Context, agentID, memberID uuid.UUID) error
	setACPBridgeTokenHash                func(ctx context.Context, agentID uuid.UUID, hash string) error
	setMCPAPIKeyHash                     func(ctx context.Context, agentID uuid.UUID, hash string) error
	setCLILoginVerifiedAt                func(ctx context.Context, agentID uuid.UUID, t time.Time) error
	findAgentByMCPAPIKeyHash             func(ctx context.Context, hash string) (*agentdom.Agent, error)
	listMCPServers                       func(ctx context.Context, agentID uuid.UUID) ([]*agentdom.AgentMCPServer, error)
	findMCPServerByID                    func(ctx context.Context, id uuid.UUID) (*agentdom.AgentMCPServer, error)
	createMCPServer                      func(ctx context.Context, server *agentdom.AgentMCPServer) error
	updateMCPServer                      func(ctx context.Context, server *agentdom.AgentMCPServer) error
	deleteMCPServer                      func(ctx context.Context, id uuid.UUID) error
	listSkills                           func(ctx context.Context, agentID uuid.UUID) ([]*agentdom.AgentSkill, error)
	findSkillByID                        func(ctx context.Context, id uuid.UUID) (*agentdom.AgentSkill, error)
	createSkill                          func(ctx context.Context, skill *agentdom.AgentSkill) error
	updateSkill                          func(ctx context.Context, skill *agentdom.AgentSkill) error
	deleteSkill                          func(ctx context.Context, id uuid.UUID) error
	listEnvVars                          func(ctx context.Context, agentID uuid.UUID) ([]*agentdom.AgentEnvironmentVariable, error)
	findEnvVarByID                       func(ctx context.Context, id uuid.UUID) (*agentdom.AgentEnvironmentVariable, error)
	findEnvVarByKey                      func(ctx context.Context, agentID uuid.UUID, key string) (*agentdom.AgentEnvironmentVariable, error)
	createEnvVar                         func(ctx context.Context, v *agentdom.AgentEnvironmentVariable) error
	updateEnvVar                         func(ctx context.Context, v *agentdom.AgentEnvironmentVariable) error
	deleteEnvVar                         func(ctx context.Context, id uuid.UUID) error
	listConversations                    func(ctx context.Context, filter agentdom.ListConversationsFilter, limit int) ([]*agentdom.AgentConversation, bool, error)
	findConversationByID                 func(ctx context.Context, id uuid.UUID) (*agentdom.AgentConversation, error)
	findLatestConversationBySession      func(ctx context.Context, chatSessionID uuid.UUID) (*agentdom.AgentConversation, error)
	createConversation                   func(ctx context.Context, conv *agentdom.AgentConversation) error
	updateConversationStatus             func(ctx context.Context, id uuid.UUID, status string) error
	claimConversationStatus              func(ctx context.Context, id uuid.UUID, fromStatus, toStatus string) (bool, error)
	updateConversation                   func(ctx context.Context, conv *agentdom.AgentConversation) error
	listConversationEvents               func(ctx context.Context, conversationID uuid.UUID, window agentdom.ConversationEventWindow) ([]*agentdom.AgentConversationEvent, int64, error)
	createConversationEvent              func(ctx context.Context, event *agentdom.AgentConversationEvent) error
	listChatSessions                     func(ctx context.Context, agentID, memberID uuid.UUID) ([]*agentdom.AgentChatSession, error)
	findChatSessionByID                  func(ctx context.Context, id uuid.UUID) (*agentdom.AgentChatSession, error)
	createChatSession                    func(ctx context.Context, session *agentdom.AgentChatSession) error
	updateChatSession                    func(ctx context.Context, session *agentdom.AgentChatSession) error
	listAgentActivities                  func(ctx context.Context, filter agentdom.ListAgentActivitiesFilter, limit int) ([]*agentdom.ActivityFeedItem, bool, error)
	listGlobalAgents                     func(ctx context.Context) ([]*agentdom.Agent, error)
	findGlobalAgentByHandle              func(ctx context.Context, handle string) (*agentdom.Agent, error)
	createGlobalAgent                    func(ctx context.Context, agent *agentdom.Agent) error
	softDeleteGlobalAgentCascade         func(ctx context.Context, agentID uuid.UUID) error
	listInvitedProjectIDs                func(ctx context.Context, agentID uuid.UUID) ([]uuid.UUID, error)
	listGlobalChatSessions               func(ctx context.Context, agentID, actorUserID uuid.UUID) ([]*agentdom.AgentChatSession, error)
	hasActiveGlobalChatSession           func(ctx context.Context, agentID, actorUserID uuid.UUID) (bool, error)
	countRunningConversations            func(ctx context.Context, agentID uuid.UUID) (int, error)
	countRunningConversationsInFolder    func(ctx context.Context, environmentID uuid.UUID, folderID *uuid.UUID) (int, error)
	createPendingTrigger                 func(ctx context.Context, t *agentdom.PendingTrigger) error
	dequeueOldestPendingTrigger          func(ctx context.Context, agentID uuid.UUID) (*agentdom.PendingTrigger, error)
	dequeueOldestPendingTriggerForFolder func(ctx context.Context, environmentID uuid.UUID, folderID *uuid.UUID) (*agentdom.PendingTrigger, error)
	deletePendingTriggerByConvID         func(ctx context.Context, conversationID uuid.UUID) (bool, error)
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

func (m *mockAgentRepo) SetCLILoginVerifiedAt(ctx context.Context, agentID uuid.UUID, t time.Time) error {
	if m.setCLILoginVerifiedAt != nil {
		return m.setCLILoginVerifiedAt(ctx, agentID, t)
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

func (m *mockAgentRepo) ListConversationEvents(ctx context.Context, conversationID uuid.UUID, window agentdom.ConversationEventWindow) ([]*agentdom.AgentConversationEvent, int64, error) {
	if m.listConversationEvents != nil {
		return m.listConversationEvents(ctx, conversationID, window)
	}
	return nil, 0, nil
}

func (m *mockAgentRepo) CreateConversationEvent(ctx context.Context, event *agentdom.AgentConversationEvent) error {
	if m.createConversationEvent != nil {
		return m.createConversationEvent(ctx, event)
	}
	return nil
}

// CountRunningConversations defaults to 0 — most tests never populate
// agent_conversations with a second, unrelated "running" row for the same
// agent, so leaving this unset means checkParallelismCapacity's default
// ParallelismLimit of 1 is never exceeded and every pre-existing test's
// dispatch-immediately behavior is unaffected.
func (m *mockAgentRepo) CountRunningConversations(ctx context.Context, agentID uuid.UUID) (int, error) {
	if m.countRunningConversations != nil {
		return m.countRunningConversations(ctx, agentID)
	}
	return 0, nil
}

// CountRunningConversationsInFolder defaults to 0 — most tests never
// populate agent_conversations with a "running" row in the same folder, so
// leaving this unset means checkFolderCapacity never blocks and every
// pre-existing test's dispatch-immediately behavior is unaffected.
func (m *mockAgentRepo) CountRunningConversationsInFolder(ctx context.Context, environmentID uuid.UUID, folderID *uuid.UUID) (int, error) {
	if m.countRunningConversationsInFolder != nil {
		return m.countRunningConversationsInFolder(ctx, environmentID, folderID)
	}
	return 0, nil
}

func (m *mockAgentRepo) CreatePendingTrigger(ctx context.Context, t *agentdom.PendingTrigger) error {
	if m.createPendingTrigger != nil {
		return m.createPendingTrigger(ctx, t)
	}
	return nil
}

func (m *mockAgentRepo) DequeueOldestPendingTrigger(ctx context.Context, agentID uuid.UUID) (*agentdom.PendingTrigger, error) {
	if m.dequeueOldestPendingTrigger != nil {
		return m.dequeueOldestPendingTrigger(ctx, agentID)
	}
	return nil, nil
}

func (m *mockAgentRepo) DequeueOldestPendingTriggerForFolder(ctx context.Context, environmentID uuid.UUID, folderID *uuid.UUID) (*agentdom.PendingTrigger, error) {
	if m.dequeueOldestPendingTriggerForFolder != nil {
		return m.dequeueOldestPendingTriggerForFolder(ctx, environmentID, folderID)
	}
	return nil, nil
}

func (m *mockAgentRepo) DeletePendingTriggerByConversationID(ctx context.Context, conversationID uuid.UUID) (bool, error) {
	if m.deletePendingTriggerByConvID != nil {
		return m.deletePendingTriggerByConvID(ctx, conversationID)
	}
	return false, nil
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
		findAgentByID: func(_ context.Context, id uuid.UUID) (*agentdom.Agent, error) {
			return &agentdom.Agent{ID: id}, nil
		},
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

	session, conv, err := svc.StartGlobalChatSession(context.Background(), agentID, actorUserID, "hello", nil, "")

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

// TestAddSkill_InvalidName_ReturnsError guards the on-disk path
// buildSkillsTar (agent-runner's executor/skills.go) and providercli's
// claude_code.go SyncFiles build from a skill name — neither sanitizes it,
// so a name like "../../../etc/cron.d/x" would otherwise let a project
// member with agents:write on their own project write a SKILL.md outside
// the intended skills directory inside the agent's own sandbox/environment
// (see validateSkillName's own doc comment).
func TestAddSkill_InvalidName_ReturnsError(t *testing.T) {
	invalidNames := []string{
		"",
		".",
		"..",
		"../../../etc/cron.d/x",
		"foo/bar",
		"foo\\bar",
		"/etc/passwd",
	}

	for _, name := range invalidNames {
		t.Run(name, func(t *testing.T) {
			agentID := uuid.New()
			repo := &mockAgentRepo{
				createSkill: func(_ context.Context, _ *agentdom.AgentSkill) error {
					t.Fatal("createSkill should not be called for an invalid name")
					return nil
				},
			}
			svc := New(repo, &mockProjectRepo{}, nil, &mockPluginRepo{})

			result, err := svc.AddSkill(context.Background(), agentID, agentdom.AddSkillInput{
				SkillName:    name,
				SkillSource:  "file",
				SkillContent: "skill content",
			})

			assert.Nil(t, result)
			assert.ErrorIs(t, err, agentdom.ErrSkillNameInvalid)
		})
	}
}

// TestAddSkill_ValidNamesWithDotsAllowed confirms the new validateSkillName
// guard only rejects an exact "." / ".." segment or a path separator — an
// ordinary name that merely contains a dot (e.g. a version suffix) must
// keep working, since it names one harmless directory segment, not a
// traversal.
func TestAddSkill_ValidNamesWithDotsAllowed(t *testing.T) {
	agentID := uuid.New()
	repo := &mockAgentRepo{
		findAgentByID: findAgentByIDReturning(agentdom.AgentTypeLLM),
		createSkill:   func(_ context.Context, _ *agentdom.AgentSkill) error { return nil },
	}
	svc := New(repo, &mockProjectRepo{}, nil, &mockPluginRepo{})

	result, err := svc.AddSkill(context.Background(), agentID, agentdom.AddSkillInput{
		SkillName:    "my-skill.v1.2",
		SkillSource:  "file",
		SkillContent: "skill content",
	})

	assert.NoError(t, err)
	if assert.NotNil(t, result) {
		assert.Equal(t, "my-skill.v1.2", result.SkillName)
	}
}

func TestGetConversation_Success(t *testing.T) {
	projectID := uuid.New()
	conversationID := uuid.New()
	memberID := uuid.New()
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

	result, err := svc.GetConversation(context.Background(), projectID, conversationID, memberID)

	assert.NoError(t, err)
	assert.Equal(t, conversationID, result.ID)
	assert.Equal(t, projectID, result.ProjectID)
}

func TestGetConversation_WrongProject(t *testing.T) {
	projectID := uuid.New()
	wrongProjectID := uuid.New()
	conversationID := uuid.New()
	memberID := uuid.New()
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

	_, err := svc.GetConversation(context.Background(), projectID, conversationID, memberID)

	assert.Error(t, err)
	assert.ErrorIs(t, err, agentdom.ErrConversationNotFound)
}

func TestGetConversation_OwnerPrivate_OwnerAllowed(t *testing.T) {
	projectID := uuid.New()
	conversationID := uuid.New()
	ownerMemberID := uuid.New()
	sessionID := uuid.New()
	conversation := &agentdom.AgentConversation{
		ID:            conversationID,
		ProjectID:     projectID,
		ChatSessionID: &sessionID,
		Audience:      agentdom.AudienceOwnerPrivate,
		Status:        "running",
	}
	session := &agentdom.AgentChatSession{ID: sessionID, MemberID: ownerMemberID}

	repo := &mockAgentRepo{
		findConversationByID: func(_ context.Context, _ uuid.UUID) (*agentdom.AgentConversation, error) {
			return conversation, nil
		},
		findChatSessionByID: func(_ context.Context, _ uuid.UUID) (*agentdom.AgentChatSession, error) {
			return session, nil
		},
	}
	svc := New(repo, &mockProjectRepo{}, nil, &mockPluginRepo{})

	result, err := svc.GetConversation(context.Background(), projectID, conversationID, ownerMemberID)

	assert.NoError(t, err)
	assert.Equal(t, conversationID, result.ID)
}

func TestGetConversation_OwnerPrivate_WrongMember(t *testing.T) {
	projectID := uuid.New()
	conversationID := uuid.New()
	ownerMemberID := uuid.New()
	otherMemberID := uuid.New()
	sessionID := uuid.New()
	conversation := &agentdom.AgentConversation{
		ID:            conversationID,
		ProjectID:     projectID,
		ChatSessionID: &sessionID,
		Audience:      agentdom.AudienceOwnerPrivate,
		Status:        "running",
	}
	session := &agentdom.AgentChatSession{ID: sessionID, MemberID: ownerMemberID}

	repo := &mockAgentRepo{
		findConversationByID: func(_ context.Context, _ uuid.UUID) (*agentdom.AgentConversation, error) {
			return conversation, nil
		},
		findChatSessionByID: func(_ context.Context, _ uuid.UUID) (*agentdom.AgentChatSession, error) {
			return session, nil
		},
	}
	svc := New(repo, &mockProjectRepo{}, nil, &mockPluginRepo{})

	_, err := svc.GetConversation(context.Background(), projectID, conversationID, otherMemberID)

	assert.Error(t, err)
	assert.ErrorIs(t, err, agentdom.ErrConversationNotFound)
}

// TestGetConversationForAgent_SameConversation_Allowed reproduces the
// read_conversation MCP tool's simplest case: an agent reading the very
// conversation it's currently running as part of. Always allowed regardless
// of audience, and doesn't require a second FindConversationByID lookup.
func TestGetConversationForAgent_SameConversation_Allowed(t *testing.T) {
	agentID := uuid.New()
	conversationID := uuid.New()
	conversation := &agentdom.AgentConversation{
		ID:       conversationID,
		AgentID:  agentID,
		Audience: agentdom.AudienceOwnerPrivate,
		Status:   "stopped",
	}
	lookups := 0
	repo := &mockAgentRepo{
		findConversationByID: func(_ context.Context, _ uuid.UUID) (*agentdom.AgentConversation, error) {
			lookups++
			return conversation, nil
		},
	}
	svc := New(repo, &mockProjectRepo{}, nil, &mockPluginRepo{})

	result, err := svc.GetConversationForAgent(context.Background(), conversationID, agentID, conversationID)

	assert.NoError(t, err)
	assert.Equal(t, conversationID, result.ID)
	assert.Equal(t, 1, lookups, "reading the current conversation itself should not need a second lookup")
}

// TestGetConversationForAgent_DifferentAgent_Rejected asserts the
// authorization boundary: an agent may not read a conversation it wasn't
// itself the agent of, even when it's project_shared — reading another
// agent's conversation isn't the case this path exists for.
func TestGetConversationForAgent_DifferentAgent_Rejected(t *testing.T) {
	callerAgentID := uuid.New()
	ownerAgentID := uuid.New()
	conversationID := uuid.New()
	conversation := &agentdom.AgentConversation{
		ID:       conversationID,
		AgentID:  ownerAgentID,
		Audience: agentdom.AudienceProjectShared,
		Status:   "stopped",
	}
	repo := &mockAgentRepo{
		findConversationByID: func(_ context.Context, _ uuid.UUID) (*agentdom.AgentConversation, error) {
			return conversation, nil
		},
	}
	svc := New(repo, &mockProjectRepo{}, nil, &mockPluginRepo{})

	_, err := svc.GetConversationForAgent(context.Background(), conversationID, callerAgentID, uuid.Nil)

	assert.Error(t, err)
	assert.ErrorIs(t, err, agentdom.ErrConversationNotFound)
}

// TestGetConversationForAgent_NoCurrentConversation_Rejected pins the
// fail-closed default: without a verifiable currentConversationID (e.g. an
// older agent-runner build that never sends X-Conversation-ID), a *different*
// conversation of the agent's own is not reachable — there is no trusted
// context to check its audience against. This is deliberately not the old
// "any conversation this agent ever had" behavior; see
// GetConversationForAgent's doc comment.
func TestGetConversationForAgent_NoCurrentConversation_Rejected(t *testing.T) {
	agentID := uuid.New()
	conversationID := uuid.New()
	conversation := &agentdom.AgentConversation{
		ID:       conversationID,
		AgentID:  agentID,
		Audience: agentdom.AudienceProjectShared,
		Status:   "stopped",
	}
	repo := &mockAgentRepo{
		findConversationByID: func(_ context.Context, id uuid.UUID) (*agentdom.AgentConversation, error) {
			if id == conversationID {
				return conversation, nil
			}
			return nil, agentdom.ErrConversationNotFound
		},
	}
	svc := New(repo, &mockProjectRepo{}, nil, &mockPluginRepo{})

	_, err := svc.GetConversationForAgent(context.Background(), conversationID, agentID, uuid.Nil)

	assert.Error(t, err)
	assert.ErrorIs(t, err, agentdom.ErrConversationNotFound)
}

// TestGetConversationForAgent_SpoofedCurrentConversation_Rejected pins that
// a currentConversationID belonging to a *different* agent (whether spoofed
// or just stale) is never trusted as context — it's treated the same as no
// current conversation at all.
func TestGetConversationForAgent_SpoofedCurrentConversation_Rejected(t *testing.T) {
	agentID := uuid.New()
	otherAgentID := uuid.New()
	conversationID := uuid.New()
	currentConversationID := uuid.New()
	target := &agentdom.AgentConversation{ID: conversationID, AgentID: agentID, Audience: agentdom.AudienceProjectShared, ProjectID: uuid.New()}
	current := &agentdom.AgentConversation{ID: currentConversationID, AgentID: otherAgentID, ProjectID: target.ProjectID}
	repo := &mockAgentRepo{
		findConversationByID: func(_ context.Context, id uuid.UUID) (*agentdom.AgentConversation, error) {
			if id == conversationID {
				return target, nil
			}
			return current, nil
		},
	}
	svc := New(repo, &mockProjectRepo{}, nil, &mockPluginRepo{})

	_, err := svc.GetConversationForAgent(context.Background(), conversationID, agentID, currentConversationID)

	assert.Error(t, err)
	assert.ErrorIs(t, err, agentdom.ErrConversationNotFound)
}

// TestGetConversationForAgent_Global_SameActor_Allowed and
// TestGetConversationForAgent_Global_DifferentActor_Rejected are the
// regression cases for the vulnerability this method used to have: a global
// agent talks to many different humans, and reading a *different* human's
// conversation with the same agent must stay denied even though the agent
// identity matches on both — the whole point of GetGlobalConversation's own
// actor check ("without it, any authenticated user could read... another
// user's conversation simply by knowing its ID").
func TestGetConversationForAgent_Global_SameActor_Allowed(t *testing.T) {
	agentID := uuid.New()
	actorUserID := uuid.New()
	targetID, currentID := uuid.New(), uuid.New()
	target := &agentdom.AgentConversation{ID: targetID, AgentID: agentID, ActorUserID: &actorUserID}
	current := &agentdom.AgentConversation{ID: currentID, AgentID: agentID, ActorUserID: &actorUserID}
	repo := &mockAgentRepo{
		findConversationByID: func(_ context.Context, id uuid.UUID) (*agentdom.AgentConversation, error) {
			if id == targetID {
				return target, nil
			}
			return current, nil
		},
	}
	svc := New(repo, &mockProjectRepo{}, nil, &mockPluginRepo{})

	result, err := svc.GetConversationForAgent(context.Background(), targetID, agentID, currentID)

	assert.NoError(t, err)
	assert.Equal(t, targetID, result.ID)
}

func TestGetConversationForAgent_Global_DifferentActor_Rejected(t *testing.T) {
	agentID := uuid.New()
	victimActorUserID := uuid.New()
	attackerActorUserID := uuid.New()
	targetID, currentID := uuid.New(), uuid.New()
	// target: the victim's own private global conversation with the shared agent.
	target := &agentdom.AgentConversation{ID: targetID, AgentID: agentID, ActorUserID: &victimActorUserID}
	// current: the attacker's own, unrelated conversation with that same agent.
	current := &agentdom.AgentConversation{ID: currentID, AgentID: agentID, ActorUserID: &attackerActorUserID}
	repo := &mockAgentRepo{
		findConversationByID: func(_ context.Context, id uuid.UUID) (*agentdom.AgentConversation, error) {
			if id == targetID {
				return target, nil
			}
			return current, nil
		},
	}
	svc := New(repo, &mockProjectRepo{}, nil, &mockPluginRepo{})

	_, err := svc.GetConversationForAgent(context.Background(), targetID, agentID, currentID)

	assert.Error(t, err, "the attacker must not be able to read the victim's private conversation with the same shared agent")
	assert.ErrorIs(t, err, agentdom.ErrConversationNotFound)
}

// TestGetConversationForAgent_Project_SharedAudience_Allowed: a
// project_shared target in the same project is readable regardless of which
// member current belongs to — it's already visible to any project member.
func TestGetConversationForAgent_Project_SharedAudience_Allowed(t *testing.T) {
	agentID := uuid.New()
	projectID := uuid.New()
	targetID, currentID := uuid.New(), uuid.New()
	target := &agentdom.AgentConversation{ID: targetID, AgentID: agentID, ProjectID: projectID, Audience: agentdom.AudienceProjectShared}
	current := &agentdom.AgentConversation{ID: currentID, AgentID: agentID, ProjectID: projectID, ChatSessionID: nil}
	repo := &mockAgentRepo{
		findConversationByID: func(_ context.Context, id uuid.UUID) (*agentdom.AgentConversation, error) {
			if id == targetID {
				return target, nil
			}
			return current, nil
		},
	}
	svc := New(repo, &mockProjectRepo{}, nil, &mockPluginRepo{})

	result, err := svc.GetConversationForAgent(context.Background(), targetID, agentID, currentID)

	assert.NoError(t, err)
	assert.Equal(t, targetID, result.ID)
}

// TestGetConversationForAgent_Project_OwnerPrivate_SameMember_Allowed and
// TestGetConversationForAgent_Project_OwnerPrivate_DifferentMember_Rejected
// are the project-scoped regression cases: a project-scoped agent can hold
// a separate owner-private conversation with each project member, and
// reading a *different* member's private conversation must stay denied —
// mirroring authorizeConversationAccess's own rule for a human caller.
func TestGetConversationForAgent_Project_OwnerPrivate_SameMember_Allowed(t *testing.T) {
	agentID := uuid.New()
	projectID := uuid.New()
	memberID := uuid.New()
	targetID, currentID := uuid.New(), uuid.New()
	targetSessionID, currentSessionID := uuid.New(), uuid.New()
	target := &agentdom.AgentConversation{ID: targetID, AgentID: agentID, ProjectID: projectID, Audience: agentdom.AudienceOwnerPrivate, ChatSessionID: &targetSessionID}
	current := &agentdom.AgentConversation{ID: currentID, AgentID: agentID, ProjectID: projectID, ChatSessionID: &currentSessionID}
	repo := &mockAgentRepo{
		findConversationByID: func(_ context.Context, id uuid.UUID) (*agentdom.AgentConversation, error) {
			if id == targetID {
				return target, nil
			}
			return current, nil
		},
		findChatSessionByID: func(_ context.Context, id uuid.UUID) (*agentdom.AgentChatSession, error) {
			// Both sessions belong to the same human — e.g. the member
			// attached an earlier private conversation of their own as
			// context in a new private chat with the same agent.
			return &agentdom.AgentChatSession{ID: id, MemberID: memberID}, nil
		},
	}
	svc := New(repo, &mockProjectRepo{}, nil, &mockPluginRepo{})

	result, err := svc.GetConversationForAgent(context.Background(), targetID, agentID, currentID)

	assert.NoError(t, err)
	assert.Equal(t, targetID, result.ID)
}

func TestGetConversationForAgent_Project_OwnerPrivate_DifferentMember_Rejected(t *testing.T) {
	agentID := uuid.New()
	projectID := uuid.New()
	victimMemberID := uuid.New()
	attackerMemberID := uuid.New()
	targetID, currentID := uuid.New(), uuid.New()
	targetSessionID, currentSessionID := uuid.New(), uuid.New()
	// target: the victim's owner-private conversation with the shared project agent.
	target := &agentdom.AgentConversation{ID: targetID, AgentID: agentID, ProjectID: projectID, Audience: agentdom.AudienceOwnerPrivate, ChatSessionID: &targetSessionID}
	// current: the attacker's own, unrelated conversation with that same agent, same project.
	current := &agentdom.AgentConversation{ID: currentID, AgentID: agentID, ProjectID: projectID, ChatSessionID: &currentSessionID}
	repo := &mockAgentRepo{
		findConversationByID: func(_ context.Context, id uuid.UUID) (*agentdom.AgentConversation, error) {
			if id == targetID {
				return target, nil
			}
			return current, nil
		},
		findChatSessionByID: func(_ context.Context, id uuid.UUID) (*agentdom.AgentChatSession, error) {
			if id == targetSessionID {
				return &agentdom.AgentChatSession{ID: id, MemberID: victimMemberID}, nil
			}
			return &agentdom.AgentChatSession{ID: id, MemberID: attackerMemberID}, nil
		},
	}
	svc := New(repo, &mockProjectRepo{}, nil, &mockPluginRepo{})

	_, err := svc.GetConversationForAgent(context.Background(), targetID, agentID, currentID)

	assert.Error(t, err, "a fellow project member must not be able to read another member's owner-private conversation with the same agent")
	assert.ErrorIs(t, err, agentdom.ErrConversationNotFound)
}

// TestGetConversationForAgent_Project_DifferentProject_Rejected covers an
// agent invited into more than one project: a project_shared conversation
// from a *different* project than current's must still be denied, even
// though every project member there could already see it in their own
// project — current's own project is the only one this call may reach into.
func TestGetConversationForAgent_Project_DifferentProject_Rejected(t *testing.T) {
	agentID := uuid.New()
	targetID, currentID := uuid.New(), uuid.New()
	target := &agentdom.AgentConversation{ID: targetID, AgentID: agentID, ProjectID: uuid.New(), Audience: agentdom.AudienceProjectShared}
	current := &agentdom.AgentConversation{ID: currentID, AgentID: agentID, ProjectID: uuid.New()}
	repo := &mockAgentRepo{
		findConversationByID: func(_ context.Context, id uuid.UUID) (*agentdom.AgentConversation, error) {
			if id == targetID {
				return target, nil
			}
			return current, nil
		},
	}
	svc := New(repo, &mockProjectRepo{}, nil, &mockPluginRepo{})

	_, err := svc.GetConversationForAgent(context.Background(), targetID, agentID, currentID)

	assert.Error(t, err)
	assert.ErrorIs(t, err, agentdom.ErrConversationNotFound)
}

// ---------------------------------------------------------------------------
// GetConversationForAgent — conversations.read enforcement
// ---------------------------------------------------------------------------
//
// GetConversationForAgent additionally requires the calling agent to hold
// conversations.read (globally, or in the target conversation's own project)
// once an authz.Authorizer is wired via WithAuthorizer — matching the MCP
// server's own tool-listing gate for read_conversation
// (apps/mcp/src/permissions.ts) — for any conversation *other* than the one
// the agent is currently running as part of. Every test above constructs a
// bare Service with no authorizer, so this check never engages for them
// (see the authorizer field's doc comment) — these tests cover the wired
// case specifically: the same-conversation shortcut stays unconditionally
// allowed (an agent already has this data as that conversation's own active
// participant, and gating it would break a global-scope agent with no
// global role — the common case, since a global agent's global role is
// optional), while a genuinely different (cross-conversation) target does
// require the grant.

// fakeAgentPermissionStore is a minimal authz.AgentPermissionStore double —
// only the two agent-permission lookups GetConversationForAgent's check
// actually calls are wired; the plain user-facing methods are unused here.
type fakeAgentPermissionStore struct {
	agentGlobalPerms  map[uuid.UUID][]authz.Permission
	agentProjectPerms map[uuid.UUID]map[uuid.UUID][]authz.Permission // project_id -> agent_id -> permissions
}

func (f *fakeAgentPermissionStore) ListGlobalPermissions(_ context.Context, _ uuid.UUID) ([]authz.Permission, error) {
	return nil, nil
}

func (f *fakeAgentPermissionStore) ListProjectPermissions(_ context.Context, _, _ uuid.UUID) ([]authz.Permission, error) {
	return nil, nil
}

func (f *fakeAgentPermissionStore) ListAgentGlobalPermissions(_ context.Context, agentID uuid.UUID) ([]authz.Permission, error) {
	return f.agentGlobalPerms[agentID], nil
}

func (f *fakeAgentPermissionStore) ListAgentProjectPermissions(_ context.Context, agentID, projectID uuid.UUID) ([]authz.Permission, error) {
	if projMap, ok := f.agentProjectPerms[projectID]; ok {
		return projMap[agentID], nil
	}
	return nil, nil
}

// fakeAgentRoleResolver reports every agent as a member (with an arbitrary
// role name — HasPermissionsForAgent only uses the role name for the
// legacy-role fallback, which these tests don't exercise) of every project
// referenced in agentProjectPerms, so ListAgentProjectPermissions above is
// actually reached instead of short-circuiting on ErrAgentNotInProject.
type fakeAgentRoleResolver struct{}

func (fakeAgentRoleResolver) GetAgentProjectRoleName(_ context.Context, _, _ uuid.UUID) (string, error) {
	return "member", nil
}

// TestGetConversationForAgent_SelfRead_AllowedWithoutConversationsRead locks
// in that the same-conversation shortcut stays exempt from the
// conversations.read check even when an authorizer is wired and the agent
// holds no grant anywhere — see GetConversationForAgent's doc comment for
// why (most importantly: a global-scope agent's global role is optional and
// commonly unset, so requiring a grant here would break reading its own
// current conversation in the common case).
func TestGetConversationForAgent_SelfRead_AllowedWithoutConversationsRead(t *testing.T) {
	agentID := uuid.New()
	conversationID := uuid.New()
	conversation := &agentdom.AgentConversation{ID: conversationID, AgentID: agentID, Audience: agentdom.AudienceOwnerPrivate}
	repo := &mockAgentRepo{
		findConversationByID: func(_ context.Context, _ uuid.UUID) (*agentdom.AgentConversation, error) {
			return conversation, nil
		},
	}
	authorizer := authz.NewAuthorizer(&fakeAgentPermissionStore{}).WithAgentRoleResolver(fakeAgentRoleResolver{})
	svc := New(repo, &mockProjectRepo{}, nil, &mockPluginRepo{}).WithAuthorizer(authorizer)

	result, err := svc.GetConversationForAgent(context.Background(), conversationID, agentID, conversationID)

	assert.NoError(t, err)
	assert.Equal(t, conversationID, result.ID)
}

func TestGetConversationForAgent_CrossConversation_RequiresConversationsRead_GlobalGrant_Allowed(t *testing.T) {
	agentID := uuid.New()
	actorUserID := uuid.New()
	targetID, currentID := uuid.New(), uuid.New()
	// Global, same actor on both sides — authorizeAgentConversationRead
	// alone would already allow this (see
	// TestGetConversationForAgent_Global_SameActor_Allowed); conversations.read
	// must not additionally block it.
	target := &agentdom.AgentConversation{ID: targetID, AgentID: agentID, ActorUserID: &actorUserID}
	current := &agentdom.AgentConversation{ID: currentID, AgentID: agentID, ActorUserID: &actorUserID}
	repo := &mockAgentRepo{
		findConversationByID: func(_ context.Context, id uuid.UUID) (*agentdom.AgentConversation, error) {
			if id == targetID {
				return target, nil
			}
			return current, nil
		},
	}
	store := &fakeAgentPermissionStore{
		agentGlobalPerms: map[uuid.UUID][]authz.Permission{agentID: {authz.PermissionConversationsRead}},
	}
	authorizer := authz.NewAuthorizer(store).WithAgentRoleResolver(fakeAgentRoleResolver{})
	svc := New(repo, &mockProjectRepo{}, nil, &mockPluginRepo{}).WithAuthorizer(authorizer)

	result, err := svc.GetConversationForAgent(context.Background(), targetID, agentID, currentID)

	assert.NoError(t, err)
	assert.Equal(t, targetID, result.ID)
}

func TestGetConversationForAgent_CrossConversation_RequiresConversationsRead_ProjectGrant_Allowed(t *testing.T) {
	agentID := uuid.New()
	projectID := uuid.New()
	targetID, currentID := uuid.New(), uuid.New()
	// project_shared — visible to any project member already, so
	// authorizeAgentConversationRead alone would allow this; conversations.read
	// must not additionally block it.
	target := &agentdom.AgentConversation{ID: targetID, AgentID: agentID, ProjectID: projectID, Audience: agentdom.AudienceProjectShared}
	current := &agentdom.AgentConversation{ID: currentID, AgentID: agentID, ProjectID: projectID}
	repo := &mockAgentRepo{
		findConversationByID: func(_ context.Context, id uuid.UUID) (*agentdom.AgentConversation, error) {
			if id == targetID {
				return target, nil
			}
			return current, nil
		},
	}
	store := &fakeAgentPermissionStore{
		agentProjectPerms: map[uuid.UUID]map[uuid.UUID][]authz.Permission{
			projectID: {agentID: {authz.PermissionConversationsRead}},
		},
	}
	authorizer := authz.NewAuthorizer(store).WithAgentRoleResolver(fakeAgentRoleResolver{})
	svc := New(repo, &mockProjectRepo{}, nil, &mockPluginRepo{}).WithAuthorizer(authorizer)

	result, err := svc.GetConversationForAgent(context.Background(), targetID, agentID, currentID)

	assert.NoError(t, err)
	assert.Equal(t, targetID, result.ID)
}

func TestGetConversationForAgent_CrossConversation_RequiresConversationsRead_NoGrant_Rejected(t *testing.T) {
	agentID := uuid.New()
	projectID := uuid.New()
	targetID, currentID := uuid.New(), uuid.New()
	// project_shared again: authorizeAgentConversationRead alone would allow
	// this, isolating conversations.read as the only reason this must fail.
	target := &agentdom.AgentConversation{ID: targetID, AgentID: agentID, ProjectID: projectID, Audience: agentdom.AudienceProjectShared}
	current := &agentdom.AgentConversation{ID: currentID, AgentID: agentID, ProjectID: projectID}
	repo := &mockAgentRepo{
		findConversationByID: func(_ context.Context, id uuid.UUID) (*agentdom.AgentConversation, error) {
			if id == targetID {
				return target, nil
			}
			return current, nil
		},
	}
	authorizer := authz.NewAuthorizer(&fakeAgentPermissionStore{}).WithAgentRoleResolver(fakeAgentRoleResolver{})
	svc := New(repo, &mockProjectRepo{}, nil, &mockPluginRepo{}).WithAuthorizer(authorizer)

	_, err := svc.GetConversationForAgent(context.Background(), targetID, agentID, currentID)

	assert.Error(t, err)
	assert.ErrorIs(t, err, agentdom.ErrConversationNotFound)
}

func TestGetConversationForAgent_CrossConversation_RequiresConversationsRead_WrongProjectGrant_Rejected(t *testing.T) {
	agentID := uuid.New()
	conversationProjectID := uuid.New()
	grantedProjectID := uuid.New()
	targetID, currentID := uuid.New(), uuid.New()
	target := &agentdom.AgentConversation{ID: targetID, AgentID: agentID, ProjectID: conversationProjectID, Audience: agentdom.AudienceProjectShared}
	current := &agentdom.AgentConversation{ID: currentID, AgentID: agentID, ProjectID: conversationProjectID}
	repo := &mockAgentRepo{
		findConversationByID: func(_ context.Context, id uuid.UUID) (*agentdom.AgentConversation, error) {
			if id == targetID {
				return target, nil
			}
			return current, nil
		},
	}
	store := &fakeAgentPermissionStore{
		// conversations.read granted in a *different* project than the one the
		// conversation actually belongs to — must not transfer.
		agentProjectPerms: map[uuid.UUID]map[uuid.UUID][]authz.Permission{
			grantedProjectID: {agentID: {authz.PermissionConversationsRead}},
		},
	}
	authorizer := authz.NewAuthorizer(store).WithAgentRoleResolver(fakeAgentRoleResolver{})
	svc := New(repo, &mockProjectRepo{}, nil, &mockPluginRepo{}).WithAuthorizer(authorizer)

	_, err := svc.GetConversationForAgent(context.Background(), targetID, agentID, currentID)

	assert.Error(t, err)
	assert.ErrorIs(t, err, agentdom.ErrConversationNotFound)
}

func TestSendChatMessage_WrongMember(t *testing.T) {
	projectID := uuid.New()
	agentID := uuid.New()
	ownerMemberID := uuid.New()
	otherMemberID := uuid.New()
	sessionID := uuid.New()
	session := &agentdom.AgentChatSession{
		ID:        sessionID,
		AgentID:   agentID,
		ProjectID: projectID,
		MemberID:  ownerMemberID,
	}

	repo := &mockAgentRepo{
		findChatSessionByID: func(_ context.Context, _ uuid.UUID) (*agentdom.AgentChatSession, error) {
			return session, nil
		},
	}
	svc := New(repo, &mockProjectRepo{}, nil, &mockPluginRepo{})

	_, err := svc.SendChatMessage(context.Background(), projectID, sessionID, otherMemberID, "Hello", nil, "")

	assert.Error(t, err)
	assert.ErrorIs(t, err, agentdom.ErrChatSessionNotFound)
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
		err := svc.SendGlobalConversationMessage(context.Background(), conversationID, "hi", attacker, nil)
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

	err := svc.SendConversationMessage(context.Background(), projectID, conversationID, "test message", uuid.New(), nil)

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

	err := svc.SendConversationMessage(context.Background(), projectID, conversationID, "test message", uuid.New(), nil)

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

			err := svc.SendConversationMessage(context.Background(), projectID, conversationID, "keep going", uuid.New(), nil)

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

	err := svc.SendConversationMessage(context.Background(), projectID, conversationID, "are you there?", uuid.New(), nil)

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

	err := svc.SendConversationMessage(context.Background(), projectID, conversationID, "are you there?", uuid.New(), nil)

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

	err := svc.SendConversationMessage(context.Background(), projectID, conversationID, "keep going", uuid.New(), nil)

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

	err := svc.StopConversation(context.Background(), projectID, conversationID, uuid.Nil)

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

			err := svc.StopConversation(context.Background(), projectID, conversationID, uuid.Nil)

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

	err := svc.PauseConversation(context.Background(), projectID, conversationID, uuid.Nil)

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

	err := svc.PauseConversation(context.Background(), projectID, conversationID, uuid.Nil)

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

	err := svc.Heartbeat(context.Background(), projectID, conversationID, uuid.Nil)

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

	err := svc.Heartbeat(context.Background(), projectID, conversationID, uuid.Nil)

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
		findAgentByID: func(_ context.Context, id uuid.UUID) (*agentdom.Agent, error) {
			return &agentdom.Agent{ID: id}, nil
		},
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

// TestListChatSessions_WrongProject_ReturnsNotFound guards the defensive
// ownership check added alongside the StartChatSession fix (GHSA-xwmv-9c7h-g947
// follow-up audit): even though the underlying query is scoped by the
// caller's own memberID, ListChatSessions should still reject an agentID
// that isn't visible in projectID rather than silently delegating.
func TestListChatSessions_WrongProject_ReturnsNotFound(t *testing.T) {
	agentID := uuid.New()
	memberID := uuid.New()

	repo := &mockAgentRepo{
		findVisibleAgentInProject: func(context.Context, uuid.UUID, uuid.UUID) (*agentdom.Agent, error) {
			return nil, agentdom.ErrAgentNotFound
		},
		listChatSessions: func(context.Context, uuid.UUID, uuid.UUID) ([]*agentdom.AgentChatSession, error) {
			t.Fatal("listChatSessions must not be called when the agent is not visible in projectID")
			return nil, nil
		},
	}
	svc := New(repo, &mockProjectRepo{}, nil, &mockPluginRepo{})

	_, err := svc.ListChatSessions(context.Background(), uuid.New(), agentID, memberID)

	assert.ErrorIs(t, err, agentdom.ErrAgentNotFound)
}

func TestStartChatSession_Success(t *testing.T) {
	projectID := uuid.New()
	agentID := uuid.New()
	memberID := uuid.New()

	repo := &mockAgentRepo{
		findAgentByID: func(_ context.Context, id uuid.UUID) (*agentdom.Agent, error) {
			return &agentdom.Agent{ID: id}, nil
		},
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

	resultSession, resultConv, err := svc.StartChatSession(context.Background(), projectID, agentID, memberID, "Hello", nil, nil, nil, "")

	assert.NoError(t, err)
	assert.NotNil(t, resultSession)
	assert.NotNil(t, resultConv)
	assert.Equal(t, agentID, resultSession.AgentID)
	assert.Equal(t, projectID, resultSession.ProjectID)
}

// TestStartChatSession_WrongProject_ReturnsNotFound is the regression test
// for the agent-execution-hijack half of GHSA-xwmv-9c7h-g947's follow-up
// audit: a caller with agents.read on their own project must not be able to
// start (and thereby trigger a live run of) another project's agent by
// supplying its agentID. No session/conversation may be created.
func TestStartChatSession_WrongProject_ReturnsNotFound(t *testing.T) {
	projectID := uuid.New()
	agentID := uuid.New()
	memberID := uuid.New()

	repo := &mockAgentRepo{
		findVisibleAgentInProject: func(context.Context, uuid.UUID, uuid.UUID) (*agentdom.Agent, error) {
			return nil, agentdom.ErrAgentNotFound
		},
		createChatSession: func(context.Context, *agentdom.AgentChatSession) error {
			t.Fatal("createChatSession must not be called when the agent is not visible in projectID")
			return nil
		},
		createConversation: func(context.Context, *agentdom.AgentConversation) error {
			t.Fatal("createConversation must not be called when the agent is not visible in projectID")
			return nil
		},
	}
	svc := New(repo, &mockProjectRepo{}, nil, &mockPluginRepo{})

	_, _, err := svc.StartChatSession(context.Background(), projectID, agentID, memberID, "Hello", nil, nil, nil, "")

	assert.ErrorIs(t, err, agentdom.ErrAgentNotFound)
}

// TestTriggerDescriptionWrite_WrongProject_ReturnsNotFound is the regression
// test for the other agent-execution-hijack vector from the same audit
// (WriteTaskDescriptionWithAI): a caller with tasks.write on their own
// project must not be able to trigger another project's agent by supplying
// its agentID in the request body. The taskID half of this same endpoint's
// fix lives in the handler layer (AgentHandler.taskChecker), since this
// service has no task-repository dependency to verify that half itself.
func TestTriggerDescriptionWrite_WrongProject_ReturnsNotFound(t *testing.T) {
	projectID := uuid.New()
	agentID := uuid.New()
	taskID := uuid.New()
	memberID := uuid.New()

	repo := &mockAgentRepo{
		findVisibleAgentInProject: func(context.Context, uuid.UUID, uuid.UUID) (*agentdom.Agent, error) {
			return nil, agentdom.ErrAgentNotFound
		},
		createConversation: func(context.Context, *agentdom.AgentConversation) error {
			t.Fatal("createConversation must not be called when the agent is not visible in projectID")
			return nil
		},
	}
	svc := New(repo, &mockProjectRepo{}, nil, &mockPluginRepo{})

	_, err := svc.TriggerDescriptionWrite(context.Background(), projectID, agentID, taskID, memberID)

	assert.ErrorIs(t, err, agentdom.ErrAgentNotFound)
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
		MemberID:  memberID,
	}

	repo := &mockAgentRepo{
		findAgentByID: func(_ context.Context, id uuid.UUID) (*agentdom.Agent, error) {
			return &agentdom.Agent{ID: id}, nil
		},
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

	resultConv, err := svc.SendChatMessage(context.Background(), projectID, sessionID, memberID, "Hello", nil, "")

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
		MemberID:  memberID,
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
		findAgentByID: func(_ context.Context, id uuid.UUID) (*agentdom.Agent, error) {
			return &agentdom.Agent{ID: id}, nil
		},
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

	resultConv, err := svc.SendChatMessage(context.Background(), projectID, sessionID, memberID, "Continuing…", nil, "")

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
				MemberID:  memberID,
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

			resultConv, err := svc.SendChatMessage(context.Background(), projectID, sessionID, memberID, "Continuing…", nil, "")

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
		MemberID:  memberID,
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

	_, err := svc.SendChatMessage(context.Background(), projectID, sessionID, memberID, "Continuing…", nil, "")

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
		MemberID:  memberID,
	}
	finished := &agentdom.AgentConversation{
		ID:            oldConvID,
		AgentID:       agentID,
		ProjectID:     projectID,
		ChatSessionID: &sessionID,
		Status:        "finished",
	}

	createCalled := false
	// Tracks which conversation IDs ever get a ClaimConversationStatus call.
	// The old (terminal) conversation must never appear here — but the newly
	// created one legitimately will, since dispatch now atomically claims a
	// fresh "queued" conversation to "running" right before publishing (see
	// claimQueuedForDispatch's doc comment), so a bare "was it called at all"
	// check would no longer distinguish resuming the old conversation from
	// correctly claiming the new one.
	var claimedIDs []uuid.UUID
	repo := &mockAgentRepo{
		findAgentByID: findAgentByIDReturning(agentdom.AgentTypeLLM),
		findChatSessionByID: func(_ context.Context, _ uuid.UUID) (*agentdom.AgentChatSession, error) {
			return session, nil
		},
		findLatestConversationBySession: func(_ context.Context, _ uuid.UUID) (*agentdom.AgentConversation, error) {
			return finished, nil
		},
		claimConversationStatus: func(_ context.Context, id uuid.UUID, _, _ string) (bool, error) {
			claimedIDs = append(claimedIDs, id)
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

	resultConv, err := svc.SendChatMessage(context.Background(), projectID, sessionID, memberID, "Hello again", nil, "")

	assert.NoError(t, err)
	assert.True(t, createCalled, "a terminal LLM conversation must create a new conversation")
	assert.NotContains(t, claimedIDs, oldConvID, "must not attempt to claim/resume the old terminal LLM conversation")
	assert.NotEqual(t, oldConvID, resultConv.ID)
}

// TestSendChatMessage_EnvironmentBackedLLMResumesTerminalConversation mirrors
// TestSendChatMessage_ACPResumesTerminalConversation for the other case that
// gets the same terminal-status resume treatment: an LLM conversation
// attached to a static environment. Unlike
// TestSendChatMessage_LLMTerminalCreatesNewConversation's plain (no
// environment) LLM conversation, this must resume in place rather than
// spin up a new conversation_id — the environment's container outlives the
// conversation's own terminal status.
func TestSendChatMessage_EnvironmentBackedLLMResumesTerminalConversation(t *testing.T) {
	for _, status := range []string{"finished", "failed", "stopped"} {
		t.Run(status, func(t *testing.T) {
			projectID := uuid.New()
			agentID := uuid.New()
			memberID := uuid.New()
			sessionID := uuid.New()
			terminalConvID := uuid.New()
			environmentID := uuid.New()
			session := &agentdom.AgentChatSession{
				ID:        sessionID,
				AgentID:   agentID,
				ProjectID: projectID,
				MemberID:  memberID,
			}
			terminal := &agentdom.AgentConversation{
				ID:            terminalConvID,
				AgentID:       agentID,
				ProjectID:     projectID,
				ChatSessionID: &sessionID,
				EnvironmentID: &environmentID,
				Status:        status,
			}

			createCalled := false
			var claimedFrom, claimedTo string
			repo := &mockAgentRepo{
				findAgentByID: findAgentByIDReturning(agentdom.AgentTypeLLM),
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

			resultConv, err := svc.SendChatMessage(context.Background(), projectID, sessionID, memberID, "Continuing…", nil, "")

			assert.NoError(t, err)
			assert.False(t, createCalled, "resuming a terminal environment-backed conversation must not create a new one")
			assert.Equal(t, terminalConvID, resultConv.ID)
			assert.Equal(t, status, claimedFrom)
			assert.Equal(t, "running", claimedTo)
		})
	}
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
		MemberID:  memberID,
	}
	paused := &agentdom.AgentConversation{
		ID:            pausedConvID,
		AgentID:       agentID,
		ProjectID:     projectID,
		ChatSessionID: &sessionID,
		Status:        "paused",
	}

	repo := &mockAgentRepo{
		findAgentByID: func(_ context.Context, id uuid.UUID) (*agentdom.Agent, error) {
			return &agentdom.Agent{ID: id}, nil
		},
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

	_, err := svc.SendChatMessage(context.Background(), projectID, sessionID, memberID, "Continuing…", nil, "")

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
		MemberID:  memberID,
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
	_, err := svc.SendChatMessage(context.Background(), projectID, sessionID, memberID, "Are you there?", nil, "")

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
		MemberID:  memberID,
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

	_, err := svc.SendChatMessage(context.Background(), projectID, sessionID, memberID, "Are you there?", nil, "")

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

	_, err := svc.SendChatMessage(context.Background(), projectID, sessionID, memberID, "Hello", nil, "")

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

func TestCreateAgent_ACPGooseProviderSuccess(t *testing.T) {
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

	// Unlike ACPProviderCustom, goose is a built-in provider — no
	// acp_command is required from the caller. apps/acp-bridge's runner.py
	// resolves the default `goose acp` command itself (the OpenHands SDK's
	// own provider registry doesn't know about goose, so this can't be
	// resolved the same way claude-code/codex/gemini-cli are).
	result, err := svc.CreateAgent(context.Background(), projectID, agentdom.CreateAgentInput{
		Name:          "Goose Agent",
		Handle:        "goose-agent",
		AgentType:     agentdom.AgentTypeACP,
		ACPProvider:   agentdom.ACPProviderGoose,
		ProjectRoleID: projectRoleID,
	})

	assert.NoError(t, err)
	assert.Equal(t, agentdom.AgentTypeACP, result.AgentType)
	assert.Equal(t, agentdom.ACPProviderGoose, *result.ACPProvider)
	assert.Empty(t, result.ACPCommand)
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
// requireGooseManagedAgent — MCP servers / skills / env vars are meaningless
// for ACP-type agents (services/ai-agent's acp_dispatch.py never reads any
// of these tables), so every write path must reject them outright instead
// of silently accepting a change that will never take effect. llm and
// provider_cli agents both pass this check — see that function's own doc
// comment.
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

// ---------------------------------------------------------------------------
// Avatar
// ---------------------------------------------------------------------------

func TestInitiateAvatarUpload_NoAvatarService_ReturnsError(t *testing.T) {
	repo := &mockAgentRepo{findAgentByID: findAgentByIDReturning(agentdom.AgentTypeLLM)}
	svc := New(repo, &mockProjectRepo{}, nil, &mockPluginRepo{}) // WithAvatarService never called

	_, err := svc.InitiateAvatarUpload(context.Background(), uuid.New(), uuid.New(), "me.png", "image/png", 1024, uuid.New())

	assert.ErrorIs(t, err, ErrAvatarServiceRequired)
}

func TestInitiateAvatarUpload_AgentNotInProject_NeverCallsAvatarService(t *testing.T) {
	repo := &mockAgentRepo{
		findAgentByID: func(context.Context, uuid.UUID) (*agentdom.Agent, error) {
			return nil, agentdom.ErrAgentNotFound
		},
	}
	avatarSvc := &fakeAvatarService{}
	svc := New(repo, &mockProjectRepo{}, nil, &mockPluginRepo{}).WithAvatarService(avatarSvc)

	_, err := svc.InitiateAvatarUpload(context.Background(), uuid.New(), uuid.New(), "me.png", "image/png", 1024, uuid.New())

	assert.ErrorIs(t, err, agentdom.ErrAgentNotFound)
	avatarSvc.mu.Lock()
	defer avatarSvc.mu.Unlock()
	assert.False(t, avatarSvc.initiateCalled, "avatar service must not be reached once ownership fails")
}

func TestCompleteAvatarUpload_SwapsKeysAndDeletesOld(t *testing.T) {
	oldKey, oldThumbKey := "avatars/agents/a1/old/full.png", "avatars/agents/a1/old/thumb.png"
	agentID := uuid.New()
	projectID := uuid.New()
	var updated *agentdom.Agent
	repo := &mockAgentRepo{
		findAgentByID: func(_ context.Context, id uuid.UUID) (*agentdom.Agent, error) {
			return &agentdom.Agent{ID: id, ProjectID: projectID, AvatarKey: &oldKey, AvatarThumbKey: &oldThumbKey}, nil
		},
		updateAgent: func(_ context.Context, a *agentdom.Agent) error {
			updated = a
			return nil
		},
	}
	avatarSvc := &fakeAvatarService{
		nextKeys: &attachmentdom.AvatarKeys{Key: "avatars/agents/a1/new/full.png", ThumbKey: "avatars/agents/a1/new/thumb.png"},
	}
	svc := New(repo, &mockProjectRepo{}, nil, &mockPluginRepo{}).WithAvatarService(avatarSvc)

	a, err := svc.CompleteAvatarUpload(context.Background(), projectID, agentID, uuid.New())

	assert.NoError(t, err)
	assert.Equal(t, avatarSvc.nextKeys.Key, *a.AvatarKey)
	assert.Equal(t, avatarSvc.nextKeys.ThumbKey, *a.AvatarThumbKey)
	if assert.NotNil(t, updated) {
		assert.Equal(t, avatarSvc.nextKeys.Key, *updated.AvatarKey)
	}

	avatarSvc.mu.Lock()
	defer avatarSvc.mu.Unlock()
	assert.ElementsMatch(t, []string{oldKey, oldThumbKey}, avatarSvc.deletedKeys)
}

func TestRemoveAvatar_NoExistingAvatar_NoOps(t *testing.T) {
	agentID := uuid.New()
	projectID := uuid.New()
	updateCalled := false
	repo := &mockAgentRepo{
		findAgentByID: func(_ context.Context, id uuid.UUID) (*agentdom.Agent, error) {
			return &agentdom.Agent{ID: id, ProjectID: projectID}, nil
		},
		updateAgent: func(context.Context, *agentdom.Agent) error {
			updateCalled = true
			return nil
		},
	}
	avatarSvc := &fakeAvatarService{}
	svc := New(repo, &mockProjectRepo{}, nil, &mockPluginRepo{}).WithAvatarService(avatarSvc)

	_, err := svc.RemoveAvatar(context.Background(), projectID, agentID)

	assert.NoError(t, err)
	assert.False(t, updateCalled, "expected repo.UpdateAgent not to be called when the agent has no avatar")
	avatarSvc.mu.Lock()
	defer avatarSvc.mu.Unlock()
	assert.Empty(t, avatarSvc.deletedKeys)
}

func TestCompleteGlobalAvatarUpload_RejectsProjectScopedAgent(t *testing.T) {
	agentID := uuid.New()
	repo := &mockAgentRepo{
		findAgentByID: func(_ context.Context, id uuid.UUID) (*agentdom.Agent, error) {
			return &agentdom.Agent{ID: id, AgentScope: agentdom.AgentScopeProject, ProjectID: uuid.New()}, nil
		},
	}
	avatarSvc := &fakeAvatarService{}
	svc := New(repo, &mockProjectRepo{}, nil, &mockPluginRepo{}).WithAvatarService(avatarSvc)

	_, err := svc.CompleteGlobalAvatarUpload(context.Background(), agentID, uuid.New())

	assert.ErrorIs(t, err, agentdom.ErrAgentNotFound)
	avatarSvc.mu.Lock()
	defer avatarSvc.mu.Unlock()
	assert.Empty(t, avatarSvc.deletedKeys, "avatar service must not be touched for a scope-mismatched agent")
}

func TestRemoveGlobalAvatar_ClearsKeysAndDeletesObjects(t *testing.T) {
	key, thumbKey := "avatars/agents/a2/full.png", "avatars/agents/a2/thumb.png"
	agentID := uuid.New()
	repo := &mockAgentRepo{
		findAgentByID: func(_ context.Context, id uuid.UUID) (*agentdom.Agent, error) {
			return &agentdom.Agent{ID: id, AgentScope: agentdom.AgentScopeGlobal, AvatarKey: &key, AvatarThumbKey: &thumbKey}, nil
		},
		updateAgent: func(context.Context, *agentdom.Agent) error { return nil },
	}
	avatarSvc := &fakeAvatarService{}
	svc := New(repo, &mockProjectRepo{}, nil, &mockPluginRepo{}).WithAvatarService(avatarSvc)

	a, err := svc.RemoveGlobalAvatar(context.Background(), agentID)

	assert.NoError(t, err)
	assert.Nil(t, a.AvatarKey)
	assert.Nil(t, a.AvatarThumbKey)
	avatarSvc.mu.Lock()
	defer avatarSvc.mu.Unlock()
	assert.ElementsMatch(t, []string{key, thumbKey}, avatarSvc.deletedKeys)
}

// ---------------------------------------------------------------------------
// provider_cli — CreateAgent/UpdateAgent validation, CreateGlobalAgent's
// rejection, and VerifyCLILogin. See agentdom.Agent.CLIProvider's doc
// comment for the feature these all guard.
// ---------------------------------------------------------------------------

// fakeEnvironmentService is a minimal environmentdom.Service double. Only
// GetEnvironment, ResolveConversationWorkdir, and VerifyCLIAuth are
// configurable — the three methods agentsvc.Service actually calls (see
// the Service.environmentSvc field's own doc comment) — every other method
// of the interface is a stub that returns a zero value, since no test
// below exercises them through this fake.
type fakeEnvironmentService struct {
	getEnvironment func(ctx context.Context, projectID, environmentID uuid.UUID) (*environmentdom.Environment, error)
	verifyCLIAuth  func(ctx context.Context, projectID, environmentID uuid.UUID, cliProvider string) (bool, error)
}

func (f *fakeEnvironmentService) ListEnvironments(context.Context, uuid.UUID) ([]*environmentdom.Environment, error) {
	return nil, nil
}

func (f *fakeEnvironmentService) GetEnvironment(ctx context.Context, projectID, environmentID uuid.UUID) (*environmentdom.Environment, error) {
	if f.getEnvironment != nil {
		return f.getEnvironment(ctx, projectID, environmentID)
	}
	return nil, environmentdom.ErrEnvironmentNotFound
}

func (f *fakeEnvironmentService) CreateEnvironment(context.Context, uuid.UUID, environmentdom.CreateEnvironmentInput) (*environmentdom.Environment, error) {
	return nil, nil
}

func (f *fakeEnvironmentService) UpdateEnvironment(context.Context, uuid.UUID, uuid.UUID, environmentdom.UpdateEnvironmentInput) (*environmentdom.Environment, error) {
	return nil, nil
}

func (f *fakeEnvironmentService) StartEnvironment(context.Context, uuid.UUID, uuid.UUID) (*environmentdom.Environment, error) {
	return nil, nil
}

func (f *fakeEnvironmentService) StopEnvironment(context.Context, uuid.UUID, uuid.UUID) (*environmentdom.Environment, error) {
	return nil, nil
}

func (f *fakeEnvironmentService) RestartEnvironment(context.Context, uuid.UUID, uuid.UUID) (*environmentdom.Environment, error) {
	return nil, nil
}

func (f *fakeEnvironmentService) DeleteEnvironment(context.Context, uuid.UUID, uuid.UUID) error {
	return nil
}

func (f *fakeEnvironmentService) Heartbeat(context.Context, uuid.UUID, uuid.UUID) error {
	return nil
}

func (f *fakeEnvironmentService) ResolveConversationWorkdir(context.Context, uuid.UUID, *uuid.UUID, *uuid.UUID) (*environmentdom.Environment, *environmentdom.EnvironmentFolder, error) {
	return nil, nil, nil
}

func (f *fakeEnvironmentService) ListFolders(context.Context, uuid.UUID, uuid.UUID) ([]*environmentdom.EnvironmentFolder, error) {
	return nil, nil
}

func (f *fakeEnvironmentService) AddFolder(context.Context, uuid.UUID, uuid.UUID, environmentdom.AddFolderInput) (*environmentdom.EnvironmentFolder, error) {
	return nil, nil
}

func (f *fakeEnvironmentService) DeleteFolder(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) error {
	return nil
}

func (f *fakeEnvironmentService) Browse(context.Context, uuid.UUID, uuid.UUID, string) (string, []environmentdom.BrowseEntry, error) {
	return "", nil, nil
}

func (f *fakeEnvironmentService) VerifyCLIAuth(ctx context.Context, projectID, environmentID uuid.UUID, cliProvider string) (bool, error) {
	if f.verifyCLIAuth != nil {
		return f.verifyCLIAuth(ctx, projectID, environmentID, cliProvider)
	}
	return false, nil
}

func (f *fakeEnvironmentService) ListSSHKeys(context.Context, uuid.UUID, uuid.UUID) ([]*environmentdom.EnvironmentSSHKey, error) {
	return nil, nil
}

func (f *fakeEnvironmentService) AddSSHKey(context.Context, uuid.UUID, uuid.UUID, environmentdom.AddSSHKeyInput) (*environmentdom.EnvironmentSSHKey, error) {
	return nil, nil
}

func (f *fakeEnvironmentService) DeleteSSHKey(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) error {
	return nil
}

func (f *fakeEnvironmentService) ListPortForwards(context.Context, uuid.UUID, uuid.UUID) ([]*environmentdom.EnvironmentPortForward, error) {
	return nil, nil
}

func (f *fakeEnvironmentService) GetPortForward(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) (*environmentdom.EnvironmentPortForward, error) {
	return nil, nil
}

func (f *fakeEnvironmentService) AddPortForward(context.Context, uuid.UUID, uuid.UUID, environmentdom.AddPortForwardInput) (*environmentdom.EnvironmentPortForward, error) {
	return nil, nil
}

func (f *fakeEnvironmentService) DeletePortForward(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) error {
	return nil
}

var _ environmentdom.Service = (*fakeEnvironmentService)(nil)

func TestCreateAgent_ProviderCLI_Success(t *testing.T) {
	projectID := uuid.New()
	envID := uuid.New()
	projectRoleID := uuid.New()

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
	envSvc := &fakeEnvironmentService{
		getEnvironment: func(_ context.Context, pid, eid uuid.UUID) (*environmentdom.Environment, error) {
			if pid != projectID || eid != envID {
				t.Fatalf("unexpected project/environment id")
			}
			return &environmentdom.Environment{ID: envID, ProjectID: projectID}, nil
		},
	}
	svc := New(repo, &mockProjectRepo{}, nil, &mockPluginRepo{}).WithEnvironmentService(envSvc)

	result, err := svc.CreateAgent(context.Background(), projectID, agentdom.CreateAgentInput{
		Name:                 "CLI Agent",
		Handle:               "cli-agent",
		AgentType:            agentdom.AgentTypeProviderCLI,
		CLIProvider:          agentdom.CLIProviderClaudeCode,
		CLIModel:             "sonnet",
		ProjectRoleID:        projectRoleID,
		DefaultEnvironmentID: &envID,
	})

	assert.NoError(t, err)
	if assert.NotNil(t, result.CLIProvider) {
		assert.Equal(t, agentdom.CLIProviderClaudeCode, *result.CLIProvider)
	}
	assert.Equal(t, "sonnet", result.CLIModel)
	// CLIAuthMode defaults to "login" when the request omits it.
	assert.Equal(t, agentdom.CLIAuthModeLogin, result.CLIAuthMode)
	if assert.NotNil(t, result.DefaultEnvironmentID) {
		assert.Equal(t, envID, *result.DefaultEnvironmentID)
	}
}

func TestCreateAgent_ProviderCLI_InvalidProvider(t *testing.T) {
	projectID := uuid.New()
	repo := &mockAgentRepo{
		findAgentByHandle: func(_ context.Context, _ uuid.UUID, _ string) (*agentdom.Agent, error) {
			return nil, agentdom.ErrAgentNotFound
		},
	}
	svc := New(repo, &mockProjectRepo{}, nil, &mockPluginRepo{})

	_, err := svc.CreateAgent(context.Background(), projectID, agentdom.CreateAgentInput{
		Name:        "CLI Agent",
		Handle:      "cli-agent",
		AgentType:   agentdom.AgentTypeProviderCLI,
		CLIProvider: "not-a-real-cli",
	})

	assert.ErrorIs(t, err, agentdom.ErrCLIProviderInvalid)
}

func TestCreateAgent_ProviderCLI_RequiresDefaultEnvironment(t *testing.T) {
	projectID := uuid.New()
	repo := &mockAgentRepo{
		findAgentByHandle: func(_ context.Context, _ uuid.UUID, _ string) (*agentdom.Agent, error) {
			return nil, agentdom.ErrAgentNotFound
		},
	}
	svc := New(repo, &mockProjectRepo{}, nil, &mockPluginRepo{})

	_, err := svc.CreateAgent(context.Background(), projectID, agentdom.CreateAgentInput{
		Name:        "CLI Agent",
		Handle:      "cli-agent",
		AgentType:   agentdom.AgentTypeProviderCLI,
		CLIProvider: agentdom.CLIProviderClaudeCode,
		// DefaultEnvironmentID intentionally omitted.
	})

	assert.ErrorIs(t, err, agentdom.ErrDefaultEnvironmentRequiredForCLIProvider)
}

func TestCreateAgent_ProviderCLI_InvalidAuthMode(t *testing.T) {
	projectID := uuid.New()
	repo := &mockAgentRepo{
		findAgentByHandle: func(_ context.Context, _ uuid.UUID, _ string) (*agentdom.Agent, error) {
			return nil, agentdom.ErrAgentNotFound
		},
	}
	svc := New(repo, &mockProjectRepo{}, nil, &mockPluginRepo{})

	_, err := svc.CreateAgent(context.Background(), projectID, agentdom.CreateAgentInput{
		Name:        "CLI Agent",
		Handle:      "cli-agent",
		AgentType:   agentdom.AgentTypeProviderCLI,
		CLIProvider: agentdom.CLIProviderClaudeCode,
		CLIAuthMode: "not-a-real-mode",
	})

	assert.ErrorIs(t, err, agentdom.ErrCLIAuthModeInvalid)
}

func TestCreateAgent_ProviderCLI_APIKeyAuthUnsupportedForCursorAgent(t *testing.T) {
	projectID := uuid.New()
	envID := uuid.New()
	repo := &mockAgentRepo{
		findAgentByHandle: func(_ context.Context, _ uuid.UUID, _ string) (*agentdom.Agent, error) {
			return nil, agentdom.ErrAgentNotFound
		},
	}
	envSvc := &fakeEnvironmentService{
		getEnvironment: func(_ context.Context, _, _ uuid.UUID) (*environmentdom.Environment, error) {
			return &environmentdom.Environment{ID: envID, ProjectID: projectID}, nil
		},
	}
	svc := New(repo, &mockProjectRepo{}, nil, &mockPluginRepo{}).WithEnvironmentService(envSvc)

	// cursor-agent has no confirmed non-interactive API-key auth path (see
	// agentdom.CLIProvidersWithAPIKeyAuth) — requesting api_key auth for it
	// must be rejected rather than silently falling back to login mode.
	_, err := svc.CreateAgent(context.Background(), projectID, agentdom.CreateAgentInput{
		Name:                 "CLI Agent",
		Handle:               "cli-agent",
		AgentType:            agentdom.AgentTypeProviderCLI,
		CLIProvider:          agentdom.CLIProviderCursor,
		CLIAuthMode:          agentdom.CLIAuthModeAPIKey,
		CLIAPIKey:            "secret-key",
		DefaultEnvironmentID: &envID,
	})

	assert.ErrorIs(t, err, agentdom.ErrCLIProviderNoAPIKeyAuth)
}

func TestUpdateAgent_ProviderCLIAgentIgnoresLLMAndACPFields(t *testing.T) {
	projectID := uuid.New()
	agentID := uuid.New()
	envID := uuid.New()
	provider := agentdom.CLIProviderClaudeCode
	agent := &agentdom.Agent{
		ID:                   agentID,
		ProjectID:            projectID,
		Name:                 "CLI Agent",
		Handle:               "cli-agent",
		AgentType:            agentdom.AgentTypeProviderCLI,
		CLIProvider:          &provider,
		CLIAuthMode:          agentdom.CLIAuthModeLogin,
		DefaultEnvironmentID: &envID,
	}

	repo := &mockAgentRepo{
		findAgentByID: func(_ context.Context, _ uuid.UUID) (*agentdom.Agent, error) {
			return agent, nil
		},
		updateAgent: func(_ context.Context, _ *agentdom.Agent) error { return nil },
	}
	svc := New(repo, &mockProjectRepo{}, nil, &mockPluginRepo{})

	newModel := "gpt-4"
	newAPIKey := "sk-leaked-onto-provider-cli-agent"
	newACPProvider := agentdom.ACPProviderCustom

	result, err := svc.UpdateAgent(context.Background(), projectID, agentID, agentdom.UpdateAgentInput{
		LLMModel:    &newModel,
		LLMAPIKey:   &newAPIKey,
		ACPProvider: &newACPProvider,
		ACPCommand:  []string{"my-server"},
	})

	assert.NoError(t, err)
	assert.Empty(t, result.LLMModel)
	assert.Empty(t, result.LLMAPIKeySecret)
	assert.Nil(t, result.ACPProvider)
	assert.Empty(t, result.ACPCommand)
}

func TestUpdateAgent_ProviderCLI_UpdatesCLIFields(t *testing.T) {
	projectID := uuid.New()
	agentID := uuid.New()
	envID := uuid.New()
	provider := agentdom.CLIProviderClaudeCode
	agent := &agentdom.Agent{
		ID:                   agentID,
		ProjectID:            projectID,
		AgentType:            agentdom.AgentTypeProviderCLI,
		CLIProvider:          &provider,
		CLIAuthMode:          agentdom.CLIAuthModeLogin,
		DefaultEnvironmentID: &envID,
	}
	repo := &mockAgentRepo{
		findAgentByID: func(_ context.Context, _ uuid.UUID) (*agentdom.Agent, error) { return agent, nil },
		updateAgent:   func(_ context.Context, _ *agentdom.Agent) error { return nil },
	}
	svc := New(repo, &mockProjectRepo{}, nil, &mockPluginRepo{})

	newModel := "opus"
	newProvider := agentdom.CLIProviderCodex

	result, err := svc.UpdateAgent(context.Background(), projectID, agentID, agentdom.UpdateAgentInput{
		CLIProvider: &newProvider,
		CLIModel:    &newModel,
	})

	assert.NoError(t, err)
	if assert.NotNil(t, result.CLIProvider) {
		assert.Equal(t, agentdom.CLIProviderCodex, *result.CLIProvider)
	}
	assert.Equal(t, "opus", result.CLIModel)
}

func TestUpdateAgent_ProviderCLI_InvalidProvider(t *testing.T) {
	projectID := uuid.New()
	agentID := uuid.New()
	envID := uuid.New()
	provider := agentdom.CLIProviderClaudeCode
	agent := &agentdom.Agent{
		ID:                   agentID,
		ProjectID:            projectID,
		AgentType:            agentdom.AgentTypeProviderCLI,
		CLIProvider:          &provider,
		DefaultEnvironmentID: &envID,
	}
	repo := &mockAgentRepo{
		findAgentByID: func(_ context.Context, _ uuid.UUID) (*agentdom.Agent, error) { return agent, nil },
	}
	svc := New(repo, &mockProjectRepo{}, nil, &mockPluginRepo{})

	badProvider := "not-a-real-cli"
	_, err := svc.UpdateAgent(context.Background(), projectID, agentID, agentdom.UpdateAgentInput{
		CLIProvider: &badProvider,
	})

	assert.ErrorIs(t, err, agentdom.ErrCLIProviderInvalid)
}

func TestUpdateAgent_ProviderCLI_ClearingDefaultEnvironmentFails(t *testing.T) {
	projectID := uuid.New()
	agentID := uuid.New()
	envID := uuid.New()
	provider := agentdom.CLIProviderClaudeCode
	agent := &agentdom.Agent{
		ID:                   agentID,
		ProjectID:            projectID,
		AgentType:            agentdom.AgentTypeProviderCLI,
		CLIProvider:          &provider,
		DefaultEnvironmentID: &envID,
	}
	repo := &mockAgentRepo{
		findAgentByID: func(_ context.Context, _ uuid.UUID) (*agentdom.Agent, error) { return agent, nil },
	}
	svc := New(repo, &mockProjectRepo{}, nil, &mockPluginRepo{})

	// uuid.Nil is UpdateAgentInput.DefaultEnvironmentID's "clear it"
	// sentinel (see that field's own doc comment) — a provider_cli agent
	// must reject this, not silently drop its CLI's persisted login state.
	clearedEnv := uuid.Nil
	_, err := svc.UpdateAgent(context.Background(), projectID, agentID, agentdom.UpdateAgentInput{
		DefaultEnvironmentID: &clearedEnv,
	})

	assert.ErrorIs(t, err, agentdom.ErrDefaultEnvironmentRequiredForCLIProvider)
}

func TestCreateGlobalAgent_RejectsProviderCLI(t *testing.T) {
	repo := &mockAgentRepo{
		findGlobalAgentByHandle: func(_ context.Context, _ string) (*agentdom.Agent, error) {
			return nil, agentdom.ErrAgentNotFound
		},
	}
	svc := New(repo, &mockProjectRepo{}, nil, &mockPluginRepo{})

	// A global agent has no single project's environments to default to,
	// so provider_cli (which requires one) is rejected outright — see
	// agentdom.ErrCLIProviderNotSupportedForGlobalAgents's own doc comment.
	_, err := svc.CreateGlobalAgent(context.Background(), agentdom.CreateGlobalAgentInput{
		Name:      "Global CLI Bot",
		Handle:    "global-cli-bot",
		AgentType: agentdom.AgentTypeProviderCLI,
	})

	assert.ErrorIs(t, err, agentdom.ErrCLIProviderNotSupportedForGlobalAgents)
}

func TestVerifyCLILogin_NonProviderCLIAgent_ReturnsError(t *testing.T) {
	projectID := uuid.New()
	agentID := uuid.New()
	agent := &agentdom.Agent{ID: agentID, ProjectID: projectID, AgentType: agentdom.AgentTypeLLM}

	repo := &mockAgentRepo{
		findVisibleAgentInProject: func(_ context.Context, _, _ uuid.UUID) (*agentdom.Agent, error) {
			return agent, nil
		},
	}
	svc := New(repo, &mockProjectRepo{}, nil, &mockPluginRepo{})

	_, err := svc.VerifyCLILogin(context.Background(), projectID, agentID)

	assert.ErrorIs(t, err, agentdom.ErrAgentNotProviderCLI)
}

func TestVerifyCLILogin_NoEnvironmentService_ReturnsError(t *testing.T) {
	projectID := uuid.New()
	agentID := uuid.New()
	envID := uuid.New()
	provider := agentdom.CLIProviderClaudeCode
	agent := &agentdom.Agent{
		ID:                   agentID,
		ProjectID:            projectID,
		AgentType:            agentdom.AgentTypeProviderCLI,
		CLIProvider:          &provider,
		DefaultEnvironmentID: &envID,
	}
	repo := &mockAgentRepo{
		findVisibleAgentInProject: func(_ context.Context, _, _ uuid.UUID) (*agentdom.Agent, error) {
			return agent, nil
		},
	}
	// No WithEnvironmentService call — a self-hosted deployment that never
	// wired one up must fail loudly here, not panic on a nil interface.
	svc := New(repo, &mockProjectRepo{}, nil, &mockPluginRepo{})

	_, err := svc.VerifyCLILogin(context.Background(), projectID, agentID)

	assert.Error(t, err)
}

func TestVerifyCLILogin_Authenticated_PersistsTimestamp(t *testing.T) {
	projectID := uuid.New()
	agentID := uuid.New()
	envID := uuid.New()
	provider := agentdom.CLIProviderClaudeCode
	agent := &agentdom.Agent{
		ID:                   agentID,
		ProjectID:            projectID,
		AgentType:            agentdom.AgentTypeProviderCLI,
		CLIProvider:          &provider,
		DefaultEnvironmentID: &envID,
	}
	var setCalled bool
	var setCalledFor uuid.UUID
	repo := &mockAgentRepo{
		findVisibleAgentInProject: func(_ context.Context, _, _ uuid.UUID) (*agentdom.Agent, error) {
			return agent, nil
		},
		setCLILoginVerifiedAt: func(_ context.Context, id uuid.UUID, _ time.Time) error {
			setCalled = true
			setCalledFor = id
			return nil
		},
	}
	envSvc := &fakeEnvironmentService{
		verifyCLIAuth: func(_ context.Context, pid, eid uuid.UUID, cliProvider string) (bool, error) {
			if pid != projectID || eid != envID || cliProvider != agentdom.CLIProviderClaudeCode {
				t.Fatalf("unexpected VerifyCLIAuth args: %s %s %s", pid, eid, cliProvider)
			}
			return true, nil
		},
	}
	svc := New(repo, &mockProjectRepo{}, nil, &mockPluginRepo{}).WithEnvironmentService(envSvc)

	authenticated, err := svc.VerifyCLILogin(context.Background(), projectID, agentID)

	assert.NoError(t, err)
	assert.True(t, authenticated)
	assert.True(t, setCalled)
	assert.Equal(t, agentID, setCalledFor)
}

func TestVerifyCLILogin_NotAuthenticated_DoesNotPersistTimestamp(t *testing.T) {
	projectID := uuid.New()
	agentID := uuid.New()
	envID := uuid.New()
	provider := agentdom.CLIProviderClaudeCode
	agent := &agentdom.Agent{
		ID:                   agentID,
		ProjectID:            projectID,
		AgentType:            agentdom.AgentTypeProviderCLI,
		CLIProvider:          &provider,
		DefaultEnvironmentID: &envID,
	}
	setCalled := false
	repo := &mockAgentRepo{
		findVisibleAgentInProject: func(_ context.Context, _, _ uuid.UUID) (*agentdom.Agent, error) {
			return agent, nil
		},
		setCLILoginVerifiedAt: func(_ context.Context, _ uuid.UUID, _ time.Time) error {
			setCalled = true
			return nil
		},
	}
	envSvc := &fakeEnvironmentService{
		verifyCLIAuth: func(context.Context, uuid.UUID, uuid.UUID, string) (bool, error) {
			return false, nil
		},
	}
	svc := New(repo, &mockProjectRepo{}, nil, &mockPluginRepo{}).WithEnvironmentService(envSvc)

	authenticated, err := svc.VerifyCLILogin(context.Background(), projectID, agentID)

	assert.NoError(t, err)
	assert.False(t, authenticated)
	assert.False(t, setCalled, "cli_login_verified_at must not be touched when the CLI isn't authenticated")
}

// -------------------------------------------------------------------------
// Parallelism limit — concurrency-safety fixes.
//
// The tests below lock in two fixes made after a deliberate review for
// cross-component races/conflicts (services/api, agent-runner,
// apps/acp-bridge):
//
//  1. Dispatch (fresh, resumed, or dequeued from agent_pending_triggers)
//     must atomically claim a conversation from "queued" to "running"
//     immediately before publishing its trigger — see
//     claimQueuedForDispatch's doc comment for the two races this closes
//     (Valkey Streams at-least-once redelivery double-dispatching a
//     terminal-status event's freed slot, and StopConversation racing
//     AdvanceQueue's dequeue of the very conversation being stopped).
//  2. parallelism_limit above 1 is rejected outright for an agent that
//     can't safely run more than one conversation at once — an ACP-type
//     agent (apps/acp-bridge's own Runner session model, keyed by task_id
//     or agent_id rather than conversation_id, rejects a second concurrent
//     turn sharing that key instead of queueing it) or any agent attached
//     to a static default_environment_id (its filesystem is shared across
//     every conversation attached to it, unlike the default ephemeral
//     per-conversation sandbox) — see requiresSerialDispatch's doc comment.
// -------------------------------------------------------------------------

func TestRequiresSerialDispatch(t *testing.T) {
	envID := uuid.New()
	tests := []struct {
		name  string
		agent *agentdom.Agent
		want  bool
	}{
		{"llm, no environment", &agentdom.Agent{AgentType: agentdom.AgentTypeLLM}, false},
		{"acp", &agentdom.Agent{AgentType: agentdom.AgentTypeACP}, true},
		{"llm, environment-backed", &agentdom.Agent{AgentType: agentdom.AgentTypeLLM, DefaultEnvironmentID: &envID}, true},
		{"provider_cli (always environment-backed)", &agentdom.Agent{AgentType: agentdom.AgentTypeProviderCLI, DefaultEnvironmentID: &envID}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, requiresSerialDispatch(tt.agent))
		})
	}
}

func TestEffectiveParallelismLimit(t *testing.T) {
	envID := uuid.New()
	tests := []struct {
		name  string
		agent *agentdom.Agent
		want  int
	}{
		{"unset defaults to 1", &agentdom.Agent{AgentType: agentdom.AgentTypeLLM}, 1},
		{"configured value honored", &agentdom.Agent{AgentType: agentdom.AgentTypeLLM, ParallelismLimit: 5}, 5},
		{"capped", &agentdom.Agent{AgentType: agentdom.AgentTypeLLM, ParallelismLimit: 999}, parallelismLimitCap},
		{"acp forced to 1 regardless of stored value", &agentdom.Agent{AgentType: agentdom.AgentTypeACP, ParallelismLimit: 5}, 1},
		{"environment-backed forced to 1 regardless of stored value", &agentdom.Agent{AgentType: agentdom.AgentTypeLLM, DefaultEnvironmentID: &envID, ParallelismLimit: 5}, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, effectiveParallelismLimit(tt.agent))
		})
	}
}

func TestCreateAgent_RejectsParallelismLimitAboveOneForACP(t *testing.T) {
	projectID := uuid.New()
	repo := &mockAgentRepo{
		findAgentByHandle: func(_ context.Context, _ uuid.UUID, _ string) (*agentdom.Agent, error) {
			return nil, agentdom.ErrAgentNotFound
		},
		createAgentWithMembership: func(context.Context, *agentdom.Agent, uuid.UUID, uuid.UUID, uuid.UUID) error {
			t.Fatal("createAgentWithMembership must not be called when parallelism_limit is invalid for this agent type")
			return nil
		},
	}
	svc := New(repo, &mockProjectRepo{}, nil, &mockPluginRepo{})

	_, err := svc.CreateAgent(context.Background(), projectID, agentdom.CreateAgentInput{
		Name:             "ACP Agent",
		Handle:           "acp-agent",
		AgentType:        agentdom.AgentTypeACP,
		ACPProvider:      agentdom.ACPProviderClaudeCode,
		ParallelismLimit: 3,
	})

	assert.ErrorIs(t, err, agentdom.ErrParallelismLimitRequiresIsolatedSandbox)
}

// TestCreateAgent_RejectsParallelismLimitAboveOneForEnvironmentBacked covers
// the other requiresSerialDispatch case via a provider_cli agent, which
// always has a DefaultEnvironmentID (see TestCreateAgent_ProviderCLI_Success
// for the same environment fixture setup) — an ordinary LLM agent that opts
// into a DefaultEnvironmentID hits the identical check.
func TestCreateAgent_RejectsParallelismLimitAboveOneForEnvironmentBacked(t *testing.T) {
	projectID := uuid.New()
	envID := uuid.New()
	repo := &mockAgentRepo{
		findAgentByHandle: func(_ context.Context, _ uuid.UUID, _ string) (*agentdom.Agent, error) {
			return nil, agentdom.ErrAgentNotFound
		},
		createAgentWithMembership: func(context.Context, *agentdom.Agent, uuid.UUID, uuid.UUID, uuid.UUID) error {
			t.Fatal("createAgentWithMembership must not be called when parallelism_limit is invalid for this agent type")
			return nil
		},
	}
	envSvc := &fakeEnvironmentService{
		getEnvironment: func(_ context.Context, pid, eid uuid.UUID) (*environmentdom.Environment, error) {
			return &environmentdom.Environment{ID: eid, ProjectID: pid}, nil
		},
	}
	svc := New(repo, &mockProjectRepo{}, nil, &mockPluginRepo{}).WithEnvironmentService(envSvc)

	_, err := svc.CreateAgent(context.Background(), projectID, agentdom.CreateAgentInput{
		Name:                 "CLI Agent",
		Handle:               "cli-agent",
		AgentType:            agentdom.AgentTypeProviderCLI,
		CLIProvider:          agentdom.CLIProviderClaudeCode,
		DefaultEnvironmentID: &envID,
		ParallelismLimit:     2,
	})

	assert.ErrorIs(t, err, agentdom.ErrParallelismLimitRequiresIsolatedSandbox)
}

// TestAdvanceQueue_ClaimsConversationBeforeDispatch is the regression guard
// for claimQueuedForDispatch's whole reason to exist: dispatching a
// backlogged conversation must flip it from "queued" to "running" as part
// of the same call that publishes its trigger, not leave that to
// agent-runner's own (asynchronous, unbounded-delay) pickup — see that
// function's doc comment.
func TestAdvanceQueue_ClaimsConversationBeforeDispatch(t *testing.T) {
	agentID := uuid.New()
	convID := uuid.New()
	pending := &agentdom.PendingTrigger{
		ID:             uuid.New(),
		AgentID:        agentID,
		ConversationID: convID,
		Topic:          "agent.task_assigned",
		Payload:        map[string]string{"conversation_id": convID.String()},
		CreatedAt:      time.Now(),
	}
	conv := &agentdom.AgentConversation{ID: convID, AgentID: agentID, Status: "queued"}

	dequeueCalls := 0
	var claimedID uuid.UUID
	var claimedFrom, claimedTo string
	repo := &mockAgentRepo{
		findAgentByID: func(_ context.Context, id uuid.UUID) (*agentdom.Agent, error) {
			return &agentdom.Agent{ID: id, ParallelismLimit: 1}, nil
		},
		countRunningConversations: func(context.Context, uuid.UUID) (int, error) {
			return 0, nil
		},
		findConversationByID: func(_ context.Context, id uuid.UUID) (*agentdom.AgentConversation, error) {
			return conv, nil
		},
		dequeueOldestPendingTrigger: func(context.Context, uuid.UUID) (*agentdom.PendingTrigger, error) {
			dequeueCalls++
			if dequeueCalls > 1 {
				return nil, nil
			}
			return pending, nil
		},
		claimConversationStatus: func(_ context.Context, id uuid.UUID, from, to string) (bool, error) {
			claimedID, claimedFrom, claimedTo = id, from, to
			return true, nil
		},
	}
	svc := New(repo, &mockProjectRepo{}, nil, &mockPluginRepo{})

	dispatched, err := svc.AdvanceQueue(context.Background(), agentID, 1)

	assert.NoError(t, err)
	assert.Equal(t, 1, dispatched)
	assert.Equal(t, convID, claimedID)
	assert.Equal(t, "queued", claimedFrom)
	assert.Equal(t, "running", claimedTo)
}

// TestAdvanceQueue_SkipsPendingTriggerWhenClaimFails is the regression guard
// for the StopConversation-vs-AdvanceQueue race claimQueuedForDispatch
// closes: if something else (most plausibly StopConversation) already moved
// the dequeued conversation out of "queued" by the time this call reaches
// it, the trigger must never be published, the failed claim must not count
// against this call's dispatched budget, and the next pending item (if any)
// still gets a fair attempt.
func TestAdvanceQueue_SkipsPendingTriggerWhenClaimFails(t *testing.T) {
	agentID := uuid.New()
	stoppedConvID := uuid.New()
	nextConvID := uuid.New()
	stoppedPending := &agentdom.PendingTrigger{ID: uuid.New(), AgentID: agentID, ConversationID: stoppedConvID, Topic: "agent.task_assigned"}
	nextPending := &agentdom.PendingTrigger{ID: uuid.New(), AgentID: agentID, ConversationID: nextConvID, Topic: "agent.task_assigned"}
	conversations := map[uuid.UUID]*agentdom.AgentConversation{
		stoppedConvID: {ID: stoppedConvID, AgentID: agentID, Status: "queued"},
		nextConvID:    {ID: nextConvID, AgentID: agentID, Status: "queued"},
	}

	queue := []*agentdom.PendingTrigger{stoppedPending, nextPending}
	repo := &mockAgentRepo{
		findAgentByID: func(_ context.Context, id uuid.UUID) (*agentdom.Agent, error) {
			return &agentdom.Agent{ID: id, ParallelismLimit: 1}, nil
		},
		countRunningConversations: func(context.Context, uuid.UUID) (int, error) {
			return 0, nil
		},
		findConversationByID: func(_ context.Context, id uuid.UUID) (*agentdom.AgentConversation, error) {
			return conversations[id], nil
		},
		dequeueOldestPendingTrigger: func(context.Context, uuid.UUID) (*agentdom.PendingTrigger, error) {
			if len(queue) == 0 {
				return nil, nil
			}
			next := queue[0]
			queue = queue[1:]
			return next, nil
		},
		claimConversationStatus: func(_ context.Context, id uuid.UUID, _, _ string) (bool, error) {
			// Simulates StopConversation having already claimed/moved
			// stoppedConvID out of "queued" between it being queued and
			// AdvanceQueue reaching it.
			return id != stoppedConvID, nil
		},
	}
	svc := New(repo, &mockProjectRepo{}, nil, &mockPluginRepo{})

	dispatched, err := svc.AdvanceQueue(context.Background(), agentID, 2)

	assert.NoError(t, err)
	// Exactly one dispatch: the claim-failed item must not consume any of
	// this call's dispatched budget, but the maxDispatch=2 passed in still
	// bounds it to at most 2 successful claims — only one pending item
	// (nextConvID) was actually claimable, so this must show 1, not 2 (which
	// would mean the failed claim was silently counted as a dispatch) and
	// not 0 (which would mean the second item was never tried at all).
	assert.Equal(t, 1, dispatched)
	assert.Empty(t, queue, "both pending triggers must have been dequeued exactly once")
}

// -------------------------------------------------------------------------
// Folder-level capacity — a second, independent constraint alongside
// ParallelismLimit: at most one conversation, from ANY agent, may run in a
// given (environment_id, folder_id) at once. See checkFolderCapacity's doc
// comment for why the per-agent limit alone can't cover this (two different
// agents sharing one DefaultEnvironmentID, or an explicit per-conversation
// environment/folder override via StartChatSession).
// -------------------------------------------------------------------------

func TestCheckFolderCapacity(t *testing.T) {
	envID := uuid.New()
	folderID := uuid.New()

	t.Run("free returns true", func(t *testing.T) {
		repo := &mockAgentRepo{
			countRunningConversationsInFolder: func(context.Context, uuid.UUID, *uuid.UUID) (int, error) {
				return 0, nil
			},
		}
		svc := New(repo, &mockProjectRepo{}, nil, &mockPluginRepo{})

		ok, err := svc.checkFolderCapacity(context.Background(), envID, &folderID, "")

		assert.NoError(t, err)
		assert.True(t, ok)
	})

	t.Run("occupied and asking returns CodeAgentEnvironmentFolderBusy", func(t *testing.T) {
		repo := &mockAgentRepo{
			countRunningConversationsInFolder: func(context.Context, uuid.UUID, *uuid.UUID) (int, error) {
				return 1, nil
			},
		}
		svc := New(repo, &mockProjectRepo{}, nil, &mockPluginRepo{})

		ok, err := svc.checkFolderCapacity(context.Background(), envID, &folderID, "")

		assert.False(t, ok)
		var apiErr *apierr.Error
		if assert.ErrorAs(t, err, &apiErr) {
			assert.Equal(t, apierr.CodeAgentEnvironmentFolderBusy, apiErr.Code)
		}
	})

	t.Run("occupied and queueing returns false with no error", func(t *testing.T) {
		repo := &mockAgentRepo{
			countRunningConversationsInFolder: func(context.Context, uuid.UUID, *uuid.UUID) (int, error) {
				return 1, nil
			},
		}
		svc := New(repo, &mockProjectRepo{}, nil, &mockPluginRepo{})

		ok, err := svc.checkFolderCapacity(context.Background(), envID, &folderID, agentdom.OnBusyQueue)

		assert.NoError(t, err)
		assert.False(t, ok)
	})

	t.Run("forcing skips the occupancy check entirely", func(t *testing.T) {
		repo := &mockAgentRepo{
			countRunningConversationsInFolder: func(context.Context, uuid.UUID, *uuid.UUID) (int, error) {
				t.Fatal("must not query occupancy at all when forcing")
				return 1, nil
			},
		}
		svc := New(repo, &mockProjectRepo{}, nil, &mockPluginRepo{})

		ok, err := svc.checkFolderCapacity(context.Background(), envID, &folderID, agentdom.OnBusyForce)

		assert.NoError(t, err)
		assert.True(t, ok)
	})
}

// TestCheckDispatchCapacity_FolderBlocksEvenWithAgentCapacity is the
// regression guard for checkDispatchCapacity actually composing both
// constraints: an agent well under its own ParallelismLimit must still be
// blocked when its target folder is occupied by someone else's conversation.
func TestCheckDispatchCapacity_FolderBlocksEvenWithAgentCapacity(t *testing.T) {
	agentID := uuid.New()
	envID := uuid.New()
	folderID := uuid.New()
	repo := &mockAgentRepo{
		findAgentByID: func(_ context.Context, id uuid.UUID) (*agentdom.Agent, error) {
			return &agentdom.Agent{ID: id, ParallelismLimit: 10}, nil
		},
		countRunningConversations: func(context.Context, uuid.UUID) (int, error) {
			return 0, nil // this agent has plenty of room
		},
		countRunningConversationsInFolder: func(context.Context, uuid.UUID, *uuid.UUID) (int, error) {
			return 1, nil // but the folder itself is taken
		},
	}
	svc := New(repo, &mockProjectRepo{}, nil, &mockPluginRepo{})

	dispatchNow, err := svc.checkDispatchCapacity(context.Background(), agentID, &envID, &folderID, agentdom.OnBusyQueue)

	assert.NoError(t, err)
	assert.False(t, dispatchNow, "agent capacity alone must not be enough to dispatch into an occupied folder")
}

// TestAdvanceQueue_RequeuesFolderBlockedItemAndTriesNext is the regression
// guard against the queue-starvation bug a naive "skip and continue" would
// have: item A (older, but its folder is occupied by a different agent)
// must not block item B (newer, folder free) from being tried and
// dispatched within the same AdvanceQueue call — and A must still be sitting
// in agent_pending_triggers afterwards, not lost.
func TestAdvanceQueue_RequeuesFolderBlockedItemAndTriesNext(t *testing.T) {
	agentID := uuid.New()
	envID := uuid.New()
	occupiedFolderID := uuid.New()
	freeFolderID := uuid.New()
	convA := uuid.New()
	convB := uuid.New()
	itemA := &agentdom.PendingTrigger{ID: uuid.New(), AgentID: agentID, ConversationID: convA, Topic: "agent.task_assigned", EnvironmentID: &envID, EnvironmentFolderID: &occupiedFolderID, CreatedAt: time.Now().Add(-time.Minute)}
	itemB := &agentdom.PendingTrigger{ID: uuid.New(), AgentID: agentID, ConversationID: convB, Topic: "agent.task_assigned", EnvironmentID: &envID, EnvironmentFolderID: &freeFolderID, CreatedAt: time.Now()}
	conversations := map[uuid.UUID]*agentdom.AgentConversation{
		convA: {ID: convA, AgentID: agentID, Status: "queued", EnvironmentID: &envID, EnvironmentFolderID: &occupiedFolderID},
		convB: {ID: convB, AgentID: agentID, Status: "queued", EnvironmentID: &envID, EnvironmentFolderID: &freeFolderID},
	}

	queue := []*agentdom.PendingTrigger{itemA, itemB}
	var reinserted []*agentdom.PendingTrigger
	repo := &mockAgentRepo{
		findAgentByID: func(_ context.Context, id uuid.UUID) (*agentdom.Agent, error) {
			return &agentdom.Agent{ID: id, ParallelismLimit: 10}, nil
		},
		countRunningConversations: func(context.Context, uuid.UUID) (int, error) {
			return 0, nil
		},
		countRunningConversationsInFolder: func(_ context.Context, _ uuid.UUID, folderID *uuid.UUID) (int, error) {
			if folderID != nil && *folderID == occupiedFolderID {
				return 1, nil
			}
			return 0, nil
		},
		findConversationByID: func(_ context.Context, id uuid.UUID) (*agentdom.AgentConversation, error) {
			return conversations[id], nil
		},
		dequeueOldestPendingTrigger: func(context.Context, uuid.UUID) (*agentdom.PendingTrigger, error) {
			if len(queue) == 0 {
				return nil, nil
			}
			next := queue[0]
			queue = queue[1:]
			return next, nil
		},
		createPendingTrigger: func(_ context.Context, t *agentdom.PendingTrigger) error {
			reinserted = append(reinserted, t)
			return nil
		},
		claimConversationStatus: func(_ context.Context, _ uuid.UUID, _, _ string) (bool, error) {
			return true, nil
		},
	}
	svc := New(repo, &mockProjectRepo{}, nil, &mockPluginRepo{})

	dispatched, err := svc.AdvanceQueue(context.Background(), agentID, 1)

	assert.NoError(t, err)
	assert.Equal(t, 1, dispatched, "item B must still be dispatched despite item A blocking ahead of it")
	if assert.Len(t, reinserted, 1, "item A must be put back, not dropped") {
		assert.Equal(t, itemA.ID, reinserted[0].ID)
		assert.Equal(t, itemA.CreatedAt, reinserted[0].CreatedAt, "must keep its original FIFO position")
	}
}

// TestAdvanceFolderQueue_DispatchesQueuedItemFromDifferentAgent is the
// regression guard for the whole reason AdvanceFolderQueue exists: the
// conversation that just freed a folder can belong to a different agent
// than whichever one is next in line for it.
func TestAdvanceFolderQueue_DispatchesQueuedItemFromDifferentAgent(t *testing.T) {
	envID := uuid.New()
	folderID := uuid.New()
	otherAgentID := uuid.New()
	convID := uuid.New()
	pending := &agentdom.PendingTrigger{ID: uuid.New(), AgentID: otherAgentID, ConversationID: convID, Topic: "agent.chat_message", EnvironmentID: &envID, EnvironmentFolderID: &folderID}
	conv := &agentdom.AgentConversation{ID: convID, AgentID: otherAgentID, Status: "queued", EnvironmentID: &envID, EnvironmentFolderID: &folderID}

	dequeueCalls := 0
	var claimedID uuid.UUID
	repo := &mockAgentRepo{
		countRunningConversationsInFolder: func(context.Context, uuid.UUID, *uuid.UUID) (int, error) {
			return 0, nil // the folder just freed up
		},
		findAgentByID: func(_ context.Context, id uuid.UUID) (*agentdom.Agent, error) {
			return &agentdom.Agent{ID: id, ParallelismLimit: 1}, nil
		},
		countRunningConversations: func(context.Context, uuid.UUID) (int, error) {
			return 0, nil // otherAgentID has room too
		},
		dequeueOldestPendingTriggerForFolder: func(context.Context, uuid.UUID, *uuid.UUID) (*agentdom.PendingTrigger, error) {
			dequeueCalls++
			if dequeueCalls > 1 {
				return nil, nil
			}
			return pending, nil
		},
		findConversationByID: func(context.Context, uuid.UUID) (*agentdom.AgentConversation, error) {
			return conv, nil
		},
		claimConversationStatus: func(_ context.Context, id uuid.UUID, _, _ string) (bool, error) {
			claimedID = id
			return true, nil
		},
	}
	svc := New(repo, &mockProjectRepo{}, nil, &mockPluginRepo{})

	dispatchedOne, err := svc.AdvanceFolderQueue(context.Background(), envID, &folderID)

	assert.NoError(t, err)
	assert.True(t, dispatchedOne)
	assert.Equal(t, convID, claimedID)
}

// TestAdvanceFolderQueue_RequeuesWhenItsAgentIsBusy covers the mirror image
// of TestAdvanceQueue_RequeuesFolderBlockedItemAndTriesNext: a folder that
// just freed up must not be handed to a queued item whose own agent is
// still at capacity — that item goes back to agent_pending_triggers
// unchanged, for its own agent's AdvanceQueue to pick up once IT has room.
func TestAdvanceFolderQueue_RequeuesWhenItsAgentIsBusy(t *testing.T) {
	envID := uuid.New()
	folderID := uuid.New()
	busyAgentID := uuid.New()
	convID := uuid.New()
	pending := &agentdom.PendingTrigger{ID: uuid.New(), AgentID: busyAgentID, ConversationID: convID, Topic: "agent.chat_message", EnvironmentID: &envID, EnvironmentFolderID: &folderID, CreatedAt: time.Now()}

	dequeueCalls := 0
	var reinserted []*agentdom.PendingTrigger
	repo := &mockAgentRepo{
		countRunningConversationsInFolder: func(context.Context, uuid.UUID, *uuid.UUID) (int, error) {
			return 0, nil
		},
		findAgentByID: func(_ context.Context, id uuid.UUID) (*agentdom.Agent, error) {
			return &agentdom.Agent{ID: id, ParallelismLimit: 1}, nil
		},
		countRunningConversations: func(context.Context, uuid.UUID) (int, error) {
			return 1, nil // busyAgentID is already at its limit elsewhere
		},
		dequeueOldestPendingTriggerForFolder: func(context.Context, uuid.UUID, *uuid.UUID) (*agentdom.PendingTrigger, error) {
			dequeueCalls++
			if dequeueCalls > 1 {
				return nil, nil
			}
			return pending, nil
		},
		createPendingTrigger: func(_ context.Context, t *agentdom.PendingTrigger) error {
			reinserted = append(reinserted, t)
			return nil
		},
	}
	svc := New(repo, &mockProjectRepo{}, nil, &mockPluginRepo{})

	dispatchedOne, err := svc.AdvanceFolderQueue(context.Background(), envID, &folderID)

	assert.NoError(t, err)
	assert.False(t, dispatchedOne)
	if assert.Len(t, reinserted, 1) {
		assert.Equal(t, pending.ID, reinserted[0].ID)
	}
}
