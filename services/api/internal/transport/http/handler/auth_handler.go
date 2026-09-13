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
	// schemeCookieName is portCookieName's scheme counterpart: the
	// forwarded preview page shares the Paca app's hostname but not
	// necessarily its scheme (e.g. a dev server the user runs with its own
	// local HTTPS cert, while Paca itself sits behind a plain-HTTP
	// self-hosted proxy, or vice versa) — apps/extension used to just
	// assume location.protocol matched, which broke silently whenever it
	// didn't. Same non-HttpOnly, same login/refresh cadence as
	// portCookieName, for the same reason.
	schemeCookieName = "paca_scheme"
	// annotationAccessCookieName/annotationRefreshCookieName carry a second,
	// domainauth.ScopeAnnotation-restricted token pair for the browser
	// extension specifically — see that constant's doc comment for why
	// access_token/refresh_token alone aren't reliably usable from a
	// forwarded preview page. Must stay in sync with the literal cookie
	// names middleware.applyAuthn falls back to reading.
	annotationAccessCookieName  = "annotation_access_token"
	annotationRefreshCookieName = "annotation_refresh_token"
	// annotationRefreshCookiePath mirrors refreshCookiePath's reasoning,
	// scoped to the annotation pair's own rotation endpoint instead.
	annotationRefreshCookiePath = "/api/v1/auth/annotation-refresh"
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
// On success, the main access/refresh tokens and the domainauth.
// ScopeAnnotation pair are all set as HttpOnly cookies (no token values
// appear in the response body), alongside the two client-readable
// paca_port/paca_scheme cookies (see portCookieName/schemeCookieName) —
// see setTokenCookies for the full set.
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

// AnnotationRefresh handles POST /auth/annotation-refresh. Mirrors Refresh,
// but reads/rotates only the domainauth.ScopeAnnotation pair the browser
// extension uses — see domainauth.Service.RefreshAnnotation's doc comment
// for why this is a fully separate endpoint rather than folded into
// Refresh.
func (h *AuthHandler) AnnotationRefresh(w http.ResponseWriter, r *http.Request) {
	refreshCookie, err := r.Cookie(annotationRefreshCookieName)
	if err != nil || refreshCookie.Value == "" {
		presenter.Error(w, r, apierr.New(apierr.CodeMissingToken, "missing annotation refresh token"))
		return
	}

	pair, err := h.svc.RefreshAnnotation(r.Context(), refreshCookie.Value)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}

	h.setAnnotationTokenCookies(w, pair.AnnotationAccessToken, pair.AnnotationRefreshToken, pair.RefreshTTL)
	presenter.OK(w, r, map[string]any{"message": "annotation token refreshed"})
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
		// Never Secure, regardless of h.cookie.Secure — deliberately not
		// the same knob the two auth cookies above use. This cookie's
		// whole job is to be readable via document.cookie from a
		// forwarded environment preview, which is very commonly plain
		// HTTP (a raw dev-server port, no TLS termination in that path)
		// even when the main Paca app itself sits behind real HTTPS
		// (COOKIE_SECURE=true). A Secure cookie is invisible to
		// document.cookie — and never attached to any request — on a
		// non-HTTPS page, full stop, regardless of SameSite or hostname
		// matching; inheriting h.cookie.Secure here would silently break
		// the extension on exactly that (common) combination. Carries no
		// token material, so there's no security reason to restrict it.
		Secure:   false,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(refreshTTL.Seconds()),
	})
	http.SetCookie(w, &http.Cookie{
		Name:     schemeCookieName,
		Value:    h.requestScheme(),
		Path:     "/",
		HttpOnly: false,
		// See portCookieName's cookie just above for why this is
		// unconditionally non-Secure too.
		Secure:   false,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(refreshTTL.Seconds()),
	})
	// Both Login and Refresh populate these now — see TokenPair's own doc
	// comment for why Refresh reissues rather than leaves them alone. The
	// guard just means a hypothetical future caller of setTokenCookies that
	// doesn't populate the annotation fields can't accidentally wipe out an
	// already-set annotation pair with empty values.
	if pair.AnnotationAccessToken != "" {
		h.setAnnotationTokenCookies(w, pair.AnnotationAccessToken, pair.AnnotationRefreshToken, refreshTTL)
	}
}

// setAnnotationTokenCookies writes the domainauth.ScopeAnnotation-restricted
// pair into their own HttpOnly cookies. Unlike every other cookie this
// handler sets, these use SameSite=None: the browser extension calls this
// API directly from a forwarded preview page that only shares a hostname
// with this one (see portCookieName's doc comment), often a different
// scheme too, which modern browsers treat as cross-site for
// SameSite=Lax/Strict purposes — see domainauth.ScopeAnnotation's doc
// comment. SameSite=None requires Secure, so on a COOKIE_SECURE=false
// deployment these two cookies are silently never stored at all (browsers
// drop a None cookie that isn't also Secure) — the correct, if unhelpful,
// degradation: without TLS on at least one side there's no way for a
// cross-scheme request to carry a cookie safely regardless of SameSite, and
// a plain-HTTP page fetching a plain-HTTP API on a different scheme-tagged
// origin was never actually the failure mode this pair fixes (an
// HTTPS-page-to-HTTP-API request is mixed content and gets blocked before
// any cookie is considered either way).
func (h *AuthHandler) setAnnotationTokenCookies(w http.ResponseWriter, accessToken, refreshToken string, refreshTTL time.Duration) {
	http.SetCookie(w, &http.Cookie{
		Name:     annotationAccessCookieName,
		Value:    accessToken,
		Path:     "/",
		HttpOnly: true,
		Secure:   h.cookie.Secure,
		SameSite: http.SameSiteNoneMode,
		MaxAge:   int(h.cookie.AccessTTL.Seconds()),
	})
	http.SetCookie(w, &http.Cookie{
		Name:     annotationRefreshCookieName,
		Value:    refreshToken,
		Path:     annotationRefreshCookiePath,
		HttpOnly: true,
		Secure:   h.cookie.Secure,
		SameSite: http.SameSiteNoneMode,
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

// requestScheme returns the scheme the Paca app is externally reachable on.
// Deliberately reads h.cookie.Secure rather than trying to detect the
// current request's own scheme (an earlier version checked X-Forwarded-Proto/
// r.TLS, mirroring requestPort's own reasoning) — that detection depends on
// every hop of whatever reverse proxy chain fronts this deployment
// correctly forwarding the header, which isn't this codebase's to control
// (e.g. Caddy's reverse_proxy recomputes X-Forwarded-Proto from its own
// connection by default, discarding a value an upstream Ingress already
// set correctly, unless separately configured not to). h.cookie.Secure has
// no such dependency: it's the operator's own COOKIE_SECURE setting, and it
// already has to correctly reflect whether the deployment is HTTPS or the
// access_token/refresh_token cookies above wouldn't work at all — so
// deriving paca_scheme from the same value costs nothing new and can't be
// broken by a proxy hop losing a header.
func (h *AuthHandler) requestScheme() string {
	if h.cookie.Secure {
		return "https"
	}
	return "http"
}

// clearCookies expires every cookie this handler ever sets.
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
		Secure:   false, // matches setTokenCookies — see that Set-Cookie's own comment
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
	http.SetCookie(w, &http.Cookie{
		Name:     schemeCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: false,
		Secure:   false, // matches setTokenCookies — see that Set-Cookie's own comment
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
	http.SetCookie(w, &http.Cookie{
		Name:     annotationAccessCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   h.cookie.Secure,
		SameSite: http.SameSiteNoneMode,
		MaxAge:   -1,
	})
	http.SetCookie(w, &http.Cookie{
		Name:     annotationRefreshCookieName,
		Value:    "",
		Path:     annotationRefreshCookiePath,
		HttpOnly: true,
		Secure:   h.cookie.Secure,
		SameSite: http.SameSiteNoneMode,
		MaxAge:   -1,
	})
}
