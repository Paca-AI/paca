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
	// IsDefault marks the role a new user (and a new global agent) starts
	// with. At most one role is the default, and it cannot be deleted; change
	// it with Service.SetDefault.
	IsDefault bool
	CreatedAt time.Time
	UpdatedAt time.Time
}
