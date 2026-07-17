// Package dto defines request and response shapes for user endpoints.
package dto

import (
	"time"

	userdom "github.com/Paca-AI/api/internal/domain/user"
	"github.com/google/uuid"
)

// CreateUserRequest is the body for POST /admin/users.
// Only users with the users.write permission can create accounts.
type CreateUserRequest struct {
	Username string `json:"username"  binding:"required"`
	Password string `json:"password"  binding:"required,min=8"`
	FullName string `json:"full_name" binding:"required"`
	// Role is optional; defaults to "USER" when omitted.
	// The provided role name is validated against the global_roles table.
	Role string `json:"role" binding:"omitempty"`
	// Email / OIDCSub (Galaxy ADR-038, admin-only): optionally pre-link the
	// account to the Vortex identity provider (user-directory sync). When
	// OIDCSub is set the account is SSO-first: must_change_password stays
	// false because the password is a random throwaway.
	Email   string `json:"email" binding:"omitempty"`
	OIDCSub string `json:"oidc_sub" binding:"omitempty"`
	// IsService (Galaxy ADR-038): mark the account as a non-human
	// service/bridge account (badged in UIs). Defaults to false.
	IsService bool `json:"is_service"`
}

// UpdateProfileRequest is the body for PATCH /users/me (self-service update).
type UpdateProfileRequest struct {
	FullName string `json:"full_name" binding:"required"`
}

// AdminUpdateUserRequest is the body for PATCH /admin/users/:userId.
type AdminUpdateUserRequest struct {
	FullName string `json:"full_name" binding:"omitempty"`
	// Role is optional; the provided name is validated against the global_roles table.
	Role string `json:"role" binding:"omitempty"`
	// Email / OIDCSub (Galaxy ADR-038, admin-only): set/correct the Vortex
	// identity link. Empty = leave unchanged (can never clear a link).
	Email   string `json:"email" binding:"omitempty"`
	OIDCSub string `json:"oidc_sub" binding:"omitempty"`
	// IsService (Galaxy ADR-038): tri-state — omitted/null leaves the flag
	// unchanged, true/false sets it.
	IsService *bool `json:"is_service"`
}

// ResetPasswordRequest is the body for PATCH /admin/users/:userId/password.
type ResetPasswordRequest struct {
	NewPassword string `json:"new_password" binding:"required,min=8"`
}

// ChangeMyPasswordRequest is the body for PATCH /users/me/password.
type ChangeMyPasswordRequest struct {
	CurrentPassword string `json:"current_password" binding:"required"`
	NewPassword     string `json:"new_password"     binding:"required,min=8"`
}

// UserResponse is the public representation of a user (no password hash).
type UserResponse struct {
	ID                 uuid.UUID `json:"id"`
	Username           string    `json:"username"`
	FullName           string    `json:"full_name"`
	Role               string    `json:"role"`
	MustChangePassword bool      `json:"must_change_password"`
	CreatedAt          time.Time `json:"created_at"`
	// Email / OIDCSub (Galaxy ADR-038): Vortex identity link, omitted when
	// the account is not linked. Lets the directory-sync reconciler match
	// existing accounts without guessing from usernames.
	Email   string `json:"email,omitempty"`
	OIDCSub string `json:"oidc_sub,omitempty"`
	// IsService (Galaxy ADR-038): non-human service/bridge account marker.
	IsService bool `json:"is_service"`
}

// PagedUsersResponse wraps a list of users with pagination metadata.
type PagedUsersResponse struct {
	Items    []UserResponse `json:"items"`
	Total    int64          `json:"total"`
	Page     int            `json:"page"`
	PageSize int            `json:"page_size"`
}

// UserFromEntity maps a domain user to a transport response.
func UserFromEntity(u *userdom.User) UserResponse {
	return UserResponse{
		ID:                 u.ID,
		Username:           u.Username,
		FullName:           u.FullName,
		Role:               u.Role,
		MustChangePassword: u.MustChangePassword,
		CreatedAt:          u.CreatedAt,
		Email:              u.Email,
		OIDCSub:            u.OIDCSub,
		IsService:          u.IsService,
	}
}
