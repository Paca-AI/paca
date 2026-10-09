package projectsvc

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"

	agentdom "github.com/Paca-AI/api/internal/domain/agent"
	projectdom "github.com/Paca-AI/api/internal/domain/project"
	roledom "github.com/Paca-AI/api/internal/domain/role"
)

type memberServiceRepoMock struct {
	findByID          func(ctx context.Context, id uuid.UUID) (*projectdom.Project, error)
	findMember        func(ctx context.Context, projectID, userID uuid.UUID) (*projectdom.ProjectMember, error)
	findMemberByAgent func(ctx context.Context, projectID, agentID uuid.UUID) (*projectdom.ProjectMember, error)
	updateMemberRole  func(ctx context.Context, projectID, userID, roleID uuid.UUID) error
	addMember         func(ctx context.Context, m *projectdom.ProjectMember) error
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

func (m *memberServiceRepoMock) Create(context.Context, *projectdom.Project, projectdom.ProjectSetup) error {
	return nil
}

func (m *memberServiceRepoMock) Update(context.Context, *projectdom.Project) error {
	return nil
}

func (m *memberServiceRepoMock) UpdateJevConfig(context.Context, uuid.UUID, string, string, string) error {
	return nil
}

func (m *memberServiceRepoMock) Delete(context.Context, uuid.UUID) error {
	return nil
}

func (m *memberServiceRepoMock) ListMembers(context.Context, uuid.UUID) ([]*projectdom.ProjectMember, error) {
	return nil, nil
}

func (m *memberServiceRepoMock) CountDistinctAgentsByProjects(context.Context, []uuid.UUID) (int64, error) {
	return 0, nil
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

func (m *memberServiceRepoMock) AddMember(ctx context.Context, member *projectdom.ProjectMember, _ []uuid.UUID, _ *uuid.UUID) error {
	if m.addMember != nil {
		return m.addMember(ctx, member)
	}
	return nil
}

func (m *memberServiceRepoMock) RemoveMember(context.Context, uuid.UUID, uuid.UUID) error {
	return nil
}

func (m *memberServiceRepoMock) FindMemberByID(_ context.Context, _ uuid.UUID) (*projectdom.ProjectMember, error) {
	return nil, projectdom.ErrMemberNotFound
}

func (m *memberServiceRepoMock) AddAgentMember(_ context.Context, _, _, _ uuid.UUID, _ []uuid.UUID, _ *uuid.UUID) error {
	return nil
}

func (m *memberServiceRepoMock) RemoveAgentMember(_ context.Context, _, _ uuid.UUID) error {
	return nil
}

func (m *memberServiceRepoMock) UpdateMemberDescription(_ context.Context, _ uuid.UUID, _ string) error {
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

func TestAddMember_HumanStoresTrimmedDescription(t *testing.T) {
	projectID, userID, roleID := uuid.New(), uuid.New(), uuid.New()
	var stored *projectdom.ProjectMember
	repo := &memberServiceRepoMock{
		findByID: func(_ context.Context, id uuid.UUID) (*projectdom.Project, error) {
			return &projectdom.Project{ID: id}, nil
		},
		findMember: func(_ context.Context, _, _ uuid.UUID) (*projectdom.ProjectMember, error) {
			if stored == nil {
				return nil, projectdom.ErrMemberNotFound // not yet a member
			}
			return stored, nil // re-fetch after AddMember
		},
		addMember: func(_ context.Context, m *projectdom.ProjectMember) error {
			stored = m
			return nil
		},
	}
	svc := New(repo, nil, nil)

	got, err := svc.AddMember(context.Background(), projectID, projectdom.AddMemberInput{
		UserID:      userID,
		RoleIDs:     []uuid.UUID{roleID},
		Description: "  Frontend lead on this project  ",
	})

	assert.NoError(t, err)
	assert.Equal(t, "Frontend lead on this project", got.Description)
}

// TestAddMember_InvitesGlobalAgent covers the "invite" flow: AddMember with
// AgentID set instead of UserID, the same endpoint/action used to add a
// human, just for an already-existing global agent.
func TestAddMember_InvitesGlobalAgent(t *testing.T) {
	projectID := uuid.New()
	agentID := uuid.New()
	roleID := uuid.New()
	agent := &agentdom.Agent{ID: agentID, AgentScope: agentdom.AgentScopeGlobal, Handle: "global-bot"}
	addedMember := &projectdom.ProjectMember{ID: uuid.New(), ProjectID: projectID, AgentID: &agentID, MemberType: "agent"}

	findMemberByAgentCalls := 0
	repo := &memberServiceRepoMock{
		findByID: func(_ context.Context, id uuid.UUID) (*projectdom.Project, error) {
			return &projectdom.Project{ID: id}, nil
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
		AgentID: &agentID,
		RoleIDs: []uuid.UUID{roleID},
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
		AgentID: &agentID,
		RoleIDs: []uuid.UUID{roleID},
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
		findMemberByAgent: func(_ context.Context, _, _ uuid.UUID) (*projectdom.ProjectMember, error) {
			return &projectdom.ProjectMember{ID: uuid.New()}, nil // already a member
		},
	}
	agents := &memberServiceAgentLookupMock{
		findAgentByID: func(_ context.Context, _ uuid.UUID) (*agentdom.Agent, error) { return agent, nil },
	}
	svc := New(repo, nil, agents)

	_, err := svc.AddMember(context.Background(), projectID, projectdom.AddMemberInput{
		AgentID: &agentID,
		RoleIDs: []uuid.UUID{roleID},
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
		AgentID: &agentID,
		RoleIDs: []uuid.UUID{roleID},
	})

	assert.ErrorIs(t, err, projectdom.ErrAgentHandleConflict)
}

type recordingInvalidator struct{ ids []string }

func (r *recordingInvalidator) Invalidate(ids ...string) { r.ids = append(r.ids, ids...) }

func TestAddMember_RequiresRoles(t *testing.T) {
	projectID := uuid.New()
	repo := &memberServiceRepoMock{
		findByID: func(_ context.Context, id uuid.UUID) (*projectdom.Project, error) {
			return &projectdom.Project{ID: id}, nil
		},
		addMember: func(context.Context, *projectdom.ProjectMember) error {
			t.Fatal("a member must not be stored without roles")
			return nil
		},
	}
	svc := New(repo, nil, nil)

	_, err := svc.AddMember(context.Background(), projectID, projectdom.AddMemberInput{UserID: uuid.New()})
	assert.ErrorIs(t, err, roledom.ErrRoleRequired)
}

// The roles go to the repository (which writes them with the membership row
// in one transaction) and the policy cache is told which roles gained a holder.
func TestAddMember_HandsRolesToTheRepositoryAndInvalidates(t *testing.T) {
	projectID, userID, roleA, roleB := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	creator := uuid.New()
	var gotRoles []uuid.UUID
	var gotBy *uuid.UUID
	var stored *projectdom.ProjectMember
	repo := &roleCapturingRepo{memberServiceRepoMock: &memberServiceRepoMock{
		findByID: func(_ context.Context, id uuid.UUID) (*projectdom.Project, error) {
			return &projectdom.Project{ID: id}, nil
		},
		findMember: func(context.Context, uuid.UUID, uuid.UUID) (*projectdom.ProjectMember, error) {
			if stored == nil {
				return nil, projectdom.ErrMemberNotFound
			}
			return stored, nil
		},
	}, onAdd: func(m *projectdom.ProjectMember, ids []uuid.UUID, by *uuid.UUID) {
		stored, gotRoles, gotBy = m, ids, by
	}}
	inv := &recordingInvalidator{}
	svc := New(repo, nil, nil).WithRoleInvalidator(inv)

	_, err := svc.AddMember(context.Background(), projectID, projectdom.AddMemberInput{
		UserID: userID, RoleIDs: []uuid.UUID{roleA, roleB}, CreatedBy: &creator,
	})
	assert.NoError(t, err)
	assert.Equal(t, []uuid.UUID{roleA, roleB}, gotRoles)
	assert.Equal(t, &creator, gotBy)
	assert.ElementsMatch(t, []string{roleA.String(), roleB.String()}, inv.ids)
}

// A role the repository refuses (unknown, or another project's) fails the add.
func TestAddMember_RoleNotAttachable(t *testing.T) {
	repo := &roleCapturingRepo{memberServiceRepoMock: &memberServiceRepoMock{
		findByID: func(_ context.Context, id uuid.UUID) (*projectdom.Project, error) {
			return &projectdom.Project{ID: id}, nil
		},
	}, err: roledom.ErrNotAttachable}
	inv := &recordingInvalidator{}
	svc := New(repo, nil, nil).WithRoleInvalidator(inv)

	_, err := svc.AddMember(context.Background(), uuid.New(), projectdom.AddMemberInput{
		UserID: uuid.New(), RoleIDs: []uuid.UUID{uuid.New()},
	})
	assert.ErrorIs(t, err, roledom.ErrNotAttachable)
	assert.Empty(t, inv.ids, "nothing was attached, nothing to invalidate")
}

// roleCapturingRepo records the roles AddMember hands to the repository.
type roleCapturingRepo struct {
	*memberServiceRepoMock
	onAdd func(*projectdom.ProjectMember, []uuid.UUID, *uuid.UUID)
	err   error
}

func (r *roleCapturingRepo) AddMember(_ context.Context, m *projectdom.ProjectMember, ids []uuid.UUID, by *uuid.UUID) error {
	if r.err != nil {
		return r.err
	}
	if r.onAdd != nil {
		r.onAdd(m, ids, by)
	}
	return nil
}
