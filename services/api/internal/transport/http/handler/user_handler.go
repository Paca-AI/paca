package handler

import (
	"context"

	"net/http"
	"net/mail"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/Paca-AI/api/internal/apierr"
	attachmentdom "github.com/Paca-AI/api/internal/domain/attachment"
	globalroledom "github.com/Paca-AI/api/internal/domain/globalrole"
	domainuser "github.com/Paca-AI/api/internal/domain/user"
	"github.com/Paca-AI/api/internal/platform/authz"
	"github.com/Paca-AI/api/internal/transport/http/dto"
	"github.com/Paca-AI/api/internal/transport/http/middleware"
	"github.com/Paca-AI/api/internal/transport/http/presenter"
)

// normalizeEmail trims raw and, if non-empty, validates it as an RFC 5322
// address, returning the canonical address lowercased. An empty/whitespace
// -only input passes through as "" unchanged — every Email input field in
// this package treats "" as "not provided" (create) or "no change"
// (update), matching FullName/Role's existing convention. Lowercasing keeps
// the uniqueness pre-check consistent with the case-sensitive
// uni_users_email_active index: without it, "User@x.com" and "user@x.com"
// would be stored as distinct rows for what mail servers treat as the same
// mailbox.
func normalizeEmail(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", nil
	}
	addr, err := mail.ParseAddress(trimmed)
	if err != nil {
		return "", apierr.New(apierr.CodeBadRequest, "invalid email address")
	}
	return strings.ToLower(addr.Address), nil
}

// SessionInvalidator revokes an authentication session by family ID.
// It is satisfied by domain/auth.Service.
type SessionInvalidator interface {
	Logout(ctx context.Context, familyID string) error
}

// roleLookup resolves a global role by its unique name. Satisfied by
// globalroledom.Repository, the same lookup the user service uses to resolve a
// submitted role name into users.role_id.
type roleLookup interface {
	FindByName(ctx context.Context, name string) (*globalroledom.GlobalRole, error)
}

// UserHandler handles user-related endpoints.
type UserHandler struct {
	svc       domainuser.Service
	authSvc   SessionInvalidator
	avatarSvc attachmentdom.AvatarService
	// authorizer backs CreateUser/AdminUpdateUser's conditional god-mode role
	// guard — see WithAuthorizer's doc comment.
	authorizer *authz.Authorizer
	// roles backs the same guard's lookup of a role's stored permissions — see
	// roleNameGrantsAll's doc comment.
	roles roleLookup
}

// NewUserHandler returns a UserHandler wired to the provided user service.
// Pass an optional SessionInvalidator (e.g. the auth service) as the second
// argument to enable automatic session revocation on password change.
func NewUserHandler(svc domainuser.Service, authSvc ...SessionInvalidator) *UserHandler {
	h := &UserHandler{svc: svc}
	if len(authSvc) > 0 {
		h.authSvc = authSvc[0]
	}
	return h
}

// WithAvatarService configures avatar URL resolution for UserResponse.
func (h *UserHandler) WithAvatarService(svc attachmentdom.AvatarService) *UserHandler {
	h.avatarSvc = svc
	return h
}

// WithAuthorizer attaches the permission authorizer used by
// CreateUser/AdminUpdateUser to require the universal authz.PermissionAll
// wildcard whenever a request names a role that grants it — see
// requireGrantableRole's own comment. The route-level users.write gate
// (router.go) covers everything else these handlers do; this is a narrower,
// conditional check on top of it, so it lives here rather than as a second
// router.With(...) permission group — same shape as AgentHandler's own
// conditional global_roles.assign check for global agents.
func (h *UserHandler) WithAuthorizer(a *authz.Authorizer) *UserHandler {
	h.authorizer = a
	return h
}

// WithRoleLookup attaches the global role lookup requireGrantableRole needs to
// see the stored permissions behind a non-built-in role name — see
// roleNameGrantsAll's doc comment. Without it only the built-in names are
// recognized, which is the rename-and-assign bypass this guard exists to close.
func (h *UserHandler) WithRoleLookup(roles roleLookup) *UserHandler {
	h.roles = roles
	return h
}

// roleNameGrantsAll reports whether a role *name* grants authz.PermissionAll,
// resolved the way the request-time grant is resolved — see
// requireGrantableRole for why both sources are needed.
func roleNameGrantsAll(ctx context.Context, name string, roles roleLookup) bool {
	// The built-ins are checked first: their meaning is static, and
	// LegacyPermissionsForRole is the same resolver the grant side uses, so the
	// look-alike spellings that fold onto a built-in name are refused here too.
	for _, p := range authz.LegacyPermissionsForRole(name) {
		if p == authz.PermissionAll {
			return true
		}
	}
	if roles == nil {
		return false
	}
	role, err := roles.FindByName(ctx, name)
	if err != nil || role == nil {
		return false
	}
	return permissionsGrantAll(role.Permissions)
}

// requireGrantableRole reports whether a request that wants to set a user's
// role to roleName may proceed, writing the 403 itself when it may not.
//
// POST/PATCH /admin/users take a role *name* and write it to both
// users.role_id and the legacy users.role claim. The token issuer copies that
// claim into every access token, and the request-time grant comes from the
// row's stored permissions either way (AuthzPermissionStore.ListGlobalPermissions
// joins users.role_id to global_roles.permissions) — so naming a role that
// carries the universal PermissionAll wildcard here hands out a privilege, it
// is not plain user administration. Without this check any ADMIN, which
// legitimately holds users.write, could promote itself or anyone else to
// SUPER_ADMIN straight from the admin user form whose role dropdown lists
// every global role
// (apps/web/src/components/admin/users/UserFormDialog.tsx).
//
// The built-in names are resolved through LegacyPermissionsForRole rather than
// a name comparison of our own: a look-alike such as "SUPER_ADMıN" (which
// strings.ToUpper folds onto "SUPER_ADMIN") must be refused here too, or the
// guard and the grant would disagree about what a name means. A name that
// resolves to no built-in is then looked up in the global_roles table, because
// the user service resolves any row by name (user_service.go) and the grant
// comes from its stored permissions — a wildcard under a custom name,
// pre-existing or moved there by renaming a god-mode role through the
// global-role Update endpoint, would otherwise pass this check and still yield
// PermissionAll. A name that resolves nowhere is left to the service, which
// produces its own 404; an empty name — the optional field's "leave the role
// alone" value — is always let through.
func (h *UserHandler) requireGrantableRole(w http.ResponseWriter, r *http.Request, roleName string) bool {
	if roleName == "" || !roleNameGrantsAll(r.Context(), roleName, h.roles) {
		return true
	}
	return middleware.EnforcePermissions(w, r, h.authorizer, middleware.GlobalScope(), authz.PermissionAll)
}

// toUserResponse maps u to a UserResponse and, if an AvatarService is
// configured, resolves its avatar keys into presigned display URLs.
func (h *UserHandler) toUserResponse(ctx context.Context, u *domainuser.User) dto.UserResponse {
	resp := dto.UserFromEntity(u)
	if h.avatarSvc != nil {
		resp.AvatarURL, _ = h.avatarSvc.ResolveAvatarURL(ctx, u.AvatarKey)
		resp.AvatarThumbURL, _ = h.avatarSvc.ResolveAvatarURL(ctx, u.AvatarThumbKey)
	}
	return resp
}

// --- Self-service routes ---------------------------------------------------

// GetMe handles GET /users/me — returns the caller's own profile.
func (h *UserHandler) GetMe(w http.ResponseWriter, r *http.Request) {
	claims := middleware.ClaimsFrom(r)
	if claims == nil {
		presenter.Error(w, r, apierr.New(apierr.CodeUnauthenticated, "unauthenticated"))
		return
	}

	id, err := uuid.Parse(claims.Subject)
	if err != nil {
		presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "invalid subject claim"))
		return
	}

	u, err := h.svc.GetByID(r.Context(), id)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}

	presenter.OK(w, r, h.toUserResponse(r.Context(), u))
}

// UpdateMe handles PATCH /users/me — lets users update their own profile.
func (h *UserHandler) UpdateMe(w http.ResponseWriter, r *http.Request) {
	claims := middleware.ClaimsFrom(r)
	if claims == nil {
		presenter.Error(w, r, apierr.New(apierr.CodeUnauthenticated, "unauthenticated"))
		return
	}

	id, err := uuid.Parse(claims.Subject)
	if err != nil {
		presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "invalid subject claim"))
		return
	}

	var req dto.UpdateProfileRequest
	if !middleware.BindJSON(w, r, &req) {
		return
	}
	email, err := normalizeEmail(req.Email)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}

	u, err := h.svc.UpdateProfile(r.Context(), id, domainuser.UpdateProfileInput{
		FullName: req.FullName,
		Email:    email,
	})
	if err != nil {
		presenter.Error(w, r, err)
		return
	}

	presenter.OK(w, r, h.toUserResponse(r.Context(), u))
}

// GetMyGlobalPermissions handles GET /users/me/global-permissions.
func (h *UserHandler) GetMyGlobalPermissions(w http.ResponseWriter, r *http.Request) {
	claims := middleware.ClaimsFrom(r)
	if claims == nil {
		presenter.Error(w, r, apierr.New(apierr.CodeUnauthenticated, "unauthenticated"))
		return
	}

	id, err := uuid.Parse(claims.Subject)
	if err != nil {
		presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "invalid subject claim"))
		return
	}

	permissions, err := h.svc.ListGlobalPermissions(r.Context(), id)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}

	presenter.OK(w, r, map[string]any{"permissions": permissions})
}

// --- Admin user management routes ------------------------------------------

// ListUsers handles GET /admin/users — returns a paginated list of all users.
func (h *UserHandler) ListUsers(w http.ResponseWriter, r *http.Request) {
	page, err := parsePage(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	pageSize, err := parsePageSize(r, 20, 100)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}

	users, total, err := h.svc.List(r.Context(), page, pageSize)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	mustChangePasswordCount, err := h.svc.CountUsersMustChangePassword(r.Context())
	if err != nil {
		presenter.Error(w, r, err)
		return
	}

	items := make([]dto.UserResponse, 0, len(users))
	for _, u := range users {
		items = append(items, h.toUserResponse(r.Context(), u))
	}

	presenter.OK(w, r, dto.PagedUsersResponse{
		Items:                   items,
		Total:                   total,
		Page:                    page,
		PageSize:                pageSize,
		MustChangePasswordCount: mustChangePasswordCount,
	})
}

// GetUserByID handles GET /admin/users/:userId.
func (h *UserHandler) GetUserByID(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "userId"))
	if err != nil {
		presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "invalid user id"))
		return
	}

	u, err := h.svc.GetByID(r.Context(), id)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}

	presenter.OK(w, r, h.toUserResponse(r.Context(), u))
}

// CreateUser handles POST /admin/users — admin-only user creation.
func (h *UserHandler) CreateUser(w http.ResponseWriter, r *http.Request) {
	var req dto.CreateUserRequest
	if !middleware.BindJSON(w, r, &req) {
		return
	}
	if req.Username == "" || req.FullName == "" {
		presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "username and full_name are required"))
		return
	}
	if len(req.Password) < 8 {
		presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "password must be at least 8 characters"))
		return
	}
	email, err := normalizeEmail(req.Email)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}

	// See requireGrantableRole: naming a role that carries the universal
	// permission is a privilege grant, not a plain users.write action.
	if !h.requireGrantableRole(w, r, req.Role) {
		return
	}

	u, err := h.svc.Create(r.Context(), domainuser.CreateInput{
		Username:           req.Username,
		Password:           req.Password,
		FullName:           req.FullName,
		Email:              email,
		Role:               req.Role,
		MustChangePassword: true,
	})
	if err != nil {
		presenter.Error(w, r, err)
		return
	}

	presenter.Created(w, r, h.toUserResponse(r.Context(), u))
}

// AdminUpdateUser handles PATCH /admin/users/:userId — admin update of any user.
func (h *UserHandler) AdminUpdateUser(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "userId"))
	if err != nil {
		presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "invalid user id"))
		return
	}

	var req dto.AdminUpdateUserRequest
	if !middleware.BindJSON(w, r, &req) {
		return
	}
	email, err := normalizeEmail(req.Email)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}

	// See CreateUser's identical check — same role-name grant boundary, and
	// the same legacy users.role claim is written here too.
	if !h.requireGrantableRole(w, r, req.Role) {
		return
	}

	u, err := h.svc.AdminUpdate(r.Context(), id, domainuser.AdminUpdateInput{
		FullName: req.FullName,
		Role:     req.Role,
		Email:    email,
	})
	if err != nil {
		presenter.Error(w, r, err)
		return
	}

	presenter.OK(w, r, h.toUserResponse(r.Context(), u))
}

// DeleteUser handles DELETE /admin/users/:userId.
func (h *UserHandler) DeleteUser(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "userId"))
	if err != nil {
		presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "invalid user id"))
		return
	}

	if err := h.svc.Delete(r.Context(), id); err != nil {
		presenter.Error(w, r, err)
		return
	}

	presenter.NoContent(w)
}

// ResetPassword handles PATCH /admin/users/:userId/password — resets a user's password.
func (h *UserHandler) ResetPassword(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "userId"))
	if err != nil {
		presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "invalid user id"))
		return
	}

	var req dto.ResetPasswordRequest
	if !middleware.BindJSON(w, r, &req) {
		return
	}
	if len(req.NewPassword) < 8 {
		presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "new_password must be at least 8 characters"))
		return
	}

	if err := h.svc.ResetPassword(r.Context(), id, req.NewPassword); err != nil {
		presenter.Error(w, r, err)
		return
	}

	presenter.NoContent(w)
}

// ChangeMyPassword handles PATCH /users/me/password — lets a user change their own password.
// After a successful change the current session is revoked and the user must re-authenticate.
func (h *UserHandler) ChangeMyPassword(w http.ResponseWriter, r *http.Request) {
	claims := middleware.ClaimsFrom(r)
	if claims == nil {
		presenter.Error(w, r, apierr.New(apierr.CodeUnauthenticated, "unauthenticated"))
		return
	}

	id, err := uuid.Parse(claims.Subject)
	if err != nil {
		presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "invalid subject claim"))
		return
	}

	var req dto.ChangeMyPasswordRequest
	if !middleware.BindJSON(w, r, &req) {
		return
	}
	if req.CurrentPassword == "" {
		presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "current_password is required"))
		return
	}
	if len(req.NewPassword) < 8 {
		presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "new_password must be at least 8 characters"))
		return
	}

	if err := h.svc.ChangeMyPassword(r.Context(), id, req.CurrentPassword, req.NewPassword); err != nil {
		presenter.Error(w, r, err)
		return
	}

	// Revoke the current session so old tokens cannot be reused after the
	// password change. The client must re-authenticate with the new password.
	if h.authSvc != nil {
		if err := h.authSvc.Logout(r.Context(), claims.FamilyID); err != nil {
			presenter.Error(w, r, err)
			return
		}
	}

	presenter.NoContent(w)
}

// SetPassword handles POST /auth/password/set — the public, unauthenticated
// endpoint a password-set-link email points to. It never requires knowing
// the account's current/previous password, only a valid single-use token
// (see domainuser.Service.IssuePasswordSetToken).
func (h *UserHandler) SetPassword(w http.ResponseWriter, r *http.Request) {
	var req dto.SetPasswordRequest
	if !middleware.BindJSON(w, r, &req) {
		return
	}
	if len(req.NewPassword) < 8 {
		presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "new_password must be at least 8 characters"))
		return
	}

	if err := h.svc.SetPasswordWithToken(r.Context(), req.Token, req.NewPassword); err != nil {
		presenter.Error(w, r, err)
		return
	}

	presenter.NoContent(w)
}

// --- Avatar ------------------------------------------------------------

// InitiateAvatarUpload handles POST /users/me/avatar/initiate-upload.
func (h *UserHandler) InitiateAvatarUpload(w http.ResponseWriter, r *http.Request) {
	claims := middleware.ClaimsFrom(r)
	if claims == nil {
		presenter.Error(w, r, apierr.New(apierr.CodeUnauthenticated, "unauthenticated"))
		return
	}
	id, err := uuid.Parse(claims.Subject)
	if err != nil {
		presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "invalid subject claim"))
		return
	}

	var req dto.InitiateUploadRequest
	if !middleware.BindJSON(w, r, &req) {
		return
	}

	session, err := h.svc.InitiateAvatarUpload(r.Context(), id, req.FileName, req.ContentType, req.FileSize)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}

	presenter.Created(w, r, dto.UploadSessionFromDomain(session))
}

// CompleteAvatarUpload handles POST /users/me/avatar/complete-upload.
func (h *UserHandler) CompleteAvatarUpload(w http.ResponseWriter, r *http.Request) {
	claims := middleware.ClaimsFrom(r)
	if claims == nil {
		presenter.Error(w, r, apierr.New(apierr.CodeUnauthenticated, "unauthenticated"))
		return
	}
	id, err := uuid.Parse(claims.Subject)
	if err != nil {
		presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "invalid subject claim"))
		return
	}

	var req dto.CompleteAvatarUploadRequest
	if !middleware.BindJSON(w, r, &req) {
		return
	}

	u, err := h.svc.CompleteAvatarUpload(r.Context(), id, req.FileID)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}

	presenter.OK(w, r, h.toUserResponse(r.Context(), u))
}

// DeleteAvatar handles DELETE /users/me/avatar.
func (h *UserHandler) DeleteAvatar(w http.ResponseWriter, r *http.Request) {
	claims := middleware.ClaimsFrom(r)
	if claims == nil {
		presenter.Error(w, r, apierr.New(apierr.CodeUnauthenticated, "unauthenticated"))
		return
	}
	id, err := uuid.Parse(claims.Subject)
	if err != nil {
		presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "invalid subject claim"))
		return
	}

	u, err := h.svc.RemoveAvatar(r.Context(), id)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}

	presenter.OK(w, r, h.toUserResponse(r.Context(), u))
}
