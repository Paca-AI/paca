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
			},
		},
		{
			Name: "PROJECT_MEMBER",
			Permissions: []Permission{
				PermissionProjectsRead,
				PermissionTasksRead,
				PermissionTasksWrite,
				PermissionSprintsRead,
			},
		},
		{
			Name: "PROJECT_VIEWER",
			Permissions: []Permission{
				PermissionProjectsRead,
				PermissionTasksRead,
				PermissionSprintsRead,
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
