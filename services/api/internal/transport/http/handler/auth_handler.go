package handler

import (
	"net/http"
	"time"

	"github.com/Paca-AI/api/internal/apierr"
	domainauth "github.com/Paca-AI/api/internal/domain/auth"
	"github.com/Paca-AI/api/internal/transport/http/dto"
	"github.com/Paca-AI/api/internal/transport/http/middleware"
	"github.com/Paca-AI/api/internal/transport/http/presenter"
)

// CookieConfig carries compile-time-safe settings for auth cookies.
type CookieConfig struct {
	Secure            bool
	AccessTTL         time.Duration
	RefreshTTL        time.Duration // persistent session (remember me = true)
	RefreshSessionTTL time.Duration // ephemeral session (remember me = false)
}

// LoginOptions tells the login page which entry points exist. It mirrors the
// instance's OIDC/local-login configuration for the public /auth/config
// endpoint — display data only, never credentials or IdP metadata.
type LoginOptions struct {
	LocalEnabled    bool
	OIDCEnabled     bool
	OIDCDisplayName string
}

// AuthHandler handles authentication endpoints.
type AuthHandler struct {
	svc    domainauth.Service
	cookie CookieConfig
	login  LoginOptions
}

// NewAuthHandler returns an AuthHandler wired to the provided auth service.
func NewAuthHandler(svc domainauth.Service, cookie CookieConfig) *AuthHandler {
	// Defaults keep every existing caller (and the local-only deployment)
	// working unchanged: local login shown, SSO hidden.
	return &AuthHandler{svc: svc, cookie: cookie, login: LoginOptions{LocalEnabled: true}}
}

// WithLoginOptions overrides which login entry points the public /auth/config
// endpoint advertises (SSO enabled/disabled, local login shown/hidden).
func (h *AuthHandler) WithLoginOptions(opts LoginOptions) *AuthHandler {
	h.login = opts
	return h
}

// Config handles GET /auth/config — the public, unauthenticated endpoint the
// login page reads to decide which entry points to render. Exposes only
// display data; client secrets, issuer metadata and endpoints stay internal.
func (h *AuthHandler) Config(w http.ResponseWriter, r *http.Request) {
	resp := map[string]any{
		"local_login_enabled": h.login.LocalEnabled,
		"oidc": map[string]any{
			"enabled":      h.login.OIDCEnabled,
			"display_name": h.login.OIDCDisplayName,
		},
	}
	presenter.OK(w, r, resp)
}

// Login handles POST /auth/login.
// On success, access and refresh tokens are set as HttpOnly cookies; no token
// values appear in the response body.
func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	var req dto.LoginRequest
	if !middleware.BindJSON(w, r, &req) {
		return
	}

	if req.Username == "" {
		presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "username is required"))
		return
	}
	if len(req.Password) < 8 {
		presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "password must be at least 8 characters"))
		return
	}

	pair, err := h.svc.Login(r.Context(), req.Username, req.Password, req.RememberMe)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}

	h.setTokenCookies(w, pair, pair.RefreshTTL)
	presenter.OK(w, r, map[string]any{"message": "logged in"})
}

// Refresh handles POST /auth/refresh.
// The refresh token is read from the HttpOnly refresh_token cookie and, on
// success, a rotated token pair is written back as cookies.
func (h *AuthHandler) Refresh(w http.ResponseWriter, r *http.Request) {
	refreshCookie, err := r.Cookie(refreshCookieName)
	if err != nil || refreshCookie.Value == "" {
		presenter.Error(w, r, apierr.New(apierr.CodeMissingToken, "missing refresh token"))
		return
	}

	pair, err := h.svc.Refresh(r.Context(), refreshCookie.Value)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}

	h.setTokenCookies(w, pair, pair.RefreshTTL)
	presenter.OK(w, r, map[string]any{"message": "token refreshed"})
}

// Logout handles POST /auth/logout.  Requires an authenticated access token.
// Revokes the session family and clears both auth cookies.
func (h *AuthHandler) Logout(w http.ResponseWriter, r *http.Request) {
	claims := middleware.ClaimsFrom(r)
	if claims == nil {
		presenter.Error(w, r, apierr.New(apierr.CodeUnauthenticated, "unauthenticated"))
		return
	}

	if err := h.svc.Logout(r.Context(), claims.FamilyID); err != nil {
		presenter.Error(w, r, err)
		return
	}

	h.clearCookies(w, r)
	presenter.OK(w, r, map[string]any{"message": "logged out"})
}

// setTokenCookies writes both tokens into HttpOnly Set-Cookie headers (see
// setAuthCookies, shared with the OIDC callback handler).
func (h *AuthHandler) setTokenCookies(w http.ResponseWriter, pair *domainauth.TokenPair, refreshTTL time.Duration) {
	setAuthCookies(w, h.cookie, pair, refreshTTL)
}

// clearCookies expires both auth cookies immediately.
func (h *AuthHandler) clearCookies(w http.ResponseWriter, _ *http.Request) {
	clearAuthCookies(w, h.cookie)
}
