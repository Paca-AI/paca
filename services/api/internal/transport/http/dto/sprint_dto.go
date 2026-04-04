package dto

import (
	"time"

	"github.com/google/uuid"
	sprintdom "github.com/paca/api/internal/domain/sprint"
)

// --- Sprint DTOs ------------------------------------------------------------

// CreateSprintRequest is the body for POST /projects/:projectId/sprints.
type CreateSprintRequest struct {
	Name      string                 `json:"name" binding:"required"`
	StartDate *time.Time             `json:"start_date"`
	EndDate   *time.Time             `json:"end_date"`
	Goal      *string                `json:"goal"`
	Status    sprintdom.SprintStatus `json:"status"`
}

// UpdateSprintRequest is the body for PATCH /projects/:projectId/sprints/:sprintId.
type UpdateSprintRequest struct {
	Name      string                  `json:"name"`
	StartDate *time.Time              `json:"start_date"`
	EndDate   *time.Time              `json:"end_date"`
	Goal      *string                 `json:"goal"`
	Status    *sprintdom.SprintStatus `json:"status"`
}

// SprintResponse is the public representation of a sprint.
type SprintResponse struct {
	ID        uuid.UUID              `json:"id"`
	ProjectID uuid.UUID              `json:"project_id"`
	Name      string                 `json:"name"`
	StartDate *time.Time             `json:"start_date,omitempty"`
	EndDate   *time.Time             `json:"end_date,omitempty"`
	Goal      *string                `json:"goal,omitempty"`
	Status    sprintdom.SprintStatus `json:"status"`
	CreatedAt time.Time              `json:"created_at"`
	UpdatedAt time.Time              `json:"updated_at"`
}

// SprintFromEntity maps a domain Sprint to a SprintResponse DTO.
func SprintFromEntity(s *sprintdom.Sprint) SprintResponse {
	return SprintResponse{
		ID:        s.ID,
		ProjectID: s.ProjectID,
		Name:      s.Name,
		StartDate: s.StartDate,
		EndDate:   s.EndDate,
		Goal:      s.Goal,
		Status:    s.Status,
		CreatedAt: s.CreatedAt,
		UpdatedAt: s.UpdatedAt,
	}
}
