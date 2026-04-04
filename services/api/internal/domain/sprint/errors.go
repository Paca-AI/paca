package sprintdom

import "errors"

// Sentinel domain errors for the sprint aggregate.
var (
	ErrSprintNotFound      = errors.New("sprint: not found")
	ErrSprintNameInvalid   = errors.New("sprint: name is empty or invalid")
	ErrSprintStatusInvalid = errors.New("sprint: invalid status value")
)
