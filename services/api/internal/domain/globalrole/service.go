package globalroledom

import (
	"context"

	"github.com/Paca-AI/api/internal/platform/authz"
	"github.com/google/uuid"
)

// CreateInput carries fields for creating a new global role.
type CreateInput struct {
	Name        string
	Permissions map[string]any
}

// UpdateInput carries mutable fields of a global role.
type UpdateInput struct {
	Name        string
	Permissions map[string]any
}

// Service defines the global role management use cases.
//
// Create, Update, and ReplaceUserRoles take the caller's own effective
// permission set and enforce a grant ceiling: a role may not be created,
// modified, or assigned so as to grant a permission the caller does not itself
// hold. Callers holding "*" (SUPER_ADMIN) bypass the ceiling.
type Service interface {
	List(ctx context.Context) ([]*GlobalRole, error)
	Create(ctx context.Context, in CreateInput, caller authz.PermissionSet) (*GlobalRole, error)
	Update(ctx context.Context, id uuid.UUID, in UpdateInput, caller authz.PermissionSet) (*GlobalRole, error)
	Delete(ctx context.Context, id uuid.UUID) error
	ReplaceUserRoles(ctx context.Context, userID uuid.UUID, roleIDs []uuid.UUID, caller authz.PermissionSet) ([]*GlobalRole, error)
}
