package ssodom

import (
	"context"

	"github.com/google/uuid"
)

// Repository persists providers and identities.
type Repository interface {
	ListProviders(ctx context.Context) ([]*Provider, error)
	FindProviderByID(ctx context.Context, id uuid.UUID) (*Provider, error)
	FindProviderBySlug(ctx context.Context, slug string) (*Provider, error)
	// CreateProvider and UpdateProvider return ErrSlugTaken on a slug
	// conflict.
	CreateProvider(ctx context.Context, p *Provider) error
	UpdateProvider(ctx context.Context, p *Provider) error
	DeleteProvider(ctx context.Context, id uuid.UUID) error

	// FindIdentity returns nil, nil when no account is linked to the
	// subject.
	FindIdentity(ctx context.Context, providerID uuid.UUID, subject string) (*Identity, error)
	// LockIdentity serializes first sign-ins for one (provider, subject):
	// it blocks until no other caller holds the lock, then holds it until
	// unlock is called. Resolving and linking an account under it means two
	// simultaneous callbacks for the same person can never both create one.
	LockIdentity(ctx context.Context, providerID uuid.UUID, subject string) (unlock func(), err error)
	// CreateIdentity returns ErrIdentityExists when the (provider, subject)
	// pair is already linked.
	CreateIdentity(ctx context.Context, id *Identity) error
	// TouchIdentity records a sign-in: sets last_login_at to now and
	// refreshes the stored email.
	TouchIdentity(ctx context.Context, id uuid.UUID, email *string) error
}

// StateStore holds the per-attempt sign-in state between the redirect to the
// provider and its callback. Take is single-use.
type StateStore interface {
	Put(ctx context.Context, state string, a *AuthAttempt) error
	// Take returns and deletes the attempt; nil, nil when absent/expired.
	Take(ctx context.Context, state string) (*AuthAttempt, error)
}

// AuthAttempt is the server-side half of one sign-in attempt.
type AuthAttempt struct {
	ProviderID   uuid.UUID `json:"provider_id"`
	Nonce        string    `json:"nonce"`
	CodeVerifier string    `json:"code_verifier"`
	// BrowserBinding must match the value in the caller's sso_state cookie,
	// so a callback URL lured into another browser cannot complete someone
	// else's sign-in (login CSRF).
	BrowserBinding string `json:"browser_binding"`
	RememberMe     bool   `json:"remember_me"`
	// Redirect is the validated in-app path to land on after sign-in.
	Redirect string `json:"redirect"`
}
