package userdom

import (
	"context"

	"github.com/google/uuid"
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
	// Email / OIDCSub (Galaxy ADR-038): optionally pre-link the account to the
	// Vortex identity provider at creation time (admin/directory-sync path).
	// Empty means "not linked". The OIDC login path (galaxyauth) then finds
	// this same row by subject — no duplicate is ever JIT-created.
	Email   string
	OIDCSub string
	// IsService (Galaxy ADR-038): non-human service/bridge account marker.
	IsService bool
}

// UpdateProfileInput carries the self-service fields a user may change on
// their own account.
type UpdateProfileInput struct {
	FullName string
}

// AdminUpdateInput carries the fields an admin may change on any user account.
// All fields are optional; empty string means "leave unchanged".
type AdminUpdateInput struct {
	FullName string
	Role     string
	// Email / OIDCSub (Galaxy ADR-038): set or correct the Vortex identity
	// link. Empty leaves the stored value untouched — this input can never
	// CLEAR a link, only set/replace it.
	Email   string
	OIDCSub string
	// IsService (Galaxy ADR-038): tri-state — nil leaves the flag unchanged.
	IsService *bool
}

// Service defines the user use-case contract.
type Service interface {
	GetByID(ctx context.Context, id uuid.UUID) (*User, error)
	// List returns a page of users and the total count.
	List(ctx context.Context, page, pageSize int) ([]*User, int64, error)
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
}
