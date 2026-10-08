package projectdom

import (
	"context"

	"github.com/google/uuid"
)

// AddMemberInput carries fields for adding a member to a project — a human
// (UserID) or, exclusively, a global agent being invited (AgentID). Exactly
// one of UserID/AgentID must be set.
type AddMemberInput struct {
	UserID  uuid.UUID
	AgentID *uuid.UUID
	// RoleIDs are the roles the new member holds in the project (at least
	// one): platform roles and the project's own roles. They become
	// project-scoped attachments, written in the same transaction as the
	// membership row.
	RoleIDs []uuid.UUID
	// Description applies to human members only — see ProjectMember.Description.
	Description string
	// CreatedBy is recorded on the attachments (the caller's user id); nil when
	// unknown.
	CreatedBy *uuid.UUID
}

// MemberService defines member management use cases.
type MemberService interface {
	ListMembers(ctx context.Context, projectID uuid.UUID) ([]*ProjectMember, error)
	// CountDistinctAgentsByProjects returns the distinct agent member count
	// across projectIDs — see MemberRepository.CountDistinctAgentsByProjects.
	CountDistinctAgentsByProjects(ctx context.Context, projectIDs []uuid.UUID) (int64, error)
	AddMember(ctx context.Context, projectID uuid.UUID, in AddMemberInput) (*ProjectMember, error)
	RemoveMember(ctx context.Context, projectID, userID uuid.UUID) error
	// UpdateMemberDescription changes a member's Jev-facing description by
	// their membership record ID — see ProjectMember.Description.
	UpdateMemberDescription(ctx context.Context, projectID, memberID uuid.UUID, description string) (*ProjectMember, error)
	// RemoveMemberByMemberID removes a member by their membership record ID.
	RemoveMemberByMemberID(ctx context.Context, projectID, memberID uuid.UUID) error
	// AddAgentMember inserts an agent as a project member holding roleIDs
	// (project-scoped attachments, written with the membership row).
	AddAgentMember(ctx context.Context, memberID, projectID, agentID uuid.UUID, roleIDs []uuid.UUID, createdBy *uuid.UUID) error
	// RemoveAgentMember soft-deletes the agent's membership record.
	RemoveAgentMember(ctx context.Context, projectID, agentID uuid.UUID) error
}
