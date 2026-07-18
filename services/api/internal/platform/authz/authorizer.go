package authz

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

// PermissionStore resolves effective permissions from global and project roles.
type PermissionStore interface {
	ListGlobalPermissions(ctx context.Context, userID uuid.UUID) ([]Permission, error)
	ListProjectPermissions(ctx context.Context, userID, projectID uuid.UUID) ([]Permission, error)
}

// AgentPermissionStore extends PermissionStore with agent-specific permission queries.
type AgentPermissionStore interface {
	PermissionStore
	ListAgentProjectPermissions(ctx context.Context, agentID, projectID uuid.UUID) ([]Permission, error)
}

// AgentRoleResolver resolves an agent's role in a project.
type AgentRoleResolver interface {
	GetAgentProjectRoleName(ctx context.Context, agentID, projectID uuid.UUID) (string, error)
}

// Authorizer checks required permissions for a user or agent.
type Authorizer struct {
	store             PermissionStore
	agentRoleResolver AgentRoleResolver
}

// NewAuthorizer returns a permission-based authorizer.
func NewAuthorizer(store PermissionStore) *Authorizer {
	return &Authorizer{store: store}
}

// WithAgentRoleResolver configures an optional agent role resolver.
func (a *Authorizer) WithAgentRoleResolver(resolver AgentRoleResolver) *Authorizer {
	a.agentRoleResolver = resolver
	return a
}

// HasPermissions reports whether userID has all required permissions in the
// given scope. projectID=nil means global scope only.
func (a *Authorizer) HasPermissions(
	ctx context.Context,
	userID uuid.UUID,
	projectID *uuid.UUID,
	legacyRole string,
	required ...Permission,
) (bool, error) {
	return a.hasPermissionsForActor(ctx, userID, nil, projectID, legacyRole, required...)
}

// HasPermissionsForAgent reports whether an agent has all required permissions in the
// given project scope.
func (a *Authorizer) HasPermissionsForAgent(
	ctx context.Context,
	agentID uuid.UUID,
	projectID uuid.UUID,
	required ...Permission,
) (bool, error) {
	if a.agentRoleResolver == nil {
		return false, fmt.Errorf("authz: agent role resolver not configured")
	}

	roleName, err := a.agentRoleResolver.GetAgentProjectRoleName(ctx, agentID, projectID)
	if err != nil {
		return false, fmt.Errorf("authz: resolve agent role: %w", err)
	}

	return a.hasPermissionsForActor(ctx, uuid.Nil, &agentID, &projectID, roleName, required...)
}

// hasPermissionsForActor is the internal implementation that works for both users and agents.
func (a *Authorizer) hasPermissionsForActor(
	ctx context.Context,
	userID uuid.UUID,
	agentID *uuid.UUID,
	projectID *uuid.UUID,
	legacyRole string,
	required ...Permission,
) (bool, error) {
	if len(required) == 0 {
		return true, nil
	}

	granted, err := a.effectivePermissionsForActor(ctx, userID, agentID, projectID, legacyRole)
	if err != nil {
		return false, err
	}

	for _, req := range required {
		if !hasPermission(map[Permission]struct{}(granted), req) {
			return false, nil
		}
	}

	return true, nil
}

func hasPermission(granted map[Permission]struct{}, required Permission) bool {
	if _, ok := granted[PermissionAll]; ok {
		return true
	}
	if _, ok := granted[required]; ok {
		return true
	}

	req := string(required)
	for p := range granted {
		s := string(p)
		if strings.HasSuffix(s, ".*") {
			prefix := strings.TrimSuffix(s, "*")
			if strings.HasPrefix(req, prefix) {
				return true
			}
		}
	}

	return false
}
