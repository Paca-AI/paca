package userdom

import "errors"

// Sentinel domain errors for the user aggregate.
var (
	ErrNotFound               = errors.New("user: not found")
	ErrUsernameTaken          = errors.New("user: username already in use")
	ErrEmailTaken             = errors.New("user: email already in use")
	ErrForbidden              = errors.New("user: forbidden")
	ErrInvalidCurrentPassword = errors.New("user: incorrect current password")
	// ErrPasswordSetTokenInvalid covers "not found", "expired", and
	// "already used" alike — deliberately not distinguished for callers, so
	// a caller can't use response differences to enumerate tokens.
	ErrPasswordSetTokenInvalid = errors.New("user: password set token invalid or expired")
)
