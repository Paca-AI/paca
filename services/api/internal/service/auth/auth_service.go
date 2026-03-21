// Package auth implements the authentication service.
package auth

import (
	"context"
	"errors"
	"fmt"
	"time"

	domainauth "github.com/paca/api/internal/domain/auth"
	"github.com/paca/api/internal/domain/user"
	"github.com/paca/api/internal/platform/token"
	"golang.org/x/crypto/bcrypt"
)

// Blacklist is the persistence contract for token revocation.
type Blacklist interface {
	Revoke(ctx context.Context, jti string, ttl time.Duration) error
	IsRevoked(ctx context.Context, jti string) (bool, error)
}

// Service is the concrete implementation of domain/auth.Service.
type Service struct {
	users      user.Repository
	tokens     *token.Manager
	blacklist  Blacklist
	refreshTTL time.Duration
}

// New returns a configured auth Service.
func New(users user.Repository, tokens *token.Manager, bl Blacklist, refreshTTL time.Duration) *Service {
	return &Service{
		users:      users,
		tokens:     tokens,
		blacklist:  bl,
		refreshTTL: refreshTTL,
	}
}

// Login validates credentials and returns a fresh token pair.
func (s *Service) Login(ctx context.Context, email, password string) (*domainauth.TokenPair, error) {
	u, err := s.users.FindByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, user.ErrNotFound) {
			return nil, fmt.Errorf("auth: invalid credentials")
		}
		return nil, err
	}

	if err := bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)); err != nil {
		return nil, fmt.Errorf("auth: invalid credentials")
	}

	sub := u.ID.String()
	access, err := s.tokens.IssueAccess(sub, u.Email, u.Role)
	if err != nil {
		return nil, err
	}
	refresh, err := s.tokens.IssueRefresh(sub, u.Email, u.Role)
	if err != nil {
		return nil, err
	}

	return &domainauth.TokenPair{AccessToken: access, RefreshToken: refresh}, nil
}

// Refresh issues a new access token given a valid refresh token.
func (s *Service) Refresh(ctx context.Context, refreshToken string) (string, error) {
	claims, err := s.tokens.Verify(refreshToken)
	if err != nil {
		return "", fmt.Errorf("auth: invalid refresh token: %w", err)
	}

	if claims.Kind != "refresh" {
		return "", fmt.Errorf("auth: expected refresh token")
	}

	revoked, err := s.blacklist.IsRevoked(ctx, claims.ID)
	if err != nil {
		return "", err
	}
	if revoked {
		return "", fmt.Errorf("auth: token has been revoked")
	}

	access, err := s.tokens.IssueAccess(claims.Subject, claims.Email, claims.Role)
	if err != nil {
		return "", err
	}
	return access, nil
}

// Logout revokes the token identified by jti for the remainder of its lifetime.
func (s *Service) Logout(ctx context.Context, jti string) error {
	// Revoke for the full refresh TTL to be safe.
	return s.blacklist.Revoke(ctx, jti, s.refreshTTL)
}
