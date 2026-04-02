package projectdom

import (
	"context"

	"github.com/google/uuid"
)

// MemberRepository defines persistence operations for project members.
type MemberRepository interface {
	ListMembers(ctx context.Context, projectID uuid.UUID) ([]*ProjectMember, error)
	FindMember(ctx context.Context, projectID, userID uuid.UUID) (*ProjectMember, error)
	AddMember(ctx context.Context, m *ProjectMember) error
	UpdateMemberRole(ctx context.Context, projectID, userID, roleID uuid.UUID) error
	RemoveMember(ctx context.Context, projectID, userID uuid.UUID) error
}
