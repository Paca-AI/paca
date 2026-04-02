package projectdom

import (
	"context"

	"github.com/google/uuid"
)

// Repository is the combined persistence contract for the project aggregate.
// It composes the per-entity sub-interfaces so a single concrete implementation
// can satisfy all of them.
type Repository interface {
	ProjectRepository
	MemberRepository
	RoleRepository
}

// ProjectRepository defines persistence operations for projects.
type ProjectRepository interface {
	List(ctx context.Context, offset, limit int) ([]*Project, int64, error)
	FindByID(ctx context.Context, id uuid.UUID) (*Project, error)
	Create(ctx context.Context, p *Project) error
	Update(ctx context.Context, p *Project) error
	Delete(ctx context.Context, id uuid.UUID) error
}
