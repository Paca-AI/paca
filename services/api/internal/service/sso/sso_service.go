// Package sso implements SSO / OpenID Connect sign-in: admin management of
// identity providers, and the authorization-code flow (with PKCE, state and
// nonce) that turns a provider's verified identity into a Paca session.
package sso

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/google/uuid"
	"golang.org/x/oauth2"

	domainauth "github.com/Paca-AI/api/internal/domain/auth"
	ssodom "github.com/Paca-AI/api/internal/domain/sso"
	userdom "github.com/Paca-AI/api/internal/domain/user"
	"github.com/Paca-AI/api/internal/platform/secret"
)

// CallbackPath returns the path of slug's OIDC redirect URI, relative to the
// deployment's public base URL. It is what an admin registers at the
// provider, so it must not change.
func CallbackPath(slug string) string {
	return "/api/v1/auth/sso/" + slug + "/callback"
}

// DefaultRedirect is where a sign-in lands when it carries no (valid)
// in-app redirect.
const DefaultRedirect = "/home"

// discoveryTTL bounds how long a provider's discovery document and signing
// keys are cached before being fetched again.
const discoveryTTL = time.Hour

var slugRe = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,38}[a-z0-9])?$`)

// SessionIssuer starts a Paca session for an authenticated user — see
// authsvc.Service.IssueSession.
type SessionIssuer interface {
	IssueSession(u *userdom.User, rememberMe bool) (*domainauth.TokenPair, error)
}

// UserCreator provisions a new account with the default global role — see
// usersvc.Service.Create.
type UserCreator interface {
	Create(ctx context.Context, in userdom.CreateInput) (*userdom.User, error)
}

// Service implements provider management and SSO sign-in.
type Service struct {
	repo       ssodom.Repository
	states     ssodom.StateStore
	users      userdom.Repository
	creator    UserCreator
	sessions   SessionIssuer
	encryptor  *secret.Encryptor
	httpClient *http.Client
	log        *slog.Logger

	mu        sync.Mutex
	discovery map[uuid.UUID]*cachedProvider
}

type cachedProvider struct {
	updatedAt time.Time
	fetchedAt time.Time
	provider  *oidc.Provider
}

// New returns an SSO Service.
func New(repo ssodom.Repository, states ssodom.StateStore, users userdom.Repository, creator UserCreator, sessions SessionIssuer, log *slog.Logger) *Service {
	return &Service{
		repo:       repo,
		states:     states,
		users:      users,
		creator:    creator,
		sessions:   sessions,
		httpClient: &http.Client{Timeout: 10 * time.Second},
		log:        log,
		discovery:  map[uuid.UUID]*cachedProvider{},
	}
}

// WithEncryptor encrypts client secrets at rest. Nil stores plaintext, the
// same fallback every other at-rest secret uses when ENCRYPTION_KEY is unset.
func (s *Service) WithEncryptor(enc *secret.Encryptor) *Service {
	s.encryptor = enc
	return s
}

// WithHTTPClient overrides the client used to reach providers (tests).
func (s *Service) WithHTTPClient(c *http.Client) *Service {
	s.httpClient = c
	return s
}

// ---------------------------------------------------------------------------
// Provider management
// ---------------------------------------------------------------------------

// ListProviders returns every provider, secrets decrypted.
func (s *Service) ListProviders(ctx context.Context) ([]*ssodom.Provider, error) {
	ps, err := s.repo.ListProviders(ctx)
	if err != nil {
		return nil, err
	}
	for _, p := range ps {
		if err := s.decryptSecret(p); err != nil {
			return nil, err
		}
	}
	return ps, nil
}

// ListEnabledProviders returns the providers shown on the login page.
func (s *Service) ListEnabledProviders(ctx context.Context) ([]*ssodom.Provider, error) {
	ps, err := s.repo.ListProviders(ctx)
	if err != nil {
		return nil, err
	}
	return slices.DeleteFunc(ps, func(p *ssodom.Provider) bool { return !p.Enabled }), nil
}

// CreateProvider validates in and stores a new provider. An enabled provider
// must pass issuer discovery first, so a typo'd issuer is caught at save time
// rather than on a user's first sign-in.
func (s *Service) CreateProvider(ctx context.Context, in ssodom.ProviderInput) (*ssodom.Provider, error) {
	now := time.Now().UTC()
	p := &ssodom.Provider{ID: uuid.New(), CreatedAt: now, UpdatedAt: now}
	if err := applyInput(p, in); err != nil {
		return nil, err
	}
	if err := s.checkDiscovery(ctx, p); err != nil {
		return nil, err
	}
	stored, err := s.withEncryptedSecret(p)
	if err != nil {
		return nil, err
	}
	if err := s.repo.CreateProvider(ctx, stored); err != nil {
		return nil, err
	}
	return p, nil
}

// UpdateProvider replaces the provider's editable fields. A nil
// in.ClientSecret keeps the stored secret.
func (s *Service) UpdateProvider(ctx context.Context, id uuid.UUID, in ssodom.ProviderInput) (*ssodom.Provider, error) {
	p, err := s.repo.FindProviderByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := s.decryptSecret(p); err != nil {
		return nil, err
	}
	if err := applyInput(p, in); err != nil {
		return nil, err
	}
	p.UpdatedAt = time.Now().UTC()
	if err := s.checkDiscovery(ctx, p); err != nil {
		return nil, err
	}
	stored, err := s.withEncryptedSecret(p)
	if err != nil {
		return nil, err
	}
	if err := s.repo.UpdateProvider(ctx, stored); err != nil {
		return nil, err
	}
	s.forget(p.ID)
	return p, nil
}

// DeleteProvider removes the provider and every account link made through
// it. The accounts themselves stay.
func (s *Service) DeleteProvider(ctx context.Context, id uuid.UUID) error {
	if err := s.repo.DeleteProvider(ctx, id); err != nil {
		return err
	}
	s.forget(id)
	return nil
}

func applyInput(p *ssodom.Provider, in ssodom.ProviderInput) error {
	slug := strings.ToLower(strings.TrimSpace(in.Slug))
	if !slugRe.MatchString(slug) {
		return fmt.Errorf("%w: slug must be 1-40 lowercase letters, digits or dashes", ssodom.ErrInvalidProvider)
	}
	name := strings.TrimSpace(in.DisplayName)
	if name == "" || len(name) > 100 {
		return fmt.Errorf("%w: display name must be 1-100 characters", ssodom.ErrInvalidProvider)
	}
	issuer := strings.TrimRight(strings.TrimSpace(in.IssuerURL), "/")
	u, err := url.Parse(issuer)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return fmt.Errorf("%w: issuer URL must be an absolute http(s) URL", ssodom.ErrInvalidProvider)
	}
	clientID := strings.TrimSpace(in.ClientID)
	if clientID == "" {
		return fmt.Errorf("%w: client ID is required", ssodom.ErrInvalidProvider)
	}

	scopes := normalizeList(in.Scopes, false)
	if len(scopes) == 0 {
		scopes = slices.Clone(ssodom.DefaultScopes)
	}
	if !slices.Contains(scopes, oidc.ScopeOpenID) {
		scopes = append([]string{oidc.ScopeOpenID}, scopes...)
	}
	domains := normalizeList(in.AllowedDomains, true)
	for _, d := range domains {
		if strings.ContainsAny(d, "@/ ") || !strings.Contains(d, ".") {
			return fmt.Errorf("%w: %q is not an email domain", ssodom.ErrInvalidProvider, d)
		}
	}

	p.Slug = slug
	p.DisplayName = name
	p.IssuerURL = issuer
	p.ClientID = clientID
	if in.ClientSecret != nil {
		p.ClientSecret = strings.TrimSpace(*in.ClientSecret)
	}
	p.Scopes = scopes
	p.Enabled = in.Enabled
	p.AutoProvision = in.AutoProvision
	p.LinkByEmail = in.LinkByEmail
	p.AllowedDomains = domains
	return nil
}

// normalizeList trims, drops empties and duplicates, and optionally
// lowercases.
func normalizeList(in []string, lower bool) []string {
	out := make([]string, 0, len(in))
	for _, v := range in {
		v = strings.TrimSpace(v)
		if lower {
			v = strings.ToLower(v)
		}
		if v != "" && !slices.Contains(out, v) {
			out = append(out, v)
		}
	}
	return out
}

func (s *Service) checkDiscovery(ctx context.Context, p *ssodom.Provider) error {
	if !p.Enabled {
		return nil
	}
	if _, err := oidc.NewProvider(oidc.ClientContext(ctx, s.httpClient), p.IssuerURL); err != nil {
		return fmt.Errorf("%w: %v", ssodom.ErrDiscoveryFailed, err)
	}
	return nil
}

func (s *Service) withEncryptedSecret(p *ssodom.Provider) (*ssodom.Provider, error) {
	stored := *p
	if s.encryptor != nil && p.ClientSecret != "" {
		enc, err := s.encryptor.Encrypt(p.ClientSecret)
		if err != nil {
			return nil, fmt.Errorf("sso svc: encrypt client secret: %w", err)
		}
		stored.ClientSecret = enc
	}
	return &stored, nil
}

func (s *Service) decryptSecret(p *ssodom.Provider) error {
	if s.encryptor == nil || p.ClientSecret == "" {
		return nil
	}
	plain, err := s.encryptor.Decrypt(p.ClientSecret)
	if err != nil {
		return fmt.Errorf("sso svc: decrypt client secret of provider %q: %w", p.Slug, err)
	}
	p.ClientSecret = plain
	return nil
}

// ---------------------------------------------------------------------------
// Sign-in flow
// ---------------------------------------------------------------------------

// BeginLogin starts a sign-in through the enabled provider slug. baseURL is
// the deployment's public origin (the redirect URI is built from it). It
// returns the provider URL to send the browser to and the browser-binding
// value the caller must store in a cookie and hand back to CompleteLogin.
func (s *Service) BeginLogin(ctx context.Context, slug, baseURL string, rememberMe bool, redirect string) (authURL, binding string, err error) {
	p, op, err := s.enabledProvider(ctx, slug)
	if err != nil {
		return "", "", err
	}
	state, err1 := randomToken()
	nonce, err2 := randomToken()
	binding, err3 := randomToken()
	if err := errors.Join(err1, err2, err3); err != nil {
		return "", "", err
	}
	verifier := oauth2.GenerateVerifier()

	if err := s.states.Put(ctx, state, &ssodom.AuthAttempt{
		ProviderID:     p.ID,
		Nonce:          nonce,
		CodeVerifier:   verifier,
		BrowserBinding: binding,
		RememberMe:     rememberMe,
		Redirect:       SafeRedirect(redirect),
	}); err != nil {
		return "", "", err
	}

	cfg := oauthConfig(p, op, baseURL)
	return cfg.AuthCodeURL(state, oidc.Nonce(nonce), oauth2.S256ChallengeOption(verifier)), binding, nil
}

// CompleteLogin finishes a sign-in at the provider's callback: it checks the
// state against this browser, exchanges the code, verifies the ID token,
// resolves the Paca account and starts a session. It returns the session and
// the in-app path to land on.
func (s *Service) CompleteLogin(ctx context.Context, slug, baseURL, code, state, binding string) (*domainauth.TokenPair, string, error) {
	if state == "" || binding == "" {
		return nil, "", ssodom.ErrStateInvalid
	}
	attempt, err := s.states.Take(ctx, state)
	if err != nil {
		return nil, "", err
	}
	if attempt == nil || !constantTimeEqual(attempt.BrowserBinding, binding) {
		return nil, "", ssodom.ErrStateInvalid
	}
	p, op, err := s.enabledProvider(ctx, slug)
	if err != nil {
		return nil, "", err
	}
	if p.ID != attempt.ProviderID {
		return nil, "", ssodom.ErrStateInvalid
	}

	octx := oidc.ClientContext(ctx, s.httpClient)
	tok, err := oauthConfig(p, op, baseURL).Exchange(octx, code, oauth2.VerifierOption(attempt.CodeVerifier))
	if err != nil {
		s.log.Warn("sso: code exchange failed", "provider", p.Slug, "error", err)
		return nil, "", ssodom.ErrExchangeFailed
	}
	rawID, _ := tok.Extra("id_token").(string)
	if rawID == "" {
		s.log.Warn("sso: token response has no id_token", "provider", p.Slug)
		return nil, "", ssodom.ErrExchangeFailed
	}
	idTok, err := op.Verifier(&oidc.Config{ClientID: p.ClientID}).Verify(octx, rawID)
	if err != nil {
		s.log.Warn("sso: id token verification failed", "provider", p.Slug, "error", err)
		return nil, "", ssodom.ErrExchangeFailed
	}
	if !constantTimeEqual(idTok.Nonce, attempt.Nonce) {
		s.log.Warn("sso: id token nonce mismatch", "provider", p.Slug)
		return nil, "", ssodom.ErrExchangeFailed
	}

	var c Claims
	if err := idTok.Claims(&c); err != nil {
		return nil, "", ssodom.ErrExchangeFailed
	}
	c.Subject = idTok.Subject
	// Some providers (e.g. those with a small ID token) put email/name only
	// in the userinfo response.
	if c.Email == "" {
		if ui, err := op.UserInfo(octx, oauth2.StaticTokenSource(tok)); err == nil && ui.Subject == c.Subject {
			var uc Claims
			if ui.Claims(&uc) == nil {
				c.merge(uc)
			}
		}
	}

	u, err := s.ResolveUser(ctx, p, c)
	if err != nil {
		return nil, "", err
	}
	pair, err := s.sessions.IssueSession(u, attempt.RememberMe)
	if err != nil {
		return nil, "", err
	}
	return pair, SafeRedirect(attempt.Redirect), nil
}

// enabledProvider loads an enabled provider and its (cached) discovery
// document. A disabled provider reads as not found.
func (s *Service) enabledProvider(ctx context.Context, slug string) (*ssodom.Provider, *oidc.Provider, error) {
	p, err := s.repo.FindProviderBySlug(ctx, slug)
	if err != nil {
		return nil, nil, err
	}
	if !p.Enabled {
		return nil, nil, ssodom.ErrProviderNotFound
	}
	if err := s.decryptSecret(p); err != nil {
		return nil, nil, err
	}
	op, err := s.discover(ctx, p)
	if err != nil {
		return nil, nil, err
	}
	return p, op, nil
}

func (s *Service) discover(ctx context.Context, p *ssodom.Provider) (*oidc.Provider, error) {
	s.mu.Lock()
	c := s.discovery[p.ID]
	s.mu.Unlock()
	if c != nil && c.updatedAt.Equal(p.UpdatedAt) && time.Since(c.fetchedAt) < discoveryTTL {
		return c.provider, nil
	}
	// The discovery context must outlive this request: go-oidc's key set
	// keeps it for fetching rotated signing keys later.
	op, err := oidc.NewProvider(oidc.ClientContext(context.WithoutCancel(ctx), s.httpClient), p.IssuerURL)
	if err != nil {
		s.log.Warn("sso: issuer discovery failed", "provider", p.Slug, "error", err)
		return nil, fmt.Errorf("%w: %v", ssodom.ErrDiscoveryFailed, err)
	}
	s.mu.Lock()
	s.discovery[p.ID] = &cachedProvider{updatedAt: p.UpdatedAt, fetchedAt: time.Now(), provider: op}
	s.mu.Unlock()
	return op, nil
}

func (s *Service) forget(id uuid.UUID) {
	s.mu.Lock()
	delete(s.discovery, id)
	s.mu.Unlock()
}

func oauthConfig(p *ssodom.Provider, op *oidc.Provider, baseURL string) *oauth2.Config {
	return &oauth2.Config{
		ClientID:     p.ClientID,
		ClientSecret: p.ClientSecret,
		Endpoint:     op.Endpoint(),
		RedirectURL:  strings.TrimRight(baseURL, "/") + CallbackPath(p.Slug),
		Scopes:       p.Scopes,
	}
}

// SafeRedirect returns redirect if it is an in-app absolute path, else
// DefaultRedirect — so the post-sign-in redirect can never leave the app
// (an open redirect would turn the SSO flow into a phishing aid).
func SafeRedirect(redirect string) string {
	if redirect == "" || !strings.HasPrefix(redirect, "/") ||
		strings.HasPrefix(redirect, "//") || strings.ContainsAny(redirect, "\\\r\n\t") {
		return DefaultRedirect
	}
	u, err := url.Parse(redirect)
	if err != nil || u.Scheme != "" || u.Host != "" {
		return DefaultRedirect
	}
	return redirect
}

func randomToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("sso svc: random: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
