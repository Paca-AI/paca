// Package auth implements the authentication service.
package auth

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	domainauth "github.com/paca/api/internal/domain/auth"
	userdom "github.com/paca/api/internal/domain/user"
	jwttoken "github.com/paca/api/internal/platform/token"
	"golang.org/x/crypto/bcrypt"
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
}

// New returns a configured auth Service.
func New(users userdom.Repository, tokens *jwttoken.Manager, refreshStore RefreshTokenStore, refreshTTL, refreshSessionTTL time.Duration) *Service {
	return &Service{
		users:             users,
		tokens:            tokens,
		refreshStore:      refreshStore,
		refreshTTL:        refreshTTL,
		refreshSessionTTL: refreshSessionTTL,
	}
}

// Login validates credentials and returns a fresh token pair.
// When rememberMe is true, the refresh token uses the long-lived TTL
// (JWT_REFRESH_TTL); when false, the shorter session TTL is used
// (JWT_REFRESH_SESSION_TTL, default 24 h).
func (s *Service) Login(ctx context.Context, username, password string, rememberMe bool) (*domainauth.TokenPair, error) {
	u, err := s.users.FindByUsername(ctx, username)
	if err != nil {
		if errors.Is(err, userdom.ErrNotFound) {
			return nil, domainauth.ErrInvalidCredentials
		}
		return nil, err
	}

	if err := bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)); err != nil {
		return nil, domainauth.ErrInvalidCredentials
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

	// Look up the user to get the current MustChangePassword flag; this
	// ensures that if an admin resets the password while the user has an
	// active session, the very next Refresh call will carry the updated flag.
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

	access, err := s.tokens.IssueAccess(claims.Subject, claims.Username, claims.Role, claims.FamilyID, u.MustChangePassword)
	if err != nil {
		return nil, err
	}
	refresh, err := s.tokens.IssueRefreshWithTTL(claims.Subject, claims.Username, claims.Role, claims.FamilyID, claims.RememberMe, refreshTTL)
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
