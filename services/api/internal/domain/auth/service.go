package auth

import (
	"context"
	"time"
)

// TokenPair holds an access token and a companion refresh token.
// RefreshTTL is the effective lifetime of the refresh token so callers (e.g.
// the HTTP handler) can align the cookie MaxAge with the JWT expiry.
type TokenPair struct {
	AccessToken  string
	RefreshToken string
	RefreshTTL   time.Duration
}

// Service defines the authentication contract.
type Service interface {
	// Login validates credentials and returns a fresh token pair.
	// rememberMe=false issues a short-lived session (see JWT_REFRESH_SESSION_TTL);
	// rememberMe=true issues a long-lived persistent session (JWT_REFRESH_TTL).
	Login(ctx context.Context, username, password string, rememberMe bool) (*TokenPair, error)
	// Refresh validates a refresh token and issues a rotated token pair.
	// Token reuse outside the grace period revokes the entire session family.
	Refresh(ctx context.Context, refreshToken string) (*TokenPair, error)
	// Logout revokes the entire token family identified by familyID.
	Logout(ctx context.Context, familyID string) error
}
