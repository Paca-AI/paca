package ssodom

import "errors"

// Sentinel domain errors for SSO.
var (
	// ErrProviderNotFound indicates no provider has the given ID or slug —
	// or, on the public sign-in routes, that it exists but is disabled.
	ErrProviderNotFound = errors.New("sso: provider not found")
	// ErrSlugTaken indicates another provider already uses the slug.
	ErrSlugTaken = errors.New("sso: provider slug already in use")
	// ErrIdentityExists indicates the (provider, subject) pair is already
	// linked to an account — a concurrent first sign-in got there first.
	ErrIdentityExists = errors.New("sso: identity already linked")
	// ErrInvalidProvider indicates a provider's configuration is invalid
	// (bad slug, missing issuer/client ID, non-http(s) issuer…). Wrapped
	// with the specific reason.
	ErrInvalidProvider = errors.New("sso: invalid provider configuration")
	// ErrDiscoveryFailed indicates the issuer's OIDC discovery document
	// could not be fetched or is invalid.
	ErrDiscoveryFailed = errors.New("sso: issuer discovery failed")

	// The errors below end a sign-in attempt. Each maps to a distinct
	// ?sso_error= code on the login page.

	// ErrStateInvalid indicates the callback's state is unknown, expired,
	// already used, or not bound to this browser.
	ErrStateInvalid = errors.New("sso: invalid or expired sign-in state")
	// ErrExchangeFailed indicates the code exchange or ID token
	// verification failed.
	ErrExchangeFailed = errors.New("sso: provider sign-in failed")
	// ErrEmailNotAllowed indicates the identity's email domain is not in the
	// provider's allow-list, or the provider did not verify the email.
	ErrEmailNotAllowed = errors.New("sso: email not allowed")
	// ErrNoAccount indicates no account is linked to the identity and the
	// provider neither auto-provisions nor links one by email.
	ErrNoAccount = errors.New("sso: no account for this identity")
	// ErrAccountExists indicates the identity's verified email already
	// belongs to an account the provider may not link to.
	ErrAccountExists = errors.New("sso: an account with this email already exists")
)
