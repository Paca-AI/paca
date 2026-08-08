package settingsdom

import "context"

// Repository defines persistence operations for the singleton workspace
// settings row. There is always exactly one row (seeded by migration), so
// unlike most repositories there is no Create/Delete/FindByID — just Get and
// Update against that one row.
type Repository interface {
	// Get returns the workspace settings row.
	Get(ctx context.Context) (*WorkspaceSettings, error)
	// Update persists s, overwriting the singleton row.
	Update(ctx context.Context, s *WorkspaceSettings) error
}
