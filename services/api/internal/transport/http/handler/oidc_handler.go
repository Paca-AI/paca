package handler

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Paca-AI/api/internal/apierr"
	domainauth "github.com/Paca-AI/api/internal/domain/auth"
	userdom "github.com/Paca-AI/api/internal/domain/user"
	"github.com/Paca-AI/api/internal/platform/oidc"
	"github.com/Paca-AI/api/internal/service/galaxyauth"
	"github.com/Paca-AI/api/internal/transport/http/presenter"
)

const (
	// oidcStateCookieName holds the HMAC-signed state+PKCE payload between
	// the login redirect and the issuer callback.
	oidcStateCookieName = "oidc_state"
	// oidcStateCookiePath scopes the cookie to the OIDC endpoints only.
	oidcStateCookiePath = "/api/v1/auth/oidc"
	// oidcStateTTL bounds how long a login attempt may take.
	oidcStateTTL = 10 * time.Minute
)

// OIDCOptions carries the OIDC client settings the handler needs (a transport
// mirror of config.OIDCConfig, kept separate so this package does not import
// the config package).
type OIDCOptions struct {
	ClientID     string
	ClientSecret string
	RedirectURL  string
	Scopes       string
}

// SessionIssuer mints a session token pair for an already-authenticated user.
// Satisfied by the auth service so OIDC logins reuse the exact same token
// issuance path as password logins.
type SessionIssuer interface {
	IssueSession(ctx context.Context, u *userdom.User, rememberMe bool) (*domainauth.TokenPair, error)
}

// OIDCUserResolver maps a verified OIDC identity to a local user account.
type OIDCUserResolver interface {
	ResolveOIDCUser(ctx context.Context, id galaxyauth.Identity) (*userdom.User, error)
}

// OIDCHandler implements the Vortex SSO login endpoints (ADR-038):
// GET /auth/oidc/login and GET /auth/oidc/callback.
type OIDCHandler struct {
	provider    *oidc.Provider
	opts        OIDCOptions
	users       OIDCUserResolver
	sessions    SessionIssuer
	auth        *AuthHandler // reused for session cookie writing
	stateSecret []byte
	log         *slog.Logger
}

// NewOIDCHandler returns an OIDCHandler.  stateSecret signs the short-lived
// state cookie; the JWT secret is reused for this purpose.
func NewOIDCHandler(provider *oidc.Provider, opts OIDCOptions, users OIDCUserResolver, sessions SessionIssuer, auth *AuthHandler, stateSecret []byte, log *slog.Logger) *OIDCHandler {
	return &OIDCHandler{
		provider:    provider,
		opts:        opts,
		users:       users,
		sessions:    sessions,
		auth:        auth,
		stateSecret: stateSecret,
		log:         log,
	}
}

// Login handles GET /auth/oidc/login: it stores state + PKCE verifier in a
// signed HttpOnly cookie and redirects the browser to the issuer's
// authorization endpoint.
func (h *OIDCHandler) Login(w http.ResponseWriter, r *http.Request) {
	disc, err := h.provider.Discover(r.Context())
	if err != nil {
		h.log.Error("oidc: discovery failed", "error", err)
		presenter.Error(w, r, apierr.New(apierr.CodeInternalError, "identity provider unavailable"))
		return
	}

	state, err := oidc.RandomToken(32)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	verifier, err := oidc.RandomToken(32)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}

	cookieVal, err := oidc.EncodeLoginState(h.stateSecret, oidc.LoginState{
		State:     state,
		Verifier:  verifier,
		ExpiresAt: time.Now().Add(oidcStateTTL).Unix(),
	})
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     oidcStateCookieName,
		Value:    cookieVal,
		Path:     oidcStateCookiePath,
		HttpOnly: true,
		Secure:   h.auth.cookie.Secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(oidcStateTTL.Seconds()),
	})

	q := url.Values{}
	q.Set("response_type", "code")
	q.Set("client_id", h.opts.ClientID)
	q.Set("redirect_uri", h.opts.RedirectURL)
	q.Set("scope", h.opts.Scopes)
	q.Set("state", state)
	q.Set("code_challenge", oidc.PKCEChallengeS256(verifier))
	q.Set("code_challenge_method", "S256")

	sep := "?"
	if strings.Contains(disc.AuthorizationEndpoint, "?") {
		sep = "&"
	}
	http.Redirect(w, r, disc.AuthorizationEndpoint+sep+q.Encode(), http.StatusFound)
}

// Callback handles GET /auth/oidc/callback: it validates state, exchanges the
// code (client_secret_post + PKCE), verifies the RS256 id_token against the
// issuer JWKS, resolves the local user, and issues the same session cookies
// as password login before redirecting to the SPA.
func (h *OIDCHandler) Callback(w http.ResponseWriter, r *http.Request) {
	if errParam := r.URL.Query().Get("error"); errParam != "" {
		h.log.Warn("oidc: issuer returned error", "error", errParam, "description", r.URL.Query().Get("error_description"))
		presenter.Error(w, r, apierr.New(apierr.CodeUnauthenticated, "identity provider rejected the login"))
		return
	}

	code := r.URL.Query().Get("code")
	stateParam := r.URL.Query().Get("state")
	if code == "" || stateParam == "" {
		presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "missing code or state"))
		return
	}

	stateCookie, err := r.Cookie(oidcStateCookieName)
	if err != nil || stateCookie.Value == "" {
		presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "missing login state cookie — restart the login flow"))
		return
	}
	h.clearStateCookie(w)

	loginState, err := oidc.DecodeLoginState(h.stateSecret, stateCookie.Value)
	if err != nil {
		h.log.Warn("oidc: state cookie rejected", "error", err)
		presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "invalid login state — restart the login flow"))
		return
	}
	if subtle.ConstantTimeCompare([]byte(loginState.State), []byte(stateParam)) != 1 {
		presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "state mismatch — restart the login flow"))
		return
	}

	idToken, err := h.exchangeCode(r.Context(), code, loginState.Verifier)
	if err != nil {
		h.log.Error("oidc: code exchange failed", "error", err)
		presenter.Error(w, r, apierr.New(apierr.CodeUnauthenticated, "token exchange with identity provider failed"))
		return
	}

	claims, err := h.provider.VerifyToken(r.Context(), idToken, h.opts.ClientID)
	if err != nil {
		h.log.Warn("oidc: id_token rejected", "error", err)
		presenter.Error(w, r, apierr.New(apierr.CodeTokenInvalid, "invalid id_token"))
		return
	}

	identity := galaxyauth.Identity{
		Subject:           stringClaim(claims, "sub"),
		Email:             stringClaim(claims, "email"),
		Name:              stringClaim(claims, "name"),
		PreferredUsername: stringClaim(claims, "preferred_username"),
	}

	user, err := h.users.ResolveOIDCUser(r.Context(), identity)
	if err != nil {
		if errors.Is(err, galaxyauth.ErrUserNotProvisioned) {
			presenter.Error(w, r, apierr.New(apierr.CodeUnauthenticated, "no local account for this identity and auto-provisioning is disabled"))
			return
		}
		h.log.Error("oidc: user resolution failed", "error", err)
		presenter.Error(w, r, apierr.New(apierr.CodeInternalError, "could not resolve user account"))
		return
	}

	pair, err := h.sessions.IssueSession(r.Context(), user, true)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}

	h.auth.setTokenCookies(w, pair, pair.RefreshTTL)
	h.log.Info("oidc: SSO login", "user_id", user.ID, "username", user.Username)
	http.Redirect(w, r, "/", http.StatusFound)
}

// exchangeCode redeems the authorization code at the issuer's token endpoint
// using client_secret_post plus the PKCE verifier, returning the raw id_token.
func (h *OIDCHandler) exchangeCode(ctx context.Context, code, verifier string) (string, error) {
	disc, err := h.provider.Discover(ctx)
	if err != nil {
		return "", err
	}

	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("redirect_uri", h.opts.RedirectURL)
	form.Set("client_id", h.opts.ClientID)
	form.Set("client_secret", h.opts.ClientSecret)
	form.Set("code_verifier", verifier)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, disc.TokenEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		// Deliberately omit the body: error payloads from misconfigured
		// issuers can echo credentials.
		return "", fmt.Errorf("token endpoint returned status %d", resp.StatusCode)
	}

	var tokenResp struct {
		IDToken string `json:"id_token"`
	}
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return "", fmt.Errorf("decode token response: %w", err)
	}
	if tokenResp.IDToken == "" {
		return "", fmt.Errorf("token response contained no id_token")
	}
	return tokenResp.IDToken, nil
}

func (h *OIDCHandler) clearStateCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     oidcStateCookieName,
		Value:    "",
		Path:     oidcStateCookiePath,
		HttpOnly: true,
		Secure:   h.auth.cookie.Secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
}

func stringClaim(claims map[string]any, key string) string {
	v, _ := claims[key].(string)
	return v
}
