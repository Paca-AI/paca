package projectdom

import (
	"context"

	"github.com/google/uuid"
)

// CreateRoleInput carries fields for creating a project-scoped role.
type CreateRoleInput struct {
	RoleName    string
	Permissions map[string]any
}

// UpdateRoleInput carries mutable fields for a project role.
type UpdateRoleInput struct {
	RoleName    string
	Permissions map[string]any
}

// RoleService defines role management use cases.
type RoleService interface {
	ListRoles(ctx context.Context, projectID uuid.UUID) ([]*ProjectRole, error)
	CreateRole(ctx context.Context, projectID uuid.UUID, in CreateRoleInput) (*ProjectRole, error)
	UpdateRole(ctx context.Context, projectID, roleID uuid.UUID, in UpdateRoleInput) (*ProjectRole, error)
	DeleteRole(ctx context.Context, projectID, roleID uuid.UUID) error
	// FindRoleByID returns a single role by its primary key, with no
	// project-ownership check — callers that need the role to belong to a
	// specific project must verify ProjectRole.ProjectID themselves, as
	// UpdateRole/DeleteRole do.
	FindRoleByID(ctx context.Context, id uuid.UUID) (*ProjectRole, error)
}
