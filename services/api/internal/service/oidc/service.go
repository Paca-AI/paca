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
package oidc

import (
	"context"
	"crypto/rand"
	"encoding/base64"
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
	// ErrUserNotProvisioned is returned when JIT provisioning is disabled and
	// no Paca user is bound to the (issuer, sub) identity.
	ErrUserNotProvisioned = errors.New("oidc: no paca user bound to this identity")
	// ErrUserRejected is returned when the bound user no longer exists (e.g.
	// soft-deleted) — the binding stays but the account may not log in.
	ErrUserRejected = errors.New("oidc: bound user is not eligible for login")
	// ErrExchangeFailed wraps any failure during code exchange or ID-token
	// verification; details stay in the server log only.
	ErrExchangeFailed = errors.New("oidc: authorization failed")
)

// loginTxTTL bounds how long a login redirect stays redeemable.
const loginTxTTL = 10 * time.Minute

// Options configures the OIDC service (mirrors the validated config.OIDCConfig).
type Options struct {
	IssuerURL     string
	ClientID      string
	ClientSecret  string
	RedirectURL   string
	Scopes        []string
	DisplayName   string
	JITProvision  bool
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
	// httpClient is used for the code exchange (and can be swapped in tests).
	httpClient *http.Client
}

// New performs OIDC discovery against the issuer and returns a ready
// Service. Failing fast here — an SSO-enabled deployment whose IdP is
// unreachable must not start half-configured.
func New(ctx context.Context, opts Options, txStore LoginTxStore, users userdom.Repository, identities extiddom.Repository, roles RoleByNameFinder, sessions SessionIssuer, log *slog.Logger) (*Service, error) {
	provider, err := oidc.NewProvider(ctx, opts.IssuerURL)
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
		httpClient: http.DefaultClient,
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
// returns the IdP authorization URL to redirect the browser to.
func (s *Service) BeginLogin(ctx context.Context) (string, error) {
	state, err := randomState()
	if err != nil {
		return "", fmt.Errorf("oidc: generate state: %w", err)
	}
	nonce, err := randomState()
	if err != nil {
		return "", fmt.Errorf("oidc: generate nonce: %w", err)
	}
	verifier, err := randomState()
	if err != nil {
		return "", fmt.Errorf("oidc: generate pkce verifier: %w", err)
	}

	payload, err := json.Marshal(LoginTx{Nonce: nonce, Verifier: verifier})
	if err != nil {
		return "", fmt.Errorf("oidc: marshal login tx: %w", err)
	}
	if err := s.txStore.Save(ctx, state, payload, loginTxTTL); err != nil {
		return "", fmt.Errorf("oidc: store login tx: %w", err)
	}

	s.log.Info("oidc: login initiated", "issuer", s.opts.IssuerURL)
	return s.oauth2Conf.AuthCodeURL(
		state,
		oidc.Nonce(nonce),
		oauth2.S256ChallengeOption(verifier),
	), nil
}

// userClaims carries the profile claims used for JIT provisioning. They are
// display data only — identity always comes from (iss, sub).
type userClaims struct {
	PreferredUsername string
	Name              string
	Email             string
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

	idToken, err := s.verifier.Verify(ctx, rawIDToken)
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
	}

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

// resolveUser maps the verified (issuer, subject) to a Paca user — loading
// the existing binding, or JIT-provisioning a new SSO-only account when
// enabled. Email/username are never used to match an existing account: only
// the (issuer, sub) binding proves account ownership.
func (s *Service) resolveUser(ctx context.Context, issuer, subject string, claims userClaims) (*userdom.User, error) {
	identity, err := s.identities.FindByIssuerSubject(ctx, issuer, subject)
	if err == nil {
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
	if !errors.Is(err, extiddom.ErrNotFound) {
		return nil, fmt.Errorf("oidc: find identity: %w", err)
	}

	if !s.opts.JITProvision {
		s.log.Warn("oidc: unknown identity and jit disabled", "issuer", issuer)
		return nil, ErrUserNotProvisioned
	}
	return s.provisionUser(ctx, issuer, subject, claims)
}

// provisionUser JIT-creates a new SSO-only Paca user together with its
// external-identity binding, in a single transaction.
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

	// Email is display data only. A collision with an existing account's
	// email never links the accounts — the email is simply dropped.
	email := ""
	if claims.Email != "" {
		if _, err := s.users.FindByEmail(ctx, claims.Email); errors.Is(err, userdom.ErrNotFound) {
			email = claims.Email
		}
	}

	baseUsername := usernameCandidate(claims.PreferredUsername, subject)
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
		if err := s.identities.ProvisionWithUser(ctx, u, identity); err != nil {
			if errors.Is(err, userdom.ErrUsernameTaken) {
				continue // lost a race — try the next candidate
			}
			if errors.Is(err, userdom.ErrEmailTaken) && u.Email != nil {
				// Lost a race on the email — retry once without it.
				u.Email = nil
				email = ""
				if err := s.identities.ProvisionWithUser(ctx, u, identity); err != nil {
					if errors.Is(err, userdom.ErrUsernameTaken) {
						continue
					}
					return nil, err
				}
				s.log.Info("oidc: jit user provisioned",
					"issuer", issuer, "user_id", u.ID.String())
				return u, nil
			}
			return nil, err
		}
		s.log.Info("oidc: jit user provisioned",
			"issuer", issuer, "user_id", u.ID.String())
		return u, nil
	}
	return nil, fmt.Errorf("oidc: could not derive a free username for new sso user")
}

// usernameCandidate sanitizes the configured username claim into a valid
// Paca username, falling back to an opaque subject-derived name when the
// claim is missing or unusable.
func usernameCandidate(claimValue, subject string) string {
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
		// Fall back to an opaque but stable, readable name derived from the
		// subject (which is what actually identifies the user).
		suffix := subject
		if len(suffix) > 8 {
			suffix = suffix[:8]
		}
		name = "sso-" + suffix
	}
	return name
}
