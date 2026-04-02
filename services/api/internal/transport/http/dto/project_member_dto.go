package dto

import (
	"github.com/google/uuid"
	projectdom "github.com/paca/api/internal/domain/project"
)

// --- Project Member DTOs ----------------------------------------------------

// AddProjectMemberRequest is the body for POST /v1/projects/:projectId/members.
type AddProjectMemberRequest struct {
	UserID        uuid.UUID `json:"user_id" binding:"required"`
	ProjectRoleID uuid.UUID `json:"project_role_id" binding:"required"`
}

// UpdateProjectMemberRoleRequest is the body for PATCH /v1/projects/:projectId/members/:userId.
type UpdateProjectMemberRoleRequest struct {
	ProjectRoleID uuid.UUID `json:"project_role_id" binding:"required"`
}

// ProjectMemberResponse is the public representation of a project membership.
type ProjectMemberResponse struct {
	ID            uuid.UUID `json:"id"`
	ProjectID     uuid.UUID `json:"project_id"`
	UserID        uuid.UUID `json:"user_id"`
	ProjectRoleID uuid.UUID `json:"project_role_id"`
	Username      string    `json:"username"`
	FullName      string    `json:"full_name"`
	RoleName      string    `json:"role_name"`
}

// ProjectMemberFromEntity maps a domain ProjectMember to a ProjectMemberResponse DTO.
func ProjectMemberFromEntity(m *projectdom.ProjectMember) ProjectMemberResponse {
	return ProjectMemberResponse{
		ID:            m.ID,
		ProjectID:     m.ProjectID,
		UserID:        m.UserID,
		ProjectRoleID: m.ProjectRoleID,
		Username:      m.Username,
		FullName:      m.FullName,
		RoleName:      m.RoleName,
	}
}
