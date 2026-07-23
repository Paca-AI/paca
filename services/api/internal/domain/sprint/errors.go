package sprintdom

import "errors"

// Sentinel domain errors for the sprint aggregate.
var (
	ErrSprintNotFound        = errors.New("sprint: not found")
	ErrSprintNameInvalid     = errors.New("sprint: name is empty or invalid")
	ErrSprintStatusInvalid   = errors.New("sprint: invalid status value")
	ErrSprintAlreadyComplete = errors.New("sprint: already completed")

	ErrViewNotFound             = errors.New("sprint view: not found")
	ErrViewNameInvalid          = errors.New("sprint view: name is empty or invalid")
	ErrViewTypeInvalid          = errors.New("sprint view: invalid view type")
	ErrViewIsLastView           = errors.New("sprint view: cannot delete the last remaining view")
	ErrViewReorderInvalid       = errors.New("sprint view: provided view IDs do not match interaction views")
	ErrViewPluginConfigRequired = errors.New("sprint view: plugin_id and plugin_component are required when view_type is 'plugin'")
)
