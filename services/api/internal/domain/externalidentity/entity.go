// Package externalidentity holds the external-identity aggregate: the binding
// between an IdP-provided identity (issuer + subject — the only stable key the
// OIDC spec guarantees) and an internal Paca user.
package externalidentity

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	userdom "github.com/Paca-AI/api/internal/domain/user"
)

// ErrNotFound is returned when no external identity matches the given key.
var ErrNotFound = errors.New("external identity: not found")

// Identity binds one external IdP identity to one Paca user.
type Identity struct {
	ID uuid.UUID
	// UserID is the internal Paca user this identity authenticates.
	UserID uuid.UUID
	// Provider labels the protocol family ("oidc" for the built-in flow).
	Provider string
	// Issuer is the OIDC issuer URL (the "iss" claim), e.g.
	// https://id.example.com/realms/company. Stored without a trailing slash.
	Issuer string
	// Subject is the OIDC subject ("sub" claim) — opaque, IdP-specific.
	Subject string
	// CreatedAt is when the binding was first established.
	CreatedAt time.Time
	// LastLoginAt is refreshed on every successful external login.
	LastLoginAt time.Time
}

// Repository defines persistence operations for external identities.
type Repository interface {
	// FindByIssuerSubject returns the identity for the (issuer, subject)
	// pair, or ErrNotFound. The pair is the stable external identity key —
	// email/username are never used for lookup.
	FindByIssuerSubject(ctx context.Context, issuer, subject string) (*Identity, error)
	// TouchLastLogin refreshes the identity's last-login timestamp.
	TouchLastLogin(ctx context.Context, id uuid.UUID) error
	// ProvisionWithUser atomically creates a new Paca user together with its
	// external identity in a single database transaction, so a JIT-provisioned
	// login can never leave an orphan user (or an orphan identity) behind.
	// Username/email unique-constraint violations surface as the same
	// userdom sentinel errors the user repository returns.
	ProvisionWithUser(ctx context.Context, u *userdom.User, identity *Identity) error
}
