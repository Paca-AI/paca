package authz_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Paca-AI/api/internal/platform/authz"
)

type mockAgentRoleResolver struct {
	roles map[uuid.UUID]map[uuid.UUID]string // project_id -> agent_id -> role_name
}

func (m *mockAgentRoleResolver) GetAgentProjectRoleName(_ context.Context, agentID, projectID uuid.UUID) (string, error) {
	if projectMap, ok := m.roles[projectID]; ok {
		if role, ok := projectMap[agentID]; ok {
			return role, nil
		}
	}
	return "", assert.AnError
}

type mockPermissionStore struct {
	globalPerms  map[uuid.UUID][]authz.Permission
	projectPerms map[uuid.UUID]map[uuid.UUID][]authz.Permission // project_id -> user_id -> permissions
	agentPerms   map[uuid.UUID]map[uuid.UUID][]authz.Permission // project_id -> agent_id -> permissions
	legacyPerms  map[string][]authz.Permission
}

func (m *mockPermissionStore) ListGlobalPermissions(_ context.Context, userID uuid.UUID) ([]authz.Permission, error) {
	return m.globalPerms[userID], nil
}

func (m *mockPermissionStore) ListProjectPermissions(_ context.Context, userID, projectID uuid.UUID) ([]authz.Permission, error) {
	if projMap, ok := m.projectPerms[projectID]; ok {
		return projMap[userID], nil
	}
	return nil, nil
}

func (m *mockPermissionStore) ListAgentProjectPermissions(_ context.Context, agentID, projectID uuid.UUID) ([]authz.Permission, error) {
	if projMap, ok := m.agentPerms[projectID]; ok {
		return projMap[agentID], nil
	}
	return nil, nil
}

func TestAgentAuthorization(t *testing.T) {
	projectID := uuid.New()
	agentID := uuid.New()
	userID := uuid.New()

	agentRoleResolver := &mockAgentRoleResolver{
		roles: map[uuid.UUID]map[uuid.UUID]string{
			projectID: {
				agentID: "agent_developer",
			},
		},
	}

	permissionStore := &mockPermissionStore{
		agentPerms: map[uuid.UUID]map[uuid.UUID][]authz.Permission{
			projectID: {
				agentID: {authz.PermissionTasksRead, authz.PermissionTasksWrite},
			},
		},
		legacyPerms: map[string][]authz.Permission{
			"agent_developer": {authz.PermissionTasksRead, authz.PermissionTasksWrite},
		},
	}

	authorizer := authz.NewAuthorizer(permissionStore).WithAgentRoleResolver(agentRoleResolver)

	t.Run("agent has correct project permissions", func(t *testing.T) {
		allowed, err := authorizer.HasPermissionsForAgent(context.Background(), agentID, projectID, authz.PermissionTasksRead)
		require.NoError(t, err)
		assert.True(t, allowed)
	})

	t.Run("agent lacks missing project permissions", func(t *testing.T) {
		allowed, err := authorizer.HasPermissionsForAgent(context.Background(), agentID, projectID, authz.PermissionProjectsWrite)
		require.NoError(t, err)
		assert.False(t, allowed)
	})

	t.Run("user permissions remain unchanged", func(t *testing.T) {
		allowed, err := authorizer.HasPermissions(context.Background(), userID, &projectID, "user", authz.PermissionTasksRead)
		require.NoError(t, err)
		assert.False(t, allowed)
	})
}

func TestAgentAuthorizationWithMultipleProjects(t *testing.T) {
	project1 := uuid.New()
	project2 := uuid.New()
	agentID := uuid.New()

	agentRoleResolver := &mockAgentRoleResolver{
		roles: map[uuid.UUID]map[uuid.UUID]string{
			project1: {
				agentID: "agent_developer",
			},
			project2: {
				agentID: "agent_reader",
			},
		},
	}

	permissionStore := &mockPermissionStore{
		agentPerms: map[uuid.UUID]map[uuid.UUID][]authz.Permission{
			project1: {
				agentID: {authz.PermissionTasksRead, authz.PermissionTasksWrite},
			},
			project2: {
				agentID: {authz.PermissionTasksRead},
			},
		},
		legacyPerms: map[string][]authz.Permission{
			"agent_developer": {authz.PermissionTasksRead, authz.PermissionTasksWrite},
			"agent_reader":    {authz.PermissionTasksRead},
		},
	}

	authorizer := authz.NewAuthorizer(permissionStore).WithAgentRoleResolver(agentRoleResolver)

	t.Run("agent has write permission in project1", func(t *testing.T) {
		allowed, err := authorizer.HasPermissionsForAgent(context.Background(), agentID, project1, authz.PermissionTasksWrite)
		require.NoError(t, err)
		assert.True(t, allowed)
	})

	t.Run("agent lacks write permission in project2", func(t *testing.T) {
		allowed, err := authorizer.HasPermissionsForAgent(context.Background(), agentID, project2, authz.PermissionTasksWrite)
		require.NoError(t, err)
		assert.False(t, allowed)
	})

	t.Run("agent has read permission in both projects", func(t *testing.T) {
		allowed1, err := authorizer.HasPermissionsForAgent(context.Background(), agentID, project1, authz.PermissionTasksRead)
		require.NoError(t, err)
		assert.True(t, allowed1)

		allowed2, err := authorizer.HasPermissionsForAgent(context.Background(), agentID, project2, authz.PermissionTasksRead)
		require.NoError(t, err)
		assert.True(t, allowed2)
	})
}
