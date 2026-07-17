package dto

import (
	"time"

	projectdom "github.com/Paca-AI/api/internal/domain/project"
	"github.com/google/uuid"
)

// --- Project DTOs -----------------------------------------------------------

// CreateProjectRequest is the body for POST /projects.
type CreateProjectRequest struct {
	Name         string         `json:"name" binding:"required"`
	Description  string         `json:"description"`
	TaskIDPrefix string         `json:"task_id_prefix"`
	IsPublic     bool           `json:"is_public"`
	Settings     map[string]any `json:"settings"`
}

// UpdateProjectRequest is the body for PATCH /projects/:projectId.
type UpdateProjectRequest struct {
	Name         string         `json:"name"`
	Description  string         `json:"description"`
	TaskIDPrefix string         `json:"task_id_prefix"`
	IsPublic     *bool          `json:"is_public"`
	Settings     map[string]any `json:"settings"`
}

// ProjectResponse is the public representation of a project.
type ProjectResponse struct {
	ID           uuid.UUID      `json:"id"`
	Name         string         `json:"name"`
	Description  string         `json:"description"`
	TaskIDPrefix string         `json:"task_id_prefix"`
	IsPublic     bool           `json:"is_public"`
	Settings     map[string]any `json:"settings"`
	CreatedBy    *uuid.UUID     `json:"created_by,omitempty"`
	CreatedAt    time.Time      `json:"created_at"`
}

// WorkspaceStatsResponse is the public representation of workspace-level
// statistics shown on the authenticated home page.
type WorkspaceStatsResponse struct {
	OpenTaskCount   int64 `json:"open_task_count"`
	TeamMemberCount int64 `json:"team_member_count"`
	AIAgentCount    int64 `json:"ai_agent_count"`
}

// ProjectFromEntity maps a domain Project to a ProjectResponse DTO.
func ProjectFromEntity(p *projectdom.Project) ProjectResponse {
	settings := p.Settings
	if settings == nil {
		settings = map[string]any{}
	}
	return ProjectResponse{
		ID:           p.ID,
		Name:         p.Name,
		Description:  p.Description,
		TaskIDPrefix: p.TaskIDPrefix,
		IsPublic:     p.IsPublic,
		Settings:     settings,
		CreatedBy:    p.CreatedBy,
		CreatedAt:    p.CreatedAt,
	}
}
