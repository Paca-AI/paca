package projectdom

import (
	"context"

	"github.com/google/uuid"
)

// MemberRepository defines persistence operations for project members.
type MemberRepository interface {
	ListMembers(ctx context.Context, projectID uuid.UUID) ([]*ProjectMember, error)
	// CountDistinctAgentsByProjects returns the number of distinct agents
	// with an active membership across all of projectIDs, in a single query
	// — used by the home page's workspace stats widget so an invited global
	// agent belonging to several projects is counted once, not once per
	// project. Human team-member counts come from userdom.Service.CountUsers
	// instead: they aren't project-scoped, so they don't need this table.
	CountDistinctAgentsByProjects(ctx context.Context, projectIDs []uuid.UUID) (int64, error)
	FindMember(ctx context.Context, projectID, userID uuid.UUID) (*ProjectMember, error)
	// FindMemberByAgent returns the active member record for an agent in a
	// project. Used to resolve an agent UUID to a member UUID.
	FindMemberByAgent(ctx context.Context, projectID, agentID uuid.UUID) (*ProjectMember, error)
	// FindMemberByUserProject returns the active member record for a user in a
	// project.  Used by background workers to resolve a user UUID to a member UUID.
	FindMemberByUserProject(ctx context.Context, userID, projectID uuid.UUID) (*ProjectMember, error)
	// FindMemberByActor resolves an actor to a project member.
	// When agentID is non-nil the agent's member record is returned;
	// otherwise the user's member record (via userID + projectID) is returned.
	// This is the single canonical lookup used by activity services and workers.
	FindMemberByActor(ctx context.Context, projectID, actorID uuid.UUID, agentID *uuid.UUID) (*ProjectMember, error)
	// FindMemberByID returns the active member record for the given
	// project_members.id.  Used to resolve an assignee member ID to a user ID.
	FindMemberByID(ctx context.Context, memberID uuid.UUID) (*ProjectMember, error)
	AddMember(ctx context.Context, m *ProjectMember) error
	UpdateMemberRole(ctx context.Context, projectID, userID, roleID uuid.UUID) error
	RemoveMember(ctx context.Context, projectID, userID uuid.UUID) error
	// UpdateMemberRoleByMemberID changes the role of a member by their membership record ID.
	UpdateMemberRoleByMemberID(ctx context.Context, memberID, roleID uuid.UUID) error
	// RemoveMemberByMemberID removes a member by their membership record ID.
	RemoveMemberByMemberID(ctx context.Context, memberID uuid.UUID) error
	// AddAgentMember inserts an agent as a project member with the given role.
	AddAgentMember(ctx context.Context, memberID, projectID, agentID, roleID uuid.UUID) error
	// RemoveAgentMember soft-deletes the agent's membership record.
	RemoveAgentMember(ctx context.Context, projectID, agentID uuid.UUID) error
}
