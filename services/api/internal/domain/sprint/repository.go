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

// ViewRepository defines persistence operations for sprint views and manual
// task ordering within those views.
type ViewRepository interface {
	// ListViews returns all views belonging to a sprint.
	ListViews(ctx context.Context, sprintID uuid.UUID) ([]*SprintView, error)

	// ListProjectViews returns all views for a project with the given context
	// (ViewContextBacklog or ViewContextTimeline).
	ListProjectViews(ctx context.Context, projectID uuid.UUID, viewCtx ViewContext) ([]*SprintView, error)

	FindViewByID(ctx context.Context, id uuid.UUID) (*SprintView, error)
	CreateView(ctx context.Context, v *SprintView) error
	UpdateView(ctx context.Context, v *SprintView) error
	DeleteView(ctx context.Context, id uuid.UUID) error

	// CountViews returns the number of views belonging to a sprint.  Used to
	// guard deletion of the last remaining view.
	CountViews(ctx context.Context, sprintID uuid.UUID) (int, error)

	// CountProjectViews returns the number of views for a project with the
	// given context.  Used to guard deletion of the last remaining view.
	CountProjectViews(ctx context.Context, projectID uuid.UUID, viewCtx ViewContext) (int, error)

	// UpsertTaskPosition stores or updates the manual position of a task
	// within a view.
	UpsertTaskPosition(ctx context.Context, pos *ViewTaskPosition) error

	// BulkUpsertTaskPositions stores or updates multiple task positions within a
	// view in a single transaction.
	BulkUpsertTaskPositions(ctx context.Context, positions []*ViewTaskPosition) error

	// ListTaskPositions returns all manual positions for a view, ordered by
	// position ASC.
	ListTaskPositions(ctx context.Context, viewID uuid.UUID) ([]*ViewTaskPosition, error)

	// ReorderViews bulk-updates the position of multiple views in a single
	// transaction.  items must contain one entry per view being repositioned.
	ReorderViews(ctx context.Context, items []ViewReorderItem) error

	// GetUserViewConfigs returns a user's personal config overrides for the
	// given views, keyed by view ID.  Views without an override are absent
	// from the returned map.
	GetUserViewConfigs(ctx context.Context, userID uuid.UUID, viewIDs []uuid.UUID) (map[uuid.UUID]ViewConfig, error)

	// UpsertUserViewConfig stores (or replaces) a user's personal config for a
	// single view.
	UpsertUserViewConfig(ctx context.Context, viewID, userID uuid.UUID, cfg ViewConfig) error
}

// ViewReorderItem carries the new position for a single view.
type ViewReorderItem struct {
	ID       uuid.UUID
	Position float64
}
