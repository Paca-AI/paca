package projectdom

import (
	"context"

	"github.com/google/uuid"
)

// CreateProjectInput carries fields required to create a new project.
type CreateProjectInput struct {
	Name        string
	Description string
	Settings    map[string]any
	CreatedBy   *uuid.UUID
}

// UpdateProjectInput carries mutable project fields.
type UpdateProjectInput struct {
	Name        string
	Description string
	Settings    map[string]any
}

// Service is the combined project management service contract.
// It composes the per-concern sub-interfaces.
type Service interface {
	ProjectService
	MemberService
	RoleService
}

// ProjectService defines project CRUD use cases.
type ProjectService interface {
	List(ctx context.Context, page, pageSize int) ([]*Project, int64, error)
	// ListAccessible returns only the projects the given user is a member of.
	ListAccessible(ctx context.Context, userID uuid.UUID, page, pageSize int) ([]*Project, int64, error)
	GetByID(ctx context.Context, id uuid.UUID) (*Project, error)
	Create(ctx context.Context, in CreateProjectInput) (*Project, error)
	Update(ctx context.Context, id uuid.UUID, in UpdateProjectInput) (*Project, error)
	Delete(ctx context.Context, id uuid.UUID) error
}
