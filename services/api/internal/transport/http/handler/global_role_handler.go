package handler

import (
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/paca/api/internal/apierr"
	globalroledom "github.com/paca/api/internal/domain/globalrole"
	"github.com/paca/api/internal/transport/http/dto"
	"github.com/paca/api/internal/transport/http/middleware"
	"github.com/paca/api/internal/transport/http/presenter"
)

// GlobalRoleHandler handles super-admin global-role endpoints.
type GlobalRoleHandler struct {
	svc globalroledom.Service
}

// NewGlobalRoleHandler returns a GlobalRoleHandler wired to the service.
func NewGlobalRoleHandler(svc globalroledom.Service) *GlobalRoleHandler {
	return &GlobalRoleHandler{svc: svc}
}

// List handles GET /admin/global-roles.
func (h *GlobalRoleHandler) List(c *gin.Context) {
	roles, err := h.svc.List(c.Request.Context())
	if err != nil {
		presenter.Error(c, err)
		return
	}
	resp := make([]dto.GlobalRoleResponse, 0, len(roles))
	for _, role := range roles {
		resp = append(resp, dto.GlobalRoleFromEntity(role))
	}
	presenter.OK(c, resp)
}

// Create handles POST /admin/global-roles.
func (h *GlobalRoleHandler) Create(c *gin.Context) {
	var req dto.CreateGlobalRoleRequest
	if !middleware.BindJSON(c, &req) {
		return
	}

	role, err := h.svc.Create(c.Request.Context(), globalroledom.CreateInput{
		Name:        req.Name,
		Permissions: req.Permissions,
	})
	if err != nil {
		presenter.Error(c, err)
		return
	}
	presenter.Created(c, dto.GlobalRoleFromEntity(role))
}

// Update handles PATCH /admin/global-roles/:roleId.
func (h *GlobalRoleHandler) Update(c *gin.Context) {
	id, err := uuid.Parse(c.Param("roleId"))
	if err != nil {
		presenter.Error(c, apierr.New(apierr.CodeBadRequest, "invalid role id"))
		return
	}

	var req dto.UpdateGlobalRoleRequest
	if !middleware.BindJSON(c, &req) {
		return
	}

	role, err := h.svc.Update(c.Request.Context(), id, globalroledom.UpdateInput{
		Name:        req.Name,
		Permissions: req.Permissions,
	})
	if err != nil {
		presenter.Error(c, err)
		return
	}
	presenter.OK(c, dto.GlobalRoleFromEntity(role))
}

// Delete handles DELETE /admin/global-roles/:roleId.
func (h *GlobalRoleHandler) Delete(c *gin.Context) {
	id, err := uuid.Parse(c.Param("roleId"))
	if err != nil {
		presenter.Error(c, apierr.New(apierr.CodeBadRequest, "invalid role id"))
		return
	}

	if err := h.svc.Delete(c.Request.Context(), id); err != nil {
		presenter.Error(c, err)
		return
	}
	presenter.OK(c, gin.H{"message": "global role deleted"})
}

// ReplaceUserRoles handles PUT /admin/users/:userId/global-roles.
func (h *GlobalRoleHandler) ReplaceUserRoles(c *gin.Context) {
	userID, err := uuid.Parse(c.Param("userId"))
	if err != nil {
		presenter.Error(c, apierr.New(apierr.CodeBadRequest, "invalid user id"))
		return
	}

	var req dto.ReplaceUserGlobalRolesRequest
	if !middleware.BindJSON(c, &req) {
		return
	}

	roles, err := h.svc.ReplaceUserRoles(c.Request.Context(), userID, req.RoleIDs)
	if err != nil {
		presenter.Error(c, err)
		return
	}

	resp := make([]dto.GlobalRoleResponse, 0, len(roles))
	for _, role := range roles {
		resp = append(resp, dto.GlobalRoleFromEntity(role))
	}
	presenter.OK(c, gin.H{"user_id": userID, "roles": resp})
}
