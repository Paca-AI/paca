package handler

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/Paca-AI/api/internal/apierr"
	domainauth "github.com/Paca-AI/api/internal/domain/auth"
	ssodom "github.com/Paca-AI/api/internal/domain/sso"
	ssosvc "github.com/Paca-AI/api/internal/service/sso"
	"github.com/Paca-AI/api/internal/transport/http/dto"
	"github.com/Paca-AI/api/internal/transport/http/httpx"
	"github.com/Paca-AI/api/internal/transport/http/middleware"
	"github.com/Paca-AI/api/internal/transport/http/presenter"
)

const (
	// ssoStateCookieName binds an in-flight SSO sign-in to the browser that
	// started it (see ssodom.AuthAttempt.BrowserBinding). SameSite=Lax, not
	// Strict: the provider's redirect back to the callback is a cross-site
	// top-level navigation, which Strict would strip the cookie from.
	ssoStateCookieName = "sso_state"
	ssoStateCookiePath = "/api/v1/auth/sso"
	ssoStateMaxAge     = 600 // seconds; matches the state store TTL
)

// SSOService is the part of ssosvc.Service the handler uses.
type SSOService interface {
	ListProviders(ctx context.Context) ([]*ssodom.Provider, error)
	ListEnabledProviders(ctx context.Context) ([]*ssodom.Provider, error)
	CreateProvider(ctx context.Context, in ssodom.ProviderInput) (*ssodom.Provider, error)
	UpdateProvider(ctx context.Context, id uuid.UUID, in ssodom.ProviderInput) (*ssodom.Provider, error)
	DeleteProvider(ctx context.Context, id uuid.UUID) error
	BeginLogin(ctx context.Context, slug, baseURL string, rememberMe bool, redirect string) (authURL, binding string, err error)
	CompleteLogin(ctx context.Context, slug, baseURL, code, state, binding string) (*domainauth.TokenPair, string, error)
}

// SSOHandler serves SSO sign-in (public) and provider management (admin).
type SSOHandler struct {
	svc       SSOService
	auth      *AuthHandler
	publicURL string
}

// NewSSOHandler returns an SSOHandler. auth supplies the session cookies —
// the very same ones password login sets. publicURL is the deployment's
// external base URL (PUBLIC_URL); when empty it is derived per request.
func NewSSOHandler(svc SSOService, auth *AuthHandler, publicURL string) *SSOHandler {
	return &SSOHandler{svc: svc, auth: auth, publicURL: strings.TrimRight(publicURL, "/")}
}

// baseURL is the origin the provider redirects back to. PUBLIC_URL wins;
// otherwise the host the browser used, which is only right when no proxy
// rewrites Host — set PUBLIC_URL in production.
func (h *SSOHandler) baseURL(r *http.Request) string {
	if h.publicURL != "" {
		return h.publicURL
	}
	host := r.Header.Get("X-Forwarded-Host")
	if host == "" {
		host = r.Host
	}
	return h.auth.requestScheme() + "://" + host
}

// ListPublicProviders handles GET /auth/sso/providers — the enabled
// providers the login page offers. Public.
func (h *SSOHandler) ListPublicProviders(w http.ResponseWriter, r *http.Request) {
	ps, err := h.svc.ListEnabledProviders(r.Context())
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	out := make([]dto.SSOPublicProviderResponse, 0, len(ps))
	for _, p := range ps {
		out = append(out, dto.SSOPublicProviderResponse{Slug: p.Slug, DisplayName: p.DisplayName})
	}
	presenter.OK(w, r, out)
}

// Login handles GET /auth/sso/{slug}/login?remember_me=&redirect= — a
// top-level browser navigation that redirects to the provider.
func (h *SSOHandler) Login(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	authURL, binding, err := h.svc.BeginLogin(r.Context(), chi.URLParam(r, "slug"), h.baseURL(r),
		q.Get("remember_me") == "true", q.Get("redirect"))
	if err != nil {
		h.failRedirect(w, r, err)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     ssoStateCookieName,
		Value:    binding,
		Path:     ssoStateCookiePath,
		HttpOnly: true,
		Secure:   h.auth.cookie.Secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   ssoStateMaxAge,
	})
	http.Redirect(w, r, authURL, http.StatusFound)
}

// Callback handles GET /auth/sso/{slug}/callback — where the provider sends
// the browser back. On success it sets the session cookies and redirects
// into the app; on failure, to the login page with ?sso_error=<code>.
func (h *SSOHandler) Callback(w http.ResponseWriter, r *http.Request) {
	binding := ""
	if c, err := r.Cookie(ssoStateCookieName); err == nil {
		binding = c.Value
	}
	http.SetCookie(w, &http.Cookie{
		Name: ssoStateCookieName, Value: "", Path: ssoStateCookiePath,
		HttpOnly: true, Secure: h.auth.cookie.Secure, SameSite: http.SameSiteLaxMode, MaxAge: -1,
	})

	q := r.URL.Query()
	if q.Get("error") != "" {
		redirectToLogin(w, r, "provider_denied")
		return
	}
	pair, redirect, err := h.svc.CompleteLogin(r.Context(), chi.URLParam(r, "slug"), h.baseURL(r),
		q.Get("code"), q.Get("state"), binding)
	if err != nil {
		h.failRedirect(w, r, err)
		return
	}
	h.auth.setTokenCookies(w, r, pair, pair.RefreshTTL)
	http.Redirect(w, r, redirect, http.StatusFound)
}

func (h *SSOHandler) failRedirect(w http.ResponseWriter, r *http.Request, err error) {
	code := "internal"
	switch {
	case errors.Is(err, ssodom.ErrProviderNotFound):
		code = "provider_not_found"
	case errors.Is(err, ssodom.ErrDiscoveryFailed):
		code = "provider_unavailable"
	case errors.Is(err, ssodom.ErrStateInvalid):
		code = "state_invalid"
	case errors.Is(err, ssodom.ErrExchangeFailed):
		code = "exchange_failed"
	case errors.Is(err, ssodom.ErrEmailNotAllowed):
		code = "email_not_allowed"
	case errors.Is(err, ssodom.ErrNoAccount):
		code = "no_account"
	case errors.Is(err, ssodom.ErrAccountExists):
		code = "account_exists"
	default:
		slog.Error("sso sign-in failed", "error", err, "request_id", httpx.RequestIDFromContext(r.Context()))
	}
	redirectToLogin(w, r, code)
}

func redirectToLogin(w http.ResponseWriter, r *http.Request, code string) {
	http.Redirect(w, r, "/?sso_error="+url.QueryEscape(code), http.StatusFound)
}

// ListProviders handles GET /admin/sso/providers.
func (h *SSOHandler) ListProviders(w http.ResponseWriter, r *http.Request) {
	ps, err := h.svc.ListProviders(r.Context())
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	out := make([]dto.SSOProviderResponse, 0, len(ps))
	for _, p := range ps {
		out = append(out, h.toResponse(r, p))
	}
	presenter.OK(w, r, out)
}

// CreateProvider handles POST /admin/sso/providers.
func (h *SSOHandler) CreateProvider(w http.ResponseWriter, r *http.Request) {
	var req dto.SSOProviderRequest
	if !middleware.BindJSON(w, r, &req) {
		return
	}
	p, err := h.svc.CreateProvider(r.Context(), req.ToInput())
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.Created(w, r, h.toResponse(r, p))
}

// UpdateProvider handles PUT /admin/sso/providers/{providerId}.
func (h *SSOHandler) UpdateProvider(w http.ResponseWriter, r *http.Request) {
	id, ok := parseProviderID(w, r)
	if !ok {
		return
	}
	var req dto.SSOProviderRequest
	if !middleware.BindJSON(w, r, &req) {
		return
	}
	p, err := h.svc.UpdateProvider(r.Context(), id, req.ToInput())
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.OK(w, r, h.toResponse(r, p))
}

// DeleteProvider handles DELETE /admin/sso/providers/{providerId}.
func (h *SSOHandler) DeleteProvider(w http.ResponseWriter, r *http.Request) {
	id, ok := parseProviderID(w, r)
	if !ok {
		return
	}
	if err := h.svc.DeleteProvider(r.Context(), id); err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.NoContent(w)
}

func (h *SSOHandler) toResponse(r *http.Request, p *ssodom.Provider) dto.SSOProviderResponse {
	return dto.SSOProviderFromDomain(p, h.baseURL(r)+ssosvc.CallbackPath(p.Slug))
}

func parseProviderID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, "providerId"))
	if err != nil {
		presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "invalid provider id"))
		return uuid.Nil, false
	}
	return id, true
}
