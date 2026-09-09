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
				// Global agents and plugins were previously left off this
				// list — an ADMIN got 403s managing either, masked in
				// practice only for accounts whose legacy role-name claim
				// happened to also read "ADMIN" (which bypasses this table
				// entirely via LegacyPermissionsForRole's blanket wildcard).
				PermissionAgentsAll,
				PermissionPluginsAll,
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
				PermissionProjectSettingsAll,
				PermissionSprintsAll,
				PermissionViewsAll,
				PermissionDocsAll,
				PermissionAgentsAll,
				PermissionConversationsAll,
				PermissionWorkflowsAll,
				PermissionEnvironmentsAll,
				PermissionAnnotationsAll,
			},
		},
		{
			Name: "PROJECT_MANAGER",
			// PermissionProjectsRead is deliberately omitted here and below —
			// AuthzPermissionStore.ListProjectPermissions grants it to any
			// active project member unconditionally, so listing it per-role
			// would be redundant (see that method's doc comment). Global-
			// scope projects.read (an admin browsing projects they aren't a
			// member of) is unaffected — this only covers the project-scoped
			// resolution.
			Permissions: []Permission{
				PermissionProjectsWrite,
				PermissionProjectMembersRead,
				PermissionProjectMembersWrite,
				PermissionTasksAll,
				PermissionProjectSettingsAll,
				PermissionSprintsAll,
				PermissionViewsAll,
				PermissionDocsAll,
				PermissionAgentsAll,
				PermissionConversationsAll,
				PermissionWorkflowsAll,
				PermissionEnvironmentsAll,
				PermissionAnnotationsAll,
			},
		},
		{
			Name: "PROJECT_MEMBER",
			Permissions: []Permission{
				PermissionProjectMembersRead,
				PermissionProjectRolesRead,
				PermissionTasksRead,
				PermissionTasksWrite,
				// Preserves what PROJECT_MEMBER could already do via
				// tasks.write before the split above — a project owner can
				// now tighten this per-project via the role editor, which is
				// the actual point of splitting these out.
				PermissionProjectSettingsAll,
				PermissionSprintsRead,
				PermissionViewsRead,
				PermissionViewsWrite,
				PermissionDocsRead,
				PermissionDocsWrite,
				PermissionAgentsRead,
				PermissionAgentsWrite,
				PermissionConversationsRead,
				PermissionConversationsWrite,
				PermissionWorkflowsRead,
				PermissionWorkflowsWrite,
				PermissionEnvironmentsRead,
				PermissionEnvironmentsWrite,
				PermissionEnvironmentsConnect,
				PermissionAnnotationsRead,
				PermissionAnnotationsWrite,
				PermissionAnnotationsResolve,
			},
		},
		{
			Name: "PROJECT_VIEWER",
			Permissions: []Permission{
				PermissionProjectMembersRead,
				PermissionProjectRolesRead,
				PermissionTasksRead,
				PermissionProjectSettingsTaskTypesRead,
				PermissionProjectSettingsTaskStatusesRead,
				PermissionProjectSettingsCustomFieldsRead,
				PermissionSprintsRead,
				PermissionViewsRead,
				PermissionDocsRead,
				PermissionAgentsRead,
				PermissionConversationsRead,
				PermissionWorkflowsRead,
				PermissionEnvironmentsRead,
				PermissionAnnotationsRead,
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
