package authz

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

// PermissionSet is a set of granted permission keys. It is used for
// grant-ceiling checks: a caller must not create or assign a role whose
// permission set is broader than the caller's own effective permissions.
type PermissionSet map[Permission]struct{}

// Grants reports whether the set grants required, honoring the "*" superuser
// wildcard and "prefix.*" family wildcards (identical semantics to the
// authorizer's own permission matching).
func (s PermissionSet) Grants(required Permission) bool {
	return hasPermission(map[Permission]struct{}(s), required)
}

// HasAll reports whether the set holds the "*" superuser permission
// (SUPER_ADMIN). Such callers bypass the grant ceiling entirely.
func (s PermissionSet) HasAll() bool {
	_, ok := s[PermissionAll]
	return ok
}

// Covers reports whether the set grants every permission in wanted. An empty
// wanted list is trivially covered. Callers with "*" cover everything.
func (s PermissionSet) Covers(wanted []Permission) bool {
	for _, w := range wanted {
		if !s.Grants(w) {
			return false
		}
	}
	return true
}

// PermissionsFromMap extracts the granted permission keys from a role's
// permission map. A key is granted when its value is boolean true, a nonzero
// number, or the string "true" — the same interpretation the persistence layer
// applies when it materializes a role's permissions (see the postgres authz
// permission store). Falsy entries are ignored so a role can carry an explicit
// {"x": false} without that counting as a grant.
func PermissionsFromMap(m map[string]any) []Permission {
	if len(m) == 0 {
		return nil
	}
	out := make([]Permission, 0, len(m))
	for key, enabled := range m {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		granted := false
		switch e := enabled.(type) {
		case bool:
			granted = e
		case float64:
			granted = e != 0
		case int:
			granted = e != 0
		case string:
			granted = strings.EqualFold(e, "true")
		}
		if granted {
			out = append(out, Permission(key))
		}
	}
	return out
}

// EffectivePermissions returns the full set of permissions granted to the user
// in the given scope: the legacy role's implicit permissions unioned with the
// permissions from the user's persisted global role and — when projectID is
// non-nil — their project role. It mirrors exactly the union that
// HasPermissions evaluates, exposed as a set so callers can enforce a grant
// ceiling (e.g. reject creating a role broader than the caller's own).
func (a *Authorizer) EffectivePermissions(
	ctx context.Context,
	userID uuid.UUID,
	projectID *uuid.UUID,
	legacyRole string,
) (PermissionSet, error) {
	return a.effectivePermissionsForActor(ctx, userID, nil, projectID, legacyRole)
}

// EffectivePermissionsForAgent returns the full set of permissions granted to
// an agent acting in the given project scope, for use in grant-ceiling checks.
func (a *Authorizer) EffectivePermissionsForAgent(
	ctx context.Context,
	agentID uuid.UUID,
	projectID uuid.UUID,
) (PermissionSet, error) {
	if a.agentRoleResolver == nil {
		return nil, fmt.Errorf("authz: agent role resolver not configured")
	}
	roleName, err := a.agentRoleResolver.GetAgentProjectRoleName(ctx, agentID, projectID)
	if err != nil {
		return nil, fmt.Errorf("authz: resolve agent role: %w", err)
	}
	return a.effectivePermissionsForActor(ctx, uuid.Nil, &agentID, &projectID, roleName)
}

// effectivePermissionsForActor gathers the granted permission set for a user or
// agent. It is the shared core of both HasPermissions and EffectivePermissions
// so the two can never diverge.
func (a *Authorizer) effectivePermissionsForActor(
	ctx context.Context,
	userID uuid.UUID,
	agentID *uuid.UUID,
	projectID *uuid.UUID,
	legacyRole string,
) (PermissionSet, error) {
	granted := make(PermissionSet)
	for _, p := range LegacyPermissionsForRole(legacyRole) {
		granted[p] = struct{}{}
	}

	if a.store == nil {
		return granted, nil
	}

	if userID != uuid.Nil {
		globalPerms, err := a.store.ListGlobalPermissions(ctx, userID)
		if err != nil {
			return nil, fmt.Errorf("authz: list global permissions: %w", err)
		}
		for _, p := range globalPerms {
			granted[p] = struct{}{}
		}
	}

	if projectID != nil {
		var projectPerms []Permission
		var err error
		if agentID != nil {
			agentStore, ok := a.store.(AgentPermissionStore)
			if !ok {
				return nil, fmt.Errorf("authz: agent project permissions not supported by store")
			}
			projectPerms, err = agentStore.ListAgentProjectPermissions(ctx, *agentID, *projectID)
		} else {
			projectPerms, err = a.store.ListProjectPermissions(ctx, userID, *projectID)
		}
		if err != nil {
			return nil, fmt.Errorf("authz: list project permissions: %w", err)
		}
		for _, p := range projectPerms {
			granted[p] = struct{}{}
		}
	}

	return granted, nil
}
