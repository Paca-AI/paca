package user

import "errors"

// Sentinel domain errors for the user aggregate.
var (
	ErrNotFound   = errors.New("user: not found")
	ErrEmailTaken = errors.New("user: email already in use")
	ErrForbidden  = errors.New("user: forbidden")
)
