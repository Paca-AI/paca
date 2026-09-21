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
	Delete(ctx context.Context, id uuid.UUID) error
	ReplaceUserRoles(ctx context.Context, userID uuid.UUID, roleIDs []uuid.UUID) ([]*GlobalRole, error)
	// FindByID returns a single global role by its primary key. Global roles
	// have no project-ownership dimension to check (unlike project roles) —
	// existence is the whole check, used to validate a caller-supplied
	// global_role_id before it's bound to an agent (see
	// agentsvc.Service.SetGlobalAgentRole).
	FindByID(ctx context.Context, id uuid.UUID) (*GlobalRole, error)
}
