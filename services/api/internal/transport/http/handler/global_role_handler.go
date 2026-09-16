package handler

import (
	"errors"
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

// superAdminRoleName is the canonical name of the built-in god-mode role
// seeded by bootstrap.seedDefaultRoles. Its definition and assignment are
// reserved for callers who themselves hold authz.PermissionAll.
const superAdminRoleName = "SUPER_ADMIN"

// GlobalRoleHandler handles super-admin global-role endpoints.
type GlobalRoleHandler struct {
	svc globalroledom.Service
	// authorizer is optional: when set, god-mode role mutations — granting
	// authz.PermissionAll to a role, or assigning/altering/deleting a role
	// that carries it, including the SUPER_ADMIN role itself — are refused
	// to callers who don't hold PermissionAll. Without this guard, any ADMIN
	// (who legitimately holds global_roles.*) could edit role permissions to
	// promote themselves to SUPER_ADMIN. Production bootstrap always wires
	// it; tests that don't exercise the guard leave it nil.
	authorizer *authz.Authorizer
}

// NewGlobalRoleHandler returns a GlobalRoleHandler wired to the service.
// Pass the authorizer to enable the privilege-escalation guard on god-mode
// role mutations.
func NewGlobalRoleHandler(svc globalroledom.Service, authorizer ...*authz.Authorizer) *GlobalRoleHandler {
	var a *authz.Authorizer
	if len(authorizer) > 0 {
		a = authorizer[0]
	}
	return &GlobalRoleHandler{svc: svc, authorizer: a}
}

// requireGodModePermission denies the request unless the authenticated caller
// (human via JWT, or global agent via agent API key) holds the universal
// authz.PermissionAll wildcard — the only privilege that may grant, assign,
// or mutate a role carrying god-mode permissions. Returns true when the
// request may proceed; with no authorizer wired (test routers only) it always
// proceeds.
func (h *GlobalRoleHandler) requireGodModePermission(w http.ResponseWriter, r *http.Request) bool {
	if h.authorizer == nil {
		return true
	}

	allowed, err := middleware.ActorHasPermissionAll(r, h.authorizer)
	if err != nil {
		presenter.Error(w, r, err)
		return false
	}
	if !allowed {
		presenter.Error(w, r, apierr.New(apierr.CodeForbidden, "only SUPER_ADMIN may grant or modify the universal permission"))
		return false
	}
	return true
}

// isReservedRoleName reports whether name refers to the built-in SUPER_ADMIN
// role. Trimmed first: the service trims names before persisting them, so an
// untrimmed comparison could be walked past with " SUPER_ADMIN ".
func isReservedRoleName(name string) bool {
	return strings.EqualFold(strings.TrimSpace(name), superAdminRoleName)
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

	// Granting the universal permission (or squatting the SUPER_ADMIN role
	// name) is the privilege-escalation vector: without this guard an ADMIN
	// holding global_roles.* could mint themselves a god-mode role.
	if (isReservedRoleName(req.Name) || authz.PermissionsGrantAll(req.Permissions)) && !h.requireGodModePermission(w, r) {
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

	existing, err := h.svc.FindByID(r.Context(), id)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	// Editing the SUPER_ADMIN role's own definition, renaming into its
	// reserved name, or adding the universal permission all require the
	// caller to hold PermissionAll themselves.
	if (isReservedRoleName(existing.Name) ||
		isReservedRoleName(req.Name) ||
		authz.PermissionsGrantAll(req.Permissions)) && !h.requireGodModePermission(w, r) {
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

	existing, err := h.svc.FindByID(r.Context(), id)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	// The SUPER_ADMIN role definition is reserved: deleting it (or letting
	// an ADMIN reshape it via Update) would let a non-god caller tamper
	// with the highest privilege tier in the system.
	if isReservedRoleName(existing.Name) && !h.requireGodModePermission(w, r) {
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

	// Assigning a role that grants the universal permission is the direct
	// self-elevation path (ADMIN puts SUPER_ADMIN on themselves). Nonexistent
	// role IDs are left to the service/repository, which rejects them with
	// GLOBAL_ROLE_NOT_FOUND — nothing to escalate with there.
	for _, roleID := range req.RoleIDs {
		role, err := h.svc.FindByID(r.Context(), roleID)
		if err != nil {
			if errors.Is(err, globalroledom.ErrNotFound) {
				continue
			}
			presenter.Error(w, r, err)
			return
		}
		if authz.PermissionsGrantAll(role.Permissions) && !h.requireGodModePermission(w, r) {
			return
		}
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
