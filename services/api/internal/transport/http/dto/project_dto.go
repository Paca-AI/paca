package dto

import (
	"time"

	"github.com/google/uuid"

	projectdom "github.com/Paca-AI/api/internal/domain/project"
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
	// JevConfigured/JevBaseURL/JevModel describe this project's Jev setup
	// without ever exposing the API key itself (same has_*-boolean
	// convention as AgentResponse's has_cli_api_key etc.) — see
	// projectdom.Project.JevAPIKeySecret's doc comment.
	JevConfigured bool   `json:"jev_configured"`
	JevBaseURL    string `json:"jev_base_url"`
	JevModel      string `json:"jev_model"`
	// AvatarURL/AvatarThumbURL are presigned GET URLs, populated by the
	// handler (not this mapper) via attachmentdom.AvatarService — nil when
	// no avatar has been uploaded.
	AvatarURL      *string    `json:"avatar_url,omitempty"`
	AvatarThumbURL *string    `json:"avatar_thumb_url,omitempty"`
	CreatedBy      *uuid.UUID `json:"created_by,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
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
		ID:            p.ID,
		Name:          p.Name,
		Description:   p.Description,
		TaskIDPrefix:  p.TaskIDPrefix,
		IsPublic:      p.IsPublic,
		Settings:      settings,
		JevConfigured: p.JevConfigured(),
		JevBaseURL:    p.JevBaseURL,
		JevModel:      p.JevModel,
		CreatedBy:     p.CreatedBy,
		CreatedAt:     p.CreatedAt,
	}
}

// UpdateProjectJevConfigRequest is the body for PATCH
// /projects/:projectId/jev-config. Each field is independently optional
// (nil = leave unchanged); ApiKey's zero value is the empty string, so an
// explicit "" clears/disables Jev for this project — see projectsvc.
// Service.UpdateJevConfig.
type UpdateProjectJevConfigRequest struct {
	APIKey  *string `json:"api_key"`
	BaseURL *string `json:"base_url"`
	Model   *string `json:"model"`
}

// ProjectJevConfigResponse is the response for both PATCH and (via
// ProjectResponse) GET reads of a project's Jev setup. The API key itself
// is never included.
type ProjectJevConfigResponse struct {
	Configured bool   `json:"configured"`
	BaseURL    string `json:"base_url"`
	Model      string `json:"model"`
}

// TestJevConfigResponse is the response for POST
// /projects/:projectId/jev-config/test — a successful response means Jev
// answered the throwaway connectivity question, i.e. this project's stored
// credentials/host/model actually work.
type TestJevConfigResponse struct {
	Success bool `json:"success"`
}
