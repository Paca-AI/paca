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

// Authorizer checks required permissions for a user.
type Authorizer struct {
	store PermissionStore
}

// NewAuthorizer returns a permission-based authorizer.
func NewAuthorizer(store PermissionStore) *Authorizer {
	return &Authorizer{store: store}
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
	if len(required) == 0 {
		return true, nil
	}

	granted := make(map[Permission]struct{})
	for _, p := range LegacyPermissionsForRole(legacyRole) {
		granted[p] = struct{}{}
	}

	if a.store != nil {
		globalPerms, err := a.store.ListGlobalPermissions(ctx, userID)
		if err != nil {
			return false, fmt.Errorf("authz: list global permissions: %w", err)
		}
		for _, p := range globalPerms {
			granted[p] = struct{}{}
		}

		if projectID != nil {
			projectPerms, err := a.store.ListProjectPermissions(ctx, userID, *projectID)
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
