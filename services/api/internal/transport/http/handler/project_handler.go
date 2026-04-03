package handler

import (
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/paca/api/internal/apierr"
	projectdom "github.com/paca/api/internal/domain/project"
	"github.com/paca/api/internal/platform/authz"
	"github.com/paca/api/internal/transport/http/dto"
	"github.com/paca/api/internal/transport/http/middleware"
	"github.com/paca/api/internal/transport/http/presenter"
)

// ProjectHandler handles project management endpoints.
type ProjectHandler struct {
	svc        projectdom.Service
	authorizer *authz.Authorizer
}

// NewProjectHandler returns a ProjectHandler wired to the service and authorizer.
func NewProjectHandler(svc projectdom.Service, authorizer *authz.Authorizer) *ProjectHandler {
	return &ProjectHandler{svc: svc, authorizer: authorizer}
}

// ListProjects handles GET /projects.
// Users with the global projects.read permission receive all projects.
// All other authenticated users receive only the projects they are a member of.
func (h *ProjectHandler) ListProjects(c *gin.Context) {
	claims := middleware.ClaimsFrom(c)
	page, pageSize := pagingParams(c)

	var (
		projects []*projectdom.Project
		total    int64
		err      error
	)

	userID, parseErr := uuid.Parse(claims.Subject)
	if parseErr != nil {
		presenter.Error(c, apierr.New(apierr.CodeBadRequest, "invalid subject claim"))
		return
	}

	hasGlobalRead, authzErr := h.authorizer.HasPermissions(
		c.Request.Context(), userID, nil, claims.Role, authz.PermissionProjectsRead,
	)
	if authzErr != nil {
		presenter.Error(c, authzErr)
		return
	}

	if hasGlobalRead {
		projects, total, err = h.svc.List(c.Request.Context(), page, pageSize)
	} else {
		projects, total, err = h.svc.ListAccessible(c.Request.Context(), userID, page, pageSize)
	}
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

// GetProject handles GET /projects/:projectId.
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

// CreateProject handles POST /projects.
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

// UpdateProject handles PATCH /projects/:projectId.
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

// DeleteProject handles DELETE /projects/:projectId.
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
