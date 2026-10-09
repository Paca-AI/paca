package roledom

import (
	"context"

	"github.com/google/uuid"
)

// ReplaceAttachmentsInput describes one replace-set of a principal's
// attachments in one scope.
type ReplaceAttachmentsInput struct {
	PrincipalType string // PrincipalUser | PrincipalAgent
	PrincipalID   uuid.UUID
	// ProjectID scopes the attachments: nil replaces the principal's
	// platform-wide set, otherwise the set within that project.
	ProjectID *uuid.UUID
	// RoleIDs is the complete desired set (already de-duplicated).
	RoleIDs   []uuid.UUID
	CreatedBy *uuid.UUID
}

// Repository is the persistence contract of the role service.
type Repository interface {
	// ListPlatform returns the platform roles (no owner project) with their
	// total attachment counts, sorted by name.
	ListPlatform(ctx context.Context) ([]*Role, error)
	// ListForProject returns the roles owned by the project followed by the
	// platform roles that are attached to someone inside it (so a converted
	// shared role stays visible where it is in use, and a new project lists
	// only its own roles), each with its attachment count inside the project.
	ListForProject(ctx context.Context, projectID uuid.UUID) ([]*Role, error)
	// FindByID returns a role (ErrNotFound when missing). With countProject
	// set, AttachmentCount counts only attachments inside that project;
	// otherwise all of them.
	FindByID(ctx context.Context, id uuid.UUID, countProject *uuid.UUID) (*Role, error)
	// FindByIDs returns the roles that exist among ids (unknown ids are
	// simply absent); AttachmentCount is not populated.
	FindByIDs(ctx context.Context, ids []uuid.UUID) ([]*Role, error)
	// Create inserts the role, filling ID and timestamps. ErrNameTaken when
	// the name exists in the scope, ErrProjectNotFound when the owner project
	// does not exist.
	Create(ctx context.Context, role *Role) error
	// Update changes name, description and policy. ErrNotFound, ErrNameTaken,
	// ErrSystemRole, and ErrLastWildcard (the new policy would leave no
	// platform-wide "*" holder).
	Update(ctx context.Context, role *Role) error
	// Delete removes the role and, by cascade, its attachments. ErrNotFound,
	// ErrSystemRole, ErrIsDefault, ErrLastWildcard.
	Delete(ctx context.Context, id uuid.UUID) error
	// SetDefault makes the platform role id the only default in one
	// transaction. ErrNotFound when it is missing or not a platform role.
	SetDefault(ctx context.Context, id uuid.UUID) error

	// UserExists reports whether the user exists and is not deleted.
	UserExists(ctx context.Context, id uuid.UUID) (bool, error)
	// GlobalAgentExists reports whether a non-deleted global-scope agent exists.
	GlobalAgentExists(ctx context.Context, id uuid.UUID) (bool, error)
	// ProjectExists reports whether the project exists and is not deleted.
	ProjectExists(ctx context.Context, id uuid.UUID) (bool, error)
	// FindMember returns the live member (ErrMemberNotFound otherwise).
	FindMember(ctx context.Context, projectID, memberID uuid.UUID) (*Member, error)

	// ListAttached returns the roles attached to the principal in one scope
	// (projectID nil = platform-wide), sorted by name.
	ListAttached(ctx context.Context, principalType string, principalID uuid.UUID, projectID *uuid.UUID) ([]*Role, error)
	// ReplaceAttachments makes the principal's attachments in the scope equal
	// to RoleIDs in one transaction (diff: stale rows deleted, missing ones
	// inserted with CreatedBy, unchanged ones untouched) and returns the ids
	// of the roles whose attachment set changed. ErrNotAttachable for an
	// unknown role or one that cannot be attached in the scope;
	// ErrLastWildcard when a platform-wide replace would leave no "*" holder.
	ReplaceAttachments(ctx context.Context, in ReplaceAttachmentsInput) ([]uuid.UUID, error)

	// RolePolicies returns the stored policy JSON of the roles that exist
	// among ids; it backs the assignment guard in the HTTP middleware.
	RolePolicies(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID][]byte, error)
}
