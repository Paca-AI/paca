package globalroledom

import "errors"

var (
	// ErrNotFound indicates the requested global role does not exist.
	ErrNotFound = errors.New("global role: not found")
	// ErrNameTaken indicates the role name is already in use.
	ErrNameTaken = errors.New("global role: name already in use")
	// ErrInvalidName indicates the provided role name is empty or invalid.
	ErrInvalidName = errors.New("global role: invalid name")
)
