package handler

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/Paca-AI/api/internal/apierr"
	globalroledom "github.com/Paca-AI/api/internal/domain/globalrole"
	"github.com/Paca-AI/api/internal/platform/authz"
	"github.com/Paca-AI/api/internal/transport/http/dto"
	"github.com/Paca-AI/api/internal/transport/http/middleware"
	"github.com/Paca-AI/api/internal/transport/http/presenter"
)

// GlobalRoleHandler handles super-admin global-role endpoints.
type GlobalRoleHandler struct {
	svc        globalroledom.Service
	authorizer *authz.Authorizer
}

// NewGlobalRoleHandler returns a GlobalRoleHandler wired to the service and
// authorizer. The authorizer resolves the caller's own effective global
// permissions so the service can enforce a grant ceiling (PACA-3).
func NewGlobalRoleHandler(svc globalroledom.Service, authorizer *authz.Authorizer) *GlobalRoleHandler {
	return &GlobalRoleHandler{svc: svc, authorizer: authorizer}
}

// callerGlobalPermissions resolves the authenticated caller's own effective
// GLOBAL permission set, used as the grant ceiling. It fails closed: a request
// with no resolvable caller identity yields an error rather than an empty (and
// therefore permissive-looking) set.
func (h *GlobalRoleHandler) callerGlobalPermissions(r *http.Request) (authz.PermissionSet, error) {
	claims := middleware.ClaimsFrom(r)
	if claims == nil {
		return nil, apierr.New(apierr.CodeUnauthenticated, "unauthenticated")
	}
	userID, err := uuid.Parse(claims.Subject)
	if err != nil {
		return nil, apierr.New(apierr.CodeBadRequest, "invalid subject claim")
	}
	return h.authorizer.EffectivePermissions(r.Context(), userID, nil, claims.Role)
}

// List handles GET /admin/global-roles.
func (h *GlobalRoleHandler) List(w http.ResponseWriter, r *http.Request) {
	roles, err := h.svc.List(r.Context())
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	resp := make([]dto.GlobalRoleResponse, 0, len(roles))
	for _, role := range roles {
		resp = append(resp, dto.GlobalRoleFromEntity(role))
	}
	presenter.OK(w, r, resp)
}

// Create handles POST /admin/global-roles.
func (h *GlobalRoleHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req dto.CreateGlobalRoleRequest
	if !middleware.BindJSON(w, r, &req) {
		return
	}
	if req.Name == "" {
		presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "name is required"))
		return
	}

	caller, err := h.callerGlobalPermissions(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}

	role, err := h.svc.Create(r.Context(), globalroledom.CreateInput{
		Name:        req.Name,
		Permissions: req.Permissions,
	}, caller)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.Created(w, r, dto.GlobalRoleFromEntity(role))
}

// Update handles PATCH /admin/global-roles/:roleId.
func (h *GlobalRoleHandler) Update(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "roleId"))
	if err != nil {
		presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "invalid role id"))
		return
	}

	var req dto.UpdateGlobalRoleRequest
	if !middleware.BindJSON(w, r, &req) {
		return
	}
	if req.Name == "" {
		presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "name is required"))
		return
	}

	caller, err := h.callerGlobalPermissions(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}

	role, err := h.svc.Update(r.Context(), id, globalroledom.UpdateInput{
		Name:        req.Name,
		Permissions: req.Permissions,
	}, caller)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.OK(w, r, dto.GlobalRoleFromEntity(role))
}

// Delete handles DELETE /admin/global-roles/:roleId.
func (h *GlobalRoleHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "roleId"))
	if err != nil {
		presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "invalid role id"))
		return
	}

	if err := h.svc.Delete(r.Context(), id); err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.OK(w, r, map[string]any{"message": "global role deleted"})
}

// ReplaceUserRoles handles PUT /admin/users/:userId/global-roles.
func (h *GlobalRoleHandler) ReplaceUserRoles(w http.ResponseWriter, r *http.Request) {
	userID, err := uuid.Parse(chi.URLParam(r, "userId"))
	if err != nil {
		presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "invalid user id"))
		return
	}

	var req dto.ReplaceUserGlobalRolesRequest
	if !middleware.BindJSON(w, r, &req) {
		return
	}

	caller, err := h.callerGlobalPermissions(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}

	roles, err := h.svc.ReplaceUserRoles(r.Context(), userID, req.RoleIDs, caller)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}

	resp := make([]dto.GlobalRoleResponse, 0, len(roles))
	for _, role := range roles {
		resp = append(resp, dto.GlobalRoleFromEntity(role))
	}
	presenter.OK(w, r, map[string]any{"user_id": userID, "roles": resp})
}
