package handler

import (
	"net/http"
	"time"

	"github.com/Paca-AI/api/internal/apierr"
	"github.com/Paca-AI/api/internal/service/oidc"
	"github.com/Paca-AI/api/internal/transport/http/presenter"
)

// apierrSSOUnavailable is the generic error for a failed login initiation —
// never carries the underlying cause (which may include IdP details).
func apierrSSOUnavailable() error {
	return apierr.New(apierr.CodeSSOUnavailable, "single sign-on is unavailable")
}

const (
	// oidcStateCookieName binds a login transaction to the browser that
	// started it. Without this binding the server-side state alone would let
	// an attacker complete their own login transaction inside the victim's
	// browser by forwarding them the callback URL (login CSRF / session
	// swapping).
	oidcStateCookieName = "oidc_login_state"
	// oidcStateCookiePath scopes the cookie to the callback endpoint only —
	// it never travels with regular API requests.
	oidcStateCookiePath = "/api/v1/auth/oidc/callback"
	// oidcStateCookieMaxAge matches the server-side login transaction TTL
	// (oidc.loginTxTTL); the cookie outliving the transaction buys nothing.
	oidcStateCookieMaxAge = 10 * time.Minute
)

// OIDCHandler serves the browser-facing OIDC login endpoints. The SPA never
// participates in the OIDC flow: it simply navigates to /auth/oidc/login and
// the API completes everything server-side, ending with the same HttpOnly
// session cookies the password login writes.
type OIDCHandler struct {
	svc    oidc.LoginService
	cookie CookieConfig
	// webBaseURL is where the browser is sent after the callback — the web
	// app's base URL (or "/" when PUBLIC_URL is unset).
	webBaseURL string
}

// NewOIDCHandler returns an OIDCHandler for the given service.
func NewOIDCHandler(svc oidc.LoginService, cookie CookieConfig, webBaseURL string) *OIDCHandler {
	return &OIDCHandler{svc: svc, cookie: cookie, webBaseURL: webBaseURL}
}

// Login handles GET /auth/oidc/login — starts the Authorization Code + PKCE
// flow by redirecting the browser to the IdP's authorization endpoint. The
// random state is mirrored into a short-lived HttpOnly cookie bound to the
// callback path; the callback only honors a state whose cookie matches this
// browser, which is what prevents login CSRF.
func (h *OIDCHandler) Login(w http.ResponseWriter, r *http.Request) {
	authURL, state, err := h.svc.BeginLogin(r.Context())
	if err != nil {
		presenter.Error(w, r, apierrSSOUnavailable())
		return
	}
	// SameSite=Lax: the cookie must survive the top-level GET redirect back
	// from the (cross-site) IdP, which Lax allows, while staying absent from
	// cross-site subresource requests.
	http.SetCookie(w, &http.Cookie{
		Name:     oidcStateCookieName,
		Value:    state,
		Path:     oidcStateCookiePath,
		HttpOnly: true,
		Secure:   h.cookie.Secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(oidcStateCookieMaxAge.Seconds()),
	})
	// The IdP page is cross-origin; don't leak the callback URL (which
	// carries state) to its referrers.
	w.Header().Set("Referrer-Policy", "no-referrer")
	http.Redirect(w, r, authURL, http.StatusFound)
}

// Callback handles GET /auth/oidc/callback — completes the flow and lands
// the browser back in the web app with a fresh Paca session in cookies.
//
// The state cookie is cleared on every exit path (success, mismatch, IdP
// error) so a state value can never be retried in this browser.
//
// Error responses are deliberately generic and never carry provider details:
// the IdP's error parameters, the authorization code, or token endpoint
// responses must not be echoed back to the browser.
func (h *OIDCHandler) Callback(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Referrer-Policy", "no-referrer")
	h.clearStateCookie(w)

	q := r.URL.Query()
	if e := q.Get("error"); e != "" {
		// The IdP itself reported a failure (user denied consent, etc.) —
		// the error/error_description params stay out of any response.
		h.redirectError(w, r)
		return
	}
	code := q.Get("code")
	state := q.Get("state")
	if code == "" || state == "" {
		h.redirectError(w, r)
		return
	}

	// Browser binding: the callback is only honored when it arrives at the
	// browser that started the login (state cookie matches the state param).
	// A callback URL forwarded to — or forced upon — another browser fails
	// here, which is the login-CSRF defense.
	stateCookie, err := r.Cookie(oidcStateCookieName)
	if err != nil || stateCookie.Value == "" || stateCookie.Value != state {
		h.redirectError(w, r)
		return
	}

	pair, err := h.svc.Callback(r.Context(), code, state)
	if err != nil {
		h.redirectError(w, r)
		return
	}

	setAuthCookies(w, h.cookie, pair, pair.RefreshTTL)
	http.Redirect(w, r, h.webBaseURL, http.StatusFound)
}

// clearStateCookie expires the login-state cookie immediately.
func (h *OIDCHandler) clearStateCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     oidcStateCookieName,
		Value:    "",
		Path:     oidcStateCookiePath,
		HttpOnly: true,
		Secure:   h.cookie.Secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
}

// redirectError sends the browser home with a generic SSO failure flag.
func (h *OIDCHandler) redirectError(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, h.webBaseURL+"?sso_error=1", http.StatusFound)
}
