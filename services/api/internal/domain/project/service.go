package projectdom

import (
	"context"

	"github.com/google/uuid"

	attachmentdom "github.com/Paca-AI/api/internal/domain/attachment"
)

// CreateProjectInput carries fields required to create a new project.
type CreateProjectInput struct {
	Name         string
	Description  string
	TaskIDPrefix string
	IsPublic     bool
	Settings     map[string]any
	CreatedBy    *uuid.UUID
}

// UpdateProjectInput carries mutable project fields.
type UpdateProjectInput struct {
	Name         string
	Description  string
	TaskIDPrefix string
	IsPublic     *bool
	Settings     map[string]any
}

// Service is the combined project management service contract.
// It composes the per-concern sub-interfaces.
type Service interface {
	ProjectService
	MemberService
	RoleService
}

// ProjectService defines project CRUD use cases.
type ProjectService interface {
	List(ctx context.Context, page, pageSize int) ([]*Project, int64, error)
	// ListAccessible returns only the projects the given user is a member of.
	ListAccessible(ctx context.Context, userID uuid.UUID, page, pageSize int) ([]*Project, int64, error)
	GetByID(ctx context.Context, id uuid.UUID) (*Project, error)
	// IsProjectPublic returns true when the project exists and is_public is set.
	IsProjectPublic(ctx context.Context, id uuid.UUID) (bool, error)
	Create(ctx context.Context, in CreateProjectInput) (*Project, error)
	Update(ctx context.Context, id uuid.UUID, in UpdateProjectInput) (*Project, error)
	Delete(ctx context.Context, id uuid.UUID) error

	// InitiateAvatarUpload starts an avatar upload for the project and
	// returns a presigned upload session.
	InitiateAvatarUpload(ctx context.Context, projectID uuid.UUID, fileName, contentType string, fileSize int64, uploadedBy uuid.UUID) (*attachmentdom.UploadSession, error)
	// CompleteAvatarUpload finishes an avatar upload, replacing any previous avatar.
	CompleteAvatarUpload(ctx context.Context, projectID, fileID uuid.UUID) (*Project, error)
	// RemoveAvatar clears the project's avatar, deleting the underlying objects.
	RemoveAvatar(ctx context.Context, projectID uuid.UUID) (*Project, error)
}
