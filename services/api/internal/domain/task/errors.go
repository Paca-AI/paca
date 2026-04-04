package taskdom

import "errors"

// Sentinel domain errors for the task aggregate.
var (
	ErrTaskNotFound     = errors.New("task: not found")
	ErrTaskTitleInvalid = errors.New("task: title is empty or invalid")

	ErrTypeNotFound    = errors.New("task type: not found")
	ErrTypeNameInvalid = errors.New("task type: name is empty or invalid")

	ErrStatusNotFound        = errors.New("task status: not found")
	ErrStatusNameInvalid     = errors.New("task status: name is empty or invalid")
	ErrStatusCategoryInvalid = errors.New("task status: invalid category value")
)
