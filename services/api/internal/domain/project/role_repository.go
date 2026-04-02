package projectdom

import (
	"context"

	"github.com/google/uuid"
)

// RoleRepository defines persistence operations for project roles.
type RoleRepository interface {
	ListRoles(ctx context.Context, projectID uuid.UUID) ([]*ProjectRole, error)
	FindRoleByID(ctx context.Context, id uuid.UUID) (*ProjectRole, error)
	FindRoleByName(ctx context.Context, projectID uuid.UUID, name string) (*ProjectRole, error)
	CreateRole(ctx context.Context, r *ProjectRole) error
	UpdateRole(ctx context.Context, r *ProjectRole) error
	DeleteRole(ctx context.Context, id uuid.UUID) error
	CountMembersWithRole(ctx context.Context, roleID uuid.UUID) (int64, error)
}
