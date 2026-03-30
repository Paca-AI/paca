package authz

// Permission is a stable machine-readable permission key.
type Permission string

// Stable permission keys used by the authorization system.
const (
	PermissionAll Permission = "*"

	PermissionUsersRead   Permission = "users.read"
	PermissionUsersWrite  Permission = "users.write"
	PermissionUsersDelete Permission = "users.delete"
	PermissionUsersAll    Permission = "users.*"

	PermissionGlobalRolesRead   Permission = "global_roles.read"
	PermissionGlobalRolesWrite  Permission = "global_roles.write"
	PermissionGlobalRolesAssign Permission = "global_roles.assign"
	PermissionGlobalRolesAll    Permission = "global_roles.*"

	PermissionProjectsRead  Permission = "projects.read"
	PermissionProjectsWrite Permission = "projects.write"
	PermissionProjectsAll   Permission = "projects.*"

	PermissionProjectMembersRead  Permission = "project.members.read"
	PermissionProjectMembersWrite Permission = "project.members.write"
	PermissionProjectMembersAll   Permission = "project.members.*"

	PermissionProjectRolesRead  Permission = "project.roles.read"
	PermissionProjectRolesWrite Permission = "project.roles.write"
	PermissionProjectRolesAll   Permission = "project.roles.*"

	PermissionTasksRead  Permission = "tasks.read"
	PermissionTasksWrite Permission = "tasks.write"
	PermissionTasksAll   Permission = "tasks.*"

	PermissionSprintsRead  Permission = "sprints.read"
	PermissionSprintsWrite Permission = "sprints.write"
	PermissionSprintsAll   Permission = "sprints.*"
)
