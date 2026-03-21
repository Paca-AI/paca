package handler

import (
	"github.com/gin-gonic/gin"
	domainauth "github.com/paca/api/internal/domain/auth"
	"github.com/paca/api/internal/transport/http/dto"
	"github.com/paca/api/internal/transport/http/middleware"
	"github.com/paca/api/internal/transport/http/presenter"
)

// AuthHandler handles authentication endpoints.
type AuthHandler struct {
	svc domainauth.Service
}

// NewAuthHandler returns an AuthHandler wired to the provided auth service.
func NewAuthHandler(svc domainauth.Service) *AuthHandler {
	return &AuthHandler{svc: svc}
}

// Login handles POST /auth/login.
func (h *AuthHandler) Login(c *gin.Context) {
	var req dto.LoginRequest
	if !middleware.BindJSON(c, &req) {
		return
	}

	pair, err := h.svc.Login(c.Request.Context(), req.Email, req.Password)
	if err != nil {
		c.AbortWithStatusJSON(401, gin.H{"error": err.Error()})
		return
	}

	presenter.OK(c, dto.LoginResponse{
		AccessToken:  pair.AccessToken,
		RefreshToken: pair.RefreshToken,
	})
}

// Refresh handles POST /auth/refresh.
func (h *AuthHandler) Refresh(c *gin.Context) {
	var req dto.RefreshRequest
	if !middleware.BindJSON(c, &req) {
		return
	}

	access, err := h.svc.Refresh(c.Request.Context(), req.RefreshToken)
	if err != nil {
		c.AbortWithStatusJSON(401, gin.H{"error": err.Error()})
		return
	}

	presenter.OK(c, dto.RefreshResponse{AccessToken: access})
}

// Logout handles POST /auth/logout.  Requires an authenticated access token.
func (h *AuthHandler) Logout(c *gin.Context) {
	claims := middleware.ClaimsFrom(c)
	if claims == nil {
		c.AbortWithStatusJSON(401, gin.H{"error": "unauthenticated"})
		return
	}

	if err := h.svc.Logout(c.Request.Context(), claims.ID); err != nil {
		presenter.Error(c, err)
		return
	}

	presenter.OK(c, gin.H{"message": "logged out"})
}
