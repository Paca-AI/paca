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
	Login(ctx context.Context, email, password string) (*TokenPair, error)
	// Refresh issues a new access token given a valid refresh token.
	Refresh(ctx context.Context, refreshToken string) (string, error)
	// Logout revokes the token identified by jti.
	Logout(ctx context.Context, jti string) error
}
