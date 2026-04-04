package sprintdom

import (
	"context"

	"github.com/google/uuid"
)

// SprintRepository defines persistence operations for sprints.
type SprintRepository interface {
	ListSprints(ctx context.Context, projectID uuid.UUID) ([]*Sprint, error)
	FindSprintByID(ctx context.Context, id uuid.UUID) (*Sprint, error)
	CreateSprint(ctx context.Context, s *Sprint) error
	UpdateSprint(ctx context.Context, s *Sprint) error
	DeleteSprint(ctx context.Context, id uuid.UUID) error
}
