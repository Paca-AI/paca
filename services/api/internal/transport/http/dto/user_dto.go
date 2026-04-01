// Package dto defines request and response shapes for user endpoints.
package dto

import (
	"time"

	"github.com/google/uuid"
	userdom "github.com/paca/api/internal/domain/user"
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
	}
}
