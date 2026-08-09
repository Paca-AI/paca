package settingsdom

import "context"

// Repository defines persistence operations for the singleton workspace
// settings row. There is always exactly one row (seeded by migration), so
// unlike most repositories there is no Create/Delete/FindByID — just Get and
// WithLock against that one row.
type Repository interface {
	// Get returns the workspace settings row.
	Get(ctx context.Context) (*WorkspaceSettings, error)

	// WithLock locks the singleton row for the duration of a database
	// transaction, invokes fn with the current row, and persists whatever
	// fn returns. If fn returns a nil *WorkspaceSettings (with a nil error),
	// nothing is written and the row as it was before fn ran is returned —
	// used for no-op cases (e.g. removing an image slot that's already
	// empty).
	//
	// Callers with a read-modify-write update (every mutation on this
	// singleton row) must go through WithLock rather than Get+a hypothetical
	// separate Update: without the row lock, two overlapping read-modify-
	// write calls (e.g. an admin uploading a logo and a favicon at nearly
	// the same time) could each read the same stale snapshot, and whichever
	// writes last would silently discard the other's change.
	WithLock(ctx context.Context, fn func(*WorkspaceSettings) (*WorkspaceSettings, error)) (*WorkspaceSettings, error)
}
