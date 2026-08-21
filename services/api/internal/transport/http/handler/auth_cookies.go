package handler

import (
	"net/http"
	"time"

	domainauth "github.com/Paca-AI/api/internal/domain/auth"
)

const (
	accessCookieName  = "access_token"
	refreshCookieName = "refresh_token"
	// refreshCookiePath restricts the refresh cookie to the rotation endpoint
	// so browsers never send it on regular API requests.
	refreshCookiePath = "/api/v1/auth/refresh"
)

// setAuthCookies writes both tokens into HttpOnly Set-Cookie headers.
// refreshTTL controls the MaxAge of the refresh cookie and should match the
// TTL embedded in the refresh JWT (see TokenPair.RefreshTTL). Shared by the
// password-login and OIDC-callback paths so both write identical cookies.
func setAuthCookies(w http.ResponseWriter, cfg CookieConfig, pair *domainauth.TokenPair, refreshTTL time.Duration) {
	http.SetCookie(w, &http.Cookie{
		Name:     accessCookieName,
		Value:    pair.AccessToken,
		Path:     "/",
		HttpOnly: true,
		Secure:   cfg.Secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(cfg.AccessTTL.Seconds()),
	})
	http.SetCookie(w, &http.Cookie{
		Name:     refreshCookieName,
		Value:    pair.RefreshToken,
		Path:     refreshCookiePath,
		HttpOnly: true,
		Secure:   cfg.Secure,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   int(refreshTTL.Seconds()),
	})
}

// clearAuthCookies expires both auth cookies immediately.
func clearAuthCookies(w http.ResponseWriter, cfg CookieConfig) {
	http.SetCookie(w, &http.Cookie{
		Name:     accessCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   cfg.Secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
	http.SetCookie(w, &http.Cookie{
		Name:     refreshCookieName,
		Value:    "",
		Path:     refreshCookiePath,
		HttpOnly: true,
		Secure:   cfg.Secure,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   -1,
	})
}
