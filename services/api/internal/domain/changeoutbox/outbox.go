// Package changeoutbox describes the transactional outbox of database changes:
// a trigger records every insert, update and delete of tasks and sprints in the same transaction as the change, so a change cannot be
// committed without a trace even if the process dies before it is announced on
// the realtime feed. The change reconciler reads the trace and announces what
// was not.
package changeoutbox

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// Entity types a trigger records.
const (
	EntityTask   = "task"
	EntitySprint = "sprint"
)

// Row is one recorded change.
type Row struct {
	ID         int64
	ProjectID  uuid.UUID
	EntityType string
	EntityID   uuid.UUID
	// Op is "insert", "update" or "delete" (a soft delete is an update; the
	// reconciler decides what happened by loading the entity).
	Op string
	// Version is the row version after the change.
	Version   *int64
	CreatedAt time.Time
}

// Repository claims recorded changes.
type Repository interface {
	// Claim locks up to limit rows older than olderThan, hands them to handle
	// and deletes them when handle returns nil; on an error nothing is deleted.
	// It returns how many rows it handled.
	Claim(ctx context.Context, olderThan time.Time, limit int, handle func([]Row) error) (int, error)
	// Prune deletes rows older than olderThan without handling them and
	// returns how many it removed. It bounds the table when the realtime
	// feed cannot be reached for a long time.
	Prune(ctx context.Context, olderThan time.Time) (int64, error)
}
