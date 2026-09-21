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
	// FindDefault returns the role marked as the default, or ErrNoDefault when
	// none is.
	FindDefault(ctx context.Context) (*GlobalRole, error)
	// SetDefault atomically makes id the only default role, clearing the flag
	// on every other role. It returns ErrNotFound when id names no role.
	SetDefault(ctx context.Context, id uuid.UUID) error
	Create(ctx context.Context, role *GlobalRole) error
	Update(ctx context.Context, role *GlobalRole) error
	Delete(ctx context.Context, id uuid.UUID) error
	ReplaceUserRoles(ctx context.Context, userID uuid.UUID, roleIDs []uuid.UUID) error
	ListUserRoles(ctx context.Context, userID uuid.UUID) ([]*GlobalRole, error)
	// CountUsersWithRole returns the number of non-deleted users whose primary
	// role foreign key points to the given role ID.
	CountUsersWithRole(ctx context.Context, id uuid.UUID) (int64, error)
}
