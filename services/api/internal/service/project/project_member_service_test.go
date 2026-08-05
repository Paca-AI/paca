package projectsvc

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"

	agentdom "github.com/Paca-AI/api/internal/domain/agent"
	projectdom "github.com/Paca-AI/api/internal/domain/project"
)

type memberServiceRepoMock struct {
	findByID          func(ctx context.Context, id uuid.UUID) (*projectdom.Project, error)
	findMember        func(ctx context.Context, projectID, userID uuid.UUID) (*projectdom.ProjectMember, error)
	findMemberByAgent func(ctx context.Context, projectID, agentID uuid.UUID) (*projectdom.ProjectMember, error)
	findRoleByID      func(ctx context.Context, id uuid.UUID) (*projectdom.ProjectRole, error)
	updateMemberRole  func(ctx context.Context, projectID, userID, roleID uuid.UUID) error
}

func (m *memberServiceRepoMock) List(context.Context, int, int) ([]*projectdom.Project, int64, error) {
	return nil, 0, nil
}

func (m *memberServiceRepoMock) ListAccessible(_ context.Context, _ uuid.UUID, _, _ int) ([]*projectdom.Project, int64, error) {
	return nil, 0, nil
}

func (m *memberServiceRepoMock) FindByID(ctx context.Context, id uuid.UUID) (*projectdom.Project, error) {
	if m.findByID != nil {
		return m.findByID(ctx, id)
	}
	return nil, projectdom.ErrNotFound
}

func (m *memberServiceRepoMock) Create(context.Context, *projectdom.Project) error {
	return nil
}

func (m *memberServiceRepoMock) Update(context.Context, *projectdom.Project) error {
	return nil
}

func (m *memberServiceRepoMock) Delete(context.Context, uuid.UUID) error {
	return nil
}

func (m *memberServiceRepoMock) ListMembers(context.Context, uuid.UUID) ([]*projectdom.ProjectMember, error) {
	return nil, nil
}

func (m *memberServiceRepoMock) FindMember(ctx context.Context, projectID, userID uuid.UUID) (*projectdom.ProjectMember, error) {
	if m.findMember != nil {
		return m.findMember(ctx, projectID, userID)
	}
	return nil, projectdom.ErrMemberNotFound
}

func (m *memberServiceRepoMock) FindMemberByAgent(ctx context.Context, projectID, agentID uuid.UUID) (*projectdom.ProjectMember, error) {
	if m.findMemberByAgent != nil {
		return m.findMemberByAgent(ctx, projectID, agentID)
	}
	return nil, projectdom.ErrMemberNotFound
}

func (m *memberServiceRepoMock) FindMemberByActor(_ context.Context, projectID, actorID uuid.UUID, agentID *uuid.UUID) (*projectdom.ProjectMember, error) {
	if agentID != nil {
		if m.findMemberByAgent != nil {
			return m.findMemberByAgent(context.Background(), projectID, *agentID)
		}
		return nil, projectdom.ErrMemberNotFound
	}
	if m.findMember != nil {
		return m.findMember(context.Background(), projectID, actorID)
	}
	return nil, projectdom.ErrMemberNotFound
}

func (m *memberServiceRepoMock) FindMemberByUserProject(_ context.Context, _, _ uuid.UUID) (*projectdom.ProjectMember, error) {
	return nil, projectdom.ErrMemberNotFound
}

func (m *memberServiceRepoMock) AddMember(context.Context, *projectdom.ProjectMember) error {
	return nil
}

func (m *memberServiceRepoMock) UpdateMemberRole(ctx context.Context, projectID, userID, roleID uuid.UUID) error {
	if m.updateMemberRole != nil {
		return m.updateMemberRole(ctx, projectID, userID, roleID)
	}
	return nil
}

func (m *memberServiceRepoMock) RemoveMember(context.Context, uuid.UUID, uuid.UUID) error {
	return nil
}

func (m *memberServiceRepoMock) ListRoles(context.Context, uuid.UUID) ([]*projectdom.ProjectRole, error) {
	return nil, nil
}

func (m *memberServiceRepoMock) FindRoleByID(ctx context.Context, id uuid.UUID) (*projectdom.ProjectRole, error) {
	if m.findRoleByID != nil {
		return m.findRoleByID(ctx, id)
	}
	return nil, projectdom.ErrRoleNotFound
}

func (m *memberServiceRepoMock) FindRoleByName(context.Context, uuid.UUID, string) (*projectdom.ProjectRole, error) {
	return nil, projectdom.ErrRoleNotFound
}

func (m *memberServiceRepoMock) CreateRole(context.Context, *projectdom.ProjectRole) error {
	return nil
}

func (m *memberServiceRepoMock) UpdateRole(context.Context, *projectdom.ProjectRole) error {
	return nil
}

func (m *memberServiceRepoMock) DeleteRole(context.Context, uuid.UUID) error {
	return nil
}

func (m *memberServiceRepoMock) CountMembersWithRole(context.Context, uuid.UUID) (int64, error) {
	return 0, nil
}

func (m *memberServiceRepoMock) FindMemberByID(_ context.Context, _ uuid.UUID) (*projectdom.ProjectMember, error) {
	return nil, projectdom.ErrMemberNotFound
}

func (m *memberServiceRepoMock) AddAgentMember(_ context.Context, _, _, _, _ uuid.UUID) error {
	return nil
}

func (m *memberServiceRepoMock) RemoveAgentMember(_ context.Context, _, _ uuid.UUID) error {
	return nil
}

func (m *memberServiceRepoMock) UpdateMemberRoleByMemberID(_ context.Context, _, _ uuid.UUID) error {
	return nil
}

func (m *memberServiceRepoMock) RemoveMemberByMemberID(_ context.Context, _ uuid.UUID) error {
	return nil
}

var _ projectdom.Repository = (*memberServiceRepoMock)(nil)

// memberServiceAgentLookupMock implements the agentLookup interface AddMember
// uses to validate and resolve a global agent being invited.
type memberServiceAgentLookupMock struct {
	findAgentByID     func(ctx context.Context, id uuid.UUID) (*agentdom.Agent, error)
	findAgentByHandle func(ctx context.Context, projectID uuid.UUID, handle string) (*agentdom.Agent, error)
}

func (m *memberServiceAgentLookupMock) FindAgentByID(ctx context.Context, id uuid.UUID) (*agentdom.Agent, error) {
	if m.findAgentByID != nil {
		return m.findAgentByID(ctx, id)
	}
	return nil, agentdom.ErrAgentNotFound
}

func (m *memberServiceAgentLookupMock) FindAgentByHandle(ctx context.Context, projectID uuid.UUID, handle string) (*agentdom.Agent, error) {
	if m.findAgentByHandle != nil {
		return m.findAgentByHandle(ctx, projectID, handle)
	}
	return nil, agentdom.ErrAgentNotFound
}

// TestAddMember_InvitesGlobalAgent covers the "invite" flow: AddMember with
// AgentID set instead of UserID, the same endpoint/action used to add a
// human, just for an already-existing global agent.
func TestAddMember_InvitesGlobalAgent(t *testing.T) {
	projectID := uuid.New()
	agentID := uuid.New()
	roleID := uuid.New()
	agent := &agentdom.Agent{ID: agentID, AgentScope: agentdom.AgentScopeGlobal, Handle: "global-bot"}
	addedMember := &projectdom.ProjectMember{ID: uuid.New(), ProjectID: projectID, AgentID: &agentID, ProjectRoleID: roleID, MemberType: "agent"}

	findMemberByAgentCalls := 0
	repo := &memberServiceRepoMock{
		findByID: func(_ context.Context, id uuid.UUID) (*projectdom.Project, error) {
			return &projectdom.Project{ID: id}, nil
		},
		findRoleByID: func(_ context.Context, id uuid.UUID) (*projectdom.ProjectRole, error) {
			return &projectdom.ProjectRole{ID: id, ProjectID: &projectID}, nil
		},
		findMemberByAgent: func(_ context.Context, _, _ uuid.UUID) (*projectdom.ProjectMember, error) {
			findMemberByAgentCalls++
			if findMemberByAgentCalls == 1 {
				return nil, projectdom.ErrMemberNotFound // not yet a member
			}
			return addedMember, nil // re-fetch after AddAgentMember
		},
	}
	agents := &memberServiceAgentLookupMock{
		findAgentByID: func(_ context.Context, id uuid.UUID) (*agentdom.Agent, error) {
			assert.Equal(t, agentID, id)
			return agent, nil
		},
		findAgentByHandle: func(_ context.Context, _ uuid.UUID, _ string) (*agentdom.Agent, error) {
			return nil, agentdom.ErrAgentNotFound // no handle collision in this project
		},
	}
	svc := New(repo, nil, agents)

	got, err := svc.AddMember(context.Background(), projectID, projectdom.AddMemberInput{
		AgentID:       &agentID,
		ProjectRoleID: roleID,
	})

	assert.NoError(t, err)
	assert.Equal(t, addedMember, got)
}

func TestAddMember_RejectsProjectScopedAgentInvite(t *testing.T) {
	projectID := uuid.New()
	agentID := uuid.New()
	roleID := uuid.New()

	repo := &memberServiceRepoMock{
		findByID: func(_ context.Context, id uuid.UUID) (*projectdom.Project, error) {
			return &projectdom.Project{ID: id}, nil
		},
		findRoleByID: func(_ context.Context, id uuid.UUID) (*projectdom.ProjectRole, error) {
			return &projectdom.ProjectRole{ID: id, ProjectID: &projectID}, nil
		},
	}
	agents := &memberServiceAgentLookupMock{
		findAgentByID: func(_ context.Context, id uuid.UUID) (*agentdom.Agent, error) {
			// A project-scoped agent already belongs to exactly one project —
			// only global-scope agents can be invited into another one.
			return &agentdom.Agent{ID: id, AgentScope: agentdom.AgentScopeProject, ProjectID: uuid.New()}, nil
		},
	}
	svc := New(repo, nil, agents)

	_, err := svc.AddMember(context.Background(), projectID, projectdom.AddMemberInput{
		AgentID:       &agentID,
		ProjectRoleID: roleID,
	})

	assert.ErrorIs(t, err, projectdom.ErrAgentNotInvitable)
}

func TestAddMember_RejectsAlreadyInvitedAgent(t *testing.T) {
	projectID := uuid.New()
	agentID := uuid.New()
	roleID := uuid.New()
	agent := &agentdom.Agent{ID: agentID, AgentScope: agentdom.AgentScopeGlobal, Handle: "global-bot"}

	repo := &memberServiceRepoMock{
		findByID: func(_ context.Context, id uuid.UUID) (*projectdom.Project, error) {
			return &projectdom.Project{ID: id}, nil
		},
		findRoleByID: func(_ context.Context, id uuid.UUID) (*projectdom.ProjectRole, error) {
			return &projectdom.ProjectRole{ID: id, ProjectID: &projectID}, nil
		},
		findMemberByAgent: func(_ context.Context, _, _ uuid.UUID) (*projectdom.ProjectMember, error) {
			return &projectdom.ProjectMember{ID: uuid.New()}, nil // already a member
		},
	}
	agents := &memberServiceAgentLookupMock{
		findAgentByID: func(_ context.Context, _ uuid.UUID) (*agentdom.Agent, error) { return agent, nil },
	}
	svc := New(repo, nil, agents)

	_, err := svc.AddMember(context.Background(), projectID, projectdom.AddMemberInput{
		AgentID:       &agentID,
		ProjectRoleID: roleID,
	})

	assert.ErrorIs(t, err, projectdom.ErrMemberAlreadyAdded)
}

// TestAddMember_RejectsAgentHandleConflict verifies that inviting a global
// agent is rejected outright when its handle collides with a different
// agent already visible in the target project — @mention resolution is
// handle-based within a project, so an ambiguous handle is never allowed.
func TestAddMember_RejectsAgentHandleConflict(t *testing.T) {
	projectID := uuid.New()
	agentID := uuid.New()
	conflictingAgentID := uuid.New()
	roleID := uuid.New()
	agent := &agentdom.Agent{ID: agentID, AgentScope: agentdom.AgentScopeGlobal, Handle: "dev-bot"}

	repo := &memberServiceRepoMock{
		findByID: func(_ context.Context, id uuid.UUID) (*projectdom.Project, error) {
			return &projectdom.Project{ID: id}, nil
		},
		findRoleByID: func(_ context.Context, id uuid.UUID) (*projectdom.ProjectRole, error) {
			return &projectdom.ProjectRole{ID: id, ProjectID: &projectID}, nil
		},
		findMemberByAgent: func(_ context.Context, _, _ uuid.UUID) (*projectdom.ProjectMember, error) {
			return nil, projectdom.ErrMemberNotFound
		},
	}
	agents := &memberServiceAgentLookupMock{
		findAgentByID: func(_ context.Context, _ uuid.UUID) (*agentdom.Agent, error) { return agent, nil },
		findAgentByHandle: func(_ context.Context, _ uuid.UUID, handle string) (*agentdom.Agent, error) {
			assert.Equal(t, "dev-bot", handle)
			return &agentdom.Agent{ID: conflictingAgentID, Handle: handle}, nil
		},
	}
	svc := New(repo, nil, agents)

	_, err := svc.AddMember(context.Background(), projectID, projectdom.AddMemberInput{
		AgentID:       &agentID,
		ProjectRoleID: roleID,
	})

	assert.ErrorIs(t, err, projectdom.ErrAgentHandleConflict)
}

func TestGetMyProjectPermissions_Success(t *testing.T) {
	projectID := uuid.New()
	userID := uuid.New()
	roleID := uuid.New()
	member := &projectdom.ProjectMember{
		ID:            uuid.New(),
		ProjectID:     projectID,
		UserID:        userID,
		ProjectRoleID: roleID,
	}
	role := &projectdom.ProjectRole{
		ID:          roleID,
		ProjectID:   &projectID,
		RoleName:    "Developer",
		Permissions: map[string]any{"tasks.read": true, "tasks.write": true},
	}

	repo := &memberServiceRepoMock{
		findMember: func(_ context.Context, _, _ uuid.UUID) (*projectdom.ProjectMember, error) {
			return member, nil
		},
		findRoleByID: func(_ context.Context, _ uuid.UUID) (*projectdom.ProjectRole, error) {
			return role, nil
		},
	}
	svc := New(repo, nil, nil)

	got, err := svc.GetMyProjectPermissions(context.Background(), projectID, userID, nil)

	assert.NoError(t, err)
	assert.Equal(t, role.Permissions, got)
}

func TestUpdateMemberRole_Success(t *testing.T) {
	projectID := uuid.New()
	userID := uuid.New()
	oldRoleID := uuid.New()
	newRoleID := uuid.New()
	findMemberCalls := 0

	repo := &memberServiceRepoMock{
		findByID: func(_ context.Context, id uuid.UUID) (*projectdom.Project, error) {
			return &projectdom.Project{ID: id}, nil
		},
		findMember: func(_ context.Context, pid, uid uuid.UUID) (*projectdom.ProjectMember, error) {
			findMemberCalls++
			roleID := oldRoleID
			if findMemberCalls > 1 {
				roleID = newRoleID
			}
			return &projectdom.ProjectMember{
				ID:            uuid.New(),
				ProjectID:     pid,
				UserID:        uid,
				ProjectRoleID: roleID,
			}, nil
		},
		findRoleByID: func(_ context.Context, id uuid.UUID) (*projectdom.ProjectRole, error) {
			return &projectdom.ProjectRole{ID: id, ProjectID: &projectID}, nil
		},
		updateMemberRole: func(_ context.Context, pid, uid, rid uuid.UUID) error {
			if pid != projectID || uid != userID || rid != newRoleID {
				t.Fatalf("unexpected ids in update call")
			}
			return nil
		},
	}
	svc := New(repo, nil, nil)

	member, err := svc.UpdateMemberRole(context.Background(), projectID, userID, projectdom.UpdateMemberRoleInput{
		ProjectRoleID: newRoleID,
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if member.ProjectRoleID != newRoleID {
		t.Fatalf("expected role id %s, got %s", newRoleID, member.ProjectRoleID)
	}
	if findMemberCalls != 2 {
		t.Fatalf("expected find member to be called twice, got %d", findMemberCalls)
	}
}

func TestGetMyProjectPermissions_MemberNotFound(t *testing.T) {
	repo := &memberServiceRepoMock{
		findMember: func(_ context.Context, _, _ uuid.UUID) (*projectdom.ProjectMember, error) {
			return nil, projectdom.ErrMemberNotFound
		},
	}
	svc := New(repo, nil, nil)

	_, err := svc.GetMyProjectPermissions(context.Background(), uuid.New(), uuid.New(), nil)
	assert.Error(t, err)
	assert.ErrorIs(t, err, projectdom.ErrMemberNotFound)
}

func TestGetMyProjectPermissions_RoleNotFound(t *testing.T) {
	projectID := uuid.New()
	userID := uuid.New()
	member := &projectdom.ProjectMember{
		ID:            uuid.New(),
		ProjectID:     projectID,
		UserID:        userID,
		ProjectRoleID: uuid.New(),
	}

	repo := &memberServiceRepoMock{
		findMember: func(_ context.Context, _, _ uuid.UUID) (*projectdom.ProjectMember, error) {
			return member, nil
		},
		findRoleByID: func(_ context.Context, _ uuid.UUID) (*projectdom.ProjectRole, error) {
			return nil, projectdom.ErrRoleNotFound
		},
	}
	svc := New(repo, nil, nil)

	_, err := svc.GetMyProjectPermissions(context.Background(), projectID, userID, nil)
	assert.Error(t, err)
	assert.ErrorIs(t, err, projectdom.ErrRoleNotFound)
}

func TestGetMyProjectPermissions_NilPermissions(t *testing.T) {
	projectID := uuid.New()
	userID := uuid.New()
	roleID := uuid.New()
	member := &projectdom.ProjectMember{
		ID:            uuid.New(),
		ProjectID:     projectID,
		UserID:        userID,
		ProjectRoleID: roleID,
	}
	role := &projectdom.ProjectRole{
		ID:          roleID,
		ProjectID:   &projectID,
		RoleName:    "Viewer",
		Permissions: nil,
	}

	repo := &memberServiceRepoMock{
		findMember: func(_ context.Context, _, _ uuid.UUID) (*projectdom.ProjectMember, error) {
			return member, nil
		},
		findRoleByID: func(_ context.Context, _ uuid.UUID) (*projectdom.ProjectRole, error) {
			return role, nil
		},
	}
	svc := New(repo, nil, nil)

	got, err := svc.GetMyProjectPermissions(context.Background(), projectID, userID, nil)

	assert.NoError(t, err)
	assert.NotNil(t, got)
	assert.Empty(t, got)
}

func TestGetMyProjectPermissions_Agent_Success(t *testing.T) {
	projectID := uuid.New()
	agentID := uuid.New()
	roleID := uuid.New()
	member := &projectdom.ProjectMember{
		ID:            uuid.New(),
		ProjectID:     projectID,
		AgentID:       &agentID,
		ProjectRoleID: roleID,
	}
	role := &projectdom.ProjectRole{
		ID:          roleID,
		ProjectID:   &projectID,
		RoleName:    "Agent Developer",
		Permissions: map[string]any{"tasks.read": true, "tasks.write": true, "prs.create": true},
	}

	repo := &memberServiceRepoMock{
		findMemberByAgent: func(_ context.Context, _, _ uuid.UUID) (*projectdom.ProjectMember, error) {
			return member, nil
		},
		findRoleByID: func(_ context.Context, _ uuid.UUID) (*projectdom.ProjectRole, error) {
			return role, nil
		},
	}
	svc := New(repo, nil, nil)

	got, err := svc.GetMyProjectPermissions(context.Background(), projectID, uuid.Nil, &agentID)

	assert.NoError(t, err)
	assert.Equal(t, role.Permissions, got)
}

func TestGetMyProjectPermissions_Agent_MemberNotFound(t *testing.T) {
	repo := &memberServiceRepoMock{
		findMemberByAgent: func(_ context.Context, _, _ uuid.UUID) (*projectdom.ProjectMember, error) {
			return nil, projectdom.ErrMemberNotFound
		},
	}
	svc := New(repo, nil, nil)

	agentID := uuid.New()
	_, err := svc.GetMyProjectPermissions(context.Background(), uuid.New(), uuid.Nil, &agentID)
	assert.Error(t, err)
	assert.ErrorIs(t, err, projectdom.ErrMemberNotFound)
}

func TestGetMyProjectPermissions_Agent_RoleNotFound(t *testing.T) {
	projectID := uuid.New()
	agentID := uuid.New()
	member := &projectdom.ProjectMember{
		ID:            uuid.New(),
		ProjectID:     projectID,
		AgentID:       &agentID,
		ProjectRoleID: uuid.New(),
	}

	repo := &memberServiceRepoMock{
		findMemberByAgent: func(_ context.Context, _, _ uuid.UUID) (*projectdom.ProjectMember, error) {
			return member, nil
		},
		findRoleByID: func(_ context.Context, _ uuid.UUID) (*projectdom.ProjectRole, error) {
			return nil, projectdom.ErrRoleNotFound
		},
	}
	svc := New(repo, nil, nil)

	_, err := svc.GetMyProjectPermissions(context.Background(), projectID, uuid.Nil, &agentID)
	assert.Error(t, err)
	assert.ErrorIs(t, err, projectdom.ErrRoleNotFound)
}

func TestGetMyProjectPermissions_Agent_NilPermissions(t *testing.T) {
	projectID := uuid.New()
	agentID := uuid.New()
	roleID := uuid.New()
	member := &projectdom.ProjectMember{
		ID:            uuid.New(),
		ProjectID:     projectID,
		AgentID:       &agentID,
		ProjectRoleID: roleID,
	}
	role := &projectdom.ProjectRole{
		ID:          roleID,
		ProjectID:   &projectID,
		RoleName:    "Agent Viewer",
		Permissions: nil,
	}

	repo := &memberServiceRepoMock{
		findMemberByAgent: func(_ context.Context, _, _ uuid.UUID) (*projectdom.ProjectMember, error) {
			return member, nil
		},
		findRoleByID: func(_ context.Context, _ uuid.UUID) (*projectdom.ProjectRole, error) {
			return role, nil
		},
	}
	svc := New(repo, nil, nil)

	got, err := svc.GetMyProjectPermissions(context.Background(), projectID, uuid.Nil, &agentID)

	assert.NoError(t, err)
	assert.NotNil(t, got)
	assert.Empty(t, got)
}
