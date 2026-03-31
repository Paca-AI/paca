package userdom

import "errors"

// Sentinel domain errors for the user aggregate.
var (
	ErrNotFound               = errors.New("user: not found")
	ErrUsernameTaken          = errors.New("user: username already in use")
	ErrForbidden              = errors.New("user: forbidden")
	ErrInvalidCurrentPassword = errors.New("user: incorrect current password")
)
