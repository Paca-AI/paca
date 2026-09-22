package globalroledom

import (
	"context"

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
type Service interface {
	List(ctx context.Context) ([]*GlobalRole, error)
	Create(ctx context.Context, in CreateInput) (*GlobalRole, error)
	Update(ctx context.Context, id uuid.UUID, in UpdateInput) (*GlobalRole, error)
	// Delete removes a role. It refuses the default role (ErrIsDefault) and a
	// role that users or global agents still hold (ErrHasAssignedUsers).
	Delete(ctx context.Context, id uuid.UUID) error
	// SetDefault makes id the role new users and global agents start with,
	// replacing the previous default, and returns it.
	SetDefault(ctx context.Context, id uuid.UUID) (*GlobalRole, error)
	// FindDefault returns the default role, or ErrNoDefault.
	FindDefault(ctx context.Context) (*GlobalRole, error)
	ReplaceUserRoles(ctx context.Context, userID uuid.UUID, roleIDs []uuid.UUID) ([]*GlobalRole, error)
	// FindByID returns a single global role by its primary key. Global roles
	// have no project-ownership dimension to check (unlike project roles) —
	// existence is the whole check, used to validate a caller-supplied
	// global_role_id before it's bound to an agent (see
	// agentsvc.Service.SetGlobalAgentRole).
	FindByID(ctx context.Context, id uuid.UUID) (*GlobalRole, error)
}
