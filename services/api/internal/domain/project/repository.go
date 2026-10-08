package projectdom

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
)

// Repository is the combined persistence contract for the project aggregate.
// It composes the per-entity sub-interfaces so a single concrete implementation
// can satisfy all of them.
type Repository interface {
	ProjectRepository
	MemberRepository
}

// SetupRole is a role owned by a project, written when the project is
// created. Policy is a plain IAM policy with the project's real id in its
// resources.
type SetupRole struct {
	Name        string
	Description string
	Policy      json.RawMessage
	System      bool
}

// ProjectSetup is what Create writes next to the project row.
type ProjectSetup struct {
	// Roles are inserted as roles owned by the project.
	Roles []SetupRole
	// Creator, when set, becomes a member of the project holding the role
	// named CreatorRole (one of Roles), scoped to the project.
	Creator     *uuid.UUID
	CreatorRole string
}

// ProjectRepository defines persistence operations for projects.
type ProjectRepository interface {
	List(ctx context.Context, offset, limit int) ([]*Project, int64, error)
	// ListAccessible returns only projects the given user is a member of.
	ListAccessible(ctx context.Context, userID uuid.UUID, offset, limit int) ([]*Project, int64, error)
	FindByID(ctx context.Context, id uuid.UUID) (*Project, error)
	// Create inserts the project and, in the same transaction, the roles and
	// the creator's membership described by setup, so a project never exists
	// without them.
	Create(ctx context.Context, p *Project, setup ProjectSetup) error
	Update(ctx context.Context, p *Project) error
	Delete(ctx context.Context, id uuid.UUID) error
	// UpdateJevConfig sets a project's Jev credentials (see Project.
	// JevAPIKeySecret's doc comment) — a dedicated method, not folded into
	// Update, since it needs its own encrypt-before-write handling
	// (service/project's encryptJevKey) that plain Update fields don't.
	UpdateJevConfig(ctx context.Context, projectID uuid.UUID, apiKeySecret, baseURL, model string) error
}
