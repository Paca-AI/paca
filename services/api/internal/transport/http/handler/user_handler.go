package handler

import (
	"github.com/gin-gonic/gin"
	domainuser "github.com/paca/api/internal/domain/user"
	"github.com/paca/api/internal/transport/http/dto"
	"github.com/paca/api/internal/transport/http/middleware"
	"github.com/paca/api/internal/transport/http/presenter"

	"github.com/google/uuid"
)

// UserHandler handles user-related endpoints.
type UserHandler struct {
	svc domainuser.Service
}

// NewUserHandler returns a UserHandler wired to the provided user service.
func NewUserHandler(svc domainuser.Service) *UserHandler {
	return &UserHandler{svc: svc}
}

// Create handles POST /users.
func (h *UserHandler) Create(c *gin.Context) {
	var req dto.CreateUserRequest
	if !middleware.BindJSON(c, &req) {
		return
	}

	u, err := h.svc.Create(c.Request.Context(), domainuser.CreateInput{
		Email:    req.Email,
		Password: req.Password,
		Name:     req.Name,
	})
	if err != nil {
		presenter.Error(c, err)
		return
	}

	presenter.Created(c, dto.UserFromEntity(u))
}

// GetMe handles GET /users/me — returns the caller's own profile.
func (h *UserHandler) GetMe(c *gin.Context) {
	claims := middleware.ClaimsFrom(c)
	if claims == nil {
		c.AbortWithStatusJSON(401, gin.H{"error": "unauthenticated"})
		return
	}

	id, err := uuid.Parse(claims.Subject)
	if err != nil {
		c.AbortWithStatusJSON(400, gin.H{"error": "invalid subject claim"})
		return
	}

	u, err := h.svc.GetByID(c.Request.Context(), id)
	if err != nil {
		presenter.Error(c, err)
		return
	}

	presenter.OK(c, dto.UserFromEntity(u))
}

// Update handles PATCH /users/:id.
func (h *UserHandler) Update(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.AbortWithStatusJSON(400, gin.H{"error": "invalid user id"})
		return
	}

	var req dto.UpdateUserRequest
	if !middleware.BindJSON(c, &req) {
		return
	}

	u, err := h.svc.Update(c.Request.Context(), id, domainuser.UpdateInput{Name: req.Name})
	if err != nil {
		presenter.Error(c, err)
		return
	}

	presenter.OK(c, dto.UserFromEntity(u))
}

// Delete handles DELETE /users/:id.
func (h *UserHandler) Delete(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.AbortWithStatusJSON(400, gin.H{"error": "invalid user id"})
		return
	}

	if err := h.svc.Delete(c.Request.Context(), id); err != nil {
		presenter.Error(c, err)
		return
	}

	presenter.OK(c, gin.H{"message": "user deleted"})
}
