package dto

import (
	"time"

	"github.com/google/uuid"
	projectdom "github.com/paca/api/internal/domain/project"
)

// --- Project DTOs -----------------------------------------------------------

// CreateProjectRequest is the body for POST /projects.
type CreateProjectRequest struct {
	Name        string         `json:"name" binding:"required"`
	Description string         `json:"description"`
	Settings    map[string]any `json:"settings"`
}

// UpdateProjectRequest is the body for PATCH /projects/:projectId.
type UpdateProjectRequest struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Settings    map[string]any `json:"settings"`
}

// ProjectResponse is the public representation of a project.
type ProjectResponse struct {
	ID          uuid.UUID      `json:"id"`
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Settings    map[string]any `json:"settings"`
	CreatedBy   *uuid.UUID     `json:"created_by,omitempty"`
	CreatedAt   time.Time      `json:"created_at"`
}

// ProjectFromEntity maps a domain Project to a ProjectResponse DTO.
func ProjectFromEntity(p *projectdom.Project) ProjectResponse {
	settings := p.Settings
	if settings == nil {
		settings = map[string]any{}
	}
	return ProjectResponse{
		ID:          p.ID,
		Name:        p.Name,
		Description: p.Description,
		Settings:    settings,
		CreatedBy:   p.CreatedBy,
		CreatedAt:   p.CreatedAt,
	}
}
