package auth

import "errors"

// Auth domain sentinel errors.
var (
	// ErrInvalidCredentials is returned when a login attempt fails due to an
	// unknown username or incorrect password.  The message is intentionally
	// vague to prevent username enumeration.
	ErrInvalidCredentials = errors.New("invalid username or password")

	// ErrTokenInvalid is returned when a token cannot be verified — it may be
	// malformed, have an invalid signature, or be expired.
	ErrTokenInvalid = errors.New("invalid or expired token")

	// ErrSessionInvalidated is returned when the token's session family has
	// been explicitly revoked (e.g. due to detected token reuse or logout).
	ErrSessionInvalidated = errors.New("session has been invalidated")
)
