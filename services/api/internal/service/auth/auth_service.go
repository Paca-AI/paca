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
	users        userdom.Repository
	tokens       *jwttoken.Manager
	refreshStore RefreshTokenStore
	refreshTTL   time.Duration
}

// New returns a configured auth Service.
func New(users userdom.Repository, tokens *jwttoken.Manager, refreshStore RefreshTokenStore, refreshTTL time.Duration) *Service {
	return &Service{
		users:        users,
		tokens:       tokens,
		refreshStore: refreshStore,
		refreshTTL:   refreshTTL,
	}
}

// Login validates credentials and returns a fresh token pair.
func (s *Service) Login(ctx context.Context, username, password string) (*domainauth.TokenPair, error) {
	u, err := s.users.FindByUsername(ctx, username)
	if err != nil {
		if errors.Is(err, userdom.ErrNotFound) {
			return nil, fmt.Errorf("auth: invalid credentials")
		}
		return nil, err
	}

	if err := bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)); err != nil {
		return nil, fmt.Errorf("auth: invalid credentials")
	}

	familyID := uuid.NewString()
	sub := u.ID.String()

	access, err := s.tokens.IssueAccess(sub, u.Username, u.Role, familyID)
	if err != nil {
		return nil, err
	}
	refresh, err := s.tokens.IssueRefresh(sub, u.Username, u.Role, familyID)
	if err != nil {
		return nil, err
	}

	return &domainauth.TokenPair{AccessToken: access, RefreshToken: refresh}, nil
}

// Refresh validates a refresh token and issues a rotated token pair.
// If the same token is presented twice outside the grace period, the entire
// session family is revoked to mitigate token-theft scenarios.
func (s *Service) Refresh(ctx context.Context, refreshToken string) (*domainauth.TokenPair, error) {
	claims, err := s.tokens.Verify(refreshToken)
	if err != nil {
		return nil, fmt.Errorf("auth: invalid refresh token: %w", err)
	}

	if claims.Kind != "refresh" {
		return nil, fmt.Errorf("auth: expected refresh token")
	}

	// Fast path: reject immediately if the family was already invalidated.
	revoked, err := s.refreshStore.IsFamilyRevoked(ctx, claims.FamilyID)
	if err != nil {
		return nil, err
	}
	if revoked {
		return nil, fmt.Errorf("auth: session has been invalidated")
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
			return nil, fmt.Errorf("auth: token recently used, please retry with the latest token")
		}
		// Outside the grace period: potential token theft — revoke the family.
		if err := s.refreshStore.RevokeFamily(ctx, claims.FamilyID, s.refreshTTL); err != nil {
			return nil, fmt.Errorf("auth: token reuse detected, failed to revoke session family: %w", err)
		}
		return nil, fmt.Errorf("auth: token reuse detected, session invalidated")
	}

	// Issue a rotated token pair preserving the same session family.
	access, err := s.tokens.IssueAccess(claims.Subject, claims.Username, claims.Role, claims.FamilyID)
	if err != nil {
		return nil, err
	}
	refresh, err := s.tokens.IssueRefresh(claims.Subject, claims.Username, claims.Role, claims.FamilyID)
	if err != nil {
		return nil, err
	}

	return &domainauth.TokenPair{AccessToken: access, RefreshToken: refresh}, nil
}

// Logout revokes the entire token family so all in-flight refresh tokens for
// this session are immediately invalidated.
func (s *Service) Logout(ctx context.Context, familyID string) error {
	if familyID == "" {
		return nil
	}
	return s.refreshStore.RevokeFamily(ctx, familyID, s.refreshTTL)
}
