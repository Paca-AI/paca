package sprintdom

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// SprintService defines sprint use cases.
type SprintService interface {
	ListSprints(ctx context.Context, projectID uuid.UUID) ([]*Sprint, error)
	// GetSprint returns the sprint identified by id, verifying it belongs to projectID.
	GetSprint(ctx context.Context, projectID, id uuid.UUID) (*Sprint, error)
	CreateSprint(ctx context.Context, in CreateSprintInput) (*Sprint, error)
	// UpdateSprint updates the sprint identified by id, verifying it belongs to projectID.
	UpdateSprint(ctx context.Context, projectID, id uuid.UUID, in UpdateSprintInput) (*Sprint, error)
	// DeleteSprint removes the sprint identified by id, verifying it belongs to projectID.
	DeleteSprint(ctx context.Context, projectID, id uuid.UUID) error
	// CompleteSprint marks a sprint as completed and bulk-moves all non-done
	// tasks to the sprint specified in CompleteSprintInput (nil = backlog).
	// Verifies the sprint belongs to projectID before proceeding.
	CompleteSprint(ctx context.Context, projectID, id uuid.UUID, in CompleteSprintInput) (*Sprint, error)
}

// CompleteSprintInput carries options for completing a sprint.
// MoveToSprintID, when nil, moves incomplete tasks to the backlog.
type CompleteSprintInput struct {
	MoveToSprintID *uuid.UUID
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

// UpdateSprintInput carries mutable sprint fields for a PATCH operation.
// Name is applied when non-empty. For StartDate, EndDate, and Goal, the
// double pointer encodes three states:
//   - nil outer pointer  → field was absent in the request; do NOT overwrite
//   - non-nil outer pointer, inner pointer nil  → explicitly set to null (clear)
//   - non-nil outer pointer, inner pointer non-nil  → set to the given value
type UpdateSprintInput struct {
	Name      string
	StartDate **time.Time
	EndDate   **time.Time
	Goal      **string
	Status    *SprintStatus
}

// ViewService defines use cases for sprint views and manual task ordering.
type ViewService interface {
	// ListViews returns all views belonging to a sprint.
	ListViews(ctx context.Context, sprintID uuid.UUID) ([]*SprintView, error)

	// ListProjectViews returns all views for a project filtered by viewCtx
	// (ViewContextBacklog or ViewContextTimeline).
	ListProjectViews(ctx context.Context, projectID uuid.UUID, viewCtx ViewContext) ([]*SprintView, error)

	// GetView returns the view identified by id, verifying it belongs to projectID.
	GetView(ctx context.Context, projectID, id uuid.UUID) (*SprintView, error)
	CreateView(ctx context.Context, in CreateViewInput) (*SprintView, error)
	// UpdateView updates the view identified by id, verifying it belongs to projectID.
	UpdateView(ctx context.Context, projectID, id uuid.UUID, in UpdateViewInput) (*SprintView, error)
	// DeleteView removes the view identified by id, verifying it belongs to projectID.
	DeleteView(ctx context.Context, projectID, id uuid.UUID) error

	// MoveTask updates the manual position of a task within a view,
	// verifying the view belongs to projectID.
	MoveTask(ctx context.Context, projectID, viewID uuid.UUID, in MoveTaskInput) error

	// BulkMoveTasks updates the manual positions of multiple tasks in a view
	// within a single transaction, verifying the view belongs to projectID.
	BulkMoveTasks(ctx context.Context, projectID, viewID uuid.UUID, items []MoveTaskInput) error

	// ListTaskPositions returns the manual ordering for all tasks in a view,
	// verifying the view belongs to projectID.
	ListTaskPositions(ctx context.Context, projectID, viewID uuid.UUID) ([]*ViewTaskPosition, error)

	// ReorderViews reorders all views belonging to a sprint.  viewIDs must
	// contain every view ID for that sprint in the desired order.
	ReorderViews(ctx context.Context, sprintID uuid.UUID, viewIDs []uuid.UUID) error

	// ReorderProjectViews reorders all views for a project with the given
	// context.  viewIDs must contain every view ID for that project+context
	// in the desired order.
	ReorderProjectViews(ctx context.Context, projectID uuid.UUID, viewCtx ViewContext, viewIDs []uuid.UUID) error

	// SetUserViewConfig stores the current user's personal config (settings and
	// filters) for a view, verifying it belongs to projectID, and returns the
	// view carrying that personal config.  The change is private to the user:
	// it never touches the shared view row and emits no project-wide event.
	SetUserViewConfig(ctx context.Context, projectID, viewID, userID uuid.UUID, cfg ViewConfig) (*SprintView, error)

	// OverlayUserConfigs replaces each view's Config with the user's personal
	// override where one exists, leaving the shared default otherwise.  It is
	// safe to call with views obtained from a cache: only the returned copies
	// are mutated.  A nil user (unauthenticated) is a no-op.
	OverlayUserConfigs(ctx context.Context, userID uuid.UUID, views []*SprintView) error
}

// CreateViewInput carries fields required to create a sprint view.
// SprintID is nil for product-backlog and timeline views; ProjectID is always required.
// ViewContext identifies the interaction this view belongs to.
type CreateViewInput struct {
	SprintID    *uuid.UUID
	ProjectID   uuid.UUID
	Name        string
	ViewType    ViewType
	Config      ViewConfig
	Position    float64
	ViewContext ViewContext
}

// UpdateViewInput carries mutable view fields.
type UpdateViewInput struct {
	Name     *string
	ViewType *ViewType
	Config   *ViewConfig
	Position *float64
}

// MoveTaskInput requests a change to a task's manual position in a view.
type MoveTaskInput struct {
	TaskID   uuid.UUID
	Position float64
	GroupKey *string
}
