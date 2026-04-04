package sprintdom

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// SprintService defines sprint use cases.
type SprintService interface {
	ListSprints(ctx context.Context, projectID uuid.UUID) ([]*Sprint, error)
	GetSprint(ctx context.Context, id uuid.UUID) (*Sprint, error)
	CreateSprint(ctx context.Context, in CreateSprintInput) (*Sprint, error)
	UpdateSprint(ctx context.Context, id uuid.UUID, in UpdateSprintInput) (*Sprint, error)
	DeleteSprint(ctx context.Context, id uuid.UUID) error
}

// CreateSprintInput carries fields required to create a sprint.
type CreateSprintInput struct {
	ProjectID uuid.UUID
	Name      string
	StartDate *time.Time
	EndDate   *time.Time
	Goal      *string
	Status    SprintStatus
}

// UpdateSprintInput carries mutable sprint fields.
type UpdateSprintInput struct {
	Name      string
	StartDate *time.Time
	EndDate   *time.Time
	Goal      *string
	Status    *SprintStatus
}
