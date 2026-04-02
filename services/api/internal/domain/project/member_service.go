package projectdom

import (
	"context"

	"github.com/google/uuid"
)

// AddMemberInput carries fields for adding a user to a project.
type AddMemberInput struct {
	UserID        uuid.UUID
	ProjectRoleID uuid.UUID
}

// MemberService defines member management use cases.
type MemberService interface {
	ListMembers(ctx context.Context, projectID uuid.UUID) ([]*ProjectMember, error)
	AddMember(ctx context.Context, projectID uuid.UUID, in AddMemberInput) (*ProjectMember, error)
	RemoveMember(ctx context.Context, projectID, userID uuid.UUID) error
}
