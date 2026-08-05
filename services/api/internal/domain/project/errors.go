package projectdom

import "errors"

// Sentinel domain errors for the project aggregate.
var (
	ErrNotFound           = errors.New("project: not found")
	ErrNameTaken          = errors.New("project: name already in use")
	ErrNameInvalid        = errors.New("project: name is empty or invalid")
	ErrPrefixInvalid      = errors.New("project: task ID prefix must be 1–10 uppercase letters/digits")
	ErrMemberAlreadyAdded = errors.New("project: user is already a member")
	ErrMemberNotFound     = errors.New("project: member not found")
	ErrRoleNotFound       = errors.New("project: role not found")
	ErrRoleNameTaken      = errors.New("project: role name already in use")
	ErrRoleNameInvalid    = errors.New("project: role name is empty or invalid")
	ErrRoleHasMembers     = errors.New("project: role still has members assigned")
	// ErrAgentNotInvitable is returned when trying to invite a
	// project-scoped agent into a project — only global-scope agents can be
	// invited; a project-scoped agent already belongs to exactly one
	// project by construction.
	ErrAgentNotInvitable = errors.New("project: only global agents can be invited into a project")
	// ErrAgentHandleConflict is returned when inviting a global agent whose
	// handle collides with another agent (project-scoped or another
	// already-invited global agent) already visible in the target project.
	ErrAgentHandleConflict = errors.New("project: agent handle conflicts with an agent already in this project")
)
