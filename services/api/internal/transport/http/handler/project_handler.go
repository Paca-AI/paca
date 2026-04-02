package handler

import (
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/paca/api/internal/apierr"
	projectdom "github.com/paca/api/internal/domain/project"
	"github.com/paca/api/internal/transport/http/dto"
	"github.com/paca/api/internal/transport/http/middleware"
	"github.com/paca/api/internal/transport/http/presenter"
)

// ProjectHandler handles project management endpoints.
type ProjectHandler struct {
	svc projectdom.Service
}

// NewProjectHandler returns a ProjectHandler wired to the service.
func NewProjectHandler(svc projectdom.Service) *ProjectHandler {
	return &ProjectHandler{svc: svc}
}

// ListProjects handles GET /admin/projects.
func (h *ProjectHandler) ListProjects(c *gin.Context) {
	page, pageSize := pagingParams(c)
	projects, total, err := h.svc.List(c.Request.Context(), page, pageSize)
	if err != nil {
		presenter.Error(c, err)
		return
	}
	resp := make([]dto.ProjectResponse, 0, len(projects))
	for _, p := range projects {
		resp = append(resp, dto.ProjectFromEntity(p))
	}
	presenter.OK(c, gin.H{"items": resp, "total": total, "page": page, "page_size": pageSize})
}

// GetProject handles GET /admin/projects/:projectId.
func (h *ProjectHandler) GetProject(c *gin.Context) {
	id, err := parseProjectID(c)
	if err != nil {
		presenter.Error(c, err)
		return
	}
	p, err := h.svc.GetByID(c.Request.Context(), id)
	if err != nil {
		presenter.Error(c, err)
		return
	}
	presenter.OK(c, dto.ProjectFromEntity(p))
}

// CreateProject handles POST /admin/projects.
func (h *ProjectHandler) CreateProject(c *gin.Context) {
	var req dto.CreateProjectRequest
	if !middleware.BindJSON(c, &req) {
		return
	}

	claims := middleware.ClaimsFrom(c)
	var createdBy *uuid.UUID
	if claims != nil {
		if uid, err := uuid.Parse(claims.Subject); err == nil {
			createdBy = &uid
		}
	}

	p, err := h.svc.Create(c.Request.Context(), projectdom.CreateProjectInput{
		Name:        req.Name,
		Description: req.Description,
		Settings:    req.Settings,
		CreatedBy:   createdBy,
	})
	if err != nil {
		presenter.Error(c, err)
		return
	}
	presenter.Created(c, dto.ProjectFromEntity(p))
}

// UpdateProject handles PATCH /admin/projects/:projectId.
func (h *ProjectHandler) UpdateProject(c *gin.Context) {
	id, err := parseProjectID(c)
	if err != nil {
		presenter.Error(c, err)
		return
	}

	var req dto.UpdateProjectRequest
	if !middleware.BindJSON(c, &req) {
		return
	}

	p, err := h.svc.Update(c.Request.Context(), id, projectdom.UpdateProjectInput{
		Name:        req.Name,
		Description: req.Description,
		Settings:    req.Settings,
	})
	if err != nil {
		presenter.Error(c, err)
		return
	}
	presenter.OK(c, dto.ProjectFromEntity(p))
}

// DeleteProject handles DELETE /admin/projects/:projectId.
func (h *ProjectHandler) DeleteProject(c *gin.Context) {
	id, err := parseProjectID(c)
	if err != nil {
		presenter.Error(c, err)
		return
	}
	if err := h.svc.Delete(c.Request.Context(), id); err != nil {
		presenter.Error(c, err)
		return
	}
	presenter.OK(c, gin.H{"message": "project deleted"})
}

// --- helpers ----------------------------------------------------------------

func parseProjectID(c *gin.Context) (uuid.UUID, error) {
	id, err := uuid.Parse(c.Param("projectId"))
	if err != nil {
		return uuid.Nil, apierr.New(apierr.CodeBadRequest, "invalid project id")
	}
	return id, nil
}

func pagingParams(c *gin.Context) (page, pageSize int) {
	page, _ = strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ = strconv.Atoi(c.DefaultQuery("page_size", "20"))
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}
	return page, pageSize
}
