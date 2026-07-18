package projectdom

import (
	"context"

	"github.com/Paca-AI/api/internal/platform/authz"
	"github.com/google/uuid"
)

// AddMemberInput carries fields for adding a user to a project.
type AddMemberInput struct {
	UserID        uuid.UUID
	ProjectRoleID uuid.UUID
}

// UpdateMemberRoleInput carries fields for changing a member's role.
type UpdateMemberRoleInput struct {
	ProjectRoleID uuid.UUID
}

// MemberService defines member management use cases.
//
// AddMember and UpdateMemberRoleByMemberID take the caller's own effective
// permission set for the project and enforce a grant ceiling: a member may not
// be assigned a role granting a permission the caller does not itself hold.
// This closes the shared-template escalation (PACA-4) where any member with
// project.members.write could assign the PROJECT_OWNER template. Callers
// holding "*" bypass the ceiling.
type MemberService interface {
	ListMembers(ctx context.Context, projectID uuid.UUID) ([]*ProjectMember, error)
	AddMember(ctx context.Context, projectID uuid.UUID, in AddMemberInput, caller authz.PermissionSet) (*ProjectMember, error)
	UpdateMemberRole(ctx context.Context, projectID, userID uuid.UUID, in UpdateMemberRoleInput) (*ProjectMember, error)
	RemoveMember(ctx context.Context, projectID, userID uuid.UUID) error
	// UpdateMemberRoleByMemberID changes the role of a member by their membership record ID.
	UpdateMemberRoleByMemberID(ctx context.Context, projectID, memberID uuid.UUID, in UpdateMemberRoleInput, caller authz.PermissionSet) (*ProjectMember, error)
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
