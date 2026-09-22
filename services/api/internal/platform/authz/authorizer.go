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
	// ListAgentGlobalPermissions returns permissions granted by a global
	// agent's own global role, for checks with no project context. Mirrors
	// ListGlobalPermissions for users.
	ListAgentGlobalPermissions(ctx context.Context, agentID uuid.UUID) ([]Permission, error)
}

// Authorizer checks required permissions for a user or agent.
type Authorizer struct {
	store PermissionStore
}

// NewAuthorizer returns a permission-based authorizer.
func NewAuthorizer(store PermissionStore) *Authorizer {
	return &Authorizer{store: store}
}

// HasPermissions reports whether userID has all required permissions in the
// given scope. projectID=nil means global scope only.
//
// Permissions come exclusively from what the user's assigned roles actually
// store (see PermissionStore) — never from a role's *name*. A name such as
// "ADMIN" (for instance the role claim carried in a JWT) confers nothing on
// its own, so editing or stripping a role's stored permissions takes effect
// immediately, and a stale or mismatched name can never grant access the
// stored role does not.
func (a *Authorizer) HasPermissions(
	ctx context.Context,
	userID uuid.UUID,
	projectID *uuid.UUID,
	required ...Permission,
) (bool, error) {
	return a.hasPermissionsForActor(ctx, userID, nil, projectID, required...)
}

// HasPermissionsForAgent reports whether an agent has all required permissions in the
// given project scope.
//
// Permissions come from the permissions its project role stores
// (PermissionStore's ListAgentProjectPermissions), never from what the role
// happens to be called. An agent with no active membership in the project
// simply has none there, so it is denied — not an error, which callers would
// surface as a 500 instead of their normal 403.
func (a *Authorizer) HasPermissionsForAgent(
	ctx context.Context,
	agentID uuid.UUID,
	projectID uuid.UUID,
	required ...Permission,
) (bool, error) {
	return a.hasPermissionsForActor(ctx, uuid.Nil, &agentID, &projectID, required...)
}

// HasGlobalPermissionsForAgent reports whether agentID has all required
// permissions via its own global role, with no project context. Distinct
// from HasPermissionsForAgent, which resolves permissions via the agent's
// project_members role within one specific project.
func (a *Authorizer) HasGlobalPermissionsForAgent(
	ctx context.Context,
	agentID uuid.UUID,
	required ...Permission,
) (bool, error) {
	return a.hasPermissionsForActor(ctx, uuid.Nil, &agentID, nil, required...)
}

// hasPermissionsForActor is the internal implementation that works for both users and agents.
func (a *Authorizer) hasPermissionsForActor(
	ctx context.Context,
	userID uuid.UUID,
	agentID *uuid.UUID,
	projectID *uuid.UUID,
	required ...Permission,
) (bool, error) {
	if len(required) == 0 {
		return true, nil
	}

	// projectScoped gates how much a *global* grant (an explicitly-assigned
	// global role via ListGlobalPermissions) is allowed to contribute below:
	// everywhere when the check is global, but only its PermissionAll entry —
	// never a named permission — once the check is scoped to one project. See
	// addGlobalGrants.
	projectScoped := projectID != nil

	// granted starts empty and is filled only from the permission store: no
	// role name (legacy claim, project role name, ...) ever contributes a
	// grant here. With no store configured nothing is granted, i.e. the
	// authorizer fails closed.
	granted := make(map[Permission]struct{})

	if a.store != nil {
		if userID != uuid.Nil {
			globalPerms, err := a.store.ListGlobalPermissions(ctx, userID)
			if err != nil {
				return false, fmt.Errorf("authz: list global permissions: %w", err)
			}
			addGlobalGrants(granted, globalPerms, projectScoped)
		} else if agentID != nil && projectID == nil {
			// Global-scope check for an agent (HasGlobalPermissionsForAgent):
			// resolve via the agent's own global role, mirroring the userID
			// branch above. Never reached from HasPermissionsForAgent, which
			// always passes a non-nil projectID.
			agentStore, ok := a.store.(AgentPermissionStore)
			if !ok {
				return false, fmt.Errorf("authz: agent global permissions not supported by store")
			}
			globalPerms, err := agentStore.ListAgentGlobalPermissions(ctx, *agentID)
			if err != nil {
				return false, fmt.Errorf("authz: list agent global permissions: %w", err)
			}
			// projectScoped is always false here (guarded by projectID ==
			// nil above), so this is a full, unfiltered merge — same as
			// every other global grant at global scope.
			addGlobalGrants(granted, globalPerms, projectScoped)
		}

		if projectID != nil {
			var projectPerms []Permission
			var err error

			if agentID != nil {
				agentStore, ok := a.store.(AgentPermissionStore)
				if !ok {
					return false, fmt.Errorf("authz: agent project permissions not supported by store")
				}
				projectPerms, err = agentStore.ListAgentProjectPermissions(ctx, *agentID, *projectID)
			} else {
				projectPerms, err = a.store.ListProjectPermissions(ctx, userID, *projectID)
			}

			if err != nil {
				return false, fmt.Errorf("authz: list project permissions: %w", err)
			}
			for _, p := range projectPerms {
				granted[p] = struct{}{}
			}
		}
	}

	for _, req := range required {
		if !hasPermission(granted, req) {
			return false, nil
		}
	}

	return true, nil
}

// addGlobalGrants merges perms — permissions from a *global* source (an
// explicitly-assigned global role) — into granted.
//
// Regression guard for GHSA-hjcj-373w-vq8m: a global source is allowed to
// satisfy a project-scoped check (projectScoped=true) only via the universal
// PermissionAll wildcard — the same "god mode" SUPER_ADMIN holds through its
// stored role and any other global role given an explicit PermissionAll.
// Every other, named permission a global role carries (users.*, projects.*,
// agents.*, ...) must not reach into a specific project's resources on its
// own; only that project's own membership grant (ListProjectPermissions /
// ListAgentProjectPermissions) can do that. Without this, any permission
// namespace a global role happens to share with a project-scoped permission
// (agents.* and projects.* both gate project-scoped routes too — see
// router.go) re-opens the exact "global role reaches any project without
// membership" bug this advisory reports, just narrower than the bare
// wildcard.
func addGlobalGrants(granted map[Permission]struct{}, perms []Permission, projectScoped bool) {
	for _, p := range perms {
		if projectScoped && p != PermissionAll {
			continue
		}
		granted[p] = struct{}{}
	}
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
