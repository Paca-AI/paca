package exportdom

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
)

// ErrNotFound is returned when an export does not exist.
var ErrNotFound = errors.New("project export not found")

// Repository defines persistence operations for project exports.
type Repository interface {
	FindByID(ctx context.Context, id uuid.UUID) (*ProjectExport, error)
	// ListByProject returns the project's most recent exports, newest first.
	ListByProject(ctx context.Context, projectID uuid.UUID, limit int) ([]*ProjectExport, error)
	// CreateIfIdle inserts e unless the project already has a pending or
	// processing export updated at or after notBefore (older ones are treated
	// as abandoned), and reports whether it inserted. The check and the insert
	// are atomic per project, so concurrent requests cannot both succeed.
	CreateIfIdle(ctx context.Context, e *ProjectExport, notBefore time.Time) (bool, error)
	// Claim atomically moves a pending export to processing. It returns false
	// when the export is not pending (already claimed, finished or gone), so a
	// redelivered stream message never runs an export twice.
	Claim(ctx context.Context, id uuid.UUID) (bool, error)
	MarkCompleted(ctx context.Context, id uuid.UUID, fileKey, fileName string, fileSize int64, rowCount int, expiresAt time.Time) error
	// MarkFailed finishes the export as failed. expiresAt is when the failed
	// row is swept, like a completed export's, so failures don't pile up.
	MarkFailed(ctx context.Context, id uuid.UUID, message string, expiresAt time.Time) error
	// ListExpired returns up to limit exports whose expires_at is before now.
	ListExpired(ctx context.Context, now time.Time, limit int) ([]*ProjectExport, error)
	// ListStale returns up to limit exports still pending/processing that were
	// last updated before notAfter.
	ListStale(ctx context.Context, notAfter time.Time, limit int) ([]*ProjectExport, error)
	Delete(ctx context.Context, id uuid.UUID) error
}
