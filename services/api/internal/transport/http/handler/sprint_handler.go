package handler

import (
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/paca/api/internal/apierr"
	sprintdom "github.com/paca/api/internal/domain/sprint"
	"github.com/paca/api/internal/transport/http/dto"
	"github.com/paca/api/internal/transport/http/middleware"
	"github.com/paca/api/internal/transport/http/presenter"
)

// SprintHandler handles sprint management endpoints.
type SprintHandler struct {
	svc sprintdom.SprintService
}

// NewSprintHandler returns a SprintHandler wired to the sprint service.
func NewSprintHandler(svc sprintdom.SprintService) *SprintHandler {
	return &SprintHandler{svc: svc}
}

// ListSprints handles GET /projects/:projectId/sprints.
func (h *SprintHandler) ListSprints(c *gin.Context) {
	projectID, err := parseProjectID(c)
	if err != nil {
		presenter.Error(c, err)
		return
	}
	sprints, err := h.svc.ListSprints(c.Request.Context(), projectID)
	if err != nil {
		presenter.Error(c, err)
		return
	}
	resp := make([]dto.SprintResponse, 0, len(sprints))
	for _, s := range sprints {
		resp = append(resp, dto.SprintFromEntity(s))
	}
	presenter.OK(c, gin.H{"items": resp})
}

// CreateSprint handles POST /projects/:projectId/sprints.
func (h *SprintHandler) CreateSprint(c *gin.Context) {
	projectID, err := parseProjectID(c)
	if err != nil {
		presenter.Error(c, err)
		return
	}

	var req dto.CreateSprintRequest
	if !middleware.BindJSON(c, &req) {
		return
	}

	s, err := h.svc.CreateSprint(c.Request.Context(), sprintdom.CreateSprintInput{
		ProjectID: projectID,
		Name:      req.Name,
		StartDate: req.StartDate,
		EndDate:   req.EndDate,
		Goal:      req.Goal,
		Status:    req.Status,
	})
	if err != nil {
		presenter.Error(c, err)
		return
	}
	presenter.Created(c, dto.SprintFromEntity(s))
}

// UpdateSprint handles PATCH /projects/:projectId/sprints/:sprintId.
func (h *SprintHandler) UpdateSprint(c *gin.Context) {
	sprintID, err := parseSprintID(c)
	if err != nil {
		presenter.Error(c, err)
		return
	}

	var req dto.UpdateSprintRequest
	if !middleware.BindJSON(c, &req) {
		return
	}

	s, err := h.svc.UpdateSprint(c.Request.Context(), sprintID, sprintdom.UpdateSprintInput{
		Name:      req.Name,
		StartDate: req.StartDate,
		EndDate:   req.EndDate,
		Goal:      req.Goal,
		Status:    req.Status,
	})
	if err != nil {
		presenter.Error(c, err)
		return
	}
	presenter.OK(c, dto.SprintFromEntity(s))
}

// DeleteSprint handles DELETE /projects/:projectId/sprints/:sprintId.
func (h *SprintHandler) DeleteSprint(c *gin.Context) {
	sprintID, err := parseSprintID(c)
	if err != nil {
		presenter.Error(c, err)
		return
	}
	if err := h.svc.DeleteSprint(c.Request.Context(), sprintID); err != nil {
		presenter.Error(c, err)
		return
	}
	presenter.OK(c, gin.H{"message": "sprint deleted"})
}

// parseSprintID extracts and validates the :sprintId path parameter.
func parseSprintID(c *gin.Context) (uuid.UUID, error) {
	id, err := uuid.Parse(c.Param("sprintId"))
	if err != nil {
		return uuid.Nil, apierr.New(apierr.CodeBadRequest, "invalid sprint id")
	}
	return id, nil
}

// GetSprint handles GET /projects/:projectId/sprints/:sprintId.
func (h *SprintHandler) GetSprint(c *gin.Context) {
	sprintID, err := parseSprintID(c)
	if err != nil {
		presenter.Error(c, err)
		return
	}
	s, err := h.svc.GetSprint(c.Request.Context(), sprintID)
	if err != nil {
		presenter.Error(c, err)
		return
	}
	presenter.OK(c, dto.SprintFromEntity(s))
}
