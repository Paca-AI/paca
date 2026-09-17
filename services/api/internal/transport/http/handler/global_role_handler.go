package handler

import (
	"net/http"
	"strings"

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
	svc globalroledom.Service
	// authorizer backs Create/Update's conditional god-mode permission guard
	// — see WithAuthorizer's doc comment.
	authorizer *authz.Authorizer
}

// NewGlobalRoleHandler returns a GlobalRoleHandler wired to the service.
func NewGlobalRoleHandler(svc globalroledom.Service) *GlobalRoleHandler {
	return &GlobalRoleHandler{svc: svc}
}

// WithAuthorizer attaches the permission authorizer used by Create/Update to
// require the universal authz.PermissionAll wildcard whenever the submitted
// permissions grant it — see requireGrantablePermissions' own comment. The
// route-level global_roles.write gate (router.go) covers everything else these
// handlers do; this is a narrower, conditional check on top of it, so it lives
// here rather than as a second router.With(...) permission group.
func (h *GlobalRoleHandler) WithAuthorizer(a *authz.Authorizer) *GlobalRoleHandler {
	h.authorizer = a
	return h
}

// requireGrantablePermissions reports whether a request that wants to store
// these permissions may proceed, writing the 403 itself when it may not.
//
// These two endpoints are the only place a global role definition is written,
// which makes them where a custom god-mode role would be born: an ADMIN holds
// global_roles.write but not PermissionAll (authz.DefaultGlobalRoles), and a
// stored "*" grants every permission there is to everything the role is later
// assigned to — a user, or a global agent whose API key then carries god mode.
// Nothing else in the request is privileged: creating an ordinary role stays a
// plain global_roles.write action, which is why this is a conditional handler
// check rather than a route-level gate.
//
// The payload is read the way the permission store reads it back
// (postgres.permissionsFromJSON): keys are trimmed before being granted, so a
// padded " *" is the wildcard here too, and a key counts as granted when its
// value is boolean true, a nonzero number, or "true" in any case. Reading it
// any other way would let a payload pass this guard and still resolve to
// PermissionAll at request time.
func (h *GlobalRoleHandler) requireGrantablePermissions(w http.ResponseWriter, r *http.Request, perms map[string]any) bool {
	if !permissionsGrantAll(perms) {
		return true
	}
	return middleware.EnforcePermissions(w, r, h.authorizer, middleware.GlobalScope(), authz.PermissionAll)
}

// permissionsGrantAll reports whether a submitted permissions map grants the
// universal authz.PermissionAll wildcard. See requireGrantablePermissions for
// why the truthiness rules match postgres.permissionsFromJSON's.
func permissionsGrantAll(perms map[string]any) bool {
	for key, value := range perms {
		if strings.TrimSpace(key) != string(authz.PermissionAll) {
			continue
		}
		switch v := value.(type) {
		case bool:
			return v
		case float64:
			return v != 0
		case string:
			return strings.EqualFold(v, "true")
		}
	}
	return false
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

	// See requireGrantablePermissions: storing "*" in a role definition is how
	// a caller grants god mode to everything that role is later assigned to.
	if !h.requireGrantablePermissions(w, r, req.Permissions) {
		return
	}

	role, err := h.svc.Create(r.Context(), globalroledom.CreateInput{
		Name:        req.Name,
		Permissions: req.Permissions,
	})
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

	// See Create's identical check — same role-definition write, same god-mode
	// boundary, and an edit can just as easily add "*" to an existing role.
	if !h.requireGrantablePermissions(w, r, req.Permissions) {
		return
	}

	role, err := h.svc.Update(r.Context(), id, globalroledom.UpdateInput{
		Name:        req.Name,
		Permissions: req.Permissions,
	})
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

	roles, err := h.svc.ReplaceUserRoles(r.Context(), userID, req.RoleIDs)
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
