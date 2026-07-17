package userdom

import "errors"

// Sentinel domain errors for the user aggregate.
var (
	ErrNotFound               = errors.New("user: not found")
	ErrUsernameTaken          = errors.New("user: username already in use")
	ErrForbidden              = errors.New("user: forbidden")
	ErrInvalidCurrentPassword = errors.New("user: incorrect current password")
	// ErrIdentityTaken indicates the email or oidc_sub is already linked to
	// another account (Galaxy ADR-038 partial unique indexes).
	ErrIdentityTaken = errors.New("user: email or oidc_sub already in use")
)
