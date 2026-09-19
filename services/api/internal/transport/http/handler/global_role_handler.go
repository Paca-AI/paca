package handler

import (
	"context"
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

// requireGrantablePermissions reports whether a request that would leave a role
// holding these permissions may proceed, writing the 403 itself when it may not.
//
// These endpoints are where a global role definition is written, which makes
// them where a custom god-mode role would be born: an ADMIN holds
// global_roles.write but not PermissionAll (authz.DefaultGlobalRoles), and a
// stored "*" grants every permission there is to everything the role is later
// assigned to — a user, or a global agent whose API key then carries god mode.
// Nothing else in the request is privileged: creating an ordinary role stays a
// plain global_roles.write action, which is why this is a conditional handler
// check rather than a route-level gate.
//
// The map checked here is the one the role would be left holding, not merely
// the submitted payload: Update's in.Permissions == nil preserves the stored
// map (global_role_service.go), so an edit that never mentions the wildcard
// can still leave it on the role. That is the boundary this guard exists for —
// a caller without PermissionAll must not be able to leave a role holding the
// wildcard, whether the payload supplies it or the stored row already did.
//
// The map is read the way the permission store reads it back
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

// effectivePermissions returns the permissions a Create/Update submission
// would leave the role holding.
func (h *GlobalRoleHandler) effectivePermissions(ctx context.Context, id uuid.UUID, submitted map[string]any) map[string]any {
	if submitted != nil {
		return submitted
	}
	existing, err := h.svc.FindByID(ctx, id)
	if err != nil || existing == nil {
		return nil
	}
	return existing.Permissions
}

// requireEffectivePermissions is requireGrantablePermissions applied to the
// permissions a request would leave the role holding — see that helper's doc
// comment for the boundary, and effectivePermissions for the resolution.
func (h *GlobalRoleHandler) requireEffectivePermissions(w http.ResponseWriter, r *http.Request, id uuid.UUID, submitted map[string]any) bool {
	return h.requireGrantablePermissions(w, r, h.effectivePermissions(r.Context(), id, submitted))
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

// requireAssignableRoles reports whether a request that wants to assign these
// roles to a user may proceed, writing the 403 itself when it may not.
//
// ReplaceUserRoles takes role *ids* — unlike /admin/users' role-name check —
// but it is the same grant: it writes users.role_id, and
// AuthzPermissionStore.ListGlobalPermissions re-reads the target row's stored
// permissions on every request, so assigning the SUPER_ADMIN row takes effect
// immediately, with no re-login. An ADMIN reaches it because the route asks
// only for global_roles.assign, which global_roles.* already satisfies through
// hasPermission's .* suffix rule, and GET /admin/global-roles (gated on
// global_roles.read, also held) hands over that row's id. Nothing in the
// request reveals the grant, so the check must resolve each id's stored
// permissions rather than inspect the payload. This is the user-side twin of
// AgentHandler's global_role_id check.
//
// Clearing the assignment (an empty list) grants nothing and stays allowed, as
// does assigning ordinary roles. Ids the lookup can't resolve are skipped so
// the service keeps producing its own 404 for them.
func (h *GlobalRoleHandler) requireAssignableRoles(w http.ResponseWriter, r *http.Request, roleIDs []uuid.UUID) bool {
	for _, roleID := range roleIDs {
		role, err := h.svc.FindByID(r.Context(), roleID)
		if err != nil || role == nil {
			continue
		}
		if !permissionsGrantAll(role.Permissions) {
			continue
		}
		return middleware.EnforcePermissions(w, r, h.authorizer, middleware.GlobalScope(), authz.PermissionAll)
	}
	return true
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

	// See requireGrantablePermissions: an edit can add "*" to a role, and one
	// that never mentions it can still leave the stored wildcard in place —
	// either way the role would keep holding god mode.
	if !h.requireEffectivePermissions(w, r, id, req.Permissions) {
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

	if !h.requireAssignableRoles(w, r, req.RoleIDs) {
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
