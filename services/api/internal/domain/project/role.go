package projectdom

import (
	"time"

	"github.com/google/uuid"
)

// ProjectRole defines a role scoped to a specific project (project_id IS NOT NULL)
// or a global template role (project_id IS NULL).
type ProjectRole struct {
	ID          uuid.UUID
	ProjectID   *uuid.UUID
	RoleName    string
	Permissions map[string]any
	CreatedAt   time.Time
	UpdatedAt   time.Time
}
