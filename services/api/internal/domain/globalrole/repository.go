package globalroledom

import (
	"context"

	"github.com/google/uuid"
)

// Repository defines persistence operations for global role management.
type Repository interface {
	List(ctx context.Context) ([]*GlobalRole, error)
	FindByID(ctx context.Context, id uuid.UUID) (*GlobalRole, error)
	FindByName(ctx context.Context, name string) (*GlobalRole, error)
	Create(ctx context.Context, role *GlobalRole) error
	Update(ctx context.Context, role *GlobalRole) error
	Delete(ctx context.Context, id uuid.UUID) error
	ReplaceUserRoles(ctx context.Context, userID uuid.UUID, roleIDs []uuid.UUID) error
	ListUserRoles(ctx context.Context, userID uuid.UUID) ([]*GlobalRole, error)
	// CountUsersWithRole returns the number of non-deleted users whose primary
	// role FK points to id, plus the number of explicit role assignments in
	// user_global_roles.
	CountUsersWithRole(ctx context.Context, id uuid.UUID) (int64, error)
}
