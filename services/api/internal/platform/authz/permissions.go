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

	PermissionProjectsRead   Permission = "projects.read"
	PermissionProjectsWrite  Permission = "projects.write"
	PermissionProjectsCreate Permission = "projects.create"
	PermissionProjectsDelete Permission = "projects.delete"
	PermissionProjectsAll    Permission = "projects.*"

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

	PermissionDocsRead  Permission = "docs.read"
	PermissionDocsWrite Permission = "docs.write"
	PermissionDocsAll   Permission = "docs.*"

	PermissionAgentsRead  Permission = "agents.read"
	PermissionAgentsWrite Permission = "agents.write"
	PermissionAgentsAll   Permission = "agents.*"

	// PermissionEnvironmentsConnect gates gaining a live, interactive
	// session inside an already-running environment — today that's
	// minting a terminal ticket (EnvironmentHandler.TerminalTicket), which
	// hands the browser a real shell. Deliberately a separate tier from
	// Write: managing an environment's configuration (folders, SSH keys,
	// port forwards, lifecycle) doesn't imply the ability to open a shell
	// inside it, and vice versa.
	PermissionEnvironmentsRead    Permission = "environments.read"
	PermissionEnvironmentsWrite   Permission = "environments.write"
	PermissionEnvironmentsConnect Permission = "environments.connect"
	PermissionEnvironmentsAll     Permission = "environments.*"

	PermissionWorkflowsRead  Permission = "workflows.read"
	PermissionWorkflowsWrite Permission = "workflows.write"
	PermissionWorkflowsAll   Permission = "workflows.*"

	// PermissionAnnotationsResolve gates resolving/reopening a page
	// annotation (a comment pinned via the Paca browser extension) without
	// granting the ability to create or delete one — split from Write the
	// same way PermissionEnvironmentsConnect is split from
	// PermissionEnvironmentsWrite, so a role can triage/dismiss comments
	// without being able to author or remove them.
	PermissionAnnotationsRead    Permission = "annotations.read"
	PermissionAnnotationsWrite   Permission = "annotations.write"
	PermissionAnnotationsResolve Permission = "annotations.resolve"
	PermissionAnnotationsAll     Permission = "annotations.*"

	// PermissionSettingsWrite gates changes to instance-wide workspace
	// branding (logo/favicon/primary color). There is no paired
	// settings.read: the branding itself is served by an unauthenticated
	// public endpoint, so the only thing to gate is writing to it.
	PermissionSettingsWrite Permission = "settings.write"
)
