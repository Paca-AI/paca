// Package ssodom holds the SSO / OpenID Connect sign-in domain: the
// admin-configured identity providers a user may sign in with, and the link
// between a Paca account and the identity a provider asserts for it.
package ssodom

import (
	"time"

	"github.com/google/uuid"
)

// DefaultScopes are the scopes requested when a provider is created without
// any: "openid" is mandatory for OIDC, "profile" and "email" supply the name
// and email a new account is provisioned with.
var DefaultScopes = []string{"openid", "profile", "email"}

// Provider is an OpenID Connect identity provider an admin has configured.
type Provider struct {
	ID uuid.UUID
	// Slug identifies the provider in its login/callback URLs
	// (/auth/sso/{slug}/…), so it must stay stable once users have signed in
	// through it: the redirect URI registered at the provider embeds it.
	Slug        string
	DisplayName string
	// IssuerURL is the OIDC issuer; its /.well-known/openid-configuration
	// supplies every other endpoint.
	IssuerURL string
	ClientID  string
	// ClientSecret is the plaintext secret inside the service. The repository
	// stores whatever the service hands it (ciphertext when ENCRYPTION_KEY is
	// set) and never sees the key.
	ClientSecret string
	Scopes       []string
	Enabled      bool
	// AutoProvision creates a Paca account, with the default global role, the
	// first time someone signs in through this provider without a linked or
	// email-matched account.
	AutoProvision bool
	// LinkByEmail links a first-time sign-in to the existing account that
	// has the same email — only when the provider reports that email as
	// verified. Anyone who controls the provider can then sign in as any
	// account, which is why configuring providers needs settings.sso.write.
	LinkByEmail bool
	// AllowedDomains restricts sign-in to these email domains; empty allows
	// any.
	AllowedDomains []string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// Identity links a Paca user to the subject a provider asserts for them.
type Identity struct {
	ID          uuid.UUID
	UserID      uuid.UUID
	ProviderID  uuid.UUID
	Subject     string
	Email       *string
	CreatedAt   time.Time
	LastLoginAt time.Time
}

// ProviderInput carries the admin-editable fields of a provider. On update a
// nil ClientSecret keeps the stored secret, so the admin UI never needs to
// read it back.
type ProviderInput struct {
	Slug           string
	DisplayName    string
	IssuerURL      string
	ClientID       string
	ClientSecret   *string
	Scopes         []string
	Enabled        bool
	AutoProvision  bool
	LinkByEmail    bool
	AllowedDomains []string
}
