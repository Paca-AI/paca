package userdom

import (
	"context"

	"github.com/google/uuid"

	attachmentdom "github.com/Paca-AI/api/internal/domain/attachment"
)

// CreateInput carries the data needed to create a new user.
// Role is optional and defaults to RoleUser when empty.
// MustChangePassword defaults to false when omitted.
type CreateInput struct {
	Username           string
	Password           string
	FullName           string
	Role               string
	MustChangePassword bool
}

// UpdateProfileInput carries the self-service fields a user may change on
// their own account.
type UpdateProfileInput struct {
	FullName string
}

// AdminUpdateInput carries the fields an admin may change on any user account.
type AdminUpdateInput struct {
	FullName string
	Role     string
}

// Service defines the user use-case contract.
type Service interface {
	GetByID(ctx context.Context, id uuid.UUID) (*User, error)
	// List returns a page of users and the total count.
	List(ctx context.Context, page, pageSize int) ([]*User, int64, error)
	// CountUsers returns the total count of users without paginating rows.
	CountUsers(ctx context.Context) (int64, error)
	ListGlobalPermissions(ctx context.Context, id uuid.UUID) ([]string, error)
	Create(ctx context.Context, in CreateInput) (*User, error)
	// UpdateProfile lets a user update their own profile.
	UpdateProfile(ctx context.Context, id uuid.UUID, in UpdateProfileInput) (*User, error)
	// AdminUpdate lets an admin update any user's profile, including their role.
	AdminUpdate(ctx context.Context, id uuid.UUID, in AdminUpdateInput) (*User, error)
	// ResetPassword replaces a user's password hash with the hash of newPassword.
	// It also sets MustChangePassword = true so the user is forced to change their
	// password on next login.
	ResetPassword(ctx context.Context, id uuid.UUID, newPassword string) error
	// ChangeMyPassword lets a user change their own password. It verifies
	// currentPassword against the stored hash, then replaces it with
	// newPassword and clears MustChangePassword.
	ChangeMyPassword(ctx context.Context, id uuid.UUID, currentPassword, newPassword string) error
	Delete(ctx context.Context, id uuid.UUID) error

	// InitiateAvatarUpload starts an avatar upload for the user's own
	// profile picture and returns a presigned upload session.
	InitiateAvatarUpload(ctx context.Context, userID uuid.UUID, fileName, contentType string, fileSize int64) (*attachmentdom.UploadSession, error)
	// CompleteAvatarUpload finishes an avatar upload started via
	// InitiateAvatarUpload, replacing any previous avatar.
	CompleteAvatarUpload(ctx context.Context, userID, fileID uuid.UUID) (*User, error)
	// RemoveAvatar clears the user's avatar, deleting the underlying objects.
	RemoveAvatar(ctx context.Context, userID uuid.UUID) (*User, error)
}
