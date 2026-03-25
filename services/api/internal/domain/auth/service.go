package auth

import "context"

// TokenPair holds an access token and a companion refresh token.
type TokenPair struct {
	AccessToken  string
	RefreshToken string
}

// Service defines the authentication contract.
type Service interface {
	// Login validates credentials and returns a fresh token pair.
	Login(ctx context.Context, username, password string) (*TokenPair, error)
	// Refresh validates a refresh token and issues a rotated token pair.
	// Token reuse outside the grace period revokes the entire session family.
	Refresh(ctx context.Context, refreshToken string) (*TokenPair, error)
	// Logout revokes the entire token family identified by familyID.
	Logout(ctx context.Context, familyID string) error
}
