package handler

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/paca/api/internal/apierr"
	domainauth "github.com/paca/api/internal/domain/auth"
	"github.com/paca/api/internal/transport/http/dto"
	"github.com/paca/api/internal/transport/http/middleware"
	"github.com/paca/api/internal/transport/http/presenter"
)

const (
	accessCookieName  = "access_token"
	refreshCookieName = "refresh_token"
	// refreshCookiePath restricts the refresh cookie to the rotation endpoint
	// so browsers never send it on regular API requests.
	refreshCookiePath = "/api/v1/auth/refresh"
)

// CookieConfig carries compile-time-safe settings for auth cookies.
type CookieConfig struct {
	Secure            bool
	AccessTTL         time.Duration
	RefreshTTL        time.Duration // persistent session (remember me = true)
	RefreshSessionTTL time.Duration // ephemeral session (remember me = false)
}

// AuthHandler handles authentication endpoints.
type AuthHandler struct {
	svc    domainauth.Service
	cookie CookieConfig
}

// NewAuthHandler returns an AuthHandler wired to the provided auth service.
func NewAuthHandler(svc domainauth.Service, cookie CookieConfig) *AuthHandler {
	return &AuthHandler{svc: svc, cookie: cookie}
}

// Login handles POST /auth/login.
// On success, access and refresh tokens are set as HttpOnly cookies; no token
// values appear in the response body.
func (h *AuthHandler) Login(c *gin.Context) {
	var req dto.LoginRequest
	if !middleware.BindJSON(c, &req) {
		return
	}

	pair, err := h.svc.Login(c.Request.Context(), req.Username, req.Password, req.RememberMe)
	if err != nil {
		presenter.Error(c, err)
		return
	}

	h.setTokenCookies(c, pair, pair.RefreshTTL)
	presenter.OK(c, gin.H{"message": "logged in"})
}

// Refresh handles POST /auth/refresh.
// The refresh token is read from the HttpOnly refresh_token cookie and, on
// success, a rotated token pair is written back as cookies.
func (h *AuthHandler) Refresh(c *gin.Context) {
	refreshToken, err := c.Cookie(refreshCookieName)
	if err != nil || refreshToken == "" {
		presenter.Error(c, apierr.New(apierr.CodeMissingToken, "missing refresh token"))
		return
	}

	pair, err := h.svc.Refresh(c.Request.Context(), refreshToken)
	if err != nil {
		presenter.Error(c, err)
		return
	}

	h.setTokenCookies(c, pair, pair.RefreshTTL)
	presenter.OK(c, gin.H{"message": "token refreshed"})
}

// Logout handles POST /auth/logout.  Requires an authenticated access token.
// Revokes the session family and clears both auth cookies.
func (h *AuthHandler) Logout(c *gin.Context) {
	claims := middleware.ClaimsFrom(c)
	if claims == nil {
		presenter.Error(c, apierr.New(apierr.CodeUnauthenticated, "unauthenticated"))
		return
	}

	if err := h.svc.Logout(c.Request.Context(), claims.FamilyID); err != nil {
		presenter.Error(c, err)
		return
	}

	h.clearCookies(c)
	presenter.OK(c, gin.H{"message": "logged out"})
}

// setTokenCookies writes both tokens into HttpOnly Set-Cookie headers.
// refreshTTL controls the MaxAge of the refresh cookie and should match the
// TTL embedded in the refresh JWT (see TokenPair.RefreshTTL).
func (h *AuthHandler) setTokenCookies(c *gin.Context, pair *domainauth.TokenPair, refreshTTL time.Duration) {
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     accessCookieName,
		Value:    pair.AccessToken,
		Path:     "/",
		HttpOnly: true,
		Secure:   h.cookie.Secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(h.cookie.AccessTTL.Seconds()),
	})
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     refreshCookieName,
		Value:    pair.RefreshToken,
		Path:     refreshCookiePath,
		HttpOnly: true,
		Secure:   h.cookie.Secure,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   int(refreshTTL.Seconds()),
	})
}

// clearCookies expires both auth cookies immediately.
func (h *AuthHandler) clearCookies(c *gin.Context) {
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     accessCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   h.cookie.Secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     refreshCookieName,
		Value:    "",
		Path:     refreshCookiePath,
		HttpOnly: true,
		Secure:   h.cookie.Secure,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   -1,
	})
}
