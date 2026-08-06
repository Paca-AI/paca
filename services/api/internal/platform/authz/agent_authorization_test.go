package authz_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Paca-AI/api/internal/platform/authz"
)

type mockAgentRoleResolver struct {
	roles map[uuid.UUID]map[uuid.UUID]string // project_id -> agent_id -> role_name
	// resolveErr, when set, is returned verbatim instead of
	// authz.ErrAgentNotInProject for any agent/project not found in roles —
	// lets tests distinguish "not a member" from a genuine resolver failure.
	resolveErr error
}

func (m *mockAgentRoleResolver) GetAgentProjectRoleName(_ context.Context, agentID, projectID uuid.UUID) (string, error) {
	if projectMap, ok := m.roles[projectID]; ok {
		if role, ok := projectMap[agentID]; ok {
			return role, nil
		}
	}
	if m.resolveErr != nil {
		return "", m.resolveErr
	}
	return "", authz.ErrAgentNotInProject
}

type mockPermissionStore struct {
	globalPerms      map[uuid.UUID][]authz.Permission
	projectPerms     map[uuid.UUID]map[uuid.UUID][]authz.Permission // project_id -> user_id -> permissions
	agentPerms       map[uuid.UUID]map[uuid.UUID][]authz.Permission // project_id -> agent_id -> permissions
	agentGlobalPerms map[uuid.UUID][]authz.Permission               // agent_id -> permissions (via its own global role)
	legacyPerms      map[string][]authz.Permission
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

func (m *mockPermissionStore) ListAgentGlobalPermissions(_ context.Context, agentID uuid.UUID) ([]authz.Permission, error) {
	return m.agentGlobalPerms[agentID], nil
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

// TestHasGlobalPermissionsForAgent_ResolvesViaAgentsOwnGlobalRole verifies
// that a global-scope agent permission check (no project context) is
// resolved from that specific agent's own global-role permissions — not
// merged with, or substituted by, any other actor's permissions. This is
// the authorizer-level counterpart to the middleware regression test in
// internal/transport/http/middleware/authz_test.go, which additionally
// verifies EnforcePermissions actually routes here instead of falling
// through to the shared agent-API-key subject.
func TestHasGlobalPermissionsForAgent_ResolvesViaAgentsOwnGlobalRole(t *testing.T) {
	lowPrivAgent := uuid.New()
	adminAgent := uuid.New()

	permissionStore := &mockPermissionStore{
		agentGlobalPerms: map[uuid.UUID][]authz.Permission{
			// lowPrivAgent has no entry at all -> zero global permissions.
			adminAgent: {authz.PermissionUsersWrite, authz.PermissionGlobalRolesWrite},
		},
	}
	authorizer := authz.NewAuthorizer(permissionStore)

	t.Run("agent with no global role has no global permissions", func(t *testing.T) {
		allowed, err := authorizer.HasGlobalPermissionsForAgent(context.Background(), lowPrivAgent, authz.PermissionUsersWrite)
		require.NoError(t, err)
		assert.False(t, allowed)
	})

	t.Run("agent with an assigned global role gets exactly its permissions", func(t *testing.T) {
		allowed, err := authorizer.HasGlobalPermissionsForAgent(context.Background(), adminAgent, authz.PermissionUsersWrite)
		require.NoError(t, err)
		assert.True(t, allowed)

		disallowed, err := authorizer.HasGlobalPermissionsForAgent(context.Background(), adminAgent, authz.PermissionProjectsDelete)
		require.NoError(t, err)
		assert.False(t, disallowed)
	})

	t.Run("global-scope check never consults ListAgentProjectPermissions", func(t *testing.T) {
		// Sanity check on the mock itself: agentPerms (project-scoped) is nil
		// on this store, so if HasGlobalPermissionsForAgent ever routed through
		// the project-scoped resolver by mistake, this would panic on a nil
		// map dereference rather than silently pass.
		allowed, err := authorizer.HasGlobalPermissionsForAgent(context.Background(), adminAgent, authz.PermissionUsersWrite)
		require.NoError(t, err)
		assert.True(t, allowed)
	})
}

// TestHasPermissionsForAgent_AgentNotInProject verifies that an agent with no
// project_members row (never added, or removed) is denied (allowed=false)
// without an error — regression test for the bug where this case propagated
// as an unhandled error and surfaced to callers as a 500 instead of the
// caller's normal 403 "insufficient permissions" path.
func TestHasPermissionsForAgent_AgentNotInProject(t *testing.T) {
	projectID := uuid.New()
	strangerAgent := uuid.New()

	resolver := &mockAgentRoleResolver{roles: map[uuid.UUID]map[uuid.UUID]string{}}
	authorizer := authz.NewAuthorizer(&mockPermissionStore{}).WithAgentRoleResolver(resolver)

	allowed, err := authorizer.HasPermissionsForAgent(context.Background(), strangerAgent, projectID, authz.PermissionTasksRead)
	require.NoError(t, err, "agent not being a project member must not surface as an error")
	assert.False(t, allowed)
}

// TestHasPermissionsForAgent_ResolverFailure verifies a genuine resolver
// failure (e.g. a DB error) still propagates as an error, so it is not
// silently swallowed into a false "not allowed" the way ErrAgentNotInProject
// deliberately is.
func TestHasPermissionsForAgent_ResolverFailure(t *testing.T) {
	projectID := uuid.New()
	agentID := uuid.New()

	resolver := &mockAgentRoleResolver{
		roles:      map[uuid.UUID]map[uuid.UUID]string{},
		resolveErr: assert.AnError,
	}
	authorizer := authz.NewAuthorizer(&mockPermissionStore{}).WithAgentRoleResolver(resolver)

	allowed, err := authorizer.HasPermissionsForAgent(context.Background(), agentID, projectID, authz.PermissionTasksRead)
	require.Error(t, err)
	assert.False(t, allowed)
	assert.False(t, errors.Is(err, authz.ErrAgentNotInProject))
}
