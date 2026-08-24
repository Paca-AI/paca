// Package auth implements the authentication service.
package auth

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	domainauth "github.com/Paca-AI/api/internal/domain/auth"
	userdom "github.com/Paca-AI/api/internal/domain/user"
	jwttoken "github.com/Paca-AI/api/internal/platform/token"
)

// gracePeriod is the window in which a reused refresh token is treated as a
// concurrent/retry request rather than a stolen token.  The family is NOT
// revoked during this window, but the request is still rejected.
const gracePeriod = 5 * time.Second

// RefreshTokenStore is the persistence contract for refresh-token rotation.
type RefreshTokenStore interface {
	// RecordFirstUse marks jti as used on the first call and returns nil.
	// Subsequent calls return the time of the first use.
	RecordFirstUse(ctx context.Context, jti string, ttl time.Duration) (*time.Time, error)
	// RevokeFamily marks the entire token family as revoked.
	RevokeFamily(ctx context.Context, familyID string, ttl time.Duration) error
	// IsFamilyRevoked returns true when the family has been revoked.
	IsFamilyRevoked(ctx context.Context, familyID string) (bool, error)
}

// Service is the concrete implementation of domain/auth.Service.
type Service struct {
	users             userdom.Repository
	tokens            *jwttoken.Manager
	refreshStore      RefreshTokenStore
	refreshTTL        time.Duration
	refreshSessionTTL time.Duration
	localLoginPolicy  func() bool
}

// New returns a configured auth Service. Local password login is enabled by
// default; SSO-only deployments disable it via WithLocalLoginEnabled(false).
func New(users userdom.Repository, tokens *jwttoken.Manager, refreshStore RefreshTokenStore, refreshTTL, refreshSessionTTL time.Duration) *Service {
	return &Service{
		users:             users,
		tokens:            tokens,
		refreshStore:      refreshStore,
		refreshTTL:        refreshTTL,
		refreshSessionTTL: refreshSessionTTL,
		localLoginPolicy:  func() bool { return true },
	}
}

// WithLocalLoginEnabled configures whether username/password login is
// accepted at all. When false, every password login is rejected at the
// service layer (SSO-only deployments) — enforced here, not only in the UI,
// so the endpoint cannot be reached by bypassing the login form.
func (s *Service) WithLocalLoginEnabled(enabled bool) *Service {
	s.localLoginPolicy = func() bool { return enabled }
	return s
}

// WithLocalLoginPolicy configures a live policy for password login. The
// callback is evaluated for every request so an admin SSO settings update can
// take effect without rebuilding the auth service or restarting the process.
func (s *Service) WithLocalLoginPolicy(policy func() bool) *Service {
	if policy == nil {
		policy = func() bool { return true }
	}
	s.localLoginPolicy = policy
	return s
}

// Login validates credentials and returns a fresh token pair.
// When rememberMe is true, the refresh token uses the long-lived TTL
// (JWT_REFRESH_TTL); when false, the shorter session TTL is used
// (JWT_REFRESH_SESSION_TTL, default 24 h).
func (s *Service) Login(ctx context.Context, username, password string, rememberMe bool) (*domainauth.TokenPair, error) {
	if !s.localLoginPolicy() {
		return nil, domainauth.ErrLocalLoginDisabled
	}

	u, err := s.users.FindByUsername(ctx, username)
	if err != nil {
		if errors.Is(err, userdom.ErrNotFound) {
			return nil, domainauth.ErrInvalidCredentials
		}
		return nil, err
	}

	// SSO-only accounts carry an unknown random password hash; a deliberate
	// compare against it keeps timing uniform with the not-found path.
	if err := bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)); err != nil {
		return nil, domainauth.ErrInvalidCredentials
	}

	if !u.PasswordLoginEnabled {
		// Same error as bad credentials — no oracle distinguishing SSO-only
		// accounts from nonexistent ones.
		return nil, domainauth.ErrInvalidCredentials
	}

	return s.IssueSessionForUser(ctx, u.ID, rememberMe)
}

// IssueSessionForUser issues a fresh token pair for the given user without
// checking credentials. The user (and its current role) is re-read from the
// repository so callers never pass in stale user state. It is the shared
// issuance path behind password login and external-identity (OIDC) login.
func (s *Service) IssueSessionForUser(ctx context.Context, userID uuid.UUID, rememberMe bool) (*domainauth.TokenPair, error) {
	u, err := s.users.FindByID(ctx, userID)
	if err != nil {
		if errors.Is(err, userdom.ErrNotFound) {
			return nil, domainauth.ErrInvalidCredentials
		}
		return nil, err
	}

	familyID := uuid.NewString()
	sub := u.ID.String()

	refreshTTL := s.refreshTTL
	if !rememberMe {
		refreshTTL = s.refreshSessionTTL
	}

	access, err := s.tokens.IssueAccess(sub, u.Username, u.Role, familyID, u.MustChangePassword)
	if err != nil {
		return nil, err
	}
	refresh, err := s.tokens.IssueRefreshWithTTL(sub, u.Username, u.Role, familyID, rememberMe, refreshTTL)
	if err != nil {
		return nil, err
	}

	return &domainauth.TokenPair{AccessToken: access, RefreshToken: refresh, RefreshTTL: refreshTTL}, nil
}

// Refresh validates a refresh token and issues a rotated token pair.
// If the same token is presented twice outside the grace period, the entire
// session family is revoked to mitigate token-theft scenarios.
func (s *Service) Refresh(ctx context.Context, refreshToken string) (*domainauth.TokenPair, error) {
	claims, err := s.tokens.Verify(refreshToken)
	if err != nil {
		return nil, domainauth.ErrTokenInvalid
	}

	if claims.Kind != "refresh" {
		return nil, domainauth.ErrTokenInvalid
	}

	// Fast path: reject immediately if the family was already invalidated.
	revoked, err := s.refreshStore.IsFamilyRevoked(ctx, claims.FamilyID)
	if err != nil {
		return nil, err
	}
	if revoked {
		return nil, domainauth.ErrSessionInvalidated
	}

	// Record use — detect reuse.
	firstUsedAt, err := s.refreshStore.RecordFirstUse(ctx, claims.ID, s.refreshTTL)
	if err != nil {
		return nil, err
	}

	if firstUsedAt != nil {
		// Token was already used once before.
		if time.Since(*firstUsedAt) <= gracePeriod {
			// Within the grace period: likely a network retry — reject without
			// breaking the session so the original response can be retried.
			return nil, domainauth.ErrTokenInvalid
		}
		// Outside the grace period: potential token theft — revoke the family.
		if err := s.refreshStore.RevokeFamily(ctx, claims.FamilyID, s.refreshTTL); err != nil {
			return nil, fmt.Errorf("auth: revoke session family: %w", err)
		}
		return nil, domainauth.ErrSessionInvalidated
	}

	// Look up the user for the CURRENT username, role, and
	// MustChangePassword flag — the refresh token's claims may be stale: an
	// SSO/local user promoted or demoted after this session started must not
	// keep generating access tokens with the old role (legacy authz grants
	// ADMIN/SUPER_ADMIN PermissionAll, so a stale elevated role is a real
	// privilege-retention issue, not a cosmetic one).
	userID, err := uuid.Parse(claims.Subject)
	if err != nil {
		return nil, domainauth.ErrSessionInvalidated
	}
	u, err := s.users.FindByID(ctx, userID)
	if err != nil {
		return nil, domainauth.ErrSessionInvalidated
	}

	// Issue a rotated token pair preserving the same session family and
	// the original remember-me preference so the TTL is consistent across
	// the entire session lifetime.
	refreshTTL := s.refreshTTL
	if !claims.RememberMe {
		refreshTTL = s.refreshSessionTTL
	}

	access, err := s.tokens.IssueAccess(u.ID.String(), u.Username, u.Role, claims.FamilyID, u.MustChangePassword)
	if err != nil {
		return nil, err
	}
	refresh, err := s.tokens.IssueRefreshWithTTL(u.ID.String(), u.Username, u.Role, claims.FamilyID, claims.RememberMe, refreshTTL)
	if err != nil {
		return nil, err
	}

	return &domainauth.TokenPair{AccessToken: access, RefreshToken: refresh, RefreshTTL: refreshTTL}, nil
}

// Logout revokes the entire token family so all in-flight refresh tokens for
// this session are immediately invalidated.
func (s *Service) Logout(ctx context.Context, familyID string) error {
	if familyID == "" {
		return nil
	}
	return s.refreshStore.RevokeFamily(ctx, familyID, s.refreshTTL)
}
