package dto

import (
	"time"

	"github.com/google/uuid"
	projectdom "github.com/paca/api/internal/domain/project"
)

// --- Project Role DTOs ------------------------------------------------------

// CreateProjectRoleRequest is the body for POST /v1/projects/:projectId/roles.
type CreateProjectRoleRequest struct {
	RoleName    string         `json:"role_name" binding:"required"`
	Permissions map[string]any `json:"permissions"`
}

// UpdateProjectRoleRequest is the body for PATCH /v1/projects/:projectId/roles/:roleId.
type UpdateProjectRoleRequest struct {
	RoleName    string         `json:"role_name" binding:"required"`
	Permissions map[string]any `json:"permissions"`
}

// ProjectRoleResponse is the public representation of a project role.
type ProjectRoleResponse struct {
	ID          uuid.UUID      `json:"id"`
	ProjectID   *uuid.UUID     `json:"project_id,omitempty"`
	RoleName    string         `json:"role_name"`
	Permissions map[string]any `json:"permissions"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
}

// ProjectRoleFromEntity maps a domain ProjectRole to a ProjectRoleResponse DTO.
func ProjectRoleFromEntity(r *projectdom.ProjectRole) ProjectRoleResponse {
	perms := r.Permissions
	if perms == nil {
		perms = map[string]any{}
	}
	return ProjectRoleResponse{
		ID:          r.ID,
		ProjectID:   r.ProjectID,
		RoleName:    r.RoleName,
		Permissions: perms,
		CreatedAt:   r.CreatedAt,
		UpdatedAt:   r.UpdatedAt,
	}
}
