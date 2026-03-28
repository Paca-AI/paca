// Package globalroledom defines the global role domain model and contracts.
package globalroledom

import (
	"time"

	"github.com/google/uuid"
)

// GlobalRole defines a role that can be assigned at the platform level.
type GlobalRole struct {
	ID          uuid.UUID
	Name        string
	Permissions map[string]any
	CreatedAt   time.Time
	UpdatedAt   time.Time
}
