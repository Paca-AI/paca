package handler

import (
	"context"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/paca/api/internal/apierr"
	domainuser "github.com/paca/api/internal/domain/user"
	"github.com/paca/api/internal/transport/http/dto"
	"github.com/paca/api/internal/transport/http/middleware"
	"github.com/paca/api/internal/transport/http/presenter"
)

// SessionInvalidator revokes an authentication session by family ID.
// It is satisfied by domain/auth.Service.
type SessionInvalidator interface {
	Logout(ctx context.Context, familyID string) error
}

// UserHandler handles user-related endpoints.
type UserHandler struct {
	svc     domainuser.Service
	authSvc SessionInvalidator
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

// --- Self-service routes ---------------------------------------------------

// GetMe handles GET /users/me — returns the caller's own profile.
func (h *UserHandler) GetMe(c *gin.Context) {
	claims := middleware.ClaimsFrom(c)
	if claims == nil {
		presenter.Error(c, apierr.New(apierr.CodeUnauthenticated, "unauthenticated"))
		return
	}

	id, err := uuid.Parse(claims.Subject)
	if err != nil {
		presenter.Error(c, apierr.New(apierr.CodeBadRequest, "invalid subject claim"))
		return
	}

	u, err := h.svc.GetByID(c.Request.Context(), id)
	if err != nil {
		presenter.Error(c, err)
		return
	}

	presenter.OK(c, dto.UserFromEntity(u))
}

// UpdateMe handles PATCH /users/me — lets users update their own profile.
func (h *UserHandler) UpdateMe(c *gin.Context) {
	claims := middleware.ClaimsFrom(c)
	if claims == nil {
		presenter.Error(c, apierr.New(apierr.CodeUnauthenticated, "unauthenticated"))
		return
	}

	id, err := uuid.Parse(claims.Subject)
	if err != nil {
		presenter.Error(c, apierr.New(apierr.CodeBadRequest, "invalid subject claim"))
		return
	}

	var req dto.UpdateProfileRequest
	if !middleware.BindJSON(c, &req) {
		return
	}

	u, err := h.svc.UpdateProfile(c.Request.Context(), id, domainuser.UpdateProfileInput{
		FullName: req.FullName,
	})
	if err != nil {
		presenter.Error(c, err)
		return
	}

	presenter.OK(c, dto.UserFromEntity(u))
}

// GetMyGlobalPermissions handles GET /users/me/global-permissions.
func (h *UserHandler) GetMyGlobalPermissions(c *gin.Context) {
	claims := middleware.ClaimsFrom(c)
	if claims == nil {
		presenter.Error(c, apierr.New(apierr.CodeUnauthenticated, "unauthenticated"))
		return
	}

	id, err := uuid.Parse(claims.Subject)
	if err != nil {
		presenter.Error(c, apierr.New(apierr.CodeBadRequest, "invalid subject claim"))
		return
	}

	permissions, err := h.svc.ListGlobalPermissions(c.Request.Context(), id)
	if err != nil {
		presenter.Error(c, err)
		return
	}

	presenter.OK(c, gin.H{"permissions": permissions})
}

// --- Admin user management routes ------------------------------------------

// ListUsers handles GET /admin/users — returns a paginated list of all users.
func (h *UserHandler) ListUsers(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))

	users, total, err := h.svc.List(c.Request.Context(), page, pageSize)
	if err != nil {
		presenter.Error(c, err)
		return
	}

	items := make([]dto.UserResponse, 0, len(users))
	for _, u := range users {
		items = append(items, dto.UserFromEntity(u))
	}

	presenter.OK(c, dto.PagedUsersResponse{
		Items:    items,
		Total:    total,
		Page:     page,
		PageSize: pageSize,
	})
}

// GetUserByID handles GET /admin/users/:userId.
func (h *UserHandler) GetUserByID(c *gin.Context) {
	id, err := uuid.Parse(c.Param("userId"))
	if err != nil {
		presenter.Error(c, apierr.New(apierr.CodeBadRequest, "invalid user id"))
		return
	}

	u, err := h.svc.GetByID(c.Request.Context(), id)
	if err != nil {
		presenter.Error(c, err)
		return
	}

	presenter.OK(c, dto.UserFromEntity(u))
}

// CreateUser handles POST /admin/users — admin-only user creation.
func (h *UserHandler) CreateUser(c *gin.Context) {
	var req dto.CreateUserRequest
	if !middleware.BindJSON(c, &req) {
		return
	}

	u, err := h.svc.Create(c.Request.Context(), domainuser.CreateInput{
		Username:           req.Username,
		Password:           req.Password,
		FullName:           req.FullName,
		Role:               req.Role,
		MustChangePassword: true,
	})
	if err != nil {
		presenter.Error(c, err)
		return
	}

	presenter.Created(c, dto.UserFromEntity(u))
}

// AdminUpdateUser handles PATCH /admin/users/:userId — admin update of any user.
func (h *UserHandler) AdminUpdateUser(c *gin.Context) {
	id, err := uuid.Parse(c.Param("userId"))
	if err != nil {
		presenter.Error(c, apierr.New(apierr.CodeBadRequest, "invalid user id"))
		return
	}

	var req dto.AdminUpdateUserRequest
	if !middleware.BindJSON(c, &req) {
		return
	}

	u, err := h.svc.AdminUpdate(c.Request.Context(), id, domainuser.AdminUpdateInput{
		FullName: req.FullName,
		Role:     req.Role,
	})
	if err != nil {
		presenter.Error(c, err)
		return
	}

	presenter.OK(c, dto.UserFromEntity(u))
}

// DeleteUser handles DELETE /admin/users/:userId.
func (h *UserHandler) DeleteUser(c *gin.Context) {
	id, err := uuid.Parse(c.Param("userId"))
	if err != nil {
		presenter.Error(c, apierr.New(apierr.CodeBadRequest, "invalid user id"))
		return
	}

	if err := h.svc.Delete(c.Request.Context(), id); err != nil {
		presenter.Error(c, err)
		return
	}

	presenter.NoContent(c)
}

// ResetPassword handles PATCH /admin/users/:userId/password — resets a user's password.
func (h *UserHandler) ResetPassword(c *gin.Context) {
	id, err := uuid.Parse(c.Param("userId"))
	if err != nil {
		presenter.Error(c, apierr.New(apierr.CodeBadRequest, "invalid user id"))
		return
	}

	var req dto.ResetPasswordRequest
	if !middleware.BindJSON(c, &req) {
		return
	}

	if err := h.svc.ResetPassword(c.Request.Context(), id, req.NewPassword); err != nil {
		presenter.Error(c, err)
		return
	}

	presenter.NoContent(c)
}

// ChangeMyPassword handles PATCH /users/me/password — lets a user change their own password.
// After a successful change the current session is revoked and the user must re-authenticate.
func (h *UserHandler) ChangeMyPassword(c *gin.Context) {
	claims := middleware.ClaimsFrom(c)
	if claims == nil {
		presenter.Error(c, apierr.New(apierr.CodeUnauthenticated, "unauthenticated"))
		return
	}

	id, err := uuid.Parse(claims.Subject)
	if err != nil {
		presenter.Error(c, apierr.New(apierr.CodeBadRequest, "invalid subject claim"))
		return
	}

	var req dto.ChangeMyPasswordRequest
	if !middleware.BindJSON(c, &req) {
		return
	}

	if err := h.svc.ChangeMyPassword(c.Request.Context(), id, req.CurrentPassword, req.NewPassword); err != nil {
		presenter.Error(c, err)
		return
	}

	// Revoke the current session so old tokens cannot be reused after the
	// password change. The client must re-authenticate with the new password.
	if h.authSvc != nil {
		_ = h.authSvc.Logout(c.Request.Context(), claims.FamilyID)
	}

	presenter.NoContent(c)
}
