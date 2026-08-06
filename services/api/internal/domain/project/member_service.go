package projectdom

import (
	"context"

	"github.com/google/uuid"
)

// AddMemberInput carries fields for adding a member to a project — a human
// (UserID) or, exclusively, a global agent being invited (AgentID). Exactly
// one of UserID/AgentID must be set.
type AddMemberInput struct {
	UserID        uuid.UUID
	AgentID       *uuid.UUID
	ProjectRoleID uuid.UUID
}

// UpdateMemberRoleInput carries fields for changing a member's role.
type UpdateMemberRoleInput struct {
	ProjectRoleID uuid.UUID
}

// MemberService defines member management use cases.
type MemberService interface {
	ListMembers(ctx context.Context, projectID uuid.UUID) ([]*ProjectMember, error)
	// CountDistinctAgentsByProjects returns the distinct agent member count
	// across projectIDs — see MemberRepository.CountDistinctAgentsByProjects.
	CountDistinctAgentsByProjects(ctx context.Context, projectIDs []uuid.UUID) (int64, error)
	AddMember(ctx context.Context, projectID uuid.UUID, in AddMemberInput) (*ProjectMember, error)
	UpdateMemberRole(ctx context.Context, projectID, userID uuid.UUID, in UpdateMemberRoleInput) (*ProjectMember, error)
	RemoveMember(ctx context.Context, projectID, userID uuid.UUID) error
	// UpdateMemberRoleByMemberID changes the role of a member by their membership record ID.
	UpdateMemberRoleByMemberID(ctx context.Context, projectID, memberID uuid.UUID, in UpdateMemberRoleInput) (*ProjectMember, error)
	// RemoveMemberByMemberID removes a member by their membership record ID.
	RemoveMemberByMemberID(ctx context.Context, projectID, memberID uuid.UUID) error
	// GetMyProjectPermissions returns the effective permission map of the
	// calling user's project role. Returns ErrMemberNotFound when the user
	// is not a member. Optionally accepts an agentID to look up agent
	// permissions instead of user permissions.
	GetMyProjectPermissions(ctx context.Context, projectID, userID uuid.UUID, agentID *uuid.UUID) (map[string]any, error)
	// AddAgentMember inserts an agent as a project member with the given role.
	AddAgentMember(ctx context.Context, memberID, projectID, agentID, roleID uuid.UUID) error
	// RemoveAgentMember soft-deletes the agent's membership record.
	RemoveAgentMember(ctx context.Context, projectID, agentID uuid.UUID) error
}
