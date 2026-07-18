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
	// ErrPermissionCeilingExceeded indicates the caller attempted to grant a
	// member a project role whose permissions exceed the caller's own effective
	// permissions for the project (a privilege-escalation attempt via the
	// shared role templates, e.g. PROJECT_OWNER). Enforced as a grant ceiling.
	ErrPermissionCeilingExceeded = errors.New("project: role grants permissions beyond the caller's own")
)
