// Package dto defines request and response shapes for user endpoints.
package dto

import (
	"time"

	"github.com/google/uuid"
	userdom "github.com/paca/api/internal/domain/user"
)

// CreateUserRequest is the body for POST /users.
type CreateUserRequest struct {
	Username string `json:"username"  binding:"required"`
	Password string `json:"password"  binding:"required,min=8"`
	FullName string `json:"full_name" binding:"required"`
}

// UpdateUserRequest is the body for PATCH /users/:id.
type UpdateUserRequest struct {
	FullName string `json:"full_name" binding:"required"`
}

// UserResponse is the public representation of a user (no password hash).
type UserResponse struct {
	ID        uuid.UUID `json:"id"`
	Username  string    `json:"username"`
	FullName  string    `json:"full_name"`
	Role      string    `json:"role"`
	CreatedAt time.Time `json:"created_at"`
}

// UserFromEntity maps a domain user to a transport response.
func UserFromEntity(u *userdom.User) UserResponse {
	return UserResponse{
		ID:        u.ID,
		Username:  u.Username,
		FullName:  u.FullName,
		Role:      u.Role,
		CreatedAt: u.CreatedAt,
	}
}
