package authz

import (
	"strings"
	"sync"
)

// RoleDefinition binds a role name to the permissions it grants.
type RoleDefinition struct {
	Name        string
	Permissions []Permission
}

// DefaultGlobalRoles returns the built-in global role set. Computed once and
// cached: LegacyPermissionsForRole calls this on every permission check
// (hasPermissionsForActor runs per request), and the data itself is static
// for the process lifetime. Callers only ever range over the result, so a
// shared cached slice is safe to hand out.
var DefaultGlobalRoles = sync.OnceValue(func() []RoleDefinition {
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
})

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
				// No ProjectSettings*Write here: redefining task
				// types/statuses/custom fields is an Admin-level (project
				// schema) action, not a content-editing one — mirrors the
				// real per-project "Editor" role's grants (see
				// projectsvc.Service.Create). No dedicated read permission
				// exists for the schema either; PermissionTasksRead above
				// already covers viewing it. A project owner can grant write
				// access per-project via the role editor.
				PermissionSprintsRead,
				// PermissionViewsWrite is a new capability here, not a
				// preservation: view CRUD previously required
				// PermissionSprintsWrite, which PROJECT_MEMBER never held
				// (only SprintsRead). Confirmed intentional — see
				// TestDefaultProjectRoles_ProjectMemberHasNoSettingsWritePermissions'
				// sibling assertions and 000054's own comment on this same
				// grant — and called out explicitly here since it's a real
				// behavior change for every existing project's members on
				// deploy, not implied by the settings-permission split above.
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
//
// Delegates to DefaultGlobalRoles rather than hand-maintaining a second,
// parallel permission list per role name — GHSA-hjcj-373w-vq8m was exactly
// that drift: this function's own ADMIN case had been hardcoded to the bare
// PermissionAll wildcard while DefaultGlobalRoles' ADMIN entry was correctly
// scoped to global-only permissions, so any caller keyed off the legacy role
// claim (the authz middleware included, via claims.Role) granted a global
// ADMIN every permission — including project-scoped ones like
// environments.connect — for any project UUID in the request, with no
// project-membership check. A single source of truth makes that class of
// drift impossible going forward.
func LegacyPermissionsForRole(role string) []Permission {
	normalized := strings.ToUpper(strings.TrimSpace(role))
	for _, def := range DefaultGlobalRoles() {
		if def.Name == normalized {
			return def.Permissions
		}
	}
	return nil
}
