package dto

import (
	"time"

	"github.com/google/uuid"
	globalroledom "github.com/paca/api/internal/domain/globalrole"
)

// CreateGlobalRoleRequest is the body for POST /admin/global-roles.
type CreateGlobalRoleRequest struct {
	Name        string         `json:"name" binding:"required"`
	Permissions map[string]any `json:"permissions"`
}

// UpdateGlobalRoleRequest is the body for PATCH /admin/global-roles/:roleId.
type UpdateGlobalRoleRequest struct {
	Name        string         `json:"name" binding:"required"`
	Permissions map[string]any `json:"permissions"`
}

// ReplaceUserGlobalRolesRequest is the body for PUT /admin/users/:userId/global-roles.
type ReplaceUserGlobalRolesRequest struct {
	RoleIDs []uuid.UUID `json:"role_ids"`
}

// GlobalRoleResponse is the public representation of a global role.
type GlobalRoleResponse struct {
	ID          uuid.UUID      `json:"id"`
	Name        string         `json:"name"`
	Permissions map[string]any `json:"permissions"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
}

// GlobalRoleFromEntity maps a domain role to a response DTO.
func GlobalRoleFromEntity(role *globalroledom.GlobalRole) GlobalRoleResponse {
	permissions := role.Permissions
	if permissions == nil {
		permissions = map[string]any{}
	}
	return GlobalRoleResponse{
		ID:          role.ID,
		Name:        role.Name,
		Permissions: permissions,
		CreatedAt:   role.CreatedAt,
		UpdatedAt:   role.UpdatedAt,
	}
}
