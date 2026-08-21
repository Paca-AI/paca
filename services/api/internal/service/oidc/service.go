// Package oidc implements the OIDC single-sign-on service: Authorization
// Code Flow + PKCE (S256) against a single, provider-neutral OIDC IdP.
//
// Scope is deliberately narrow — OIDC only proves *who* the human is:
//   - external identity is keyed by (issuer, subject), never email/username;
//   - a successful login resolves to a Paca user and issues the same Paca
//     JWT session the password login path issues (auth.Service.IssueSessionForUser);
//   - Paca RBAC, API keys, ACP bridge tokens and agent MCP keys are untouched;
//   - no IdP token is ever stored in the database or handed to the browser
//     or an agent.
//
// Login CSRF defense: the caller (HTTP handler) binds the state to the
// initiating browser with a short-lived HttpOnly cookie, and the callback is
// only honored when the cookie matches the state in the callback URL. The
// server-side login transaction alone is NOT browser-bound, so the handler's
// cookie check is a mandatory part of the flow — see oidc_handler.go.
package oidc

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
	"golang.org/x/oauth2"

	domainauth "github.com/Paca-AI/api/internal/domain/auth"
	extiddom "github.com/Paca-AI/api/internal/domain/externalidentity"
	globalroledom "github.com/Paca-AI/api/internal/domain/globalrole"
	userdom "github.com/Paca-AI/api/internal/domain/user"
)

// LoginTx is the per-login-attempt transaction state: the OIDC nonce and the
// PKCE verifier, stored server-side keyed by the random state value. It is
// single-use and short-lived (see loginTxTTL).
type LoginTx struct {
	Nonce    string `json:"nonce"`
	Verifier string `json:"verifier"`
}

// LoginTxStore persists login transactions between the redirect to the IdP
// and the callback, keyed by the random state value. Payloads are opaque
// bytes so the store stays decoupled from this package's types. Consume must
// be single-use: a second call with the same state returns no payload.
type LoginTxStore interface {
	Save(ctx context.Context, state string, payload []byte, ttl time.Duration) error
	Consume(ctx context.Context, state string) ([]byte, error)
}

// SessionIssuer issues Paca sessions for a resolved user; satisfied by the
// auth service's IssueSessionForUser.
type SessionIssuer interface {
	IssueSessionForUser(ctx context.Context, userID uuid.UUID, rememberMe bool) (*domainauth.TokenPair, error)
}

// RoleByNameFinder resolves a global role by name; satisfied by the postgres
// GlobalRoleRepository.
type RoleByNameFinder interface {
	FindByName(ctx context.Context, name string) (*globalroledom.GlobalRole, error)
}

// Sentinel errors surfaced to the HTTP layer.
var (
	// ErrInvalidState covers unknown, expired, and already-used states alike
	// — deliberately undistinguished so a caller cannot probe them.
	ErrInvalidState = errors.New("oidc: invalid or expired login state")
	// ErrUserRejected is returned when the bound user no longer exists (e.g.
	// soft-deleted) — the binding stays but the account may not log in.
	ErrUserRejected = errors.New("oidc: bound user is not eligible for login")
	// ErrExchangeFailed wraps any failure during code exchange or ID-token
	// verification; details stay in the server log only.
	ErrExchangeFailed = errors.New("oidc: authorization failed")
)

const (
	// loginTxTTL bounds how long a login redirect stays redeemable.
	loginTxTTL = 10 * time.Minute
	// httpTimeout bounds every outbound call to the IdP: discovery at
	// startup, the code exchange, JWKS fetches, and the UserInfo call. An
	// IdP that is half-up must fail fast rather than hang the request.
	httpTimeout = 10 * time.Second
)

// Options configures the OIDC service (mirrors the validated config.OIDCConfig).
type Options struct {
	IssuerURL     string
	ClientID      string
	ClientSecret  string
	RedirectURL   string
	Scopes        []string
	DisplayName   string
	DefaultRole   string
	UsernameClaim string
}

// Service drives the OIDC login flow.
type Service struct {
	opts       Options
	provider   *oidc.Provider
	verifier   *oidc.IDTokenVerifier
	oauth2Conf oauth2.Config
	txStore    LoginTxStore
	users      userdom.Repository
	identities extiddom.Repository
	roles      RoleByNameFinder
	sessions   SessionIssuer
	log        *slog.Logger
	// httpClient is used for the code exchange, UserInfo, and JWKS fetches,
	// with a bounded timeout (see httpTimeout).
	httpClient *http.Client
}

// New performs OIDC discovery against the issuer and returns a ready
// Service. Failing fast here — an SSO-enabled deployment whose IdP is
// unreachable must not start half-configured. All outbound IdP traffic goes
// through a dedicated HTTP client with a fixed timeout so a half-dead IdP
// fails fast instead of hanging startup or requests.
func New(ctx context.Context, opts Options, txStore LoginTxStore, users userdom.Repository, identities extiddom.Repository, roles RoleByNameFinder, sessions SessionIssuer, log *slog.Logger) (*Service, error) {
	httpClient := &http.Client{Timeout: httpTimeout}
	provider, err := oidc.NewProvider(oidc.ClientContext(ctx, httpClient), opts.IssuerURL)
	if err != nil {
		return nil, fmt.Errorf("oidc: discovery against %s: %w", opts.IssuerURL, err)
	}
	return &Service{
		opts:     opts,
		provider: provider,
		verifier: provider.Verifier(&oidc.Config{ClientID: opts.ClientID}),
		oauth2Conf: oauth2.Config{
			ClientID:     opts.ClientID,
			ClientSecret: opts.ClientSecret,
			Endpoint:     provider.Endpoint(),
			RedirectURL:  opts.RedirectURL,
			Scopes:       opts.Scopes,
		},
		txStore:    txStore,
		users:      users,
		identities: identities,
		roles:      roles,
		sessions:   sessions,
		log:        log,
		httpClient: httpClient,
	}, nil
}

// randomState generates a cryptographically random URL-safe string for use
// as the OIDC state / nonce / PKCE verifier material.
func randomState() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// BeginLogin starts a fresh Authorization Code + PKCE flow: it stores the
// single-use login transaction server-side (keyed by a random state) and
// returns the IdP authorization URL plus the state value itself. The caller
// MUST bind the returned state to the initiating browser (short-lived
// HttpOnly cookie) and verify that binding in the callback — the server-side
// transaction alone does not bind the flow to a browser, and without the
// cookie check a callback URL forwarded to another browser completes a login
// CSRF.
func (s *Service) BeginLogin(ctx context.Context) (authURL string, state string, err error) {
	state, err = randomState()
	if err != nil {
		return "", "", fmt.Errorf("oidc: generate state: %w", err)
	}
	nonce, err := randomState()
	if err != nil {
		return "", "", fmt.Errorf("oidc: generate nonce: %w", err)
	}
	verifier, err := randomState()
	if err != nil {
		return "", "", fmt.Errorf("oidc: generate pkce verifier: %w", err)
	}

	payload, err := json.Marshal(LoginTx{Nonce: nonce, Verifier: verifier})
	if err != nil {
		return "", "", fmt.Errorf("oidc: marshal login tx: %w", err)
	}
	if err := s.txStore.Save(ctx, state, payload, loginTxTTL); err != nil {
		return "", "", fmt.Errorf("oidc: store login tx: %w", err)
	}

	s.log.Info("oidc: login initiated", "issuer", s.opts.IssuerURL)
	return s.oauth2Conf.AuthCodeURL(
		state,
		oidc.Nonce(nonce),
		oauth2.S256ChallengeOption(verifier),
	), state, nil
}

// userClaims carries the profile claims used for JIT provisioning. They are
// display data only — identity always comes from the ID token's (iss, sub).
type userClaims struct {
	PreferredUsername string
	Name              string
	Email             string
	// EmailVerified mirrors the OIDC email_verified claim. Paca only stores
	// an email the IdP has actually confirmed the user controls — see
	// provisionUser.
	EmailVerified bool
}

// userInfoClaims is the wire shape for the UserInfo endpoint (standard OIDC
// claim names).
type userInfoClaims struct {
	Sub               string `json:"sub"`
	PreferredUsername string `json:"preferred_username"`
	Name              string `json:"name"`
	Email             string `json:"email"`
	EmailVerified     bool   `json:"email_verified"`
}

// Callback completes the flow: consume the login transaction, exchange the
// code with the PKCE verifier, verify the ID token (signature, issuer,
// audience, expiry, nonce), resolve the Paca user, and issue a Paca session
// (rememberMe=false — ephemeral refresh TTL).
func (s *Service) Callback(ctx context.Context, code, state string) (*domainauth.TokenPair, error) {
	payload, err := s.txStore.Consume(ctx, state)
	if err != nil {
		return nil, fmt.Errorf("oidc: consume login tx: %w", err)
	}
	if payload == nil {
		s.log.Warn("oidc: callback with invalid state", "issuer", s.opts.IssuerURL)
		return nil, ErrInvalidState
	}
	var tx LoginTx
	if err := json.Unmarshal(payload, &tx); err != nil {
		return nil, fmt.Errorf("oidc: unmarshal login tx: %w", err)
	}

	authCtx := context.WithValue(ctx, oauth2.HTTPClient, s.httpClient)
	token, err := s.oauth2Conf.Exchange(authCtx, code, oauth2.VerifierOption(tx.Verifier))
	if err != nil {
		// Never log the underlying error body — token endpoint responses can
		// echo credentials. Log the failure class only.
		s.log.Warn("oidc: code exchange failed", "issuer", s.opts.IssuerURL)
		return nil, ErrExchangeFailed
	}

	rawIDToken, ok := token.Extra("id_token").(string)
	if !ok || rawIDToken == "" {
		s.log.Warn("oidc: token response missing id_token", "issuer", s.opts.IssuerURL)
		return nil, ErrExchangeFailed
	}

	idToken, err := s.verifier.Verify(authCtx, rawIDToken)
	if err != nil {
		s.log.Warn("oidc: id token verification failed", "issuer", s.opts.IssuerURL)
		return nil, ErrExchangeFailed
	}
	if idToken.Nonce != tx.Nonce {
		s.log.Warn("oidc: nonce mismatch", "issuer", s.opts.IssuerURL)
		return nil, ErrExchangeFailed
	}

	var raw map[string]any
	if err := idToken.Claims(&raw); err != nil {
		return nil, ErrExchangeFailed
	}
	claims := userClaims{
		PreferredUsername: stringClaim(raw, s.opts.UsernameClaim),
		Name:              stringClaim(raw, "name"),
		Email:             stringClaim(raw, "email"),
		EmailVerified:     boolClaim(raw, "email_verified"),
	}

	// Profile/email claims are not guaranteed to live in the ID token — under
	// the standard code flow they may only be available from the UserInfo
	// endpoint. Identity stays anchored to the ID token's (iss, sub); this
	// call only enriches the profile, and the returned sub must agree.
	claims = s.enrichFromUserInfo(authCtx, token, idToken.Subject, claims)

	user, err := s.resolveUser(ctx, idToken.Issuer, idToken.Subject, claims)
	if err != nil {
		return nil, err
	}

	pair, err := s.sessions.IssueSessionForUser(ctx, user.ID, false)
	if err != nil {
		return nil, err
	}
	s.log.Info("oidc: login succeeded",
		"issuer", idToken.Issuer,
		"user_id", user.ID.String())
	return pair, nil
}

// enrichFromUserInfo calls the provider's UserInfo endpoint with the access
// token and overlays profile/email claims on top of the ID-token-derived
// ones. A UserInfo sub that disagrees with the ID token's subject is never
// merged. If the endpoint is unreachable or unsupported the ID-token claims
// stand — identity was already established by the ID token, so only display
// data is at stake.
func (s *Service) enrichFromUserInfo(ctx context.Context, token *oauth2.Token, idTokenSub string, claims userClaims) userClaims {
	ui, err := s.provider.UserInfo(ctx, oauth2.StaticTokenSource(token))
	if err != nil {
		s.log.Warn("oidc: userinfo unavailable, falling back to id token claims", "issuer", s.opts.IssuerURL)
		return claims
	}
	var uic userInfoClaims
	if err := ui.Claims(&uic); err != nil {
		s.log.Warn("oidc: userinfo claims undecodable, falling back to id token claims", "issuer", s.opts.IssuerURL)
		return claims
	}
	if uic.Sub != idTokenSub {
		// The UserInfo response is about a different subject — never merge it.
		s.log.Warn("oidc: userinfo subject mismatch", "issuer", s.opts.IssuerURL)
		return claims
	}
	if uic.PreferredUsername != "" {
		claims.PreferredUsername = uic.PreferredUsername
	}
	if uic.Name != "" {
		claims.Name = uic.Name
	}
	if uic.Email != "" {
		claims.Email = uic.Email
		claims.EmailVerified = uic.EmailVerified
	}
	return claims
}

// stringClaim pulls a string-valued claim from the decoded ID-token payload;
// missing or non-string claims yield "".
func stringClaim(raw map[string]any, key string) string {
	if key == "" {
		return ""
	}
	if v, ok := raw[key].(string); ok {
		return strings.TrimSpace(v)
	}
	return ""
}

// boolClaim pulls a bool-valued claim; missing or non-bool yields false.
func boolClaim(raw map[string]any, key string) bool {
	if key == "" {
		return false
	}
	if v, ok := raw[key].(bool); ok {
		return v
	}
	return false
}

// resolveUser maps the verified (issuer, subject) to a Paca user — loading
// the existing binding, or JIT-provisioning a new SSO-only account. JIT
// provisioning is always on in this MVP: the only supported write path for
// user_external_identities is this provisioning flow, so a "no JIT" mode
// would leave first logins with no way to succeed at all.
// Email/username are never used to match an existing account: only the
// (issuer, sub) binding proves account ownership.
func (s *Service) resolveUser(ctx context.Context, issuer, subject string, claims userClaims) (*userdom.User, error) {
	user, err := s.resolveExistingUser(ctx, issuer, subject)
	if err == nil {
		return user, nil
	}
	if !errors.Is(err, extiddom.ErrNotFound) {
		return nil, err
	}
	return s.provisionUser(ctx, issuer, subject, claims)
}

// resolveExistingUser loads the user bound to (issuer, subject), refreshing
// the binding's last-login timestamp, or returns extiddom.ErrNotFound.
func (s *Service) resolveExistingUser(ctx context.Context, issuer, subject string) (*userdom.User, error) {
	identity, err := s.identities.FindByIssuerSubject(ctx, issuer, subject)
	if err != nil {
		return nil, err
	}
	user, err := s.users.FindByID(ctx, identity.UserID)
	if err != nil {
		// Deleted or missing user: the binding exists but the account is
		// not eligible. Fail closed.
		s.log.Warn("oidc: bound user not found or deleted",
			"issuer", issuer, "user_id", identity.UserID.String())
		return nil, ErrUserRejected
	}
	if err := s.identities.TouchLastLogin(ctx, identity.ID); err != nil {
		return nil, fmt.Errorf("oidc: touch last login: %w", err)
	}
	return user, nil
}

// provisionUser JIT-creates a new SSO-only Paca user together with its
// external-identity binding, in a single transaction. If a concurrent login
// already created the binding in between the pre-check and the insert, the
// unique-index conflict is resolved by loading that winner's user instead of
// failing the login.
func (s *Service) provisionUser(ctx context.Context, issuer, subject string, claims userClaims) (*userdom.User, error) {
	role, err := s.roles.FindByName(ctx, s.opts.DefaultRole)
	if err != nil {
		// Fail closed: an unresolvable default role must not fall back to
		// some other (possibly elevated) role.
		s.log.Error("oidc: default role unresolvable", "role", s.opts.DefaultRole)
		return nil, fmt.Errorf("oidc: default role %q not found", s.opts.DefaultRole)
	}

	// Unknown random password: the account can never log in with a local
	// password because nobody knows it — and PasswordLoginEnabled=false
	// additionally blocks every password operation (change, admin reset,
	// set-token) fail-closed.
	randomPassword, err := randomState()
	if err != nil {
		return nil, fmt.Errorf("oidc: generate random password: %w", err)
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(randomPassword), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("oidc: hash random password: %w", err)
	}

	// Email is display/notification data only, and only stored when the IdP
	// has verified the user controls it (email_verified=true). A collision
	// with an existing account's email never links the accounts — the email
	// is simply dropped.
	email := ""
	if claims.Email != "" && claims.EmailVerified {
		if _, err := s.users.FindByEmail(ctx, claims.Email); errors.Is(err, userdom.ErrNotFound) {
			email = claims.Email
		}
	}

	baseUsername := usernameCandidate(claims.PreferredUsername, issuer, subject)
	now := time.Now().UTC()

	// Try successive username candidates; each attempt is race-safe because
	// the unique index — not this pre-check — is the actual guarantee.
	for attempt := 0; attempt < 5; attempt++ {
		username := baseUsername
		if attempt > 0 {
			username = fmt.Sprintf("%s-%d", baseUsername, attempt+1)
		}
		if _, err := s.users.FindByUsername(ctx, username); err == nil {
			continue // taken — try the next candidate
		} else if !errors.Is(err, userdom.ErrNotFound) {
			return nil, err
		}

		u := &userdom.User{
			ID:                   uuid.New(),
			Username:             username,
			PasswordHash:         string(hash),
			FullName:             claims.Name,
			RoleID:               role.ID,
			Role:                 role.Name,
			MustChangePassword:   false,
			PasswordLoginEnabled: false,
			CreatedAt:            now,
			UpdatedAt:            now,
		}
		if email != "" {
			e := email
			u.Email = &e
		}
		identity := &extiddom.Identity{
			ID:          uuid.New(),
			UserID:      u.ID,
			Provider:    "oidc",
			Issuer:      issuer,
			Subject:     subject,
			CreatedAt:   now,
			LastLoginAt: now,
		}
		err := s.identities.ProvisionWithUser(ctx, u, identity)
		if err == nil {
			s.log.Info("oidc: jit user provisioned",
				"issuer", issuer, "user_id", u.ID.String())
			return u, nil
		}
		switch {
		case errors.Is(err, extiddom.ErrIdentityTaken):
			// A concurrent first login bound this exact (issuer, subject)
			// first — its user is the answer, not a new account.
			s.log.Info("oidc: identity provision lost race, resolving winner",
				"issuer", issuer)
			winner, rerr := s.resolveExistingUser(ctx, issuer, subject)
			if rerr == nil {
				return winner, nil
			}
			if errors.Is(rerr, extiddom.ErrNotFound) || errors.Is(rerr, ErrUserRejected) {
				return nil, ErrUserRejected
			}
			return nil, rerr
		case errors.Is(err, userdom.ErrUsernameTaken):
			continue // lost a race on the username — try the next candidate
		case errors.Is(err, userdom.ErrEmailTaken) && u.Email != nil:
			// Lost a race on the email — retry without it.
			u.Email = nil
			email = ""
			if err := s.identities.ProvisionWithUser(ctx, u, identity); err != nil {
				if errors.Is(err, extiddom.ErrIdentityTaken) {
					winner, rerr := s.resolveExistingUser(ctx, issuer, subject)
					if rerr == nil {
						return winner, nil
					}
					if errors.Is(rerr, extiddom.ErrNotFound) || errors.Is(rerr, ErrUserRejected) {
						return nil, ErrUserRejected
					}
					return nil, rerr
				}
				if errors.Is(err, userdom.ErrUsernameTaken) {
					continue
				}
				return nil, err
			}
			s.log.Info("oidc: jit user provisioned",
				"issuer", issuer, "user_id", u.ID.String())
			return u, nil
		default:
			return nil, err
		}
	}
	return nil, fmt.Errorf("oidc: could not derive a free username for new sso user")
}

// usernameCandidate sanitizes the configured username claim into a valid
// Paca username, falling back to an opaque but stable name derived from a
// hash of (issuer, subject) — the subject itself is an arbitrary IdP-local
// string and must never be used raw.
func usernameCandidate(claimValue, issuer, subject string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(claimValue) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '.', r == '_', r == '-':
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
	}
	name := strings.Trim(b.String(), "-")
	if len(name) < 3 || len(name) > 64 {
		// Opaque fallback: 12 hex chars of SHA-256(issuer NUL subject).
		// Stable across logins, safe in every Paca username context, and
		// leaks nothing about the raw subject.
		sum := sha256.Sum256([]byte(issuer + "\x00" + subject))
		name = "sso-" + hex.EncodeToString(sum[:])[:12]
	}
	return name
}
