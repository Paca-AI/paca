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
}
