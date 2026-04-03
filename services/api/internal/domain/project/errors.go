package projectdom

import "errors"

// Sentinel domain errors for the project aggregate.
var (
	ErrNotFound           = errors.New("project: not found")
	ErrNameTaken          = errors.New("project: name already in use")
	ErrNameInvalid        = errors.New("project: name is empty or invalid")
	ErrMemberAlreadyAdded = errors.New("project: user is already a member")
	ErrMemberNotFound     = errors.New("project: member not found")
	ErrRoleNotFound       = errors.New("project: role not found")
	ErrRoleNameTaken      = errors.New("project: role name already in use")
	ErrRoleNameInvalid    = errors.New("project: role name is empty or invalid")
	ErrRoleHasMembers     = errors.New("project: role still has members assigned")
)
