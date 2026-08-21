package handler

import (
	"net/http"

	"github.com/Paca-AI/api/internal/apierr"
	"github.com/Paca-AI/api/internal/service/oidc"
	"github.com/Paca-AI/api/internal/transport/http/presenter"
)

// apierrSSOUnavailable is the generic error for a failed login initiation —
// never carries the underlying cause (which may include IdP details).
func apierrSSOUnavailable() error {
	return apierr.New(apierr.CodeSSOUnavailable, "single sign-on is unavailable")
}

// OIDCHandler serves the browser-facing OIDC login endpoints. The SPA never
// participates in the OIDC flow: it simply navigates to /auth/oidc/login and
// the API completes everything server-side, ending with the same HttpOnly
// session cookies the password login writes.
type OIDCHandler struct {
	svc    *oidc.Service
	cookie CookieConfig
	// webBaseURL is where the browser is sent after the callback — the web
	// app's base URL (or "/" when PUBLIC_URL is unset).
	webBaseURL string
}

// NewOIDCHandler returns an OIDCHandler for the given service.
func NewOIDCHandler(svc *oidc.Service, cookie CookieConfig, webBaseURL string) *OIDCHandler {
	return &OIDCHandler{svc: svc, cookie: cookie, webBaseURL: webBaseURL}
}

// Login handles GET /auth/oidc/login — starts the Authorization Code + PKCE
// flow by redirecting the browser to the IdP's authorization endpoint.
func (h *OIDCHandler) Login(w http.ResponseWriter, r *http.Request) {
	authURL, err := h.svc.BeginLogin(r.Context())
	if err != nil {
		presenter.Error(w, r, apierrSSOUnavailable())
		return
	}
	// The IdP page is cross-origin; don't leak the callback URL (which
	// carries state) to its referrers.
	w.Header().Set("Referrer-Policy", "no-referrer")
	http.Redirect(w, r, authURL, http.StatusFound)
}

// Callback handles GET /auth/oidc/callback — completes the flow and lands
// the browser back in the web app with a fresh Paca session in cookies.
//
// Error responses are deliberately generic and never carry provider details:
// the IdP's error parameters, the authorization code, or token endpoint
// responses must not be echoed back to the browser.
func (h *OIDCHandler) Callback(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Referrer-Policy", "no-referrer")

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

	pair, err := h.svc.Callback(r.Context(), code, state)
	if err != nil {
		h.redirectError(w, r)
		return
	}

	setAuthCookies(w, h.cookie, pair, pair.RefreshTTL)
	http.Redirect(w, r, h.webBaseURL, http.StatusFound)
}

// redirectError sends the browser home with a generic SSO failure flag.
func (h *OIDCHandler) redirectError(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, h.webBaseURL+"?sso_error=1", http.StatusFound)
}
