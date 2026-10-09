package roledom

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
)

// RoleInput carries the editable fields of a role. Policy is the policy
// document as JSON.
type RoleInput struct {
	Name        string
	Description string
	Policy      json.RawMessage
}

// SimulateInput is a what-if authorization request.
type SimulateInput struct {
	Policy json.RawMessage
	// Principal, when set, adds that principal's own grants to the simulation.
	Principal  *PrincipalRef
	Action     string
	Resource   string
	Attributes map[string][]string
}

// Service defines the role and attachment use cases. It performs validation
// and data-integrity checks only: who may call each use case is decided by
// the route gates and guards in the HTTP middleware.
//
// projectID selects the scope: nil addresses platform roles, otherwise the
// roles owned by that project.
type Service interface {
	ListPlatform(ctx context.Context) ([]*Role, error)
	// ListForProject returns the project's own roles and the platform roles
	// attached inside it.
	ListForProject(ctx context.Context, projectID uuid.UUID) ([]*Role, error)
	// Get returns a role of the scope; with a project scope, platform roles
	// are readable too.
	Get(ctx context.Context, projectID *uuid.UUID, id uuid.UUID) (*Role, error)
	Create(ctx context.Context, projectID *uuid.UUID, in RoleInput) (*Role, error)
	Update(ctx context.Context, projectID *uuid.UUID, id uuid.UUID, in RoleInput) (*Role, error)
	Delete(ctx context.Context, projectID *uuid.UUID, id uuid.UUID) error
	// SetDefault makes a platform role the default and returns it.
	SetDefault(ctx context.Context, id uuid.UUID) (*Role, error)

	ListUserRoles(ctx context.Context, userID uuid.UUID) ([]*Role, error)
	ReplaceUserRoles(ctx context.Context, userID uuid.UUID, roleIDs []uuid.UUID, createdBy *uuid.UUID) ([]*Role, error)
	ListAgentRoles(ctx context.Context, agentID uuid.UUID) ([]*Role, error)
	ReplaceAgentRoles(ctx context.Context, agentID uuid.UUID, roleIDs []uuid.UUID, createdBy *uuid.UUID) ([]*Role, error)
	ListMemberRoles(ctx context.Context, projectID, memberID uuid.UUID) ([]*Role, error)
	ReplaceMemberRoles(ctx context.Context, projectID, memberID uuid.UUID, roleIDs []uuid.UUID, createdBy *uuid.UUID) ([]*Role, error)

	// Actions lists every registered action, sorted.
	Actions() []string
	// AttributeDefs lists the declared condition attributes, sorted by key.
	AttributeDefs() []AttributeDef
	// ValidatePolicy returns the problems of a policy document (empty = valid).
	// With a project, it also checks the resources of a role owned by that
	// project (they must lie inside it).
	ValidatePolicy(policy json.RawMessage, projectID *uuid.UUID) []Issue
	// Simulate evaluates the given policy (and optionally a principal's
	// grants) for one request.
	Simulate(ctx context.Context, in SimulateInput) (*SimulationResult, error)
}
