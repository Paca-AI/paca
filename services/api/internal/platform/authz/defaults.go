package authz

import "strings"

// RoleDefinition binds a role name to the permissions it grants.
type RoleDefinition struct {
	Name        string
	Permissions []Permission
}

// DefaultGlobalRoles returns the built-in global role set.
func DefaultGlobalRoles() []RoleDefinition {
	return []RoleDefinition{
		{
			Name:        "SUPER_ADMIN",
			Permissions: []Permission{PermissionAll},
		},
		{
			Name: "ADMIN",
			Permissions: []Permission{
				PermissionUsersAll,
				PermissionGlobalRolesAll,
				PermissionProjectsAll,
				PermissionSettingsWrite,
			},
		},
		{
			Name: "USER",
			Permissions: []Permission{
				PermissionUsersRead,
			},
		},
	}
}

// DefaultProjectRoles returns built-in project role templates.
func DefaultProjectRoles() []RoleDefinition {
	return []RoleDefinition{
		{
			Name: "PROJECT_OWNER",
			Permissions: []Permission{
				PermissionProjectsAll,
				PermissionProjectMembersAll,
				PermissionProjectRolesAll,
				PermissionTasksAll,
				PermissionSprintsAll,
				PermissionDocsAll,
				PermissionAgentsAll,
				PermissionWorkflowsAll,
			},
		},
		{
			Name: "PROJECT_MANAGER",
			Permissions: []Permission{
				PermissionProjectsRead,
				PermissionProjectsWrite,
				PermissionProjectMembersRead,
				PermissionProjectMembersWrite,
				PermissionTasksAll,
				PermissionSprintsAll,
				PermissionDocsAll,
				PermissionAgentsAll,
				PermissionWorkflowsAll,
			},
		},
		{
			Name: "PROJECT_MEMBER",
			Permissions: []Permission{
				PermissionProjectsRead,
				PermissionProjectMembersRead,
				PermissionProjectRolesRead,
				PermissionTasksRead,
				PermissionTasksWrite,
				PermissionSprintsRead,
				PermissionDocsRead,
				PermissionDocsWrite,
				PermissionAgentsRead,
				PermissionAgentsWrite,
				PermissionWorkflowsRead,
				PermissionWorkflowsWrite,
			},
		},
		{
			Name: "PROJECT_VIEWER",
			Permissions: []Permission{
				PermissionProjectsRead,
				PermissionProjectMembersRead,
				PermissionProjectRolesRead,
				PermissionTasksRead,
				PermissionSprintsRead,
				PermissionDocsRead,
				PermissionAgentsRead,
				PermissionWorkflowsRead,
			},
		},
	}
}

// LegacyPermissionsForRole preserves compatibility with the existing
// users.role claim until all callers are migrated to explicit role assignment.
func LegacyPermissionsForRole(role string) []Permission {
	normalized := strings.ToUpper(strings.TrimSpace(role))
	switch normalized {
	case "SUPER_ADMIN":
		return []Permission{PermissionAll}
	case "ADMIN":
		return []Permission{PermissionAll}
	case "USER":
		return []Permission{PermissionUsersRead}
	default:
		return nil
	}
}
