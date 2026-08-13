package handler

import (
	"context"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/Paca-AI/api/internal/apierr"
	attachmentdom "github.com/Paca-AI/api/internal/domain/attachment"
	domainuser "github.com/Paca-AI/api/internal/domain/user"
	"github.com/Paca-AI/api/internal/transport/http/dto"
	"github.com/Paca-AI/api/internal/transport/http/middleware"
	"github.com/Paca-AI/api/internal/transport/http/presenter"
)

// SessionInvalidator revokes an authentication session by family ID.
// It is satisfied by domain/auth.Service.
type SessionInvalidator interface {
	Logout(ctx context.Context, familyID string) error
}

// UserNotifier delivers credential e-mails on user lifecycle events.
// Both methods are best-effort: they return (false, nil) when e-mail sending
// is disabled or unconfigured, so the caller can invoke them unconditionally.
// It is satisfied by service/email.Service.
type UserNotifier interface {
	NotifyUserCreated(ctx context.Context, to, username, password string) (bool, error)
	NotifyPasswordReset(ctx context.Context, to, username, password string) (bool, error)
}

// UserHandler handles user-related endpoints.
type UserHandler struct {
	svc       domainuser.Service
	authSvc   SessionInvalidator
	avatarSvc attachmentdom.AvatarService
	notifier  UserNotifier
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

// WithNotifier enables credential e-mails on user creation / password reset.
func (h *UserHandler) WithNotifier(n UserNotifier) *UserHandler {
	h.notifier = n
	return h
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

	u, err := h.svc.UpdateProfile(r.Context(), id, domainuser.UpdateProfileInput{
		FullName: req.FullName,
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

	items := make([]dto.UserResponse, 0, len(users))
	for _, u := range users {
		items = append(items, h.toUserResponse(r.Context(), u))
	}

	presenter.OK(w, r, dto.PagedUsersResponse{
		Items:    items,
		Total:    total,
		Page:     page,
		PageSize: pageSize,
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
	if !looksLikeEmail(req.Email) {
		presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "a valid email is required"))
		return
	}
	if len(req.Password) < 8 {
		presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "password must be at least 8 characters"))
		return
	}

	emailPtr := req.Email
	u, err := h.svc.Create(r.Context(), domainuser.CreateInput{
		Username:           req.Username,
		Password:           req.Password,
		FullName:           req.FullName,
		Email:              &emailPtr,
		Role:               req.Role,
		MustChangePassword: true,
	})
	if err != nil {
		presenter.Error(w, r, err)
		return
	}

	resp := h.toUserResponse(r.Context(), u)
	resp.EmailSent = h.deliverCredentials(r, req.Email, u.Username, req.Password, true)
	presenter.Created(w, r, resp)
}

// deliverCredentials best-effort e-mails credentials to the user. It never
// fails the request: a nil return means no attempt (no notifier), otherwise
// the pointed-to bool reports whether the send succeeded. Errors are logged.
func (h *UserHandler) deliverCredentials(r *http.Request, to, username, password string, created bool) *bool {
	if h.notifier == nil {
		return nil
	}
	var sent bool
	var err error
	if created {
		sent, err = h.notifier.NotifyUserCreated(r.Context(), to, username, password)
	} else {
		sent, err = h.notifier.NotifyPasswordReset(r.Context(), to, username, password)
	}
	if err != nil {
		slog.WarnContext(r.Context(), "credential email failed", "user", username, "created", created, "error", err)
		failed := false
		return &failed
	}
	return &sent
}

// looksLikeEmail is a minimal sanity check (presence of "@" with text on both
// sides). Full RFC validation lives in the binding layer; this guards the
// handler path.
func looksLikeEmail(s string) bool {
	at := strings.IndexByte(s, '@')
	return at > 0 && at < len(s)-1
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

	u, err := h.svc.AdminUpdate(r.Context(), id, domainuser.AdminUpdateInput{
		FullName: req.FullName,
		Role:     req.Role,
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

	// Best-effort: e-mail the new credentials if e-mail sending is enabled
	// and the user has an address on file. Never fails the reset.
	if h.notifier != nil {
		if u, gErr := h.svc.GetByID(r.Context(), id); gErr == nil {
			to := ""
			if u.Email != nil {
				to = *u.Email
			}
			h.deliverCredentials(r, to, u.Username, req.NewPassword, false)
		}
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
