package handler

import (
	"net"
	"net/http"
	"time"

	"github.com/Paca-AI/api/internal/apierr"
	domainauth "github.com/Paca-AI/api/internal/domain/auth"
	"github.com/Paca-AI/api/internal/transport/http/dto"
	"github.com/Paca-AI/api/internal/transport/http/middleware"
	"github.com/Paca-AI/api/internal/transport/http/presenter"
)

const (
	accessCookieName  = "access_token"
	refreshCookieName = "refresh_token"
	// refreshCookiePath restricts the refresh cookie to the rotation endpoint
	// so browsers never send it on regular API requests.
	refreshCookiePath = "/api/v1/auth/refresh"
	// portCookieName carries no token material — just the port this
	// request reached the API on (see requestPort) — so unlike the two
	// above it's deliberately NOT HttpOnly: the Paca browser extension
	// (apps/extension) reads it via plain document.cookie, from a
	// completely different page (an environment's forwarded preview,
	// reachable on the same hostname but an arbitrary different port —
	// see corsMiddleware's own doc comment for the same-hostname fact that
	// makes a cookie set here visible there at all), to know which port to
	// call this API on even when Paca isn't running on 443/80. Set
	// alongside the real auth cookies purely so it rides the same
	// login/refresh cadence already happening for other reasons, not
	// because it's itself auth state.
	portCookieName = "paca_port"
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
// On success, access and refresh tokens are set as HttpOnly cookies (no
// token values appear in the response body), alongside the client-readable
// paca_port cookie (see portCookieName).
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

	h.setTokenCookies(w, r, pair, pair.RefreshTTL)
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

	h.setTokenCookies(w, r, pair, pair.RefreshTTL)
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

// setTokenCookies writes both tokens into HttpOnly Set-Cookie headers, plus
// the client-readable paca_port cookie alongside them (see portCookieName's
// own doc comment for why it travels with these two rather than being set
// some other way). refreshTTL controls the MaxAge of the refresh cookie
// and should match the TTL embedded in the refresh JWT (see
// TokenPair.RefreshTTL).
func (h *AuthHandler) setTokenCookies(w http.ResponseWriter, r *http.Request, pair *domainauth.TokenPair, refreshTTL time.Duration) {
	http.SetCookie(w, &http.Cookie{
		Name:     accessCookieName,
		Value:    pair.AccessToken,
		Path:     "/",
		HttpOnly: true,
		Secure:   h.cookie.Secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(h.cookie.AccessTTL.Seconds()),
	})
	http.SetCookie(w, &http.Cookie{
		Name:     refreshCookieName,
		Value:    pair.RefreshToken,
		Path:     refreshCookiePath,
		HttpOnly: true,
		Secure:   h.cookie.Secure,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   int(refreshTTL.Seconds()),
	})
	http.SetCookie(w, &http.Cookie{
		Name:     portCookieName,
		Value:    requestPort(r),
		Path:     "/",
		HttpOnly: false,
		Secure:   h.cookie.Secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(refreshTTL.Seconds()),
	})
}

// requestPort returns the port the client used to reach this request: the
// X-Forwarded-Port a reverse proxy set (preferred, since some proxies
// terminate TLS on one port and forward to the app on another), else
// whatever port is in the Host header the client actually sent, else the
// conventional default for the connection's scheme when neither carries
// one (a bare "example.com" always means 443 over TLS or 80 over plain
// HTTP).
func requestPort(r *http.Request) string {
	if p := r.Header.Get("X-Forwarded-Port"); p != "" {
		return p
	}
	if _, port, err := net.SplitHostPort(r.Host); err == nil {
		return port
	}
	if r.TLS != nil {
		return "443"
	}
	return "80"
}

// clearCookies expires all three cookies immediately.
func (h *AuthHandler) clearCookies(w http.ResponseWriter, _ *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     accessCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   h.cookie.Secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
	http.SetCookie(w, &http.Cookie{
		Name:     refreshCookieName,
		Value:    "",
		Path:     refreshCookiePath,
		HttpOnly: true,
		Secure:   h.cookie.Secure,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   -1,
	})
	http.SetCookie(w, &http.Cookie{
		Name:     portCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: false,
		Secure:   h.cookie.Secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
}
