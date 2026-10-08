package roledom

import "errors"

var (
	// ErrNotFound: no role with that id (in the requested scope).
	ErrNotFound = errors.New("role: not found")
	// ErrNameTaken: another role in the same scope already has the name.
	ErrNameTaken = errors.New("role: name already in use")
	// ErrNameInvalid: the name is empty (after trimming) or too long.
	ErrNameInvalid = errors.New("role: invalid name")
	// ErrSystemRole: system roles cannot be edited or deleted.
	ErrSystemRole = errors.New("role: system roles cannot be modified")
	// ErrIsDefault: the default role cannot be deleted; make another role the
	// default first.
	ErrIsDefault = errors.New("role: the default role cannot be deleted")
	// ErrLastWildcard: the change would leave no account holding a
	// platform-wide "*" on "*", i.e. nobody able to administer the workspace.
	ErrLastWildcard = errors.New("role: this would remove the last platform-wide full-access attachment")
	// ErrNoDefault: no platform role is marked as the default, so there is
	// nothing to attach to a new account.
	ErrNoDefault = errors.New("role: no default role is set")
	// ErrRoleRequired: a request that gives a principal its first project
	// roles (adding a member or an agent) named none.
	ErrRoleRequired = errors.New("role: at least one role is required")
	// ErrNotAttachable: a role id is unknown, or the role cannot be attached
	// in the requested scope (a project-owned role outside its project, or
	// anywhere platform-wide).
	ErrNotAttachable = errors.New("role: role cannot be attached here")
	// ErrUserNotFound: the user does not exist or is deleted.
	ErrUserNotFound = errors.New("role: user not found")
	// ErrAgentNotFound: the global agent does not exist or is deleted.
	ErrAgentNotFound = errors.New("role: agent not found")
	// ErrProjectNotFound: the project does not exist or is deleted.
	ErrProjectNotFound = errors.New("role: project not found")
	// ErrMemberNotFound: the project member does not exist or was removed.
	ErrMemberNotFound = errors.New("role: project member not found")
)
