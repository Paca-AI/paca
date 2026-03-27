// Package dto defines request and response shapes for auth endpoints.
package dto

// LoginRequest is the body for POST /auth/login.
type LoginRequest struct {
	Username   string `json:"username" binding:"required"`
	Password   string `json:"password" binding:"required,min=8"`
	RememberMe bool   `json:"remember_me"`
}

// RefreshRequest is accepted but unused; the refresh token is read from the
// HttpOnly cookie instead. The body is ignored and this type is retained
// only for backwards-compatible API/schema compatibility.
type RefreshRequest struct{}
