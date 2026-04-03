package handler

import (
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/paca/api/internal/apierr"
	projectdom "github.com/paca/api/internal/domain/project"
	"github.com/paca/api/internal/transport/http/dto"
	"github.com/paca/api/internal/transport/http/middleware"
	"github.com/paca/api/internal/transport/http/presenter"
)

// ListRoles handles GET /projects/:projectId/roles.
func (h *ProjectHandler) ListRoles(c *gin.Context) {
	id, err := parseProjectID(c)
	if err != nil {
		presenter.Error(c, err)
		return
	}
	roles, err := h.svc.ListRoles(c.Request.Context(), id)
	if err != nil {
		presenter.Error(c, err)
		return
	}
	resp := make([]dto.ProjectRoleResponse, 0, len(roles))
	for _, r := range roles {
		resp = append(resp, dto.ProjectRoleFromEntity(r))
	}
	presenter.OK(c, resp)
}

// CreateRole handles POST /projects/:projectId/roles.
func (h *ProjectHandler) CreateRole(c *gin.Context) {
	id, err := parseProjectID(c)
	if err != nil {
		presenter.Error(c, err)
		return
	}

	var req dto.CreateProjectRoleRequest
	if !middleware.BindJSON(c, &req) {
		return
	}

	role, err := h.svc.CreateRole(c.Request.Context(), id, projectdom.CreateRoleInput{
		RoleName:    req.RoleName,
		Permissions: req.Permissions,
	})
	if err != nil {
		presenter.Error(c, err)
		return
	}
	presenter.Created(c, dto.ProjectRoleFromEntity(role))
}

// UpdateRole handles PATCH /projects/:projectId/roles/:roleId.
func (h *ProjectHandler) UpdateRole(c *gin.Context) {
	projectID, err := parseProjectID(c)
	if err != nil {
		presenter.Error(c, err)
		return
	}
	roleID, err := uuid.Parse(c.Param("roleId"))
	if err != nil {
		presenter.Error(c, apierr.New(apierr.CodeBadRequest, "invalid role id"))
		return
	}

	var req dto.UpdateProjectRoleRequest
	if !middleware.BindJSON(c, &req) {
		return
	}

	role, err := h.svc.UpdateRole(c.Request.Context(), projectID, roleID, projectdom.UpdateRoleInput{
		RoleName:    req.RoleName,
		Permissions: req.Permissions,
	})
	if err != nil {
		presenter.Error(c, err)
		return
	}
	presenter.OK(c, dto.ProjectRoleFromEntity(role))
}

// DeleteRole handles DELETE /projects/:projectId/roles/:roleId.
func (h *ProjectHandler) DeleteRole(c *gin.Context) {
	projectID, err := parseProjectID(c)
	if err != nil {
		presenter.Error(c, err)
		return
	}
	roleID, err := uuid.Parse(c.Param("roleId"))
	if err != nil {
		presenter.Error(c, apierr.New(apierr.CodeBadRequest, "invalid role id"))
		return
	}
	if err := h.svc.DeleteRole(c.Request.Context(), projectID, roleID); err != nil {
		presenter.Error(c, err)
		return
	}
	presenter.OK(c, gin.H{"message": "project role deleted"})
}
