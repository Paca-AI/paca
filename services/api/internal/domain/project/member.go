package projectdom

import (
	"time"

	"github.com/google/uuid"
)

// ProjectMember represents a user's membership in a project with an assigned role.
type ProjectMember struct {
	ID            uuid.UUID
	ProjectID     uuid.UUID
	UserID        uuid.UUID
	ProjectRoleID uuid.UUID
	// Populated by JOIN for display purposes.
	Username  string
	FullName  string
	RoleName  string
	CreatedAt time.Time
}
