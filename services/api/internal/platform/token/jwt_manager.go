// Package jwttoken signs and verifies HS256 JWTs using golang-jwt/jwt.
package jwttoken

import (
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	domainauth "github.com/paca/api/internal/domain/auth"
)

// Manager handles JWT creation and verification.
type Manager struct {
	secret     []byte
	accessTTL  time.Duration
	refreshTTL time.Duration
}

// New returns a Manager configured with the given secret and token lifetimes.
func New(secret string, accessTTL, refreshTTL time.Duration) *Manager {
	return &Manager{
		secret:     []byte(secret),
		accessTTL:  accessTTL,
		refreshTTL: refreshTTL,
	}
}

// IssueAccess creates a signed access token for the given claims subject.
func (m *Manager) IssueAccess(sub, username, role, familyID string) (string, error) {
	return m.sign(sub, username, role, familyID, m.accessTTL, "access")
}

// IssueRefresh creates a signed refresh token.
func (m *Manager) IssueRefresh(sub, username, role, familyID string) (string, error) {
	return m.sign(sub, username, role, familyID, m.refreshTTL, "refresh")
}

func (m *Manager) sign(sub, username, role, familyID string, ttl time.Duration, kind string) (string, error) {
	now := time.Now()
	claims := domainauth.Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   sub,
			ID:        uuid.NewString(),
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
		},
		Username: username,
		Role:     role,
		Kind:     kind,
		FamilyID: familyID,
	}

	t := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := t.SignedString(m.secret)
	if err != nil {
		return "", fmt.Errorf("token: sign: %w", err)
	}
	return signed, nil
}

// Verify parses and validates a token, returning its claims.
func (m *Manager) Verify(tokenStr string) (*domainauth.Claims, error) {
	t, err := jwt.ParseWithClaims(tokenStr, &domainauth.Claims{}, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("token: unexpected signing method: %v", t.Header["alg"])
		}
		return m.secret, nil
	})
	if err != nil {
		return nil, fmt.Errorf("token: verify: %w", err)
	}

	claims, ok := t.Claims.(*domainauth.Claims)
	if !ok || !t.Valid {
		return nil, fmt.Errorf("token: invalid claims")
	}

	return claims, nil
}
