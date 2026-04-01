package globalroledom

import "errors"

var (
	// ErrNotFound indicates the requested global role does not exist.
	ErrNotFound = errors.New("global role: not found")
	// ErrNameTaken indicates the role name is already in use.
	ErrNameTaken = errors.New("global role: name already in use")
	// ErrInvalidName indicates the provided role name is empty or invalid.
	ErrInvalidName = errors.New("global role: invalid name")
	// ErrHasAssignedUsers indicates the role cannot be deleted because one or
	// more users are still assigned to it (primary role FK or explicit assignment).
	ErrHasAssignedUsers = errors.New("global role: role has assigned users")
)
