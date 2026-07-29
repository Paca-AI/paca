package dto

import (
	"github.com/google/uuid"

	projectdom "github.com/Paca-AI/api/internal/domain/project"
)

// --- Project Member DTOs ----------------------------------------------------

// AddProjectMemberRequest is the body for POST /v1/projects/:projectId/members.
type AddProjectMemberRequest struct {
	UserID        uuid.UUID `json:"user_id" binding:"required"`
	ProjectRoleID uuid.UUID `json:"project_role_id" binding:"required"`
}

// UpdateProjectMemberRoleRequest is the body for PATCH /v1/projects/:projectId/members/:memberId.
type UpdateProjectMemberRoleRequest struct {
	ProjectRoleID uuid.UUID `json:"project_role_id" binding:"required"`
}

// ProjectMemberResponse is the public representation of a project membership.
type ProjectMemberResponse struct {
	ID            uuid.UUID  `json:"id"`
	ProjectID     uuid.UUID  `json:"project_id"`
	UserID        uuid.UUID  `json:"user_id"`
	ProjectRoleID uuid.UUID  `json:"project_role_id"`
	Username      string     `json:"username"`
	FullName      string     `json:"full_name"`
	RoleName      string     `json:"role_name"`
	MemberType    string     `json:"member_type"`
	AgentID       *uuid.UUID `json:"agent_id,omitempty"`
	AgentName     string     `json:"agent_name,omitempty"`
	AgentHandle   string     `json:"agent_handle,omitempty"`
}

// ProjectMemberFromEntity maps a domain ProjectMember to a ProjectMemberResponse DTO.
func ProjectMemberFromEntity(m *projectdom.ProjectMember) ProjectMemberResponse {
	resp := ProjectMemberResponse{
		ID:            m.ID,
		ProjectID:     m.ProjectID,
		UserID:        m.UserID,
		ProjectRoleID: m.ProjectRoleID,
		RoleName:      m.RoleName,
		MemberType:    m.MemberType,
		AgentID:       m.AgentID,
		AgentName:     m.AgentName,
		AgentHandle:   m.AgentHandle,
	}
	if m.IsAgent() {
		// For agent members, populate username/full_name from agent fields so
		// existing display logic (full_name || username) works without change.
		resp.FullName = m.AgentName
		resp.Username = m.AgentHandle
	} else {
		resp.Username = m.Username
		resp.FullName = m.FullName
	}
	return resp
}
